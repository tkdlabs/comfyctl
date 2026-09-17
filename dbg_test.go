package main

import (
	"fmt"
	"os"
	"testing"
)

func TestDbgSet(t *testing.T) {
	const path = "testdata/video_wan2_2_14B_t2v.json"
	dst := copyWorkflowToTemp(t, path)
	if err := markFile(t, dst, "myint", "128:114:value"); err != nil {
		t.Fatalf("mark: %v", err)
	}
	in, _ := os.Open(dst)
	defer in.Close()
	out, _ := os.CreateTemp(t.TempDir(), "dbg-*.json")
	defer out.Close()
	oldIn, oldOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = in, out
	err := cmdSet([]string{"myint", "999"})
	os.Stdin, os.Stdout = oldIn, oldOut
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(out.Name())
	_ = fmt.Sprintf("")
	os.WriteFile("/tmp/opencode/dbg.json", data, 0o644)
}
