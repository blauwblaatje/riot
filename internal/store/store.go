// Package store keeps RIOT's namespaces: a namespace is a set of uploaded
// files (Java scoreboard game files and statsbooks), each said to be from a
// tournament, kept on disk so that it can be added to later.
//
// On disk, under the data folder:
//
//	ns/<id>/namespace.json    name, files, what was read from them
//	ns/<id>/files/<fid>.json  the uploaded file as it was (.xlsx for statsbooks)
//	ns/<id>/read/<fid>.json.gz  the games read from it (replay summaries)
package store

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"crgformat"
	"crgformat/replay"
)

// Kinds of file.
const (
	KindScoreboard = "scoreboard" // a Java CRG game file or data export (.json)
	KindStatsbook  = "statsbook"  // a WFTDA statsbook (.xlsx)
)

// NoTournament is the tournament of games that don't say.
const NoTournament = "Other games"

// Store is the data folder.
type Store struct {
	dir  string
	v    *crgformat.Validators
	mu   sync.Mutex
	open map[string]*Namespace
	// Idle: a namespace not used for this long is dropped from memory (its
	// games are read again from disk when it's next used).
	Idle time.Duration
}

// Namespace is a set of files.
type Namespace struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Created time.Time `json:"created"`
	Files   []*File   `json:"files"`

	mu        sync.Mutex
	dir       string
	v         *crgformat.Validators
	summaries map[string][]*replay.Summary // file id -> games read, per entry
	used      time.Time
	games     *Games // grouped, nil after a change
}

// File is an uploaded file.
type File struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Kind     string    `json:"kind"`
	Size     int64     `json:"size"`
	Uploaded time.Time `json:"uploaded"`
	// Tournament set at upload or later; "" means the one each game names.
	Tournament string `json:"tournament,omitempty"`
	// Ignored files are kept but left out of the games (an older save).
	Ignored bool     `json:"ignored,omitempty"`
	Error   string   `json:"error,omitempty"` // the file couldn't be read
	Entries []*Entry `json:"entries"`
}

// Entry is a game read from a file.
type Entry struct {
	Tournament string    `json:"tournament,omitempty"` // as the file says
	Date       string    `json:"date,omitempty"`
	GameNo     string    `json:"gameNo,omitempty"`
	Teams      [2]string `json:"teams"`
	State      string    `json:"state,omitempty"` // Java: Prepared, Running, Finished
	Jams       int       `json:"jams"`
	Penalties  int       `json:"penalties"`
	Notes      []string  `json:"notes,omitempty"`
	Error      string    `json:"error,omitempty"`
}

// TournamentOf is the tournament an entry of f counts for.
func (f *File) TournamentOf(e *Entry) string {
	return strings.TrimSpace(firstOf(f.Tournament, e.Tournament, NoTournament))
}

func firstOf(s ...string) string {
	for _, x := range s {
		if strings.TrimSpace(x) != "" {
			return x
		}
	}
	return ""
}

// Open opens (and makes) the data folder.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(dir, "ns"), 0o755); err != nil {
		return nil, err
	}
	v, err := crgformat.NewValidators()
	if err != nil {
		return nil, err
	}
	return &Store{dir: dir, v: v, open: map[string]*Namespace{}, Idle: 20 * time.Minute}, nil
}

var idRe = regexp.MustCompile(`^[0-9a-f]{10}$`)

// ErrNotFound: no such namespace or file.
var ErrNotFound = errors.New("not found")

func newID(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Create makes a namespace.
func (s *Store) Create(name string) (*Namespace, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("give the namespace a name")
	}
	ns := &Namespace{ID: newID(5), Name: name, Created: time.Now().UTC(), Files: []*File{},
		summaries: map[string][]*replay.Summary{}, used: time.Now(), v: s.v}
	ns.dir = filepath.Join(s.dir, "ns", ns.ID)
	for _, d := range []string{"files", "read"} {
		if err := os.MkdirAll(filepath.Join(ns.dir, d), 0o755); err != nil {
			return nil, err
		}
	}
	if err := ns.save(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.open[ns.ID] = ns
	s.mu.Unlock()
	return ns, nil
}

// Get opens a namespace.
func (s *Store) Get(id string) (*Namespace, error) {
	if !idRe.MatchString(id) {
		return nil, ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ns := s.open[id]; ns != nil {
		ns.mu.Lock()
		ns.used = time.Now()
		ns.mu.Unlock()
		return ns, nil
	}
	dir := filepath.Join(s.dir, "ns", id)
	data, err := os.ReadFile(filepath.Join(dir, "namespace.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	ns := &Namespace{}
	if err := json.Unmarshal(data, ns); err != nil {
		return nil, fmt.Errorf("namespace %s: %w", id, err)
	}
	ns.dir, ns.used, ns.v = dir, time.Now(), s.v
	s.open[id] = ns
	return ns, nil
}

// Delete removes a namespace and its files.
func (s *Store) Delete(id string) error {
	if !idRe.MatchString(id) {
		return ErrNotFound
	}
	s.mu.Lock()
	delete(s.open, id)
	s.mu.Unlock()
	return os.RemoveAll(filepath.Join(s.dir, "ns", id))
}

// Clean drops namespaces not used for a while from memory.
func (s *Store) Clean() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, ns := range s.open {
		ns.mu.Lock()
		idle := time.Since(ns.used) > s.Idle
		ns.mu.Unlock()
		if idle {
			delete(s.open, id)
		}
	}
}

// Count is the number of namespaces on disk.
func (s *Store) Count() int {
	m, _ := filepath.Glob(filepath.Join(s.dir, "ns", "*", "namespace.json"))
	return len(m)
}

// save writes namespace.json (ns.mu held, or not yet shared).
func (ns *Namespace) save() error {
	data, err := json.MarshalIndent(ns, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(ns.dir, "namespace.json.tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(ns.dir, "namespace.json"))
}

// Snapshot is a copy of the namespace's fields, safe to encode.
func (ns *Namespace) Snapshot() Namespace {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	files := make([]*File, len(ns.Files))
	for i, f := range ns.Files {
		c := *f
		files[i] = &c
	}
	return Namespace{ID: ns.ID, Name: ns.Name, Created: ns.Created, Files: files}
}

// Rename changes the namespace's name.
func (ns *Namespace) Rename(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("the name can't be empty")
	}
	ns.mu.Lock()
	defer ns.mu.Unlock()
	ns.Name = name
	return ns.save()
}

// file finds a file (ns.mu held).
func (ns *Namespace) file(id string) *File {
	for _, f := range ns.Files {
		if f.ID == id {
			return f
		}
	}
	return nil
}

// FilePath is where an uploaded file is kept, and its name.
func (ns *Namespace) FilePath(id string) (path, name string, err error) {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	f := ns.file(id)
	if f == nil {
		return "", "", ErrNotFound
	}
	return filepath.Join(ns.dir, "files", f.ID+ext(f.Kind)), f.Name, nil
}

func ext(kind string) string {
	if kind == KindStatsbook {
		return ".xlsx"
	}
	return ".json"
}

// Update changes a file's tournament ("" for the file's own) or whether it
// is ignored.
func (ns *Namespace) Update(id string, tournament *string, ignored *bool) error {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	f := ns.file(id)
	if f == nil {
		return ErrNotFound
	}
	if tournament != nil {
		f.Tournament = strings.TrimSpace(*tournament)
	}
	if ignored != nil {
		f.Ignored = *ignored
	}
	ns.games = nil
	return ns.save()
}

// Remove deletes a file.
func (ns *Namespace) Remove(id string) error {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	for i, f := range ns.Files {
		if f.ID == id {
			ns.Files = append(ns.Files[:i], ns.Files[i+1:]...)
			os.Remove(filepath.Join(ns.dir, "files", f.ID+ext(f.Kind)))
			os.Remove(filepath.Join(ns.dir, "read", f.ID+".json.gz"))
			delete(ns.summaries, f.ID)
			ns.games = nil
			return ns.save()
		}
	}
	return ErrNotFound
}

// summariesOf reads a file's games from disk once (ns.mu held).
func (ns *Namespace) summariesOf(f *File) []*replay.Summary {
	if ns.summaries == nil {
		ns.summaries = map[string][]*replay.Summary{}
	}
	if s, ok := ns.summaries[f.ID]; ok {
		return s
	}
	var out []*replay.Summary
	if data, err := os.ReadFile(filepath.Join(ns.dir, "read", f.ID+".json.gz")); err == nil {
		if zr, err := gzip.NewReader(bytes.NewReader(data)); err == nil {
			json.NewDecoder(zr).Decode(&out)
		}
	}
	ns.summaries[f.ID] = out
	return out
}

func writeSummaries(path string, s []*replay.Summary) error {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if err := json.NewEncoder(zw).Encode(s); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// Tournaments lists the tournament names used in the namespace, sorted.
func (ns *Namespace) Tournaments() []string {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	seen := map[string]bool{}
	var out []string
	for _, f := range ns.Files {
		for _, e := range f.Entries {
			if t := f.TournamentOf(e); !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	sort.Strings(out)
	return out
}
