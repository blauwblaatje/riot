// Package stats computes game statistics for displays and announcers: per
// jammer, pivot and blocker, as CRG-Stats and Jamstats show them, and the
// score and penalties over the game (docs/engine.md "Stats in our own
// display").
package stats

import (
	"sort"

	"crgformat/derive"
	"crgformat/replay"
)

// Game is the stats of a game.
type Game struct {
	Teams []*Team `json:"teams"`
	// Score after every jam, for a score chart.
	Progress []Point `json:"progress"`
}

// Point is the score after a jam.
type Point struct {
	Period int    `json:"period"`
	Jam    int    `json:"jam"`
	Score  [2]int `json:"score"`
	Lead   string `json:"lead,omitempty"` // "1", "2" or ""
}

// Team is one team's stats.
type Team struct {
	Team      string     `json:"team"`
	Name      string     `json:"name"`
	Jammers   []*Jammer  `json:"jammers"`
	Pivots    []*Pivot   `json:"pivots"`   // skaters who received the star
	Blockers  []*Blocker `json:"blockers"` // everyone who skated as pivot or blocker
	LeadJams  int        `json:"leadJams"`
	Jams      int        `json:"jams"`
	Penalties int        `json:"penalties"`
	// Points and penalties per period.
	PeriodPoints    []int `json:"periodPoints"`
	PeriodPenalties []int `json:"periodPenalties"`
}

// Skater identifies a skater in the stats.
type Skater struct {
	ID     string `json:"id"`
	Number string `json:"number"`
	Name   string `json:"name,omitempty"`
	// For announcers: pronouns, and how to say the name.
	Pronouns      string `json:"pronouns,omitempty"`
	Pronunciation string `json:"pronunciation,omitempty"`
}

// Jammer: jams as jammer, lead, call-offs (of lead jams), points for and
// against in those jams, time on track (until a star pass), penalties, star
// passes given. Calloffs are the Call marks the scorekeeper recorded;
// CRG-Stats instead counts lead jams that ended before 1:59, which also
// counts jams the officials stopped.
type Jammer struct {
	Skater
	Jams       int   `json:"jams"`
	Lead       int   `json:"lead"`
	Calloffs   int   `json:"calloffs"`
	For        int   `json:"for"`
	Against    int   `json:"against"`
	TrackMs    int64 `json:"trackMs"`
	Penalties  int   `json:"penalties"`
	StarPasses int   `json:"starPasses"`
}

// Pivot: star passes received, points after them, time on track as jammer.
type Pivot struct {
	Skater
	Received  int      `json:"received"`
	For       int      `json:"for"`
	TrackMs   int64    `json:"trackMs"`
	Penalties int      `json:"penalties"`
	From      []string `json:"from"` // numbers of the jammers who passed
}

// Blocker: jams played as blocker or pivot, of which as pivot, track time.
type Blocker struct {
	Skater
	Jams      int   `json:"jams"`
	PivotJams int   `json:"pivotJams"`
	TrackMs   int64 `json:"trackMs"`
	Penalties int   `json:"penalties"`
}

// Compute works out the stats of a game.
func Compute(s *replay.Summary) *Game {
	g := derive.New(s)
	out := &Game{Progress: []Point{}}
	pens := map[string]int{}
	for _, p := range s.Penalties {
		pens[p.Skater]++
	}
	ref := func(id string) Skater {
		sk := g.SkaterByID[id]
		if sk == nil {
			return Skater{ID: id, Number: "?"}
		}
		return Skater{ID: id, Number: sk.Number, Name: sk.Name, Pronouns: sk.Pronouns, Pronunciation: sk.Pronunciation}
	}
	teams := make([]*Team, len(s.Teams))
	jammers := make([]map[string]*Jammer, len(s.Teams))
	pivots := make([]map[string]*Pivot, len(s.Teams))
	blockers := make([]map[string]*Blocker, len(s.Teams))
	for i, t := range s.Teams {
		teams[i] = &Team{Team: t.Team, Name: t.Name, Jammers: []*Jammer{}, Pivots: []*Pivot{}, Blockers: []*Blocker{}}
		jammers[i], pivots[i], blockers[i] = map[string]*Jammer{}, map[string]*Pivot{}, map[string]*Blocker{}
	}
	var score [2]int
	for _, p := range s.Periods {
		for _, t := range teams {
			t.PeriodPoints = append(t.PeriodPoints, 0)
			t.PeriodPenalties = append(t.PeriodPenalties, 0)
		}
		for _, j := range p.Jams {
			dur := int64(0)
			if j.Duration != nil {
				dur = *j.Duration
			}
			var jamScore [2]int
			for i, tj := range j.Teams {
				jamScore[i] = tj.OsOffset
				for _, tr := range tj.Trips {
					jamScore[i] += tr.Points
				}
			}
			pt := Point{Period: p.Number, Jam: j.Number}
			for i, tj := range j.Teams {
				t := teams[i]
				t.Jams++
				if tj.Lead {
					t.LeadJams++
					pt.Lead = tj.Team
				}
				score[i] += jamScore[i]
				t.PeriodPoints[len(t.PeriodPoints)-1] += jamScore[i]
				// The jammer, and after a star pass the pivot.
				spAt := -1
				for k, tr := range tj.Trips {
					if tr.ID == tj.StarPassTrip {
						spAt = k
					}
				}
				// With a star pass the jammer's time ends, and the pivot's
				// starts, at the start of the trip the star was passed in (as
				// CRG-Stats splits it).
				jammerMs, pivotMs := dur, int64(0)
				if spAt >= 0 {
					if st := tj.Trips[spAt].JamClockStart; st != nil {
						jammerMs, pivotMs = *st, dur-*st
					}
				}
				if f := tj.Lineup["Jammer"]; f != nil && f.Skater != "" {
					jm := jammers[i][f.Skater]
					if jm == nil {
						jm = &Jammer{Skater: ref(f.Skater)}
						jammers[i][f.Skater] = jm
					}
					jm.Jams++
					jm.For += jamScore[i]
					jm.Against += jamScore[1-i]
					jm.TrackMs += jammerMs
					if tj.Lead {
						jm.Lead++
						if tj.Calloff {
							jm.Calloffs++
						}
					}
					if spAt >= 0 {
						jm.StarPasses++
					}
				}
				if f := tj.Lineup["Pivot"]; f != nil && f.Skater != "" && spAt >= 0 {
					pv := pivots[i][f.Skater]
					if pv == nil {
						pv = &Pivot{Skater: ref(f.Skater), From: []string{}}
						pivots[i][f.Skater] = pv
					}
					pv.Received++
					for _, tr := range tj.Trips {
						if tr.AfterStarPass {
							pv.For += tr.Points
						}
					}
					pv.TrackMs += pivotMs
					if jf := tj.Lineup["Jammer"]; jf != nil && jf.Skater != "" {
						pv.From = append(pv.From, ref(jf.Skater).Number)
					}
				}
				for _, pos := range []string{"Pivot", "Blocker1", "Blocker2", "Blocker3"} {
					if f := tj.Lineup[pos]; f != nil && f.Skater != "" {
						bl := blockers[i][f.Skater]
						if bl == nil {
							bl = &Blocker{Skater: ref(f.Skater)}
							blockers[i][f.Skater] = bl
						}
						bl.Jams++
						bl.TrackMs += dur
						if pos == "Pivot" {
							bl.PivotJams++
						}
					}
				}
			}
			pt.Score = score
			out.Progress = append(out.Progress, pt)
		}
	}
	for _, pen := range s.Penalties {
		for i, t := range s.Teams {
			if t.Team == pen.Team {
				teams[i].Penalties++
				if jp := g.JamByID[pen.Jam]; jp != nil && !jp.Upcoming {
					for k, p := range s.Periods {
						if p.Number == jp.Period && k < len(teams[i].PeriodPenalties) {
							teams[i].PeriodPenalties[k]++
						}
					}
				}
			}
		}
	}
	byNumber := func(a, b Skater) bool { return a.Number < b.Number }
	for i, t := range teams {
		for id, jm := range jammers[i] {
			jm.Penalties = pens[id]
			t.Jammers = append(t.Jammers, jm)
		}
		for id, pv := range pivots[i] {
			pv.Penalties = pens[id]
			t.Pivots = append(t.Pivots, pv)
		}
		for id, bl := range blockers[i] {
			bl.Penalties = pens[id]
			t.Blockers = append(t.Blockers, bl)
		}
		sort.Slice(t.Jammers, func(a, b int) bool { return byNumber(t.Jammers[a].Skater, t.Jammers[b].Skater) })
		sort.Slice(t.Pivots, func(a, b int) bool { return byNumber(t.Pivots[a].Skater, t.Pivots[b].Skater) })
		sort.Slice(t.Blockers, func(a, b int) bool { return byNumber(t.Blockers[a].Skater, t.Blockers[b].Skater) })
	}
	out.Teams = teams
	return out
}
