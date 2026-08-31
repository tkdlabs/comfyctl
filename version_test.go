package main

import (
	"strings"
	"testing"
)

// TestVersionString guards the version/build metadata surfaced by `version`
// and `--version`. The vars are populated by goreleaser -ldflags at release
// time; without ldflags they fall back to the dev defaults.
func TestVersionString(t *testing.T) {
	wantVN, wantCommit, wantDate := version, commit, date
	got := versionString()
	if !strings.Contains(got, "comfyctl "+version) {
		t.Errorf("versionString = %q, want it to contain %q", got, "comfyctl "+version)
	}
	if !strings.Contains(got, wantVN) || !strings.Contains(got, wantCommit) || !strings.Contains(got, wantDate) {
		t.Errorf("versionString = %q, want it to echo version=%q commit=%q date=%q",
			got, wantVN, wantCommit, wantDate)
	}
}

// TestVersionDefaultsForDevBuild asserts that a locally built binary (no
// ldflags) reports the dev placeholders rather than garbage.
func TestVersionDefaultsForDevBuild(t *testing.T) {
	if version != "dev" || commit != "none" || date != "unknown" {
		t.Skip("ldflags-injected build; dev defaults not in effect")
	}
	got := versionString()
	for _, want := range []string{"comfyctl dev", "commit: none", "built: unknown"} {
		if !strings.Contains(got, want) {
			t.Errorf("versionString = %q, want it to contain %q", got, want)
		}
	}
}
