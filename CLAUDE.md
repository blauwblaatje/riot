# RIOT

Roller derby Insights On Tournaments. `README.md` describes the features,
the API and the code layout (section "Code"); keep it up to date with
changes.

- `make test` (vet + tests, `-mod=vendor`); `make run` serves on :4201 with data in `data/`.
- `crgformat` is vendored from `../crg-format` (`replace` in go.mod). After changing crg-format: `make vendor`, and commit the vendor refresh separately.
- Tests with real data skip without it: `../statsbooks/rotterdam` (TestRotterdamSeasons: 29 games, Rotterdam's A team under 9 spellings), `../old-games`, `../statsbooks/2026-postseason`.
- Teams are league + team name folded (`store.TeamID`); A and B teams stay separate (the user wants teams, not leagues). Hand merges are `TeamGroups` in `namespace.json`. Seasons: club Aug–Jul, WFTDA two years from 1 June 2024.
- Front end: plain JS in `internal/web/static` (`app.js`, `teams.js`), no build step.
- Commit on main.
