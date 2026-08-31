package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
)

// runDump drives the real cmdDump by swapping stdin/stdout and returns the
// bytes written to stdout.
func runDump(t *testing.T, inputPath string, args ...string) []byte {
	t.Helper()
	in, err := os.Open(inputPath)
	if err != nil {
		t.Fatalf("open input: %v", err)
	}
	defer in.Close()

	out, err := os.CreateTemp(t.TempDir(), "dump-out-*.txt")
	if err != nil {
		t.Fatalf("temp out: %v", err)
	}
	defer out.Close()

	oldIn, oldOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = in, out
	err = cmdDump(args)
	os.Stdin, os.Stdout = oldIn, oldOut
	if err != nil {
		t.Fatalf("cmdDump %v: %v", args, err)
	}

	b, err := os.ReadFile(out.Name())
	if err != nil {
		t.Fatalf("read out: %v", err)
	}
	return b
}

// goldenDump pairs a curated workflow with its full `comfyctl dump` output.
// These were captured from current behavior and deliberately kept small: the
// chosen workflows are single-prompt/single-seed and parse deterministically,
// so their dump output is stable across finder tweaks. A change here is a real
// formatting or value regression, not heuristic noise.
func TestGoldenDump(t *testing.T) {
	cases := []struct {
		file string
		want string
	}{
		{
			file: "testdata/image_flux2_text_to_image.json",
			want: `Found batch size: 1
Failed to find frames per second: Unable to find fps in the workflow
Found height of output artifact: 1024
Failed to find input image: Unable to find source image in the workflow
Failed to find negative prompt: Unable to find negative prompt in the workflow
Found positive prompt: high fashion, vintage couture, street photography, luxury fashion shoot, neo brutalist architecture, pastel paints
Found seed: 1027111520328378
Found width of output artifact: 1024
`,
		},
		{
			file: "testdata/image_flux2_text_to_image_9b.json",
			want: `Found batch size: 1
Failed to find frames per second: Unable to find fps in the workflow
Found height of output artifact: 1024
Failed to find input image: Unable to find source image in the workflow
Found negative prompt: 
Found positive prompt: A vintage motorcycle parked in front of a retro diner at sunset, warm orange and pink sky, neon signs glowing, 80s vintage photo style, film grain, warm color cast
Found seed: 145965955694731
Found width of output artifact: 1024
`,
		},
		{
			file: "testdata/video_hunyuan_video_1.5_720p_t2v.json",
			want: `Found batch size: 1
Found frames per second: 24
Found height of output artifact: 720
Failed to find input image: Unable to find source image in the workflow
Found negative prompt: 
Found positive prompt: A paper airplane released from the top of a skyscraper, gliding through urban canyons, crossing traffic, flying over streets, spiraling upward between buildings. The camera follows the paper airplane's perspective, shooting cityscape in first-person POV, finally flying toward the sunset, disappearing in golden light. Creative camera movement, free perspective, dreamlike colors.
Found seed: 887963123424675
Found width of output artifact: 1280
`,
		},
	}

	for _, c := range cases {
		name := strings.TrimSuffix(c.file[strings.LastIndex(c.file, "/")+1:], ".json")
		t.Run(name, func(t *testing.T) {
			if got := string(runDump(t, c.file)); got != c.want {
				t.Errorf("dump output mismatch\ngot:\n%q\nwant:\n%q", got, c.want)
			}
		})
	}
}

// TestGoldenSet locks in `set`'s value semantics on a deterministic workflow:
// int roles must round-trip as exact json.Number values with no float
// corruption and no scientific notation. seed random must yield a canonical
// int64 (already covered more deeply by TestSetSeedRandom; this is the light
// golden form).
func TestGoldenSet(t *testing.T) {
	// video_hunyuan carries single-valued width/height (node 124) and a single
	// seed (node 129, noise_seed), so int roles serialize at predictable keys
	// and update exactly one node. The assertions key off the serialized bytes
	// (exact `key: value` integer literals, no scientific notation) and a
	// re-parse that must preserve the values as canonical json.Number.
	path := "testdata/video_hunyuan_video_1.5_720p_t2v.json"

	t.Run("set seed large", func(t *testing.T) {
		const wantSeed = "1234567890123456"
		out := runSet(t, path, "seed", wantSeed)
		if !bytes.Contains(out, []byte(`"noise_seed": `+wantSeed)) {
			t.Errorf("output missing exact seed literal `\"noise_seed\": %s`:\n%s", wantSeed, out)
		}
		assertCanonicalInts(t, out, "noise_seed", wantSeed)
	})

	t.Run("set width", func(t *testing.T) {
		out := runSet(t, path, "width", "512")
		if !bytes.Contains(out, []byte(`"width": 512`)) {
			t.Errorf("output missing exact width literal, may be float/scientific:\n%s", out)
		}
		assertCanonicalInts(t, out, "width", "512")
	})

	t.Run("set height", func(t *testing.T) {
		out := runSet(t, path, "height", "768")
		if !bytes.Contains(out, []byte(`"height": 768`)) {
			t.Errorf("output missing exact height literal, may be float/scientific:\n%s", out)
		}
		assertCanonicalInts(t, out, "height", "768")
	})

	t.Run("set seed random canonical int64", func(t *testing.T) {
		out := runSet(t, path, "seed", "random")
		cw, err := OpenComfyWorkflow(bytes.NewReader(out))
		if err != nil {
			t.Fatalf("output does not parse: %v", err)
		}
		refs, err := FindSeed(cw)
		if err != nil {
			t.Fatalf("FindSeed: %v", err)
		}
		for _, ref := range refs {
			v, err := cw.Resolve(ref)
			if err != nil {
				t.Fatalf("resolve seed: %v", err)
			}
			num, ok := v.(json.Number)
			if !ok {
				t.Fatalf("seed resolved to %T, want json.Number", v)
			}
			if n, err := strconv.ParseInt(num.String(), 10, 64); err != nil || strconv.FormatInt(n, 10) != num.String() {
				t.Errorf("random seed %s is not a canonical int64", num.String())
			}
		}
	})
}

// assertCanonicalInts re-parses serialized set output and asserts every value
// under the numeric input key is a json.Number that round-trips as the exact
// integer literal (no float corruption, no scientific notation).
func assertCanonicalInts(t *testing.T, out []byte, key, want string) {
	t.Helper()
	cw, err := OpenComfyWorkflow(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("output does not parse: %v", err)
	}
	checked := 0
	checkNum := func(v json.Number) string {
		if v.String() != want {
			return "got " + v.String() + ", want exact json.Number " + want
		}
		if n, err := strconv.ParseInt(v.String(), 10, 64); err != nil || strconv.FormatInt(n, 10) != v.String() {
			return v.String() + " is not a canonical int64"
		}
		return ""
	}
	for _, node := range cw.Nodes {
		in, ok := node.Inputs[key]
		if !ok {
			continue
		}
		checked++
		if msg := checkNum(in.Number); msg != "" {
			t.Errorf("input %q: %s", key, msg)
		}
	}
	if checked == 0 {
		t.Errorf("no input key %q found in output", key)
	}
}

// TestGoldenDumpMarkers verifies dump honors markers: a custom role marked on
// a node is resolved by `dump <role>` and shows up in `dump` (all roles) via
// FindAllMarkedRoles, while unmarked roles still fall back to heuristics.
func TestGoldenDumpMarkers(t *testing.T) {
	const markedPrompt = "high fashion, vintage couture, street photography, luxury fashion shoot, neo brutalist architecture, pastel paints"

	dst := copyWorkflowToTemp(t, "testdata/image_flux2_text_to_image.json")
	if err := markFile(t, dst, "myprompt", "98:6:text"); err != nil {
		t.Fatalf("mark: %v", err)
	}

	t.Run("dump single custom role", func(t *testing.T) {
		want := "Found custom role marker 'myprompt': " + markedPrompt + "\n"
		if got := string(runDump(t, dst, "myprompt")); got != want {
			t.Errorf("dump myprompt mismatch\ngot:\n%q\nwant:\n%q", got, want)
		}
	})

	t.Run("dump all includes marked role", func(t *testing.T) {
		out := string(runDump(t, dst))
		if !strings.Contains(out, "Found custom role marker 'myprompt': "+markedPrompt) {
			t.Errorf("dump all missing marked role line:\n%s", out)
		}
		// The unmarked 'positive' still resolves via the same heuristics.
		if !strings.Contains(out, "Found positive prompt: "+markedPrompt) {
			t.Errorf("dump all missing heuristic positive line:\n%s", out)
		}
	})
}
