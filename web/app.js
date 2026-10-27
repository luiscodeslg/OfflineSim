"use strict";

const $ = (s, el = document) => el.querySelector(s);
const $$ = (s, el = document) => [...el.querySelectorAll(s)];
const esc = (s) => String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
const fmt = (n, d = 0) => (Number.isFinite(n) ? n.toLocaleString(undefined, { minimumFractionDigits: d, maximumFractionDigits: d }) : "–");
const pct = (n, d = 1) => (Number.isFinite(n) ? (n * 100).toFixed(d) + "%" : "–");

const store = {
  get(k, d) { try { const v = localStorage.getItem("osim." + k); return v == null ? d : JSON.parse(v); } catch { return d; } },
  set(k, v) { try { localStorage.setItem("osim." + k, JSON.stringify(v)); } catch {} },
};

async function api(path, opts = {}) {
  const init = { method: opts.method || (opts.body !== undefined ? "POST" : "GET"), headers: {} };
  if (opts.body !== undefined) {
    if (opts.raw) init.body = opts.body;
    else { init.body = JSON.stringify(opts.body); init.headers["Content-Type"] = "application/json"; }
  }
  const r = await fetch(path, init);
  const data = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(data.error || r.statusText);
  return data;
}

let toastTimer;
function toast(msg, err = false) {
  const t = $("#toast");
  t.textContent = msg;
  t.className = "toast" + (err ? " err" : "");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => t.classList.add("hidden"), err ? 6000 : 2500);
}

const SIM_TYPES = {
  quick: { title: "Quick Sim", blurb: "Sim your character as it is right now: DPS, ability breakdown and the full SimulationCraft report.", run: "Run Quick Sim" },
  topgear: { title: "Top Gear", blurb: "Find the best combination of the gear in your bags and Great Vault.", run: "Run Top Gear" },
  stats: { title: "Stat Weights", blurb: "How much DPS each point of your primary and secondary stats is worth, with a Pawn string.", run: "Run Stat Weights" },
  apl: { title: "APL Compare", blurb: "Run your character with different action priority lists side by side.", run: "Compare APLs" },
  advanced: { title: "Advanced", blurb: "Write raw SimulationCraft input. Anything simc understands works here.", run: "Run Advanced Sim" },
};

const state = {
  status: null,
  classes: [],
  customAPLs: [],
  simType: "quick",
  parsed: null,
  selectedItems: new Set(store.get("selectedItems", [])),
  job: null,
  lib: { cls: store.get("libClass", "deathknight"), spec: store.get("libSpec", null), sel: "default", view: "read", content: "", dirty: false },
};

/* ---------- Engine status ---------- */

async function refreshStatus() {
  try {
    state.status = await api("/api/status");
  } catch (e) {
    state.status = null;
  }
  const s = state.status;
  const badge = $("#engineBadge");
  const banner = $("#engineBanner");
  if (s && s.simcVersion) {
    badge.innerHTML = `<span class="dot ok"></span>${esc(s.simcVersion.replace(/^SimulationCraft\s*/, "SimC "))}`;
    banner.classList.add("hidden");
  } else {
    badge.innerHTML = `<span class="dot"></span>Engine not installed`;
    banner.innerHTML = `<span><b>SimulationCraft isn't installed yet.</b> OfflineSim needs it once; after that everything runs offline.${s && s.simcError ? " (" + esc(s.simcError) + ")" : ""}</span><a class="btn primary" href="#settings">Set it up</a>`;
    banner.classList.remove("hidden");
  }
  if (s) {
    const fsSel = $("#fightStyle");
    if (!fsSel.options.length) {
      fsSel.innerHTML = s.fightStyles.map((f) => `<option>${f}</option>`).join("");
      fsSel.value = store.get("fightStyle", "Patchwerk");
    }
  }
  renderSettings();
  return s;
}

/* ---------- Routing ---------- */

function showPage(id) {
  $$(".page").forEach((p) => p.classList.toggle("hidden", p.id !== "page-" + id));
}

function route() {
  const [page, ...rest] = (location.hash.slice(1) || "quick").split("/");
  $$(".sidebar a").forEach((a) => a.classList.toggle("on", a.dataset.page === page || (page === "result" && a.dataset.page === "results")));
  if (SIM_TYPES[page]) {
    if (state.job) { showPage("running"); return; }
    state.simType = page;
    renderSimPage();
    showPage("sim");
  } else if (page === "running") {
    showPage(state.job ? "running" : "sim");
  } else if (page === "result") {
    showResult(rest[0]);
  } else if (page === "library") {
    if (rest[0]) { state.lib.cls = rest[0]; state.lib.spec = rest[1] || null; }
    renderLibrary();
    showPage("library");
  } else if (page === "results") {
    renderHistory();
    showPage("results");
  } else if (page === "settings") {
    renderSettings();
    showPage("settings");
  } else {
    location.hash = "#quick";
  }
  $("#main").scrollTop = 0;
}

/* ---------- Sim form ---------- */

function renderSimPage() {
  const t = state.simType;
  const meta = SIM_TYPES[t];
  $("#simTitle").textContent = meta.title;
  $("#simBlurb").textContent = meta.blurb;
  $("#runBtn").textContent = meta.run;
  $("#topGearCard").classList.toggle("hidden", t !== "topgear");
  $("#aplCompareCard").classList.toggle("hidden", t !== "apl");
  $("#advancedCard").classList.toggle("hidden", t !== "advanced");
  $("#optionsCard").classList.toggle("hidden", t === "advanced");
  $("#charCard").classList.toggle("hidden", t === "advanced" || !state.parsed || !state.parsed.class);
  $("#profileInput").placeholder = t === "advanced"
    ? "Full simc input, e.g.\n\nmage=\"Test\"\nspec=fire\n...\niterations=5000\nfight_style=DungeonSlice"
    : "Type /simc in game, copy the text from the SimulationCraft addon window and paste it here.";
  const key = t === "advanced" ? "advancedInput" : "profile";
  const want = store.get(key, "");
  if ($("#profileInput").dataset.key !== key) {
    $("#profileInput").value = want;
    $("#profileInput").dataset.key = key;
    onProfileChanged();
  }
  $("#previewOut").classList.add("hidden");
  renderAplSelect();
  renderCompareList();
  renderGear();
}

let parseTimer;
function onProfileChanged() {
  const ta = $("#profileInput");
  store.set(ta.dataset.key || "profile", ta.value);
  clearTimeout(parseTimer);
  parseTimer = setTimeout(async () => {
    if (state.simType === "advanced") return;
    try { state.parsed = await api("/api/parse", { body: { profile: ta.value } }); } catch { state.parsed = null; }
    renderChar();
    renderAplSelect();
    renderCompareList();
    renderGear();
  }, 250);
}

function classInfo(token) { return state.classes.find((c) => c.token === token); }
function specName(cls, spec) {
  const c = classInfo(cls);
  const s = c && c.specs.find((x) => x.token === spec);
  return s ? s.name : spec || "";
}

function renderChar() {
  const p = state.parsed;
  const el = $("#charCard");
  if (!p || !p.class || state.simType === "advanced") { el.classList.add("hidden"); return; }
  const c = classInfo(p.class) || { name: p.class, color: "#999" };
  const where = [p.region && p.region.toUpperCase(), p.server && p.server.replace(/[-_]/g, " ")].filter(Boolean).join(" · ");
  const bags = p.items.filter((i) => !i.equipped).length;
  el.innerHTML = `<div class="crest" style="background:${c.color}">${esc(c.name[0])}</div>
    <div><b style="color:${c.color}">${esc(p.name || "Unnamed")}</b>
    <div class="muted small">Level ${esc(p.level || "?")} ${esc(specName(p.class, p.spec))} ${esc(c.name)}${p.race ? " · " + esc(p.race.replace(/_/g, " ")) : ""}${where ? " · " + esc(where) : ""}</div>
    <div class="muted small">${p.items.filter((i) => i.equipped).length} items equipped${bags ? `, ${bags} in bags/vault` : ""}</div>
    ${p.engineWarning ? `<div class="engine-warn">${esc(p.engineWarning)} <a href="#settings">Open Settings</a></div>` : ""}</div>`;
  el.classList.remove("hidden");
}

function aplsForCurrentSpec() {
  const p = state.parsed;
  if (!p || !p.class) return state.customAPLs;
  return state.customAPLs.filter((a) => a.class === p.class && (!a.spec || !p.spec || a.spec === p.spec));
}

function renderAplSelect() {
  const sel = $("#aplSelect");
  const keep = sel.value || store.get("apl", "default");
  const list = aplsForCurrentSpec();
  sel.innerHTML = `<option value="default">Default (SimulationCraft)</option>` +
    list.map((a) => `<option value="${esc(a.id)}">${esc(a.name)}</option>`).join("");
  sel.value = list.some((a) => a.id === keep) ? keep : "default";
}

function renderCompareList() {
  const list = aplsForCurrentSpec();
  const checked = new Set(store.get("compareTo", []));
  const rows = [{ id: "default", name: "Default (SimulationCraft)" }, ...list];
  $("#compareList").innerHTML = rows.map((a) =>
    `<label><input type="checkbox" value="${esc(a.id)}" ${checked.has(a.id) ? "checked" : ""}> ${esc(a.name)}</label>`).join("") +
    (list.length ? "" : `<p class="muted small">You have no custom APLs for this spec yet. Make one in the <a href="#library">APL Library</a>.</p>`);
}

const SLOT_ORDER = ["head", "neck", "shoulder", "back", "chest", "wrist", "hands", "waist", "legs", "feet", "finger", "trinket", "main_hand", "off_hand"];
const SLOT_NAMES = { head: "Head", neck: "Neck", shoulder: "Shoulders", back: "Back", chest: "Chest", wrist: "Wrists", hands: "Hands", waist: "Waist", legs: "Legs", feet: "Feet", finger: "Rings", trinket: "Trinkets", main_hand: "Main Hand", off_hand: "Off Hand" };

function renderGear() {
  if (state.simType !== "topgear") return;
  const p = state.parsed;
  const grid = $("#gearGrid");
  if (!p || !p.items || !p.items.length) {
    grid.innerHTML = `<p class="muted">Paste a /simc string to pick gear. Make sure "bag items" are included in the addon output.</p>`;
    updateComboCount();
    return;
  }
  const valid = new Set(p.items.filter((i) => !i.equipped).map((i) => itemKey(i)));
  state.selectedItems = new Set([...state.selectedItems].filter((k) => valid.has(k)));
  grid.innerHTML = SLOT_ORDER.map((g) => {
    const items = p.items.filter((i) => i.group === g);
    if (!items.length) return "";
    return `<div class="slot"><h4>${SLOT_NAMES[g] || g}</h4>${items.map((i) => {
      if (i.equipped) return `<label><input type="checkbox" checked disabled><span class="eq">${esc(i.label)}</span><span class="src">equipped</span></label>`;
      const k = itemKey(i);
      return `<label><input type="checkbox" data-key="${esc(k)}" ${state.selectedItems.has(k) ? "checked" : ""}><span>${esc(i.label)}</span><span class="src">${esc(i.source)}</span></label>`;
    }).join("")}</div>`;
  }).join("");
  updateComboCount();
}

// Item keys survive re-pasting the same string; indexes may not.
function itemKey(i) { return i.slot + "|" + i.value; }

function selectedIndexes() {
  const p = state.parsed;
  if (!p) return [];
  return p.items.filter((i) => !i.equipped && state.selectedItems.has(itemKey(i))).map((i) => i.index);
}

let comboTimer, comboSeq = 0;
function updateComboCount() {
  store.set("selectedItems", [...state.selectedItems]);
  const el = $("#comboCount");
  clearTimeout(comboTimer);
  comboTimer = setTimeout(async () => {
    const seq = ++comboSeq;
    let text = "0 combinations", warn = false;
    if (state.selectedItems.size) {
      try {
        const d = await api("/api/preview", { body: buildRequest() });
        text = `${fmt(d.combos)} combination${d.combos === 1 ? "" : "s"}`;
      } catch (e) {
        const m = e.message.match(/that is (\d+) combinations/);
        text = m ? `${fmt(+m[1])} combinations (too many)` : "0 combinations";
        warn = !!m;
      }
    }
    if (seq !== comboSeq) return;
    el.textContent = text;
    el.classList.toggle("warn", warn);
  }, 200);
}

function precisionValue() {
  const [k, v] = $("#precision").value.split(":");
  return k === "it" ? { iterations: +v, targetError: 0 } : { iterations: 0, targetError: +v };
}

function buildRequest() {
  const t = state.simType;
  const req = {
    type: t,
    profile: $("#profileInput").value,
    fightStyle: $("#fightStyle").value,
    maxTime: +$("#maxTime").value,
    targets: +$("#targets").value || 1,
    ...precisionValue(),
    extra: $("#extraInput").value,
    customApl: $("#aplSelect").value,
  };
  if (t === "apl") req.compareTo = $$("#compareList input:checked").map((c) => c.value);
  if (t === "topgear") req.items = selectedIndexes();
  return req;
}

function saveOptions() {
  store.set("fightStyle", $("#fightStyle").value);
  store.set("maxTime", $("#maxTime").value);
  store.set("targets", $("#targets").value);
  store.set("precision", $("#precision").value);
  store.set("extra", $("#extraInput").value);
  store.set("apl", $("#aplSelect").value);
  store.set("compareTo", $$("#compareList input:checked").map((c) => c.value));
  $("#maxTimeLabel").textContent = $("#maxTime").value + "s";
}

function loadOptions() {
  $("#maxTime").value = store.get("maxTime", 300);
  $("#targets").value = store.get("targets", 1);
  $("#precision").value = store.get("precision", "te:0.1");
  $("#extraInput").value = store.get("extra", "");
  $("#maxTimeLabel").textContent = $("#maxTime").value + "s";
}

async function runSim() {
  saveOptions();
  try {
    const { job } = await api("/api/sim", { body: buildRequest() });
    trackJob(job);
  } catch (e) {
    toast(e.message, true);
  }
}

/* ---------- Running ---------- */

async function trackJob(id) {
  state.job = id;
  $("#runLog").textContent = "";
  $("#runBar").style.width = "0";
  location.hash = "#running";
  showPage("running");
  let since = 0;
  while (state.job === id) {
    let s;
    try { s = await api(`/api/jobs/${id}?since=${since}`); } catch (e) { toast(e.message, true); break; }
    since = s.next;
    $("#runTitle").textContent = s.title;
    const bar = $("#runBar").parentElement;
    bar.classList.toggle("indeterminate", s.progress < 0);
    if (s.progress >= 0) $("#runBar").style.width = (s.progress * 100).toFixed(1) + "%";
    $("#runPhase").textContent = s.phase || "Working";
    $("#runElapsed").textContent = Math.round(s.elapsed) + "s";
    if (s.lines.length) {
      const log = $("#runLog");
      log.textContent += s.lines.join("\n") + "\n";
      log.scrollTop = log.scrollHeight;
    }
    if (s.state !== "running") {
      state.job = null;
      if (s.state === "done") { location.hash = "#result/" + s.resultId; }
      else if (s.state === "cancelled") { toast("Simulation cancelled"); history.back(); }
      else {
        toast(s.error || "Simulation failed", true);
        $("#runPhase").textContent = "Failed: " + (s.error || "");
        $("#cancelBtn").textContent = "Back";
        $("#cancelBtn").onclick = () => { $("#cancelBtn").textContent = "Cancel"; $("#cancelBtn").onclick = cancelJob; location.hash = "#" + state.simType; };
      }
      break;
    }
    await new Promise((r) => setTimeout(r, 400));
  }
}

async function cancelJob() {
  if (state.job) await api(`/api/jobs/${state.job}/cancel`, { method: "POST" }).catch(() => {});
}

/* ---------- Results ---------- */

async function showResult(id) {
  const page = $("#page-result");
  showPage("result");
  page.innerHTML = `<div class="empty">Loading…</div>`;
  let r;
  try { r = await api("/api/results/" + encodeURIComponent(id)); } catch (e) { page.innerHTML = `<div class="empty">${esc(e.message)}</div>`; return; }
  const main = r.actors[0];
  const tabs = [];
  if (r.profilesets && r.profilesets.length) tabs.push(["topgear", r.type === "topgear" ? "Top Gear" : "Profilesets"]);
  if (r.actors.length > 1) tabs.push(["compare", "Comparison"]);
  tabs.push(["abilities", "Damage breakdown"]);
  if (r.actors.some((a) => a.scaleFactors && Object.keys(a.scaleFactors).length)) {
    if (r.type === "stats") tabs.unshift(["weights", "Stat weights"]); else tabs.push(["weights", "Stat weights"]);
  }
  tabs.push(["buffs", "Buffs"], ["report", "Full report"], ["input", "Input"]);
  const best = r.profilesets && r.profilesets.length && r.profilesets[0].mean > main.dps.mean ? r.profilesets[0] : null;
  page.innerHTML = `
    <header class="page-head row between"><div><h1>${esc(r.title)}</h1>
      <p class="muted">${esc(new Date(r.created).toLocaleString())} · ${esc(r.fightStyle)} · ${fmt(r.maxTime)}s · ${r.targets > 1 ? r.targets + " targets · " : ""}${fmt(r.iterations)} iterations · ${fmt(r.wallSeconds, 1)}s</p></div>
      <div class="row gap"><button class="btn ghost" id="rerun">Edit &amp; re-run</button><button class="btn ghost" id="openReport">Open report in browser</button><button class="btn danger ghost" id="delResult">Delete</button></div></header>
    ${(r.notes || []).map((n) => `<div class="notes">${esc(n)}</div>`).join("")}
    <div class="card result-hero">
      <div><div class="muted">${esc(main.name)} · ${esc(main.spec)}</div>
        <div class="dps-big">${fmt(main.dps.mean)}<small>DPS ± ${fmt(main.dps.error)}</small></div>
        <div class="stat-row"><span>Min <b>${fmt(main.dps.min)}</b></span><span>Max <b>${fmt(main.dps.max)}</b></span><span>Std dev <b>${fmt(main.dps.stdDev)}</b></span>${main.hps > 1 ? `<span>HPS <b>${fmt(main.hps)}</b></span>` : ""}${main.dtps > 1 ? `<span>DTPS <b>${fmt(main.dtps)}</b></span>` : ""}<span>Fight <b>${fmt(main.fightLength, 1)}s</b></span></div>
      </div>
      ${best ? `<div class="card" style="margin:0"><div class="muted small">Best gear set</div><div class="dps-big" style="font-size:26px">${fmt(best.mean)} <small class="delta-pos">+${fmt(best.mean - main.dps.mean)} (${pct((best.mean - main.dps.mean) / main.dps.mean, 2)})</small></div></div>` : ""}
    </div>
    <div class="tabs">${tabs.map(([k, l], i) => `<button data-tab="${k}" class="${i === 0 ? "on" : ""}">${l}</button>`).join("")}</div>
    <div id="resultBody"></div>`;
  const body = $("#resultBody");
  const render = (k) => {
    $$(".tabs button", page).forEach((b) => b.classList.toggle("on", b.dataset.tab === k));
    if (k === "topgear") body.innerHTML = renderProfilesets(r, main);
    if (k === "compare") body.innerHTML = renderActors(r);
    if (k === "abilities") body.innerHTML = r.actors.map((a) => renderAbilities(a, r.actors.length > 1)).join("");
    if (k === "weights") body.innerHTML = r.actors.filter((a) => a.scaleFactors && Object.keys(a.scaleFactors).length).map(renderWeights).join("");
    if (k === "buffs") body.innerHTML = r.actors.map(renderBuffs).join("");
    if (k === "report") body.innerHTML = `<iframe class="report-frame" src="/reports/${encodeURIComponent(r.id)}/report.html"></iframe>`;
    if (k === "input") {
      body.innerHTML = `<div class="card"><div class="card-head"><h2>simc input</h2><div class="row gap"><a class="btn ghost" href="/reports/${encodeURIComponent(r.id)}/log.txt" target="_blank">simc log</a><a class="btn ghost" href="/reports/${encodeURIComponent(r.id)}/result.json" target="_blank">Raw JSON</a></div></div><pre class="code-block" id="inputText">Loading…</pre></div>`;
      api(`/api/results/${encodeURIComponent(r.id)}/input`).then((d) => { $("#inputText").textContent = d.input; });
    }
    $$("[data-toggle]", body).forEach((tr) => tr.addEventListener("click", () => {
      $$(`tr[data-parent="${tr.dataset.toggle}"]`, body).forEach((c) => c.classList.toggle("hidden"));
    }));
    $$("[data-copy]", body).forEach((b) => b.addEventListener("click", () => copyText($(b.dataset.copy).textContent)));
  };
  $$(".tabs button", page).forEach((b) => b.addEventListener("click", () => render(b.dataset.tab)));
  render(tabs[0][0]);
  $("#openReport").onclick = () => api("/api/open", { body: { what: "report", id: r.id } });
  $("#delResult").onclick = async () => {
    if (!confirm("Delete this result?")) return;
    await api("/api/results/" + encodeURIComponent(r.id), { method: "DELETE" });
    location.hash = "#results";
  };
  $("#rerun").onclick = async () => {
    const d = await api(`/api/results/${encodeURIComponent(r.id)}/input`);
    const q = d.request;
    if (!q) return;
    const type = SIM_TYPES[q.type] ? q.type : "quick";
    store.set(type === "advanced" ? "advancedInput" : "profile", q.profile);
    $("#profileInput").dataset.key = "";
    if (q.fightStyle) $("#fightStyle").value = q.fightStyle;
    if (q.maxTime) $("#maxTime").value = q.maxTime;
    $("#targets").value = q.targets || 1;
    $("#extraInput").value = q.extra || "";
    saveOptions();
    location.hash = "#" + type;
  };
}

function renderProfilesets(r, main) {
  const base = main.dps.mean;
  const rows = [{ name: "Currently equipped", mean: base, error: main.dps.error, changes: [], baseline: true }, ...r.profilesets]
    .sort((a, b) => b.mean - a.mean);
  const top = rows[0].mean || 1;
  return `<div class="card"><table class="table"><tr><th>#</th><th>Gear changes</th><th class="num">DPS</th><th class="num">vs equipped</th><th></th></tr>
    ${rows.map((p, i) => {
      const d = p.mean - base;
      return `<tr><td>${i + 1}</td><td>${p.baseline ? "<b>Currently equipped</b>" : (p.changes && p.changes.length ? p.changes.map(esc).join("<br>") : esc(p.name))}</td>
      <td class="num">${fmt(p.mean)}<div class="muted small">± ${fmt(p.error)}</div></td>
      <td class="num ${d > 0 ? "delta-pos" : d < 0 ? "delta-neg" : ""}">${p.baseline ? "–" : (d >= 0 ? "+" : "") + fmt(d) + " (" + pct(d / base, 2) + ")"}</td>
      <td style="width:22%"><div class="bar ${p.baseline ? "gold" : ""}"><div style="width:${(p.mean / top) * 100}%"></div></div></td></tr>`;
    }).join("")}</table></div>`;
}

function renderActors(r) {
  const top = Math.max(...r.actors.map((a) => a.dps.mean)) || 1;
  const base = r.actors[0].dps.mean;
  return `<div class="card actors-chart">${r.actors.slice().sort((a, b) => b.dps.mean - a.dps.mean).map((a) => {
    const d = a.dps.mean - base;
    return `<div class="row"><span class="name" title="${esc(a.name)}">${esc(a.name)}</span><div class="bar ${a === r.actors[0] ? "gold" : ""}"><div style="width:${(a.dps.mean / top) * 100}%"></div></div>
      <span style="width:120px;text-align:right">${fmt(a.dps.mean)}</span><span style="width:120px;text-align:right" class="${d > 0 ? "delta-pos" : d < 0 ? "delta-neg" : "muted"}">${a === r.actors[0] ? "baseline" : (d >= 0 ? "+" : "") + pct(d / base, 2)}</span></div>`;
  }).join("")}<p class="muted small">The first actor (gold) is your character with the APL picked under Options. Each other actor is a copy with a different APL.</p></div>`;
}

let abilityRowId = 0;
function renderAbilities(a, showName) {
  const top = a.abilities.length ? a.abilities[0].dps : 1;
  const row = (ab, parent) => {
    const id = "ab" + ++abilityRowId;
    const kids = ab.children || [];
    return `<tr class="${parent ? "child hidden" : ""} ${kids.length ? "click" : ""}" ${parent ? `data-parent="${parent}"` : ""} ${kids.length ? `data-toggle="${id}"` : ""}>
      <td><span class="school-${esc((ab.school || "").toLowerCase())}">${esc(ab.name)}</span>${kids.length ? ` <span class="muted small">▸ ${kids.length}</span>` : ""}${ab.source && ab.source !== a.name ? ` <span class="muted small">(${esc(ab.source)})</span>` : ""}</td>
      <td class="num">${fmt(ab.dps)}</td><td class="num">${pct(ab.share)}</td><td style="width:30%"><div class="bar"><div style="width:${(ab.dps / top) * 100}%"></div></div></td>
      <td class="num">${ab.casts ? fmt(ab.casts, 1) : "–"}</td></tr>` + kids.map((k) => row(k, id)).join("");
  };
  return `<div class="card">${showName ? `<div class="card-head"><h2>${esc(a.name)}</h2><span class="muted">${fmt(a.dps.mean)} DPS</span></div>` : ""}
    <table class="table"><tr><th>Ability</th><th class="num">DPS</th><th class="num">Share</th><th></th><th class="num">Casts</th></tr>
    ${a.abilities.map((ab) => row(ab)).join("") || `<tr><td colspan="5" class="muted">No damage recorded.</td></tr>`}</table></div>`;
}

const PAWN = { Str: "Strength", Agi: "Agility", Int: "Intellect", Crit: "CritRating", Haste: "HasteRating", Mastery: "MasteryRating", Vers: "Versatility", Wdps: "Dps", WOHdps: "OffHandDps" };
function renderWeights(a) {
  const sf = a.scaleFactors;
  const keys = Object.keys(sf).filter((k) => sf[k] !== 0 || PAWN[k]).sort((x, y) => sf[y] - sf[x]);
  const prim = keys.find((k) => ["Str", "Agi", "Int"].includes(k));
  const norm = prim && sf[prim] ? sf[prim] : Math.max(...keys.map((k) => sf[k])) || 1;
  const max = Math.max(...keys.map((k) => sf[k])) || 1;
  const pawnId = "pawn" + Math.random().toString(36).slice(2);
  const cls = state.classes.find((c) => a.spec.endsWith(c.name));
  const pawnClass = (cls ? cls.name : a.spec.split(" ").pop()).replace(/\s/g, "");
  const pawnSpec = (cls ? a.spec.slice(0, -cls.name.length) : a.spec.split(" ")[0]).replace(/\s/g, "");
  const pawn = `( Pawn: v1: "${a.name}-${a.spec}": Class=${pawnClass}, Spec=${pawnSpec}, ` +
    keys.filter((k) => PAWN[k]).map((k) => `${PAWN[k]}=${(sf[k] / norm).toFixed(2)}`).join(", ") + " )";
  return `<div class="card"><div class="card-head"><h2>${esc(a.name)}</h2><span class="muted small">DPS per point of stat</span></div>
    <table class="table"><tr><th>Stat</th><th class="num">DPS per point</th><th class="num">Normalized</th><th></th></tr>
    ${keys.map((k) => `<tr><td>${esc(PAWN[k] ? PAWN[k].replace("Rating", "") : k)}</td><td class="num">${fmt(sf[k], 3)}</td><td class="num">${fmt(sf[k] / norm, 2)}</td><td style="width:35%"><div class="bar gold"><div style="width:${Math.max(0, (sf[k] / max) * 100)}%"></div></div></td></tr>`).join("")}</table>
    <h3>Pawn string</h3><pre class="code-block" id="${pawnId}">${esc(pawn)}</pre><button class="btn ghost" data-copy="#${pawnId}">Copy Pawn string</button></div>`;
}

function renderBuffs(a) {
  if (!a.buffs || !a.buffs.length) return `<div class="card muted">No buff data for ${esc(a.name)}.</div>`;
  return `<div class="card"><div class="card-head"><h2>${esc(a.name)}</h2></div><table class="table"><tr><th>Buff</th><th class="num">Uptime</th><th></th></tr>
    ${a.buffs.map((b) => `<tr><td>${esc(b.name.replace(/_/g, " "))}</td><td class="num">${fmt(b.uptime, 1)}%</td><td style="width:40%"><div class="bar"><div style="width:${Math.min(100, b.uptime)}%"></div></div></td></tr>`).join("")}</table></div>`;
}

async function renderHistory() {
  const t = $("#historyTable");
  let rows = [];
  try { rows = await api("/api/results"); } catch {}
  if (!rows.length) { t.innerHTML = `<tr><td class="empty">No sims yet. Start with a <a href="#quick">Quick Sim</a>.</td></tr>`; return; }
  t.innerHTML = `<tr><th>When</th><th>Sim</th><th>Type</th><th>Spec</th><th>Fight</th><th class="num">DPS</th></tr>` +
    rows.map((r) => `<tr class="click" data-id="${esc(r.id)}"><td>${esc(new Date(r.created).toLocaleString())}</td><td>${esc(r.title)}</td><td>${esc(SIM_TYPES[r.type] ? SIM_TYPES[r.type].title : r.type)}</td><td>${esc(r.spec)}</td><td>${esc(r.fight)}</td><td class="num">${fmt(r.dps)}</td></tr>`).join("");
  $$("tr[data-id]", t).forEach((tr) => tr.addEventListener("click", () => (location.hash = "#result/" + tr.dataset.id)));
}

/* ---------- APL library ---------- */

async function loadCustomAPLs() {
  try { state.customAPLs = await api("/api/apl/custom"); } catch { state.customAPLs = []; }
}

function renderLibrary() {
  const L = state.lib;
  const cls = classInfo(L.cls) || state.classes[0];
  if (!cls) return;
  L.cls = cls.token;
  if (!L.spec || !cls.specs.some((s) => s.token === L.spec)) L.spec = cls.specs[0].token;
  store.set("libClass", L.cls); store.set("libSpec", L.spec);
  $("#classStrip").innerHTML = state.classes.map((c) =>
    `<button data-cls="${c.token}" style="color:${c.color}" class="${c.token === L.cls ? "on" : ""}">${esc(c.name)}</button>`).join("");
  $("#specTabs").innerHTML = cls.specs.map((s) =>
    `<button data-spec="${s.token}" class="${s.token === L.spec ? "on" : ""}">${esc(s.name)}<span class="role">${s.role}</span></button>`).join("");
  $("#libDefaultItem").innerHTML = `<button class="lib-item ${L.sel === "default" ? "on" : ""}" data-apl="default">${esc(specName(L.cls, L.spec))} ${esc(cls.name)}<small>SimulationCraft default</small></button>`;
  const mine = state.customAPLs.filter((a) => a.class === L.cls && a.spec === L.spec);
  $("#libCustomList").innerHTML = mine.map((a) =>
    `<button class="lib-item ${L.sel === a.id ? "on" : ""}" data-apl="${esc(a.id)}">${esc(a.name)}<small>edited ${esc(new Date(a.updated).toLocaleDateString())}</small></button>`).join("") ||
    `<p class="muted small">None yet. Duplicate the default to start.</p>`;
  $("#libAllCustom").innerHTML = state.customAPLs.map((a) =>
    `<button class="lib-item" data-goto="${esc(a.class)}/${esc(a.spec)}/${esc(a.id)}">${esc(a.name)}<small>${esc(specName(a.class, a.spec))} ${esc((classInfo(a.class) || {}).name || a.class)}</small></button>`).join("") ||
    `<p class="muted small">Nothing saved.</p>`;
  $$("#classStrip button").forEach((b) => b.onclick = () => guardDirty(() => { L.cls = b.dataset.cls; L.spec = null; L.sel = "default"; renderLibrary(); }));
  $$("#specTabs button").forEach((b) => b.onclick = () => guardDirty(() => { L.spec = b.dataset.spec; L.sel = "default"; renderLibrary(); }));
  $$(".lib-item[data-apl]").forEach((b) => b.onclick = () => guardDirty(() => { L.sel = b.dataset.apl; renderLibrary(); }));
  $$(".lib-item[data-goto]").forEach((b) => b.onclick = () => guardDirty(() => { const [c, s, id] = b.dataset.goto.split("/"); L.cls = c; L.spec = s; L.sel = id; renderLibrary(); }));
  loadAplContent();
}

function guardDirty(fn) {
  if (state.lib.dirty && !confirm("Discard unsaved changes to this APL?")) return;
  state.lib.dirty = false;
  fn();
}

let aplLoadSeq = 0;
async function loadAplContent(refresh = false) {
  const L = state.lib;
  const seq = ++aplLoadSeq;
  const isDefault = L.sel === "default";
  $("#aplName").value = isDefault ? `${specName(L.cls, L.spec)} ${(classInfo(L.cls) || {}).name} (default)` : "";
  $("#aplName").readOnly = isDefault;
  $("#aplSave").classList.toggle("hidden", isDefault);
  $("#aplDelete").classList.toggle("hidden", isDefault || L.sel === "new");
  $("#aplRefresh").classList.toggle("hidden", !isDefault);
  $("#aplDup").classList.toggle("hidden", L.sel === "new");
  $("#aplText").readOnly = isDefault;
  $("#aplMsg").textContent = isDefault ? "Generating from your SimulationCraft build…" : "";
  let content = "";
  try {
    if (isDefault) {
      const d = await api(`/api/apl/default?class=${L.cls}&spec=${L.spec}${refresh ? "&refresh=1" : ""}`);
      content = d.content;
      if (seq === aplLoadSeq) $("#aplMsg").textContent = "Read-only. This is exactly what the engine runs when no custom APL is set. Use Duplicate as mine to edit it.";
    } else if (L.sel === "new") {
      $("#aplName").value = "My " + specName(L.cls, L.spec) + " APL";
      content = "# Action lists run top to bottom; the first usable action fires.\nactions.precombat=snapshot_stats\nactions=auto_attack\n";
    } else {
      const c = await api("/api/apl/custom/" + encodeURIComponent(L.sel));
      $("#aplName").value = c.name;
      content = c.content;
    }
  } catch (e) {
    if (seq === aplLoadSeq) $("#aplMsg").textContent = e.message;
  }
  if (seq !== aplLoadSeq) return;
  L.content = content;
  L.dirty = L.sel === "new";
  $("#aplText").value = content;
  setAplView(L.sel === "new" ? "text" : L.view);
}

function setAplView(v) {
  state.lib.view = v;
  $$("#aplView button").forEach((b) => b.classList.toggle("on", b.dataset.v === v));
  $("#aplText").classList.toggle("hidden", v !== "text");
  $("#aplReadable").classList.toggle("hidden", v !== "read");
  if (v === "read") $("#aplReadable").innerHTML = renderReadableAPL($("#aplText").value);
}

// Turn "actions.list+=/spell,if=cond,target_if=x" lines into a readable table.
function renderReadableAPL(text) {
  const lists = new Map();
  let pendingComments = [];
  const order = [];
  for (const raw of text.split(/\r?\n/)) {
    const line = raw.trim();
    if (!line) continue;
    if (line.startsWith("#")) { pendingComments.push(line.replace(/^#\s?/, "")); continue; }
    const m = line.match(/^actions(?:\.([\w-]+))?\s*(\+?=)\s*\/?(.*)$/);
    if (!m) continue;
    const list = m[1] || "default";
    if (!lists.has(list)) { lists.set(list, []); order.push(list); }
    const parts = splitTopLevel(m[3]);
    const action = parts.shift() || "";
    lists.get(list).push({ action, opts: parts, comments: pendingComments });
    pendingComments = [];
  }
  if (!order.length) return `<p class="muted">No action lines found.</p>`;
  return order.map((list) => {
    let n = 0;
    return `<div class="apl-list"><h4>${list === "default" ? "actions (main list)" : "actions." + esc(list)}</h4>` +
      lists.get(list).map((e) => e.comments.map((c) => `<div class="apl-comment">${esc(c)}</div>`).join("") +
        `<div class="apl-line"><span class="n">${++n}</span><span class="act ${/^(call|run)_action_list$/.test(e.action) ? "call" : ""}">${esc(e.action.replace(/_/g, " "))}</span>
        <span class="cond">${e.opts.map((o) => { const i = o.indexOf("="); return i > 0 ? `<span class="k">${esc(o.slice(0, i))}=</span>${esc(o.slice(i + 1))}` : esc(o); }).join("<br>")}</span></div>`).join("") + `</div>`;
  }).join("");
}

// Split on commas that aren't inside parentheses.
function splitTopLevel(s) {
  const out = [];
  let depth = 0, cur = "";
  for (const ch of s) {
    if (ch === "(") depth++;
    if (ch === ")") depth--;
    if (ch === "," && depth === 0) { out.push(cur); cur = ""; continue; }
    cur += ch;
  }
  if (cur) out.push(cur);
  return out;
}

async function saveApl() {
  const L = state.lib;
  try {
    const c = await api("/api/apl/custom", { body: { id: L.sel === "new" || L.sel === "default" ? "" : L.sel, name: $("#aplName").value, class: L.cls, spec: L.spec, content: $("#aplText").value } });
    L.sel = c.id;
    L.dirty = false;
    await loadCustomAPLs();
    renderLibrary();
    toast("APL saved");
  } catch (e) { toast(e.message, true); }
}

async function duplicateApl() {
  const L = state.lib;
  const base = $("#aplName").value.replace(/\s*\(default\)$/, "");
  try {
    const c = await api("/api/apl/custom", { body: { name: base + " (copy)", class: L.cls, spec: L.spec, content: $("#aplText").value } });
    L.sel = c.id;
    L.view = "text";
    await loadCustomAPLs();
    renderLibrary();
    toast("Copied. Edit it in the Text view and Save.");
  } catch (e) { toast(e.message, true); }
}

async function deleteApl() {
  const L = state.lib;
  if (!confirm("Delete this APL?")) return;
  await api("/api/apl/custom/" + encodeURIComponent(L.sel), { method: "DELETE" }).catch((e) => toast(e.message, true));
  L.sel = "default";
  L.dirty = false;
  await loadCustomAPLs();
  renderLibrary();
}

/* ---------- Settings ---------- */

function renderSettings() {
  const s = state.status;
  if (!s) return;
  const kv = [
    ["Engine", s.simcVersion ? esc(s.simcVersion) : `<span class="delta-neg">Not installed</span>`],
    ["Game version", s.engine && s.engine.wow ? `WoW ${esc(s.engine.wow)}${s.engine.expansion ? " · " + esc(s.engine.expansion) : ""}${s.engine.levelCap ? " · level cap " + s.engine.levelCap : ""}` : "–"],
    ["simc executable", s.simcPath ? esc(s.simcPath) : "–"],
    ["Installed build", s.install ? `${esc(s.install.name)} <span class="muted">(${esc(new Date(s.install.installedAt).toLocaleString())})</span>` : "–"],
    ["Data folder", esc(s.dataDir)],
    ["CPU threads used", s.threads],
    ["OfflineSim", "v" + esc(s.appVersion)],
  ];
  $("#engineInfo").innerHTML = kv.map(([k, v]) => `<div class="row" style="gap:12px;padding:3px 0"><span class="muted" style="width:150px">${k}</span><span>${v}</span></div>`).join("");
  if (!$("#setThreads").dataset.loaded) {
    $("#setSimcPath").value = s.settings.simcPath || "";
    $("#setThreads").value = s.settings.threads || 0;
    $("#setNightly").value = s.settings.nightlyUrl || "";
    $("#setLowPri").checked = !!s.settings.lowPriority;
    $("#setThreads").dataset.loaded = "1";
  }
  if (s.installJob && !installTracking) trackInstall(s.installJob);
}

let installTracking = false;
async function trackInstall(id) {
  installTracking = true;
  $("#installLog").classList.remove("hidden");
  $("#installBtn").disabled = true;
  const text = $("#installLogText");
  text.textContent = "";
  let since = 0;
  for (;;) {
    let s;
    try { s = await api(`/api/jobs/${id}?since=${since}`); } catch { break; }
    since = s.next;
    if (s.lines.length) text.textContent += s.lines.join("\n") + "\n";
    const bar = $("#installBar");
    bar.parentElement.classList.toggle("indeterminate", s.progress < 0);
    if (s.progress >= 0) bar.style.width = (s.progress * 100).toFixed(1) + "%";
    if (s.phase && s.state === "running") text.dataset.phase = s.phase;
    if (s.state !== "running") {
      if (s.state === "done") toast("SimulationCraft is ready");
      else toast(s.error || "Install failed", true);
      break;
    }
    await new Promise((r) => setTimeout(r, 400));
  }
  installTracking = false;
  $("#installBtn").disabled = false;
  await refreshStatus();
  loadSamples();
}

async function loadSamples() {
  let list = [];
  try { list = await api("/api/profiles"); } catch {}
  const sel = $("#sampleSelect");
  const groups = {};
  for (const p of list) (groups[p.tier] = groups[p.tier] || []).push(p);
  sel.innerHTML = `<option value="">${list.length ? "Load a sample character…" : (state.status && state.status.simcVersion ? "No sample characters found" : "No sample characters (install the engine)")}</option>` +
    Object.entries(groups).map(([tier, ps]) => `<optgroup label="${esc(tier)}">${ps.map((p) => `<option value="${esc(p.path)}">${esc(p.name)}</option>`).join("")}</optgroup>`).join("");
}

async function copyText(t) {
  try { await navigator.clipboard.writeText(t); toast("Copied"); }
  catch {
    const ta = document.createElement("textarea");
    ta.value = t; document.body.appendChild(ta); ta.select(); document.execCommand("copy"); ta.remove(); toast("Copied");
  }
}

/* ---------- Wiring ---------- */

function wire() {
  window.addEventListener("hashchange", route);
  $("#profileInput").addEventListener("input", onProfileChanged);
  $("#pasteBtn").onclick = async () => {
    try {
      const t = await navigator.clipboard.readText();
      if (!t) return toast("Clipboard is empty", true);
      $("#profileInput").value = t;
      onProfileChanged();
    } catch { toast("Couldn't read the clipboard. Click the box and press Ctrl+V.", true); }
  };
  $("#sampleSelect").onchange = async (e) => {
    if (!e.target.value) return;
    try {
      const d = await api("/api/profile?path=" + encodeURIComponent(e.target.value));
      $("#profileInput").value = d.content;
      onProfileChanged();
    } catch (err) { toast(err.message, true); }
    e.target.value = "";
  };
  ["fightStyle", "maxTime", "targets", "precision", "extraInput", "aplSelect"].forEach((id) => $("#" + id).addEventListener("input", saveOptions));
  $("#compareList").addEventListener("change", saveOptions);
  $("#gearGrid").addEventListener("change", (e) => {
    const k = e.target.dataset.key;
    if (!k) return;
    e.target.checked ? state.selectedItems.add(k) : state.selectedItems.delete(k);
    updateComboCount();
  });
  $("#selectAllItems").onclick = () => { (state.parsed?.items || []).filter((i) => !i.equipped).forEach((i) => state.selectedItems.add(itemKey(i))); renderGear(); };
  $("#clearItems").onclick = () => { state.selectedItems.clear(); renderGear(); };
  $("#runBtn").onclick = runSim;
  $("#previewBtn").onclick = async () => {
    saveOptions();
    const out = $("#previewOut");
    try {
      const d = await api("/api/preview", { body: buildRequest() });
      out.textContent = d.input;
    } catch (e) { out.textContent = e.message; }
    out.classList.remove("hidden");
    out.scrollIntoView({ behavior: "smooth" });
  };
  $("#cancelBtn").onclick = cancelJob;

  $$("#aplView button").forEach((b) => b.onclick = () => setAplView(b.dataset.v));
  $("#aplText").addEventListener("input", () => { if (state.lib.sel !== "default") state.lib.dirty = true; });
  $("#aplName").addEventListener("input", () => { if (state.lib.sel !== "default") state.lib.dirty = true; });
  $("#aplSave").onclick = saveApl;
  $("#aplDup").onclick = duplicateApl;
  $("#aplDelete").onclick = deleteApl;
  $("#aplRefresh").onclick = () => loadAplContent(true);
  $("#aplCopy").onclick = () => copyText($("#aplText").value);
  $("#newAplBtn").onclick = () => guardDirty(() => { state.lib.sel = "new"; renderLibrary(); });

  $("#installBtn").onclick = async () => {
    try { const { job } = await api("/api/simc/install", { method: "POST" }); trackInstall(job); }
    catch (e) { toast(e.message, true); }
  };
  $("#installFile").onchange = async (e) => {
    const f = e.target.files[0];
    if (!f) return;
    try {
      const { job } = await api("/api/simc/upload?name=" + encodeURIComponent(f.name), { body: f, raw: true });
      trackInstall(job);
    } catch (err) { toast(err.message, true); }
    e.target.value = "";
  };
  $("#saveSettings").onclick = async () => {
    try {
      await api("/api/settings", { body: {
        ...state.status.settings,
        simcPath: $("#setSimcPath").value.trim().replace(/^"|"$/g, ""),
        threads: +$("#setThreads").value || 0,
        nightlyUrl: $("#setNightly").value.trim(),
        lowPriority: $("#setLowPri").checked,
      } });
      toast("Settings saved");
      await refreshStatus();
      loadSamples();
    } catch (e) { toast(e.message, true); }
  };
  $("#openData").onclick = () => api("/api/open", { body: { what: "data" } });
  $("#quitBtn").onclick = async () => {
    if (!confirm("Quit OfflineSim?")) return;
    await api("/api/quit", { method: "POST" }).catch(() => {});
    document.body.innerHTML = `<div class="empty">OfflineSim has stopped. You can close this window.</div>`;
  };
}

(async function init() {
  wire();
  loadOptions();
  try { state.classes = await api("/api/classes"); } catch {}
  await Promise.all([refreshStatus(), loadCustomAPLs()]);
  loadSamples();
  if (state.status && state.status.simJob) state.job = null, trackJob(state.status.simJob);
  else route();
})();
