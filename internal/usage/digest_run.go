package usage

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// assets are the summarizer scripts, shipped inside the binary so `claudeme`
// stays a single file. They are copies of spike-summarize/, verbatim — the
// spike remains where the bake-off evidence lives.
//
//go:embed assets/distill.py assets/summarize.sh assets/schema.json
var assets embed.FS

// DigestModel is the model summarize.sh calls. Recorded on every digest so a
// summary can always be traced to what produced it.
const DigestModel = "gpt-5.6-luna"

// Runner holds the unpacked scripts for the length of a digest run. Unpacking
// once and reusing it keeps a 500-session backfill from writing the same three
// files 500 times.
type Runner struct {
	dir string

	// Quiet drops summarize.sh's per-session desktop notification. Correct for
	// a bulk run, where a handful of transient failures out of hundreds would
	// otherwise be a handful of popups for a condition the next run heals.
	Quiet bool
}

// NewRunner unpacks the embedded scripts into a private temp directory.
func NewRunner() (*Runner, error) {
	dir, err := os.MkdirTemp("", "claudeme-digest-")
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	for _, name := range []string{"distill.py", "summarize.sh", "schema.json"} {
		data, err := assets.ReadFile("assets/" + name)
		if err != nil {
			os.RemoveAll(dir)
			return nil, err
		}
		mode := os.FileMode(0o600)
		if strings.HasSuffix(name, ".sh") {
			mode = 0o700
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, mode); err != nil {
			os.RemoveAll(dir)
			return nil, err
		}
	}
	return &Runner{dir: dir}, nil
}

// Close removes the unpacked scripts.
func (r *Runner) Close() error {
	if r == nil || r.dir == "" {
		return nil
	}
	return os.RemoveAll(r.dir)
}

// Summarize runs the pipeline over one transcript and returns the model's JSON
// summary. Archived transcripts are decompressed first: distill.py reads plain
// JSONL, and teaching it gzip would fork the script from the spike.
func (r *Runner) Summarize(transcript string) (json.RawMessage, error) {
	src := transcript
	if strings.HasSuffix(transcript, gzExt) {
		plain, err := r.decompress(transcript)
		if err != nil {
			return nil, err
		}
		defer os.Remove(plain)
		src = plain
	}

	out := filepath.Join(r.dir, "out.json")
	os.Remove(out)

	cmd := exec.Command(filepath.Join(r.dir, "summarize.sh"), src, out)
	cmd.Dir = r.dir
	if r.Quiet {
		cmd.Env = append(os.Environ(), "SUMMARIZE_NOTIFY=0")
	}
	stderr := &strings.Builder{}
	cmd.Stderr = stderr
	cmd.Stdout = io.Discard
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w: %s", err, lastLines(stderr.String(), 3))
	}

	data, err := os.ReadFile(out)
	if err != nil {
		return nil, err
	}
	if !json.Valid(data) {
		return nil, fmt.Errorf("summarize.sh wrote invalid JSON for %s", filepath.Base(transcript))
	}
	os.Remove(out)
	return json.RawMessage(data), nil
}

// decompress writes a gzipped transcript out as plain JSONL and returns the
// path, which the caller removes.
func (r *Runner) decompress(transcript string) (string, error) {
	in, err := openTranscript(transcript)
	if err != nil {
		return "", err
	}
	defer in.Close()

	plain := filepath.Join(r.dir, sessionID(transcript)+".jsonl")
	f, err := os.OpenFile(plain, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(f, in); err != nil {
		f.Close()
		os.Remove(plain)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(plain)
		return "", err
	}
	return plain, nil
}

// lastLines keeps the tail of a failed run's stderr — summarize.sh already
// prints the codex log tail there, and the whole log is too much for a
// one-line-per-session report.
func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}
