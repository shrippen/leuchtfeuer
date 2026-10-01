'use strict';
// Weboberfläche von invoked. Gestaltung: Kante (kante/shrippen.css), keine eigenen Farben.
// Alle sichtbaren Texte gibt es zweisprachig (lang="en" / lang="de"), der Umschalter kommt von Kante.

const $ = (s, e = document) => e.querySelector(s);
const $$ = (s, e = document) => [...e.querySelectorAll(s)];
const esc = s => String(s ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const T = (en, de) => `<span lang="en">${en}</span><span lang="de">${de}</span>`;
const tt = (en, de) => (document.documentElement.lang === 'de' ? de : en);

const ICON = {
  play: '<path d="M7 5v14l12-7z"/>', stop: '<rect x="6" y="6" width="12" height="12"/>',
  plus: '<path d="M12 5v14M5 12h14"/>', trash: '<path d="M5 7h14M10 7V4h4v3M7 7l1 13h8l1-13"/>',
  bt: '<path d="M7 7l10 10-5 4V3l5 4L7 17"/>', save: '<path d="M5 4h11l3 3v13H5zM8 4v5h7V4M8 20v-6h8v6"/>',
  snooze: '<path d="M5 6h6L5 12h6M13 9h5l-5 5h5"/>', test: '<circle cx="12" cy="12" r="3"/><path d="M12 3v3M12 18v3M3 12h3M18 12h3"/>',
  restart: '<path d="M4 12a8 8 0 1 0 3-6.2M4 4v5h5"/>',
};
const ico = n => `<svg viewBox="0 0 24 24" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${ICON[n] || ''}</svg>`;

const TABS = [
  ['overview', 'Overview', 'Übersicht'], ['radio', 'Radio', 'Radio'], ['alarms', 'Alarms & timers', 'Wecker & Timer'],
  ['buttons', 'Buttons', 'Tasten'], ['network', 'Network', 'Netzwerk'], ['ha', 'Home Assistant', 'Home Assistant'],
  ['settings', 'Settings', 'Einstellungen'],
];

let S = null;        // Status
let CFG = null;      // Einstellungen
let route = 'overview';
let draft = {};      // ungespeicherte Änderungen je Ansicht
let dragging = false;
let lastSig = '';
let dirty = false;   // ungespeicherte Eingaben in der aktuellen Ansicht: dann nichts von außen überschreiben
let playSig = '';
let cfgSig = '';

async function api(path, method = 'GET', body) {
  const r = await fetch(path, { method, headers: { 'Content-Type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body) });
  const j = await r.json().catch(() => ({}));
  if (r.status === 401 && path !== '/api/login') { location.reload(); throw new Error('login'); }
  if (!r.ok) throw new Error(j.error || r.statusText);
  return j;
}
function toast(msg, kind) {
  const d = document.createElement('div');
  d.className = 'toast';
  if (kind) d.dataset.kind = kind;
  d.textContent = msg;
  $('#toasts').appendChild(d);
  setTimeout(() => d.remove(), 4500);
}
async function act(fn, okMsg) {
  try { await fn(); if (okMsg) toast(okMsg, 'ok'); await refresh(); }
  catch (e) { toast(e.message, 'error'); }
}

// ---------------------------------------------------------------- Bausteine
const stateLabel = st => ({ playing: ['playing', 'spielt'], paused: ['paused', 'pausiert'], stopped: ['stopped', 'gestoppt'], idle: ['idle', 'bereit'], buffering: ['buffering', 'lädt'] }[st] ? T(...{ playing: ['playing', 'spielt'], paused: ['paused', 'pausiert'], stopped: ['stopped', 'gestoppt'], idle: ['idle', 'bereit'], buffering: ['buffering', 'lädt'] }[st]) : esc(st));
const sw = (id, on, en, de) => `<button class="switch" role="switch" id="${id}" aria-checked="${!!on}" type="button"><span class="switch-track"></span><span class="switch-label">${T(en, de)}</span></button>`;
const fld = (id, en, de, val, extra = '') => `<div class="field"><label for="${id}">${T(en, de)}</label><input class="input" id="${id}" value="${esc(val)}" ${extra}></div>`;
const kpi = (v, en, de) => `<div class="kpi"><b>${v}</b><span>${T(en, de)}</span></div>`;
function seg(id, opts, val) {
  return `<div class="seg" id="${id}" role="group">${opts.map(([v, en, de]) => `<button type="button" data-v="${v}" aria-pressed="${v === val}">${T(en, de)}</button>`).join('')}</div>`;
}
function bindSw(root = document) {
  $$('.switch', root).forEach(b => b.onclick = () => { b.setAttribute('aria-checked', b.getAttribute('aria-checked') !== 'true'); b.dispatchEvent(new Event('change')); });
}
function bindSeg(root = document) {
  $$('.seg', root).forEach(g => $$('button', g).forEach(b => b.onclick = () => {
    $$('button', g).forEach(x => x.setAttribute('aria-pressed', x === b));
    g.dispatchEvent(new Event('change'));
  }));
}
const segVal = id => { const b = $(`#${id} button[aria-pressed="true"]`); return b ? b.dataset.v : ''; };
const swVal = id => $(`#${id}`).getAttribute('aria-checked') === 'true';
const fmtDur = s => { s = Math.max(0, Math.round(s)); const h = Math.floor(s / 3600), m = Math.floor(s % 3600 / 60), x = s % 60; return (h ? h + ':' + String(m).padStart(2, '0') : m) + ':' + String(x).padStart(2, '0'); };
const fmtUp = s => { const d = Math.floor(s / 86400), h = Math.floor(s % 86400 / 3600), m = Math.floor(s % 3600 / 60); return d ? `${d}d ${h}h` : h ? `${h}h ${m}m` : `${m}m`; };
const band = f => (f > 4000 ? '5 GHz' : f ? '2.4 GHz' : '–');
const fmtTime = iso => iso ? new Date(iso).toLocaleString([], { weekday: 'short', hour: '2-digit', minute: '2-digit' }) : '–';

// ---------------------------------------------------------------- Ansichten
function viewOverview() {
  const s = S;
  const tier = s.wifi.good ? 'ok' : 'warn';
  return `
  <div class="grid kpis">
    ${kpi(s.volumeKnown ? s.volume + ' %' : '–', 'Volume', 'Lautstärke')}
    ${kpi(Math.round(s.sys.tempC) + ' °C', 'Temperature', 'Temperatur')}
    ${kpi(s.wifi.rssi ? s.wifi.rssi + ' dBm' : '–', 'Wi-Fi signal', 'WLAN-Signal')}
    ${kpi(s.wifi.lossPct + ' %', 'Packet loss', 'Paketverlust')}
    ${kpi(s.sys.dataFreeMB + ' MB', 'Free storage', 'Freier Speicher')}
    ${kpi(fmtUp(s.sys.uptimeSecs), 'Uptime', 'Laufzeit')}
  </div>
  <div class="grid">
    <div class="card" data-tier="yellow" id="c-play"></div>
    <div class="card" data-tier="cyan" id="c-time"></div>
    <div class="card" id="c-bt"></div>
  </div>
  <h3 class="mono muted small" style="margin:1.6rem 0 .6rem">${T('SERVICES', 'DIENSTE')}</h3>
  <div class="grid kpis" id="c-svc"></div>`;
}
function fillOverview() {
  const s = S;
  const pl = s.player, al = s.alarm;
  $('#c-play').innerHTML = `
    <h3>${T('Playback', 'Wiedergabe')}</h3>
    <div class="range field"><label for="vol">${T('Volume', 'Lautstärke')}</label>
      <div class="range-row"><input id="vol" type="range" min="0" max="100" value="${s.volume}"><output class="range-out" id="vol-out">${s.volume}</output></div></div>
    <div class="row">
      ${sw('mute', s.muted, 'Mute', 'Stumm')}
      <button class="btn btn-outline btn-sm" id="vm">−5</button><button class="btn btn-outline btn-sm" id="vp">+5</button>
    </div>
    ${al.active ? `<div class="callout callout-warn"><b>${al.state === 'ringing' ? T('Alarm ringing', 'Wecker klingelt') : T('Alarm snoozed', 'Wecker geschlummert')}:</b> ${esc(al.name)}</div>
      <div class="row"><button class="btn btn-accent btn-sm" id="a-stop">${ico('stop')}${T('Stop', 'Stopp')}</button>
      ${al.state === 'ringing' ? `<button class="btn btn-outline btn-sm" id="a-snooze">${ico('snooze')}${T('Snooze', 'Schlummern')}</button>` : ''}</div>` : ''}
    ${pl.kind ? `<p class="mono">${T('Now playing', 'Läuft')}: <b class="muted">${esc(pl.name)}</b> ${pl.title ? '— ' + esc(pl.title) : ''}<br><span class="small">${stateLabel(pl.state)}</span></p>
      <button class="btn btn-outline btn-sm" id="p-stop">${ico('stop')}${T('Stop', 'Stopp')}</button>` :
      `<p>${T('Sources play through the speaker: Spotify, UPnP/DLNA, Cast, AirPlay, Bluetooth, Sendspin. Web radio, alarms and timers start from here.', 'Quellen spielen über den Lautsprecher: Spotify, UPnP/DLNA, Cast, AirPlay, Bluetooth, Sendspin. Webradio, Wecker und Timer starten hier.')}</p>`}`;
  const timers = s.timers.map(t => `<div class="item"><div><div class="t">${esc(t.name)}</div><div class="s">${fmtDur(t.remaining)} / ${fmtDur(t.total)}</div>
      <div class="bar"><i style="width:${t.total ? (100 - 100 * t.remaining / t.total) : 0}%"></i></div></div>
      <button class="btn btn-outline btn-sm" data-tcancel="${t.id}">${ico('trash')}</button></div>`).join('');
  $('#c-time').innerHTML = `
    <h3>${T('Alarms & timers', 'Wecker & Timer')}</h3>
    <p class="mono">${T('Next alarm', 'Nächster Wecker')}: <b class="muted">${s.nextAlarm ? esc(s.nextAlarmName) + ' · ' + fmtTime(s.nextAlarm) : '–'}</b></p>
    <div class="list">${timers || `<p>${T('No timer running.', 'Kein Timer läuft.')}</p>`}</div>
    <div class="chips">${[1, 5, 10, 15, 30, 60].map(m => `<button class="chip is-filter" data-quick="${m}">${m} min</button>`).join('')}</div>`;
  const bt = s.bluetooth;
  $('#c-bt').innerHTML = `
    <h3>Bluetooth</h3>
    <p>${s.btMode === 'always' ? T('Always visible and ready to pair.', 'Immer sichtbar und koppelbereit.')
      : bt.open ? T('Pairing window <b>open</b>: pair now.', 'Kopplungs-Fenster <b>offen</b>: jetzt koppeln.')
        : T('Not visible. Press the Bluetooth button on the speaker or open the window here; paired phones reconnect by themselves.',
          'Nicht sichtbar. Bluetooth-Knopf am Lautsprecher drücken oder hier das Fenster öffnen; gekoppelte Handys verbinden sich selbst.')}</p>
    ${s.btMode === 'always' ? '' : `<button class="btn btn-outline btn-sm" id="bt-toggle">${ico('bt')}${bt.open ? T('Close pairing', 'Kopplung schließen') : T('Open pairing (2 min)', 'Kopplung öffnen (2 Min.)')}</button>`}`;
  $('#c-svc').innerHTML = s.sys.services.map(v => `<div class="status" data-state="${v.running ? 'ok' : 'off'}"><span class="name">${esc(v.name)}</span><span class="row">${v.running ? T('running', 'läuft') : T('not running', 'läuft nicht')}</span></div>`).join('') +
    `<div class="status" data-state="${s.wamp ? 'ok' : 'bad'}"><span class="name">audio-ui (WAMP)</span><span class="row">${s.wamp ? T('connected', 'verbunden') : T('not connected', 'nicht verbunden')}</span></div>`;
  bindOverview();
}
function bindOverview() {
  const vol = $('#vol');
  if (vol) {
    vol.oninput = () => { dragging = true; $('#vol-out').textContent = vol.value; };
    vol.onchange = () => { dragging = false; vol.blur(); act(() => api('/api/volume', 'POST', { volume: +vol.value })); };
  }
  bindSw($('#c-play'));
  const m = $('#mute'); if (m) m.onchange = () => act(() => api('/api/mute', 'POST', { muted: swVal('mute') }));
  const f = (id, fn) => { const e = $(id); if (e) e.onclick = fn; };
  f('#vm', () => act(() => api('/api/volume', 'POST', { delta: -5 })));
  f('#vp', () => act(() => api('/api/volume', 'POST', { delta: 5 })));
  f('#a-stop', () => act(() => api('/api/alarm/stop', 'POST', {})));
  f('#a-snooze', () => act(() => api('/api/alarm/snooze', 'POST', {})));
  f('#p-stop', () => act(() => api('/api/radio/stop', 'POST', {})));
  f('#bt-toggle', () => act(() => api('/api/bluetooth/pairing', 'POST', { action: 'toggle' })));
  $$('[data-quick]').forEach(b => b.onclick = () => act(() => api('/api/timers', 'POST', { seconds: b.dataset.quick * 60 }), tt('Timer started', 'Timer gestartet')));
  $$('[data-tcancel]').forEach(b => b.onclick = () => act(() => api('/api/timers/cancel', 'POST', { id: b.dataset.tcancel })));
}

function viewRadio() {
  const R = draft.radio || (draft.radio = JSON.parse(JSON.stringify(CFG.settings.radio)));
  const cur = S.player.kind === 'radio' ? S.player.name : '';
  return `<div class="card" data-tier="yellow">
    <h3>${T('Web radio', 'Webradio')}</h3>
    <p>${T('Stations are plain stream addresses (MP3/AAC over http or https). They play through the speaker like every other source.', 'Sender sind einfache Stream-Adressen (MP3/AAC über http oder https). Sie spielen wie jede andere Quelle über den Lautsprecher.')}</p>
    <div class="list">${R.map((p, i) => `<div class="item" data-i="${i}">
      <div class="stack"><div class="field"><label>${T('Name', 'Name')}</label><input class="input r-name" value="${esc(p.name)}"></div>
      <div class="field"><label>URL</label><input class="input r-url" value="${esc(p.url)}"></div>
      ${cur === p.name ? `<span class="pill" data-state="applied">${T('playing', 'läuft')}</span>` : ''}</div>
      <div class="stack"><button class="btn btn-accent btn-sm" data-play="${i}">${ico('play')}${T('Play', 'Abspielen')}</button>
      <button class="btn btn-outline btn-sm" data-del="${i}">${ico('trash')}</button></div></div>`).join('')}</div>
    <div class="row"><button class="btn btn-outline btn-sm" id="r-add">${ico('plus')}${T('Add station', 'Sender hinzufügen')}</button>
      <button class="btn btn-accent btn-sm" id="r-save">${ico('save')}${T('Save', 'Speichern')}</button>
      <button class="btn btn-outline btn-sm" id="r-stop">${ico('stop')}${T('Stop', 'Stopp')}</button></div></div>`;
}
function collectRadio() {
  return $$('#view .item[data-i]').map(it => ({ name: $('.r-name', it).value, url: $('.r-url', it).value }));
}
function bindRadio() {
  const keep = () => { draft.radio = collectRadio(); };
  $('#r-add').onclick = () => { keep(); draft.radio.push({ name: '', url: 'https://' }); render(); };
  $('#r-save').onclick = () => act(async () => { keep(); await api('/api/settings/radio', 'PUT', draft.radio); delete draft.radio; await loadCfg(); }, tt('Saved', 'Gespeichert'));
  $('#r-stop').onclick = () => act(() => api('/api/radio/stop', 'POST', {}));
  $$('[data-del]').forEach(b => b.onclick = () => { keep(); draft.radio.splice(+b.dataset.del, 1); render(); });
  $$('[data-play]').forEach(b => b.onclick = () => act(async () => {
    keep();
    const saved = JSON.stringify(CFG.settings.radio.map(p => [p.name, p.url])) === JSON.stringify(draft.radio.map(p => [p.name, p.url]));
    if (!saved) { await api('/api/settings/radio', 'PUT', draft.radio); await loadCfg(); }
    await api('/api/radio/play', 'POST', { index: +b.dataset.play });
  }));
}

const DAYS = [[1, 'Mo', 'Mo'], [2, 'Tu', 'Di'], [3, 'We', 'Mi'], [4, 'Th', 'Do'], [5, 'Fr', 'Fr'], [6, 'Sa', 'Sa'], [0, 'Su', 'So']];
const timersHTML = () => S.timers.map(t => `<div class="item"><div><div class="t">${esc(t.name)}</div><div class="s">${fmtDur(t.remaining)}</div></div><button class="btn btn-outline btn-sm" data-tcancel="${t.id}">${ico('trash')}</button></div>`).join('');
const mqttHTML = () => S.mqtt ? T('Connected to the MQTT broker.', 'Mit dem MQTT-Broker verbunden.') : T('Not connected.', 'Nicht verbunden.');
function viewAlarms() {
  const A = draft.alarms || (draft.alarms = JSON.parse(JSON.stringify(CFG.settings.alarms)));
  const radios = CFG.settings.radio;
  const alarmCard = (a, i) => `<div class="card" data-tier="${a.enabled ? 'yellow' : ''}" data-ai="${i}">
    <div class="row between"><input class="input time-in a-time" type="time" value="${esc(a.time)}">${sw('a-en-' + i, a.enabled, 'Enabled', 'Aktiv')}</div>
    <div class="alarm">
      ${fld('a-name-' + i, 'Name', 'Name', a.name)}
      <div class="field"><label for="a-src-${i}">${T('Sound', 'Ton')}</label><select class="select" id="a-src-${i}"><option value="tone">${tt('Beeps', 'Signalton')}</option>
        ${radios.map((r, k) => `<option value="radio:${k}" ${a.source === 'radio:' + k ? 'selected' : ''}>${esc(r.name)}</option>`).join('')}</select></div>
      <div class="field wide"><span class="field-label">${T('Days (none = every day)', 'Tage (keine = täglich)')}</span>
        <div class="chips">${DAYS.map(([d, en, de]) => `<button type="button" class="chip is-filter a-day" data-d="${d}" aria-pressed="${(a.days || []).includes(d)}">${T(en, de)}</button>`).join('')}</div></div>
      ${fld('a-vol-' + i, 'Volume % (0 = unchanged)', 'Lautstärke % (0 = unverändert)', a.volume, 'type="number" min="0" max="100"')}
      ${fld('a-ramp-' + i, 'Fade-in (seconds)', 'Anstieg (Sekunden)', a.rampSecs, 'type="number" min="0" max="1800"')}
      ${fld('a-snz-' + i, 'Snooze (minutes)', 'Schlummern (Minuten)', a.snoozeMin, 'type="number" min="0" max="60"')}
      ${fld('a-max-' + i, 'Stop after (minutes)', 'Stoppen nach (Minuten)', a.maxMins, 'type="number" min="0" max="240"')}
    </div>
    <div class="row"><button class="btn btn-outline btn-sm" data-adel="${i}">${ico('trash')}${T('Delete', 'Löschen')}</button></div></div>`;
  return `
  <div class="grid">
    <div class="card" data-tier="cyan"><h3>${T('Timers', 'Timer')}</h3>
      <div class="chips">${[1, 3, 5, 10, 15, 30, 45, 60].map(m => `<button class="chip is-filter" data-quick="${m}">${m} min</button>`).join('')}</div>
      <div class="row"><div class="field" style="flex:1;min-width:8rem"><label for="t-name">${T('Name (optional)', 'Name (optional)')}</label><input class="input" id="t-name"></div>
        <div class="field" style="width:7rem"><label for="t-min">${T('Minutes', 'Minuten')}</label><input class="input" id="t-min" type="number" min="1" max="1440" value="20"></div>
        <button class="btn btn-accent btn-sm" id="t-start" style="align-self:end">${ico('play')}${T('Start', 'Start')}</button></div>
      <div class="list" id="t-list">${timersHTML()}</div></div>
    <div class="card"><h3>${T('Time zone', 'Zeitzone')}</h3>
      <p>${T('Alarms use this time zone (the speaker itself runs on Pacific time).', 'Wecker nutzen diese Zeitzone (der Lautsprecher selbst läuft auf Pacific Time).')}</p>
      <div class="row"><input class="input" id="tz" list="tzs" value="${esc(CFG.settings.timezone)}" style="max-width:16rem"><datalist id="tzs">${['Europe/Berlin', 'Europe/Vienna', 'Europe/Zurich', 'Europe/London', 'Europe/Paris', 'America/New_York', 'America/Chicago', 'America/Los_Angeles', 'UTC'].map(z => `<option value="${z}">`).join('')}</datalist>
        <button class="btn btn-outline btn-sm" id="tz-save">${ico('save')}${T('Save', 'Speichern')}</button></div>
      <p class="mono small">${T('Speaker time', 'Zeit am Lautsprecher')}: <span id="spk-time">${fmtTime(S.now)}</span></p></div>
  </div>
  <h3 class="mono muted small" style="margin:1.6rem 0 .6rem">${T('ALARMS', 'WECKER')}</h3>
  <div class="grid">${A.map(alarmCard).join('')}</div>
  <div class="row" style="margin-top:1.2rem"><button class="btn btn-outline btn-sm" id="a-add">${ico('plus')}${T('Add alarm', 'Wecker hinzufügen')}</button>
    <button class="btn btn-accent btn-sm" id="a-save">${ico('save')}${T('Save alarms', 'Wecker speichern')}</button></div>`;
}
function collectAlarms() {
  return $$('#view [data-ai]').map((c, i) => ({
    id: (draft.alarms[i] || {}).id || '', time: $('.a-time', c).value || '07:00',
    name: $(`#a-name-${i}`).value, enabled: swVal('a-en-' + i), source: $(`#a-src-${i}`).value,
    days: $$('.a-day[aria-pressed="true"]', c).map(b => +b.dataset.d),
    volume: +$(`#a-vol-${i}`).value || 0, rampSecs: +$(`#a-ramp-${i}`).value || 0, snoozeMin: +$(`#a-snz-${i}`).value || 0, maxMins: +$(`#a-max-${i}`).value || 0,
  }));
}
function bindAlarms() {
  bindSw($('#view'));
  $$('.a-day').forEach(b => b.onclick = () => b.setAttribute('aria-pressed', b.getAttribute('aria-pressed') !== 'true'));
  const keep = () => { draft.alarms = collectAlarms(); };
  $('#a-add').onclick = () => { keep(); draft.alarms.push({ id: '', time: '07:00', name: tt('Alarm', 'Wecker'), days: [1, 2, 3, 4, 5], enabled: true, source: 'tone', volume: 40, rampSecs: 60, snoozeMin: 9, maxMins: 30 }); render(); };
  $('#a-save').onclick = () => act(async () => { keep(); await api('/api/settings/alarms', 'PUT', draft.alarms); delete draft.alarms; await loadCfg(); }, tt('Saved', 'Gespeichert'));
  $$('[data-adel]').forEach(b => b.onclick = () => { keep(); draft.alarms.splice(+b.dataset.adel, 1); render(); });
  $$('[data-quick]').forEach(b => b.onclick = () => act(() => api('/api/timers', 'POST', { seconds: b.dataset.quick * 60 }), tt('Timer started', 'Timer gestartet')));
  $$('[data-tcancel]').forEach(b => b.onclick = () => act(() => api('/api/timers/cancel', 'POST', { id: b.dataset.tcancel })));
  $('#t-start').onclick = () => act(() => api('/api/timers', 'POST', { name: $('#t-name').value, seconds: Math.round($('#t-min').value * 60) }), tt('Timer started', 'Timer gestartet'));
  $('#tz-save').onclick = () => act(async () => { await api('/api/settings/timezone', 'PUT', { timezone: $('#tz').value }); await loadCfg(); }, tt('Saved', 'Gespeichert'));
}

function actionLabel(a) {
  const m = { none: ['Nothing', 'Nichts'], smart: ['Smart: snooze alarm / end timer / mute', 'Smart: Wecker schlummern / Timer beenden / Stumm'], mute_toggle: ['Toggle mute', 'Stumm umschalten'], volume_up: ['Volume +5', 'Lauter +5'], volume_down: ['Volume −5', 'Leiser −5'], radio_toggle: ['Radio on/off', 'Radio an/aus'], radio_next: ['Next station', 'Nächster Sender'], alarm_stop: ['Stop alarm', 'Wecker stoppen'], alarm_snooze: ['Snooze alarm', 'Wecker schlummern'], timer_dismiss: ['End ringing timer', 'Klingelnden Timer beenden'], timers_cancel: ['Cancel all timers', 'Alle Timer abbrechen'], stop_all: ['Stop everything', 'Alles stoppen'], bt_pairing: ['Bluetooth pairing window', 'Bluetooth-Kopplungsfenster'] }[a];
  return m ? tt(m[0], m[1]) : a;
}
function evListHTML() {
  return S.buttons.slice().reverse().map(e => `<div class="item"><div><div class="t mono">${esc(e.name)} <span class="muted">/ ${esc(e.value)}</span></div><div class="s">${new Date(e.time).toLocaleTimeString()} ${e.action ? '→ ' + esc(actionLabel(e.action)) : ''}</div></div></div>`).join('') || `<p>${T('No events yet. Press a button on the speaker.', 'Noch keine Ereignisse. Drücke eine Taste am Lautsprecher.')}</p>`;
}
function viewButtons() {
  const rows = draft.buttons || (draft.buttons = Object.entries(CFG.settings.buttons).flatMap(([b, m]) => Object.entries(m).map(([p, a]) => ({ b, p, a }))));
  const seen = [...new Set(S.buttons.map(e => e.name))];
  return `<div class="grid"><div class="card" data-tier="yellow"><h3>${T('Button mapping', 'Tastenbelegung')}</h3>
    <p>${T('The speaker reports button presses to this program. Press a button and watch the log on the right to learn its name and value, then assign an action. The volume knob and the Bluetooth button keep their own function (their presses are only logged and sent to Home Assistant).', 'Der Lautsprecher meldet Tastendrücke an dieses Programm. Drücke eine Taste und sieh rechts im Protokoll ihren Namen und Wert, dann ordne eine Aktion zu. Drehrad und Bluetooth-Knopf behalten ihre eigene Funktion (ihre Ereignisse werden nur protokolliert und an Home Assistant gesendet).')}</p>
    <div class="list">${rows.map((r, i) => `<div class="item" data-bi="${i}"><div class="stack">
      <div class="row"><div class="field" style="width:9rem"><label>${T('Button', 'Taste')}</label><input class="input b-name" list="b-seen" value="${esc(r.b)}"></div>
      <div class="field" style="width:8rem"><label>${T('Press / value', 'Druck / Wert')}</label><input class="input b-press" list="b-press" value="${esc(r.p)}"></div></div>
      <div class="field"><label>${T('Action', 'Aktion')}</label><select class="select b-act">${CFG.actions.map(a => `<option value="${a}" ${a === r.a ? 'selected' : ''}>${esc(actionLabel(a))}</option>`).join('')}</select></div></div>
      <button class="btn btn-outline btn-sm" data-bdel="${i}">${ico('trash')}</button></div>`).join('')}</div>
    <datalist id="b-seen">${seen.map(n => `<option value="${esc(n)}">`).join('')}<option value="mic"></datalist><datalist id="b-press"><option value="short"><option value="long"><option value="0"><option value="1"></datalist>
    <div class="row"><button class="btn btn-outline btn-sm" id="b-add">${ico('plus')}${T('Add mapping', 'Belegung hinzufügen')}</button>
      <button class="btn btn-accent btn-sm" id="b-save">${ico('save')}${T('Save', 'Speichern')}</button></div></div>
    <div class="card"><h3>${T('Recent button events', 'Letzte Tasten-Ereignisse')}</h3>
      <div class="list" id="ev-list">${evListHTML()}</div></div></div>`;
}
function collectButtons() {
  return $$('#view [data-bi]').map(it => ({ b: $('.b-name', it).value.trim(), p: $('.b-press', it).value.trim() || 'short', a: $('.b-act', it).value }));
}
function bindButtons() {
  const keep = () => { draft.buttons = collectButtons(); };
  $('#b-add').onclick = () => { keep(); draft.buttons.push({ b: '', p: 'short', a: 'none' }); render(); };
  $('#b-save').onclick = () => act(async () => {
    keep(); const o = {}; draft.buttons.filter(r => r.b).forEach(r => { (o[r.b] = o[r.b] || {})[r.p] = r.a; });
    await api('/api/settings/buttons', 'PUT', o); delete draft.buttons; await loadCfg();
  }, tt('Saved', 'Gespeichert'));
  $$('[data-bdel]').forEach(b => b.onclick = () => { keep(); draft.buttons.splice(+b.dataset.bdel, 1); render(); });
}

function wifiLiveHTML() {
  const w = S.wifi;
  return `<div class="grid kpis" style="margin:0 0 .8rem"><div class="kpi"><b>${esc(w.ssid || '–')}</b><span>SSID</span></div>
      ${kpi(band(w.freq), 'Band', 'Band')}${kpi((w.rssi || '–') + ' dBm', 'Signal', 'Signal')}${kpi(w.lossPct + ' %', 'Loss', 'Verlust')}${kpi(Math.round(w.rttMs) + ' ms', 'Round trip', 'Laufzeit')}</div>
    <p class="mono small">BSSID ${esc(w.bssid || '–')} · ${T('router', 'Router')} ${esc(w.gateway || '–')} · ${w.good ? T('connection good', 'Verbindung gut') : T('connection poor', 'Verbindung schlecht')}</p>`;
}
function viewNetwork() {
  const w = S.wifi, c = CFG.settings.wifi, d = CFG.device;
  return `<div class="grid">
  <div class="card" data-tier="cyan"><h3>${T('Wi-Fi guard', 'WLAN-Wächter')}</h3>
    <div id="w-live">${wifiLiveHTML()}</div>
    <p>${T('Measures the real quality to the router. If it stays poor, the speaker switches to another access point of the same network and avoids the poor one for a while.', 'Misst die tatsächliche Qualität zum Router. Bleibt sie schlecht, wechselt der Lautsprecher zu einem anderen Access Point desselben Netzes und meidet den schlechten eine Weile.')}</p>
    ${sw('w-en', c.enabled, 'Guard enabled', 'Wächter aktiv')}${sw('w-5', c.prefer5GHz, 'Prefer 5 GHz', '5 GHz bevorzugen')}${sw('w-dry', c.dryRun, 'Dry run (log only, do not switch)', 'Probelauf (nur protokollieren, nicht wechseln)')}
    <div class="alarm">${fld('w-int', 'Check every (seconds)', 'Prüfen alle (Sekunden)', c.intervalSec, 'type="number" min="10" max="600"')}${fld('w-loss', 'Poor from loss %', 'Schlecht ab Verlust %', c.lossPct, 'type="number" min="1" max="100"')}
      ${fld('w-rtt', 'Poor from round trip (ms)', 'Schlecht ab Laufzeit (ms)', c.rttMs, 'type="number" min="20" max="5000"')}${fld('w-pen', 'Avoid a poor AP for (minutes)', 'Schlechten AP meiden (Minuten)', c.penaltyMins, 'type="number" min="1" max="1440"')}</div>
    <div class="row"><button class="btn btn-accent btn-sm" id="w-save">${ico('save')}${T('Save', 'Speichern')}</button>
      <button class="btn btn-outline btn-sm" id="w-scan">${ico('test')}${T('Find access points', 'Access Points suchen')}</button></div>
    <div class="list" id="w-aps"></div></div>
  <div class="card"><h3>${T('Guard log', 'Wächter-Protokoll')}</h3><div class="log" id="w-log">${esc((w.log || []).join('\n')) || tt('Nothing to report.', 'Nichts zu melden.')}</div></div>
  <div class="card"><h3>${T('Names & discovery', 'Namen & Erkennung')}</h3>
    ${fld('d-host', 'DHCP host name (router lists it as name.lan)', 'DHCP-Hostname (der Router führt ihn als name.lan)', d.dhcpHostname)}
    ${fld('d-ss', 'Sendspin server (host:8927, empty = auto)', 'Sendspin-Server (host:8927, leer = automatisch)', d.sendspinServer)}
    <p>${T('Sendspin streams from Music Assistant. Many routers do not forward multicast between Wi-Fi and LAN, so the address may be needed. Changes apply after restarting the service (Settings).', 'Sendspin streamt von Music Assistant. Viele Router leiten Multicast nicht zwischen WLAN und LAN weiter, dann braucht es die Adresse. Änderungen gelten nach dem Neustart des Dienstes (Einstellungen).')}</p>
    <div class="row"><button class="btn btn-accent btn-sm" id="d-save">${ico('save')}${T('Save', 'Speichern')}</button></div></div></div>`;
}
function bindNetwork() {
  bindSw($('#view'));
  $('#w-save').onclick = () => act(async () => {
    await api('/api/settings/wifi', 'PUT', { enabled: swVal('w-en'), prefer5GHz: swVal('w-5'), dryRun: swVal('w-dry'), intervalSec: +$('#w-int').value, lossPct: +$('#w-loss').value, rttMs: +$('#w-rtt').value, penaltyMins: +$('#w-pen').value });
    await loadCfg();
  }, tt('Saved', 'Gespeichert'));
  $('#w-scan').onclick = async () => {
    const box = $('#w-aps'); box.innerHTML = `<p>${T('Scanning ...', 'Suche läuft ...')}</p>`;
    try {
      const aps = await api('/api/wifi/scan');
      box.innerHTML = aps.map(a => `<div class="item"><div><div class="t mono">${esc(a.bssid)} ${a.current ? `<span class="pill" data-state="applied">${T('connected', 'verbunden')}</span>` : ''}</div>
        <div class="s">${band(a.freq)} · ${a.rssi} dBm${a.avoidedMins ? ' · ' + tt('avoided for', 'gemieden noch') + ' ' + a.avoidedMins + ' min' : ''}</div></div>
        ${a.current ? '' : `<button class="btn btn-outline btn-sm" data-roam="${esc(a.bssid)}">${T('Switch', 'Wechseln')}</button>`}</div>`).join('') || `<p>${T('No access point of this network found.', 'Kein Access Point dieses Netzes gefunden.')}</p>`;
      $$('[data-roam]', box).forEach(b => b.onclick = () => act(() => api('/api/wifi/roam', 'POST', { bssid: b.dataset.roam }), tt('Switching ...', 'Wechsle ...')));
    } catch (e) { box.innerHTML = ''; toast(e.message, 'error'); }
  };
  $('#d-save').onclick = () => act(async () => { await api('/api/settings/device', 'PUT', { ...CFG.device, dhcpHostname: $('#d-host').value, sendspinServer: $('#d-ss').value }); await loadCfg(); }, tt('Saved', 'Gespeichert'));
}

function viewHA() {
  const m = CFG.settings.mqtt;
  return `<div class="card" data-tier="yellow" style="max-width:40rem"><h3>Home Assistant (MQTT)</h3>
    <p><span id="mq-st">${mqttHTML()}</span>
    ${T('Enter the broker that Home Assistant uses (Mosquitto add-on). The speaker then shows up automatically as a device with volume, mute, web radio, Bluetooth pairing, timers, alarm buttons, sensors (temperature, Wi-Fi, uptime) and button events.', 'Trage den Broker ein, den Home Assistant nutzt (Mosquitto-Add-on). Der Lautsprecher erscheint dann automatisch als Gerät mit Lautstärke, Stumm, Webradio, Bluetooth-Kopplung, Timern, Wecker-Tasten, Sensoren (Temperatur, WLAN, Laufzeit) und Tasten-Ereignissen.')}</p>
    ${sw('m-en', m.enabled, 'Enabled', 'Aktiv')}
    <div class="alarm">${fld('m-host', 'Broker host', 'Broker-Adresse', m.host)}${fld('m-port', 'Port', 'Port', m.port, 'type="number"')}
      ${fld('m-user', 'User', 'Benutzer', m.user)}<div class="field"><label for="m-pass">${T('Password (empty = keep)', 'Passwort (leer = behalten)')}</label><input class="input" id="m-pass" type="password" autocomplete="new-password"></div>
      ${fld('m-disc', 'Discovery prefix', 'Erkennungs-Präfix', m.discovery)}</div>
    <div class="row"><button class="btn btn-accent btn-sm" id="m-save">${ico('save')}${T('Save', 'Speichern')}</button></div></div>`;
}
function bindHA() {
  bindSw($('#view'));
  $('#m-save').onclick = () => act(async () => {
    await api('/api/settings/mqtt', 'PUT', { enabled: swVal('m-en'), host: $('#m-host').value.trim(), port: +$('#m-port').value || 1883, user: $('#m-user').value, pass: $('#m-pass').value, discovery: $('#m-disc').value.trim() || 'homeassistant' });
    await loadCfg();
  }, tt('Saved', 'Gespeichert'));
}

const vizHTML = () => !S.viz.tap ? T('No music signal found yet (play something; needs the current installation).', 'Noch kein Musiksignal gefunden (etwas abspielen; braucht die aktuelle Installation).')
  : S.viz.active ? `<span class="pill" data-state="applied">${T('showing music', 'zeigt Musik')}</span>` : '';
function viewSettings() {
  const d = CFG.device, v = CFG.settings.viz;
  return `<div class="grid">
  <div class="card" data-tier="yellow"><h3>${T('Speaker', 'Lautsprecher')}</h3>
    ${fld('s-name', 'Name (Spotify, UPnP, Cast, AirPlay, Bluetooth)', 'Name (Spotify, UPnP, Cast, AirPlay, Bluetooth)', d.name)}
    <div class="field"><span class="field-label">${T('Bluetooth pairing', 'Bluetooth-Kopplung')}</span>${seg('s-bt', [['button', 'Only after button press', 'Nur nach Knopfdruck'], ['always', 'Always open', 'Immer offen']], d.bluetoothPairing)}</div>
    ${sw('s-air', d.airplay !== 'off', 'AirPlay receiver', 'AirPlay-Empfänger')}
    <p>${T('A name change applies after restarting the services below.', 'Eine Namensänderung gilt nach dem Neustart der Dienste weiter unten.')}</p>
    <div class="row"><button class="btn btn-accent btn-sm" id="s-save">${ico('save')}${T('Save', 'Speichern')}</button></div></div>
  <div class="card" data-tier="cyan"><h3>${T('Light ring', 'Leuchtring')}</h3>
    <p>${T('The ring can follow the music of all receivers. Volume knob, mute, alarm, timer and buttons keep showing their own animations.', 'Der Ring kann der Musik aller Empfänger folgen. Drehrad, Stumm, Wecker, Timer und Tasten zeigen weiter ihre eigenen Animationen.')} <span id="v-st">${vizHTML()}</span></p>
    <div class="field"><span class="field-label">${T('Display', 'Anzeige')}</span>${seg('v-mode', [['off', 'Off', 'Aus'], ['spectrum', 'Spectrum', 'Spektrum'], ['level', 'Level', 'Pegel'], ['pulse', 'Pulse', 'Puls']], v.mode)}</div>
    <div class="field"><span class="field-label">${T('Colour', 'Farbe')}</span>${seg('v-col', [['rainbow', 'Rainbow', 'Regenbogen'], ['white', 'White', 'Weiß'], ['warm', 'Warm', 'Warm'], ['blue', 'Blue', 'Blau'], ['green', 'Green', 'Grün'], ['red', 'Red', 'Rot'], ['purple', 'Purple', 'Lila']], v.color)}</div>
    <div class="alarm"><div class="range field"><label for="v-bri">${T('Brightness %', 'Helligkeit %')}</label>
        <div class="range-row"><input id="v-bri" type="range" min="5" max="100" step="5" value="${v.brightness}"><output class="range-out" id="v-bri-o">${v.brightness}</output></div></div>
      ${fld('v-rot', 'Start LED (turns the display)', 'Start-LED (dreht die Anzeige)', v.rotate, 'type="number" min="0" max="11"')}</div>
    <div class="row"><button class="btn btn-accent btn-sm" id="v-save">${ico('save')}${T('Save', 'Speichern')}</button><button class="btn btn-outline btn-sm" id="l-test">${ico('test')}${T('Test ring', 'Ring testen')}</button></div></div>
  <div class="card"><h3>${T('Web password', 'Web-Passwort')}</h3>
    <p>${T('At least 6 characters. Stored only as a salted hash.', 'Mindestens 6 Zeichen. Wird nur als gesalzener Hash gespeichert.')}</p>
    <div class="field"><label for="pw">${T('New password', 'Neues Passwort')}</label><input class="input" id="pw" type="password" autocomplete="new-password"></div>
    <div class="row"><button class="btn btn-accent btn-sm" id="pw-save">${ico('save')}${T('Change', 'Ändern')}</button><button class="btn btn-outline btn-sm" id="logout">${T('Sign out', 'Abmelden')}</button></div></div>
  <div class="card"><h3>${T('Services', 'Dienste')}</h3>
    <p>${T('Restarting stops a service; the supervisor starts it again within 30 seconds.', 'Neustart beendet einen Dienst; der Überwacher startet ihn binnen 30 Sekunden neu.')}</p>
    <div class="list">${CFG.services.map(n => `<div class="item"><div class="t mono">${esc(n)}</div><div class="row"><button class="btn btn-outline btn-sm" data-log="${n}">${T('Log', 'Protokoll')}</button><button class="btn btn-outline btn-sm" data-rs="${n}">${ico('restart')}</button></div></div>`).join('')}</div></div></div>
  <div class="card" style="margin-top:1.2rem" id="logcard"><h3 id="log-t">${T('Log', 'Protokoll')}</h3><div class="log" id="logbox">${tt('Choose a service above.', 'Wähle oben einen Dienst.')}</div></div>`;
}
function bindSettings() {
  bindSw($('#view')); bindSeg($('#view'));
  $('#s-save').onclick = () => act(async () => { await api('/api/settings/device', 'PUT', { ...CFG.device, name: $('#s-name').value, bluetoothPairing: segVal('s-bt'), airplay: swVal('s-air') ? 'on' : 'off' }); await loadCfg(); }, tt('Saved', 'Gespeichert'));
  $('#l-test').onclick = () => act(() => api('/api/led/test', 'POST', {}));
  $('#v-bri').oninput = () => { $('#v-bri-o').textContent = $('#v-bri').value; };
  $('#v-save').onclick = () => act(async () => { await api('/api/settings/viz', 'PUT', { mode: segVal('v-mode'), color: segVal('v-col'), brightness: +$('#v-bri').value, rotate: +$('#v-rot').value || 0 }); await loadCfg(); }, tt('Saved', 'Gespeichert'));
  $('#pw-save').onclick = () => act(async () => { await api('/api/settings/device', 'PUT', { ...CFG.device, webPassword: $('#pw').value }); $('#pw').value = ''; }, tt('Password changed - sign in again', 'Passwort geändert - bitte neu anmelden'));
  $('#logout').onclick = async () => { await fetch('/api/logout', { method: 'POST' }); location.reload(); };
  $$('[data-rs]').forEach(b => b.onclick = () => act(() => api('/api/services/restart', 'POST', { name: b.dataset.rs }), tt('Stopped, will restart within 30 s', 'Gestoppt, startet binnen 30 s neu')));
  $$('[data-log]').forEach(b => b.onclick = async () => {
    try { const r = await api('/api/logs?name=' + b.dataset.log); $('#logbox').textContent = r.log || '–'; $('#logbox').scrollTop = 1e9; } catch (e) { toast(e.message, 'error'); }
  });
}

const VIEWS = {
  overview: [viewOverview, fillOverview], radio: [viewRadio, bindRadio], alarms: [viewAlarms, bindAlarms], buttons: [viewButtons, bindButtons],
  network: [viewNetwork, bindNetwork], ha: [viewHA, bindHA], settings: [viewSettings, bindSettings],
};

// ---------------------------------------------------------------- Rahmen
function renderTabs() {
  // Die Reiter stehen fest in index.html (Kante setzt dort einen gleitenden Indikator ein): nur Auswahl umschalten.
  const t = $(`#tabs [data-r="${route}"]`);
  if (t && t.getAttribute('aria-selected') !== 'true') t.click(); // Kantes Handler verschiebt den Indikator
  $$('#tabs button').forEach(b => b.setAttribute('aria-selected', b.dataset.r === route));
}
function render() {
  if (!S || !CFG) return;
  renderTabs();
  const [html, bind] = VIEWS[route];
  $('#view').innerHTML = html();
  bind();
}
// Übersicht und Anzeigen, die sich laufend ändern, ohne Eingabefelder zu stören
function tick() {
  if (!S) return;
  $('#conn').innerHTML = S.wamp ? T('connected', 'verbunden') : T('audio-ui not reachable', 'audio-ui nicht erreichbar');
  $('#brand').textContent = S.name;
  document.title = S.name;
  if (route === 'overview') {
    const focus = document.activeElement;
    if (dragging) return;
    if (focus && focus.id === 'vol') { // Regler behält nach dem Loslassen den Fokus: nur Wert und Anzeige nachführen, nichts neu zeichnen
      if (+focus.value !== S.volume) { focus.value = S.volume; $('#vol-out').textContent = S.volume; }
      return;
    }
    const sig = JSON.stringify({ ...S, now: 0, buttons: 0, sys: { ...S.sys, uptimeSecs: Math.floor(S.sys.uptimeSecs / 60) } }) + (S.timers.map(t => t.remaining).join());
    if (sig === lastSig) return;
    lastSig = sig;
    $('#view').innerHTML = viewOverview(); fillOverview();
  } else if (route === 'alarms') {
    const l = $('#t-list'), sig = JSON.stringify(S.timers);
    if (l && l.dataset.sig !== sig) {
      l.dataset.sig = sig; l.innerHTML = timersHTML();
      $$('[data-tcancel]', l).forEach(b => b.onclick = () => act(() => api('/api/timers/cancel', 'POST', { id: b.dataset.tcancel })));
    }
    const t = $('#spk-time'); if (t) t.textContent = fmtTime(S.now);
  } else if (route === 'settings') {
    const v = $('#v-st'); if (v) v.innerHTML = vizHTML();
  } else if (route === 'ha') {
    const m = $('#mq-st'); if (m) m.innerHTML = mqttHTML();
  } else if (route === 'radio') {
    const sig = JSON.stringify(S.player);
    if (sig !== playSig) { playSig = sig; if (!dirty && !inFormFocus()) { draft = {}; render(); } }
  } else if (route === 'buttons') {
    const e = $('#ev-list'); if (e) e.innerHTML = evListHTML();
  } else if (route === 'network') {
    const e = $('#w-live'); if (e) e.innerHTML = wifiLiveHTML();
    const l = $('#w-log'); if (l) l.textContent = (S.wifi.log || []).join('\n') || tt('Nothing to report.', 'Nichts zu melden.');
  }
}
const cfgSigOf = c => JSON.stringify({ ...c, settings: { ...c.settings, timers: 0 } });
const inFormFocus = () => { const e = document.activeElement; return !!e && $('#view').contains(e) && ['INPUT', 'SELECT', 'TEXTAREA'].includes(e.tagName); };
async function loadCfg() { CFG = await api('/api/settings'); cfgSig = cfgSigOf(CFG); dirty = false; }
// Einstellungen, die sich von außen ändern (zweiter Browser, Home Assistant, Datei), live nachführen,
// solange der Nutzer in der Ansicht nichts eingegeben hat
async function pollCfg() {
  const c = await api('/api/settings'), sig = cfgSigOf(c);
  if (sig === cfgSig) return;
  if (route !== 'overview' && (dirty || inFormFocus())) return; // später erneut versuchen
  cfgSig = sig; CFG = c;
  if (route !== 'overview') { draft = {}; render(); }
}
$('#view').addEventListener('input', e => { if (e.target.id !== 'vol') dirty = true; });
$('#view').addEventListener('change', e => { if (e.target.id !== 'vol') dirty = true; });
$('#view').addEventListener('click', e => { if (e.target.closest('.switch, .seg button, .a-day, [data-del], [data-adel], [data-bdel], #r-add, #a-add, #b-add') && route !== 'overview') dirty = true; });
async function refresh() { S = await api('/api/status'); tick(); pollCfg().catch(() => {}); }
async function start() {
  try { await loadCfg(); await refresh(); } catch (e) { toast(e.message, 'error'); return; }
  onHash(); setInterval(() => refresh().catch(() => { $('#conn').textContent = '…'; }), 2000);
}
function onHash() {
  const r = (location.hash.replace(/^#\/?/, '') || 'overview');
  route = VIEWS[r] ? r : 'overview'; draft = {}; lastSig = ''; dirty = false; playSig = JSON.stringify(S.player); render();
}
$$('#tabs button').forEach(b => b.addEventListener('click', () => { const h = '#/' + (b.dataset.r === 'overview' ? '' : b.dataset.r); if (location.hash !== h) location.hash = h; }));
window.addEventListener('hashchange', onHash);
new MutationObserver(() => render()).observe(document.documentElement, { attributes: true, attributeFilter: ['lang'] });
start();
