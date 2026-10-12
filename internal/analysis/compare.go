// Package analysis works out what RATS shows beyond the tournament stats of
// crgformat/stats: what a corrected statsbook changed compared with the
// scoreboard's own game file (Compare), the officiating crews and their
// penalty calls (Crews), and the stats that only a scoreboard file has,
// because a statsbook records no times (Timing).
package analysis

import (
	"cmp"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"crgformat/derive"
	"crgformat/replay"
)

// Areas of the paperwork, roughly by the NSO position that fills them in.
const (
	AreaScore     = "Score"     // scorekeepers: trips, lead, call, star pass
	AreaLineups   = "Lineups"   // lineup trackers: who skated where
	AreaBox       = "Box"       // lineup trackers / penalty box: box symbols
	AreaPenalties = "Penalties" // penalty trackers
	AreaJams      = "Jams"      // jams only in one of the two
	AreaRoster    = "Roster"    // the IGRF's skaters
	AreaOfficials = "Officials" // the IGRF's officials
)

// Change is one thing the statsbook shows differently from the scoreboard
// file: From is the scoreboard's value, To the statsbook's ("" when absent).
type Change struct {
	Area   string `json:"area"`
	Kind   string `json:"kind"`             // e.g. "penalty code changed", "box symbol changed"
	Where  string `json:"where,omitempty"`  // "P1 J3", or "" for the whole game
	Team   string `json:"team,omitempty"`   // "1" or "2"
	Skater string `json:"skater,omitempty"` // number
	From   string `json:"from,omitempty"`
	To     string `json:"to,omitempty"`
}

// Pattern is what the change was, for counting common fixes: "B → F" for a
// penalty code, "- → $" for a box symbol, "+1" for trip points.
func (c *Change) Pattern() string {
	switch c.Kind {
	case "trip points changed", "OS offset changed":
		a, _ := strconv.Atoi(c.From)
		b, _ := strconv.Atoi(c.To)
		return fmt.Sprintf("%+d", b-a)
	case "penalty code changed", "box symbols changed", "position changed", "skater flags changed":
		return or(c.From, "none") + " → " + or(c.To, "none")
	case "penalty moved":
		return c.From + " → " + c.To
	}
	return ""
}

func or(s, def string) string { return cmp.Or(s, def) }

// --- the paperwork of one game -------------------------------------------------------

// teamJam is what a statsbook holds for one team in one jam.
type teamJam struct {
	flags    map[string]bool
	trips    []tripPoints
	osOffset int
	pos      map[string]string // skater number -> position (Jammer, Pivot, Blocker); "n/a" key count for unfielded
	unfilled int               // positions marked not fielded
	symbols  map[string]string // skater number -> box symbols, "- SP:S"
}

type tripPoints struct {
	name   string // "trip 2", "trip 2 (after SP)"
	points int
}

type penaltyFact struct {
	team, skater, jam, code string
	jamIndex                int
}

type paper struct {
	jams      map[string]*[2]*teamJam // "P1 J3"
	jamOrder  []string
	jamIndex  map[string]int
	injury    map[string]bool
	penalties []penaltyFact
	expelled  map[string]string    // "T1 #23" -> code
	roster    [2]map[string]string // number -> name
	flags     [2]map[string]string // number -> flags (C, A, ALT, ...)
	officials map[string]string    // normalised name -> role(s), without head
	hr, hnso  string               // normalised names
}

func paperOf(s *replay.Summary) *paper {
	g := derive.New(s)
	syms := g.BoxSymbols()
	p := &paper{jams: map[string]*[2]*teamJam{}, jamIndex: map[string]int{}, injury: map[string]bool{},
		expelled: map[string]string{}, officials: map[string]string{}}
	for _, jp := range g.Jams {
		if jp.Upcoming {
			continue
		}
		j := jp.Jam
		key := fmt.Sprintf("P%d J%d", jp.Period, j.Number)
		if _, dup := p.jams[key]; dup {
			continue
		}
		p.jamIndex[key] = len(p.jamOrder)
		p.jamOrder = append(p.jamOrder, key)
		if j.EndReason == "injury" {
			p.injury[key] = true
		}
		pair := &[2]*teamJam{}
		p.jams[key] = pair
		for _, tj := range j.Teams {
			i := derive.TeamIndex(tj.Team)
			if i < 0 || i > 1 {
				continue
			}
			t := &teamJam{flags: map[string]bool{}, pos: map[string]string{}, symbols: map[string]string{}, osOffset: tj.OsOffset}
			pair[i] = t
			for name, on := range map[string]bool{"lead": tj.Lead, "lost": tj.Lost, "call": tj.Calloff, "no pivot": tj.NoPivot,
				"star pass": tj.StarPassTrip != ""} {
				if on {
					t.flags[name] = true
				}
			}
			ni, niSP := derive.NI(tj)
			t.flags["NI"] = ni
			t.flags["NI after star pass"] = niSP
			for k, tr := range tj.Trips {
				if k == 0 && tr.Points == 0 {
					continue
				}
				name := "trip " + strconv.Itoa(k+1)
				if tr.AfterStarPass {
					name += " (after SP)"
				}
				t.trips = append(t.trips, tripPoints{name, tr.Points})
			}
			for _, pos := range derive.Positions {
				f := tj.Lineup[pos]
				switch {
				case f == nil:
				case f.NotFielded:
					t.unfilled++
				case f.Skater != "":
					num := g.Number(f.Skater)
					t.pos[num] = strings.TrimRight(pos, "123")
					if sym := syms[derive.FieldingKey(j.ID, tj.Team, pos)]; sym != nil {
						sy := slices.Clone(sym.BeforeSP)
						for _, x := range sym.AfterSP {
							sy = append(sy, "SP:"+x)
						}
						if len(sy) > 0 {
							t.symbols[num] = strings.Join(sy, " ")
						}
					}
				}
			}
		}
	}
	for _, pen := range s.Penalties {
		jp := g.JamByID[pen.Jam]
		where, idx := "", -1
		if jp != nil && !jp.Upcoming {
			where = fmt.Sprintf("P%d J%d", jp.Period, jp.Jam.Number)
			idx = p.jamIndex[where]
		}
		p.penalties = append(p.penalties, penaltyFact{team: pen.Team, skater: g.Number(pen.Skater), jam: where, code: pen.Code, jamIndex: idx})
	}
	for _, e := range s.Expulsions {
		if pen := g.Penalties[e.Penalty]; pen != nil {
			p.expelled["T"+pen.Team+" #"+g.Number(pen.Skater)] = pen.Code
		}
	}
	for i := 0; i < 2 && i < len(s.Teams); i++ {
		p.roster[i], p.flags[i] = map[string]string{}, map[string]string{}
		for _, sk := range s.Teams[i].Skaters {
			p.roster[i][sk.Number] = strings.TrimSpace(sk.Name)
			p.flags[i][sk.Number] = skaterFlags(sk)
		}
	}
	hr, hnso := derive.Heads(s)
	for _, o := range s.Officials {
		n := NormName(o.Name)
		switch o.ID {
		case hr:
			p.hr = n
		case hnso:
			p.hnso = n
		}
		if n == "" {
			continue
		}
		if o.Role == "Head Referee" || o.Role == "Head Non-Skating Official" {
			if _, ok := p.officials[n]; !ok {
				p.officials[n] = "" // the head's role is compared on its own
			}
			continue
		}
		if r := p.officials[n]; r != "" && !strings.Contains(r, o.Role) {
			p.officials[n] = r + ", " + o.Role
		} else if r == "" {
			p.officials[n] = o.Role
		}
	}
	return p
}

// skaterFlags is how the roster marks a skater: captain, alternate, not
// skating (a Java game's alternates are also not in the game).
func skaterFlags(sk *replay.Skater) string {
	var f []string
	for _, x := range strings.Fields(sk.Flags) {
		if x != "ALT" {
			f = append(f, x)
		}
	}
	if strings.Contains(" "+sk.Flags+" ", " ALT ") || sk.Status == "notInGame" {
		f = append(f, "not skating")
	}
	sort.Strings(f)
	return strings.Join(f, " ")
}

// NormName normalises an official's or skater's name for comparing:
// lower case, single spaces, no tabs.
func NormName(s string) string { return strings.Join(strings.Fields(strings.ToLower(s)), " ") }

// --- comparing -----------------------------------------------------------------------

// Compare lists what the statsbook (sb) shows differently from the
// scoreboard's game file (game). Skaters are compared by number, after
// renumbering a skater whose number the statsbook changed; blockers are
// compared as a set (the lineup sheet's blocker order means nothing).
func Compare(game, sb *replay.Summary) []*Change {
	a, b := paperOf(game), paperOf(sb)
	var out []*Change
	add := func(c *Change) { out = append(out, c) }

	// Roster: by name first, so a renumbered skater is one change, not one
	// per jam they skated.
	renumber := [2]map[string]string{{}, {}} // game number -> statsbook number
	for i := 0; i < 2; i++ {
		ra, rb := a.roster[i], b.roster[i]
		if ra == nil || rb == nil {
			continue
		}
		byName := map[string]string{}
		for num, name := range rb {
			if name != "" {
				byName[NormName(name)] = num
			}
		}
		team := strconv.Itoa(i + 1)
		for num, name := range ra {
			if _, ok := rb[num]; ok {
				if nb := rb[num]; NormName(nb) != NormName(name) && name != "" && nb != "" {
					add(&Change{Area: AreaRoster, Kind: "skater name changed", Team: team, Skater: num, From: name, To: nb})
				}
				if fa, fb := a.flags[i][num], b.flags[i][num]; fa != fb {
					add(&Change{Area: AreaRoster, Kind: "skater flags changed", Team: team, Skater: num, From: fa, To: fb})
				}
				continue
			}
			if other, ok := byName[NormName(name)]; ok && name != "" {
				if _, taken := ra[other]; !taken {
					renumber[i][num] = other
					add(&Change{Area: AreaRoster, Kind: "skater number changed", Team: team, Skater: other, From: num, To: other})
					continue
				}
			}
			add(&Change{Area: AreaRoster, Kind: "skater removed", Team: team, Skater: num, From: name})
		}
		for num, name := range rb {
			if _, ok := ra[num]; ok {
				continue
			}
			if slices.Contains(mapValues(renumber[i]), num) {
				continue
			}
			add(&Change{Area: AreaRoster, Kind: "skater added", Team: team, Skater: num, To: name})
		}
	}
	num := func(team int, n string) string {
		if m, ok := renumber[team][n]; ok {
			return m
		}
		return n
	}

	// Officials.
	if a.hr != b.hr {
		add(&Change{Area: AreaOfficials, Kind: "head referee changed", From: a.hr, To: b.hr})
	}
	if a.hnso != b.hnso {
		add(&Change{Area: AreaOfficials, Kind: "head NSO changed", From: a.hnso, To: b.hnso})
	}
	for n, role := range a.officials {
		if rb, ok := b.officials[n]; !ok {
			add(&Change{Area: AreaOfficials, Kind: "official removed", From: strings.TrimSuffix(n+" ("+role+")", " ()")})
		} else if rb != role && rb != "" && role != "" {
			add(&Change{Area: AreaOfficials, Kind: "official role changed", Skater: "", From: n + ": " + role, To: rb})
		}
	}
	for n, role := range b.officials {
		if _, ok := a.officials[n]; !ok {
			add(&Change{Area: AreaOfficials, Kind: "official added", To: strings.TrimSuffix(n+" ("+role+")", " ()")})
		}
	}

	// Jams. A statsbook saved during the game misses the last jams: one
	// change, not one per jam.
	trailing := 0
	for k := len(a.jamOrder) - 1; k >= 0 && b.jams[a.jamOrder[k]] == nil; k-- {
		trailing++
	}
	if trailing > 1 && trailing < len(a.jamOrder) {
		from := a.jamOrder[len(a.jamOrder)-trailing]
		add(&Change{Area: AreaJams, Kind: "statsbook stops early", Where: from, From: strconv.Itoa(trailing) + " jams missing"})
	}
	for k, key := range a.jamOrder {
		if b.jams[key] == nil && (trailing <= 1 || k < len(a.jamOrder)-trailing) {
			add(&Change{Area: AreaJams, Kind: "jam removed", Where: key})
		}
	}
	for _, key := range b.jamOrder {
		if a.jams[key] == nil {
			add(&Change{Area: AreaJams, Kind: "jam added", Where: key})
		}
	}
	for _, key := range a.jamOrder {
		pb := b.jams[key]
		if pb == nil {
			continue
		}
		pa := a.jams[key]
		if a.injury[key] != b.injury[key] {
			add(&Change{Area: AreaScore, Kind: flagKind("injury", b.injury[key]), Where: key})
		}
		for i := 0; i < 2; i++ {
			ta, tb := pa[i], pb[i]
			if ta == nil || tb == nil {
				continue
			}
			team := strconv.Itoa(i + 1)
			compareTeamJam(key, team, i, ta, tb, num, add)
		}
	}

	// Penalties: the same penalty, in that order of likeliness, with another
	// code, skater or jam; the rest added or removed.
	// Penalties in a jam only one of them has are part of that change.
	pa := make([]penaltyFact, 0, len(a.penalties))
	for _, p := range a.penalties {
		if p.jam != "" && b.jams[p.jam] == nil {
			continue
		}
		if i := derive.TeamIndex(p.team); i == 0 || i == 1 {
			p.skater = num(i, p.skater)
		}
		pa = append(pa, p)
	}
	var pb []penaltyFact
	for _, p := range b.penalties {
		if p.jam == "" || a.jams[p.jam] != nil {
			pb = append(pb, p)
		}
	}
	match := func(same func(x, y penaltyFact) bool, change func(x, y penaltyFact)) {
		for ia := 0; ia < len(pa); ia++ {
			for ib := 0; ib < len(pb); ib++ {
				if same(pa[ia], pb[ib]) {
					if change != nil {
						change(pa[ia], pb[ib])
					}
					pa = slices.Delete(pa, ia, ia+1)
					pb = slices.Delete(pb, ib, ib+1)
					ia--
					break
				}
			}
		}
	}
	match(func(x, y penaltyFact) bool { return x == y }, nil)
	match(func(x, y penaltyFact) bool { return x.team == y.team && x.skater == y.skater && x.jam == y.jam },
		func(x, y penaltyFact) {
			add(&Change{Area: AreaPenalties, Kind: "penalty code changed", Where: x.jam, Team: x.team, Skater: x.skater, From: x.code, To: y.code})
		})
	match(func(x, y penaltyFact) bool { return x.team == y.team && x.code == y.code && x.jam == y.jam },
		func(x, y penaltyFact) {
			add(&Change{Area: AreaPenalties, Kind: "penalty skater changed", Where: x.jam, Team: x.team, Skater: y.skater, From: "#" + x.skater, To: "#" + y.skater})
		})
	match(func(x, y penaltyFact) bool { return x.team == y.team && x.skater == y.skater && x.code == y.code },
		func(x, y penaltyFact) {
			add(&Change{Area: AreaPenalties, Kind: "penalty moved", Where: y.jam, Team: x.team, Skater: x.skater,
				From: or(x.jam, "no jam"), To: or(y.jam, "no jam")})
		})
	for _, p := range pa {
		add(&Change{Area: AreaPenalties, Kind: "penalty removed", Where: p.jam, Team: p.team, Skater: p.skater, From: p.code})
	}
	for _, p := range pb {
		add(&Change{Area: AreaPenalties, Kind: "penalty added", Where: p.jam, Team: p.team, Skater: p.skater, To: p.code})
	}
	for k, code := range a.expelled {
		if _, ok := b.expelled[k]; !ok {
			add(&Change{Area: AreaPenalties, Kind: "expulsion removed", Skater: k, From: code})
		}
	}
	for k, code := range b.expelled {
		if _, ok := a.expelled[k]; !ok {
			add(&Change{Area: AreaPenalties, Kind: "expulsion added", Skater: k, To: code})
		}
	}
	sortChanges(out, a, b)
	return out
}

func flagKind(flag string, on bool) string {
	if on {
		return flag + " added"
	}
	return flag + " removed"
}

func compareTeamJam(key, team string, i int, ta, tb *teamJam, num func(int, string) string, add func(*Change)) {
	for _, f := range []string{"lead", "lost", "call", "no pivot", "star pass", "NI", "NI after star pass"} {
		if ta.flags[f] != tb.flags[f] {
			add(&Change{Area: AreaScore, Kind: flagKind(f, tb.flags[f]), Where: key, Team: team})
		}
	}
	trips := map[string][2]*int{}
	var names []string
	for k, t := range [2][]tripPoints{ta.trips, tb.trips} {
		for _, tr := range t {
			v := trips[tr.name]
			if v[0] == nil && v[1] == nil {
				names = append(names, tr.name)
			}
			p := tr.points
			v[k] = &p
			trips[tr.name] = v
		}
	}
	for _, n := range names {
		v := trips[n]
		switch {
		case v[0] == nil:
			add(&Change{Area: AreaScore, Kind: "trip added", Where: key, Team: team, From: n, To: strconv.Itoa(*v[1])})
		case v[1] == nil:
			add(&Change{Area: AreaScore, Kind: "trip removed", Where: key, Team: team, From: n + ": " + strconv.Itoa(*v[0])})
		case *v[0] != *v[1]:
			add(&Change{Area: AreaScore, Kind: "trip points changed", Where: key + " " + n, Team: team, From: strconv.Itoa(*v[0]), To: strconv.Itoa(*v[1])})
		}
	}
	if ta.osOffset != tb.osOffset {
		add(&Change{Area: AreaScore, Kind: "OS offset changed", Where: key, Team: team, From: strconv.Itoa(ta.osOffset), To: strconv.Itoa(tb.osOffset)})
	}
	// Lineups.
	posA := map[string]string{}
	symA := map[string]string{}
	for n, p := range ta.pos {
		posA[num(i, n)] = p
	}
	for n, s := range ta.symbols {
		symA[num(i, n)] = s
	}
	for n, p := range posA {
		if q, ok := tb.pos[n]; !ok {
			add(&Change{Area: AreaLineups, Kind: "skater removed from lineup", Where: key, Team: team, Skater: n, From: p})
		} else if q != p {
			add(&Change{Area: AreaLineups, Kind: "position changed", Where: key, Team: team, Skater: n, From: p, To: q})
		}
	}
	for n, q := range tb.pos {
		if _, ok := posA[n]; !ok {
			add(&Change{Area: AreaLineups, Kind: "skater added to lineup", Where: key, Team: team, Skater: n, To: q})
		}
	}
	if ta.unfilled != tb.unfilled {
		add(&Change{Area: AreaLineups, Kind: "positions marked not fielded", Where: key, Team: team, From: strconv.Itoa(ta.unfilled), To: strconv.Itoa(tb.unfilled)})
	}
	// Box symbols, for skaters in both lineups (a skater added or removed
	// is already one change).
	for n := range posA {
		if _, ok := tb.pos[n]; !ok {
			continue
		}
		if symA[n] != tb.symbols[n] {
			add(&Change{Area: AreaBox, Kind: "box symbols changed", Where: key, Team: team, Skater: n, From: symA[n], To: tb.symbols[n]})
		}
	}
}

func mapValues(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

// sortChanges puts changes in game order (jams), then area, kind, team,
// skater.
func sortChanges(cs []*Change, a, b *paper) {
	order := func(c *Change) int {
		w := c.Where
		if i := strings.Index(w, " trip"); i >= 0 {
			w = w[:i]
		}
		if i, ok := a.jamIndex[w]; ok {
			return i
		}
		if i, ok := b.jamIndex[w]; ok {
			return i
		}
		if w == "" {
			return -1
		}
		return 1 << 20
	}
	sort.SliceStable(cs, func(x, y int) bool {
		p, q := cs[x], cs[y]
		return cmp.Or(cmp.Compare(order(p), order(q)), strings.Compare(p.Where, q.Where), strings.Compare(p.Area, q.Area),
			strings.Compare(p.Kind, q.Kind), strings.Compare(p.Team, q.Team), strings.Compare(p.Skater, q.Skater)) < 0
	})
}
