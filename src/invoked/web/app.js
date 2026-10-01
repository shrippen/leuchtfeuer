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
  restart: '<path d="M4 12a8 8 0 1 0 3-6.2M4 4v5h5"/>', pause: '<path d="M8 5v14M16 5v14"/>', next: '<path d="M6 5l9 7-9 7zM18 5v14"/>',
  bell: '<path d="M6 16V11a6 6 0 0 1 12 0v5l2 2H4zM10 20a2 2 0 0 0 4 0"/>', moon: '<path d="M20 14A8 8 0 1 1 10 4a7 7 0 0 0 10 10z"/>',
  down: '<path d="M12 4v12M7 11l5 5 5-5M5 20h14"/>', up: '<path d="M12 20V8M7 13l5-5 5 5M5 4h14"/>', skip: '<path d="M5 5l14 14M12 3a9 9 0 1 0 0 18"/>',
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
  <div id="c-warn"></div>
  <div class="grid">
    <div class="card" data-tier="yellow" id="c-play"></div>
    <div class="card" data-tier="yellow" id="c-now"></div>
    <div class="card" data-tier="cyan" id="c-time"></div>
    <div class="card" id="c-bt"></div>
    <div class="card" id="c-ann"></div>
  </div>
  <h3 class="mono muted small" style="margin:1.6rem 0 .6rem">${T('SERVICES', 'DIENSTE')}</h3>
  <div class="grid kpis" id="c-svc"></div>`;
}
const SRC = { spotify: 'Spotify', upnp: 'UPnP/DLNA', cast: 'Cast', airplay: 'AirPlay', bluetooth: 'Bluetooth', sendspin: 'Sendspin', tidal: 'Tidal', snapcast: 'Snapcast', radio: ['Web radio', 'Webradio'], alarm: ['Alarm', 'Wecker'], announce: ['Announcement', 'Durchsage'] };
const srcName = n => { const v = SRC[n]; return Array.isArray(v) ? T(v[0], v[1]) : esc(v || n); };
const srcNameT = n => { const v = SRC[n]; return Array.isArray(v) ? tt(v[0], v[1]) : (v || n); };
function nowHTML() {
  const list = S.sources || [];
  if (!list.length) return `<h3>${T('Now playing', 'Läuft gerade')}</h3><p>${T('Nothing is playing.', 'Es spielt nichts.')}</p>`;
  return `<h3>${T('Now playing', 'Läuft gerade')}</h3><div class="list">${list.map(x => `<div class="item"><div>
      <div class="t">${x.title ? esc(x.title) : srcName(x.name)}</div>
      <div class="s">${srcName(x.name)}${x.artist ? ' · ' + esc(x.artist) : ''}${x.album ? ' · ' + esc(x.album) : ''} · ${stateLabel(x.state)}</div></div>
      <div class="row">${x.muted ? `<span class="pill" data-state="pending">${T('paused (other source)', 'pausiert (andere Quelle)')}</span>` : ''}
      ${x.name === 'bluetooth' ? `<button class="btn btn-outline btn-sm" data-btc="${x.state === 'playing' ? 'pause' : 'play'}" aria-label="${tt('Play/pause', 'Abspielen/Pause')}">${ico(x.state === 'playing' ? 'pause' : 'play')}</button><button class="btn btn-outline btn-sm" data-btc="next" aria-label="${tt('Next', 'Weiter')}">${ico('next')}</button>` : ''}</div></div>`).join('')}</div>`;
}
function warnHTML() {
  const w = [], c = S.clock || {}, u = S.update || {};
  if (c.checked && !c.error && !c.synced) w.push(T(`The speaker clock is off by ${Math.round(c.offsetMs / 1000)} s; alarms may ring at the wrong time.`, `Die Uhr des Lautsprechers geht ${Math.round(c.offsetMs / 1000)} s falsch; Wecker klingeln dann zur falschen Zeit.`));
  if (u.rolledBack) w.push(T('The last update was rolled back because a service kept failing. See Settings > Update.', 'Das letzte Update wurde zurückgenommen, weil ein Dienst wiederholt ausfiel. Siehe Einstellungen > Update.'));
  const bad = (S.sys.services || []).filter(v => v.failing);
  if (bad.length) w.push(T('Keeps failing: ', 'Fällt wiederholt aus: ') + bad.map(v => esc(v.title)).join(', '));
  return w.map(x => `<div class="callout callout-warn" style="margin-bottom:1rem">${x}</div>`).join('');
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
  $('#c-warn').innerHTML = warnHTML();
  $('#c-now').innerHTML = nowHTML();
  $('#c-ann').innerHTML = `<h3>${T('Announcement', 'Durchsage')}</h3>
    <p>${T('Plays over the music, which is lowered meanwhile. Home Assistant can send text-to-speech or any audio address.', 'Spielt über die Musik, die solange leiser wird. Home Assistant kann Sprachausgabe oder jede Audio-Adresse schicken.')}</p>
    <div class="chips"><button class="chip is-filter" data-ann="chime">${T('Chime', 'Gong')}</button><button class="chip is-filter" data-ann="bell">${T('Door bell', 'Türklingel')}</button><button class="chip is-filter" data-ann="beep">${T('Beep', 'Piep')}</button></div>`;
  const timers = s.timers.map(t => `<div class="item"><div><div class="t">${esc(t.name)}</div><div class="s">${fmtDur(t.remaining)} / ${fmtDur(t.total)}</div>
      <div class="bar"><i style="width:${t.total ? (100 - 100 * t.remaining / t.total) : 0}%"></i></div></div>
      <button class="btn btn-outline btn-sm" data-tcancel="${t.id}">${ico('trash')}</button></div>`).join('');
  $('#c-time').innerHTML = `
    <h3>${T('Alarms & timers', 'Wecker & Timer')}</h3>
    <p class="mono">${T('Next alarm', 'Nächster Wecker')}: <b class="muted">${s.nextAlarm ? esc(s.nextAlarmName) + ' · ' + fmtTime(s.nextAlarm) : '–'}</b></p>
    <div class="list">${timers || `<p>${T('No timer running.', 'Kein Timer läuft.')}</p>`}</div>
    <div class="chips">${[1, 5, 10, 15, 30, 60].map(m => `<button class="chip is-filter" data-quick="${m}">${m} min</button>`).join('')}</div>
    <p class="mono">${T('Sleep timer', 'Schlummertimer')}: <b class="muted">${s.sleepSecs ? fmtDur(s.sleepSecs) : T('off', 'aus')}</b></p>
    <div class="chips">${[15, 30, 45, 60, 90].map(m => `<button class="chip is-filter" data-sleep="${m}">${m} min</button>`).join('')}${s.sleepSecs ? `<button class="chip is-filter" data-sleep="0">${T('Off', 'Aus')}</button>` : ''}</div>`;
  const bt = s.bluetooth;
  $('#c-bt').innerHTML = `
    <h3>Bluetooth</h3>
    <p>${s.btMode === 'always' ? T('Always visible and ready to pair.', 'Immer sichtbar und koppelbereit.')
      : bt.open ? T('Pairing window <b>open</b>: pair now.', 'Kopplungs-Fenster <b>offen</b>: jetzt koppeln.')
        : T('Not visible. Press the Bluetooth button on the speaker or open the window here; paired phones reconnect by themselves.',
          'Nicht sichtbar. Bluetooth-Knopf am Lautsprecher drücken oder hier das Fenster öffnen; gekoppelte Handys verbinden sich selbst.')}</p>
    ${s.btMode === 'always' ? '' : `<button class="btn btn-outline btn-sm" id="bt-toggle">${ico('bt')}${bt.open ? T('Close pairing', 'Kopplung schließen') : T('Open pairing (2 min)', 'Kopplung öffnen (2 Min.)')}</button>`}`;
  $('#c-svc').innerHTML = (s.sys.services || []).filter(v => v.enabled).map(v => `<div class="status" data-state="${v.failing ? 'bad' : v.running ? 'ok' : 'off'}"><span class="name">${esc(v.title)}</span><span class="row">${v.failing ? T(`keeps failing, retry in ${v.waitSecs} s`, `fällt aus, neuer Versuch in ${v.waitSecs} s`) : v.running ? T('running', 'läuft') : T('not running', 'läuft nicht')}</span></div>`).join('') +
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
  bindNow();
  $$('[data-sleep]').forEach(b => b.onclick = () => act(() => api('/api/sleep', 'POST', { minutes: +b.dataset.sleep }), +b.dataset.sleep ? tt('Sleep timer set', 'Schlummertimer gesetzt') : tt('Sleep timer off', 'Schlummertimer aus')));
  $$('[data-ann]').forEach(b => b.onclick = () => act(() => api('/api/announce', 'POST', { tone: b.dataset.ann })));
}
function bindNow() {
  $$('[data-btc]').forEach(b => b.onclick = () => act(() => api('/api/bluetooth/control', 'POST', { action: b.dataset.btc })));
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
      <div class="field"><label for="a-src-${i}">${T('Sound', 'Ton')}</label><select class="select a-src" id="a-src-${i}"><option value="tone">${tt('Beeps', 'Signalton')}</option>
        ${radios.map((r, k) => `<option value="radio:${k}" ${a.source === 'radio:' + k ? 'selected' : ''}>${esc(r.name)}</option>`).join('')}
        <option value="url" ${(a.source || '').startsWith('url:') ? 'selected' : ''}>${tt('Address (stream or file) …', 'Adresse (Stream oder Datei) …')}</option></select></div>
      <div class="field ${(a.source || '').startsWith('url:') ? '' : 'hidden'}" id="a-urlf-${i}"><label for="a-url-${i}">${T('Address', 'Adresse')}</label><input class="input" id="a-url-${i}" value="${esc((a.source || '').startsWith('url:') ? a.source.slice(4) : '')}" placeholder="https://"></div>
      <div class="field wide"><span class="field-label">${T('Days (none = every day)', 'Tage (keine = täglich)')}</span>
        <div class="chips">${DAYS.map(([d, en, de]) => `<button type="button" class="chip is-filter a-day" data-d="${d}" aria-pressed="${(a.days || []).includes(d)}">${T(en, de)}</button>`).join('')}</div></div>
      ${fld('a-vol-' + i, 'Volume % (0 = unchanged)', 'Lautstärke % (0 = unverändert)', a.volume, 'type="number" min="0" max="100"')}
      ${fld('a-ramp-' + i, 'Fade-in (seconds)', 'Anstieg (Sekunden)', a.rampSecs, 'type="number" min="0" max="1800"')}
      ${fld('a-snz-' + i, 'Snooze (minutes)', 'Schlummern (Minuten)', a.snoozeMin, 'type="number" min="0" max="60"')}
      ${fld('a-max-' + i, 'Stop after (minutes)', 'Stoppen nach (Minuten)', a.maxMins, 'type="number" min="0" max="240"')}
      ${fld('a-sun-' + i, 'Sunrise light before (minutes, 0 = off)', 'Lichtwecker vorher (Minuten, 0 = aus)', a.sunriseMin || 0, 'type="number" min="0" max="60"')}
      ${fld('a-fade-' + i, 'Fade out when stopped (seconds)', 'Beim Stoppen ausblenden (Sekunden)', a.fadeOutSecs || 0, 'type="number" min="0" max="120"')}
      <div class="wide">${sw('a-hol-' + i, a.skipHolidays, 'Not on public holidays', 'Nicht an Feiertagen')}</div>
    </div>
    <div class="row"><button class="btn btn-outline btn-sm" data-adel="${i}">${ico('trash')}${T('Delete', 'Löschen')}</button>
      ${a.id ? (a.skipDate ? `<button class="btn btn-outline btn-sm" data-askip="${a.id}" data-v="0">${T('Skipped on', 'Aussetzen am')} ${esc(a.skipDate)} ${T('— undo', '— zurücknehmen')}</button>` : `<button class="btn btn-outline btn-sm" data-askip="${a.id}" data-v="1">${ico('skip')}${T('Skip next', 'Nächstes Mal aussetzen')}</button>`) : ''}</div></div>`;
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
      <p class="mono small">${T('Speaker time', 'Zeit am Lautsprecher')}: <span id="spk-time">${fmtTime(S.now)}</span>${S.clock && S.clock.checked ? ` · ${T('deviation', 'Abweichung')} ${(S.clock.offsetMs / 1000).toFixed(1)} s` : ''}</p>
      <div class="field"><label for="hol">${T('Public holidays (for "not on holidays")', 'Feiertage (für „nicht an Feiertagen“)')}</label><div class="row"><select class="select" id="hol" style="max-width:16rem"><option value="">${tt('None', 'Keine')}</option>${CFG.holidayRegions.map(r => `<option value="${r}" ${CFG.settings.holidays === r ? 'selected' : ''}>${r === 'DE' ? tt('Germany (nationwide)', 'Deutschland (bundesweit)') : r}</option>`).join('')}</select>
        <button class="btn btn-outline btn-sm" id="hol-save">${ico('save')}${T('Save', 'Speichern')}</button></div>${S.holiday ? `<p class="small">${T('Today', 'Heute')}: ${esc(S.holiday)}</p>` : ''}</div></div>
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
    sunriseMin: +$(`#a-sun-${i}`).value || 0, fadeOutSecs: +$(`#a-fade-${i}`).value || 0, skipHolidays: swVal('a-hol-' + i),
    skipDate: (draft.alarms[i] || {}).skipDate || '',
  })).map((a, i) => a.source === 'url' ? { ...a, source: 'url:' + $(`#a-url-${i}`).value.trim() } : a);
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
  $('#hol-save').onclick = () => act(async () => { await api('/api/settings/holidays', 'PUT', { region: $('#hol').value }); await loadCfg(); }, tt('Saved', 'Gespeichert'));
  $$('.a-src').forEach(sel => sel.onchange = () => $('#a-urlf-' + sel.id.split('-').pop()).classList.toggle('hidden', sel.value !== 'url'));
  $$('[data-askip]').forEach(b => b.onclick = () => act(async () => { await api('/api/alarms/skip', 'POST', { id: b.dataset.askip, skip: b.dataset.v === '1' }); delete draft.alarms; await loadCfg(); render(); }));
}

function actionLabel(a) {
  const m = { sleep_toggle: ['Sleep timer 30 min on/off', 'Schlummertimer 30 Min. an/aus'], chime: ['Chime', 'Gong'], none: ['Nothing', 'Nichts'], smart: ['Smart: snooze alarm / end timer / mute', 'Smart: Wecker schlummern / Timer beenden / Stumm'], mute_toggle: ['Toggle mute', 'Stumm umschalten'], volume_up: ['Volume +5', 'Lauter +5'], volume_down: ['Volume −5', 'Leiser −5'], radio_toggle: ['Radio on/off', 'Radio an/aus'], radio_next: ['Next station', 'Nächster Sender'], alarm_stop: ['Stop alarm', 'Wecker stoppen'], alarm_snooze: ['Snooze alarm', 'Wecker schlummern'], timer_dismiss: ['End ringing timer', 'Klingelnden Timer beenden'], timers_cancel: ['Cancel all timers', 'Alle Timer abbrechen'], stop_all: ['Stop everything', 'Alles stoppen'], bt_pairing: ['Bluetooth pairing window', 'Bluetooth-Kopplungsfenster'] }[a];
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
    ${sw('m-tls', m.tls, 'Encrypted (TLS, port 8883)', 'Verschlüsselt (TLS, Port 8883)')}${sw('m-ins', m.insecure, 'Accept self-signed broker certificate', 'Selbst signiertes Broker-Zertifikat annehmen')}
    <p>${T('Also offered: the light ring as a light (colour, brightness, effects), announcements (text entity: audio address or chime / bell / beep), sleep timer, now playing (source, title, artist).', 'Außerdem: der Leuchtring als Licht (Farbe, Helligkeit, Effekte), Durchsagen (Text-Entität: Audio-Adresse oder chime / bell / beep), Schlummertimer, „Läuft gerade“ (Quelle, Titel, Interpret).')}</p>
    <div class="row"><button class="btn btn-accent btn-sm" id="m-save">${ico('save')}${T('Save', 'Speichern')}</button></div></div>`;
}
function bindHA() {
  bindSw($('#view'));
  $('#m-save').onclick = () => act(async () => {
    await api('/api/settings/mqtt', 'PUT', { enabled: swVal('m-en'), host: $('#m-host').value.trim(), port: +$('#m-port').value || 1883, user: $('#m-user').value, pass: $('#m-pass').value, discovery: $('#m-disc').value.trim() || 'homeassistant', tls: swVal('m-tls'), insecure: swVal('m-ins') });
    await loadCfg();
  }, tt('Saved', 'Gespeichert'));
}

const vizHTML = () => !S.viz.tap ? T('No music signal found yet (play something; needs the current installation).', 'Noch kein Musiksignal gefunden (etwas abspielen; braucht die aktuelle Installation).')
  : S.viz.active ? `<span class="pill" data-state="applied">${T('showing music', 'zeigt Musik')}</span>` : '';
const GROUPS = { spotify: 'Spotify Connect', upnp: 'UPnP/DLNA', cast: 'Cast', airplay: 'AirPlay', sendspin: 'Sendspin (Music Assistant)', bluetooth: 'Bluetooth', tidal: 'Tidal Connect', snapcast: 'Snapcast (Multiroom)' };
const updHTML = () => {
  const u = S.update || {};
  return `<p class="mono small">${T('Installed', 'Installiert')}: <b class="muted">${esc(u.current || CFG.version)}</b>${u.pending ? ' · ' + T('watching the new version (10 min)', 'neue Version unter Beobachtung (10 Min.)') : ''}</p>
    ${u.rolledBack ? `<div class="callout callout-warn">${T('The last update was rolled back', 'Das letzte Update wurde zurückgenommen')}: ${esc(u.rolledBack)}</div>` : ''}
    ${u.busy || u.message ? `<p class="mono small">${esc(u.message || '')}</p>` : ''}`;
};
function viewSettings() {
  const d = CFG.device, v = CFG.settings.viz, e = CFG.settings.eq || {}, so = CFG.settings.sources || { policy: 'last', limits: {} };
  const lim = so.limits || {};
  const rgbHex = '#' + ((v.rgb || []).some(x => x) ? v.rgb : [255, 190, 110]).map(x => x.toString(16).padStart(2, '0')).join('');
  return `<div class="grid">
  <div class="card" data-tier="yellow"><h3>${T('Speaker', 'Lautsprecher')}</h3>
    ${fld('s-name', 'Name (Spotify, UPnP, Cast, AirPlay, Bluetooth)', 'Name (Spotify, UPnP, Cast, AirPlay, Bluetooth)', d.name)}
    <div class="field"><span class="field-label">${T('Bluetooth pairing', 'Bluetooth-Kopplung')}</span>${seg('s-bt', [['button', 'Only after button press', 'Nur nach Knopfdruck'], ['always', 'Always open', 'Immer offen']], d.bluetoothPairing)}</div>
    <p>${T('A name change applies after restarting the services below.', 'Eine Namensänderung gilt nach dem Neustart der Dienste weiter unten.')}</p>
    <div class="row"><button class="btn btn-accent btn-sm" id="s-save">${ico('save')}${T('Save', 'Speichern')}</button></div></div>
  <div class="card" data-tier="yellow"><h3>${T('Sound', 'Klang')}</h3>
    <p>${T('Applies to every source. Loudness adds bass and treble the quieter the speaker plays; night mode evens out loud and quiet passages.', 'Gilt für alle Quellen. Loudness hebt Bass und Höhen an, je leiser der Lautsprecher spielt; der Nachtmodus gleicht laute und leise Stellen an.')}</p>
    <div class="range field"><label for="e-bass">${T('Bass (dB)', 'Bass (dB)')}</label><div class="range-row"><input id="e-bass" type="range" min="-12" max="12" step="1" value="${e.bass || 0}"><output class="range-out" id="e-bass-o">${e.bass || 0}</output></div></div>
    <div class="range field"><label for="e-treb">${T('Treble (dB)', 'Höhen (dB)')}</label><div class="range-row"><input id="e-treb" type="range" min="-12" max="12" step="1" value="${e.treble || 0}"><output class="range-out" id="e-treb-o">${e.treble || 0}</output></div></div>
    ${sw('e-loud', e.loudness, 'Loudness', 'Loudness')}${sw('e-night', e.night, 'Night mode', 'Nachtmodus')}
    <div class="row"><button class="btn btn-accent btn-sm" id="e-save">${ico('save')}${T('Save', 'Speichern')}</button></div></div>
  <div class="card" data-tier="cyan"><h3>${T('Sources and volume', 'Quellen und Lautstärke')}</h3>
    <div class="field"><span class="field-label">${T('When a second source starts', 'Wenn eine zweite Quelle beginnt')}</span>${seg('q-pol', [['last', 'The newest plays, the others pause', 'Die neueste spielt, die anderen pausieren'], ['mix', 'All play together', 'Alle spielen zusammen']], so.policy)}</div>
    <div class="alarm">${fld('q-max', 'Highest volume % (0 = no limit)', 'Höchste Lautstärke % (0 = keine Grenze)', so.max || 0, 'type="number" min="0" max="100"')}
      ${fld('q-duck', 'Lower music during announcements (dB)', 'Musik bei Durchsagen absenken (dB)', so.duckDB || 0, 'type="number" min="0" max="40"')}</div>
    <p>${T('Per source: highest volume and the volume it starts with (0 = no rule).', 'Je Quelle: höchste Lautstärke und die Lautstärke beim Start (0 = keine Regel).')}</p>
    <div class="list">${CFG.sourceNames.map(n => `<div class="item"><div class="t">${srcName(n)}</div><div class="row">
      <input class="input q-lmax" data-n="${n}" type="number" min="0" max="100" value="${(lim[n] || {}).max || 0}" style="width:5.2rem" aria-label="${tt('max', 'höchstens')}" title="${tt('Highest %', 'Höchstens %')}">
      <input class="input q-lst" data-n="${n}" type="number" min="0" max="100" value="${(lim[n] || {}).start || 0}" style="width:5.2rem" aria-label="${tt('start', 'Start')}" title="${tt('Start %', 'Start %')}"></div></div>`).join('')}</div>
    <p class="small mono">${T('left: highest % · right: start %', 'links: höchstens % · rechts: Start %')}</p>
    <div class="row"><button class="btn btn-accent btn-sm" id="q-save">${ico('save')}${T('Save', 'Speichern')}</button></div></div>
  <div class="card" data-tier="cyan"><h3>${T('Light ring', 'Leuchtring')}</h3>
    <p>${T('The ring can follow the music of all receivers or glow as a lamp. Volume knob, mute, alarm, timer and buttons keep showing their own animations.', 'Der Ring kann der Musik aller Empfänger folgen oder als Lampe leuchten. Drehrad, Stumm, Wecker, Timer und Tasten zeigen weiter ihre eigenen Animationen.')} <span id="v-st">${vizHTML()}</span></p>
    <div class="field"><span class="field-label">${T('Display', 'Anzeige')}</span>${seg('v-mode', [['off', 'Off', 'Aus'], ['spectrum', 'Spectrum', 'Spektrum'], ['level', 'Level', 'Pegel'], ['pulse', 'Pulse', 'Puls'], ['static', 'Lamp', 'Lampe']], v.mode)}</div>
    <div class="field"><span class="field-label">${T('Colour', 'Farbe')}</span>${seg('v-col', [['rainbow', 'Rainbow', 'Regenbogen'], ['white', 'White', 'Weiß'], ['warm', 'Warm', 'Warm'], ['blue', 'Blue', 'Blau'], ['green', 'Green', 'Grün'], ['red', 'Red', 'Rot'], ['purple', 'Purple', 'Lila'], ['custom', 'Own', 'Eigene']], v.color)}</div>
    <div class="alarm"><div class="field"><label for="v-rgb">${T('Own colour', 'Eigene Farbe')}</label><input class="input" id="v-rgb" type="color" value="${rgbHex}" style="height:2.4rem;padding:.2rem"></div>
      <div class="range field"><label for="v-bri">${T('Brightness %', 'Helligkeit %')}</label>
        <div class="range-row"><input id="v-bri" type="range" min="5" max="100" step="5" value="${v.brightness}"><output class="range-out" id="v-bri-o">${v.brightness}</output></div></div>
      ${fld('v-rot', 'Start LED (turns the display)', 'Start-LED (dreht die Anzeige)', v.rotate, 'type="number" min="0" max="11"')}
      <div class="wide">${sw('v-timer', v.timerRing, 'Show the remaining time of a timer on the ring', 'Restzeit eines Timers auf dem Ring zeigen')}</div></div>
    <div class="row"><button class="btn btn-accent btn-sm" id="v-save">${ico('save')}${T('Save', 'Speichern')}</button><button class="btn btn-outline btn-sm" id="l-test">${ico('test')}${T('Test ring', 'Ring testen')}</button></div></div>
  <div class="card"><h3>${T('Services', 'Dienste')}</h3>
    <p>${T('Switched-off services are not started and their network ports stay closed.', 'Ausgeschaltete Dienste werden nicht gestartet, ihre Netzwerk-Ports bleiben zu.')}</p>
    <div class="stack">${CFG.groups.map(g => sw('g-' + g.group, g.enabled, GROUPS[g.group] || g.group, GROUPS[g.group] || g.group)).join('')}</div>
    <p>${T('Restarting stops a service; the supervisor starts it again within 30 seconds.', 'Neustart beendet einen Dienst; der Überwacher startet ihn binnen 30 Sekunden neu.')}</p>
    <div class="list" id="svc-list">${svcListHTML()}</div></div>
  <div class="card"><h3>${T('Back up and restore', 'Sichern und wiederherstellen')}</h3>
    <p>${T('The backup holds the settings, Bluetooth pairings, Spotify login and SSH keys. It contains secrets: keep it safe.', 'Die Sicherung enthält Einstellungen, Bluetooth-Kopplungen, Spotify-Anmeldung und SSH-Schlüssel. Sie enthält Geheimnisse: sicher aufbewahren.')}</p>
    <div class="row"><a class="btn btn-outline btn-sm" href="/api/backup">${ico('down')}${T('Download backup', 'Sicherung herunterladen')}</a>
      <label class="btn btn-outline btn-sm">${ico('up')}${T('Restore …', 'Wiederherstellen …')}<input type="file" id="b-file" accept=".gz,.tgz,application/gzip" hidden></label></div>
    <p>${T('For a bug report: status, settings without secrets and the logs.', 'Für einen Fehlerbericht: Status, Einstellungen ohne Geheimnisse und die Protokolle.')}</p>
    <div class="row"><a class="btn btn-outline btn-sm" href="/api/diag">${ico('down')}${T('Diagnostics package', 'Diagnosepaket')}</a></div></div>
  <div class="card"><h3>${T('Update', 'Update')}</h3>
    <div id="u-st">${updHTML()}</div>
    ${(S.update || {}).keySet ? `<div class="row"><button class="btn btn-outline btn-sm" id="u-check">${T('Check', 'Prüfen')}</button><button class="btn btn-accent btn-sm hidden" id="u-inst">${T('Install', 'Installieren')}</button></div>
      <p class="small">${T('Without internet on the speaker: upload the package and its .sig file.', 'Ohne Internet am Lautsprecher: Paket und .sig-Datei hochladen.')}</p>
      <div class="row"><input type="file" id="u-pkg" accept=".gz"><input type="file" id="u-sig" accept=".sig"><button class="btn btn-outline btn-sm" id="u-up">${ico('up')}${T('Upload', 'Hochladen')}</button></div>`
      : `<p>${T('Updates from here need the release signing key (UPDATE_PUBKEY in /data/invoke/config, see README). Until then update with ./install.sh.', 'Updates von hier brauchen den Signaturschlüssel der Releases (UPDATE_PUBKEY in /data/invoke/config, siehe README). Bis dahin mit ./install.sh aktualisieren.')}</p>`}
    <div class="row"><button class="btn btn-outline btn-sm" id="u-back">${ico('restart')}${T('Roll back last update', 'Letztes Update zurücknehmen')}</button></div></div>
  <div class="card"><h3>${T('Web password', 'Web-Passwort')}</h3>
    <p>${T('At least 6 characters. Stored only as a salted hash.', 'Mindestens 6 Zeichen. Wird nur als gesalzener Hash gespeichert.')}</p>
    <div class="field"><label for="pw">${T('New password', 'Neues Passwort')}</label><input class="input" id="pw" type="password" autocomplete="new-password"></div>
    <div class="row"><button class="btn btn-accent btn-sm" id="pw-save">${ico('save')}${T('Change', 'Ändern')}</button><button class="btn btn-outline btn-sm" id="logout">${T('Sign out', 'Abmelden')}</button></div></div></div>
  <div class="card" style="margin-top:1.2rem" id="logcard"><h3 id="log-t">${T('Log', 'Protokoll')}</h3><div class="log" id="logbox">${tt('Choose a service above.', 'Wähle oben einen Dienst.')}</div></div>`;
}
function svcListHTML() {
  const st = Object.fromEntries((S.sys.services || []).map(v => [v.name, v]));
  return [...CFG.services.map(d => d.name), 'hook'].map(n => {
    const v = st[n] || {};
    const label = n === 'hook' ? T('Supervisor (hook)', 'Überwacher (Hook)') : esc(v.title || n);
    const state = n === 'hook' ? '' : !v.enabled ? T('off', 'aus') : v.failing ? T(`keeps failing (${v.fails}×), retry in ${v.waitSecs} s`, `fällt aus (${v.fails}×), neuer Versuch in ${v.waitSecs} s`) : v.running ? T('running', 'läuft') + (v.restarts ? ` · ${v.restarts}× ${tt('restarted', 'neu gestartet')}` : '') : T('not running', 'läuft nicht');
    return `<div class="item"><div><div class="t">${label}</div><div class="s">${n}${state ? ' · ' : ''}${state}</div></div><div class="row"><button class="btn btn-outline btn-sm" data-log="${n}">${T('Log', 'Protokoll')}</button>${n === 'hook' || !v.running ? '' : `<button class="btn btn-outline btn-sm" data-rs="${n}" aria-label="${tt('Restart', 'Neu starten')}">${ico('restart')}</button>`}</div></div>`;
  }).join('');
}
function bindSvcList() {
  $$('[data-rs]').forEach(b => b.onclick = () => act(() => api('/api/services/restart', 'POST', { name: b.dataset.rs }), tt('Stopped, will restart within 30 s', 'Gestoppt, startet binnen 30 s neu')));
  $$('[data-log]').forEach(b => b.onclick = async () => {
    try { const r = await api('/api/logs?name=' + b.dataset.log); $('#logbox').textContent = r.log || '–'; $('#logbox').scrollTop = 1e9; } catch (e) { toast(e.message, 'error'); }
  });
}
function bindSettings() {
  bindSw($('#view')); bindSeg($('#view'));
  $('#s-save').onclick = () => act(async () => { await api('/api/settings/device', 'PUT', { ...CFG.device, name: $('#s-name').value, bluetoothPairing: segVal('s-bt') }); await loadCfg(); }, tt('Saved', 'Gespeichert'));
  $('#l-test').onclick = () => act(() => api('/api/led/test', 'POST', {}));
  const rng = (id) => { $('#' + id).oninput = () => { $('#' + id + '-o').textContent = $('#' + id).value; }; };
  rng('v-bri'); rng('e-bass'); rng('e-treb');
  $('#v-rgb').oninput = () => $$('#v-col button').forEach(x => x.setAttribute('aria-pressed', x.dataset.v === 'custom'));
  $('#v-save').onclick = () => act(async () => {
    const h = $('#v-rgb').value, rgb = [1, 3, 5].map(i => parseInt(h.slice(i, i + 2), 16));
    await api('/api/settings/viz', 'PUT', { mode: segVal('v-mode'), color: segVal('v-col'), rgb, brightness: +$('#v-bri').value, rotate: +$('#v-rot').value || 0, timerRing: swVal('v-timer') }); await loadCfg();
  }, tt('Saved', 'Gespeichert'));
  $('#e-save').onclick = () => act(async () => { await api('/api/settings/eq', 'PUT', { bass: +$('#e-bass').value, treble: +$('#e-treb').value, loudness: swVal('e-loud'), night: swVal('e-night') }); await loadCfg(); }, tt('Saved', 'Gespeichert'));
  $('#q-save').onclick = () => act(async () => {
    const limits = {};
    CFG.sourceNames.forEach(n => { limits[n] = { max: +$(`.q-lmax[data-n="${n}"]`).value || 0, start: +$(`.q-lst[data-n="${n}"]`).value || 0 }; });
    await api('/api/settings/sources', 'PUT', { policy: segVal('q-pol'), max: +$('#q-max').value || 0, duckDB: +$('#q-duck').value || 0, limits }); await loadCfg();
  }, tt('Saved', 'Gespeichert'));
  CFG.groups.forEach(g => { const el = $('#g-' + g.group); el.onchange = () => act(async () => { await api('/api/services/group', 'POST', { group: g.group, enabled: swVal('g-' + g.group) }); await loadCfg(); }, swVal('g-' + g.group) ? tt('Switched on, starts within 30 s', 'Eingeschaltet, startet binnen 30 s') : tt('Switched off', 'Ausgeschaltet')); });
  bindSvcList();
  $('#b-file').onchange = async () => {
    const f = $('#b-file').files[0]; if (!f) return;
    if (!confirm(tt('Restore this backup? Settings, pairings and keys are replaced and all services restart.', 'Diese Sicherung zurückspielen? Einstellungen, Kopplungen und Schlüssel werden ersetzt, alle Dienste starten neu.'))) return;
    try { const r = await fetch('/api/restore', { method: 'POST', headers: { 'Content-Type': 'application/gzip' }, body: f }); const j = await r.json().catch(() => ({})); if (!r.ok) throw new Error(j.error || r.statusText); toast(tt('Restored, services restart', 'Wiederhergestellt, Dienste starten neu'), 'ok'); }
    catch (e) { toast(e.message, 'error'); }
  };
  const uc = $('#u-check');
  if (uc) uc.onclick = () => act(async () => {
    const r = await api('/api/update');
    $('#u-st').innerHTML = updHTML() + `<p class="mono small">${r.message ? esc(r.message) : r.available ? T('New version', 'Neue Version') + ': <b>' + esc(r.latest) + '</b>' : T('Up to date', 'Aktuell') + (r.latest ? ' (' + esc(r.latest) + ')' : '')}</p>${r.notes ? `<div class="log">${esc(r.notes)}</div>` : ''}`;
    $('#u-inst').classList.toggle('hidden', !r.available);
  });
  const ui = $('#u-inst');
  if (ui) ui.onclick = () => { if (confirm(tt('Install the update now? All services restart; a failing update is rolled back automatically.', 'Update jetzt installieren? Alle Dienste starten neu; ein fehlerhaftes Update wird von selbst zurückgenommen.'))) act(() => api('/api/update/install', 'POST', {}), tt('Update is being installed', 'Update wird installiert')); };
  const uu = $('#u-up');
  if (uu) uu.onclick = async () => {
    const p = $('#u-pkg').files[0], g = $('#u-sig').files[0];
    if (!p || !g) { toast(tt('Choose package and .sig file', 'Paket und .sig-Datei wählen'), 'error'); return; }
    const fd = new FormData(); fd.append('file', p); fd.append('sig', await g.text());
    try { const r = await fetch('/api/update/upload', { method: 'POST', body: fd }); const j = await r.json().catch(() => ({})); if (!r.ok) throw new Error(j.error || r.statusText); toast(tt('Update is being installed', 'Update wird installiert'), 'ok'); }
    catch (e) { toast(e.message, 'error'); }
  };
  $('#u-back').onclick = () => { if (confirm(tt('Restore the version before the last update?', 'Den Stand vor dem letzten Update wiederherstellen?'))) act(() => api('/api/update/rollback', 'POST', {}), tt('Rolling back, services restart', 'Wird zurückgenommen, Dienste starten neu')); };
  $('#pw-save').onclick = () => act(async () => { await api('/api/settings/device', 'PUT', { ...CFG.device, webPassword: $('#pw').value }); $('#pw').value = ''; }, tt('Password changed - sign in again', 'Passwort geändert - bitte neu anmelden'));
  $('#logout').onclick = async () => { await fetch('/api/logout', { method: 'POST' }); location.reload(); };
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
    const sl = $('#svc-list'), sig = JSON.stringify(S.sys.services);
    if (sl && sl.dataset.sig !== sig) { sl.dataset.sig = sig; sl.innerHTML = svcListHTML(); bindSvcList(); }
    const u = $('#u-st'), us = JSON.stringify(S.update);
    if (u && u.dataset.sig !== us && !$('#u-inst:not(.hidden)')) { u.dataset.sig = us; u.innerHTML = updHTML(); }
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
async function refresh() { S = await api('/api/status'); tick(); }
// Live-Zustand über Server-Sent Events (/api/events): Status bei jeder Änderung und alle 10 s, "settings" bei geänderten
// Einstellungen. Timer und Schlummertimer zählt die Seite selbst herunter. Fällt der Strom aus, fragt sie alle 5 s ab
// und versucht es später erneut.
let es = null, pollTimer = null;
function connectEvents() {
  if (!window.EventSource) { pollTimer = pollTimer || setInterval(() => refresh().catch(() => {}), 5000); return; }
  es = new EventSource('/api/events');
  es.addEventListener('status', ev => {
    S = JSON.parse(ev.data); tick();
    if (pollTimer) { clearInterval(pollTimer); pollTimer = null; }
  });
  es.addEventListener('settings', () => pollCfg().catch(() => {}));
  es.onerror = () => {
    $('#conn').textContent = '…';
    es.close(); es = null;
    fetch('/api/status').then(r => { if (r.status === 401) location.reload(); }).catch(() => {});
    if (!pollTimer) pollTimer = setInterval(() => refresh().catch(() => {}), 5000);
    setTimeout(connectEvents, 8000);
  };
}
setInterval(() => {
  if (!S) return;
  let ch = false;
  S.timers.forEach(t => { if (t.remaining > 0) { t.remaining--; ch = true; } });
  if (S.sleepSecs > 1) { S.sleepSecs--; ch = true; }
  if (ch) tick();
}, 1000);
async function start() {
  try { await loadCfg(); await refresh(); } catch (e) { toast(e.message, 'error'); return; }
  onHash(); connectEvents();
}
function onHash() {
  const r = (location.hash.replace(/^#\/?/, '') || 'overview');
  route = VIEWS[r] ? r : 'overview'; draft = {}; lastSig = ''; dirty = false; playSig = JSON.stringify(S.player); render();
}
$$('#tabs button').forEach(b => b.addEventListener('click', () => { const h = '#/' + (b.dataset.r === 'overview' ? '' : b.dataset.r); if (location.hash !== h) location.hash = h; }));
window.addEventListener('hashchange', onHash);
new MutationObserver(() => render()).observe(document.documentElement, { attributes: true, attributeFilter: ['lang'] });
start();
