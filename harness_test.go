package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// testdataFiles returns every workflow JSON under testdata/, sorted for
// deterministic output.
func testdataFiles(t *testing.T) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join("testdata", "*.json"))
	if err != nil {
		t.Fatalf("globbing testdata: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("no testdata/*.json files found")
	}
	sort.Strings(matches)
	return matches
}

func openFile(t *testing.T, path string) (ComfyWorkflow, error) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}
	defer f.Close()
	return OpenComfyWorkflow(f)
}

// TestParse is the compliance contract: every vanilla workflow we vendor must
// parse without error. A failure here is a concrete noncompliance to fix.
func TestParse(t *testing.T) {
	for _, path := range testdataFiles(t) {
		name := filepath.Base(path)
		t.Run(name, func(t *testing.T) {
			if _, err := openFile(t, path); err != nil {
				t.Errorf("parse failed: %v", err)
			}
		})
	}
}

// finder pairs a human label with a Find* function so the report can loop.
type finder struct {
	label string
	find  func(ComfyWorkflow) (InputRef, error)
}

var finders = []finder{
	{"positive", FindPositivePrompt},
	{"negative", FindNegativePrompt},
	{"width", FindWidth},
	{"height", FindHeight},
	{"batch", FindBatchSize},
	{"fps", FindFps},
	//	{"seed", FindSeed},
	{"image", FindImage},
}

// TestFindersReport is a non-failing characterization baseline. It prints a
// per-workflow matrix plus a coverage summary, so improvements to the finder
// heuristics show up as rising hit counts and regressions show up as diffs in
// `go test -v` output.
func TestFindersReport(t *testing.T) {
	// Finders log incidental "Weird" lines to stderr (see IMPROVEMENTS.md #3);
	// silence them so the report is the only output.
	log.SetOutput(io.Discard)
	defer log.SetOutput(os.Stderr)

	files := testdataFiles(t)
	hits := make(map[string]int, len(finders))

	var b strings.Builder
	fmt.Fprintf(&b, "\ncompliance matrix (%d workflows)\n", len(files))

	for _, path := range files {
		name := filepath.Base(path)
		cw, err := openFile(t, path)
		if err != nil {
			fmt.Fprintf(&b, "\n%-48s  PARSE ERROR: %v\n", name, err)
			continue
		}
		var found []string
		for _, f := range finders {
			ref, err := f.find(cw)
			if err != nil {
				continue
			}
			hits[f.label]++
			val, rerr := cw.Resolve(ref)
			if rerr != nil {
				found = append(found, f.label+"=<resolve err>")
				continue
			}
			found = append(found, fmt.Sprintf("%s=%s", f.label, truncate(val)))
		}
		// seed
		refs, err := FindSeed(cw)
		if err != nil {
			continue
		}
		hits["seed"]++
		for _, ref := range refs {
			val, rerr := cw.Resolve(ref)
			if rerr != nil {
				found = append(found, "seed=<resolve err>")
				continue
			}
			found = append(found, fmt.Sprintf("seed=%s", truncate(val)))
		}

		fmt.Fprintf(&b, "\n%-48s\n  %s\n", name, strings.Join(found, "\n  "))
	}

	fmt.Fprintf(&b, "\ncoverage (found / %d workflows)\n", len(files))
	for _, f := range finders {
		fmt.Fprintf(&b, "  %-10s %2d\n", f.label, hits[f.label])
	}
	fmt.Fprintf(&b, "  %-10s %2d\n", "seed", hits["seed"])
	t.Log(b.String())
}

// enabledSeedNodes reads the raw map directly (independent of the finder
// heuristics) and returns nodeID -> seed value for every node whose seed is
// active: a scalar noise_seed/seed input, with add_noise absent or "enable".
// This is the ground-truth oracle for "which seeds a set must update".
func enabledSeedNodes(raw map[string]any) map[string]string {
	res := make(map[string]string)
	for id, nv := range raw {
		nodeMap, ok := nv.(map[string]any)
		if !ok {
			continue
		}
		inputs, ok := nodeMap["inputs"].(map[string]any)
		if !ok {
			continue
		}
		key := "noise_seed"
		if _, ok := inputs[key]; !ok {
			key = "seed"
			if _, ok := inputs[key]; !ok {
				continue
			}
		}
		if an, ok := inputs["add_noise"].(string); ok && an != "enable" {
			continue // disabled sampler; its seed is inert
		}
		if num, ok := inputs[key].(json.Number); ok {
			res[id] = num.String() // scalar only; skip node-ref inputs
		}
	}
	return res
}

// TestSetSeedUpdatesAllNodes guards the multi-seed fix: after `set seed X`,
// every enabled seed node must hold X. Today `set` updates only the single
// node the finder returns, so multi-sampler workflows (key-frames, LTX2) are
// left half-updated. Remove the Skip once FindSeed/set fan out to all matches
// (see IMPROVEMENTS.md).
func TestSetSeedUpdatesAllNodes(t *testing.T) {
	//t.Skip("known bug: set updates one node; multi-seed workflows stay half-updated (IMPROVEMENTS.md)")

	const newSeed = int64(1234567890123456)
	want := strconv.FormatInt(newSeed, 10)

	for _, path := range testdataFiles(t) {
		name := filepath.Base(path)
		t.Run(name, func(t *testing.T) {
			cw, err := openFile(t, path)
			if err != nil {
				t.Skipf("parse failed: %v", err)
			}
			before := enabledSeedNodes(cw.Raw)
			if len(before) < 2 {
				t.Skip("single/no enabled seed node; not a multi-seed case")
			}
			refs, err := FindSeed(cw)
			if err != nil {
				t.Fatalf("FindSeed: %v", err)
			}
			for _, ref := range refs {
				if err := cw.SetInt(ref, newSeed); err != nil {
					t.Fatalf("SetInt: %v", err)
				}
			}
			var stale []string
			for id, v := range enabledSeedNodes(cw.Raw) {
				if v != want {
					stale = append(stale, fmt.Sprintf("%s=%s", id, v))
				}
			}
			sort.Strings(stale)
			if len(stale) > 0 {
				t.Errorf("set seed left %d/%d enabled seed nodes stale: %v",
					len(stale), len(before), stale)
			}
		})
	}
}

// runSet drives the real cmdSet end-to-end by swapping os.Stdin/os.Stdout,
// so the "set seed random" branch, role resolution, and JSON round-trip are
// all exercised as the user would hit them. Not parallel-safe (mutates global
// stdio); relies on go test running functions sequentially.
func runSet(t *testing.T, inputPath string, args ...string) []byte {
	t.Helper()
	in, err := os.Open(inputPath)
	if err != nil {
		t.Fatalf("open input: %v", err)
	}
	defer in.Close()

	out, err := os.CreateTemp(t.TempDir(), "set-out-*.json")
	if err != nil {
		t.Fatalf("temp out: %v", err)
	}
	defer out.Close()

	oldIn, oldOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = in, out
	err = cmdSet(args)
	os.Stdin, os.Stdout = oldIn, oldOut
	if err != nil {
		t.Fatalf("cmdSet %v: %v", args, err)
	}

	b, err := os.ReadFile(out.Name())
	if err != nil {
		t.Fatalf("read out: %v", err)
	}
	return b
}

// TestSetSeedRandom validates `set seed random`: every enabled seed node must
// end up holding a fresh, canonical int64 (no float corruption, no scientific
// notation), different from its original value. Uses the raw-map oracle so it
// is independent of the finder heuristics, and covers single- and multi-seed
// workflows alike.
func TestSetSeedRandom(t *testing.T) {
	for _, path := range testdataFiles(t) {
		name := filepath.Base(path)
		t.Run(name, func(t *testing.T) {
			orig, err := openFile(t, path)
			if err != nil {
				t.Skipf("parse failed: %v", err)
			}
			before := enabledSeedNodes(orig.Raw)
			if len(before) == 0 {
				t.Skip("no enabled seed node")
			}

			out := runSet(t, path, "seed", "random")

			got, err := OpenComfyWorkflow(bytes.NewReader(out))
			if err != nil {
				t.Fatalf("output does not parse back: %v", err)
			}
			after := enabledSeedNodes(got.Raw)

			for id, old := range before {
				nv, ok := after[id]
				if !ok {
					t.Errorf("node %s lost its seed after set random", id)
					continue
				}
				n, err := strconv.ParseInt(nv, 10, 64)
				if err != nil {
					t.Errorf("node %s seed %q is not a valid int64: %v", id, nv, err)
					continue
				}
				if strconv.FormatInt(n, 10) != nv {
					t.Errorf("node %s seed %q is not a canonical integer (precision/format lost)", id, nv)
				}
				if nv == old {
					t.Errorf("node %s seed unchanged (%s); random did not write", id, old)
				}
			}
		})
	}
}

func truncate(v any) string {
	s := fmt.Sprintf("%v", v)
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 60 {
		return s[:57] + "..."
	}
	return s
}
