package store

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"crgformat/replay"
	"crgformat/stats"
)

func TestTeamID(t *testing.T) {
	for _, c := range []struct {
		league, team, want string
	}{
		{"Rotterdam Roller Derby", "Harbour Honeys", "rotterdam roller derby|harbour honeys"},
		{"Rotterdam roller derby", "Harbour  Honeys", "rotterdam roller derby|harbour honeys"},
		{"Huiñas Roller Derby", "", "huinas roller derby|"},
		{"Huinas Roller-Derby", "Huinas Roller Derby", "huinas roller derby|"},
		{"", "Oslo Roller Derby", "oslo roller derby|"},
		{"Auverniak's Roller Derby", "Les Auver'Niaks", "auverniak s roller derby|les auver niaks"},
	} {
		if got := TeamID(&replay.Team{League: c.league, TeamName: c.team}); got != c.want {
			t.Errorf("TeamID(%q, %q) = %q, want %q", c.league, c.team, got, c.want)
		}
	}
}

func TestSeasonOf(t *testing.T) {
	for _, c := range []struct{ kind, date, want string }{
		{SeasonClub, "2025-07-31", "2024/25"},
		{SeasonClub, "2025-08-01", "2025/26"},
		{SeasonClub, "1999-12-31", "1999/00"},
		{SeasonWFTDA, "2024-05-31", "Before June 2024"},
		{SeasonWFTDA, "2024-06-01", "2024–2026"},
		{SeasonWFTDA, "2026-05-31", "2024–2026"},
		{SeasonWFTDA, "2026-06-01", "2026–2028"},
		{SeasonWFTDA, "2029-01-10", "2028–2030"},
	} {
		if got, _, _, _ := SeasonOf(c.kind, c.date); got != c.want {
			t.Errorf("SeasonOf(%s, %s) = %q, want %q", c.kind, c.date, got, c.want)
		}
	}
	if _, _, _, ok := SeasonOf(SeasonClub, ""); ok {
		t.Error("a game without a date has a season")
	}
}

// TestRotterdamSeasons: Rotterdam's A team, from 2023 to 2026 under nine
// spellings (Death Row Honeys became Harbour Honeys), in
// ../../../statsbooks/rotterdam.
func TestRotterdamSeasons(t *testing.T) {
	files, _ := filepath.Glob("../../../statsbooks/rotterdam/*.xlsx")
	if len(files) == 0 {
		t.Skip("no ../../../statsbooks/rotterdam")
	}
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ns, err := s.Create("Rotterdam")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ns.Add(filepath.Base(f), data, "", time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	tl := ns.Teams()
	var rdam *Suggestion
	for _, sg := range tl.Suggestions {
		if strings.HasPrefix(sg.Teams[0], "rotterdam") {
			rdam = sg
		}
	}
	if rdam == nil || len(rdam.Teams) != 8 {
		t.Fatalf("no suggestion with Rotterdam's 8 spellings: %+v", tl.Suggestions)
	}
	var members []string
	for _, k := range rdam.Teams {
		members = append(members, tl.Get(k).Members...)
	}
	if err := ns.SetTeamGroups([]*TeamGroup{{Name: " Rotterdam  Harbour Honeys", Members: members}}); err != nil {
		t.Fatal(err)
	}
	if err := ns.SetTeamGroups([]*TeamGroup{{Name: "a", Members: members[:1]}, {Name: "b", Members: members[:1]}}); err == nil {
		t.Error("a team in two merged teams")
	}

	// Kept: open the namespace again from disk.
	s2, _ := Open(dir)
	ns2, err := s2.Get(ns.ID)
	if err != nil {
		t.Fatal(err)
	}
	tl = ns2.Teams()
	team := tl.List[0]
	if !team.Group || team.Name != "Rotterdam Harbour Honeys" || team.Games != 29 || team.First != "2023-04-29" || len(team.Spellings) != 9 {
		t.Fatalf("merged team: %+v", team)
	}
	if slices.ContainsFunc(tl.Suggestions, func(sg *Suggestion) bool { return slices.Contains(sg.Teams, team.Key) }) {
		t.Error("the merged team is still suggested")
	}

	club, undated, err := ns2.Seasons(team.Key, SeasonClub)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][3]int{"2025/26": {8, 1, 7}, "2024/25": {8, 6, 2}, "2023/24": {7, 6, 1}, "2022/23": {6, 3, 3}}
	if len(club) != 4 || len(undated) != 0 || club[0].Label != "2025/26" {
		t.Fatalf("club seasons: %d, undated %d", len(club), len(undated))
	}
	for _, se := range club {
		w := want[se.Label]
		if got := [3]int{se.Team.Games, se.Team.Wins, se.Team.Losses}; got != w {
			t.Errorf("%s: games, wins, losses %v, want %v", se.Label, got, w)
		}
		if len(se.Games) != se.Team.Games || len(se.Skaters) < 14 {
			t.Errorf("%s: %d games listed, %d skaters", se.Label, len(se.Games), len(se.Skaters))
		}
		for _, sk := range se.Skaters {
			if sk.Team != team.Key {
				t.Errorf("%s: skater of %s", se.Label, sk.Team)
			}
		}
	}
	wftda, _, _ := ns2.Seasons(team.Key, SeasonWFTDA)
	var labels []string
	n := 0
	for _, se := range wftda {
		labels = append(labels, se.Label)
		n += se.Team.Games
	}
	if strings.Join(labels, ",") != "2026–2028,2024–2026,Before June 2024" || n != 29 {
		t.Errorf("WFTDA seasons %v, %d games", labels, n)
	}

	// The tournament stats count the merged team as one.
	var games []*replay.Summary
	for _, g := range ns2.Games().List {
		games = append(games, g.Stats)
	}
	for _, tt := range stats.ComputeTournament("all", games).Teams {
		if strings.Contains(strings.ToLower(tt.Name), "rotterdam") && tt.Games != 29 {
			t.Errorf("tournament stats: %s has %d games", tt.Name, tt.Games)
		}
	}
	if _, _, err := ns2.Seasons("nobody|", SeasonClub); err != ErrNotFound {
		t.Errorf("unknown team: %v", err)
	}
}
