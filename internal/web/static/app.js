"use strict";
// A namespace: its tournaments (pick one or more), the stats of the picked
// ones, the crews, what the statsbooks corrected, and the uploaded files.

const $ = (id) => document.getElementById(id);
const esc = (s) => String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
const NS = document.body.dataset.ns;
const api = (p) => "/n/" + NS + "/api" + p;
const params = new URLSearchParams(location.search);

let N = null;    // the namespace (files, tournaments, games)
let S = null;    // the stats of the selection
let picked = params.getAll("t");
let day = params.get("date") || "";
let tab = params.get("tab") || "";
let crewKind = "referee";
const filter = { team: "", minJams: null };
const sorts = {};

window.addEventListener("error", (e) => report(e.message, e.filename, e.lineno, e.colno, e.error && e.error.stack));
window.addEventListener("unhandledrejection", (e) => report(String(e.reason && e.reason.message || e.reason), "", 0, 0, e.reason && e.reason.stack));
function report(message, source, line, column, stack) {
  try {
    fetch("/n/" + NS + "/log", { method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ message, source, line, column, stack: stack || "", page: location.href, agent: navigator.userAgent }) });
  } catch (e) { /* nothing */ }
}

async function getJSON(url, opts) {
  const r = await fetch(url, opts);
  if (!r.ok) throw new Error((await r.text()).trim() || r.statusText);
  return r.status === 204 ? null : r.json();
}

// --- loading ---------------------------------------------------------------------------

async function loadNS() {
  N = await getJSON(api(""));
  remember();
  const names = N.tournaments.map((t) => t.name);
  picked = picked.filter((t) => names.includes(t));
  if (!tab) tab = N.games.length ? "overview" : "files";
  renderTours();
  await loadStats();
}

function remember() {
  try {
    let list = JSON.parse(localStorage.getItem("riot.recent") || "[]");
    if (!Array.isArray(list)) list = [];
    list = [{ id: NS, name: N.name }, ...list.filter((r) => r.id !== NS)].slice(0, 20);
    localStorage.setItem("riot.recent", JSON.stringify(list));
  } catch (e) { /* private window */ }
}

async function loadStats() {
  const q = new URLSearchParams();
  picked.forEach((t) => q.append("t", t));
  if (day && picked.length === 1) q.set("date", day); else day = "";
  S = await getJSON(api("/stats?" + q));
  const u = new URL(location.href);
  u.searchParams.delete("t");
  picked.forEach((t) => u.searchParams.append("t", t));
  if (day) u.searchParams.set("date", day); else u.searchParams.delete("date");
  u.searchParams.set("tab", tab);
  history.replaceState(null, "", u);
  filter.team = "";
  render();
}

// --- tournaments --------------------------------------------------------------------------

function renderTours() {
  const ts = N.tournaments.filter((t) => t.games > 0);
  if (!ts.length) { $("tours").innerHTML = ""; return; }
  const all = !picked.length;
  $("tours").innerHTML = `<label class="tour ${all ? "on" : ""}"><input type="radio" name="tAll" data-all ${all ? "checked" : ""}>
      <span><b>Everything</b><small>${ts.reduce((n, t) => n + t.games, 0)} games, ${ts.length} ${ts.length === 1 ? "tournament" : "tournaments"}</small></span></label>` +
    ts.map((t) => {
      const on = picked.includes(t.name);
      return `<label class="tour ${on ? "on" : ""}"><input type="checkbox" data-t="${esc(t.name)}" ${on ? "checked" : ""}>
        <span><b>${esc(t.name)}</b><small>${t.games} games · ${t.scoreboard} scoreboard · ${t.statsbook} statsbook${t.dates.length ? ` · ${esc(dateRange(t.dates))}` : ""}</small></span></label>`;
    }).join("");
  if (picked.length === 1) {
    const t = N.tournaments.find((x) => x.name === picked[0]);
    if (t && t.dates.length > 1) {
      $("tours").insertAdjacentHTML("beforeend", `<select id="day" aria-label="Day"><option value="">All days</option>${t.dates.map((d, i) =>
        `<option value="${d}" ${d === day ? "selected" : ""}>Day ${i + 1} · ${esc(dayName(d))}</option>`).join("")}</select>`);
    }
  }
}

function dateRange(ds) { return ds.length === 1 ? ds[0] : `${ds[0]} – ${ds[ds.length - 1]}`; }
function dayName(d) {
  const t = new Date(d + "T12:00:00");
  return isNaN(t) ? d : t.toLocaleDateString(undefined, { weekday: "short", day: "numeric", month: "short" });
}

// --- numbers --------------------------------------------------------------------------------

const T = () => S.stats;
const teamName = (key) => (T().teams.find((t) => t.key === key) || { name: key }).name;
const totalPoints = (s) => s.jammerPoints + s.pivotPoints;
const pct = (a, b) => (b ? (100 * a) / b : 0);
const per = (a, b) => (b ? a / b : 0);
const f1 = (x) => x.toFixed(1), f2 = (x) => x.toFixed(2);
const signed = (x, d = 2) => (x > 0 ? "+" : x < 0 ? "−" : "") + Math.abs(x).toFixed(d);
const who = (s) => `#${esc(s.number)} ${esc(s.name || "")} <small>(${esc(teamName(s.team))})</small>`;
const secs = (ms) => (ms / 1000).toFixed(1) + " s";
function clock(ms) {
  const s = Math.round(ms / 1000);
  return Math.floor(s / 60) + ":" + String(s % 60).padStart(2, "0");
}
function hm(ms) {
  const m = Math.round(ms / 60000);
  return m >= 60 ? `${Math.floor(m / 60)} h ${String(m % 60).padStart(2, "0")}` : `${m} min`;
}
const gameLabel = (id) => (S.gameInfo[id] || { label: id }).label;

function defaultMin(field) {
  const most = Math.max(0, ...T().skaters.map((s) => s[field]));
  return Math.max(5, Math.round(most / 4));
}
const skaters = () => T().skaters.filter((s) => !filter.team || s.team === filter.team);

// --- charts --------------------------------------------------------------------------------

// bars: a ranked list of rows {html, value, text, tip}; signed: a zero line,
// positive blue to the right, negative red to the left.
function bars(rows, opts = {}) {
  if (!rows.length) return `<p class="muted">${opts.empty || "Nothing yet."}</p>`;
  const max = Math.max(...rows.map((r) => Math.abs(r.value)), 1e-9);
  const neg = opts.signed && rows.some((r) => r.value < 0);
  const zero = neg ? 50 : 0, span = neg ? 50 : 100;
  return `<div class="bars" role="list">${rows.map((r) => {
    const w = (Math.abs(r.value) / max) * span;
    const left = r.value < 0 ? zero - w : zero;
    return `<div class="row" role="listitem" tabindex="0" data-tip="${esc(r.tip || "")}"><span class="who">${r.html}</span>
      <span class="track">${neg ? `<span class="zero" style="left:${zero}%"></span>` : ""}<span class="bar ${r.value < 0 ? "neg" : ""}" style="left:${left}%;width:${w}%"></span></span>
      <span class="val">${r.text}</span></div>`;
  }).join("")}</div>`;
}

const ranked = (list, value, n = 15) => list.filter((x) => value(x) !== null).sort((a, b) => value(b) - value(a)).slice(0, n);
const section = (title, note, body) => `<section><h2>${title}</h2>${note ? `<p class="note">${note}</p>` : ""}${body}</section>`;
const tiles = (list) => `<div class="tiles">${list.map(([l, v, sub]) => `<div class="tile"><div class="label">${l}</div><div class="value">${v}</div>${sub ? `<div class="sub">${sub}</div>` : ""}</div>`).join("")}</div>`;

// hist draws a histogram of durations: h = {step, bins, n} (ms); unit
// labels every other bin; the last bin is "and more".
function hist(h, what) {
  if (!h || !h.n) return `<p class="muted">No times recorded.</p>`;
  const max = Math.max(...h.bins, 1);
  const lab = (i) => (i === h.bins.length - 1 ? clock(i * h.step) + "+" : clock(i * h.step));
  const cols = h.bins.map((n, i) => {
    const range = i === h.bins.length - 1 ? `${clock(i * h.step)} or more` : `${clock(i * h.step)} – ${clock((i + 1) * h.step)}`;
    return `<div class="col" tabindex="0" data-tip="${esc(`${n} ${what} (${f1(pct(n, h.n))}%)\n${range}`)}"><span style="height:${(100 * n) / max}%"></span></div>`;
  }).join("");
  const axis = h.bins.map((_, i) => `<span>${i % 2 === 0 || i === h.bins.length - 1 ? lab(i) : ""}</span>`).join("");
  return `<div class="hist" role="img" aria-label="${esc(what)} by time">${cols}</div><div class="hist-axis">${axis}</div>
    <p class="note">${h.n} ${what} · mean ${clock(h.mean)} · median ${clock(h.median)}</p>`;
}

// table: sortable by clicking a heading (cols: [title, sort value, cell html?, "l"]).
function table(id, cols, rows) {
  const st = sorts[id] || { col: -1, desc: true };
  if (st.col >= 0) {
    const v = cols[st.col][1];
    rows = [...rows].sort((a, b) => { const x = v(a), y = v(b); return (x < y ? -1 : x > y ? 1 : 0) * (st.desc ? -1 : 1); });
  }
  return `<div class="scroll"><table class="t" data-table="${id}"><thead><tr>${cols.map((c, i) => `<th class="${c[3] || ""} ${i === st.col ? "sorted" : ""}" data-col="${i}">${c[0]}${i === st.col ? (st.desc ? " ↓" : " ↑") : ""}</th>`).join("")}</tr></thead>
    <tbody>${rows.map((r) => `<tr>${cols.map((c) => `<td class="${c[3] || ""}">${c[2] ? c[2](r) : esc(c[1](r))}</td>`).join("")}</tr>`).join("")}</tbody></table></div>`;
}

// --- the scope line --------------------------------------------------------------------------

function scopeLine() {
  const c = S.coverage;
  if (!c.games) return "";
  const what = picked.length ? S.tournaments.map(esc).join(" + ") : "all tournaments";
  const parts = [`${c.games} games in ${what}${day ? ` on ${esc(dayName(day))}` : ""}`];
  parts.push(`${c.statsbook} with a statsbook, ${c.scoreboard} with a scoreboard file, ${c.both} with both`);
  return parts.join(" · ") + `. Stats use the statsbook when there is one (it has the corrections).`;
}

// --- tabs: stats as in the scoreboard's tournament view -----------------------------------

function boards(which, n) {
  const min = filter.minJams;
  const jMin = min ?? defaultMin("jammerJams"), bMin = min ?? defaultMin("blockerJams"), pMin = min ?? defaultMin("pivotJams");
  const SK = skaters();
  const out = {
    scorers: () => section("Leading scorers", "points scored as jammer, and as pivot after taking the star",
      bars(ranked(SK, totalPoints, n).map((s) => ({ html: who(s), value: totalPoints(s),
        text: `${totalPoints(s)}${s.pivotPoints ? ` <small>(${s.pivotPoints} as pivot)</small>` : ""}`,
        tip: `${s.jammerJams} jams as jammer, ${s.jammerPoints} points; ${s.pivotPoints} points after a star pass` })))),
    ppj: () => section("Points per jam", `jammer points per jam as jammer; at least ${jMin} jams`,
      bars(ranked(SK.filter((s) => s.jammerJams >= jMin), (s) => per(s.jammerPoints, s.jammerJams), n).map((s) => ({ html: who(s),
        value: per(s.jammerPoints, s.jammerJams), text: f2(per(s.jammerPoints, s.jammerJams)), tip: `${s.jammerPoints} points in ${s.jammerJams} jams` })))),
    lead: () => section("Lead jammer %", `jams as jammer with lead; at least ${jMin} jams`,
      bars(ranked(SK.filter((s) => s.jammerJams >= jMin), (s) => pct(s.lead, s.jammerJams), n).map((s) => ({ html: who(s),
        value: pct(s.lead, s.jammerJams), text: `${f1(pct(s.lead, s.jammerJams))}%`, tip: `lead in ${s.lead} of ${s.jammerJams} jams` })))),
    jammerPM: () => section("Average +/− as jammer", `jam score difference per jam as jammer; at least ${jMin} jams`,
      bars(ranked(SK.filter((s) => s.jammerJams >= jMin), (s) => per(s.jammerPM, s.jammerJams), n).map((s) => ({ html: who(s),
        value: per(s.jammerPM, s.jammerJams), text: signed(per(s.jammerPM, s.jammerJams)), tip: `${signed(s.jammerPM, 0)} over ${s.jammerJams} jams` })), { signed: true })),
    blockerPM: () => section("Average +/− as blocker", `jam score difference per jam as pivot or blocker; at least ${bMin} jams`,
      bars(ranked(SK.filter((s) => s.blockerJams >= bMin), (s) => per(s.blockerPM, s.blockerJams), n).map((s) => ({ html: who(s),
        value: per(s.blockerPM, s.blockerJams), text: signed(per(s.blockerPM, s.blockerJams)), tip: `${signed(s.blockerPM, 0)} over ${s.blockerJams} jams` })), { signed: true })),
    spJammer: () => section("Star pass rate, jammers", `how often the jammer passed the star; at least ${jMin} jams`,
      bars(ranked(SK.filter((s) => s.jammerJams >= jMin && s.starPasses), (s) => pct(s.starPasses, s.jammerJams), n).map((s) => ({ html: who(s),
        value: pct(s.starPasses, s.jammerJams), text: `${f1(pct(s.starPasses, s.jammerJams))}%`, tip: `${s.starPasses} star passes in ${s.jammerJams} jams` })))),
    spPivot: () => section("Star pass rate, pivots", `how often the pivot took the star; at least ${pMin} jams as pivot`,
      bars(ranked(SK.filter((s) => s.pivotJams >= pMin && s.received), (s) => pct(s.received, s.pivotJams), n).map((s) => ({ html: who(s),
        value: pct(s.received, s.pivotJams), text: `${f1(pct(s.received, s.pivotJams))}%`, tip: `took the star ${s.received} times in ${s.pivotJams} jams as pivot` })))),
    bestJam: () => section("Highest single-jam scores", "a team's points in one jam",
      bars(T().topJams.filter((j) => !filter.team || j.team === filter.team).slice(0, n).map((j) => ({
        html: `${j.number ? `#${esc(j.number)} ${esc(j.name || "")} ` : ""}<small>(${esc(teamName(j.team))}, P${j.period} J${j.jam})</small>`,
        value: j.points, text: String(j.points), tip: gameLabel(j.game) })))),
    blockerJams: () => section("Most jams as blocker", "pivot and blocker positions",
      bars(ranked(SK, (s) => s.blockerJams, n).map((s) => ({ html: who(s), value: s.blockerJams, text: String(s.blockerJams),
        tip: `${s.pivotJams} as pivot, ${s.games} games` })))),
    penPerJam: () => {
      const all = SK.filter((s) => s.jammerJams + s.blockerJams >= bMin);
      const rate = (s) => per(s.penalties, s.jammerJams + s.blockerJams);
      return section("Fewest penalties per jam", `penalties per jam on track; at least ${bMin} jams`,
        bars(all.sort((a, b) => rate(a) - rate(b) || (b.jammerJams + b.blockerJams) - (a.jammerJams + a.blockerJams)).slice(0, n).map((s) => ({ html: who(s),
          value: rate(s), text: f2(rate(s)), tip: `${s.penalties} penalties in ${s.jammerJams + s.blockerJams} jams` })), { empty: "Nobody with that many jams." }));
    },
  };
  return which.map((k) => out[k]()).join("");
}

function filtersHTML(withTeam) {
  return `<div class="filters">
    ${withTeam ? `<label>Team <select id="fTeam"><option value="">All teams</option>${T().teams.map((t) => `<option value="${esc(t.key)}" ${t.key === filter.team ? "selected" : ""}>${esc(t.name)}</option>`).join("")}</select></label>` : ""}
    <label>Minimum jams for rates <input type="number" id="fMin" min="0" value="${filter.minJams ?? ""}" placeholder="auto"></label>
  </div>`;
}

function overview() {
  const t = T(), g = t.games.length;
  return tiles([
    ["Games", g], ["Jams", t.jams], ["Points per game", f1(per(t.points, g))], ["Penalties per game", f1(per(t.penalties, g))],
    ["Jams with a lead jammer", `${Math.round(pct(t.leadJams, t.jams))}%`], ["Star passes", t.starPasses],
    ...(S.timing.games ? [["Average jam", clock(S.timing.jamLength.mean), `${S.timing.games} games with times`]] : []),
    ...(S.fixes.compared ? [["Corrections per statsbook", f1(per(S.fixes.changes, S.fixes.compared)), `${S.fixes.compared} games compared`]] : []),
  ]) + filtersHTML(false) + `<div class="grid">${boards(["scorers", "ppj", "lead", "jammerPM", "blockerPM", "bestJam"], 10)}</div>`;
}

function skatersTab() {
  return `${filtersHTML(true)}<div class="grid">${boards(["scorers", "ppj", "lead", "jammerPM", "blockerPM", "spJammer", "spPivot", "bestJam", "blockerJams", "penPerJam"], 15)}</div>
    <h2>All skaters</h2>${table("sk", skaterColumns, skaters())}`;
}

const skaterColumns = [
  ["#", (s) => s.number, (s) => esc(s.number), "l"], ["Name", (s) => s.name || "", (s) => esc(s.name || ""), "l"],
  ["Team", (s) => teamName(s.team), (s) => esc(teamName(s.team)), "l"], ["Games", (s) => s.games],
  ["Jammer jams", (s) => s.jammerJams], ["Points", totalPoints], ["Pts/jam", (s) => per(s.jammerPoints, s.jammerJams), (s) => s.jammerJams ? f2(per(s.jammerPoints, s.jammerJams)) : ""],
  ["Lead %", (s) => pct(s.lead, s.jammerJams), (s) => s.jammerJams ? f1(pct(s.lead, s.jammerJams)) : ""],
  ["Jammer +/−", (s) => per(s.jammerPM, s.jammerJams), (s) => s.jammerJams ? signed(per(s.jammerPM, s.jammerJams)) : ""],
  ["SP", (s) => s.starPasses], ["Blocker jams", (s) => s.blockerJams], ["Pivot jams", (s) => s.pivotJams], ["SP taken", (s) => s.received],
  ["Blocker +/−", (s) => per(s.blockerPM, s.blockerJams), (s) => s.blockerJams ? signed(per(s.blockerPM, s.blockerJams)) : ""],
  ["Penalties", (s) => s.penalties], ["Pen/jam", (s) => per(s.penalties, s.jammerJams + s.blockerJams), (s) => f2(per(s.penalties, s.jammerJams + s.blockerJams))],
];

function teamsTab() {
  const ts = T().teams;
  const chart = (title, note, value, text, opts) => section(title, note, bars([...ts].sort((a, b) => value(b) - value(a)).map((t) => ({ html: esc(t.name), value: value(t), text: text(t) })), opts));
  return `${table("tm", teamColumns, ts)}<div class="grid" style="margin-top:20px">
    ${chart("Average margin", "points for minus against, per game", (t) => per(t.for - t.against, t.games), (t) => signed(per(t.for - t.against, t.games), 1), { signed: true })}
    ${chart("Lead jammer %", "jams in which the team's jammer got lead", (t) => pct(t.leadJams, t.jams), (t) => `${f1(pct(t.leadJams, t.jams))}%`)}
    ${chart("Points per jam", "", (t) => per(t.for, t.jams), (t) => f2(per(t.for, t.jams)))}
    ${chart("Penalties per game", "", (t) => per(t.penalties, t.games), (t) => f1(per(t.penalties, t.games)))}
    ${section("Penalty codes", "all teams together", bars(Object.entries(T().penaltyCodes).sort((a, b) => b[1] - a[1]).map(([c, n]) => ({ html: esc(c), value: n, text: `${n} <small>(${f1(pct(n, T().penalties))}%)</small>` }))))}
  </div>`;
}

const teamColumns = [
  ["Team", (t) => t.name, (t) => esc(t.name), "l"], ["Games", (t) => t.games], ["W", (t) => t.wins], ["L", (t) => t.losses],
  ["For", (t) => t.for], ["Against", (t) => t.against], ["Avg margin", (t) => per(t.for - t.against, t.games), (t) => signed(per(t.for - t.against, t.games), 1)],
  ["Jams", (t) => t.jams], ["Lead %", (t) => pct(t.leadJams, t.jams), (t) => f1(pct(t.leadJams, t.jams))],
  ["Pts/jam", (t) => per(t.for, t.jams), (t) => f2(per(t.for, t.jams))], ["Star passes", (t) => t.starPasses],
  ["Penalties", (t) => t.penalties], ["Pen/game", (t) => per(t.penalties, t.games), (t) => f1(per(t.penalties, t.games))],
];

function gamesTab() {
  const label = (g) => `${esc(teamName(g.teams[0]))} ${g.score[0]} – ${g.score[1]} ${esc(teamName(g.teams[1]))}`;
  const gs = T().games;
  const ratio = (g) => per(Math.max(...g.score), Math.min(...g.score) || 1);
  return `${table("gm", gameColumns, gs)}<div class="grid" style="margin-top:20px">
    ${section("Comebacks", "the largest deficit the winner made up", bars(ranked(gs.filter((g) => g.comeback > 0), (g) => g.comeback, 12).map((g) => ({ html: label(g), value: g.comeback, text: `${g.comeback} pts`, tip: gameLabel(g.id) }))))}
    ${section("Most lead changes and ties", "how often the score went between ahead, tied and behind", bars(ranked(gs.filter((g) => g.leadChanges > 0), (g) => g.leadChanges, 12).map((g) => ({ html: label(g), value: g.leadChanges, text: String(g.leadChanges), tip: `${gameLabel(g.id)}; ${g.lastLeadChanges} in the last period` }))))}
    ${section("Closest games", "score ratio, winner's points over the loser's", bars([...gs].sort((a, b) => ratio(a) - ratio(b)).slice(0, 12).map((g) => ({ html: label(g), value: ratio(g) - 1, text: `${f2(ratio(g))} <small>(${Math.abs(g.score[0] - g.score[1])})</small>`, tip: gameLabel(g.id) }))))}
    ${section("Most common jam scores", "higher score first", bars(T().jamScores.slice(0, 12).map((x) => ({ html: esc(x.score), value: x.count, text: String(x.count) }))))}
  </div>`;
}

const gameRow = (id) => N.games.find((g) => g.id === id) || {};
function sources(id) {
  const g = gameRow(id);
  return `${g.statsbook ? `<span class="badge used" title="statsbook">xlsx</span>` : ""}${g.scoreboard ? `<span class="badge used" title="scoreboard file">json</span>` : ""}`;
}
const gameColumns = [
  ["Date", (g) => g.date, (g) => esc(g.date), "l"], ["Game", (g) => +g.gameNo || 0, (g) => esc(g.gameNo || "")],
  ["Tournament", (g) => (S.gameInfo[g.id] || {}).tournament || "", (g) => esc((S.gameInfo[g.id] || {}).tournament || ""), "l"],
  ["Team", (g) => teamName(g.teams[0]), (g) => `<span class="${g.score[0] > g.score[1] ? "w" : ""}">${esc(teamName(g.teams[0]))}</span>`, "l"],
  ["Score", (g) => g.score[0], (g) => `${g.score[0]} – ${g.score[1]}`],
  ["Team", (g) => teamName(g.teams[1]), (g) => `<span class="${g.score[1] > g.score[0] ? "w" : ""}">${esc(teamName(g.teams[1]))}</span>`, "l"],
  ["Margin", (g) => Math.abs(g.score[0] - g.score[1])], ["Periods", () => 0, (g) => g.periods.map((p) => `${p[0]}–${p[1]}`).join(", ")],
  ["Jams", (g) => g.jams], ["Lead changes/ties", (g) => g.leadChanges], ["Comeback", (g) => g.comeback],
  ["Penalties", (g) => g.penalties[0] + g.penalties[1], (g) => `${g.penalties[0]} – ${g.penalties[1]}`],
  ["From", (g) => (gameRow(g.id).statsbook ? 2 : 0) + (gameRow(g.id).scoreboard ? 1 : 0), (g) => sources(g.id), "l"],
  ["Corrections", (g) => gameRow(g.id).compared ? gameRow(g.id).changes : -1, (g) => gameRow(g.id).compared ? `<button class="link" data-changes="${g.id}">${gameRow(g.id).changes}</button>` : `<span class="muted">–</span>`],
];

// --- timing (scoreboard files only) ------------------------------------------------------

function timingTab() {
  const m = S.timing;
  if (!m.games) {
    return `<div class="callout">None of these games has a scoreboard file. Times only come from the CRG scoreboard's game files (.json):
      a statsbook records no jam lengths, lead times or timeouts. Upload the game files on the Files tab to see them.</div>`;
  }
  const to = m.timeouts;
  const toRow = (k, name) => to[k].count ? `<tr><td class="l">${name}</td><td>${to[k].count}</td><td>${f1(per(to[k].count, m.games))}</td><td>${to[k].timed ? clock(per(to[k].time, to[k].timed)) : ""}</td></tr>` : "";
  const durGames = m.perGame.filter((g) => g.duration > 0);
  const avgDur = per(durGames.reduce((n, g) => n + g.duration, 0), durGames.length);
  const leadMin = Math.max(3, Math.round(Math.max(0, ...m.jammers.map((j) => j.leads)) / 4));
  const endName = { calloff: "Called off", time: "Jam clock ran out", injury: "Injury", official: "Officials", unknown: "Not recorded" };
  return `${m.games < S.coverage.games ? `<p class="callout">From the ${m.games} of ${S.coverage.games} games that have a scoreboard file; statsbooks record no times.</p>` : ""}
    ${tiles([["Games with times", m.games], ["Average jam", clock(m.jamLength.mean)], ["Median time to lead", m.leadAt.n ? clock(m.leadAt.median) : "–"],
      ["Average lineup", m.lineup.n ? secs(m.lineup.mean) : "–", "jam end to next jam start"], ["Game, first to last whistle", durGames.length ? hm(avgDur) : "–"]])}
    <div class="grid">
      ${section("Jam length", "how long jams lasted", hist(m.jamLength, "jams"))}
      ${section("Time to lead", "jam clock when the lead jammer finished their initial trip", hist(m.leadAt, "lead jammers"))}
      ${section("When lead jammers call it off", "jam clock at the call-off", hist(m.callAt, "call-offs"))}
      ${section("When penalties are called", "jam clock when the penalty was entered (entry follows the call by a few seconds)", hist(m.penaltyAt, "penalties"))}
      ${section("Lineups", "from a jam's end to the next jam's start, without a timeout in between", hist(m.lineup, "lineups"))}
      ${section("How jams ended", "", bars(Object.entries(m.jamEnds).sort((a, b) => b[1] - a[1]).map(([r, n]) => ({ html: esc(endName[r] || r), value: n, text: `${n} <small>(${f1(pct(n, m.jams))}%)</small>` }))))}
      ${section("Timeouts", "", `<table class="t"><thead><tr><th class="l">Kind</th><th>Count</th><th>Per game</th><th>Average length</th></tr></thead><tbody>
        ${toRow("team", "Team timeouts")}${toRow("review", "Official reviews")}${toRow("official", "Official timeouts")}</tbody></table>`)}
      ${section("Teams: time to lead", "average jam clock when the team's jammer got lead",
        bars([...m.teams].filter((t) => t.leads).sort((a, b) => per(a.leadTime, a.leads) - per(b.leadTime, b.leads)).map((t) => ({ html: esc(teamName(t.team)),
          value: per(t.leadTime, t.leads) / 1000, text: secs(per(t.leadTime, t.leads)), tip: `${t.leads} leads in ${t.jams} jams` }))))}
      ${section("Quickest jammers to lead", `average time to lead; at least ${leadMin} leads`,
        bars(m.jammers.filter((j) => j.leads >= leadMin).sort((a, b) => per(a.leadTime, a.leads) - per(b.leadTime, b.leads)).slice(0, 15).map((j) => ({
          html: `#${esc(j.number)} ${esc(j.name || "")} <small>(${esc(teamName(j.team))})</small>`, value: per(j.leadTime, j.leads) / 1000,
          text: secs(per(j.leadTime, j.leads)), tip: `${j.leads} leads in ${j.jams} jams; quickest ${secs(j.fastest)}` })), { empty: "Nobody with that many leads." }))}
    </div>
    <h2>Per game</h2>${table("tg", timingGameColumns, m.perGame)}`;
}

const timingGameColumns = [
  ["Game", (g) => g.label, (g) => esc(g.label), "l"], ["First to last whistle", (g) => g.duration, (g) => g.duration ? hm(g.duration) : ""],
  ["Jams", (g) => g.jams], ["Average jam", (g) => per(g.jamTime, g.jams), (g) => clock(per(g.jamTime, g.jams))],
  ["Average lineup", (g) => per(g.lineup, g.lineups), (g) => g.lineups ? secs(per(g.lineup, g.lineups)) : ""],
  ["Timeouts", (g) => g.timeouts], ["Official reviews", (g) => g.reviews],
];

// --- crews -------------------------------------------------------------------------------

// The tint of a crew's count of a code: blue fewer, red more than the teams
// got with other crews, grey about the same; pale when there's little to go on.
// The tint grows with the ratio (half or double is full) and with how sure
// it is: a difference of two standard deviations (Poisson) or more counts in
// full, less fades towards grey.
function tint(obs, exp) {
  if (exp < 3 && obs < 3) return { cls: "weak", style: "" };
  const r = Math.log2((obs + 0.5) / (exp + 0.5)); // -1 = half, +1 = double
  const sure = Math.min(1, Math.abs(obs - exp) / Math.sqrt(Math.max(exp, 1)) / 2);
  const a = Math.min(1, Math.abs(r)) * sure * 0.85;
  const color = r > 0 ? "var(--neg)" : "var(--bar)";
  return { cls: "", style: `background: color-mix(in srgb, ${color} ${Math.round(a * 100)}%, var(--mid))` + (a > 0.55 ? ";color:#fff" : "") };
}

function crewName(c) { return `${esc(c.head)}'s crew`; }

function crewsTab() {
  const C = crewKind === "referee" ? S.referees : S.nsos;
  const seg = `<div class="filters"><span class="seg"><button data-crew="referee" class="${crewKind === "referee" ? "on" : ""}">Referee crews</button><button data-crew="nso" class="${crewKind === "nso" ? "on" : ""}">NSO crews</button></span>
    <span class="small">A crew is recognised by its people: within a tournament, games worked by mostly the same ${crewKind === "referee" ? "referees" : "NSOs"} are one crew. It's named after its usual head.</span></div>`;
  if (!C.crews.length) return seg + `<p class="callout">No officials on the IGRF of these games.</p>`;
  const unknown = C.unknown.length ? `<p class="note">${C.unknown.length} games have no ${crewKind === "referee" ? "referees" : "NSOs"} on the IGRF and are left out.</p>` : "";
  const multi = new Set(C.crews.map((c) => c.tournament)).size > 1;
  const name = (c) => `${crewName(c)}${multi ? ` <small class="muted">${esc(c.tournament)}</small>` : ""}`;
  let body = "";
  if (crewKind === "referee") {
    const cols = [
      ["Crew", (c) => c.head, (c) => name(c), "l"], ["Games", (c) => c.games.length], ["Jams", (c) => c.jams],
      ["Penalties", (c) => c.penalties], ["Per game", (c) => per(c.penalties, c.games.length), (c) => f1(per(c.penalties, c.games.length))],
      ["Per jam", (c) => per(c.penalties, c.jams), (c) => f2(per(c.penalties, c.jams))],
      ["Expected", (c) => c.hasExpected ? c.expected : -1, (c) => c.hasExpected ? f1(c.expected) : "–"],
      ["vs expected", (c) => c.hasExpected ? per(c.penalties, c.expected) : 0, (c) => c.hasExpected ? signedPct(c.penalties, c.expected) : "–"],
      ["Expulsions", (c) => c.expulsions], ["Official reviews", (c) => c.reviews, (c) => c.reviews ? `${c.reviews} <small class="muted">(${c.retained} retained)</small>` : "0"],
      ["Official timeouts", (c) => per(c.officialTimeouts, c.timedGames), (c) => c.timedGames ? `${f1(per(c.officialTimeouts, c.timedGames))}/game` : "–"],
      ["Avg OTO", (c) => per(c.officialTimeoutTime, c.officialTimeouts), (c) => c.officialTimeouts ? clock(per(c.officialTimeoutTime, c.officialTimeouts)) : "–"],
    ];
    const total = C.crews.reduce((n, c) => n + c.penalties, 0);
    const codes = C.codes.filter((k) => C.crews.reduce((n, c) => n + (c.codes[k] || 0), 0) >= Math.max(3, total * 0.01));
    const heat = `<div class="scroll"><table class="t heat"><thead><tr><th class="l">Crew</th>${codes.map((k) => `<th>${esc(k)}</th>`).join("")}</tr></thead><tbody>
      ${C.crews.map((c) => `<tr><td class="l">${name(c)}</td>${codes.map((k) => {
        const o = c.codes[k] || 0, e = c.expectedCodes[k] || 0;
        const t = c.hasExpected ? tint(o, e) : { cls: "weak", style: "" };
        const tip = c.hasExpected ? `${c.head}'s crew, ${k}\n${o} called, ${f1(e)} expected (${signedPct(o, e, true)})` : `${o} called`;
        return `<td class="cell ${t.cls}" style="${t.style}" tabindex="0" data-tip="${esc(tip)}">${o}</td>`;
      }).join("")}</tr>`).join("")}</tbody></table></div>`;
    body = `${table("crews", cols, C.crews)}
      <h2>Penalty codes per crew</h2>
      <p class="note">Each cell is how often the crew called the code. The colour compares it with what the same teams got from the other crews
        of the same tournament: per team, its rate per jam with the other crews (a team with few jams elsewhere counts as an average team), times its jams with this crew.
        This takes out some of the "it depends on who they officiated", not all: a team that changes its play against stronger opponents, or a
        crew that only worked one bracket, still colours the numbers.</p>
      <div class="legend"><span><i style="background: color-mix(in srgb, var(--bar) 85%, var(--mid))"></i>half as many or fewer</span>
        <span><i style="background: var(--mid)"></i>about as expected</span><span><i style="background: color-mix(in srgb, var(--neg) 85%, var(--mid))"></i>twice as many or more</span>
        <span class="muted">the less sure the difference (few penalties), the greyer; pale numbers: too few to say</span></div>${heat}`;
  } else {
    const areas = [...new Set(C.crews.flatMap((c) => Object.keys(c.fixAreas)))].sort();
    const cols = [
      ["Crew", (c) => c.head, (c) => name(c), "l"], ["Games", (c) => c.games.length], ["Compared", (c) => c.compared, (c) => c.compared || `<span class="muted">0</span>`],
      ["Corrections", (c) => c.fixes], ["Per game", (c) => per(c.fixes, c.compared), (c) => c.compared ? f1(per(c.fixes, c.compared)) : "–"],
      ...areas.map((a) => [esc(a), (c) => c.fixAreas[a] || 0]),
      ["Penalties per game", (c) => per(c.penalties, c.games.length), (c) => f1(per(c.penalties, c.games.length))],
    ];
    body = `<p class="note">Corrections: what the crew's statsbooks show differently from the scoreboard's game file, for games that have both (see Corrections).
      A statsbook saved before the post-game check shows none.</p>${table("nsocrews", cols, C.crews)}`;
  }
  const members = `<h2>Who was in each crew</h2><div class="grid">${C.crews.map((c) => section(name(c), `${c.games.length} games`,
    `<table class="t"><tbody>${c.members.map((m) => `<tr><td class="l">${esc(m.name)}</td><td class="l wrap small muted">${esc(m.roles)}</td><td>${m.games}</td></tr>`).join("")}</tbody></table>
     <details class="small"><summary>Games</summary><ul>${c.games.map((id) => `<li>${esc(gameLabel(id))}</li>`).join("")}</ul></details>`)).join("")}</div>`;
  return seg + unknown + body + members;
}

function signedPct(o, e, words) {
  if (!e) return "–";
  const d = Math.round(100 * (o - e) / e);
  if (words) return d === 0 ? "as expected" : `${d > 0 ? "+" : "−"}${Math.abs(d)}%`;
  return `${d > 0 ? "+" : d < 0 ? "−" : ""}${Math.abs(d)}%`;
}

// --- corrections ---------------------------------------------------------------------------

const areaNote = {
  Score: "scorekeepers: trips, lead, call-off, star pass", Lineups: "lineup trackers: who skated where", Box: "lineup trackers and the box: box symbols",
  Penalties: "penalty trackers", Jams: "jams only in one of the two", Roster: "the IGRF's skaters", Officials: "the IGRF's officials",
};

function fixesTab() {
  const F = S.fixes;
  if (!F.compared) {
    return `<div class="callout">To see what was corrected, a game needs both its scoreboard file (.json) and its statsbook (.xlsx).
      None of these games has both yet.</div>`;
  }
  const kinds = F.kinds.map((k) => `<tr><td class="l">${esc(k.area)}</td><td class="l">${esc(k.kind)}</td><td>${k.changes}</td><td>${k.games}</td>
    <td class="l wrap small">${(k.patterns || []).map((p) => `<span class="badge">${esc(p.value)} ×${p.count}</span>`).join(" ")}</td></tr>`).join("");
  const games = [...F.games].sort((a, b) => b.changes - a.changes);
  return `<p class="note">For each game with both files: what the statsbook shows differently from the scoreboard's own game file. Those are mostly
      the corrections made in the statsbook after the game, so they point at what goes wrong while scoring, or in the scoreboard itself.
      A statsbook saved before the post-game check shows none. Blocker order and the order of penalties in a jam are not compared.</p>
    ${tiles([["Games compared", F.compared], ["With corrections", F.withChanges, `${Math.round(pct(F.withChanges, F.compared))}%`],
      ["Corrections", F.changes], ["Per game", f1(per(F.changes, F.compared))]])}
    <div class="grid">
      ${section("By area", "", bars(F.areas.map((a) => ({ html: `${esc(a.area)} <small>${esc(areaNote[a.area] || "")}</small>`, value: a.changes,
        text: `${a.changes} <small>in ${a.games} games</small>`, tip: `${a.changes} corrections in ${a.games} of ${F.compared} games` }))))}
      ${section("Games with the most corrections", "", bars(games.filter((g) => g.changes).slice(0, 12).map((g) => ({
        html: `<button class="link" data-changes="${g.id}">${esc(g.label)}</button>`, value: g.changes, text: String(g.changes),
        tip: Object.entries(g.byArea).map(([a, n]) => `${a}: ${n}`).join("\n") })), { empty: "No corrections." }))}
    </div>
    <h2>Common corrections</h2>
    <p class="note">Badges show what the change was: scoreboard → statsbook ("- → $" is a box symbol, "B → F" a penalty code, "+1" points).</p>
    <div class="scroll"><table class="t"><thead><tr><th class="l">Area</th><th class="l">Change</th><th>Times</th><th>Games</th><th class="l">What</th></tr></thead><tbody>${kinds}</tbody></table></div>
    <h2>Every compared game</h2>
    <div id="gameChanges">${games.map((g) => `<details class="changes" data-game="${g.id}"><summary>${esc(g.label)} <span class="muted">· ${g.changes} ${g.changes === 1 ? "correction" : "corrections"}</span></summary><div class="body muted small">Loading…</div></details>`).join("")}</div>`;
}

async function showChanges(det) {
  const body = det.querySelector(".body");
  if (det.dataset.loaded) return;
  det.dataset.loaded = "1";
  try {
    const g = await getJSON(api("/games/" + det.dataset.game));
    if (!g.changes.length) { body.textContent = "Scoreboard file and statsbook agree."; return; }
    body.className = "body";
    body.innerHTML = `<div class="scroll"><table class="t"><thead><tr><th class="l">Where</th><th class="l">Team</th><th class="l">Area</th><th class="l">Change</th><th class="l">Skater</th><th class="l">Scoreboard</th><th class="l">Statsbook</th></tr></thead><tbody>
      ${g.changes.map((c) => `<tr><td class="l">${esc(c.where || "")}</td><td class="l">${c.team ? "Team " + esc(c.team) : ""}</td><td class="l">${esc(c.area)}</td><td class="l">${esc(c.kind)}</td>
        <td class="l">${c.skater ? (c.skater.startsWith("T") ? esc(c.skater) : "#" + esc(c.skater)) : ""}</td><td class="l">${esc(c.from || "")}</td><td class="l">${esc(c.to || "")}</td></tr>`).join("")}</tbody></table></div>`;
  } catch (e) {
    body.textContent = e.message;
  }
}

// --- files ---------------------------------------------------------------------------------

function filesTab() {
  const tnames = N.tournaments.map((t) => t.name).filter((n) => n !== N.noTournament);
  const files = [...N.files].reverse();
  const kindName = { scoreboard: "Scoreboard", statsbook: "Statsbook" };
  const fileRow = (f) => {
    const played = f.entries.filter((e) => e.jams > 0).length;
    const fromFile = [...new Set(f.entries.map((e) => e.tournament).filter(Boolean))];
    const entries = f.entries.length > 1 || f.entries.some((e) => e.error || (e.notes && e.notes.length)) ? `<details class="small"><summary>${f.entries.length} ${f.entries.length === 1 ? "game" : "games"}, ${played} played</summary><ul>
      ${f.entries.map((e) => `<li>${esc(e.date || "")} ${e.gameNo ? "#" + esc(e.gameNo) : ""} ${esc(e.teams[0] || "")} – ${esc(e.teams[1] || "")}
        <span class="muted">${e.jams ? `${e.jams} jams` : esc((e.state || "not played").toLowerCase())}${e.tournament ? ` · ${esc(e.tournament)}` : ""}</span>
        ${e.error ? `<span class="error">${esc(e.error)}</span>` : ""}${(e.notes || []).map((n) => `<br><span class="muted">${esc(n)}</span>`).join("")}</li>`).join("")}</ul></details>`
      : f.entries.length ? `<span class="small">${played ? `${f.entries[0].jams} jams` : `<span class="muted">not played${f.kind === "statsbook" ? " (filled in before the game)" : ""}</span>`}</span>` : "";
    return `<tr class="${f.ignored ? "muted" : ""}"><td class="l wrap"><a href="${api("/files/" + f.id)}">${esc(f.name)}</a>${f.error ? `<br><span class="error small">${esc(f.error)}</span>` : ""}</td>
      <td class="l">${kindName[f.kind] || esc(f.kind)}</td>
      <td class="l"><input type="text" list="tlist" data-tour="${f.id}" value="${esc(f.tournament)}" placeholder="${esc(fromFile.join(", ") || "as the file says")}" aria-label="Tournament"></td>
      <td class="l wrap">${entries}</td>
      <td class="l small">${esc(new Date(f.uploaded).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" }))}</td>
      <td class="l"><label class="small"><input type="checkbox" data-ignore="${f.id}" ${f.ignored ? "checked" : ""}> leave out</label></td>
      <td><button class="quiet danger" data-delete="${f.id}" title="Delete this file">Delete</button></td></tr>`;
  };
  return `<div class="drop" id="drop">
      <b>Drop scoreboard files (.json), statsbooks (.xlsx) or a zip here</b>
      <div class="row"><input type="file" id="fileInput" multiple accept=".json,.xlsx,.zip">
        <label>from tournament <input type="text" id="upTour" list="tlist" placeholder="as each file says"></label>
        <button class="primary" id="uploadBtn">Upload</button></div>
      <p class="small muted">Leave the tournament empty to use the name in each game (the IGRF's tournament). A data export with games from several
        tournaments sorts itself out that way. Uploading a later save of a game is fine: RIOT uses the scoreboard file with the most in it and the
        statsbook uploaded last.</p>
    </div>
    <datalist id="tlist">${tnames.map((n) => `<option value="${esc(n)}">`).join("")}</datalist>
    <div class="results" id="results"></div>
    ${files.length ? `<h2>Files</h2><div class="scroll"><table class="t"><thead><tr><th class="l">File</th><th class="l">Kind</th><th class="l">Tournament</th><th class="l">Games</th><th class="l">Uploaded</th><th class="l"></th><th></th></tr></thead>
      <tbody>${files.map(fileRow).join("")}</tbody></table></div>` : `<p class="muted">No files yet.</p>`}
    ${N.games.length ? `<h2>Games and their files</h2><p class="note">Used files are outlined. Leave out a file to use another save of the game.</p>
      ${table("srcgames", sourceColumns, N.games)}` : ""}
    <h2>This namespace</h2>
    <p class="small">Anyone with this page's address can see and change it. <button class="quiet danger" id="deleteNS">Delete the namespace</button></p>`;
}

const sourceColumns = [
  ["Tournament", (g) => g.tournament, (g) => esc(g.tournament), "l"], ["Game", (g) => g.label, (g) => esc(g.label), "l"],
  ["Score", (g) => g.score[0], (g) => `${g.score[0]} – ${g.score[1]}`],
  ["Files", (g) => g.versions.length, (g) => g.versions.map((v) => `<span class="badge ${v.used ? "used" : ""}" title="${esc(v.name)}${v.used ? " (used)" : ""}">${v.kind === "statsbook" ? "xlsx" : "json"} ${v.jams}j</span>`).join(""), "l"],
];

async function upload(fileList) {
  const files = [...fileList];
  if (!files.length) return;
  const fd = new FormData();
  fd.append("tournament", $("upTour").value.trim());
  files.forEach((f) => fd.append("files", f, f.name));
  $("uploadBtn").disabled = true;
  $("results").innerHTML = `<p class="muted">Reading ${files.length} ${files.length === 1 ? "file" : "files"}… a data export with many games takes a few seconds.</p>`;
  try {
    const r = await getJSON(api("/files"), { method: "POST", body: fd });
    $("results").innerHTML = `<ul>${r.results.map((x) => `<li>${esc(x.name)}: ${x.error ? `<span class="error">${esc(x.error)}</span>` :
      `<span class="ok">${x.games} played ${x.games === 1 ? "game" : "games"}</span>${x.files > 1 ? ` in ${x.files} files` : ""}`}</li>`).join("")}</ul>`;
    const keep = $("results").innerHTML;
    await loadNS();
    if (tab === "files") { $("results").innerHTML = keep; }
  } catch (e) {
    $("results").innerHTML = `<p class="error">${esc(e.message)}</p>`;
  } finally {
    if ($("uploadBtn")) $("uploadBtn").disabled = false;
  }
}

async function patchFile(id, body) {
  N = await getJSON(api("/files/" + id), { method: "PATCH", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
  const names = N.tournaments.map((t) => t.name);
  picked = picked.filter((t) => names.includes(t));
  renderTours();
  await loadStats();
}

// --- render & events ---------------------------------------------------------------------------

function render() {
  document.querySelectorAll("#tabs button").forEach((b) => b.classList.toggle("on", b.dataset.tab === tab));
  $("scope").innerHTML = tab === "files" ? "" : scopeLine();
  if (tab !== "files" && !S.coverage.games) {
    $("main").innerHTML = `<div class="callout">${N.files.length ? "No played games in this selection yet." : "Nothing here yet."}
      <button class="link" data-goto="files">Upload scoreboard files and statsbooks</button>.</div>`;
    return;
  }
  const views = { overview, skaters: skatersTab, teams: teamsTab, games: gamesTab, timing: timingTab, crews: crewsTab, fixes: fixesTab, files: filesTab };
  $("main").innerHTML = (views[tab] || overview)();
  if (tab === "files") wireDrop();
}

function wireDrop() {
  const drop = $("drop");
  ["dragenter", "dragover"].forEach((ev) => drop.addEventListener(ev, (e) => { e.preventDefault(); drop.classList.add("over"); }));
  ["dragleave", "drop"].forEach((ev) => drop.addEventListener(ev, (e) => { e.preventDefault(); drop.classList.remove("over"); }));
  drop.addEventListener("drop", (e) => upload(e.dataTransfer.files));
}

function setTab(t) {
  tab = t;
  const u = new URL(location.href);
  u.searchParams.set("tab", tab);
  history.replaceState(null, "", u);
  render();
}

document.addEventListener("click", async (e) => {
  const b = e.target.closest("#tabs button");
  if (b) return setTab(b.dataset.tab);
  const go = e.target.closest("[data-goto]");
  if (go) return setTab(go.dataset.goto);
  const th = e.target.closest("th[data-col]");
  if (th) {
    const id = th.closest("table").dataset.table, col = +th.dataset.col, st = sorts[id] || { col: -1, desc: true };
    sorts[id] = { col, desc: st.col === col ? !st.desc : true };
    return render();
  }
  const cr = e.target.closest("[data-crew]");
  if (cr) { crewKind = cr.dataset.crew; return render(); }
  const ch = e.target.closest("[data-changes]");
  if (ch) {
    const id = ch.dataset.changes;
    if (tab !== "fixes") setTab("fixes");
    const det = document.querySelector(`details[data-game="${id}"]`);
    if (det) { det.open = true; det.scrollIntoView({ behavior: "smooth", block: "start" }); }
    return;
  }
  if (e.target.id === "uploadBtn") return upload($("fileInput").files);
  const del = e.target.closest("[data-delete]");
  if (del) {
    const f = N.files.find((x) => x.id === del.dataset.delete);
    if (!f || !confirm(`Delete ${f.name}? Its games leave the stats.`)) return;
    N = await getJSON(api("/files/" + f.id), { method: "DELETE" });
    renderTours();
    return loadStats();
  }
  if (e.target.id === "deleteNS") {
    if (!confirm(`Delete "${N.name}" and all ${N.files.length} files in it? This can't be undone.`)) return;
    await getJSON(api(""), { method: "DELETE" });
    try {
      const list = JSON.parse(localStorage.getItem("riot.recent") || "[]").filter((r) => r.id !== NS);
      localStorage.setItem("riot.recent", JSON.stringify(list));
    } catch (err) { /* nothing */ }
    location.href = "/";
    return;
  }
  if (e.target.id === "copyLink") {
    const url = location.origin + "/n/" + NS;
    try { await navigator.clipboard.writeText(url); e.target.textContent = "Copied"; } catch (err) { prompt("The namespace's link:", url); }
    setTimeout(() => { if ($("copyLink")) $("copyLink").textContent = "Copy link"; }, 2000);
    return;
  }
  if (e.target.id === "renameNS") {
    const name = prompt("Name of this namespace:", N.name);
    if (!name || !name.trim()) return;
    N = await getJSON(api(""), { method: "PATCH", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ name }) });
    $("nsName").textContent = N.name;
    document.title = N.name + " · RIOT";
    remember();
  }
});

document.addEventListener("toggle", (e) => {
  if (e.target.matches && e.target.matches("details.changes") && e.target.open) showChanges(e.target);
}, true);

document.addEventListener("change", async (e) => {
  const t = e.target;
  if (t.dataset.all !== undefined) { picked = []; day = ""; renderTours(); return loadStats(); }
  if (t.dataset.t !== undefined) {
    picked = t.checked ? [...picked, t.dataset.t] : picked.filter((x) => x !== t.dataset.t);
    day = "";
    renderTours();
    return loadStats();
  }
  if (t.id === "day") { day = t.value; return loadStats(); }
  if (t.id === "fTeam") { filter.team = t.value; return render(); }
  if (t.id === "fMin") { filter.minJams = t.value === "" ? null : Math.max(0, +t.value); return render(); }
  if (t.dataset.tour) return patchFile(t.dataset.tour, { tournament: t.value.trim() });
  if (t.dataset.ignore) return patchFile(t.dataset.ignore, { ignored: t.checked });
  if (t.id === "fileInput" && t.files.length && !$("upTour").value) { /* wait for the button */ }
});

// One tooltip for every mark with data-tip, on hover and keyboard focus.
const tip = $("tip");
function showTip(el, x, y) {
  const text = el.dataset.tip;
  if (!text) { tip.style.display = "none"; return; }
  tip.textContent = text;
  tip.style.display = "block";
  const w = tip.offsetWidth, h = tip.offsetHeight;
  tip.style.left = Math.max(4, Math.min(window.innerWidth - w - 4, x + 12)) + "px";
  tip.style.top = Math.max(4, y - h - 10) + "px";
}
document.addEventListener("pointermove", (e) => {
  const el = e.target.closest && e.target.closest("[data-tip]");
  if (el) showTip(el, e.clientX, e.clientY); else tip.style.display = "none";
});
document.addEventListener("focusin", (e) => {
  const el = e.target.closest && e.target.closest("[data-tip]");
  if (el) { const r = el.getBoundingClientRect(); showTip(el, r.left, r.top); } else tip.style.display = "none";
});
document.addEventListener("scroll", () => { tip.style.display = "none"; }, true);

loadNS().catch((e) => { $("main").innerHTML = `<p class="error">${esc(e.message)}</p>`; report(e.message, "", 0, 0, e.stack); });
