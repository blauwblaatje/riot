package analysis

import (
	"cmp"
	"sort"

	"crgformat/replay"
)

// Game is one game as RATS knows it: from a scoreboard file, a statsbook,
// or both.
type Game struct {
	ID         string
	Tournament string
	Label      string // "2026-06-12 #3 Nantes – Stockholm"
	// Stats is what the stats are made from: the statsbook when there is a
	// played one (it holds the corrections), else the scoreboard file.
	Stats *replay.Summary
	// Scoreboard is the scoreboard's game file, nil if there is none with a
	// jam played. Times only come from here.
	Scoreboard *replay.Summary
	// Changes: what the statsbook shows differently from the scoreboard
	// file; Compared is false unless the game has both.
	Changes  []*Change
	Compared bool
}

// --- common fixes ---------------------------------------------------------------------

// Fixes counts what the statsbooks changed compared with the scoreboard
// files, over the games that have both.
type Fixes struct {
	Compared    int        `json:"compared"`    // games with both
	WithChanges int        `json:"withChanges"` // of those, games with a change
	Changes     int        `json:"changes"`
	Areas       []*AreaFix `json:"areas"`
	Kinds       []*KindFix `json:"kinds"`
	Games       []*GameFix `json:"games"`
}

// AreaFix is one area of the paperwork.
type AreaFix struct {
	Area    string `json:"area"`
	Changes int    `json:"changes"`
	Games   int    `json:"games"`
}

// KindFix is one kind of change, with what it was ("B → F").
type KindFix struct {
	Area     string   `json:"area"`
	Kind     string   `json:"kind"`
	Changes  int      `json:"changes"`
	Games    int      `json:"games"`
	Patterns []*Count `json:"patterns,omitempty"`
}

// Count is a value and how often it was.
type Count struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

// GameFix is one compared game.
type GameFix struct {
	ID         string         `json:"id"`
	Label      string         `json:"label"`
	Tournament string         `json:"tournament"`
	Changes    int            `json:"changes"`
	ByArea     map[string]int `json:"byArea"`
}

// CommonFixes adds up the changes of the compared games.
func CommonFixes(games []*Game) *Fixes {
	f := &Fixes{Areas: []*AreaFix{}, Kinds: []*KindFix{}, Games: []*GameFix{}}
	areas := map[string]*AreaFix{}
	kinds := map[string]*KindFix{}
	patterns := map[*KindFix]map[string]int{}
	for _, g := range games {
		if !g.Compared {
			continue
		}
		f.Compared++
		gf := &GameFix{ID: g.ID, Label: g.Label, Tournament: g.Tournament, Changes: len(g.Changes), ByArea: map[string]int{}}
		f.Games = append(f.Games, gf)
		if len(g.Changes) > 0 {
			f.WithChanges++
		}
		seenArea, seenKind := map[string]bool{}, map[string]bool{}
		for _, c := range g.Changes {
			f.Changes++
			gf.ByArea[c.Area]++
			a := areas[c.Area]
			if a == nil {
				a = &AreaFix{Area: c.Area}
				areas[c.Area] = a
				f.Areas = append(f.Areas, a)
			}
			a.Changes++
			if !seenArea[c.Area] {
				seenArea[c.Area] = true
				a.Games++
			}
			kk := c.Area + "/" + c.Kind
			k := kinds[kk]
			if k == nil {
				k = &KindFix{Area: c.Area, Kind: c.Kind}
				kinds[kk] = k
				patterns[k] = map[string]int{}
				f.Kinds = append(f.Kinds, k)
			}
			k.Changes++
			if !seenKind[kk] {
				seenKind[kk] = true
				k.Games++
			}
			if p := c.Pattern(); p != "" {
				patterns[k][p]++
			}
		}
	}
	for k, ps := range patterns {
		k.Patterns = counts(ps, 12)
	}
	sort.SliceStable(f.Areas, func(a, b int) bool { return f.Areas[a].Changes > f.Areas[b].Changes })
	sort.SliceStable(f.Kinds, func(a, b int) bool {
		x, y := f.Kinds[a], f.Kinds[b]
		return x.Changes > y.Changes || x.Changes == y.Changes && x.Kind < y.Kind
	})
	return f
}

// counts sorts a tally, most first, and keeps the top n.
func counts(m map[string]int, n int) []*Count {
	out := make([]*Count, 0, len(m))
	for v, c := range m {
		out = append(out, &Count{Value: v, Count: c})
	}
	sort.Slice(out, func(a, b int) bool {
		return cmp.Or(out[b].Count-out[a].Count, cmp.Compare(out[a].Value, out[b].Value)) < 0
	})
	if n > 0 && len(out) > n {
		out = out[:n]
	}
	return out
}
