package analysis

import (
	"encoding/json"
	"os"
	"testing"

	"crgformat"
	"crgformat/legacy"
	"crgformat/replay"
)

func game18(t *testing.T) *replay.Summary {
	t.Helper()
	data, err := os.ReadFile("../../../old-games/crg-game-2026-04-30_Game_18__Team_Italy_vs._Team_Japan_Roller_Derby_(Finished__222_-_2).json")
	if err != nil {
		t.Skip("no ../../../old-games")
	}
	v, err := crgformat.NewValidators()
	if err != nil {
		t.Fatal(err)
	}
	r, err := legacy.Convert(data, "game18.json", v)
	if err != nil || r.Summary == nil {
		t.Fatal(err)
	}
	return r.Summary
}

// clone deep-copies a summary.
func clone(t *testing.T, s *replay.Summary) *replay.Summary {
	b, _ := json.Marshal(s)
	var c replay.Summary
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	return &c
}

func TestCompare(t *testing.T) {
	s := game18(t)
	if cs := Compare(s, clone(t, s)); len(cs) != 0 {
		t.Fatalf("a game differs from itself: %+v", cs[0])
	}

	// Another penalty code, swapped blockers (no change), and a trip's
	// points.
	c := clone(t, s)
	c.Penalties[0].Code = "Z"
	// (in a jam without box trips: a box trip records the position)
	inBox := map[string]bool{}
	for _, bt := range c.BoxTrips {
		inBox[bt.Start.Jam] = true
		if bt.End != nil {
			inBox[bt.End.Jam] = true
		}
	}
	for _, j := range c.Periods[0].Jams {
		if !inBox[j.ID] {
			tj := j.Teams[0]
			tj.Lineup["Blocker1"], tj.Lineup["Blocker2"] = tj.Lineup["Blocker2"], tj.Lineup["Blocker1"]
			break
		}
	}
	var tripJam string
	for _, j := range c.Periods[0].Jams {
		if tr := j.Teams[1].Trips; len(tr) > 1 && !tr[1].AfterStarPass {
			tr[1].Points++
			tripJam = "P1 J" + itoa(j.Number)
			break
		}
	}
	cs := Compare(s, c)
	kinds := map[string]*Change{}
	for _, ch := range cs {
		kinds[ch.Kind] = ch
	}
	if len(cs) != 2 || kinds["penalty code changed"] == nil || kinds["trip points changed"] == nil {
		for _, ch := range cs {
			t.Logf("%+v", *ch)
		}
		t.Fatalf("%d changes, want a penalty code and trip points", len(cs))
	}
	if p := kinds["penalty code changed"]; p.To != "Z" || p.Pattern() != p.From+" → Z" {
		t.Errorf("penalty change %+v", *p)
	}
	if p := kinds["trip points changed"]; p.Pattern() != "+1" || p.Where != tripJam+" trip 2" {
		t.Errorf("trip change %+v (%s)", *p, tripJam)
	}

	// A statsbook saved in the middle of the game: one change.
	c = clone(t, s)
	last := c.Periods[len(c.Periods)-1]
	n := len(last.Jams)
	gone := map[string]bool{}
	for _, j := range last.Jams[n-5:] {
		gone[j.ID] = true
	}
	last.Jams = last.Jams[:n-5]
	var pens []*replay.Penalty
	for _, p := range c.Penalties {
		if !gone[p.Jam] {
			pens = append(pens, p)
		}
	}
	c.Penalties = pens
	cs = Compare(s, c)
	if len(cs) != 1 || cs[0].Kind != "statsbook stops early" {
		t.Errorf("early statsbook: %d changes, first %+v", len(cs), cs[0])
	}
}

func TestExpected(t *testing.T) {
	// Team a gets 1 penalty per jam with other crews, team b none; in the
	// crew's game of 10 jams, a is expected 10 (pulled a little towards the
	// average), b a little.
	games := map[string]*teamGame{
		"x": {jams: 10, teams: [2]string{"a", "b"}, pens: [2]map[string]int{{"B": 12}, {}}},
		"y": {jams: 100, teams: [2]string{"a", "c"}, pens: [2]map[string]int{{"B": 100}, {}}},
		"z": {jams: 100, teams: [2]string{"b", "c"}, pens: [2]map[string]int{{}, {}}},
	}
	cr := &Crew{ExpectedCodes: map[string]float64{}}
	expected(cr, map[string]bool{"x": true}, games, 30)
	// average outside: 100 B in 400 team-jams = 0.25; a: (100+7.5)/130,
	// b: 7.5/130; times 10 jams each.
	want := 10 * (107.5/130 + 7.5/130)
	if !cr.HasExpected || cr.Expected < want-0.01 || cr.Expected > want+0.01 {
		t.Errorf("expected %.2f, want %.2f", cr.Expected, want)
	}
}
