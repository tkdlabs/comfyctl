package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestInvertedCrawl guards issue #4's rewrite: the crawl follows any upstream
// node-ref (no hand-maintained follow list), but must hold the documented
// "crawl through routing, mark through merges" boundary and never let one
// polarity bleed into the other. Each case names the file + raw behaviour that
// pins a specific rule.

const krea2UserPrompt = "A high-resolution, surreal digital illustration"
const krea2SystemPromptLead = "You are an expert prompt engineer"

// TestInvertedCrawlResolvesKrea2Positive pins the flagship ambiguity case from
// the issue: krea2's positive lives behind a ComfySwitchNode chain that fans
// into a StringConcatenate merge of system + user prompt. The crawl must
// resolve the user prompt (via the switch arms), never the system prompt, and
// never dead-end the way the whitelist crawl did.
func TestInvertedCrawlResolvesKrea2Positive(t *testing.T) {
	cw, err := openFile(t, "testdata/image_krea2_turbo_t2i.json")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ref, err := FindPositivePrompt(cw)
	if err != nil {
		t.Fatalf("FindPositivePrompt: %v", err)
	}
	val, err := cw.Resolve(ref)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	v, ok := val.(string)
	if !ok {
		t.Fatalf("positive resolved to %T, want string", val)
	}
	if !strings.HasPrefix(v, krea2UserPrompt) {
		t.Errorf("positive = %.60q..., want the user prompt (%.60q...)", v, krea2UserPrompt)
	}
	if strings.Contains(v, krea2SystemPromptLead) {
		t.Errorf("positive grabbed the SYSTEM prompt (%.60q...); user prompt required", v)
	}
}

// TestInvertedCrawlNegativeBoundary guards the polarity leak: a negative crawl
// must never fall back to a positive-prompt source. krea2 gates its negative
// through a banned ConditioningZeroOut, flux_fill_inpaint the same; without the
// hint-set separation these resolve to the *positive* text silently.
func TestInvertedCrawlNegativeBoundary(t *testing.T) {
	for _, path := range []string{
		"testdata/image_krea2_turbo_t2i.json",
		"testdata/flux_fill_inpaint_example.json",
		"testdata/image_flux2_klein_image_edit_4b_distilled.json",
	} {
		t.Run(path, func(t *testing.T) {
			cw, err := openFile(t, path)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			if _, err := FindNegativePrompt(cw); err == nil {
				t.Errorf("negative unexpectedly resolved; must stay marker-resolved (banned zero-out/merge)")
			}
		})
	}
}

// TestInvertedCrawlBespokeFieldStaysMarked pins issue #4's other boundary: the
// dotted ns.api_wan2_7 input has no flat anchor, and must stay un-findable so
// `mark` is the only path (never substring-matched into text).
func TestInvertedCrawlBespokeFieldStaysMarked(t *testing.T) {
	cw, err := openFile(t, "testdata/api_wan2_7_i2v.json")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := FindPositivePrompt(cw); err == nil {
		t.Errorf("positive unexpectedly resolved on dotted-key node; expected a miss (mark path)")
	}
}

// TestInvertedCrawlRoutesThroughMathAndSwitches pins the "survives new node
// types" win: video_ltx2_3 reaches fps/width through a ComfyMathExpression
// whose input is named values.a (not in any hand list), and its positive
// through a ComfySwitchNode. All must resolve now.
func TestInvertedCrawlRoutesThroughMathAndSwitches(t *testing.T) {
	cw, err := openFile(t, "testdata/video_ltx2_3_t2v.json")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Run("fps via math expression", func(t *testing.T) {
		ref, err := FindFps(cw)
		if err != nil {
			t.Fatalf("FindFps: %v", err)
		}
		val, err := cw.Resolve(ref)
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if v, ok := val.(json.Number); !ok || v.String() != "25" {
			t.Errorf("fps = %v, want 25", val)
		}
	})
	t.Run("positive via switch", func(t *testing.T) {
		ref, err := FindPositivePrompt(cw)
		if err != nil {
			t.Fatalf("FindPositivePrompt: %v", err)
		}
		val, _ := cw.Resolve(ref)
		if v, ok := val.(string); !ok || !strings.HasPrefix(v, "Dynamic cinematic close-up") {
			t.Errorf("positive = %.60q..., want the switch-routed prompt", v)
		}
	})
	t.Run("negative stays on its own branch", func(t *testing.T) {
		ref, err := FindNegativePrompt(cw)
		if err != nil {
			t.Fatalf("FindNegativePrompt: %v", err)
		}
		val, _ := cw.Resolve(ref)
		if v, ok := val.(string); !ok || !strings.HasPrefix(v, "pc game") {
			t.Errorf("negative = %.60q..., want 'pc game...'", v)
		}
	})
}

// TestInvertedCrawlUnknownNodeTypeSurvives proves the inversion directly: a
// fabricated workflow with invented routing classes and gibberish slot names
// resolves with no edits to the hand-maintained follow list. The old crawl
// dead-ends on any of these.
func TestInvertedCrawlUnknownNodeTypeSurvives(t *testing.T) {
	raw := `{
		"1": {"class_type": "KSampler", "inputs": {"seed": 123, "positive": ["2", 0]}},
		"2": {"class_type": "HoloRoutingNode", "inputs": {"wibble": ["3", 0]}},
		"3": {"class_type": "HoloPrimitiveText", "inputs": {"value": "hello from beyond"}},
		"4": {"class_type": "HoloScaler", "inputs": {"width": ["5", 0]}},
		"5": {"class_type": "HoloMathRouter", "inputs": {"pivot.scale": ["6", 0]}},
		"6": {"class_type": "PrimitiveInt", "inputs": {"value": 42}}
	}`
	cw, err := OpenComfyWorkflow(bytes.NewReader([]byte(raw)))
	if err != nil {
		t.Fatalf("parse synthetic workflow: %v", err)
	}
	t.Run("text through invented router", func(t *testing.T) {
		ref, err := FindPositivePrompt(cw)
		if err != nil {
			t.Fatalf("FindPositivePrompt: %v", err)
		}
		val, _ := cw.Resolve(ref)
		if v, ok := val.(string); !ok || v != "hello from beyond" {
			t.Errorf("positive = %v, want 'hello from beyond'", val)
		}
	})
	t.Run("number through invented router", func(t *testing.T) {
		ref, err := FindWidth(cw)
		if err != nil {
			t.Fatalf("FindWidth: %v", err)
		}
		val, _ := cw.Resolve(ref)
		if v, ok := val.(json.Number); !ok || v.String() != "42" {
			t.Errorf("width = %v, want 42", val)
		}
	})
}

// TestInvertedCrawlPhantomPrimitiveRejected pins the generic-hop bound: a
// primitive buried in the model graph ("Steps", respun_name) must not be
// mistaken for the role's value. krea2's ResolutionSelector (megapixels)
// and image_flux2's GetImageSize (image-derived) have no directly writable
// scalar, so width/height must stay unresolved rather than pointing at one.
func TestInvertedCrawlPhantomPrimitiveRejected(t *testing.T) {
	for _, path := range []string{
		"testdata/image_krea2_turbo_t2i.json", // ResolutionSelector
		"testdata/image_flux2.json",           // GetImageSize-derived
	} {
		t.Run(path, func(t *testing.T) {
			cw, err := openFile(t, path)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			if _, err := FindWidth(cw); err == nil {
				t.Errorf("width unexpectedly resolved; phantom primitive must be rejected")
			}
		})
	}
}
