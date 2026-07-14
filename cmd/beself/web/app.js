const dateInput = document.querySelector("#date");
const trackingButton = document.querySelector("#tracking");
const startupButton = document.querySelector("#startup");
const tooltip = document.querySelector("#tooltip");
const timelineEl = document.querySelector("#timeline");
let currentData = null;
let selectedRange = null;
let selecting = false;

const statusClass = {
  active: "active",
  locked: "locked",
  idle: "idle",
  unknown: "idle",
  tracking_off: "idle"
};

dateInput.value = new Date().toISOString().slice(0, 10);
dateInput.addEventListener("change", load);
timelineEl.addEventListener("mousedown", startTimelineSelection);
timelineEl.addEventListener("mousemove", moveTimelineSelection);
window.addEventListener("mouseup", finishTimelineSelection);
trackingButton.addEventListener("click", async () => {
  const enabled = trackingButton.dataset.enabled !== "true";
  await fetch("/api/tracking", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ enabled })
  });
  await load();
});
startupButton.addEventListener("click", async () => {
  const enabled = startupButton.dataset.enabled !== "true";
  await fetch("/api/startup", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ enabled })
  });
  await loadStartup();
});

async function load() {
  const [dayRes, weekRes] = await Promise.all([
    fetch(`/api/day?date=${dateInput.value}`),
    fetch(`/api/week?date=${dateInput.value}`)
  ]);
  const data = await dayRes.json();
  const week = await weekRes.json();
  document.querySelector("#worked").textContent = duration(data.totals.activeSeconds);
  document.querySelector("#weekAvg").textContent = duration(week.averageActiveSeconds);
  document.querySelector("#locked").textContent = duration(data.totals.lockedSeconds);
  document.querySelector("#idle").textContent = duration(data.totals.idleSeconds + data.totals.unknownSeconds);
  document.querySelector("#weekTotal").textContent = `${duration(week.totalActiveSeconds)} total active`;
  trackingButton.dataset.enabled = data.tracking ? "true" : "false";
  trackingButton.textContent = data.tracking ? "Stop tracking" : "Start tracking";
  trackingButton.classList.toggle("primary", !data.tracking);
  currentData = data;
  selectedRange = null;
  renderTimeline(data);
  renderSelection();
  renderWeek(week);
  renderPrograms(data.programs);
  await loadStartup();
}

async function loadStartup() {
  const res = await fetch("/api/startup");
  const data = await res.json();
  startupButton.dataset.enabled = data.enabled ? "true" : "false";
  startupButton.textContent = data.enabled ? "Disable startup" : "Enable startup";
}

function renderTimeline(data) {
  const el = timelineEl;
  el.innerHTML = "";
  const start = new Date(`${data.date}T00:00:00`);
  const end = new Date(start.getTime() + 24 * 60 * 60 * 1000);
  for (const session of data.sessions) {
    const s = clamp(new Date(session.startTime), start, end);
    const e = clamp(session.endTime ? new Date(session.endTime) : new Date(), start, end);
    if (e <= s) continue;
    const block = document.createElement("div");
    block.className = `block ${statusClass[session.status] || "idle"}`;
    block.style.left = `${((s - start) / (end - start)) * 100}%`;
    block.style.width = `${((e - s) / (end - start)) * 100}%`;
    block.dataset.tip = tip(session, s, e);
    block.addEventListener("mousemove", showTip);
    block.addEventListener("mouseleave", hideTip);
    el.appendChild(block);
  }
  const selection = document.createElement("div");
  selection.id = "timelineSelection";
  selection.className = "timeline-selection";
  selection.hidden = true;
  el.appendChild(selection);
}

function startTimelineSelection(event) {
  if (!currentData) return;
  selecting = true;
  const t = timeFromTimelineEvent(event);
  selectedRange = { start: t, end: t };
  renderSelection();
}

function moveTimelineSelection(event) {
  if (!selecting || !selectedRange) return;
  selectedRange.end = timeFromTimelineEvent(event);
  renderSelection();
  renderPrograms(filteredPrograms());
}

function finishTimelineSelection(event) {
  if (!selecting || !selectedRange) return;
  selecting = false;
  selectedRange.end = timeFromTimelineEvent(event);
  if (Math.abs(selectedRange.end - selectedRange.start) < 60 * 1000) {
    selectedRange = null;
    renderPrograms(currentData.programs);
  } else {
    renderPrograms(filteredPrograms());
  }
  renderSelection();
}

function renderSelection() {
  const el = document.querySelector("#timelineSelection");
  if (!el || !selectedRange || !currentData) {
    if (el) el.hidden = true;
    return;
  }
  const start = new Date(`${currentData.date}T00:00:00`);
  const end = new Date(start.getTime() + 24 * 60 * 60 * 1000);
  const a = clamp(new Date(Math.min(selectedRange.start, selectedRange.end)), start, end);
  const b = clamp(new Date(Math.max(selectedRange.start, selectedRange.end)), start, end);
  el.hidden = false;
  el.style.left = `${((a - start) / (end - start)) * 100}%`;
  el.style.width = `${((b - a) / (end - start)) * 100}%`;
}

function timeFromTimelineEvent(event) {
  const rect = timelineEl.getBoundingClientRect();
  const x = Math.min(Math.max(event.clientX - rect.left, 0), rect.width);
  const start = new Date(`${currentData.date}T00:00:00`);
  return new Date(start.getTime() + (x / rect.width) * 24 * 60 * 60 * 1000);
}

function filteredPrograms() {
  if (!selectedRange || !currentData) return currentData?.programs || [];
  const rangeStart = new Date(Math.min(selectedRange.start, selectedRange.end));
  const rangeEnd = new Date(Math.max(selectedRange.start, selectedRange.end));
  const map = new Map();
  let total = 0;
  for (const session of currentData.sessions) {
    if (session.status !== "active") continue;
    const s = new Date(session.startTime);
    const e = session.endTime ? new Date(session.endTime) : new Date();
    const from = new Date(Math.max(s, rangeStart));
    const to = new Date(Math.min(e, rangeEnd));
    if (to <= from) continue;
    const seconds = Math.floor((to - from) / 1000);
    total += seconds;
    const key = `${session.processName}\0${session.executablePath}`;
    const item = map.get(key) || {
      processName: session.processName,
      executablePath: session.executablePath,
      totalSeconds: 0,
      firstUsed: from,
      lastUsed: to,
      percentActiveTime: 0
    };
    item.totalSeconds += seconds;
    if (from < new Date(item.firstUsed)) item.firstUsed = from;
    if (to > new Date(item.lastUsed)) item.lastUsed = to;
    map.set(key, item);
  }
  return [...map.values()]
    .map(item => ({ ...item, percentActiveTime: total ? item.totalSeconds * 100 / total : 0 }))
    .sort((a, b) => b.totalSeconds - a.totalSeconds);
}

function renderWeek(week) {
  const el = document.querySelector("#weekChart");
  el.innerHTML = "";
  const max = Math.max(1, ...week.days.map(day => day.activeSeconds));
  for (const day of week.days) {
    const item = document.createElement("div");
    item.className = "week-bar";
    item.dataset.date = day.date;
    const date = new Date(`${day.date}T00:00:00`);
    item.innerHTML = `
      <div class="bar-track"><i style="height:${Math.max(2, day.activeSeconds / max * 100)}%"></i></div>
      <span>${date.toLocaleDateString([], { weekday: "short" })}</span>
      <strong>${duration(day.activeSeconds)}</strong>`;
    item.addEventListener("click", () => {
      dateInput.value = day.date;
      selectedRange = null;
      load();
    });
    el.appendChild(item);
  }
}

function renderPrograms(programs) {
  const list = document.querySelector("#programList");
  const table = document.querySelector("#programTable");
  list.innerHTML = "";
  table.innerHTML = "";
  if (!programs.length) {
    list.innerHTML = `<p class="muted">No active program time recorded for this date.</p>`;
    return;
  }
  for (const program of programs.slice(0, 8)) {
    const row = document.createElement("div");
    row.className = "program-row";
    row.innerHTML = `<span>${programIcon(program.processName)}${escapeHtml(program.processName || "Unknown")}</span><strong>${duration(program.totalSeconds)}</strong>`;
    list.appendChild(row);
  }
  for (const program of programs) {
    const tr = document.createElement("tr");
    tr.innerHTML = `
      <td>${programIcon(program.processName)}${escapeHtml(program.processName || "Unknown")}</td>
      <td>${duration(program.totalSeconds)}</td>
      <td>${timeOnly(program.firstUsed)}</td>
      <td>${timeOnly(program.lastUsed)}</td>
      <td>${program.percentActiveTime.toFixed(1)}%</td>`;
    table.appendChild(tr);
  }
}

function initial(value) {
  const text = String(value || "?").replace(/\.(exe|app)$/i, "").trim();
  return escapeHtml((text[0] || "?").toUpperCase());
}

function programIcon(value) {
  const name = String(value || "").replace(/\.exe$/i, "").toLowerCase();
  const domain = `${name}.com`;
  return `<span class="app-icon-wrap"><img src="https://www.google.com/s2/favicons?domain=${encodeURIComponent(domain)}&sz=32" alt="" onerror="this.hidden=true;this.nextElementSibling.hidden=false"><b class="app-icon-fallback" hidden>${initial(value)}</b></span>`;
}

function tip(session, start, end) {
  return `<strong>${escapeHtml(session.processName || session.status)}</strong>
    ${timeOnly(start)} - ${timeOnly(end)}<br>
    Duration: ${duration(Math.floor((end - start) / 1000))}<br>
    Status: ${escapeHtml(session.status)}<br>
    ${escapeHtml(session.windowTitle || "")}`;
}

function showTip(event) {
  tooltip.innerHTML = event.currentTarget.dataset.tip;
  tooltip.hidden = false;
  tooltip.style.left = `${Math.min(event.clientX + 12, window.innerWidth - 300)}px`;
  tooltip.style.top = `${event.clientY + 12}px`;
}

function hideTip() {
  tooltip.hidden = true;
}

function clamp(date, start, end) {
  return new Date(Math.min(Math.max(date.getTime(), start.getTime()), end.getTime()));
}

function duration(seconds) {
  seconds = Math.max(0, Number(seconds || 0));
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  if (h) return `${h}h ${m}m`;
  return `${m}m`;
}

function timeOnly(value) {
  if (!value) return "-";
  return new Date(value).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

function escapeHtml(value) {
  return String(value).replace(/[&<>"']/g, c => ({
    "&": "&amp;",
    "<": "&lt;",
    ">": "&gt;",
    '"': "&quot;",
    "'": "&#39;"
  }[c]));
}

load();
setInterval(load, 30000);
