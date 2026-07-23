package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const submitUsage = `comfyctl submit [flags] - submits the workflow from stdin to ComfyUI

The workflow is read from stdin (must be API format, same as 'dump'/'set'),
POSTed to the ComfyUI /prompt endpoint, and its output files are downloaded
once generation finishes.

Flags:
  --host string     ComfyUI server address (default "http://127.0.0.1:8188")
  -o, --out dir     directory to save output files (default ".")
  --prefix string   prefix prepended to saved output filenames
  --no-download     submit only; print prompt id and exit without waiting
  --include-temp    also download 'temp' type outputs (previews)
  --timeout dur     max time to wait for completion (default 10m0s)

Example:
  comfyctl set seed random < wf.json | comfyctl submit -o ./out`

type submitOpts struct {
	host        string
	out         string
	prefix      string
	noDownload  bool
	includeTemp bool
	timeout     time.Duration
}

func cmdSubmit(args []string) error {
	var opts submitOpts
	fs := flag.NewFlagSet("submit", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprintln(os.Stderr, submitUsage) }
	fs.StringVar(&opts.host, "host", "http://127.0.0.1:8188", "ComfyUI server address")
	fs.StringVar(&opts.out, "out", ".", "directory to save output files")
	fs.StringVar(&opts.out, "o", ".", "directory to save output files (shorthand)")
	fs.StringVar(&opts.prefix, "prefix", "", "prefix prepended to saved output filenames")
	fs.BoolVar(&opts.noDownload, "no-download", false, "submit only; do not wait or download")
	fs.BoolVar(&opts.includeTemp, "include-temp", false, "also download 'temp' type outputs")
	fs.DurationVar(&opts.timeout, "timeout", 10*time.Minute, "max time to wait for completion")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil // usage already printed by the flag set
		}
		return err
	}

	base, err := normalizeHost(opts.host)
	if err != nil {
		return err
	}

	reader := bufio.NewReader(os.Stdin)
	cw, err := OpenComfyWorkflow(reader)
	if err != nil {
		return fmt.Errorf("Error parsing workflow: %v", err)
	}

	client := &comfyClient{base: base, http: &http.Client{}}
	clientID := newClientID()

	promptID, err := client.submitPrompt(cw.Raw, clientID)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "submitted, prompt id: %s\n", promptID)

	if opts.noDownload {
		fmt.Println(promptID)
		return nil
	}

	outputs, err := client.waitForOutputs(promptID, opts.timeout)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(opts.out, 0o755); err != nil {
		return fmt.Errorf("Unable to create output directory %s: %v", opts.out, err)
	}

	saved := 0
	for _, f := range outputs {
		if f.Type == "temp" && !opts.includeTemp {
			continue
		}
		dest, err := client.downloadOutput(f, opts.out, opts.prefix)
		if err != nil {
			return fmt.Errorf("Error downloading %s: %v", f.Filename, err)
		}
		fmt.Println(dest)
		saved++
	}
	if saved == 0 {
		fmt.Fprintln(os.Stderr, "warning: workflow completed but produced no downloadable outputs")
	}
	return nil
}

// normalizeHost accepts "host:port", "http://host:port" or a full URL and
// returns a clean base URL with a scheme and no trailing slash.
func normalizeHost(host string) (string, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return "", fmt.Errorf("empty --host")
	}
	if !strings.Contains(host, "://") {
		host = "http://" + host
	}
	u, err := url.Parse(host)
	if err != nil {
		return "", fmt.Errorf("invalid --host %q: %v", host, err)
	}
	if u.Host == "" {
		return "", fmt.Errorf("invalid --host %q: missing host", host)
	}
	return strings.TrimRight(u.Scheme+"://"+u.Host, "/"), nil
}

// newClientID returns a random UUIDv4-shaped string used to tag the submission.
func newClientID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand should not fail; fall back to a time-based id.
		return fmt.Sprintf("comfyctl-%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

type comfyClient struct {
	base string
	http *http.Client
}

// outputFile identifies one artifact produced by a workflow, as reported in the
// /history outputs map.
type outputFile struct {
	Filename  string `json:"filename"`
	Subfolder string `json:"subfolder"`
	Type      string `json:"type"`
}

func (c *comfyClient) submitPrompt(raw map[string]any, clientID string) (string, error) {
	body, err := json.Marshal(map[string]any{
		"prompt":    raw,
		"client_id": clientID,
	})
	if err != nil {
		return "", fmt.Errorf("Unable to encode prompt: %v", err)
	}
	resp, err := c.http.Post(c.base+"/prompt", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("Unable to reach ComfyUI at %s: %v", c.base, err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ComfyUI rejected the prompt (HTTP %d): %s",
			resp.StatusCode, strings.TrimSpace(string(data)))
	}

	var out struct {
		PromptID   string          `json:"prompt_id"`
		NodeErrors json.RawMessage `json:"node_errors"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("Unexpected response from /prompt: %v", err)
	}
	if out.PromptID == "" {
		return "", fmt.Errorf("ComfyUI did not return a prompt id: %s", strings.TrimSpace(string(data)))
	}
	return out.PromptID, nil
}

// historyEntry is the subset of a /history/<id> record we consume.
type historyEntry struct {
	Outputs map[string]map[string]json.RawMessage `json:"outputs"`
	Status  struct {
		StatusStr string `json:"status_str"`
		Completed bool   `json:"completed"`
	} `json:"status"`
}

// waitForOutputs polls /history/<promptID> until the prompt reaches a terminal
// state, then returns its output files. Errors if the run failed or the timeout
// elapses.
func (c *comfyClient) waitForOutputs(promptID string, timeout time.Duration) ([]outputFile, error) {
	deadline := time.Now().Add(timeout)
	for {
		entry, found, err := c.fetchHistory(promptID)
		if err != nil {
			return nil, err
		}
		if found && entry.Status.Completed {
			if entry.Status.StatusStr != "" && entry.Status.StatusStr != "success" {
				return nil, fmt.Errorf("workflow did not succeed (status: %s)", entry.Status.StatusStr)
			}
			return collectOutputs(entry), nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out after %s waiting for prompt %s to complete", timeout, promptID)
		}
		time.Sleep(time.Second)
	}
}

func (c *comfyClient) fetchHistory(promptID string) (historyEntry, bool, error) {
	resp, err := c.http.Get(c.base + "/history/" + url.PathEscape(promptID))
	if err != nil {
		return historyEntry{}, false, fmt.Errorf("Unable to poll history: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return historyEntry{}, false, fmt.Errorf("history request failed (HTTP %d)", resp.StatusCode)
	}
	var hist map[string]historyEntry
	if err := json.NewDecoder(resp.Body).Decode(&hist); err != nil {
		return historyEntry{}, false, fmt.Errorf("Unexpected response from /history: %v", err)
	}
	entry, found := hist[promptID]
	return entry, found, nil
}

// collectOutputs flattens every file entry across output categories (images,
// gifs, videos, audio, ...) into a single slice. Categories whose values are
// not file-descriptor arrays (e.g. scalar text outputs) are skipped.
func collectOutputs(entry historyEntry) []outputFile {
	var files []outputFile
	for _, category := range entry.Outputs {
		for _, raw := range category {
			var items []outputFile
			if err := json.Unmarshal(raw, &items); err != nil {
				continue // not a file array (e.g. "text" outputs)
			}
			for _, it := range items {
				if it.Filename != "" {
					files = append(files, it)
				}
			}
		}
	}
	return files
}

// downloadOutput fetches one file from /view and writes it into dir, returning
// the path it was written to.
func (c *comfyClient) downloadOutput(f outputFile, dir, prefix string) (string, error) {
	q := url.Values{}
	q.Set("filename", f.Filename)
	q.Set("subfolder", f.Subfolder)
	q.Set("type", f.Type)

	resp, err := c.http.Get(c.base + "/view?" + q.Encode())
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// Guard against path traversal from server-supplied names.
	dest := filepath.Join(dir, prefix+filepath.Base(f.Filename))
	out, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	defer out.Close()
	if _, err := io.Copy(out, resp.Body); err != nil {
		return "", err
	}
	return dest, nil
}
