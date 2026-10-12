package analysis

import (
	"cmp"
	"math"
	"sort"
	"strings"

	"crgformat/derive"
	"crgformat/replay"
	"crgformat/stats"
)

// Crews: the officials who worked a game, grouped into crews. Neither a
// scoreboard file nor a statsbook says which crew it was, so a crew is
// recognised by its people: within a tournament, games whose referees (or
// NSOs) are mostly the same people are one crew. Referee crews are compared
// on their penalty calls, NSO crews on what was corrected in their
// statsbooks.

// Crew kinds.
const (
	KindReferee = "referee"
	KindNSO     = "nso"
)

// Crews is the crews of one kind.
type Crews struct {
	Kind  string   `json:"kind"`
	Crews []*Crew  `json:"crews"`
	Codes []string `json:"codes"` // penalty codes, most called first
	// Unknown: games without officials of this kind on the IGRF.
	Unknown []string `json:"unknown"`
	// Prior: how many of a team's jams outside a crew weigh as much as the
	// average of all teams, when working out a crew's expected penalties.
	Prior float64 `json:"prior"`
}

// Crew is one crew in one tournament.
type Crew struct {
	Key        string    `json:"key"`
	Tournament string    `json:"tournament"`
	Head       string    `json:"head"` // most often head referee or head NSO
	Members    []*Member `json:"members"`
	Games      []string  `json:"games"` // game ids
	Jams       int       `json:"jams"`
	// Penalties called in the crew's games, and how many the teams would
	// have got with the tournament's other crews (HasExpected: there were
	// other crews to compare with): per penalty code, from each team's rate
	// per jam in the games other crews worked.
	Penalties     int                `json:"penalties"`
	Codes         map[string]int     `json:"codes"`
	HasExpected   bool               `json:"hasExpected"`
	Expected      float64            `json:"expected"`
	ExpectedCodes map[string]float64 `json:"expectedCodes"`
	Expulsions    int                `json:"expulsions"`
	// Official reviews, and how many the team kept (it was right).
	Reviews  int `json:"reviews"`
	Retained int `json:"retained"`
	// From the scoreboard files only (TimedGames of the crew's games).
	TimedGames     int   `json:"timedGames"`
	OfficialTOs    int   `json:"officialTimeouts"`
	OfficialTOTime int64 `json:"officialTimeoutTime"` // ms, all of them together
	// Corrections in the crew's statsbooks (Compared of its games had both).
	Compared int            `json:"compared"`
	Fixes    int            `json:"fixes"`
	FixAreas map[string]int `json:"fixAreas"`
}

// Member is an official in a crew and how many of its games they worked.
type Member struct {
	Name  string `json:"name"`
	Roles string `json:"roles"`
	Games int    `json:"games"`
}

func isReferee(role string) bool { return strings.Contains(role, "Referee") }

// crewPeople is the officials of one kind in a game: normalised name ->
// display name, role.
func crewPeople(s *replay.Summary, kind string) (names map[string][2]string, head string) {
	names = map[string][2]string{}
	hr, hnso := derive.Heads(s)
	for _, o := range s.Officials {
		n := NormName(o.Name)
		if n == "" || o.Role == "" || isReferee(o.Role) != (kind == KindReferee) {
			continue
		}
		if prev, ok := names[n]; ok {
			if !strings.Contains(prev[1], o.Role) {
				names[n] = [2]string{prev[0], prev[1] + ", " + o.Role}
			}
		} else {
			names[n] = [2]string{strings.Join(strings.Fields(o.Name), " "), o.Role}
		}
		if (kind == KindReferee && o.ID == hr) || (kind == KindNSO && o.ID == hnso) {
			head = n
		}
	}
	return names, head
}

// officials is a game's officials of both kinds: the statsbook's IGRF when
// it has officials (it may have been corrected), else the scoreboard's.
func officials(g *Game) *replay.Summary {
	if g.Stats != nil && len(g.Stats.Officials) > 0 {
		return g.Stats
	}
	if g.Scoreboard != nil && len(g.Scoreboard.Officials) > 0 {
		return g.Scoreboard
	}
	return g.Stats
}

// ComputeCrews groups the games' officials into crews of one kind and adds
// up their games. t is the stats of the same games (for the teams).
func ComputeCrews(games []*Game, t *stats.Tourn, kind string) *Crews {
	out := &Crews{Kind: kind, Crews: []*Crew{}, Codes: []string{}, Unknown: []string{}, Prior: 30}
	type cluster struct {
		crew   *Crew
		people map[string]int // normalised name -> games
		disp   map[string][2]string
		heads  map[string]int
	}
	var clusters []*cluster
	crewOf := map[string]*cluster{} // game id -> crew
	ordered := sortedGames(games)
	for _, g := range ordered {
		s := officials(g)
		if s == nil {
			continue
		}
		people, head := crewPeople(s, kind)
		if len(people) == 0 {
			out.Unknown = append(out.Unknown, g.ID)
			continue
		}
		var best *cluster
		bestShare := 0.0
		for _, c := range clusters {
			if c.crew.Tournament != g.Tournament {
				continue
			}
			n := 0
			for p := range people {
				if c.people[p] > 0 {
					n++
				}
			}
			if share := float64(n) / float64(len(people)); share > bestShare {
				best, bestShare = c, share
			}
		}
		if bestShare < 0.5 {
			best = &cluster{crew: &Crew{Tournament: g.Tournament, Codes: map[string]int{}, ExpectedCodes: map[string]float64{},
				FixAreas: map[string]int{}}, people: map[string]int{}, disp: map[string][2]string{}, heads: map[string]int{}}
			clusters = append(clusters, best)
		}
		for p, d := range people {
			best.people[p]++
			if old, ok := best.disp[p]; ok && !strings.Contains(old[1], d[1]) {
				d[1] = old[1] + ", " + d[1]
			}
			best.disp[p] = d
		}
		if head != "" {
			best.heads[head]++
		}
		best.crew.Games = append(best.crew.Games, g.ID)
		crewOf[g.ID] = best
	}

	// Per game: jams, the teams, penalties per team and code.
	nums := map[string]*teamGame{}
	byID := map[string]*Game{}
	for _, g := range games {
		byID[g.ID] = g
	}
	allCodes := map[string]int{}
	for _, r := range t.Games {
		g := byID[r.ID]
		if g == nil || g.Stats == nil {
			continue
		}
		n := &teamGame{jams: r.Jams, teams: r.Teams, pens: [2]map[string]int{{}, {}}}
		for _, p := range g.Stats.Penalties {
			if i := derive.TeamIndex(p.Team); i == 0 || i == 1 {
				n.pens[i][p.Code]++
				allCodes[p.Code]++
			}
		}
		nums[r.ID] = n
	}
	for _, c := range counts(allCodes, 0) {
		out.Codes = append(out.Codes, c.Value)
	}

	for ci, c := range clusters {
		cr := c.crew
		cr.Key = kind + "-" + itoa(ci+1)
		// The head: most often head; else the one in the most games.
		cr.Head = mostOf(c.heads)
		for p, n := range c.people {
			cr.Members = append(cr.Members, &Member{Name: c.disp[p][0], Roles: c.disp[p][1], Games: n})
		}
		sort.Slice(cr.Members, func(a, b int) bool {
			x, y := cr.Members[a], cr.Members[b]
			return cmp.Or(y.Games-x.Games, strings.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name))) < 0
		})
		if cr.Head == "" && len(cr.Members) > 0 {
			cr.Head = cr.Members[0].Name
		} else if d, ok := c.disp[cr.Head]; ok {
			cr.Head = d[0]
		}
		in := map[string]bool{}
		for _, id := range cr.Games {
			in[id] = true
		}
		for _, id := range cr.Games {
			g := byID[id]
			if n := nums[id]; n != nil {
				cr.Jams += n.jams
				for i := 0; i < 2; i++ {
					for code, k := range n.pens[i] {
						cr.Codes[code] += k
						cr.Penalties += k
					}
				}
			}
			if g.Stats != nil {
				cr.Expulsions += len(g.Stats.Expulsions)
				for _, p := range g.Stats.Periods {
					for _, to := range p.Timeouts {
						if to.Review {
							cr.Reviews++
							if to.Retained != nil && *to.Retained {
								cr.Retained++
							}
						}
					}
				}
			}
			if s := g.Scoreboard; s != nil {
				cr.TimedGames++
				for _, p := range s.Periods {
					for _, to := range p.Timeouts {
						if to.Owner == "O" && !to.Review {
							cr.OfficialTOs++
							if to.Duration != nil {
								cr.OfficialTOTime += *to.Duration
							}
						}
					}
				}
			}
			if g.Compared {
				cr.Compared++
				cr.Fixes += len(g.Changes)
				for _, ch := range g.Changes {
					cr.FixAreas[ch.Area]++
				}
			}
		}
		// Compared with the other crews of the same tournament only: rates
		// differ between tournaments (rules, level, game length).
		same := map[string]*teamGame{}
		for id, n := range nums {
			if g := byID[id]; g != nil && g.Tournament == cr.Tournament {
				same[id] = n
			}
		}
		expected(cr, in, same, out.Prior)
		out.Crews = append(out.Crews, cr)
	}
	return out
}

type teamGame struct {
	jams  int
	teams [2]string
	pens  [2]map[string]int
}

// expected works out the penalties a crew's teams would have got with the
// other crews: per team and code, the team's penalties per jam in games
// outside the crew, pulled towards the average of all teams outside the
// crew by prior jams (a team with few games elsewhere counts as average),
// times the team's jams in the crew's games.
func expected(cr *Crew, in map[string]bool, games map[string]*teamGame, prior float64) {
	teamJams := map[string]int{}
	teamPens := map[string]map[string]int{}
	allJams := 0
	allPens := map[string]int{}
	for id, g := range games {
		if in[id] {
			continue
		}
		for i := 0; i < 2; i++ {
			t := g.teams[i]
			teamJams[t] += g.jams
			allJams += g.jams
			if teamPens[t] == nil {
				teamPens[t] = map[string]int{}
			}
			for code, n := range g.pens[i] {
				teamPens[t][code] += n
				allPens[code] += n
			}
		}
	}
	if allJams == 0 {
		return
	}
	cr.HasExpected = true
	for id, g := range games {
		if !in[id] {
			continue
		}
		for i := 0; i < 2; i++ {
			t := g.teams[i]
			for code, n := range allPens {
				avg := float64(n) / float64(allJams)
				rate := (float64(teamPens[t][code]) + avg*prior) / (float64(teamJams[t]) + prior)
				e := rate * float64(g.jams)
				cr.ExpectedCodes[code] += e
				cr.Expected += e
			}
		}
	}
	for code, e := range cr.ExpectedCodes {
		cr.ExpectedCodes[code] = math.Round(e*100) / 100
	}
	cr.Expected = math.Round(cr.Expected*100) / 100
}

func mostOf(m map[string]int) string {
	best, n := "", 0
	for k, v := range m {
		if v > n || v == n && k < best {
			best, n = k, v
		}
	}
	return best
}

func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return itoa(i/10) + string(rune('0'+i%10))
}

// sortedGames orders games by date and game number.
func sortedGames(games []*Game) []*Game {
	out := append([]*Game(nil), games...)
	key := func(g *Game) (string, int) {
		if g.Stats == nil {
			return "", 0
		}
		n := 0
		for _, r := range g.Stats.Info["GameNo"] {
			if r < '0' || r > '9' {
				break
			}
			n = n*10 + int(r-'0')
		}
		return g.Stats.Info["Date"], n
	}
	sort.SliceStable(out, func(a, b int) bool {
		da, na := key(out[a])
		db, nb := key(out[b])
		return da < db || da == db && na < nb
	})
	return out
}
