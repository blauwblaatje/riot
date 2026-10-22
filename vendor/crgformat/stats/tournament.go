package stats

import (
	"cmp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"crgformat/derive"
	"crgformat/replay"
)

// Tournament stats: the games of a tournament together, after Adam
// Czarnecki's tournament stats for the WFTDA Postseason and the Roller Derby
// World Cup (Roller Derby Innovation): leading scorers, points per jam, lead
// jammer %, +/-, star pass rates, penalties, comebacks, lead changes.
//
// A team is recognised over the games by its league and its roster, not its
// name, which varies between game files ("Crime City Rollers" and "Crime
// City Rollers A-team", "SCO" and "Power Of Scotland"): game teams of one
// league with mostly the same skater numbers are one team, and a league's B
// team is another. A skater is a team and a number, so someone who skates
// for two teams has their stats per team.

// Tourn is a tournament's stats.
type Tourn struct {
	Name    string        `json:"name"`
	Through string        `json:"through,omitempty"` // date of the last game
	Games   []*GameResult `json:"games"`
	Teams   []*TeamTotals `json:"teams"`
	Skaters []*SkaterTot  `json:"skaters"`
	// Records over all jams.
	TopJams      []*JamRecord   `json:"topJams"`      // highest single-jam scores
	JamScores    []*ScoreCount  `json:"jamScores"`    // most common jam scores, higher first ("4-0")
	JamEnds      map[string]int `json:"jamEnds"`      // why jams ended: calloff, time, injury, official, unknown
	PenaltyCodes map[string]int `json:"penaltyCodes"` // code -> count
	Jams         int            `json:"jams"`
	Points       int            `json:"points"`
	Penalties    int            `json:"penalties"`
	LeadJams     int            `json:"leadJams"` // jams where someone got lead
	StarPasses   int            `json:"starPasses"`
}

// GameResult is one game: score, margin, and how it went.
type GameResult struct {
	ID       string    `json:"id"`
	Date     string    `json:"date,omitempty"`
	GameNo   string    `json:"gameNo,omitempty"`
	Teams    [2]string `json:"teams"` // team keys
	Score    [2]int    `json:"score"`
	Periods  [][2]int  `json:"periods"` // points per period
	Jams     int       `json:"jams"`
	Penalty  [2]int    `json:"penalties"`
	Finished bool      `json:"finished"`
	// LeadChanges: how often the score went from one team ahead, tied or
	// the other team ahead to another of those, after a jam (as Roller Derby
	// Innovation's "lead changes/ties"); LastLeadChanges within the last
	// period.
	LeadChanges     int `json:"leadChanges"`
	LastLeadChanges int `json:"lastLeadChanges"`
	// Comeback: the largest deficit the winner made up.
	Comeback int `json:"comeback"`
}

// TeamTotals is a team over the tournament.
type TeamTotals struct {
	Key        string         `json:"key"`
	Name       string         `json:"name"`
	League     string         `json:"league,omitempty"`
	Games      int            `json:"games"`
	Wins       int            `json:"wins"`
	Losses     int            `json:"losses"`
	Ties       int            `json:"ties"`
	For        int            `json:"for"`
	Against    int            `json:"against"`
	Jams       int            `json:"jams"`
	LeadJams   int            `json:"leadJams"`
	Penalties  int            `json:"penalties"`
	StarPasses int            `json:"starPasses"`
	Codes      map[string]int `json:"codes"`
}

// SkaterTot is a skater (a team and a number) over the tournament.
type SkaterTot struct {
	Team     string `json:"team"` // team key
	Number   string `json:"number"`
	Name     string `json:"name,omitempty"`
	Pronouns string `json:"pronouns,omitempty"`
	Games    int    `json:"games"`
	// As jammer (the one who started the jam with the star).
	JammerJams    int `json:"jammerJams"`
	Lead          int `json:"lead"`
	Calloffs      int `json:"calloffs"`
	StarPasses    int `json:"starPasses"`
	JammerPoints  int `json:"jammerPoints"`  // points scored before any star pass
	JammerAgainst int `json:"jammerAgainst"` // the other team's points in those jams
	JammerPM      int `json:"jammerPM"`      // jam score difference in those jams
	BestJam       int `json:"bestJam"`       // most points in one jam as jammer
	// As pivot and blocker.
	PivotJams   int `json:"pivotJams"`
	BlockerJams int `json:"blockerJams"` // pivot and blocker positions together
	Received    int `json:"received"`    // star passes received as pivot
	PivotPoints int `json:"pivotPoints"` // points after receiving the star
	BlockerPM   int `json:"blockerPM"`   // jam score difference in pivot and blocker jams
	Penalties   int `json:"penalties"`
}

// JamRecord is one team's score in one jam.
type JamRecord struct {
	Game   string `json:"game"`
	Period int    `json:"period"`
	Jam    int    `json:"jam"`
	Team   string `json:"team"`
	Number string `json:"number,omitempty"` // the jammer
	Name   string `json:"name,omitempty"`
	Points int    `json:"points"`
}

// ScoreCount is a jam score ("4-0", higher first) and how often it was.
type ScoreCount struct {
	Score string `json:"score"`
	Count int    `json:"count"`
}

// played is a game with at least one jam.
func played(s *replay.Summary) bool {
	for _, p := range s.Periods {
		if len(p.Jams) > 0 {
			return true
		}
	}
	return false
}

// ComputeTournament works out the stats of a tournament's games; games
// without a jam played are left out.
func ComputeTournament(name string, games []*replay.Summary) *Tourn {
	return computeTournament(name, games, nil)
}

// TeamID is a team over several games, as the caller recognises it.
type TeamID struct{ Key, Name, League string }

// ComputeTournamentWith is ComputeTournament with the teams given by the
// caller instead of recognised by league and roster: team gives game s's
// team i (0 or 1).
func ComputeTournamentWith(name string, games []*replay.Summary, team func(s *replay.Summary, i int) TeamID) *Tourn {
	return computeTournament(name, games, team)
}

func computeTournament(name string, games []*replay.Summary, team func(s *replay.Summary, i int) TeamID) *Tourn {
	var gs []*replay.Summary
	for _, s := range games {
		if played(s) {
			gs = append(gs, s)
		}
	}
	sort.SliceStable(gs, func(a, b int) bool {
		da, db := gs[a].Info["Date"], gs[b].Info["Date"]
		if da != db {
			return da < db
		}
		return gameNo(gs[a]) < gameNo(gs[b])
	})
	out := &Tourn{Name: name, Games: []*GameResult{}, Teams: []*TeamTotals{}, Skaters: []*SkaterTot{}, TopJams: []*JamRecord{},
		JamScores: []*ScoreCount{}, JamEnds: map[string]int{}, PenaltyCodes: map[string]int{}}
	var keys [][2]teamKey
	if team == nil {
		keys = teamKeys(gs)
	} else {
		keys = make([][2]teamKey, len(gs))
		for gi, s := range gs {
			for i := 0; i < 2 && i < len(s.Teams); i++ {
				id := team(s, i)
				keys[gi][i] = teamKey{key: id.Key, name: id.Name, league: id.League}
			}
		}
	}
	teams := map[string]*TeamTotals{}
	skaters := map[string]*SkaterTot{}
	scores := map[string]int{}
	for gi, s := range gs {
		if d := s.Info["Date"]; d > out.Through {
			out.Through = d
		}
		g := derive.New(s)
		res := &GameResult{ID: s.ID, Date: s.Info["Date"], GameNo: s.Info["GameNo"], Score: derive.Scores(s), Periods: [][2]int{},
			Finished: s.State == "finished"}
		tt := [2]*TeamTotals{}
		for i := 0; i < 2 && i < len(s.Teams); i++ {
			k := keys[gi][i]
			res.Teams[i] = k.key
			t := teams[k.key]
			if t == nil {
				t = &TeamTotals{Key: k.key, Name: k.name, League: k.league, Codes: map[string]int{}}
				teams[k.key] = t
			}
			tt[i] = t
		}
		if tt[0] == nil || tt[1] == nil {
			continue // not a two-team game
		}
		skater := func(i int, id string) *SkaterTot {
			sk := g.SkaterByID[id]
			if sk == nil {
				return nil
			}
			key := tt[i].Key + "/" + sk.Number
			st := skaters[key]
			if st == nil {
				st = &SkaterTot{Team: tt[i].Key, Number: sk.Number}
				skaters[key] = st
			}
			// The latest game's name and pronouns.
			st.Name, st.Pronouns = cmp.Or(sk.Name, st.Name), cmp.Or(sk.Pronouns, st.Pronouns)
			return st
		}
		inGame := map[*SkaterTot]bool{}
		var running [2]int
		ahead, aheadInLast, comeback := "", false, [2]int{}
		for pi, p := range s.Periods {
			var pp [2]int
			last := pi == len(s.Periods)-1
			for _, j := range p.Jams {
				if len(j.Teams) < 2 {
					continue
				}
				res.Jams++
				out.Jams++
				out.JamEnds[cmp.Or(j.EndReason, "unknown")]++
				var js [2]int
				for i, tj := range j.Teams[:2] {
					js[i] = tj.OsOffset
					for _, tr := range tj.Trips {
						js[i] += tr.Points
					}
				}
				hi, lo := max(js[0], js[1]), min(js[0], js[1])
				scores[strconv.Itoa(hi)+"-"+strconv.Itoa(lo)]++
				for i, tj := range j.Teams[:2] {
					t := tt[i]
					t.Jams++
					pp[i] += js[i]
					if tj.Lead {
						t.LeadJams++
						out.LeadJams++
					}
					spAt := -1
					for k, tr := range tj.Trips {
						if tr.ID == tj.StarPassTrip {
							spAt = k
						}
					}
					var before, after int
					for _, tr := range tj.Trips {
						if tr.AfterStarPass {
							after += tr.Points
						} else {
							before += tr.Points
						}
					}
					if spAt >= 0 {
						t.StarPasses++
						out.StarPasses++
					}
					diff := js[i] - js[1-i]
					rec := &JamRecord{Game: s.ID, Period: p.Number, Jam: j.Number, Team: t.Key, Points: js[i]}
					if f := tj.Lineup["Jammer"]; f != nil && f.Skater != "" {
						if st := skater(i, f.Skater); st != nil {
							inGame[st] = true
							st.JammerJams++
							st.JammerPoints += before
							st.JammerAgainst += js[1-i]
							st.JammerPM += diff
							st.BestJam = max(st.BestJam, before)
							if tj.Lead {
								st.Lead++
								if tj.Calloff {
									st.Calloffs++
								}
							}
							if spAt >= 0 {
								st.StarPasses++
							}
							rec.Number, rec.Name = st.Number, st.Name
						}
					}
					out.TopJams = append(out.TopJams, rec)
					for _, pos := range []string{"Pivot", "Blocker1", "Blocker2", "Blocker3"} {
						f := tj.Lineup[pos]
						if f == nil || f.Skater == "" {
							continue
						}
						st := skater(i, f.Skater)
						if st == nil {
							continue
						}
						inGame[st] = true
						st.BlockerJams++
						st.BlockerPM += diff
						if pos == "Pivot" {
							st.PivotJams++
							if spAt >= 0 {
								st.Received++
								st.PivotPoints += after
							}
						}
					}
				}
				// Who is ahead after the jam: lead changes, and the winner's
				// largest deficit.
				running[0] += js[0]
				running[1] += js[1]
				now := "="
				switch {
				case running[0] > running[1]:
					now = "1"
				case running[1] > running[0]:
					now = "2"
				}
				if ahead != "" && now != ahead {
					res.LeadChanges++
					if last && aheadInLast {
						res.LastLeadChanges++
					}
				}
				ahead, aheadInLast = now, last
				comeback[0] = max(comeback[0], running[1]-running[0])
				comeback[1] = max(comeback[1], running[0]-running[1])
			}
			res.Periods = append(res.Periods, pp)
		}
		for _, pen := range s.Penalties {
			i := derive.TeamIndex(pen.Team)
			if i < 0 || i > 1 {
				continue
			}
			tt[i].Penalties++
			tt[i].Codes[pen.Code]++
			res.Penalty[i]++
			out.Penalties++
			out.PenaltyCodes[pen.Code]++
			if pen.Skater != "" {
				if st := skater(i, pen.Skater); st != nil {
					st.Penalties++
					inGame[st] = true
				}
			}
		}
		for st := range inGame {
			st.Games++
		}
		for i := 0; i < 2; i++ {
			t := tt[i]
			t.Games++
			t.For += res.Score[i]
			t.Against += res.Score[1-i]
			out.Points += res.Score[i]
			switch {
			case res.Score[i] > res.Score[1-i]:
				t.Wins++
			case res.Score[i] < res.Score[1-i]:
				t.Losses++
			default:
				t.Ties++
			}
		}
		if w := winner(res.Score); w >= 0 {
			res.Comeback = comeback[w]
		}
		out.Games = append(out.Games, res)
	}
	for _, t := range teams {
		out.Teams = append(out.Teams, t)
	}
	sort.Slice(out.Teams, func(a, b int) bool { return out.Teams[a].Name < out.Teams[b].Name })
	for _, st := range skaters {
		out.Skaters = append(out.Skaters, st)
	}
	sort.Slice(out.Skaters, func(a, b int) bool {
		x, y := out.Skaters[a], out.Skaters[b]
		return x.Team < y.Team || x.Team == y.Team && x.Number < y.Number
	})
	sort.SliceStable(out.TopJams, func(a, b int) bool { return out.TopJams[a].Points > out.TopJams[b].Points })
	if len(out.TopJams) > 20 {
		out.TopJams = out.TopJams[:20]
	}
	for sc, n := range scores {
		out.JamScores = append(out.JamScores, &ScoreCount{Score: sc, Count: n})
	}
	sort.Slice(out.JamScores, func(a, b int) bool {
		x, y := out.JamScores[a], out.JamScores[b]
		return x.Count > y.Count || x.Count == y.Count && x.Score < y.Score
	})
	if len(out.JamScores) > 20 {
		out.JamScores = out.JamScores[:20]
	}
	return out
}

func winner(sc [2]int) int {
	switch {
	case sc[0] > sc[1]:
		return 0
	case sc[1] > sc[0]:
		return 1
	}
	return -1
}

func gameNo(s *replay.Summary) int {
	n := 0
	for _, r := range s.Info["GameNo"] {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// --- recognising teams over games -------------------------------------------------

type teamKey struct{ key, name, league string }

// cluster is one team of a league: the skater numbers seen in its games.
type cluster struct {
	league  string // as written
	numbers map[string]bool
	names   map[string]int // team names used, for the label
	key     string
}

// teamKeys gives each game's two teams their tournament team: per league,
// game teams with mostly the same skater numbers (at least half of the
// smaller roster) are one team.
func teamKeys(gs []*replay.Summary) [][2]teamKey {
	byLeague := map[string][]*cluster{}
	var order []string
	assign := make([][2]*cluster, len(gs))
	for gi, s := range gs {
		for i := 0; i < 2 && i < len(s.Teams); i++ {
			t := s.Teams[i]
			league := strings.TrimSpace(cmp.Or(t.League, t.Name))
			norm := normalise(league)
			nums := map[string]bool{}
			for _, sk := range t.Skaters {
				if !strings.Contains(" "+sk.Flags+" ", " ALT ") && sk.Status != "notInGame" {
					nums[sk.Number] = true
				}
			}
			var best *cluster
			bestScore := 0.0
			for _, c := range byLeague[norm] {
				if i == 1 && assign[gi][0] == c {
					continue // a league's two teams playing each other
				}
				if sc := overlap(nums, c.numbers); sc > bestScore {
					best, bestScore = c, sc
				}
			}
			switch {
			case bestScore >= 0.5:
			case len(nums) == 0 && len(byLeague[norm]) > 0:
				best = byLeague[norm][0] // no roster to go by
			default:
				best = &cluster{league: league, numbers: map[string]bool{}, names: map[string]int{}}
				if len(byLeague[norm]) == 0 {
					order = append(order, norm)
				}
				byLeague[norm] = append(byLeague[norm], best)
			}
			for n := range nums {
				best.numbers[n] = true
			}
			best.names[strings.TrimSpace(cmp.Or(t.TeamName, t.Name))]++
			assign[gi][i] = best
		}
	}
	// Labels: the league; a league with more than one team adds the team name.
	used := map[string]bool{}
	for _, norm := range order {
		cs := byLeague[norm]
		for _, c := range cs {
			label := c.league
			if len(cs) > 1 {
				label += " " + mostUsed(c.names)
			}
			key := normalise(label)
			for used[key] {
				key += "+"
			}
			used[key] = true
			c.key = key
		}
	}
	out := make([][2]teamKey, len(gs))
	for gi := range gs {
		for i := 0; i < 2; i++ {
			if c := assign[gi][i]; c != nil {
				label := c.league
				if len(byLeague[normalise(c.league)]) > 1 {
					label += " " + mostUsed(c.names)
				}
				out[gi][i] = teamKey{key: c.key, name: label, league: c.league}
			}
		}
	}
	return out
}

func normalise(s string) string { return strings.Join(strings.Fields(strings.ToLower(s)), " ") }

// overlap is the share of the smaller roster that is in the other.
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

func mostUsed(names map[string]int) string {
	var keys []string
	for k := range names {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b string) int { return cmp.Or(names[b]-names[a], len(b)-len(a), strings.Compare(a, b)) })
	if len(keys) == 0 {
		return ""
	}
	return keys[0]
}
