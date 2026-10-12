package store

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"crgformat/legacy"
	"crgformat/replay"
	"crgformat/statsbook"
)

// MaxFile is the largest file RATS reads (a zip's files each count).
const MaxFile = 64 << 20

// Add reads an uploaded file and keeps it: a Java scoreboard file (one game
// or a data export with many), a statsbook, or a zip of those. tournament
// is the tournament the games are from, "" for what each game says. Files
// uploaded together share batch, their upload time. A file that can't be
// read is kept with its error, so the upload list shows it.
func (ns *Namespace) Add(name string, data []byte, tournament string, batch time.Time) ([]*File, error) {
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	kind, err := sniff(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if kind == "zip" {
		return ns.addZip(name, data, tournament, batch)
	}
	f, err := ns.read(name, kind, data, tournament, batch)
	if err != nil {
		return nil, err
	}
	return []*File{f}, nil
}

func (ns *Namespace) addZip(name string, data []byte, tournament string, batch time.Time) ([]*File, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	var out []*File
	for _, zf := range zr.File {
		base := path.Base(zf.Name)
		lower := strings.ToLower(base)
		if zf.FileInfo().IsDir() || strings.HasPrefix(zf.Name, "__MACOSX/") || strings.HasPrefix(base, ".") ||
			!(strings.HasSuffix(lower, ".json") || strings.HasSuffix(lower, ".xlsx")) {
			continue
		}
		if zf.UncompressedSize64 > MaxFile {
			return out, fmt.Errorf("%s in %s is too large", base, name)
		}
		rc, err := zf.Open()
		if err != nil {
			return out, err
		}
		inner, err := io.ReadAll(io.LimitReader(rc, MaxFile+1))
		rc.Close()
		if err != nil {
			return out, err
		}
		kind, err := sniff(inner)
		if err != nil || kind == "zip" {
			continue
		}
		f, err := ns.read(base, kind, inner, tournament, batch)
		if err != nil {
			return out, err
		}
		out = append(out, f)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: no scoreboard files or statsbooks in the zip", name)
	}
	return out, nil
}

// sniff tells a statsbook (an xlsx, which is a zip with a workbook), a
// zip of files and a JSON file apart.
func sniff(data []byte) (string, error) {
	if len(data) > MaxFile {
		return "", errors.New("the file is too large")
	}
	if bytes.HasPrefix(data, []byte("PK")) {
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return "", errors.New("not a statsbook or zip file")
		}
		for _, f := range zr.File {
			if f.Name == "xl/workbook.xml" {
				return KindStatsbook, nil
			}
		}
		return "zip", nil
	}
	if t := bytes.TrimLeft(data, " \t\r\n\ufeff"); len(t) > 0 && t[0] == '{' {
		return KindScoreboard, nil
	}
	return "", errors.New("not a scoreboard file (.json), statsbook (.xlsx) or zip")
}

// read reads one file's games and keeps the file.
func (ns *Namespace) read(name, kind string, data []byte, tournament string, batch time.Time) (*File, error) {
	f := &File{ID: newID(6), Name: name, Kind: kind, Size: int64(len(data)), Uploaded: batch.UTC(),
		Tournament: strings.TrimSpace(tournament), Entries: []*Entry{}}
	var sums []*replay.Summary
	switch kind {
	case KindScoreboard:
		results, err := legacy.ConvertFile(data, name, ns.v)
		if err != nil {
			f.Error = "not a CRG scoreboard file: " + err.Error()
			break
		}
		if len(results) == 0 {
			f.Error = "no games in this file (only teams or settings?)"
		}
		for _, r := range results {
			e := &Entry{State: r.State, Teams: [2]string{}}
			if r.Err != nil {
				e.Error = r.Err.Error()
				e.Teams[0] = r.Name
				sums = append(sums, nil)
			} else {
				describe(e, r.Summary)
				e.Notes = trimNotes(r.Notes)
				if e.Jams == 0 {
					sums = append(sums, nil) // a prepared game: nothing to count
				} else {
					sums = append(sums, r.Summary)
				}
			}
			f.Entries = append(f.Entries, e)
		}
	case KindStatsbook:
		r, err := statsbook.Import(data, name, ns.v)
		if err != nil {
			f.Error = "not a statsbook RATS can read: " + err.Error()
			break
		}
		e := &Entry{State: "Finished", Teams: [2]string{}}
		describe(e, r.Summary)
		e.Notes = trimNotes(r.Notes)
		if e.Jams == 0 {
			e.State = "Prepared" // a statsbook filled in before the game
			sums = append(sums, nil)
		} else {
			sums = append(sums, r.Summary)
		}
		f.Entries = append(f.Entries, e)
	}
	ns.mu.Lock()
	defer ns.mu.Unlock()
	if err := os.WriteFile(filepath.Join(ns.dir, "files", f.ID+ext(kind)), data, 0o644); err != nil {
		return nil, err
	}
	if err := writeSummaries(filepath.Join(ns.dir, "read", f.ID+".json.gz"), sums); err != nil {
		return nil, err
	}
	if ns.summaries == nil {
		ns.summaries = map[string][]*replay.Summary{}
	}
	ns.summaries[f.ID] = sums
	ns.Files = append(ns.Files, f)
	ns.games = nil
	return f, ns.save()
}

func describe(e *Entry, s *replay.Summary) {
	e.Tournament, e.Date, e.GameNo = strings.TrimSpace(s.Info["Tournament"]), s.Info["Date"], s.Info["GameNo"]
	for i := 0; i < 2 && i < len(s.Teams); i++ {
		e.Teams[i] = TeamName(s.Teams[i])
	}
	// A league's two teams playing each other: their team names.
	if len(s.Teams) == 2 && e.Teams[0] == e.Teams[1] {
		for i := range 2 {
			if n := strings.TrimSpace(firstOf(s.Teams[i].TeamName, s.Teams[i].Name)); n != "" {
				e.Teams[i] = n
			}
		}
	}
	for _, p := range s.Periods {
		e.Jams += len(p.Jams)
	}
	e.Penalties = len(s.Penalties)
}

// TeamName is how a game's team is shown: the league, or the team's name
// without one.
func TeamName(t *replay.Team) string {
	return strings.Join(strings.Fields(firstOf(t.League, t.TeamName, t.Name)), " ")
}

// trimNotes drops what every statsbook says and keeps a few.
func trimNotes(notes []string) []string {
	var out []string
	for _, n := range notes {
		if strings.Contains(n, "ruleset isn't in a statsbook") {
			continue
		}
		out = append(out, n)
		if len(out) == 8 {
			break
		}
	}
	return out
}
