package store

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	unorm "golang.org/x/text/unicode/norm"

	"crgformat/replay"
	"crgformat/stats"
)

// Teams over games and seasons. A team is its league and team name, as a
// game names it, written loosely: case, accents, punctuation and spaces
// don't count, and a team name that is the league's is no team name. What
// the files still spell differently (a typo, a renamed team) is merged by
// hand into a TeamGroup.

// TeamGroup is spellings of one team merged by hand, and the name to show.
type TeamGroup struct {
	Name    string   `json:"name"`
	Members []string `json:"members"` // team ids
}

// TeamID is a game team's id: its folded league and team name.
func TeamID(t *replay.Team) string {
	league := fold(cmp.Or(t.League, t.TeamName, t.Name))
	team := fold(cmp.Or(t.TeamName, t.Name))
	if team == league {
		team = ""
	}
	return league + "|" + team
}

// fold: lower case, no accents, letters and digits only.
func fold(s string) string {
	var b strings.Builder
	space := false
	for _, r := range unorm.NFD.String(s) {
		switch {
		case unicode.Is(unicode.Mn, r):
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			b.WriteRune(unicode.ToLower(r))
		default:
			space = true
		}
	}
	return b.String()
}

// Spelling is how files wrote a team.
type Spelling struct {
	League string `json:"league"`
	Team   string `json:"team,omitempty"`
	Games  int    `json:"games"`
	Last   string `json:"last,omitempty"` // date of the last game
}

// TeamRef is a team in the namespace: one team id, or a group of them.
type TeamRef struct {
	Key       string      `json:"key"`
	Name      string      `json:"name"`
	League    string      `json:"league"`
	Group     bool        `json:"group"` // merged by hand
	Members   []string    `json:"members"`
	Spellings []*Spelling `json:"spellings"`
	Games     int         `json:"games"`
	First     string      `json:"first,omitempty"`
	Last      string      `json:"last,omitempty"`
	numbers   map[string]bool
	opponents map[string]bool
	lastSeen  string // date of the spelling shown
}

// Suggestion: teams that are probably one, by their skater numbers.
type Suggestion struct {
	Teams []string `json:"teams"` // keys, most games first
}

// TeamList is the namespace's teams, most games first.
type TeamList struct {
	List        []*TeamRef    `json:"teams"`
	Suggestions []*Suggestion `json:"suggestions"`
	byKey       map[string]*TeamRef
	of          map[string]string // team id -> key
}

// Teams lists the teams of the namespace's played games.
func (ns *Namespace) Teams() *TeamList {
	gs := ns.Games()
	ns.mu.Lock()
	groups := slices.Clone(ns.TeamGroups)
	ns.mu.Unlock()
	return teamList(gs, groups)
}

func teamList(gs *Games, groups []*TeamGroup) *TeamList {
	tl := &TeamList{List: []*TeamRef{}, Suggestions: []*Suggestion{}, byKey: map[string]*TeamRef{}, of: map[string]string{}}
	for _, g := range groups {
		ref := &TeamRef{Key: groupKey(g), Name: g.Name, Group: true, Members: slices.Clone(g.Members)}
		tl.byKey[ref.Key] = ref
		for _, m := range g.Members {
			tl.of[m] = ref.Key
		}
	}
	spell := map[string]map[[2]string]*Spelling{}
	for _, g := range gs.List {
		if g.Stats == nil {
			continue
		}
		for i := range 2 {
			id := g.TeamIDs[i]
			if id == "" {
				continue
			}
			key := cmp.Or(tl.of[id], id)
			ref := tl.byKey[key]
			if ref == nil {
				ref = &TeamRef{Key: key, Members: []string{id}}
				tl.byKey[key] = ref
				tl.of[id] = key
			}
			if ref.numbers == nil {
				ref.numbers, ref.opponents = map[string]bool{}, map[string]bool{}
			}
			ref.Games++
			if g.Date != "" && (ref.First == "" || g.Date < ref.First) {
				ref.First = g.Date
			}
			ref.Last = max(ref.Last, g.Date)
			t := g.original[i]
			for _, sk := range t.Skaters {
				if !strings.Contains(" "+sk.Flags+" ", " ALT ") && sk.Status != "notInGame" {
					ref.numbers[sk.Number] = true
				}
			}
			ref.opponents[g.TeamIDs[1-i]] = true
			league := strings.Join(strings.Fields(cmp.Or(t.League, t.TeamName, t.Name)), " ")
			team := strings.Join(strings.Fields(cmp.Or(t.TeamName, t.Name)), " ")
			if fold(team) == fold(league) {
				team = ""
			}
			if spell[key] == nil {
				spell[key] = map[[2]string]*Spelling{}
			}
			sp := spell[key][[2]string{league, team}]
			if sp == nil {
				sp = &Spelling{League: league, Team: team}
				spell[key][[2]string{league, team}] = sp
			}
			sp.Games++
			sp.Last = max(sp.Last, g.Date)
			// The name shown: the latest spelling, of the team when there is one.
			if ref.League == "" || g.Date >= ref.lastSeen {
				ref.League, ref.lastSeen = league, g.Date
				if !ref.Group {
					ref.Name = cmp.Or(team, league)
				}
			}
		}
	}
	for key, ref := range tl.byKey {
		if ref.Games == 0 {
			continue // a group whose games are gone
		}
		for _, sp := range spell[key] {
			ref.Spellings = append(ref.Spellings, sp)
		}
		slices.SortFunc(ref.Spellings, func(a, b *Spelling) int { return cmp.Or(strings.Compare(b.Last, a.Last), b.Games-a.Games) })
		tl.List = append(tl.List, ref)
	}
	slices.SortFunc(tl.List, func(a, b *TeamRef) int {
		return cmp.Or(b.Games-a.Games, strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)))
	})
	// Suggestions: teams of which most of the smaller roster's numbers are
	// in the other's, and that never played each other; linked pairs make
	// one suggestion.
	parent, linked := map[string]string{}, map[string]bool{}
	var find func(k string) string
	find = func(k string) string {
		if p, ok := parent[k]; ok && p != k {
			parent[k] = find(p)
			return parent[k]
		}
		return k
	}
	for i, a := range tl.List {
		for _, b := range tl.List[i+1:] {
			if !a.played(b) && min(len(a.numbers), len(b.numbers)) >= 5 && overlap(a.numbers, b.numbers) >= 0.5 {
				parent[find(b.Key)] = find(a.Key)
				linked[a.Key], linked[b.Key] = true, true
			}
		}
	}
	byRoot := map[string]*Suggestion{}
	for _, t := range tl.List {
		if !linked[t.Key] {
			continue
		}
		r := find(t.Key)
		if byRoot[r] == nil {
			byRoot[r] = &Suggestion{}
			tl.Suggestions = append(tl.Suggestions, byRoot[r])
		}
		byRoot[r].Teams = append(byRoot[r].Teams, t.Key)
	}
	return tl
}

func (a *TeamRef) played(b *TeamRef) bool {
	for _, m := range b.Members {
		if a.opponents[m] {
			return true
		}
	}
	return false
}

func groupKey(g *TeamGroup) string { return "g:" + slices.Min(g.Members) }

// Key is the team a team id belongs to.
func (tl *TeamList) Key(id string) string { return cmp.Or(tl.of[id], id) }

// Get is a team by its key.
func (tl *TeamList) Get(key string) *TeamRef { return tl.byKey[key] }

// SetTeamGroups replaces the merged teams. A team id can be in one group;
// a group needs a name and at least one member.
func (ns *Namespace) SetTeamGroups(groups []*TeamGroup) error {
	seen := map[string]bool{}
	var out []*TeamGroup
	for _, g := range groups {
		g.Name = strings.Join(strings.Fields(g.Name), " ")
		if g.Name == "" {
			return errors.New("a merged team needs a name")
		}
		var ms []string
		for _, m := range g.Members {
			if m == "" || !strings.Contains(m, "|") {
				return fmt.Errorf("%q is not a team", m)
			}
			if seen[m] {
				return fmt.Errorf("%q is in two merged teams", m)
			}
			seen[m] = true
			ms = append(ms, m)
		}
		if len(ms) == 0 {
			continue
		}
		out = append(out, &TeamGroup{Name: g.Name, Members: ms})
	}
	ns.mu.Lock()
	defer ns.mu.Unlock()
	ns.TeamGroups = out
	ns.games = nil
	return ns.save()
}

// --- seasons ---------------------------------------------------------------

// Kinds of season.
const (
	SeasonClub  = "club"  // August to July
	SeasonWFTDA = "wftda" // two years from June, the first from 1 June 2024
)

// Season is a team's games in one season.
type Season struct {
	Label   string             `json:"label"`
	From    string             `json:"from,omitempty"`
	To      string             `json:"to"`
	Team    *stats.TeamTotals  `json:"team"`
	Skaters []*stats.SkaterTot `json:"skaters"`
	Games   []*SeasonGame      `json:"games"`
}

// SeasonGame is one of the team's games, from the team's side.
type SeasonGame struct {
	ID         string `json:"id"`
	Date       string `json:"date"`
	Tournament string `json:"tournament"`
	Opponent   string `json:"opponent"`
	OppKey     string `json:"opponentKey"`
	For        int    `json:"for"`
	Against    int    `json:"against"`
	Penalties  int    `json:"penalties"`
}

// SeasonOf is the season a date (YYYY-MM-DD) is in.
func SeasonOf(kind, date string) (label, from, to string, ok bool) {
	d, err := time.Parse("2006-01-02", date)
	if err != nil {
		return "", "", "", false
	}
	y, m := d.Year(), int(d.Month())
	if kind == SeasonWFTDA {
		if date < "2024-06-01" {
			return "Before June 2024", "", "2024-05-31", true
		}
		start := 2024 + 2*(((y-2024)*12+m-6)/24)
		return fmt.Sprintf("%d–%d", start, start+2), fmt.Sprintf("%d-06-01", start), fmt.Sprintf("%d-05-31", start+2), true
	}
	start := y
	if m < 8 {
		start--
	}
	return fmt.Sprintf("%d/%02d", start, (start+1)%100), fmt.Sprintf("%d-08-01", start), fmt.Sprintf("%d-07-31", start+1), true
}

// Seasons are a team's seasons, newest first, and its games without a date.
func (ns *Namespace) Seasons(key, kind string) (seasons []*Season, undated []*SeasonGame, err error) {
	gs := ns.Games()
	ns.mu.Lock()
	groups := slices.Clone(ns.TeamGroups)
	ns.mu.Unlock()
	tl := teamList(gs, groups)
	if tl.Get(key) == nil {
		return nil, nil, ErrNotFound
	}
	bySeason := map[string]*Season{}
	games := map[string][]*GameInfo{}
	for _, g := range gs.List {
		if g.Stats == nil || tl.Key(g.TeamIDs[0]) != key && tl.Key(g.TeamIDs[1]) != key {
			continue
		}
		label, from, to, ok := SeasonOf(kind, g.Date)
		if !ok {
			undated = append(undated, seasonGame(g, key, tl, nil))
			continue
		}
		if bySeason[label] == nil {
			bySeason[label] = &Season{Label: label, From: from, To: to}
			seasons = append(seasons, bySeason[label])
		}
		games[label] = append(games[label], g)
	}
	slices.SortFunc(seasons, func(a, b *Season) int { return strings.Compare(b.To, a.To) })
	for _, s := range seasons {
		ids := map[*replay.Summary]*GameInfo{}
		var sums []*replay.Summary
		for _, g := range games[s.Label] {
			ids[g.Stats] = g
			sums = append(sums, g.Stats)
		}
		t := stats.ComputeTournamentWith(s.Label, sums, func(sum *replay.Summary, i int) stats.TeamID {
			k := tl.Key(ids[sum].TeamIDs[i])
			ref := tl.Get(k)
			return stats.TeamID{Key: k, Name: ref.Name, League: ref.League}
		})
		s.Skaters = []*stats.SkaterTot{}
		for _, tt := range t.Teams {
			if tt.Key == key {
				s.Team = tt
			}
		}
		for _, sk := range t.Skaters {
			if sk.Team == key {
				s.Skaters = append(s.Skaters, sk)
			}
		}
		res := map[string]*stats.GameResult{}
		for _, r := range t.Games {
			res[r.ID] = r
		}
		for _, g := range games[s.Label] {
			s.Games = append(s.Games, seasonGame(g, key, tl, res[g.ID]))
		}
		slices.SortFunc(s.Games, func(a, b *SeasonGame) int { return strings.Compare(a.Date, b.Date) })
	}
	return seasons, undated, nil
}

func seasonGame(g *GameInfo, key string, tl *TeamList, r *stats.GameResult) *SeasonGame {
	us := 0
	if tl.Key(g.TeamIDs[0]) != key {
		us = 1
	}
	opp := tl.Key(g.TeamIDs[1-us])
	sg := &SeasonGame{ID: g.ID, Date: g.Date, Tournament: g.Tournament, OppKey: opp, For: g.Score[us], Against: g.Score[1-us]}
	if ref := tl.Get(opp); ref != nil {
		sg.Opponent = ref.Name
	}
	if r != nil {
		sg.Penalties = r.Penalty[us]
	}
	return sg
}

// mergedTeam is the name a merged team's games get.
type mergedTeam struct{ Name, League string }

// mergedTeams: per team id in a group, the group's name and the league as
// its latest game wrote it (ns.mu held).
func (ns *Namespace) mergedTeams(games []*GameInfo) map[string]*mergedTeam {
	out := map[string]*mergedTeam{}
	for _, grp := range ns.TeamGroups {
		mt := &mergedTeam{Name: grp.Name}
		for _, m := range grp.Members {
			out[m] = mt
		}
	}
	latest := map[*mergedTeam]string{}
	for _, g := range games {
		for i, t := range g.original {
			if mt := out[g.TeamIDs[i]]; mt != nil && t != nil && (mt.League == "" || g.Date >= latest[mt]) {
				mt.League, latest[mt] = strings.Join(strings.Fields(cmp.Or(t.League, t.TeamName, t.Name)), " "), g.Date
			}
		}
	}
	return out
}
