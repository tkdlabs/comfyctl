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

// copyWorkflowToTemp copies a workflow file into a fresh temp dir and returns
// the copy's path, so mark-style in-place mutations never touch testdata.
func copyWorkflowToTemp(t *testing.T, srcPath string) string {
	t.Helper()
	data, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("read src %s: %v", srcPath, err)
	}
	dst := filepath.Join(t.TempDir(), "marked.json")
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatalf("write temp %s: %v", dst, err)
	}
	return dst
}

// markFile drives the real cmdMark against a workflow file in place,
// returning its error (nil on success).
func markFile(t *testing.T, file string, args ...string) error {
	t.Helper()
	markArgs := append([]string{"-i", file}, args...)
	return cmdMark(markArgs)
}

// markedRole reopens a workflow file and returns the marker role carried by
// a node, or "" if it has none.
func markedRole(t *testing.T, file, nodeID string) string {
	t.Helper()
	cw, err := openFile(t, file)
	if err != nil {
		t.Fatalf("reopen %s: %v", file, err)
	}
	return cw.Nodes[nodeID].MarkerRole
}

// TestFindSeedOrder guards the stable-output fix: FindSeed must return refs
// sorted by node id, so `dump seed` output doesn't flicker run-to-run.
func TestFindSeedOrder(t *testing.T) {
	for _, path := range testdataFiles(t) {
		name := filepath.Base(path)
		t.Run(name, func(t *testing.T) {
			cw, err := openFile(t, path)
			if err != nil {
				t.Skipf("parse failed: %v", err)
			}
			refs, err := FindSeed(cw)
			if err != nil || len(refs) < 2 {
				return // not a multi-seed workflow
			}
			for i := 1; i < len(refs); i++ {
				if refs[i-1].nodeId > refs[i].nodeId {
					t.Errorf("seed refs out of order: %v -> %v", refs[i-1].nodeId, refs[i].nodeId)
				}
			}
		})
	}
}

// TestMarkOverwriteProtection guards the mark clobber guard: a node holds one
// marker and a role lives in one place, so re-marking must refuse unless -f
// is given, and -f must move the role cleanly.
func TestMarkOverwriteProtection(t *testing.T) {
	path := testdataFiles(t)[0]
	cw, err := openFile(t, path)
	if err != nil {
		t.Skipf("parse failed: %v", err)
	}
	all := FindAllNonRefInputs(cw)
	var a, b InputRef
	for _, r := range all {
		switch {
		case a.nodeId == "":
			a = r
		case r.nodeId != a.nodeId:
			b = r
		}
		if b.nodeId != "" {
			break
		}
	}
	if b.nodeId == "" {
		t.Fatal("need markable inputs on at least two different nodes")
	}
	aRef, bRef := a.nodeId+":"+a.inputId, b.nodeId+":"+b.inputId

	dst := copyWorkflowToTemp(t, path)

	// First mark succeeds and lands on node a.
	if err := markFile(t, dst, "myrole", aRef); err != nil {
		t.Fatalf("first mark: %v", err)
	}
	if got := markedRole(t, dst, a.nodeId); got != "myrole" {
		t.Fatalf("node %s marked as %q, want myrole", a.nodeId, got)
	}

	// Re-marking the same role on another node refuses without -f ...
	if err := markFile(t, dst, "myrole", bRef); err == nil {
		t.Fatal("expected error re-marking 'myrole' on another node without -f")
	}
	if got := markedRole(t, dst, b.nodeId); got != "" {
		t.Errorf("node %s got marker %q despite refusal", b.nodeId, got)
	}
	if got := markedRole(t, dst, a.nodeId); got != "myrole" {
		t.Errorf("node %s lost marker %q on refused move", a.nodeId, got)
	}

	// ... and with -f moves it: old node cleared, new node marked.
	if err := markFile(t, dst, "-f", "myrole", bRef); err != nil {
		t.Fatalf("move with -f: %v", err)
	}
	if got := markedRole(t, dst, a.nodeId); got != "" {
		t.Errorf("node %s still marked %q after move", a.nodeId, got)
	}
	if got := markedRole(t, dst, b.nodeId); got != "myrole" {
		t.Errorf("node %s marked as %q after move, want myrole", b.nodeId, got)
	}

	// Clobbering a node that already carries a different marker refuses ...
	if err := markFile(t, dst, "other", bRef); err == nil {
		t.Fatal("expected error replacing 'myrole' on node b with 'other' without -f")
	}
	if got := markedRole(t, dst, b.nodeId); got != "myrole" {
		t.Errorf("node %s marker %q was clobbered without -f", b.nodeId, got)
	}

	// ... and with -f replaces it.
	if err := markFile(t, dst, "-f", "other", bRef); err != nil {
		t.Fatalf("replace with -f: %v", err)
	}
	if got := markedRole(t, dst, b.nodeId); got != "other" {
		t.Errorf("node %s marked as %q after replace, want other", b.nodeId, got)
	}
}

// TestMarkDelete guards `mark -d <role>`: after marking, deleting the role
// removes its marker from every node carrying it, returning them to heuristic
// resolution, and the workflow still parses back cleanly.
func TestMarkDelete(t *testing.T) {
	path := testdataFiles(t)[0]
	cw, err := openFile(t, path)
	if err != nil {
		t.Skipf("parse failed: %v", err)
	}
	all := FindAllNonRefInputs(cw)
	if len(all) == 0 {
		t.Fatal("no markable inputs")
	}
	a := all[0]
	aRef := a.nodeId + ":" + a.inputId

	dst := copyWorkflowToTemp(t, path)

	if err := markFile(t, dst, "myrole", aRef); err != nil {
		t.Fatalf("mark: %v", err)
	}
	if got := markedRole(t, dst, a.nodeId); got != "myrole" {
		t.Fatalf("node %s marked as %q, want myrole", a.nodeId, got)
	}

	// Delete the role; its marker should be gone and the file should reparse.
	if err := markFile(t, dst, "-d", "myrole"); err != nil {
		t.Fatalf("mark -d: %v", err)
	}
	if got := markedRole(t, dst, a.nodeId); got != "" {
		t.Errorf("node %s still marked %q after mark -d", a.nodeId, got)
	}

	// Deleting a role that isn't marked is a no-op success, not an error.
	if err := markFile(t, dst, "-d", "nope"); err != nil {
		t.Errorf("mark -d on unmarked role should not error: %v", err)
	}
}

// TestMarkDeleteFlagValidation guards the `-d` argument contract: it takes
// exactly one role and no ref, and must error otherwise.
func TestMarkDeleteFlagValidation(t *testing.T) {
	path := testdataFiles(t)[0]
	dst := copyWorkflowToTemp(t, path)

	if err := markFile(t, dst, "-d"); err == nil {
		t.Error("expected error: mark -d with no role")
	}
	if err := markFile(t, dst, "-d", "a", "b"); err == nil {
		t.Error("expected error: mark -d with too many args")
	}
	// real role + ref combination must be rejected because -d takes no ref.
	if err := markFile(t, dst, "-d", "role", "1:x"); err == nil {
		t.Error("expected error: mark -d with a ref")
	}
}

// TestDetectMarkerConflicts covers the reconcile half of #5: a role marked on
// two nodes is detected, and `mark -d` resolves it.
func TestDetectMarkerConflicts(t *testing.T) {
	path := testdataFiles(t)[0]
	cw, err := openFile(t, path)
	if err != nil {
		t.Skipf("parse failed: %v", err)
	}
	all := FindAllNonRefInputs(cw)
	var a, b InputRef
	for _, r := range all {
		switch {
		case a.nodeId == "":
			a = r
		case r.nodeId != a.nodeId:
			b = r
		}
		if b.nodeId != "" {
			break
		}
	}
	if b.nodeId == "" {
		t.Fatal("need markable inputs on two different nodes")
	}
	aRef, bRef := a.nodeId+":"+a.inputId, b.nodeId+":"+b.inputId

	dst := copyWorkflowToTemp(t, path)
	markFile(t, dst, "dup", aRef)
	markFile(t, dst, "-f", "dup", bRef) // force-move so b carries it

	// Simulate a file with the role marked on both nodes (hand-edited / merge):
	// reopen, add the marker back to node a, write out.
	cw2, err := openFile(t, dst)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if err := cw2.MarkRole(a, "dup"); err != nil {
		t.Fatalf("force duplicate mark: %v", err)
	}
	out, err := os.Create(dst)
	if err != nil {
		t.Fatalf("create %s: %v", dst, err)
	}
	if err := cw2.WriteOut(out); err != nil {
		out.Close()
		t.Fatalf("write: %v", err)
	}
	out.Close()

	cw3, err := openFile(t, dst)
	if err != nil {
		t.Fatalf("reopen for detect: %v", err)
	}
	conflicts, err := cw3.DetectMarkerConflicts()
	if err != nil {
		t.Fatalf("DetectMarkerConflicts: %v", err)
	}
	var found bool
	for _, c := range conflicts {
		if c.Role == "dup" && len(c.Nodes) >= 2 {
			found = true
		}
	}
	if !found {
		t.Errorf("expected duplicate role 'dup' conflict, got %+v", conflicts)
	}

	// mark -d on the duplicated role clears both markers.
	if err := markFile(t, dst, "-d", "dup"); err != nil {
		t.Fatalf("mark -d dup: %v", err)
	}
	if got := markedRole(t, dst, a.nodeId); got != "" {
		t.Errorf("node %s still marked %q after -d", a.nodeId, got)
	}
	if got := markedRole(t, dst, b.nodeId); got != "" {
		t.Errorf("node %s still marked %q after -d", b.nodeId, got)
	}
}

// TestRolesCommand guards issue #1's `roles` command: it lists every marked
// role with its node count and surfaces a duplicated role as a note.
func TestRolesCommand(t *testing.T) {
	path := testdataFiles(t)[0]
	cw, err := openFile(t, path)
	if err != nil {
		t.Skipf("parse failed: %v", err)
	}
	all := FindAllNonRefInputs(cw)
	var a, b InputRef
	for _, r := range all {
		switch {
		case a.nodeId == "":
			a = r
		case r.nodeId != a.nodeId:
			b = r
		}
		if b.nodeId != "" {
			break
		}
	}
	if b.nodeId == "" {
		t.Fatal("need markable inputs on two different nodes")
	}

	dst := copyWorkflowToTemp(t, path)
	markFile(t, dst, "character_image", a.nodeId+":"+a.inputId)
	markFile(t, dst, "bg_audio", b.nodeId+":"+b.inputId)

	out := runRoles(t, dst)

	for _, want := range []string{"character_image", "bg_audio", "(1 node)"} {
		if !strings.Contains(out, want) {
			t.Errorf("roles output missing %q:\n%s", want, out)
		}
	}

	// A duplicated role surfaces as a "marked on 2 nodes" note: force the
	// role onto the second node too (overwriting its bg_audio marker).
	cw2, err := openFile(t, dst)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if err := cw2.MarkRole(b, "character_image"); err != nil {
		t.Fatalf("force duplicate: %v", err)
	}
	df, err := os.Create(dst)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := cw2.WriteOut(df); err != nil {
		df.Close()
		t.Fatalf("write: %v", err)
	}
	df.Close()

	out2 := runRoles(t, dst)
	if !strings.Contains(out2, "marked on 2 nodes") {
		t.Errorf("roles output should flag the duplicate role:\n%s", out2)
	}
}

// runRoles drives cmdRoles by swapping stdin to read from a file and
// capturing stdout.
func runRoles(t *testing.T, inputPath string) string {
	t.Helper()
	in, err := os.Open(inputPath)
	if err != nil {
		t.Fatalf("open input: %v", err)
	}
	defer in.Close()

	out, err := os.CreateTemp(t.TempDir(), "roles-out-*.txt")
	if err != nil {
		t.Fatalf("temp out: %v", err)
	}
	defer out.Close()

	oldIn, oldOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = in, out
	err = cmdRoles(nil)
	os.Stdin, os.Stdout = oldIn, oldOut
	if err != nil {
		t.Fatalf("cmdRoles: %v", err)
	}

	b, err := os.ReadFile(out.Name())
	if err != nil {
		t.Fatalf("read out: %v", err)
	}
	return string(b)
}

func truncate(v any) string {
	s := fmt.Sprintf("%v", v)
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 60 {
		return s[:57] + "..."
	}
	return s
}
