package store

import (
	"crgformat/replay"

	"riot/internal/analysis"
)

// Summaries is what the games' stats are made from (Game.Stats); their ids
// are the games' RIOT ids, so the stats' games can be found again.
func Summaries(gs []*analysis.Game) []*replay.Summary {
	out := make([]*replay.Summary, 0, len(gs))
	for _, g := range gs {
		if g.Stats != nil {
			out = append(out, g.Stats)
		}
	}
	return out
}
