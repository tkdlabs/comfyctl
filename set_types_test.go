package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// rawNode returns node input values parsed without UseNumber, so type drift
// (a JSON number written as a string, etc.) is detectable by comparing against
// float64/bool/string.
func rawInputs(t *testing.T, path, nodeID string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var wf map[string]any
	if json.Unmarshal(data, &wf) != nil {
		t.Fatalf("%s does not parse", path)
	}
	node, ok := wf[nodeID].(map[string]any)
	if !ok {
		t.Fatalf("node %s not found in %s", nodeID, path)
	}
	inputs, _ := node["inputs"].(map[string]any)
	return inputs
}

func runSetCapture(t *testing.T, inputPath string, args ...string) string {
	t.Helper()
	in, err := os.Open(inputPath)
	if err != nil {
		t.Fatalf("open input: %v", err)
	}
	defer in.Close()

	dst := filepath.Join(t.TempDir(), "set-out.json")
	out, err := os.Create(dst)
	if err != nil {
		t.Fatalf("create out: %v", err)
	}

	oldIn, oldOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = in, out
	err = cmdSet(args)
	os.Stdin, os.Stdout = oldIn, oldOut
	out.Close()
	if err != nil {
		t.Fatalf("cmdSet %v: %v", args, err)
	}
	return dst
}

func TestSetCustomIntRole(t *testing.T) {
	const path = "testdata/video_wan2_2_14B_t2v.json"
	dst := copyWorkflowToTemp(t, path)
	if err := markFile(t, dst, "myint", "128:114:value"); err != nil {
		t.Fatalf("mark: %v", err)
	}
	out := runSetCapture(t, dst, "myint", "999")
	node := rawInputs(t, out, "128:114")
	if got := node["value"]; got != float64(999) {
		t.Errorf("value = %v (%T), want number 999", got, got)
	}
}

func TestSetCustomBoolRole(t *testing.T) {
	const path = "testdata/video_wan2_2_14B_t2v.json"
	dst := copyWorkflowToTemp(t, path)
	if err := markFile(t, dst, "myflag", "128:129:value"); err != nil {
		t.Fatalf("mark: %v", err)
	}
	out := runSetCapture(t, dst, "myflag", "true")
	node := rawInputs(t, out, "128:129")
	if got := node["value"]; got != true {
		t.Errorf("value = %v (%T), want boolean true", got, got)
	}
}

func TestSetSeedRandomOnCustomIntRole(t *testing.T) {
	const path = "testdata/video_wan2_2_14B_t2v.json"
	dst := copyWorkflowToTemp(t, path)
	if err := markFile(t, dst, "myint", "128:114:value"); err != nil {
		t.Fatalf("mark: %v", err)
	}
	out := runSetCapture(t, dst, "myint", "random")
	node := rawInputs(t, out, "128:114")
	v, ok := node["value"].(float64)
	if !ok || v == 20 {
		t.Errorf("value = %v (%T), want a random number (was 20)", node["value"], node["value"])
	}
}

// TestSetRefGuarded: `set` must refuse to overwrite a node-ref input, the one
// case where a scalar assignment silently rewrites ["node", idx] into a string
// and breaks the graph.
func TestSetRefGuarded(t *testing.T) {
	const path = "testdata/video_wan2_2_14B_t2v.json"
	dst := copyWorkflowToTemp(t, path)
	if err := markFile(t, dst, "myref", "80:video"); err != nil {
		t.Fatalf("mark: %v", err)
	}
	before, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	in, err := os.Open(dst)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer in.Close()
	oldIn := os.Stdin
	os.Stdin = in
	setErr := cmdSet([]string{"myref", "scalar-value"})
	os.Stdin = oldIn
	if setErr == nil {
		t.Fatal("expected set on node-ref input to error")
	}
	if !strings.Contains(setErr.Error(), "node reference") {
		t.Errorf("error = %v, want a node-reference refusal", setErr)
	}
	// set never writes the file; a failed run must leave it untouched.
	after, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if string(before) != string(after) {
		t.Fatal("failed set modified the workflow file")
	}
	_ = fmt.Sprint()
}
