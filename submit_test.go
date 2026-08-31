package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
)

// submitWorkflowJSON is a minimal API-format workflow piped into cmdSubmit on
// stdin. The mock server never inspects the prompt body, so only the format
// requirements matter.
const submitWorkflowJSON = `{"1":{"class_type":"TestNode","inputs":{}}}`

// nodeCats maps a node id to its /history output categories (images, videos,
// audio, ...).
type nodeCats map[string]map[string][]outputFile

// mkHistory builds a /history/<id> body for the given prompt.
func mkHistory(t *testing.T, promptID, status string, completed bool, cats nodeCats) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		promptID: map[string]any{
			"outputs": cats,
			"status":  map[string]any{"status_str": status, "completed": completed},
		},
	})
	if err != nil {
		t.Fatalf("mkHistory: %v", err)
	}
	return string(b)
}

// comfyMock is a scripted stand-in for a ComfyUI server: it accepts the
// prompt submission, serves /history/<id> responses in order (the last one
// repeats), and serves /view bytes.
type comfyMock struct {
	srv *httptest.Server

	mu           sync.Mutex
	promptCalls  int
	historyCalls int
	viewCalls    int
	viewed       []string

	promptID  string
	promptErr bool
	histories []string
	viewBody  []byte
}

// mockCfg configures newComfyMock.
type mockCfg struct {
	promptID  string
	promptErr bool // /prompt responds with HTTP 400
	histories []string
	viewBody  []byte
}

func newComfyMock(t *testing.T, cfg mockCfg) *comfyMock {
	t.Helper()
	m := &comfyMock{
		promptID:  cfg.promptID,
		promptErr: cfg.promptErr,
		histories: cfg.histories,
		viewBody:  cfg.viewBody,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/prompt", m.handlePrompt)
	mux.HandleFunc("/history/", m.handleHistory)
	mux.HandleFunc("/view", m.handleView)
	m.srv = httptest.NewServer(mux)
	t.Cleanup(m.srv.Close)
	return m
}

func (m *comfyMock) handlePrompt(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	m.promptCalls++
	m.mu.Unlock()
	if m.promptErr {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":"rejected"}`)
		return
	}
	io.WriteString(w, `{"prompt_id":"`+m.promptID+`"}`)
}

func (m *comfyMock) handleHistory(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	idx := m.historyCalls
	m.historyCalls++
	m.mu.Unlock()
	body := "{}" // no history configured: prompt stays in-progress forever
	if idx < len(m.histories) {
		body = m.histories[idx]
	} else if len(m.histories) > 0 {
		body = m.histories[len(m.histories)-1]
	}
	io.WriteString(w, body)
}

func (m *comfyMock) handleView(w http.ResponseWriter, r *http.Request) {
	filename := r.URL.Query().Get("filename")
	m.mu.Lock()
	m.viewCalls++
	m.viewed = append(m.viewed, filename)
	m.mu.Unlock()
	payload := m.viewBody
	if payload == nil {
		payload = []byte("bytes:" + filename + "\n")
	}
	w.Write(payload)
}

// callCounts returns how many times each endpoint was hit.
func (m *comfyMock) callCounts() (prompt, history, view int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.promptCalls, m.historyCalls, m.viewCalls
}

// viewedFiles returns the filenames requested from /view, sorted.
func (m *comfyMock) viewedFiles() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	v := append([]string(nil), m.viewed...)
	sort.Strings(v)
	return v
}

// runSubmit drives the real cmdSubmit end-to-end by piping a workflow in on
// stdin and capturing stdout (stderr notes are ignored). Not parallel-safe
// (mutates global stdio); relies on go test running functions sequentially.
func runSubmit(t *testing.T, args ...string) ([]byte, error) {
	t.Helper()
	dir := t.TempDir()
	in, err := os.CreateTemp(dir, "submit-in-*.json")
	if err != nil {
		t.Fatalf("temp stdin: %v", err)
	}
	if _, err := io.WriteString(in, submitWorkflowJSON); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
	if _, err := in.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("rewind stdin: %v", err)
	}
	defer in.Close()

	out, err := os.CreateTemp(dir, "submit-out-*.txt")
	if err != nil {
		t.Fatalf("temp stdout: %v", err)
	}
	defer out.Close()

	oldIn, oldOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = in, out
	cmdErr := cmdSubmit(args)
	os.Stdin, os.Stdout = oldIn, oldOut
	if cmdErr != nil {
		return nil, cmdErr
	}
	b, err := os.ReadFile(out.Name())
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	return b, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// TestSubmitSuccess covers the happy path: /prompt returns an id, /history
// reports image + video outputs on the first poll, and both files land in the
// -o dir with the /view bytes.
func TestSubmitSuccess(t *testing.T) {
	m := newComfyMock(t, mockCfg{
		promptID: "p1",
		histories: []string{mkHistory(t, "p1", "success", true, nodeCats{
			"35": {
				"images": {{Filename: "out.png", Type: "output"}},
				"videos": {{Filename: "clip.mp4", Subfolder: "movie", Type: "output"}},
			},
		})},
	})
	out := filepath.Join(t.TempDir(), "out")

	if _, err := runSubmit(t, "--host", m.srv.URL, "-o", out); err != nil {
		t.Fatalf("cmdSubmit: %v", err)
	}
	if p, h, view := m.callCounts(); p != 1 || h != 1 || view != 2 {
		t.Errorf("calls = (prompt, history, view) (%d, %d, %d), want (1, 1, 2)", p, h, view)
	}
	if v := m.viewedFiles(); !slices.Equal(v, []string{"clip.mp4", "out.png"}) {
		t.Errorf("downloaded files = %v, want [clip.mp4 out.png]", v)
	}
	for _, want := range []struct{ name, content string }{
		{"out.png", "bytes:out.png\n"},
		{"clip.mp4", "bytes:clip.mp4\n"},
	} {
		path := filepath.Join(out, want.name)
		if !fileExists(path) {
			t.Errorf("%s not saved in %s", want.name, out)
			continue
		}
		if got := readFile(t, path); got != want.content {
			t.Errorf("%s content = %q, want %q", want.name, got, want.content)
		}
	}
}

// TestSubmitIncludeTemp guards the --include-temp flag: temp-type outputs are
// skipped by default and downloaded only when the flag is set.
func TestSubmitIncludeTemp(t *testing.T) {
	hist := mkHistory(t, "p1", "success", true, nodeCats{
		"35": {"images": {{Filename: "out.png", Type: "output"}}},
		"36": {"images": {{Filename: "preview.png", Type: "temp"}}},
	})

	m := newComfyMock(t, mockCfg{promptID: "p1", histories: []string{hist}})
	out := filepath.Join(t.TempDir(), "out")
	if _, err := runSubmit(t, "--host", m.srv.URL, "-o", out); err != nil {
		t.Fatalf("cmdSubmit: %v", err)
	}
	if !fileExists(filepath.Join(out, "out.png")) {
		t.Error("out.png not downloaded")
	}
	if fileExists(filepath.Join(out, "preview.png")) {
		t.Error("temp preview downloaded without --include-temp")
	}
	if v := m.viewedFiles(); !slices.Equal(v, []string{"out.png"}) {
		t.Errorf("viewed files = %v, want [out.png]", v)
	}

	m2 := newComfyMock(t, mockCfg{promptID: "p1", histories: []string{hist}})
	out2 := filepath.Join(t.TempDir(), "out")
	if _, err := runSubmit(t, "--host", m2.srv.URL, "-o", out2, "--include-temp"); err != nil {
		t.Fatalf("cmdSubmit --include-temp: %v", err)
	}
	if !fileExists(filepath.Join(out2, "preview.png")) {
		t.Error("temp preview not downloaded with --include-temp")
	}
	if v := m2.viewedFiles(); !slices.Equal(v, []string{"out.png", "preview.png"}) {
		t.Errorf("viewed files = %v, want [out.png preview.png]", v)
	}
}

// TestSubmitNoDownload guards --no-download: the prompt id is printed and no
// history polling or /view downloads happen at all.
func TestSubmitNoDownload(t *testing.T) {
	m := newComfyMock(t, mockCfg{promptID: "p-42"})
	out := filepath.Join(t.TempDir(), "out")

	stdout, err := runSubmit(t, "--host", m.srv.URL, "-o", out, "--no-download")
	if err != nil {
		t.Fatalf("cmdSubmit: %v", err)
	}
	if got := strings.TrimSpace(string(stdout)); got != "p-42" {
		t.Errorf("stdout = %q, want prompt id p-42", got)
	}
	if p, history, view := m.callCounts(); p != 1 || history != 0 || view != 0 {
		t.Errorf("calls = (prompt, history, view) (%d, %d, %d), want (1, 0, 0)", p, history, view)
	}
	if fileExists(out) {
		t.Error("output dir created despite --no-download")
	}
}

// TestSubmitPromptRejected: a non-200 response from /prompt surfaces as an
// error.
func TestSubmitPromptRejected(t *testing.T) {
	m := newComfyMock(t, mockCfg{promptID: "p1", promptErr: true})
	_, err := runSubmit(t, "--host", m.srv.URL, "-o", filepath.Join(t.TempDir(), "out"))
	if err == nil {
		t.Fatal("expected error when /prompt returns HTTP 400")
	}
	if !strings.Contains(err.Error(), "HTTP 400") {
		t.Errorf("error = %v, want mention of HTTP 400", err)
	}
}

// TestSubmitHistoryError: a completed history entry with status_str != success
// surfaces as an error instead of downloading anything.
func TestSubmitHistoryError(t *testing.T) {
	m := newComfyMock(t, mockCfg{
		promptID: "p1",
		histories: []string{mkHistory(t, "p1", "error", true, nodeCats{
			"35": {"images": {{Filename: "out.png", Type: "output"}}},
		})},
	})
	_, err := runSubmit(t, "--host", m.srv.URL, "-o", filepath.Join(t.TempDir(), "out"))
	if err == nil {
		t.Fatal("expected error when history reports status_str: error")
	}
	if !strings.Contains(err.Error(), "did not succeed") {
		t.Errorf("error = %v, want a did-not-succeed error", err)
	}
}

// TestSubmitTimeout: a history that never completes trips the --timeout
// deadline. The deadline is checked after the first poll, so a negative
// timeout is already expired when waitForOutputs reaches the threshold check
// and the test exits without hitting its 1s poll sleep.
func TestSubmitTimeout(t *testing.T) {
	m := newComfyMock(t, mockCfg{promptID: "p1", histories: []string{"{}"}})
	_, err := runSubmit(t, "--host", m.srv.URL, "-o", filepath.Join(t.TempDir(), "out"),
		"--timeout", "-1ms")
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error = %v, want a timed-out error", err)
	}
	if _, h, view := m.callCounts(); h != 1 || view != 0 {
		t.Errorf("calls = history=%d, view=%d; want exactly one history poll and no downloads", h, view)
	}
}

// TestSubmitPathTraversal: a server-supplied traversal filename lands in the
// -o dir under its basename and never escapes.
func TestSubmitPathTraversal(t *testing.T) {
	m := newComfyMock(t, mockCfg{
		promptID: "p1",
		histories: []string{mkHistory(t, "p1", "success", true, nodeCats{
			"35": {"images": {{Filename: "../../evil.png", Type: "output"}}},
		})},
	})
	root := t.TempDir()
	out := filepath.Join(root, "out")

	if _, err := runSubmit(t, "--host", m.srv.URL, "-o", out); err != nil {
		t.Fatalf("cmdSubmit: %v", err)
	}
	saved := filepath.Join(out, "evil.png")
	if !fileExists(saved) {
		t.Fatalf("saved file %s does not exist", saved)
	}
	if got := readFile(t, saved); got != "bytes:../../evil.png\n" {
		t.Errorf("saved content = %q, want the raw /view payload", got)
	}
	if fileExists(filepath.Join(root, "evil.png")) {
		t.Error("path traversal escaped the output dir")
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatalf("read out dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "evil.png" {
		t.Errorf("output dir entries = %d, want just evil.png", len(entries))
	}
}
