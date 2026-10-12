// Package web is RIOT's website: make a namespace, upload scoreboard game
// files and statsbooks into it with the tournament they're from, and look
// at the stats of one tournament or several together, the officiating
// crews, and what the statsbooks corrected.
package web

import (
	"bytes"
	"cmp"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"crgformat/stats"

	"riot/internal/analysis"
	"riot/internal/logs"
	"riot/internal/store"
)

//go:embed static templates
var files embed.FS

// MaxUpload is the most one upload may hold.
const MaxUpload = 256 << 20

// Config is how the site runs.
type Config struct {
	Store   *store.Store
	Logs    *logs.Logs
	Version string
}

// Site serves RIOT.
type Site struct {
	cfg  Config
	tmpl *template.Template
}

// New makes the site.
func New(cfg Config) (*Site, error) {
	if cfg.Store == nil {
		return nil, errors.New("no store")
	}
	if cfg.Logs == nil {
		cfg.Logs, _ = logs.Open("", io.Discard)
	}
	tmpl, err := template.ParseFS(files, "templates/*.html")
	if err != nil {
		return nil, err
	}
	return &Site{cfg: cfg, tmpl: tmpl}, nil
}

// Handler is the site's HTTP handler.
func (s *Site) Handler() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(files, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok\n") })
	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("POST /new", s.create)
	mux.HandleFunc("GET /n/{id}", s.withNS(s.page))
	mux.HandleFunc("GET /n/{id}/api", s.withNS(s.info))
	mux.HandleFunc("PATCH /n/{id}/api", s.withNS(s.rename))
	mux.HandleFunc("DELETE /n/{id}/api", s.withNS(s.deleteNS))
	mux.HandleFunc("POST /n/{id}/api/files", s.withNS(s.upload))
	mux.HandleFunc("GET /n/{id}/api/files/{fid}", s.withNS(s.download))
	mux.HandleFunc("PATCH /n/{id}/api/files/{fid}", s.withNS(s.updateFile))
	mux.HandleFunc("DELETE /n/{id}/api/files/{fid}", s.withNS(s.removeFile))
	mux.HandleFunc("GET /n/{id}/api/games/{gid}", s.withNS(s.game))
	mux.HandleFunc("GET /n/{id}/api/stats", s.withNS(s.stats))
	mux.HandleFunc("POST /log", s.clientLog)
	mux.HandleFunc("POST /n/{id}/log", s.clientLog)
	return s.logRequests(securityHeaders(mux))
}

func (s *Site) logf(id, format string, args ...any) { s.cfg.Logs.Printf(id, format, args...) }

// logRequests logs every request with its answer and time, a failed one
// with what it answered, and one about a namespace in its log too.
func (s *Site) logRequests(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &recorder{ResponseWriter: w, status: http.StatusOK}
		h.ServeHTTP(rec, r)
		if r.URL.Path == "/healthz" || strings.HasPrefix(r.URL.Path, "/static/") && rec.status < 400 {
			return
		}
		id := ""
		if m := pathID.FindStringSubmatch(r.URL.Path); m != nil {
			id = m[1]
		}
		msg := fmt.Sprintf("%s %s %d %s %dB", r.Method, r.URL.RequestURI(), rec.status, time.Since(start).Round(time.Millisecond), rec.size)
		if rec.status >= 400 && rec.errBody.Len() > 0 {
			msg += ": " + strings.TrimSpace(rec.errBody.String())
		}
		s.logf(id, "%s", msg)
	})
}

var (
	pathID = regexp.MustCompile(`^/n/([0-9a-f]{10})(?:/|$)`)
	idRe   = regexp.MustCompile(`^[0-9a-f]{10}$`)
)

type recorder struct {
	http.ResponseWriter
	status  int
	size    int
	errBody bytes.Buffer
}

func (r *recorder) WriteHeader(code int) { r.status = code; r.ResponseWriter.WriteHeader(code) }
func (r *recorder) Write(b []byte) (int, error) {
	if r.status >= 400 && r.errBody.Len() < 500 {
		r.errBody.Write(b[:min(len(b), 500-r.errBody.Len())])
	}
	r.size += len(b)
	return r.ResponseWriter.Write(b)
}

// clientLog takes an error the page ran into in the browser, so it is in
// the logs too.
func (s *Site) clientLog(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
	var e struct {
		Message, Source, Stack, Page, Agent string
		Line, Column                        int
	}
	if json.Unmarshal(body, &e) != nil {
		http.Error(w, "bad log", http.StatusBadRequest)
		return
	}
	id := r.PathValue("id")
	if !idRe.MatchString(id) {
		id = ""
	}
	clean := func(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, "\r", " "), "\n", " | ") }
	s.logf(id, "BROWSER ERROR on %s: %s (%s:%d:%d) stack: %s agent: %s", clean(e.Page), clean(e.Message), clean(e.Source),
		e.Line, e.Column, clean(e.Stack), clean(e.Agent))
	w.WriteHeader(http.StatusNoContent)
}

func securityHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; img-src 'self' data:")
		h.ServeHTTP(w, r)
	})
}

// --- pages ---------------------------------------------------------------------------

func (s *Site) home(w http.ResponseWriter, r *http.Request) {
	s.render(w, "home.html", map[string]any{"Error": r.URL.Query().Get("error"), "Version": s.cfg.Version})
}

func (s *Site) render(w http.ResponseWriter, name string, data any) {
	var buf bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		s.logf("", "page %s: %v", name, err)
		http.Error(w, "page error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(buf.Bytes())
}

func (s *Site) create(w http.ResponseWriter, r *http.Request) {
	ns, err := s.cfg.Store.Create(r.FormValue("name"))
	if err != nil {
		http.Redirect(w, r, "/?error="+template.URLQueryEscaper(err.Error()), http.StatusSeeOther)
		return
	}
	s.logf(ns.ID, "namespace %q made", ns.Name)
	http.Redirect(w, r, "/n/"+ns.ID, http.StatusSeeOther)
}

func (s *Site) withNS(f func(http.ResponseWriter, *http.Request, *store.Namespace)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ns, err := s.cfg.Store.Get(r.PathValue("id"))
		switch {
		case errors.Is(err, store.ErrNotFound):
			if strings.Contains(r.URL.Path, "/api") {
				http.Error(w, "no such namespace", http.StatusNotFound)
			} else {
				w.WriteHeader(http.StatusNotFound)
				s.render(w, "home.html", map[string]any{"Error": "There is no namespace at this address. Was it deleted?", "Version": s.cfg.Version})
			}
			return
		case err != nil:
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		f(w, r, ns)
	}
}

func (s *Site) page(w http.ResponseWriter, r *http.Request, ns *store.Namespace) {
	snap := ns.Snapshot()
	s.render(w, "namespace.html", map[string]any{"ID": ns.ID, "Name": snap.Name, "Version": s.cfg.Version})
}

// --- the namespace -------------------------------------------------------------------------

// TournamentInfo is a tournament in the namespace.
type TournamentInfo struct {
	Name       string   `json:"name"`
	Games      int      `json:"games"`      // with a jam played
	Scoreboard int      `json:"scoreboard"` // games with a scoreboard file
	Statsbook  int      `json:"statsbook"`  // games with a statsbook
	Both       int      `json:"both"`
	Dates      []string `json:"dates"`
}

// GameRow is a game in the list.
type GameRow struct {
	ID         string       `json:"id"`
	Tournament string       `json:"tournament"`
	Label      string       `json:"label"`
	Date       string       `json:"date,omitempty"`
	GameNo     string       `json:"gameNo,omitempty"`
	Teams      [2]string    `json:"teams"`
	Score      [2]int       `json:"score"`
	Scoreboard string       `json:"scoreboard,omitempty"` // file id used
	Statsbook  string       `json:"statsbook,omitempty"`
	Compared   bool         `json:"compared"`
	Changes    int          `json:"changes"`
	Versions   []VersionRow `json:"versions"`
}

// VersionRow is one file's copy of a game.
type VersionRow struct {
	File string `json:"file"` // file id
	Name string `json:"name"`
	Kind string `json:"kind"`
	Jams int    `json:"jams"`
	Used bool   `json:"used"`
}

func (s *Site) info(w http.ResponseWriter, r *http.Request, ns *store.Namespace) {
	snap := ns.Snapshot()
	gs := ns.Games()
	tours := map[string]*TournamentInfo{}
	for _, name := range ns.Tournaments() {
		tours[name] = &TournamentInfo{Name: name, Dates: []string{}}
	}
	rows := []*GameRow{}
	for _, g := range gs.List {
		t := tours[g.Tournament]
		if t == nil {
			t = &TournamentInfo{Name: g.Tournament, Dates: []string{}}
			tours[g.Tournament] = t
		}
		t.Games++
		if g.UsedScoreboard != nil {
			t.Scoreboard++
		}
		if g.UsedStatsbook != nil {
			t.Statsbook++
		}
		if g.Compared {
			t.Both++
		}
		if g.Date != "" && !slices.Contains(t.Dates, g.Date) {
			t.Dates = append(t.Dates, g.Date)
		}
		row := &GameRow{ID: g.ID, Tournament: g.Tournament, Label: g.Label, Date: g.Date, GameNo: g.GameNo, Teams: g.Teams,
			Score: g.Score, Compared: g.Compared, Changes: len(g.Changes), Versions: []VersionRow{}}
		if g.UsedScoreboard != nil {
			row.Scoreboard = g.UsedScoreboard.File.ID
		}
		if g.UsedStatsbook != nil {
			row.Statsbook = g.UsedStatsbook.File.ID
		}
		for _, v := range g.Versions {
			row.Versions = append(row.Versions, VersionRow{File: v.File.ID, Name: v.File.Name, Kind: v.File.Kind,
				Jams: v.File.Entries[v.Index].Jams, Used: v == g.UsedScoreboard || v == g.UsedStatsbook})
		}
		rows = append(rows, row)
	}
	list := []*TournamentInfo{}
	for _, t := range tours {
		sort.Strings(t.Dates)
		list = append(list, t)
	}
	sort.Slice(list, func(a, b int) bool {
		x, y := list[a], list[b]
		if (x.Name == store.NoTournament) != (y.Name == store.NoTournament) {
			return y.Name == store.NoTournament
		}
		return cmp.Or(cmp.Compare(first(x.Dates), first(y.Dates)), cmp.Compare(x.Name, y.Name)) < 0
	})
	writeJSON(w, map[string]any{"id": snap.ID, "name": snap.Name, "created": snap.Created, "files": snap.Files,
		"tournaments": list, "games": rows, "noTournament": store.NoTournament})
}

func first(s []string) string {
	if len(s) == 0 {
		return "9999"
	}
	return s[0]
}

func (s *Site) rename(w http.ResponseWriter, r *http.Request, ns *store.Namespace) {
	var body struct{ Name string }
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := ns.Rename(body.Name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.info(w, r, ns)
}

func (s *Site) deleteNS(w http.ResponseWriter, r *http.Request, ns *store.Namespace) {
	if err := s.cfg.Store.Delete(ns.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.logf(ns.ID, "namespace deleted")
	s.cfg.Logs.Remove(ns.ID)
	w.WriteHeader(http.StatusNoContent)
}

// upload takes files (field "files", any number) and the tournament they
// are from (field "tournament"; empty: what each game says).
func (s *Site) upload(w http.ResponseWriter, r *http.Request, ns *store.Namespace) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxUpload)
	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "send the files as multipart/form-data", http.StatusBadRequest)
		return
	}
	tournament := ""
	batch := time.Now()
	type result struct {
		Name  string `json:"name"`
		Error string `json:"error,omitempty"`
		Files int    `json:"files"`
		Games int    `json:"games"`
	}
	results := []result{}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			http.Error(w, "upload: "+err.Error(), http.StatusBadRequest)
			return
		}
		switch part.FormName() {
		case "tournament":
			b, _ := io.ReadAll(io.LimitReader(part, 500))
			tournament = strings.TrimSpace(string(b))
		case "files":
			data, err := io.ReadAll(io.LimitReader(part, store.MaxFile+1))
			name := part.FileName()
			if err != nil {
				http.Error(w, "upload: "+err.Error(), http.StatusBadRequest)
				return
			}
			res := result{Name: name}
			added, err := ns.Add(name, data, tournament, batch)
			if err != nil {
				res.Error = err.Error()
			}
			for _, f := range added {
				res.Files++
				if f.Error != "" && res.Error == "" {
					res.Error = f.Name + ": " + f.Error
				}
				for _, e := range f.Entries {
					if e.Jams > 0 {
						res.Games++
					}
				}
			}
			s.logf(ns.ID, "upload %q (%d bytes, tournament %q): %d files, %d played games %s", name, len(data), tournament, res.Files, res.Games, res.Error)
			results = append(results, res)
		}
		part.Close()
	}
	writeJSON(w, map[string]any{"results": results})
}

func (s *Site) download(w http.ResponseWriter, r *http.Request, ns *store.Namespace) {
	path, name, err := ns.FilePath(r.PathValue("fid"))
	if err != nil {
		http.Error(w, "no such file", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", strings.ReplaceAll(name, `"`, "")))
	http.ServeFile(w, r, path)
}

func (s *Site) updateFile(w http.ResponseWriter, r *http.Request, ns *store.Namespace) {
	var body struct {
		Tournament *string `json:"tournament"`
		Ignored    *bool   `json:"ignored"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := ns.Update(r.PathValue("fid"), body.Tournament, body.Ignored); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	s.info(w, r, ns)
}

func (s *Site) removeFile(w http.ResponseWriter, r *http.Request, ns *store.Namespace) {
	if err := ns.Remove(r.PathValue("fid")); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	s.info(w, r, ns)
}

// game is one game's sources and what its statsbook changed.
func (s *Site) game(w http.ResponseWriter, r *http.Request, ns *store.Namespace) {
	g := ns.Games().ByID[r.PathValue("gid")]
	if g == nil {
		http.Error(w, "no such game", http.StatusNotFound)
		return
	}
	changes := g.Changes
	if changes == nil {
		changes = []*analysis.Change{}
	}
	writeJSON(w, map[string]any{"id": g.ID, "label": g.Label, "tournament": g.Tournament, "compared": g.Compared, "changes": changes})
}

// StatsResponse is the stats of the chosen tournaments' games.
type StatsResponse struct {
	Tournaments []string          `json:"tournaments"`
	Coverage    Coverage          `json:"coverage"`
	Stats       *stats.Tourn      `json:"stats"`
	Timing      *analysis.Timing  `json:"timing"`
	Referees    *analysis.Crews   `json:"referees"`
	NSOs        *analysis.Crews   `json:"nsos"`
	Fixes       *analysis.Fixes   `json:"fixes"`
	GameInfo    map[string]GameTo `json:"gameInfo"` // game id -> tournament, label
}

// Coverage says where the games' data comes from.
type Coverage struct {
	Games      int `json:"games"`
	Scoreboard int `json:"scoreboard"`
	Statsbook  int `json:"statsbook"`
	Both       int `json:"both"`
}

// GameTo is a game's tournament and label.
type GameTo struct {
	Tournament string `json:"tournament"`
	Label      string `json:"label"`
}

// stats: ?t=<tournament> any number of times (none: all); &date=YYYY-MM-DD
// for one day.
func (s *Site) stats(w http.ResponseWriter, r *http.Request, ns *store.Namespace) {
	want := r.URL.Query()["t"]
	date := r.URL.Query().Get("date")
	gs := ns.Games()
	var games []*analysis.Game
	var names []string
	resp := &StatsResponse{Tournaments: []string{}, GameInfo: map[string]GameTo{}}
	for _, g := range gs.List {
		if len(want) > 0 && !slices.Contains(want, g.Tournament) || date != "" && g.Date != date || g.Stats == nil {
			continue
		}
		games = append(games, &g.Game)
		if !slices.Contains(resp.Tournaments, g.Tournament) {
			resp.Tournaments = append(resp.Tournaments, g.Tournament)
		}
		resp.Coverage.Games++
		if g.Scoreboard != nil {
			resp.Coverage.Scoreboard++
		}
		if g.UsedStatsbook != nil {
			resp.Coverage.Statsbook++
		}
		if g.Compared {
			resp.Coverage.Both++
		}
		resp.GameInfo[g.ID] = GameTo{Tournament: g.Tournament, Label: g.Label}
	}
	names = resp.Tournaments
	label := strings.Join(names, " + ")
	if len(want) == 0 {
		label = ns.Snapshot().Name
	}
	resp.Stats = stats.ComputeTournament(label, store.Summaries(games))
	resp.Timing = analysis.ComputeTiming(games, resp.Stats)
	resp.Referees = analysis.ComputeCrews(games, resp.Stats, analysis.KindReferee)
	resp.NSOs = analysis.ComputeCrews(games, resp.Stats, analysis.KindNSO)
	resp.Fixes = analysis.CommonFixes(games)
	writeJSON(w, resp)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
