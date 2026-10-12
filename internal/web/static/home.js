"use strict";
// The namespaces opened in this browser, for coming back (kept in
// localStorage by the namespace page).
(function () {
  let list = [];
  try { list = JSON.parse(localStorage.getItem("rats.recent") || "[]"); } catch (e) { list = []; }
  if (!Array.isArray(list) || !list.length) return;
  const ul = document.getElementById("recentList");
  for (const r of list.slice(0, 12)) {
    if (!/^[0-9a-f]{10}$/.test(r.id)) continue;
    const li = document.createElement("li");
    const a = document.createElement("a");
    a.href = "/n/" + r.id;
    a.textContent = r.name || r.id;
    li.appendChild(a);
    ul.appendChild(li);
  }
  document.getElementById("recent").hidden = !ul.children.length;
})();
