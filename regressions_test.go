package main

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNonBugEmptyNegativeKleinBase locks in that the empty negative on
// flux-klein base models is genuine empty conditioning, not a finder miss: the
// role must resolve (to node 75:67's "" text input) instead of erroring.
func TestNonBugEmptyNegativeKleinBase(t *testing.T) {
	log.SetOutput(io.Discard)
	defer log.SetOutput(os.Stderr)

	for _, path := range []string{
		"testdata/image_flux2_klein_image_edit_4b_base.json",
		"testdata/image_flux2_klein_image_edit_9b_base.json",
	} {
		name := filepath.Base(path)
		cw, err := openFile(t, path)
		if err != nil {
			t.Fatalf("open %s: %v", path, err)
		}
		refs, err := cw.ResolveRole("negative")
		if err != nil {
			t.Errorf("negative must resolve on %s, got: %v", name, err)
			continue
		}
		if len(refs) != 1 {
			t.Errorf("expected one negative ref on %s, got %d", name, len(refs))
			continue
		}
		if refs[0].nodeId != "75:67" {
			t.Errorf("negative resolved to node %s, want 75:67", refs[0].nodeId)
		}
		val, err := cw.Resolve(refs[0])
		if err != nil {
			t.Errorf("resolve negative on %s: %v", name, err)
			continue
		}
		if s, ok := val.(string); !ok || s != "" {
			t.Errorf("negative value on %s = %q (%T), want empty string", name, val, val)
		}
	}
}

// TestColonNamespacedNodeIDsParse guards that colon-namespaced node ids
// ("98:22") round-trip: every testdata workflow carrying one parse cleanly and
// keeps the full literal id as a cw.Nodes key.
func TestColonNamespacedNodeIDsParse(t *testing.T) {
	checked := 0
	for _, path := range testdataFiles(t) {
		cw, err := openFile(t, path)
		if err != nil {
			t.Errorf("open %s: %v", path, err)
			continue
		}
		for key := range cw.Raw {
			if !strings.Contains(key, ":") {
				continue
			}
			checked++
			if _, found := cw.Nodes[key]; !found {
				t.Errorf("colon key %q in %s missing from cw.Nodes", key, filepath.Base(path))
			}
		}
	}
	if checked == 0 {
		t.Fatal("no testdata workflow contains a colon-namespaced node id")
	}
}

// TestNonBugLoadImageDumpResolves is a light check that wrong-typed `dump
// image` still resolves: a LoadImage node's string path is reported as-is.
func TestNonBugLoadImageDumpResolves(t *testing.T) {
	log.SetOutput(io.Discard)
	defer log.SetOutput(os.Stderr)

	path := "testdata/api_bytedance_seedance1_5_image_to_video.json"
	cw, err := openFile(t, path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	ref, err := FindImage(cw)
	if err != nil {
		t.Fatalf("FindImage on %s must not error: %v", path, err)
	}
	val, err := cw.Resolve(ref)
	if err != nil {
		t.Fatalf("resolve image on %s: %v", path, err)
	}
	if s, ok := val.(string); !ok || s == "" {
		t.Errorf("image path = %q (%T), want a non-empty string", val, val)
	}
}
