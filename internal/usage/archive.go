package usage

import (
	"compress/gzip"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Archiving moves old transcripts out of the tree Claude Code manages and into
// a gzipped mirror of it. Two things follow from that, and both are the point:
// the live tree stays small, and the archived history is beyond the reach of
// cleanupPeriodDays, so reports get deeper over time instead of truncating at
// the retention window.
//
// The move is per-file and ordered write-then-delete, so an interrupted run
// leaves every transcript readable in exactly one of the two roots.

// ArchiveResult is what one archive run did.
type ArchiveResult struct {
	Files  int   // transcripts moved
	Metas  int   // sidecar .meta.json files moved with them
	Before int64 // bytes read from the live tree
	After  int64 // bytes written to the archive
}

// Saved returns the disk space the compression recovered.
func (r ArchiveResult) Saved() int64 { return r.Before - r.After }

// Archive moves every transcript under src last modified before cutoff into
// dst, gzipped, preserving the relative path. Subagent metadata sidecars move
// with their transcript so agent attribution survives.
//
// A dry run reports what would move and touches nothing.
func Archive(src, dst string, cutoff time.Time, dryRun bool) (ArchiveResult, error) {
	var res ArchiveResult

	stale, err := staleTranscripts(src, cutoff)
	if err != nil {
		return res, err
	}
	sort.Strings(stale)

	for _, path := range stale {
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return res, err
		}
		info, err := os.Stat(path)
		if err != nil {
			continue // vanished mid-run: nothing to move
		}

		res.Files++
		res.Before += info.Size()
		if dryRun {
			res.After += info.Size() / 4 // rough: jsonl gzips to about a quarter
			res.Metas += countMeta(path)
			continue
		}

		written, err := gzipTo(path, filepath.Join(dst, rel+gzExt), info.ModTime())
		if err != nil {
			return res, err
		}
		res.After += written
		if err := os.Remove(path); err != nil {
			return res, err
		}

		moved, err := moveMeta(path, src, dst)
		if err != nil {
			return res, err
		}
		res.Metas += moved
	}

	if !dryRun {
		pruneEmptyDirs(src, stale)
	}
	return res, nil
}

// staleTranscripts lists the uncompressed transcripts under src older than
// cutoff. Already-archived files never appear: the archive is a separate root.
func staleTranscripts(src string, cutoff time.Time) ([]string, error) {
	var out []string
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable subtree: skip, not fatal
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		info, err := d.Info()
		if err != nil || !info.ModTime().Before(cutoff) {
			return nil
		}
		out = append(out, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// metaPath is the subagent metadata sidecar for a transcript, or "" when the
// transcript is not a subagent's or has no sidecar.
func metaPath(transcript string) string {
	if !strings.HasPrefix(filepath.Base(transcript), "agent-") {
		return ""
	}
	meta := strings.TrimSuffix(transcript, ".jsonl") + ".meta.json"
	if _, err := os.Stat(meta); err != nil {
		return ""
	}
	return meta
}

func countMeta(transcript string) int {
	if metaPath(transcript) == "" {
		return 0
	}
	return 1
}

// moveMeta relocates a transcript's metadata sidecar, uncompressed — it is a
// few hundred bytes and agentType reads it directly off disk.
func moveMeta(transcript, src, dst string) (int, error) {
	meta := metaPath(transcript)
	if meta == "" {
		return 0, nil
	}
	rel, err := filepath.Rel(src, meta)
	if err != nil {
		return 0, err
	}
	target := filepath.Join(dst, rel)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return 0, err
	}
	data, err := os.ReadFile(meta)
	if err != nil {
		return 0, nil // gone already: the transcript still moved fine
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		return 0, err
	}
	if err := os.Remove(meta); err != nil {
		return 0, err
	}
	return 1, nil
}

// gzipTo compresses src to dst and returns the bytes written. It writes to a
// temporary file and renames, so a partial write is never mistaken for an
// archived transcript. The original mtime carries over: it is what a second
// archive run, and every "last seen" column, reads.
func gzipTo(src, dst string, modTime time.Time) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return 0, err
	}
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()

	tmp, err := os.CreateTemp(filepath.Dir(dst), ".archive-*")
	if err != nil {
		return 0, err
	}
	defer os.Remove(tmp.Name()) // no-op once the rename succeeds

	zw := gzip.NewWriter(tmp)
	if _, err := io.Copy(zw, in); err != nil {
		tmp.Close()
		return 0, err
	}
	if err := zw.Close(); err != nil {
		tmp.Close()
		return 0, err
	}
	size, err := tmp.Seek(0, io.SeekEnd)
	if err != nil {
		tmp.Close()
		return 0, err
	}
	if err := tmp.Close(); err != nil {
		return 0, err
	}
	if err := os.Chtimes(tmp.Name(), modTime, modTime); err != nil {
		return 0, err
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		return 0, err
	}
	return size, nil
}

// pruneEmptyDirs removes the directories this run emptied, deepest first,
// stopping below root.
//
// Only directories a moved file lived in are considered. A transcript
// directory that was already empty is the last trace of a project whose
// history retention deleted — `projects --all` still lists it — and archiving
// is not the thing that gets to erase that.
//
// Errors are ignored: a directory that refuses to go is cosmetic, not a
// failed archive.
func pruneEmptyDirs(root string, moved []string) {
	touched := map[string]bool{}
	for _, path := range moved {
		for d := filepath.Dir(path); isUnder(d, root) && d != root; d = filepath.Dir(d) {
			touched[d] = true
		}
	}

	dirs := make([]string, 0, len(touched))
	for d := range touched {
		dirs = append(dirs, d)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dirs)))

	for _, d := range dirs {
		if entries, err := os.ReadDir(d); err == nil && len(entries) == 0 {
			os.Remove(d)
		}
	}
}
