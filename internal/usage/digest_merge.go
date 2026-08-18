package usage

import "fmt"

// PutDigest files one session into history/<date>/<project>.json, creating the
// file on first use. Existing sessions in the file are preserved; the same
// session digested twice replaces itself rather than appearing twice.
//
// Every write passes through StripNaiveTokens, so distill.py's undeduped token
// counts never reach disk to contradict the ledger in Tokens. This is the one
// funnel every writer goes through, which is why the normalization lives here
// rather than in each caller.
func PutDigest(root string, d *Digest) error {
	if d.Session == "" || d.Date == "" {
		return fmt.Errorf("digest needs a session and a date, got %q/%q", d.Session, d.Date)
	}
	if cleaned, changed := StripNaiveTokens(d.Metrics); changed {
		d.Metrics = cleaned
	}
	path := digestPathIn(root, d.Date, d.Project)
	f, err := LoadDigest(path)
	if err != nil {
		return err
	}
	f.Version = digestVersion
	f.Date = d.Date
	f.Project = d.Project
	// Updated only ever moves forward: a backfill re-filing an old session
	// must not make the file look older than the last summary written to it.
	stamps := []string{d.DigestedAt, d.MetricsAt}
	if d.Prompts != nil {
		stamps = append(stamps, d.Prompts.Last)
	}
	if d.Tokens != nil {
		stamps = append(stamps, d.Tokens.ExtractedAt)
	}
	for _, stamp := range stamps {
		if stamp > f.Updated {
			f.Updated = stamp
		}
	}
	f.Sessions[d.Session] = d
	return SaveDigest(path, f)
}

// Records returns every session on record for one date+project, keyed by
// session id. Pending and PendingMetrics ask different questions of the same
// record, so they read it rather than a flattened set of ids.
func Records(root, date, project string) (map[string]*Digest, error) {
	f, err := LoadDigest(digestPathIn(root, date, project))
	if err != nil {
		return nil, err
	}
	return f.Sessions, nil
}

// GetDigest returns one session's record, or nil when it has none. The
// metrics backfill reads it to add a field without discarding the summary
// that is already there.
func GetDigest(root, date, project, session string) (*Digest, error) {
	sessions, err := Records(root, date, project)
	if err != nil {
		return nil, err
	}
	return sessions[session], nil
}

// PutTokens files a session's token ledger onto the record already there,
// creating the record when the session has never been digested. Everything
// else on the record — summary, model, metrics — is left untouched.
func PutTokens(root string, c Candidate, t *Tokens) error {
	d, err := GetDigest(root, c.Date, c.Project, c.Session)
	if err != nil {
		return err
	}
	if d == nil {
		d = &Digest{
			Session:    c.Session,
			Date:       c.Date,
			Cwd:        c.Cwd,
			Project:    c.Project,
			Transcript: c.Path,
		}
	}
	d.Tokens = t
	return PutDigest(root, d)
}

// Digested reports the session ids already on record for one date+project.
func Digested(root, date, project string) (map[string]bool, error) {
	sessions, err := Records(root, date, project)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(sessions))
	for id := range sessions {
		seen[id] = true
	}
	return seen, nil
}
