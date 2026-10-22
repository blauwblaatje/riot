# RIOT: Roller derby Insights On Tournaments

A website for the stats of roller derby tournaments, from the files a
tournament produces anyway: the **CRG scoreboard's game files** (Java,
.json) and the **WFTDA statsbooks** (.xlsx).

1. Make a **namespace** (a name; its address is the key, so keep the link).
2. Upload scoreboard files, statsbooks, or a zip of them, and say which
   **tournament** they're from, or leave that empty to use the tournament
   each game names on its IGRF. You can change a file's tournament later.
3. Pick one tournament, several, or everything, and look:

| Tab | What | From |
|---|---|---|
| Overview, Skaters, Teams, Games | leaderboards (scorers, points per jam, lead %, +/−, star passes), team results, comebacks, lead changes, jam scores; as the CRG Go scoreboard's tournament stats (`crgformat/stats`) | the statsbook if the game has one, else the scoreboard file |
| A game (click its score on the Games tab) | the score after every jam as a chart, points per jam per team with the lead jammer, points and penalties per period, penalties per code, and per team its jammers (jams, lead, call-offs, points, +/−, star passes, penalties), blockers and pivots, and the pivots who took the star; time on track when the jams have real lengths (a scoreboard file, or a statsbook with its game clock sheet filled in) | the statsbook if the game has one, else the scoreboard file |
| Timing | jam lengths, time to lead, when lead jammers call it off, when penalties are called, lineup time, timeouts and their length, quickest jammers to lead, game length | **scoreboard files only**: a statsbook has no times, so games with only a statsbook are left out, and the tab says so |
| Crews | referee and NSO crews, their penalties per game and per code, against what the same teams got from the other crews of the tournament; official reviews, official timeouts; for NSO crews the corrections in their statsbooks | both |
| Corrections | what the statsbook shows differently from the scoreboard file, per area (score, lineups, box, penalties, roster, officials), the most common kinds ("box symbols `- SP:$` → `SP:+`", "penalty code B → F") and every game's list | games with both |
| Files | uploads, each file's games, its tournament, leave a file out, delete | |

Several tournaments together (say European Play-offs and Champs) give one
set of stats over all their games; teams are recognised across games by
league and roster, as in the scoreboard's tournament stats.

## Teams over seasons

The namespace page has two views. **Tournaments** is the above.
**Teams** follows one team over its seasons, with every game in the
namespace, whatever its tournament:

- **Club seasons** run from 1 August to 31 July ("2025/26"); **WFTDA
  seasons** run two years from 1 June, the first from 1 June 2024
  ("2024–2026"); earlier games are "Before June 2024". A game without a
  date has no season and is listed apart.
- Per season: games, wins and losses, points for and against, margin, lead
  jammer %, points per jam, penalties, with charts over the seasons, and per
  season the games and the team's skaters.
- A team is its league and team name as the files write them; capitals,
  accents, punctuation and spaces don't count, and a team name that is the
  league's is no team name. A league's A and B teams are separate teams.
- **Team names** merges the spellings of one team (a typo, "Rotterdam RD",
  or a team that changed its name, as the Death Row Honeys became the
  Harbour Honeys) under one name; it suggests teams with mostly the same
  skater numbers that never played each other. A merge can be renamed and
  split again, is kept in `namespace.json`, and counts in the tournament
  stats too.

## How games are put together

A game is identified by tournament, date and game number, or, without a
game number, by its teams (names, else skater numbers). A game can come
from several files: data exports hold many copies of a game (prepared,
half-way, finished), and statsbooks get saved more than once. RIOT uses:

- the **scoreboard file with the most in it** (jams, trips, lineups,
  penalties);
- the **statsbook uploaded last**; of statsbooks uploaded together, a
  browser's `name (2).xlsx` counts as newer than `name (1).xlsx` and
  `name.xlsx`;
- for the stats, the statsbook when there is a played one, since that's
  where the corrections are.

Prepared games and statsbooks filled in before the game (no jams) are
listed with their file but left out. "Leave out" on the Files tab makes RIOT
use another save of a game.

## Corrections

For a game with both files, the scoreboard file and the statsbook are each
turned into what a statsbook shows (`crgformat/statsbook` reads the xlsx;
the scoreboard file goes through `crgformat/legacy`) and compared fact by
fact (`internal/analysis/compare.go`):

- jams in one but not the other; a statsbook missing the last jams counts
  once, as "statsbook stops early" (saved during the game);
- per team and jam: lead, lost, call, no pivot, star pass, NI, points per
  trip, OS offset;
- lineups: who skated, as jammer, pivot or blocker; **the blockers' order
  isn't compared** (it means nothing on the lineup sheet); box symbols per
  skater;
- penalties: paired as the same penalty with another code, then another
  skater, then another jam, the rest added or removed; the order of
  penalties in a jam isn't compared; expulsions;
- the IGRF: skaters (a renumbered skater is one change, matched by name,
  not a change in every jam they skated), captain and not-skating marks,
  officials and their roles, head referee and head NSO.

An unedited statsbook exported by the Java scoreboard compares as equal to
its game file (checked on the MRDWC 2026 test games), so what's left are
the edits. A statsbook saved before the post-game check shows none.

## Crews

Neither file says which crew worked a game, so crews are recognised by
their people: within a tournament, games whose referees (or NSOs) are at
least half the same people as a crew's are that crew. Names are compared
ignoring case and spacing. A crew is named after its most frequent head.
Games without officials on the IGRF are listed as left out.

**Expected penalties.** Who a crew officiated matters, so each crew's
calls are compared with what the same teams got from the tournament's
other crews: per team and penalty code, the team's penalties per jam in
the other crews' games, pulled towards the average of all teams by 30 jams
(a team that hardly played elsewhere counts as average), times the team's
jams with this crew. The colour of a cell shows observed against expected,
faded when the difference is less than about two standard deviations
(Poisson). It takes out part of the "it depends on the teams", not all.

## Running it

```sh
make run            # bin/riot on :4201, namespaces in data/, logs in logs/
make test           # needs ../old-games for the tests on real games (skipped without)
```

| Flag | Environment | Default | |
|---|---|---|---|
| `-addr` | `RIOT_ADDR` | `:4201` | where to listen |
| `-data` | `RIOT_DATA` | `data` | where namespaces are kept |
| `-logs` | `RIOT_LOGS` | `logs` | `riot.log`, and `<id>.log` per namespace |

On disk, per namespace `data/ns/<id>/`: `namespace.json` (name, files, what
was read from them), `files/` (the uploads as they were) and `read/` (the
games read from each file, gzipped, so a restart doesn't read every file
again). A namespace in use is held in memory and dropped after 20 minutes
unused; about 150 MB for the 104 test files (60 games).

**Logs.** Everything to `logs/riot.log` (moved to `riot.log.1` at 10 MB) and
standard error; what concerns a namespace also to `logs/<id>.log`: uploads
(file, size, tournament, games read, errors), failed requests with their
answer, and errors the page ran into in the browser.

There is no login: the namespace's address is the key, as with RTFM.

## Container and k3s

```sh
make image                         # docker build
docker compose up -d               # http://localhost:4201
make k3s-import                    # or push to a registry and set it in deploy/k8s/kustomization.yaml
kubectl apply -k deploy/k8s        # namespace riot, 5 Gi volume, Traefik ingress (change the host)
```

The image is distroless, runs as non-root with a read-only root file
system, and keeps everything in `/data`. `make image-multi` builds amd64
and arm64.

## Code

| | |
|---|---|
| `cmd/riot` | the server |
| `internal/store` | namespaces, uploads (json, xlsx, zip), grouping files into games, teams and seasons (`teams.go`) |
| `internal/analysis` | corrections (`compare.go`), crews (`crews.go`), timing (`timing.go`) |
| `internal/web` | the site: one page per namespace (`static/app.js`, the Teams view in `static/teams.js`), JSON API under `/n/<id>/api` |

The reading of files and the tournament stats come from the CRG Go rewrite
(`../crg-format`, module `crgformat`), vendored in `vendor/crgformat` so the
repository builds on its own; `make vendor` refreshes it.

API, for scripts: `GET /n/<id>/api` (files, tournaments, games), `POST
/n/<id>/api/files` (multipart: `tournament`, `files`…), `PATCH|DELETE
/n/<id>/api/files/<fid>`, `GET /n/<id>/api/stats?t=<tournament>&t=…&date=`,
`GET /n/<id>/api/games/<gid>` (a game's stats, score after every jam, and
corrections), `GET /n/<id>/api/teams` (teams, merged teams, suggestions),
`PUT /n/<id>/api/teams` (`{"groups": [{"name", "members": [team ids]}]}`,
replaces all merges), `GET /n/<id>/api/seasons?team=<key>&kind=club|wftda`.

GPL-3.
