package usage

import "fmt"

// PutDigest files one session into history/<date>/<project>.json, creating the
// file on first use. Existing sessions in the file are preserved; the same
// session digested twice replaces itself rather than appearing twice.
func PutDigest(root string, d *Digest) error {
	if d.Session == "" || d.Date == "" {
		return fmt.Errorf("digest needs a session and a date, got %q/%q", d.Session, d.Date)
	}
	path := digestPathIn(root, d.Date, d.Project)
	f, err := LoadDigest(path)
	if err != nil {
		return err
	}
	f.Version = digestVersion
	f.Date = d.Date
	f.Project = d.Project
	f.Updated = d.DigestedAt
	f.Sessions[d.Session] = d
	return SaveDigest(path, f)
}

// Digested reports the session ids already on record for one date+project.
func Digested(root, date, project string) (map[string]bool, error) {
	f, err := LoadDigest(digestPathIn(root, date, project))
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(f.Sessions))
	for id := range f.Sessions {
		seen[id] = true
	}
	return seen, nil
}
