package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
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
	{"seed", FindSeed},
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
		fmt.Fprintf(&b, "\n%-48s\n  %s\n", name, strings.Join(found, "\n  "))
	}

	fmt.Fprintf(&b, "\ncoverage (found / %d workflows)\n", len(files))
	for _, f := range finders {
		fmt.Fprintf(&b, "  %-10s %2d\n", f.label, hits[f.label])
	}
	t.Log(b.String())
}

func truncate(v any) string {
	s := fmt.Sprintf("%v", v)
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 60 {
		return s[:57] + "..."
	}
	return s
}
