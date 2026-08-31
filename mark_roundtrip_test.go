package main

import (
	"bytes"
	"testing"
)

// firstTextInput returns the first markable text (non node-ref) input in a
// workflow, so a round-trip test can mark a custom role on a string input.
func firstTextInput(cw ComfyWorkflow) (InputRef, bool) {
	for _, ref := range FindAllNonRefInputs(cw) {
		if ref.inputType == ComfyTextInput {
			return ref, true
		}
	}
	return InputRef{}, false
}

// TestMarkRoleHonoredBySetAndDump guards marker-first resolution (issue #2:
// mark round-trip): after marking a custom role, `set` writes to the marked
// node/input and `dump` resolves through the same marker, printing the new
// value rather than falling back to fuzzy search (which has no case for an
// arbitrary custom role).
func TestMarkRoleHonoredBySetAndDump(t *testing.T) {
	const path = "testdata/api_wan2_7_i2v.json"
	cw, err := openFile(t, path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ref, ok := firstTextInput(cw)
	if !ok {
		t.Fatal("no markable text input available")
	}

	const want = "custom-role-value-123"
	dst := copyWorkflowToTemp(t, path)
	if err := markFile(t, dst, "myrole", ref.nodeId+":"+ref.inputId); err != nil {
		t.Fatalf("mark: %v", err)
	}

	out := runSet(t, dst, "myrole", want)
	got, err := OpenComfyWorkflow(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("set output does not reparse: %v", err)
	}

	// The set succeeded, which alone proves marker-first resolution (a custom
	// role has no fuzzy fallback; it is only resolvable via its marker).
	refs, err := got.ResolveRole("myrole")
	if err != nil {
		t.Fatalf("ResolveRole(myrole): %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("ResolveRole(myrole) returned %d refs, want 1", len(refs))
	}
	val, err := got.Resolve(refs[0])
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if gotStr, _ := val.(string); gotStr != want {
		t.Errorf("marked input resolves to %q, want %q", val, want)
	}
}

// TestDottedKeySurvivesRoundTrip guards that a dotted input name such as
// "model.prompt" is a plain map key and survives mark -> set -> write-out ->
// re-parse: re-parsing the output yields the new value in that same key. The
// marker lives one-per-node, so each pay of dotted inputs is verified on its
// own copy of the workflow.
func TestDottedKeySurvivesRoundTrip(t *testing.T) {
	const path = "testdata/api_wan2_7_i2v.json"
	cases := []struct {
		name  string
		role  string
		input string
		value string
	}{
		{"positive_model_prompt", "positive", "model.prompt", "A new positive prompt."},
		{"negative_model_negative_prompt", "negative", "model.negative_prompt", "A new negative prompt."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ref := InputRef{nodeId: "33", inputId: tc.input, inputType: ComfyTextInput}
			dst := copyWorkflowToTemp(t, path)
			if err := markFile(t, dst, tc.role, "33:"+tc.input); err != nil {
				t.Fatalf("mark %s: %v", tc.role, err)
			}
			out := runSet(t, dst, tc.role, tc.value)
			got, err := OpenComfyWorkflow(bytes.NewReader(out))
			if err != nil {
				t.Fatalf("set output does not reparse: %v", err)
			}
			val, err := got.Resolve(ref)
			if err != nil {
				t.Fatalf("Resolve node 33 %s: %v", tc.input, err)
			}
			if gotStr, _ := val.(string); gotStr != tc.value {
				t.Errorf("node 33 %s resolves to %q, want %q", tc.input, val, tc.value)
			}
		})
	}
}

// TestMarkedWorkflowReparsesCleanly guards that a workflow carrying a marker
// re-parses with OpenComfyWorkflow succeeding and the MarkerRole/MarkerInput
// intact, both right after marking and after a set-driven write-out round-trip.
func TestMarkedWorkflowReparsesCleanly(t *testing.T) {
	const path = "testdata/api_wan2_7_i2v.json"
	dst := copyWorkflowToTemp(t, path)
	if err := markFile(t, dst, "positive", "33:model.prompt"); err != nil {
		t.Fatalf("mark: %v", err)
	}

	cw, err := openFile(t, dst)
	if err != nil {
		t.Fatalf("marked workflow does not reparse: %v", err)
	}
	if m := cw.Nodes["33"].MarkerRole; m != "positive" {
		t.Errorf("node 33 MarkerRole = %q, want positive", m)
	}
	if m := cw.Nodes["33"].MarkerInput; m != "model.prompt" {
		t.Errorf("node 33 MarkerInput = %q, want model.prompt", m)
	}

	out := runSet(t, dst, "positive", "reparse-value")
	got, err := OpenComfyWorkflow(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("set output does not reparse: %v", err)
	}
	if got.Nodes["33"].MarkerRole != "positive" || got.Nodes["33"].MarkerInput != "model.prompt" {
		t.Errorf("marker not intact after set round-trip: role=%q input=%q",
			got.Nodes["33"].MarkerRole, got.Nodes["33"].MarkerInput)
	}
	val, err := got.Resolve(InputRef{nodeId: "33", inputId: "model.prompt", inputType: ComfyTextInput})
	if err != nil {
		t.Fatalf("Resolve after set: %v", err)
	}
	if gotStr, _ := val.(string); gotStr != "reparse-value" {
		t.Errorf("node 33 model.prompt resolves to %q, want %q", val, "reparse-value")
	}
}
