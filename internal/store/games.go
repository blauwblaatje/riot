package store

import (
	"cmp"
	"crypto/sha1"
	"encoding/hex"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"crgformat/derive"
	"crgformat/replay"

	"rats/internal/analysis"
)

// Games is the namespace's files grouped into games.
type Games struct {
	List []*GameInfo
	ByID map[string]*GameInfo
}

// GameInfo is a game and where it comes from.
type GameInfo struct {
	analysis.Game
	Date     string    `json:"date,omitempty"`
	GameNo   string    `json:"gameNo,omitempty"`
	Teams    [2]string `json:"teams"`
	Score    [2]int    `json:"score"`
	Versions []*Version
	// The versions used: the scoreboard file with the most in it, and the
	// statsbook uploaded last (nil if none).
	UsedScoreboard *Version
	UsedStatsbook  *Version
}

// Version is one file's copy of a game.
type Version struct {
	File    *File
	Index   int // entry in the file
	Summary *replay.Summary
	order   [3]int64 // upload batch, then "(n)" in the name, then file order
}

func newer(a, b *Version) bool {
	for i := range a.order {
		if a.order[i] != b.order[i] {
			return a.order[i] > b.order[i]
		}
	}
	return false
}

// Games groups the files' games (cached until a file changes).
func (ns *Namespace) Games() *Games {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	if ns.games != nil {
		return ns.games
	}
	var versions []*Version
	for fi, f := range ns.Files {
		if f.Ignored || f.Error != "" {
			continue
		}
		sums := ns.summariesOf(f)
		for i := range f.Entries {
			if i < len(sums) && sums[i] != nil {
				versions = append(versions, &Version{File: f, Index: i, Summary: sums[i], order: [3]int64{f.Uploaded.UnixMilli(), int64(nameOrder(f.Name)), int64(fi)}})
			}
		}
	}
	gs := &Games{ByID: map[string]*GameInfo{}}
	for _, v := range versions {
		e := v.File.Entries[v.Index]
		t := v.File.TournamentOf(e)
		var game *GameInfo
		for _, g := range gs.List {
			if g.Tournament == t && sameGame(g, e, v.Summary) {
				game = g
				break
			}
		}
		if game == nil {
			game = &GameInfo{Game: analysis.Game{Tournament: t}, Date: e.Date, GameNo: e.GameNo, Teams: e.Teams}
			gs.List = append(gs.List, game)
		}
		game.Versions = append(game.Versions, v)
		game.Date, game.GameNo = cmp.Or(game.Date, e.Date), cmp.Or(game.GameNo, e.GameNo)
	}
	for _, g := range gs.List {
		for _, v := range g.Versions {
			switch v.File.Kind {
			case KindScoreboard:
				if u := g.UsedScoreboard; u == nil || size(v.Summary) > size(u.Summary) ||
					size(v.Summary) == size(u.Summary) && newer(v, u) {
					g.UsedScoreboard = v
				}
			case KindStatsbook:
				if u := g.UsedStatsbook; u == nil || newer(v, u) {
					g.UsedStatsbook = v
				}
			}
		}
		if g.UsedScoreboard != nil {
			g.Scoreboard = g.UsedScoreboard.Summary
			g.Stats = g.Scoreboard
		}
		if g.UsedStatsbook != nil {
			g.Stats = g.UsedStatsbook.Summary
			e := g.UsedStatsbook.File.Entries[g.UsedStatsbook.Index]
			g.Teams = e.Teams
		}
		if g.Scoreboard != nil && g.UsedStatsbook != nil {
			g.Changes = analysis.Compare(g.Scoreboard, g.UsedStatsbook.Summary)
			g.Compared = true
		}
		g.Score = derive.Scores(g.Stats)
		h := sha1.Sum([]byte(g.Tournament + "|" + g.Date + "|" + g.GameNo + "|" + g.Teams[0] + "|" + g.Teams[1]))
		g.ID = hex.EncodeToString(h[:5])
		for gs.ByID[g.ID] != nil {
			g.ID += "x"
		}
		g.Label = label(g)
		gs.ByID[g.ID] = g
		// The stats name games by summary id: shallow copies with the game's.
		for _, sum := range []**replay.Summary{&g.Stats, &g.Scoreboard} {
			if *sum != nil {
				c := **sum
				c.ID = g.ID
				*sum = &c
			}
		}
	}
	sort.SliceStable(gs.List, func(a, b int) bool {
		x, y := gs.List[a], gs.List[b]
		return cmp.Or(cmp.Compare(x.Tournament, y.Tournament), cmp.Compare(x.Date, y.Date), cmp.Compare(num(x.GameNo), num(y.GameNo))) < 0
	})
	ns.games = gs
	return gs
}

func label(g *GameInfo) string {
	var b strings.Builder
	if g.Date != "" {
		b.WriteString(g.Date + " ")
	}
	if g.GameNo != "" {
		b.WriteString("#" + g.GameNo + " ")
	}
	b.WriteString(g.Teams[0] + " – " + g.Teams[1])
	return b.String()
}

// size is how much of a game a summary has.
func size(s *replay.Summary) int {
	n := len(s.Penalties)
	for _, p := range s.Periods {
		for _, j := range p.Jams {
			n += 2
			for _, tj := range j.Teams {
				n += len(tj.Trips) + len(tj.Lineup)
			}
		}
	}
	return n
}

// sameGame: a game of the same tournament on the same day with the same
// game number, or, when a game number is missing, the same teams (by name
// or by their skaters' numbers).
func sameGame(g *GameInfo, e *Entry, s *replay.Summary) bool {
	if g.Date != "" && e.Date != "" && g.Date != e.Date {
		return false
	}
	if g.GameNo != "" && e.GameNo != "" {
		return g.GameNo == e.GameNo
	}
	if norm(g.Teams[0]) == norm(e.Teams[0]) && norm(g.Teams[1]) == norm(e.Teams[1]) {
		return true
	}
	other := g.Versions[0].Summary
	return len(s.Teams) == 2 && len(other.Teams) == 2 &&
		overlap(numbers(s.Teams[0]), numbers(other.Teams[0])) >= 0.6 && overlap(numbers(s.Teams[1]), numbers(other.Teams[1])) >= 0.6
}

func norm(s string) string { return strings.Join(strings.Fields(strings.ToLower(s)), " ") }

func numbers(t *replay.Team) map[string]bool {
	m := map[string]bool{}
	for _, sk := range t.Skaters {
		m[sk.Number] = true
	}
	return m
}

func overlap(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	n := 0
	for k := range a {
		if b[k] {
			n++
		}
	}
	return float64(n) / float64(min(len(a), len(b)))
}

func num(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n
}

var copyRe = regexp.MustCompile(`[ _]?\((\d+)\)|_(\d+)$`)

// nameOrder: of files uploaded together, a browser's "name (2).xlsx" is
// newer than "name (1).xlsx", which is newer than "name.xlsx".
func nameOrder(name string) int {
	base := strings.TrimSuffix(name, path.Ext(name))
	m := copyRe.FindStringSubmatch(base)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(cmp.Or(m[1], m[2]))
	return min(n, 999)
}
