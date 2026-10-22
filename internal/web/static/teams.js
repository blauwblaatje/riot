"use strict";
// The Teams view: one team over its seasons (club seasons, August to July,
// or WFTDA seasons, two years from June), and the team names, where
// spellings of one team are merged. It uses app.js's helpers.

const TV = {
  team: params.get("team") || "",
  kind: params.get("kind") === "wftda" ? "wftda" : "club",
  page: params.get("tpage") === "names" ? "names" : "seasons",
  list: null,    // /api/teams
  seasons: {},   // team|kind -> /api/seasons
  checked: new Set(), // team keys ticked on the names page
};

async function loadTeams() {
  TV.list = await getJSON(api("/teams"));
  if (!TV.list.teams.some((t) => t.key === TV.team)) TV.team = TV.list.teams.length ? TV.list.teams[0].key : "";
}

const teamRef = (key) => TV.list.teams.find((t) => t.key === key);
const kindNames = { club: "Club season", wftda: "WFTDA season" };
const kindNotes = { club: "1 August to 31 July", wftda: "two years from 1 June; the first started on 1 June 2024" };

function teamsURL() {
  const u = new URL(location.href);
  ["t", "date", "tab", "game"].forEach((k) => u.searchParams.delete(k));
  u.searchParams.set("view", "teams");
  u.searchParams.set("tpage", TV.page);
  if (TV.team) u.searchParams.set("team", TV.team); else u.searchParams.delete("team");
  u.searchParams.set("kind", TV.kind);
  history.replaceState(null, "", u);
}

function renderTeams() {
  if (!TV.list) {
    loadTeams().then(renderTeams).catch((e) => { $("main").innerHTML = `<p class="error">${esc(e.message)}</p>`; });
    $("main").innerHTML = `<p class="muted">Loading…</p>`;
    return;
  }
  teamsURL();
  if (!TV.list.teams.length) {
    $("teamBar").innerHTML = "";
    $("main").innerHTML = `<div class="callout">No played games yet. <button class="link" data-view="tournaments" data-goto="files">Upload statsbooks and scoreboard files</button>.</div>`;
    return;
  }
  const n = TV.list.suggestions.length;
  $("teamBar").innerHTML = `<div class="filters">
      <label>Team <select id="tTeam">${TV.list.teams.map((t) => `<option value="${esc(t.key)}" ${t.key === TV.team ? "selected" : ""}>${esc(t.name)} — ${esc(t.league)} (${t.games})</option>`).join("")}</select></label>
      <span class="seg" role="group" aria-label="Season">${["club", "wftda"].map((k) => `<button data-kind="${k}" class="${k === TV.kind ? "on" : ""}" title="${kindNotes[k]}">${kindNames[k]}</button>`).join("")}</span>
    </div>
    <nav class="tabs"><button data-tpage="seasons" class="${TV.page === "seasons" ? "on" : ""}">Seasons</button>
      <button data-tpage="names" class="${TV.page === "names" ? "on" : ""}">Team names${n ? ` <span class="badge used">${n} to check</span>` : ""}</button></nav>`;
  $("main").innerHTML = TV.page === "names" ? namesPage() : seasonsPage();
}

// --- seasons -------------------------------------------------------------------------------------

function seasonsPage() {
  const id = TV.team + "|" + TV.kind;
  const D = TV.seasons[id];
  if (!D) {
    getJSON(api(`/seasons?team=${encodeURIComponent(TV.team)}&kind=${TV.kind}`)).then((d) => { TV.seasons[id] = d; if (view === "teams") renderTeams(); })
      .catch((e) => { $("main").innerHTML = `<p class="error">${esc(e.message)}</p>`; });
    return `<p class="muted">Loading…</p>`;
  }
  const ref = teamRef(TV.team);
  const ss = D.seasons;
  const old = [...ss].reverse(); // oldest first, for the charts
  const margin = (s) => per(s.team.for - s.team.against, s.team.games);
  const chart = (title, value, text, opts) => section(title, "", bars(old.map((s) => ({ html: esc(s.label), value: value(s), text: text(s), tip: `${s.team.games} games` })), opts));
  const spell = ref.spellings.length > 1 ? `<p class="note">Played as ${ref.spellings.map((s) => esc(s.team || s.league)).filter((x, i, a) => a.indexOf(x) === i).join(", ")}.
    <button class="link" data-tpage="names">Team names</button></p>` : "";
  return `<h2 style="margin-top:4px">${esc(ref.name)} <small class="muted">${esc(ref.league)}</small></h2>${spell}
    <p class="note">${kindNames[TV.kind]}s: ${kindNotes[TV.kind]}. Every game in the namespace counts, whatever its tournament.</p>
    ${table("seasons", seasonColumns, ss)}
    ${ss.length > 1 ? `<div class="grid" style="margin-top:20px">
      ${chart("Average margin", margin, (s) => signed(margin(s), 1), { signed: true })}
      ${chart("Lead jammer %", (s) => pct(s.team.leadJams, s.team.jams), (s) => `${f1(pct(s.team.leadJams, s.team.jams))}%`)}
      ${chart("Points per jam", (s) => per(s.team.for, s.team.jams), (s) => f2(per(s.team.for, s.team.jams)))}
      ${chart("Penalties per game", (s) => per(s.team.penalties, s.team.games), (s) => f1(per(s.team.penalties, s.team.games)))}
    </div>` : ""}
    ${ss.map((s, i) => `<details class="season" ${i === 0 ? "open" : ""}><summary><b>${esc(s.label)}</b>
        <span class="muted small">${s.team.wins}–${s.team.losses}${s.team.ties ? "–" + s.team.ties : ""} · ${s.team.games} games</span></summary>
      <h2>Games</h2>${table("sg" + i, seasonGameColumns, s.games)}
      <h2>Skaters</h2>${table("ss" + i, seasonSkaterColumns, s.skaters)}</details>`).join("")}
    ${D.undated.length ? `<div class="callout">${D.undated.length} ${D.undated.length === 1 ? "game has" : "games have"} no date, so no season:
      ${D.undated.map((g) => `<button class="link" data-tgame="${esc(g.id)}">${esc(g.opponent)}</button>`).join(", ")}.</div>` : ""}`;
}

const seasonColumns = [
  ["Season", (s) => s.to, (s) => `<b>${esc(s.label)}</b> <small class="muted">${esc(s.from ? s.from + " – " + s.to : "until " + s.to)}</small>`, "l"],
  ["Games", (s) => s.team.games], ["W", (s) => s.team.wins], ["L", (s) => s.team.losses],
  ["For", (s) => s.team.for], ["Against", (s) => s.team.against],
  ["Avg margin", (s) => per(s.team.for - s.team.against, s.team.games), (s) => signed(per(s.team.for - s.team.against, s.team.games), 1)],
  ["Jams", (s) => s.team.jams], ["Lead %", (s) => pct(s.team.leadJams, s.team.jams), (s) => f1(pct(s.team.leadJams, s.team.jams))],
  ["Pts/jam", (s) => per(s.team.for, s.team.jams), (s) => f2(per(s.team.for, s.team.jams))], ["Star passes", (s) => s.team.starPasses],
  ["Penalties", (s) => s.team.penalties], ["Pen/game", (s) => per(s.team.penalties, s.team.games), (s) => f1(per(s.team.penalties, s.team.games))],
];

const seasonGameColumns = [
  ["Date", (g) => g.date, (g) => esc(g.date), "l"], ["Tournament", (g) => g.tournament, (g) => esc(g.tournament), "l"],
  ["Opponent", (g) => g.opponent, (g) => esc(g.opponent), "l"],
  ["Score", (g) => g.for - g.against, (g) => `<button class="link" data-tgame="${esc(g.id)}" title="the game's stats and score progression">${g.for} – ${g.against}</button>`],
  ["Result", (g) => Math.sign(g.for - g.against), (g) => g.for > g.against ? `<span class="w">W</span>` : g.for < g.against ? "L" : "T"],
  ["Margin", (g) => g.for - g.against, (g) => signed(g.for - g.against, 0)], ["Penalties", (g) => g.penalties],
];

const seasonSkaterColumns = [
  ["#", (s) => s.number, (s) => esc(s.number), "l"], ["Name", (s) => s.name || "", (s) => esc(s.name || ""), "l"], ["Games", (s) => s.games],
  ["Jammer jams", (s) => s.jammerJams], ["Points", totalPoints], ["Pts/jam", (s) => per(s.jammerPoints, s.jammerJams), (s) => s.jammerJams ? f2(per(s.jammerPoints, s.jammerJams)) : ""],
  ["Lead %", (s) => pct(s.lead, s.jammerJams), (s) => s.jammerJams ? f1(pct(s.lead, s.jammerJams)) : ""],
  ["Jammer +/−", (s) => per(s.jammerPM, s.jammerJams), (s) => s.jammerJams ? signed(per(s.jammerPM, s.jammerJams)) : ""],
  ["Blocker jams", (s) => s.blockerJams], ["Pivot jams", (s) => s.pivotJams],
  ["Blocker +/−", (s) => per(s.blockerPM, s.blockerJams), (s) => s.blockerJams ? signed(per(s.blockerPM, s.blockerJams)) : ""],
  ["Penalties", (s) => s.penalties], ["Pen/jam", (s) => per(s.penalties, s.jammerJams + s.blockerJams), (s) => f2(per(s.penalties, s.jammerJams + s.blockerJams))],
];

// --- team names: merging spellings -------------------------------------------------------------

function spellings(t) {
  return t.spellings.map((s) => `${s.team ? `${esc(s.team)} <small class="muted">${esc(s.league)}</small>` : esc(s.league)}${t.spellings.length > 1 ? ` <small class="muted">×${s.games}</small>` : ""}`).join("<br>");
}
const span = (t) => esc(t.first === t.last ? t.first || "" : `${t.first} – ${t.last}`);

function namesPage() {
  const sugg = TV.list.suggestions.map((s, i) => {
    const ts = s.teams.map(teamRef).filter(Boolean);
    const latest = [...ts].sort((a, b) => (b.last || "").localeCompare(a.last || ""))[0];
    return `<div class="callout suggestion" data-sugg="${i}"><p style="margin:0 0 6px">These look like one team: mostly the same skater numbers, and they never played each other.
      Untick any that are a different team.</p>
      <table class="t"><tbody>${ts.map((t) => `<tr><td class="l"><input type="checkbox" data-sm="${esc(t.key)}" checked aria-label="include"></td>
        <td class="l wrap">${spellings(t)}</td><td>${t.games} ${t.games === 1 ? "game" : "games"}</td><td class="l">${span(t)}</td></tr>`).join("")}</tbody></table>
      <p style="margin:8px 0 0"><label>Name <input type="text" data-sname value="${esc(latest.name)}"></label> <button class="primary" data-merge-sugg="${i}">Merge</button></p></div>`;
  }).join("");
  const n = TV.checked.size;
  return `<p class="note">A team is its league and team name as the files write them; capitals, accents and punctuation don't count.
      Merge the spellings of one team (a typo, or a team that changed its name) to count them as one, here and in the tournament stats.
      A league's A and B teams are separate teams.</p>
    ${sugg}
    <div class="filters"><button id="mergeChecked" ${n < 2 ? "disabled" : ""}>Merge ${n || ""} selected</button>
      <span class="muted small">Tick two or more teams below.</span></div>
    ${table("names", nameColumns, TV.list.teams)}`;
}

const nameColumns = [
  ["", () => 0, (t) => `<input type="checkbox" data-pick="${esc(t.key)}" ${TV.checked.has(t.key) ? "checked" : ""} aria-label="select ${esc(t.name)}">`, "l"],
  ["Team", (t) => t.name.toLowerCase(), (t) => `<b>${esc(t.name)}</b>${t.group ? ` <span class="badge used">merged</span>` : ""}`, "l"],
  ["Spelled as", (t) => t.spellings.length, (t) => spellings(t), "l wrap"],
  ["Games", (t) => t.games], ["Dates", (t) => t.first || "", (t) => span(t), "l"],
  ["", () => 0, (t) => t.group ? `<button class="link" data-rename="${esc(t.key)}">Rename</button> · <button class="link" data-split="${esc(t.key)}">${t.members.length > 1 ? "Split" : "Undo rename"}</button>`
    : `<button class="link" data-rename="${esc(t.key)}">Rename</button>`, "l"],
];

// saveGroups sends all merged teams; the stats change with them.
async function saveGroups(groups) {
  TV.list = await getJSON(api("/teams"), { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ groups }) });
  TV.seasons = {};
  TV.checked.clear();
  if (!TV.list.teams.some((t) => t.key === TV.team)) TV.team = TV.list.teams.length ? TV.list.teams[0].key : "";
  N = await getJSON(api(""));
  S = null; // the tournament stats, loaded again in that view
  renderTeams();
}

// merge makes one group of the teams' members, replacing their groups.
function merge(keys, name) {
  const ts = keys.map(teamRef).filter(Boolean);
  const members = ts.flatMap((t) => t.members);
  const groups = TV.list.groups.filter((g) => !g.members.some((m) => members.includes(m)));
  groups.push({ name, members });
  return saveGroups(groups);
}

document.addEventListener("click", async (e) => {
  if (view !== "teams") return;
  const t = e.target;
  if (t.dataset.kind) { TV.kind = t.dataset.kind; return renderTeams(); }
  if (t.closest("[data-tpage]")) { TV.page = t.closest("[data-tpage]").dataset.tpage; return renderTeams(); }
  if (t.dataset.tgame) { backToTeams = true; return showGame(t.dataset.tgame); }
  if (t.dataset.mergeSugg !== undefined) {
    const box = t.closest("[data-sugg]");
    const keys = [...box.querySelectorAll("[data-sm]:checked")].map((x) => x.dataset.sm);
    const name = box.querySelector("[data-sname]").value.trim();
    if (keys.length < 2) return alert("Tick at least two teams to merge.");
    if (!name) return alert("Give the merged team a name.");
    return merge(keys, name);
  }
  if (t.id === "mergeChecked") {
    const keys = [...TV.checked];
    const latest = keys.map(teamRef).filter(Boolean).sort((a, b) => (b.last || "").localeCompare(a.last || ""))[0];
    const name = prompt("Name of the merged team:", latest ? latest.name : "");
    if (!name || !name.trim()) return;
    return merge(keys, name.trim());
  }
  if (t.dataset.rename) {
    const ref = teamRef(t.dataset.rename);
    const name = prompt("Name of this team:", ref.name);
    if (!name || !name.trim()) return;
    const groups = TV.list.groups.filter((g) => !g.members.some((m) => ref.members.includes(m)));
    groups.push({ name: name.trim(), members: ref.members });
    return saveGroups(groups);
  }
  if (t.dataset.split) {
    const ref = teamRef(t.dataset.split);
    if (!confirm(ref.members.length > 1 ? `Split ${ref.name} into its spellings again?` : `Give ${ref.name} the name its files use again?`)) return;
    return saveGroups(TV.list.groups.filter((g) => !g.members.some((m) => ref.members.includes(m))));
  }
});

document.addEventListener("change", (e) => {
  if (view !== "teams") return;
  const t = e.target;
  if (t.id === "tTeam") { TV.team = t.value; TV.page = "seasons"; return renderTeams(); }
  if (t.dataset.pick) {
    if (t.checked) TV.checked.add(t.dataset.pick); else TV.checked.delete(t.dataset.pick);
    const b = $("mergeChecked");
    b.disabled = TV.checked.size < 2;
    b.textContent = `Merge ${TV.checked.size || ""} selected`;
  }
});
