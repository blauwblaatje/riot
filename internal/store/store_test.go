package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"crgformat/stats"

	"rats/internal/analysis"
)

// loadOldGames uploads the test games in ../../../old-games (MRDWC 2026
// and 2026 Regionals Europe: Java game files, data exports, statsbooks).
func loadOldGames(t *testing.T) *Namespace {
	t.Helper()
	files, _ := filepath.Glob("../../../old-games/*")
	more, _ := filepath.Glob("../../../old-games/datasets/*.json")
	files = append(files, more...)
	if len(files) == 0 {
		t.Skip("no ../../../old-games")
	}
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ns, err := s.Create("Post-season")
	if err != nil {
		t.Fatal(err)
	}
	batch := time.Now()
	for _, f := range files {
		if st, err := os.Stat(f); err != nil || st.IsDir() {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ns.Add(filepath.Base(f), data, "", batch); err != nil {
			t.Logf("%s: %v", f, err)
		}
	}
	return ns
}

func TestOldGames(t *testing.T) {
	ns := loadOldGames(t)
	gs := ns.Games()
	byT := map[string]int{}
	var both int
	var all []*analysis.Game
	for _, g := range gs.List {
		byT[g.Tournament]++
		if g.Compared {
			both++
		}
		all = append(all, &g.Game)
		t.Logf("%-40s %-60s sb=%v xlsx=%v changes=%d", g.Tournament, g.Label, g.UsedScoreboard != nil, g.UsedStatsbook != nil, len(g.Changes))
	}
	t.Logf("tournaments %v, compared %d", byT, both)
	if byT["MRDWC 2026"] < 30 || byT["2026 WFTDA Regional Championships Europe"] != 17 {
		t.Errorf("games per tournament: %v", byT)
	}
	// Paris – Crime City: three statsbooks; the last one, "(2)", is the
	// complete one and has no corrections.
	for _, g := range gs.List {
		if g.Tournament == "2026 WFTDA Regional Championships Europe" && g.GameNo == "16" {
			if g.UsedStatsbook == nil || g.UsedStatsbook.File.Name != "STATS-2026-06-14_ParisRollerDerby_vs_CrimeCityRollers (2).xlsx" {
				t.Errorf("game 16 uses statsbook %+v", g.UsedStatsbook)
			}
			if len(g.Changes) != 0 {
				t.Errorf("game 16 changes: %d", len(g.Changes))
			}
		}
	}
	tn := stats.ComputeTournament("all", Summaries(all))
	fixes := analysis.CommonFixes(all)
	t.Logf("fixes: %d compared, %d with changes, %d changes", fixes.Compared, fixes.WithChanges, fixes.Changes)
	for _, k := range fixes.Kinds {
		t.Logf("  %-10s %-30s %3d in %2d games %v", k.Area, k.Kind, k.Changes, k.Games, k.Patterns)
	}
	for _, kind := range []string{analysis.KindReferee, analysis.KindNSO} {
		cs := analysis.ComputeCrews(all, tn, kind)
		for _, c := range cs.Crews {
			t.Logf("%s crew %-25s %-40s games %d pen %d exp %.1f fixes %d/%d members %d", kind, c.Head, c.Tournament, len(c.Games), c.Penalties, c.Expected, c.Fixes, c.Compared, len(c.Members))
		}
		t.Logf("unknown %d", len(cs.Unknown))
	}
	tm := analysis.ComputeTiming(all, tn)
	t.Logf("timing: %d games %d jams; jam mean %.0f lead mean %.0f lineup mean %.0f, penaltyAt n=%d, timeouts %+v %+v %+v",
		tm.Games, tm.Jams, tm.JamLength.Mean, tm.LeadAt.Mean, tm.Lineup.Mean, tm.PenaltyAt.N, *tm.Timeouts["team"], *tm.Timeouts["official"], *tm.Timeouts["review"])
}
