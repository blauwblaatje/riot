package web

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rats/internal/store"
)

func newSite(t *testing.T) *httptest.Server {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	site, err := New(Config{Store: st})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(site.Handler())
	t.Cleanup(srv.Close)
	return srv
}

func noRedirect() *http.Client {
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// create makes a namespace and returns its path, /n/<id>.
func create(t *testing.T, srv *httptest.Server, name string) string {
	t.Helper()
	resp, err := noRedirect().PostForm(srv.URL+"/new", map[string][]string{"name": {name}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	if resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(loc, "/n/") {
		t.Fatalf("new: %d %q", resp.StatusCode, loc)
	}
	return loc
}

func upload(t *testing.T, url, tournament string, files ...string) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("tournament", tournament)
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Skip("test file missing:", err)
		}
		w, _ := mw.CreateFormFile("files", filepath.Base(f))
		w.Write(data)
	}
	mw.Close()
	resp, err := http.Post(url, mw.FormDataContentType(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	return decode(t, resp)
}

func decode(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s: %d %s", resp.Request.URL, resp.StatusCode, body)
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("%s: %v", resp.Request.URL, err)
	}
	return out
}

func get(t *testing.T, url string) map[string]any {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	return decode(t, resp)
}

func TestPages(t *testing.T) {
	srv := newSite(t)
	for path, want := range map[string]int{"/": 200, "/healthz": 200, "/static/app.js": 200, "/n/0123456789": 404, "/n/nope/api": 404} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("%s: %d, want %d", path, resp.StatusCode, want)
		}
	}
	resp, _ := noRedirect().PostForm(srv.URL+"/new", map[string][]string{"name": {"  "}})
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "error=") {
		t.Errorf("an empty name made a namespace: %q", loc)
	}
	ns := create(t, srv, "Test")
	resp, _ = http.Get(srv.URL + ns)
	page, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(page), "<title>Test · RATS</title>") {
		t.Errorf("namespace page: %.300s", page)
	}
}

// A game's scoreboard file and its corrected statsbook (MRDWC 2026 game 18,
// Italy – Japan) end up as one game, compared; a file with a tournament
// set counts for that tournament.
func TestUploadAndStats(t *testing.T) {
	srv := newSite(t)
	ns := create(t, srv, "MRDWC")
	game := "../../../old-games/crg-game-2026-04-30_Game_18__Team_Italy_vs._Team_Japan_Roller_Derby_(Finished__222_-_2).json"
	sb := "../../../old-games/STATS-2026-04-30_TeamItaly_vs_TeamJapanRollerDerby.xlsx"
	res := upload(t, srv.URL+ns+"/api/files", "", game, sb)
	if rs := res["results"].([]any); len(rs) != 2 {
		t.Fatalf("results %v", rs)
	}
	info := get(t, srv.URL+ns+"/api")
	games := info["games"].([]any)
	if len(games) != 1 {
		t.Fatalf("%d games, want 1: %v", len(games), games)
	}
	g := games[0].(map[string]any)
	if g["tournament"] != "MRDWC 2026" || g["compared"] != true || g["changes"].(float64) == 0 {
		t.Errorf("game %v", g)
	}
	st := get(t, srv.URL+ns+"/api/stats")
	if c := st["coverage"].(map[string]any); c["both"].(float64) != 1 || c["games"].(float64) != 1 {
		t.Errorf("coverage %v", c)
	}
	if f := st["fixes"].(map[string]any); f["compared"].(float64) != 1 {
		t.Errorf("fixes %v", f)
	}
	if tm := st["timing"].(map[string]any); tm["games"].(float64) != 1 {
		t.Errorf("timing games %v", tm["games"])
	}
	detail := get(t, srv.URL+ns+"/api/games/"+g["id"].(string))
	if len(detail["changes"].([]any)) == 0 {
		t.Errorf("no changes in %v", detail)
	}

	// Put the statsbook in another tournament: now two games, none compared.
	files := info["files"].([]any)
	var sbID string
	for _, f := range files {
		if f.(map[string]any)["kind"] == "statsbook" {
			sbID = f.(map[string]any)["id"].(string)
		}
	}
	req, _ := http.NewRequest("PATCH", srv.URL+ns+"/api/files/"+sbID, strings.NewReader(`{"tournament":"Elsewhere"}`))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	info = decode(t, resp)
	if n := len(info["games"].([]any)); n != 2 {
		t.Errorf("after moving the statsbook: %d games", n)
	}
	st = get(t, srv.URL+ns+"/api/stats?t=Elsewhere")
	if c := st["coverage"].(map[string]any); c["games"].(float64) != 1 || c["scoreboard"].(float64) != 0 {
		t.Errorf("Elsewhere coverage %v", c)
	}
	if tm := st["timing"].(map[string]any); tm["games"].(float64) != 0 {
		t.Errorf("a statsbook-only selection has times: %v", tm["games"])
	}

	// Delete it.
	req, _ = http.NewRequest("DELETE", srv.URL+ns+"/api/files/"+sbID, nil)
	resp, _ = http.DefaultClient.Do(req)
	info = decode(t, resp)
	if n := len(info["files"].([]any)); n != 1 {
		t.Errorf("%d files after delete", n)
	}
	req, _ = http.NewRequest("DELETE", srv.URL+ns+"/api", nil)
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp, _ := http.Get(srv.URL + ns + "/api"); resp.StatusCode != 404 {
		t.Errorf("deleted namespace still there: %d", resp.StatusCode)
	}
}

func TestUploadJunk(t *testing.T) {
	srv := newSite(t)
	ns := create(t, srv, "Junk")
	tmp := filepath.Join(t.TempDir(), "notes.txt")
	os.WriteFile(tmp, []byte("hello"), 0o644)
	res := upload(t, srv.URL+ns+"/api/files", "", tmp)
	r := res["results"].([]any)[0].(map[string]any)
	if r["error"] == nil {
		t.Errorf("junk accepted: %v", r)
	}
}
