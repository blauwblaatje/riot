package analysis

import (
	"cmp"
	"slices"
	"sort"
	"time"

	"crgformat/derive"
	"crgformat/stats"
)

// Timing is what only the scoreboard's game files can tell: a statsbook has
// no times. Over the games that have a scoreboard file with a jam played.
type Timing struct {
	Games int `json:"games"`
	Jams  int `json:"jams"`
	// Jam length (ms), how jams ended, and how many ran the full time.
	JamLength *Hist          `json:"jamLength"`
	JamEnds   map[string]int `json:"jamEnds"` // calloff, time, injury, official, unknown
	// Jam clock when a lead jammer was declared (the end of their initial
	// trip), and when a lead jammer called the jam off.
	LeadAt *Hist `json:"leadAt"`
	CallAt *Hist `json:"callAt"`
	// Jam clock when penalties were entered: when in a jam they happen.
	PenaltyAt *Hist `json:"penaltyAt"`
	// Time between a jam's end and the next jam's start, without a timeout
	// or the end of a period in between.
	Lineup *Hist `json:"lineup"`
	// Timeouts: team, official, official review; count and total time (ms).
	Timeouts map[string]*Durations `json:"timeouts"`
	Teams    []*TeamTiming         `json:"teams"`
	Jammers  []*JammerTiming       `json:"jammers"`
	PerGame  []*GameTiming         `json:"perGame"`
}

// Hist is a set of durations (ms): bins of Step ms from 0, the last bin
// holding everything from there on, and the mean.
type Hist struct {
	Step   int64   `json:"step"`
	Bins   []int   `json:"bins"`
	N      int     `json:"n"`
	Sum    int64   `json:"sum"`
	Mean   float64 `json:"mean"`
	Max    int64   `json:"max"`
	Median int64   `json:"median"`
	all    []int64
}

func newHist(step int64, bins int) *Hist { return &Hist{Step: step, Bins: make([]int, bins)} }

func (h *Hist) add(ms int64) {
	if ms < 0 {
		return
	}
	i := int(ms / h.Step)
	if i >= len(h.Bins) {
		i = len(h.Bins) - 1
	}
	h.Bins[i]++
	h.N++
	h.Sum += ms
	h.Max = max(h.Max, ms)
	h.all = append(h.all, ms)
}

func (h *Hist) done() {
	if h.N > 0 {
		h.Mean = float64(h.Sum) / float64(h.N)
		slices.Sort(h.all)
		h.Median = h.all[h.N/2]
	}
	h.all = nil
}

// Durations is a count and the time together, ms.
type Durations struct {
	Count int   `json:"count"`
	Time  int64 `json:"time"`
	Timed int   `json:"timed"` // of Count, how many have a duration
}

// TeamTiming is a team's times.
type TeamTiming struct {
	Team     string `json:"team"` // team key (stats.Tourn)
	Jams     int    `json:"jams"`
	Leads    int    `json:"leads"`
	LeadTime int64  `json:"leadTime"` // ms, all leads together
	Calls    int    `json:"calls"`
	CallTime int64  `json:"callTime"` // jam clock at the call-offs, together
}

// JammerTiming is a jammer's lead times.
type JammerTiming struct {
	Team     string `json:"team"`
	Number   string `json:"number"`
	Name     string `json:"name,omitempty"`
	Jams     int    `json:"jams"`
	Leads    int    `json:"leads"`
	LeadTime int64  `json:"leadTime"` // ms, together
	Fastest  int64  `json:"fastest"`  // ms
}

// GameTiming is one game's times.
type GameTiming struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Duration int64  `json:"duration"` // first jam start to last jam end, ms
	Jams     int    `json:"jams"`
	JamTime  int64  `json:"jamTime"` // all jams together, ms
	Lineups  int    `json:"lineups"`
	Lineup   int64  `json:"lineup"` // all timed lineups together, ms
	Timeouts int    `json:"timeouts"`
	Reviews  int    `json:"reviews"`
}

// ComputeTiming works out the times of the games with a scoreboard file. t
// is the stats of the same games (for the team keys).
func ComputeTiming(games []*Game, t *stats.Tourn) *Timing {
	out := &Timing{JamLength: newHist(10000, 13), JamEnds: map[string]int{}, LeadAt: newHist(5000, 13),
		CallAt: newHist(10000, 13), PenaltyAt: newHist(10000, 13), Lineup: newHist(5000, 13),
		Timeouts: map[string]*Durations{"team": {}, "official": {}, "review": {}},
		Teams:    []*TeamTiming{}, Jammers: []*JammerTiming{}, PerGame: []*GameTiming{}}
	teamsOf := map[string][2]string{}
	for _, r := range t.Games {
		teamsOf[r.ID] = r.Teams
	}
	teams := map[string]*TeamTiming{}
	jammers := map[string]*JammerTiming{}
	for _, g := range sortedGames(games) {
		s := g.Scoreboard
		if s == nil {
			continue
		}
		keys, ok := teamsOf[g.ID]
		if !ok {
			continue
		}
		out.Games++
		dg := derive.New(s)
		gt := &GameTiming{ID: g.ID, Label: g.Label}
		var first, last time.Time
		for _, p := range s.Periods {
			timeoutAfter := map[string]bool{}
			for _, to := range p.Timeouts {
				timeoutAfter[to.AfterJam] = true
				kind := "team"
				switch {
				case to.Review:
					kind = "review"
					gt.Reviews++
				case to.Owner == "O":
					kind = "official"
				}
				d := out.Timeouts[kind]
				d.Count++
				gt.Timeouts++
				if to.Duration != nil {
					d.Timed++
					d.Time += *to.Duration
				}
			}
			for k, j := range p.Jams {
				if len(j.Teams) < 2 {
					continue
				}
				out.Jams++
				gt.Jams++
				out.JamEnds[cmp.Or(j.EndReason, "unknown")]++
				if j.Duration != nil {
					out.JamLength.add(*j.Duration)
					gt.JamTime += *j.Duration
				}
				start, end := parseTime(j.Start), parseTime(j.End)
				if !start.IsZero() && (first.IsZero() || start.Before(first)) {
					first = start
				}
				if end.After(last) {
					last = end
				}
				if k+1 < len(p.Jams) && !timeoutAfter[j.ID] && !end.IsZero() {
					if next := parseTime(p.Jams[k+1].Start); !next.IsZero() {
						gap := next.Sub(end).Milliseconds()
						// Longer than 2 minutes: something else happened (an
						// unrecorded stoppage).
						if gap > 0 && gap < 120000 {
							out.Lineup.add(gap)
							gt.Lineups++
							gt.Lineup += gap
						}
					}
				}
				for i, tj := range j.Teams[:2] {
					tm := teams[keys[i]]
					if tm == nil {
						tm = &TeamTiming{Team: keys[i]}
						teams[keys[i]] = tm
					}
					tm.Jams++
					var jt *JammerTiming
					if f := tj.Lineup["Jammer"]; f != nil && f.Skater != "" {
						if sk := dg.SkaterByID[f.Skater]; sk != nil {
							k := keys[i] + "/" + sk.Number
							jt = jammers[k]
							if jt == nil {
								jt = &JammerTiming{Team: keys[i], Number: sk.Number}
								jammers[k] = jt
							}
							jt.Name = cmp.Or(sk.Name, jt.Name)
							jt.Jams++
						}
					}
					if tj.Lead && len(tj.Trips) > 0 && tj.Trips[0].JamClockEnd != nil {
						at := *tj.Trips[0].JamClockEnd
						out.LeadAt.add(at)
						tm.Leads++
						tm.LeadTime += at
						if jt != nil {
							if jt.Leads == 0 || at < jt.Fastest {
								jt.Fastest = at
							}
							jt.Leads++
							jt.LeadTime += at
						}
					}
					if tj.Lead && tj.Calloff && j.Duration != nil {
						out.CallAt.add(*j.Duration)
						tm.Calls++
						tm.CallTime += *j.Duration
					}
				}
			}
		}
		for _, p := range s.Penalties {
			if p.JamClock != nil {
				if jp := dg.JamByID[p.Jam]; jp != nil && !jp.Upcoming {
					out.PenaltyAt.add(*p.JamClock)
				}
			}
		}
		if !first.IsZero() && last.After(first) {
			gt.Duration = last.Sub(first).Milliseconds()
		}
		out.PerGame = append(out.PerGame, gt)
	}
	for _, h := range []*Hist{out.JamLength, out.LeadAt, out.CallAt, out.PenaltyAt, out.Lineup} {
		h.done()
	}
	for _, tm := range teams {
		out.Teams = append(out.Teams, tm)
	}
	sort.Slice(out.Teams, func(a, b int) bool { return out.Teams[a].Team < out.Teams[b].Team })
	for _, jt := range jammers {
		out.Jammers = append(out.Jammers, jt)
	}
	sort.Slice(out.Jammers, func(a, b int) bool {
		x, y := out.Jammers[a], out.Jammers[b]
		return x.Team < y.Team || x.Team == y.Team && x.Number < y.Number
	})
	return out
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
