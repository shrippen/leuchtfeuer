'use strict';
// Weboberfläche von leuchtfeuerd. Gestaltung: Kante (kante/shrippen.css), keine eigenen Farben.
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
  sun: '<circle cx="12" cy="13" r="4"/><path d="M12 3v3M4.9 6.9l2.1 2.1M19.1 6.9 17 9M2 13h3M19 13h3M3 19h18"/>', mic: '<rect x="9" y="3" width="6" height="11" rx="3"/><path d="M5 11a7 7 0 0 0 14 0M12 18v3"/>',
  search: '<circle cx="11" cy="11" r="6"/><path d="M20 20l-4.5-4.5"/>', key: '<circle cx="8" cy="15" r="4"/><path d="M11 12l9-9M17 6l3 3M15 8l2 2"/>',
  copy: '<rect x="9" y="9" width="11" height="11"/><path d="M5 15V4h11"/>', arrowUp: '<path d="M12 19V5M6 11l6-6 6 6"/>', arrowDown: '<path d="M12 5v14M6 13l6 6 6-6"/>',
  wave: '<path d="M3 12h3l2-6 4 12 3-9 2 3h4"/>',
};
const ico = n => `<svg viewBox="0 0 24 24" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${ICON[n] || ''}</svg>`;

const TABS = [
  ['overview', 'Overview', 'Übersicht'], ['radio', 'Radio', 'Radio'], ['alarms', 'Alarms & timers', 'Wecker & Timer'],
  ['buttons', 'Buttons', 'Tasten'], ['network', 'Network', 'Netzwerk'], ['ha', 'Home Assistant', 'Home Assistant'],
  ['settings', 'Settings', 'Einstellungen'],
];

let S = null;        // Status
// Fähigkeiten des Zielgeräts (Status device.capabilities: buttons, ring, vendorSounds, vendorVolume). Ohne Angabe
// (ältere Version) gilt alles als vorhanden.
const can = c => !S || !S.device || (S.device.capabilities || []).includes(c);
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
const STATES = { playing: ['playing', 'spielt'], paused: ['paused', 'pausiert'], stopped: ['stopped', 'gestoppt'], idle: ['idle', 'bereit'], buffering: ['buffering', 'lädt'], reconnecting: ['reconnecting …', 'verbindet neu …'] };
const stateLabel = st => (STATES[st] ? T(...STATES[st]) : esc(st));
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
    <div class="card hidden" data-tier="cyan" id="c-voice"></div>
  </div>
  <h3 class="mono muted small" style="margin:1.6rem 0 .6rem">${T('SERVICES', 'DIENSTE')}</h3>
  <div class="grid kpis" id="c-svc"></div>`;
}
const SRC = { spotify: 'Spotify', upnp: 'UPnP/DLNA', cast: 'Cast', airplay: 'AirPlay', bluetooth: 'Bluetooth', sendspin: 'Sendspin', tidal: 'Tidal', snapcast: 'Snapcast', radio: ['Web radio', 'Webradio'], alarm: ['Alarm', 'Wecker'], announce: ['Announcement', 'Durchsage'], briefing: 'Briefing', measure: ['Room measurement', 'Raummessung'] };
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
      `<p>${T('Sources play through the speaker: Spotify, UPnP/DLNA, Cast, AirPlay, Bluetooth, Sendspin. Web radio, alarms and timers start from here.', 'Quellen spielen über den Lautsprecher: Spotify, UPnP/DLNA, Cast, AirPlay, Bluetooth, Sendspin. Webradio, Wecker und Timer starten hier.')}</p>`}
    <div class="row"><button class="btn btn-outline btn-sm" id="br-go">${ico('sun')}${T('Briefing', 'Briefing')}</button>
      ${S.voice && S.voice.state !== 'off' ? `<button class="btn btn-outline btn-sm" id="vo-go" ${S.voice.connected && !S.voice.muted ? '' : 'disabled'}>${ico('mic')}${T('Listen', 'Zuhören')}</button>` : ''}</div>`;
  $('#c-voice').classList.toggle('hidden', !(S.voice && S.voice.state !== 'off'));
  $('#c-voice').innerHTML = voiceHTML();
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
  // Bluetooth-Karte nur, wenn der Agent eingeschaltet und installiert ist (ältere Versionen ohne Dienstliste: immer)
  const agent = (s.sys.services || []).find(v => v.process === 'btagent');
  $('#c-bt').classList.toggle('hidden', !!(s.sys.services && s.sys.services.length) && !(agent && agent.enabled && !agent.missing));
  $('#c-bt').innerHTML = `
    <h3>Bluetooth</h3>
    <p>${s.btMode === 'always' ? T('Always visible and ready to pair.', 'Immer sichtbar und koppelbereit.')
      : bt.open ? T('Pairing window <b>open</b>: pair now.', 'Kopplungs-Fenster <b>offen</b>: jetzt koppeln.')
        : can('buttons') ? T('Not visible. Press the Bluetooth button on the speaker or open the window here; paired phones reconnect by themselves.',
          'Nicht sichtbar. Bluetooth-Knopf am Lautsprecher drücken oder hier das Fenster öffnen; gekoppelte Handys verbinden sich selbst.')
          : T('Not visible. Open the window here or in Home Assistant; paired phones reconnect by themselves.',
            'Nicht sichtbar. Hier oder in Home Assistant das Fenster öffnen; gekoppelte Handys verbinden sich selbst.')}</p>
    ${s.btMode === 'always' ? '' : `<button class="btn btn-outline btn-sm" id="bt-toggle">${ico('bt')}${bt.open ? T('Close pairing', 'Kopplung schließen') : T('Open pairing (2 min)', 'Kopplung öffnen (2 Min.)')}</button>`}`;
  $('#c-svc').innerHTML = (s.sys.services || []).filter(v => v.enabled).map(v => `<div class="status" data-state="${v.failing ? 'bad' : v.running ? 'ok' : 'off'}"><span class="name">${esc(v.title)}</span><span class="row">${v.missing ? T('not installed', 'nicht installiert') : v.failing ? T(`keeps failing, retry in ${v.waitSecs} s`, `fällt aus, neuer Versuch in ${v.waitSecs} s`) : v.running ? T('running', 'läuft') : T('not running', 'läuft nicht')}</span></div>`).join('') +
    (s.device && !s.device.link ? '' : `<div class="status" data-state="${s.wamp ? 'ok' : 'bad'}"><span class="name">${esc((s.device && s.device.link) || 'audio-ui (WAMP)')}</span><span class="row">${s.wamp ? T('connected', 'verbunden') : T('not connected', 'nicht verbunden')}</span></div>`);
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
  f('#br-go', () => act(() => api('/api/briefing/start', 'POST', {}), tt('Briefing starts', 'Briefing beginnt')));
  f('#vo-go', () => act(() => api('/api/voice/listen', 'POST', {})));
}
const VSTATE = { off: ['off', 'aus'], idle: ['waiting for the wake word', 'wartet auf das Aktivierungswort'], listening: ['listening …', 'hört zu …'], thinking: ['thinking …', 'denkt nach …'], speaking: ['answering', 'antwortet'] };
function voiceHTML(head = true) {
  const v = S.voice || { state: 'off' };
  const st = v.state === 'idle' && (CFG.settings.voice || {}).mode === 'button' ? T('ready (button)', 'bereit (Taste)') : T(...(VSTATE[v.state] || [v.state, v.state]));
  return `${head ? `<h3>${T('Voice assistant', 'Sprachassistent')}</h3>` : ''}
    <p class="mono">${v.state === 'off' ? T('off', 'aus') : v.connected ? st : T('Home Assistant not connected', 'Home Assistant nicht verbunden')}${v.muted ? ' · ' + T('microphone off', 'Mikrofon aus') : ''}</p>
    ${v.lastHeard ? `<p><b class="muted">${T('Heard', 'Gehört')}:</b> ${esc(v.lastHeard)}<br><b class="muted">${T('Answer', 'Antwort')}:</b> ${esc(v.lastAnswer || '–')}</p>` : ''}
    ${v.error ? `<p class="small">${esc(v.error)}</p>` : ''}`;
}
function bindNow() {
  $$('[data-btc]').forEach(b => b.onclick = () => act(() => api('/api/bluetooth/control', 'POST', { action: b.dataset.btc })));
}

function viewRadio() {
  const R = draft.radio || (draft.radio = JSON.parse(JSON.stringify(CFG.settings.radio)));
  const cur = S.player.kind === 'radio' ? S.player.name : '';
  return `<div class="grid">
  <div class="card" data-tier="yellow">
    <h3>${T('Web radio', 'Webradio')}</h3>
    <p>${T('Stations are plain stream addresses (MP3/AAC over http or https). The first five are favourites for the buttons (actions "Favourite 1 … 5"). If a stream breaks off, the speaker reconnects by itself.', 'Sender sind einfache Stream-Adressen (MP3/AAC über http oder https). Die ersten fünf sind Lieblingssender für die Tasten (Aktionen „Lieblingssender 1 … 5“). Bricht ein Stream ab, verbindet sich der Lautsprecher von selbst neu.')}</p>
    <div class="list">${R.map((p, i) => `<div class="item" data-i="${i}">
      <div class="stack"><div class="field"><label>${i < 5 ? `<span class="pill" data-state="applied">${i + 1}</span> ` : ''}${T('Name', 'Name')}</label><input class="input r-name" value="${esc(p.name)}"></div>
      <div class="field"><label>URL</label><input class="input r-url" value="${esc(p.url)}"></div>
      ${cur === p.name ? `<span class="pill" data-state="applied">${stateLabel(S.player.state)}</span>` : ''}</div>
      <div class="stack"><button class="btn btn-accent btn-sm" data-play="${i}">${ico('play')}${T('Play', 'Abspielen')}</button>
      <div class="row"><button class="btn btn-outline btn-sm" data-mv="${i}" data-d="-1" aria-label="${tt('Up', 'Nach oben')}" ${i ? '' : 'disabled'}>${ico('arrowUp')}</button>
      <button class="btn btn-outline btn-sm" data-mv="${i}" data-d="1" aria-label="${tt('Down', 'Nach unten')}" ${i < R.length - 1 ? '' : 'disabled'}>${ico('arrowDown')}</button>
      <button class="btn btn-outline btn-sm" data-del="${i}" aria-label="${tt('Delete', 'Löschen')}">${ico('trash')}</button></div></div></div>`).join('')}</div>
    <div class="row"><button class="btn btn-outline btn-sm" id="r-add">${ico('plus')}${T('Add station', 'Sender hinzufügen')}</button>
      <button class="btn btn-accent btn-sm" id="r-save">${ico('save')}${T('Save', 'Speichern')}</button>
      <button class="btn btn-outline btn-sm" id="r-stop">${ico('stop')}${T('Stop', 'Stopp')}</button></div></div>
  <div class="card" data-tier="cyan">
    <h3>${T('Find stations', 'Sender suchen')}</h3>
    <p>${T('Search the free directory radio-browser.info (only stations that worked at the last check). Listen first, then add.', 'Sucht im freien Verzeichnis radio-browser.info (nur Sender, die bei der letzten Prüfung liefen). Erst anhören, dann hinzufügen.')}</p>
    <div class="row"><input class="input" id="rs-q" placeholder="${tt('Name, e.g. jazz or Deutschlandfunk', 'Name, z. B. Jazz oder Deutschlandfunk')}" style="flex:1;min-width:10rem">
      <select class="select" id="rs-c" style="width:7rem"><option value="">${tt('All', 'Alle')}</option>${['DE', 'AT', 'CH', 'GB', 'US', 'FR', 'NL'].map(c => `<option ${c === 'DE' && document.documentElement.lang === 'de' ? 'selected' : ''}>${c}</option>`).join('')}</select>
      <button class="btn btn-accent btn-sm" id="rs-go">${ico('search')}${T('Search', 'Suchen')}</button></div>
    <div class="list" id="rs-list"></div></div></div>`;
}
function collectRadio() {
  return $$('#view .item[data-i]').map(it => { const old = draft.radio[+it.dataset.i] || {}; return { name: $('.r-name', it).value, url: $('.r-url', it).value, uuid: old.url === $('.r-url', it).value ? old.uuid : undefined }; });
}
let rsResults = [], rsQ = null;
function bindRadio() {
  const keep = () => { draft.radio = collectRadio(); };
  $('#r-add').onclick = () => { keep(); draft.radio.push({ name: '', url: 'https://' }); render(); };
  $('#r-save').onclick = () => act(async () => { keep(); await api('/api/settings/radio', 'PUT', draft.radio); delete draft.radio; await loadCfg(); }, tt('Saved', 'Gespeichert'));
  $('#r-stop').onclick = () => act(() => api('/api/radio/stop', 'POST', {}));
  $$('[data-del]').forEach(b => b.onclick = () => { keep(); draft.radio.splice(+b.dataset.del, 1); render(); });
  $$('[data-mv]').forEach(b => b.onclick = () => {
    keep(); const i = +b.dataset.mv, j = i + +b.dataset.d;
    if (j < 0 || j >= draft.radio.length) return;
    [draft.radio[i], draft.radio[j]] = [draft.radio[j], draft.radio[i]]; dirty = true; render();
  });
  $$('[data-play]').forEach(b => b.onclick = () => act(async () => {
    keep();
    const saved = JSON.stringify(CFG.settings.radio.map(p => [p.name, p.url])) === JSON.stringify(draft.radio.map(p => [p.name, p.url]));
    if (!saved) { await api('/api/settings/radio', 'PUT', draft.radio); await loadCfg(); }
    await api('/api/radio/play', 'POST', { index: +b.dataset.play });
  }));
  const showResults = () => {
    const box = $('#rs-list');
    box.innerHTML = rsResults.map((r, i) => `<div class="item"><div><div class="t">${esc(r.name)}</div>
      <div class="s">${esc(r.countrycode || '')} · ${esc(r.codec || '?')} ${r.bitrate ? r.bitrate + ' kbit/s' : ''}${r.tags ? ' · ' + esc(r.tags.split(',').slice(0, 3).join(', ')) : ''}</div></div>
      <div class="row"><button class="btn btn-outline btn-sm" data-rsplay="${i}" aria-label="${tt('Listen', 'Anhören')}">${ico('play')}</button>
      <button class="btn btn-outline btn-sm" data-rsadd="${i}" ${draft.radio.some(p => p.url === r.url) ? 'disabled' : ''}>${ico('plus')}${T('Add', 'Hinzufügen')}</button></div></div>`).join('') || `<p>${T('Nothing found.', 'Nichts gefunden.')}</p>`;
    $$('[data-rsplay]', box).forEach(b => b.onclick = () => { const r = rsResults[+b.dataset.rsplay]; act(() => api('/api/radio/url', 'POST', { name: r.name, url: r.url, uuid: r.stationuuid })); });
    $$('[data-rsadd]', box).forEach(b => b.onclick = () => {
      const r = rsResults[+b.dataset.rsadd]; keep();
      draft.radio.push({ name: r.name, url: r.url, uuid: r.stationuuid }); dirty = true; render();
      toast(tt('Added - save to keep it', 'Hinzugefügt - speichern, damit er bleibt'), 'ok');
    });
  };
  const search = async () => {
    rsQ = { q: $('#rs-q').value, c: $('#rs-c').value };
    $('#rs-list').innerHTML = `<p>${T('Searching …', 'Suche läuft …')}</p>`;
    try { rsResults = await api('/api/radio/search?q=' + encodeURIComponent(rsQ.q) + '&country=' + rsQ.c); showResults(); }
    catch (e) { rsResults = []; $('#rs-list').innerHTML = ''; toast(e.message, 'error'); }
  };
  if (rsQ) { $('#rs-q').value = rsQ.q; $('#rs-c').value = rsQ.c; if (rsResults.length) showResults(); }
  $('#rs-go').onclick = search;
  $('#rs-q').onkeydown = e => { if (e.key === 'Enter') search(); };
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
        <option value="briefing" ${a.source === 'briefing' ? 'selected' : ''}>${tt('Briefing (chime, briefing, then station)', 'Briefing (Gong, Briefing, danach Sender)')}</option>
        <option value="url" ${(a.source || '').startsWith('url:') ? 'selected' : ''}>${tt('Address (stream or file) …', 'Adresse (Stream oder Datei) …')}</option></select></div>
      <div class="field ${(a.source || '').startsWith('url:') ? '' : 'hidden'}" id="a-urlf-${i}"><label for="a-url-${i}">${T('Address', 'Adresse')}</label><input class="input" id="a-url-${i}" value="${esc((a.source || '').startsWith('url:') ? a.source.slice(4) : '')}" placeholder="https://"></div>
      <div class="field wide"><span class="field-label">${T('Days (none = every day)', 'Tage (keine = täglich)')}</span>
        <div class="chips">${DAYS.map(([d, en, de]) => `<button type="button" class="chip is-filter a-day" data-d="${d}" aria-pressed="${(a.days || []).includes(d)}">${T(en, de)}</button>`).join('')}</div></div>
      ${fld('a-vol-' + i, 'Volume % (0 = unchanged)', 'Lautstärke % (0 = unverändert)', a.volume, 'type="number" min="0" max="100"')}
      ${fld('a-ramp-' + i, 'Fade-in (seconds)', 'Anstieg (Sekunden)', a.rampSecs, 'type="number" min="0" max="1800"')}
      ${fld('a-snz-' + i, 'Snooze (minutes)', 'Schlummern (Minuten)', a.snoozeMin, 'type="number" min="0" max="60"')}
      ${fld('a-max-' + i, 'Stop after (minutes)', 'Stoppen nach (Minuten)', a.maxMins, 'type="number" min="0" max="240"')}
      ${can('ring') ? fld('a-sun-' + i, 'Sunrise light before (minutes, 0 = off)', 'Lichtwecker vorher (Minuten, 0 = aus)', a.sunriseMin || 0, 'type="number" min="0" max="60"') : ''}
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
  <p class="hint">${T('If a station does not start within 15 s or stops early, the built-in alarm tone rings instead.', 'Startet ein Sender nicht binnen 15 s oder bricht er ab, klingelt stattdessen der eingebaute Weckton.')}</p>
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
    sunriseMin: $(`#a-sun-${i}`) ? +$(`#a-sun-${i}`).value || 0 : a.sunriseMin || 0, fadeOutSecs: +$(`#a-fade-${i}`).value || 0, skipHolidays: swVal('a-hol-' + i),
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

// ---------------------------------------------------------------- Briefing
const BTYPES = {
  greeting: ['Greeting, date, holiday', 'Begrüßung, Datum, Feiertag'], weather: ['Weather (Open-Meteo)', 'Wetter (Open-Meteo)'],
  warnings: ['Severe weather warnings (DWD)', 'Unwetterwarnungen (DWD)'], pollen: ['Pollen (DWD)', 'Pollenflug (DWD)'],
  calendar: ['Calendar (ICS)', 'Kalender (ICS)'], podcast: ['News / podcast', 'Nachrichten / Podcast'],
  ha: ['Home Assistant template', 'Home-Assistant-Vorlage'], text: ['Own text', 'Eigener Text'],
};
let pollenRegions = null;
function briefItemHTML(it, i, n) {
  const head = `<div class="row between"><b>${T(...BTYPES[it.type])}</b><div class="row">${sw('bi-on-' + i, it.on, 'On', 'An')}
    <button class="btn btn-outline btn-sm" data-bmv="${i}" data-d="-1" ${i ? '' : 'disabled'} aria-label="${tt('Up', 'Nach oben')}">${ico('arrowUp')}</button>
    <button class="btn btn-outline btn-sm" data-bmv="${i}" data-d="1" ${i < n - 1 ? '' : 'disabled'} aria-label="${tt('Down', 'Nach unten')}">${ico('arrowDown')}</button>
    <button class="btn btn-outline btn-sm" data-bdel2="${i}" aria-label="${tt('Delete', 'Löschen')}">${ico('trash')}</button></div></div>`;
  let body = '';
  if (it.type === 'calendar') body = `<div class="alarm">${fld('bi-name-' + i, 'Name (spoken)', 'Name (wird gesprochen)', it.name)}
      <div class="field"><label for="bi-days-${i}">${T('Day', 'Tag')}</label><select class="select" id="bi-days-${i}">${[[0, 'today', 'heute'], [1, 'tomorrow (e.g. waste collection)', 'morgen (z. B. Müllabfuhr)'], [2, 'in 2 days', 'übermorgen']].map(([v, en, de]) => `<option value="${v}" ${+it.days === v ? 'selected' : ''}>${tt(en, de)}</option>`).join('')}</select></div>
      <div class="field wide"><label for="bi-url-${i}">${T('Calendar address (ICS / webcal)', 'Kalender-Adresse (ICS / webcal)')}</label><input class="input" id="bi-url-${i}" value="${esc(it.url)}" placeholder="https://…/basic.ics"></div></div>`;
  else if (it.type === 'podcast') body = `<div class="alarm"><div class="field wide"><label for="bi-pre-${i}">${T('Podcast', 'Podcast')}</label><select class="select bi-pre" id="bi-pre-${i}" data-i="${i}">
      ${CFG.podcasts.map(p => `<option value="${esc(p.url)}" ${p.url === it.url ? 'selected' : ''}>${esc(p.name)}</option>`).join('')}<option value="" ${CFG.podcasts.some(p => p.url === it.url) ? '' : 'selected'}>${tt('Other feed …', 'Anderer Feed …')}</option></select></div>
      <div class="field wide ${CFG.podcasts.some(p => p.url === it.url) ? 'hidden' : ''}" id="bi-urlf-${i}"><label for="bi-url-${i}">${T('Feed address (RSS)', 'Feed-Adresse (RSS)')}</label><input class="input" id="bi-url-${i}" value="${esc(it.url)}" placeholder="https://"></div></div>`;
  else if (it.type === 'ha') body = `<div class="field"><label for="bi-text-${i}">${T('Template (Jinja), e.g. travel time', 'Vorlage (Jinja), z. B. Fahrzeit')}</label><textarea class="input" rows="2" id="bi-text-${i}" placeholder="Bis zur Arbeit: {{ states('sensor.fahrzeit') }} Minuten.">${esc(it.text)}</textarea></div>`;
  else if (it.type === 'text') body = `<div class="field"><label for="bi-text-${i}">${T('Text', 'Text')}</label><textarea class="input" rows="2" id="bi-text-${i}">${esc(it.text)}</textarea></div>`;
  else if (it.type === 'pollen') body = `<div class="field"><label for="bi-reg-${i}">${T('Region', 'Region')}</label><select class="select bi-reg" id="bi-reg-${i}" data-v="${it.region || 0}">
      ${pollenRegions ? pollenRegions.map(r => `<option value="${r.id}" ${r.id === it.region ? 'selected' : ''}>${esc(r.name)}</option>`).join('') : `<option value="${it.region || 0}">${it.region ? tt('Region ', 'Region ') + it.region : tt('loading …', 'lädt …')}</option>`}</select></div>`;
  return `<div class="item" data-bi2="${i}"><div class="stack" style="width:100%">${head}${body}</div></div>`;
}
function viewBriefing() {
  const B = draft.briefing || (draft.briefing = JSON.parse(JSON.stringify(CFG.settings.briefing)));
  const H = CFG.settings.homeAssistant || {};
  const radios = CFG.settings.radio;
  return `<div class="grid">
  <div class="card" data-tier="yellow"><h3>${T('Briefing', 'Briefing')}</h3>
    <p>${T('A short morning overview: greeting, weather, warnings, appointments, news. It plays from a button (action "Play briefing"), from here, from Home Assistant or as an alarm sound (Alarms > Sound > Briefing).', 'Ein kurzer Überblick am Morgen: Begrüßung, Wetter, Warnungen, Termine, Nachrichten. Er spielt per Taste (Aktion „Briefing abspielen“), von hier, aus Home Assistant oder als Weckton (Wecker > Ton > Briefing).')}</p>
    <div class="list" id="bi-list">${B.items.map((it, i) => briefItemHTML(it, i, B.items.length)).join('')}</div>
    <div class="row"><select class="select" id="bi-new" style="max-width:16rem">${Object.keys(BTYPES).map(k => `<option value="${k}">${tt(...BTYPES[k])}</option>`).join('')}</select>
      <button class="btn btn-outline btn-sm" id="bi-add">${ico('plus')}${T('Add', 'Hinzufügen')}</button></div>
    <div class="row"><button class="btn btn-accent btn-sm" id="bi-save">${ico('save')}${T('Save', 'Speichern')}</button>
      <button class="btn btn-outline btn-sm" id="bi-play">${ico('play')}${T('Play now', 'Jetzt abspielen')}</button>
      <button class="btn btn-outline btn-sm" id="bi-stop">${ico('stop')}${T('Stop', 'Stopp')}</button></div></div>
  <div class="stack" style="gap:1.2rem">
  <div class="card" data-tier="cyan"><h3>${T('Place and language', 'Ort und Sprache')}</h3>
    <p class="mono">${B.place ? esc(B.place) + ` <span class="muted small">(${(+B.lat).toFixed(2)}, ${(+B.lon).toFixed(2)})</span>` : T('No place set.', 'Kein Ort eingestellt.')}</p>
    <div class="row"><input class="input" id="bi-geo" placeholder="${tt('Search a place', 'Ort suchen')}" style="flex:1;min-width:9rem"><button class="btn btn-outline btn-sm" id="bi-geo-go">${ico('search')}${T('Search', 'Suchen')}</button></div>
    <div class="list" id="bi-geo-list"></div>
    <div class="field"><span class="field-label">${T('Language', 'Sprache')}</span>${seg('bi-lang', [['de', 'German', 'Deutsch'], ['en', 'English', 'Englisch']], B.lang || 'de')}</div>
    <div class="field"><label for="bi-then">${T('Afterwards', 'Danach')}</label><select class="select" id="bi-then"><option value="">${tt('Nothing', 'Nichts')}</option>${radios.map((r, k) => `<option value="radio:${k}" ${B.then === 'radio:' + k ? 'selected' : ''}>${esc(r.name)}</option>`).join('')}</select></div></div>
  <div class="card"><h3>${T('Speech', 'Sprachausgabe')}</h3>
    <p>${T('Texts are spoken by a text-to-speech service. Without one, the briefing plays only the news and shows the texts in the preview.', 'Texte spricht ein Sprachausgabe-Dienst. Ohne ihn spielt das Briefing nur die Nachrichten und zeigt die Texte in der Vorschau.')}</p>
    ${seg('bi-tts', [['', 'None', 'Keine'], ['ha', 'Home Assistant', 'Home Assistant'], ['url', 'Own address', 'Eigene Adresse']], B.tts || '')}
    <p class="small ${B.tts === 'ha' ? '' : 'hidden'}" id="bi-tts-ha">${H.url && H.ttsEngine ? T('Uses ', 'Nutzt ') + esc(H.ttsEngine) + ' @ ' + esc(H.url) : T('Set address, token and TTS entity under Home Assistant.', 'Adresse, Token und TTS-Entität unter Home Assistant eintragen.')}</p>
    <div class="field ${B.tts === 'url' ? '' : 'hidden'}" id="bi-tts-urlf"><label for="bi-tts-url">${T('Address with {text} (and {lang})', 'Adresse mit {text} (und {lang})')}</label><input class="input" id="bi-tts-url" value="${esc(B.ttsUrl || '')}" placeholder="http://piper.lan:5000/?text={text}"></div></div>
  <div class="card"><h3>${T('Preview', 'Vorschau')}</h3>
    <p>${T('What the briefing would say right now (data is fetched fresh).', 'Was das Briefing jetzt sagen würde (Daten werden frisch geholt).')}</p>
    <div class="row"><button class="btn btn-outline btn-sm" id="bi-prev">${ico('test')}${T('Show preview', 'Vorschau zeigen')}</button></div>
    <div class="list" id="bi-prev-list"></div></div></div></div>`;
}
function collectBriefing() {
  const B = draft.briefing;
  B.items = $$('#view [data-bi2]').map(el => {
    const i = +el.dataset.bi2, it = { ...B.items[i] };
    it.on = swVal('bi-on-' + i);
    const v = id => { const e = $(`#${id}-${i}`); return e ? e.value : undefined; };
    if (it.type === 'calendar') { it.name = v('bi-name'); it.days = +v('bi-days'); it.url = v('bi-url').trim(); }
    if (it.type === 'podcast') { const pre = v('bi-pre'); it.url = pre || v('bi-url').trim(); const p = CFG.podcasts.find(x => x.url === it.url); it.name = p ? p.name : (it.name || 'Podcast'); }
    if (it.type === 'ha' || it.type === 'text') it.text = v('bi-text');
    if (it.type === 'pollen') it.region = +v('bi-reg') || 0;
    return it;
  });
  B.lang = segVal('bi-lang') || 'de'; B.then = $('#bi-then').value; B.tts = segVal('bi-tts'); B.ttsUrl = $('#bi-tts-url').value.trim();
  return B;
}
async function loadPollenRegions() {
  if (pollenRegions || !$('.bi-reg')) return;
  try { pollenRegions = await api('/api/briefing/pollen-regions'); } catch (e) { toast(e.message, 'error'); return; }
  $$('.bi-reg').forEach(sel => { const v = +sel.dataset.v; sel.innerHTML = `<option value="0">${tt('Choose …', 'Wählen …')}</option>` + pollenRegions.map(r => `<option value="${r.id}" ${r.id === v ? 'selected' : ''}>${esc(r.name)}</option>`).join(''); });
}
function bindBriefing() {
  bindSw($('#view')); bindSeg($('#view'));
  const keep = () => collectBriefing();
  $('#bi-add').onclick = () => { keep(); const t = $('#bi-new').value; draft.briefing.items.push({ type: t, on: true, name: t === 'calendar' ? tt('Calendar', 'Kalender') : '', url: t === 'podcast' ? CFG.podcasts[0].url : '', days: 0, text: '', region: 0 }); dirty = true; render(); };
  $$('[data-bdel2]').forEach(b => b.onclick = () => { keep(); draft.briefing.items.splice(+b.dataset.bdel2, 1); dirty = true; render(); });
  $$('[data-bmv]').forEach(b => b.onclick = () => { keep(); const L = draft.briefing.items, i = +b.dataset.bmv, j = i + +b.dataset.d; if (j < 0 || j >= L.length) return; [L[i], L[j]] = [L[j], L[i]]; dirty = true; render(); });
  $$('.bi-pre').forEach(sel => sel.onchange = () => $('#bi-urlf-' + sel.dataset.i).classList.toggle('hidden', sel.value !== ''));
  $('#bi-tts').addEventListener('change', () => { const v = segVal('bi-tts'); $('#bi-tts-ha').classList.toggle('hidden', v !== 'ha'); $('#bi-tts-urlf').classList.toggle('hidden', v !== 'url'); });
  const save = async () => { await api('/api/settings/briefing', 'PUT', keep()); delete draft.briefing; await loadCfg(); };
  $('#bi-save').onclick = () => act(save, tt('Saved', 'Gespeichert'));
  $('#bi-play').onclick = () => act(async () => { if (dirty) await save(); await api('/api/briefing/start', 'POST', {}); }, tt('Briefing starts', 'Briefing beginnt'));
  $('#bi-stop').onclick = () => act(() => api('/api/briefing/stop', 'POST', {}));
  const geo = async () => {
    const box = $('#bi-geo-list'); box.innerHTML = `<p>${T('Searching …', 'Suche läuft …')}</p>`;
    try {
      const r = await api('/api/briefing/geocode?lang=' + document.documentElement.lang + '&q=' + encodeURIComponent($('#bi-geo').value));
      box.innerHTML = r.map((p, i) => `<div class="item"><div><div class="t">${esc(p.name)}</div><div class="s">${esc([p.admin, p.country].filter(Boolean).join(', '))}</div></div><button class="btn btn-outline btn-sm" data-geo="${i}">${T('Choose', 'Wählen')}</button></div>`).join('') || `<p>${T('Nothing found.', 'Nichts gefunden.')}</p>`;
      $$('[data-geo]', box).forEach(b => b.onclick = () => { keep(); const p = r[+b.dataset.geo]; Object.assign(draft.briefing, { place: p.name, lat: p.lat, lon: p.lon }); dirty = true; render(); });
    } catch (e) { box.innerHTML = ''; toast(e.message, 'error'); }
  };
  $('#bi-geo-go').onclick = geo;
  $('#bi-geo').onkeydown = e => { if (e.key === 'Enter') geo(); };
  $('#bi-prev').onclick = async () => {
    const box = $('#bi-prev-list'); box.innerHTML = `<p>${T('Fetching …', 'Wird geholt …')}</p>`;
    try {
      if (dirty) await save();
      const segs = await api('/api/briefing/preview');
      box.innerHTML = segs.map(x => `<div class="item"><div><div class="s">${esc(x.type)}</div><div class="t">${x.audio ? ico('play') + ' ' + esc(x.audio) : esc(x.text || '')}</div>${x.error ? `<div class="s">${esc(x.error)}</div>` : ''}</div></div>`).join('') || `<p>${T('Nothing to say right now.', 'Gerade nichts zu sagen.')}</p>`;
    } catch (e) { box.innerHTML = ''; toast(e.message, 'error'); }
  };
  loadPollenRegions();
}

function actionLabel(a) {
  const m = { sleep_toggle: ['Sleep timer 30 min on/off', 'Schlummertimer 30 Min. an/aus'], chime: ['Chime', 'Gong'], none: ['Nothing', 'Nichts'], smart: ['Smart: snooze alarm / end timer / mute', 'Smart: Wecker schlummern / Timer beenden / Stumm'], mute_toggle: ['Toggle mute', 'Stumm umschalten'], volume_up: ['Volume +5', 'Lauter +5'], volume_down: ['Volume −5', 'Leiser −5'], radio_toggle: ['Radio on/off', 'Radio an/aus'], radio_next: ['Next station', 'Nächster Sender'], alarm_stop: ['Stop alarm', 'Wecker stoppen'], alarm_snooze: ['Snooze alarm', 'Wecker schlummern'], timer_dismiss: ['End ringing timer', 'Klingelnden Timer beenden'], timers_cancel: ['Cancel all timers', 'Alle Timer abbrechen'], stop_all: ['Stop everything', 'Alles stoppen'], bt_pairing: ['Bluetooth pairing window', 'Bluetooth-Kopplungsfenster'],
    radio_1: ['Favourite 1', 'Lieblingssender 1'], radio_2: ['Favourite 2', 'Lieblingssender 2'], radio_3: ['Favourite 3', 'Lieblingssender 3'], radio_4: ['Favourite 4', 'Lieblingssender 4'], radio_5: ['Favourite 5', 'Lieblingssender 5'],
    briefing: ['Play briefing', 'Briefing abspielen'], voice: ['Voice assistant: listen', 'Sprachassistent: zuhören'], voice_mute: ['Voice assistant: microphone on/off', 'Sprachassistent: Mikrofon an/aus'] }[a];
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
    <datalist id="b-seen">${seen.map(n => `<option value="${esc(n)}">`).join('')}<option value="mic"></datalist><datalist id="b-press"><option value="short"><option value="double"><option value="triple"><option value="long"><option value="0"><option value="1"></datalist>
    <p class="small">${T('"double" and "triple" are counted by this program: once one of them is mapped, a short press waits a moment for more.', '„double“ und „triple“ zählt dieses Programm selbst: Ist eins davon belegt, wartet ein kurzer Druck einen Moment auf weitere.')}</p>
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
  const m = CFG.settings.mqtt, H = CFG.settings.homeAssistant || {}, V = CFG.settings.voice || {};
  return `<div class="grid">
  <div class="card" data-tier="yellow"><h3>${T('Integration', 'Integration')}</h3>
    <p>${T('The Leuchtfeuer integration for Home Assistant (folder custom_components/leuchtfeuer, also via HACS) finds the speaker by itself and offers a real media player, buttons and sensors. It needs an API key (System > Access, scope "full").', 'Die Leuchtfeuer-Integration für Home Assistant (Ordner custom_components/leuchtfeuer, auch über HACS) findet den Lautsprecher von selbst und bietet einen echten Mediaplayer, Knöpfe und Sensoren. Sie braucht einen API-Schlüssel (System > Zugang, Umfang „voll“).')}</p>
    <p>${T('MQTT (below) keeps working in addition.', 'MQTT (unten) funktioniert weiterhin zusätzlich.')}</p>
    <div class="row"><a class="btn btn-outline btn-sm" href="#/system">${ico('key')}${T('Create API key', 'API-Schlüssel anlegen')}</a></div></div>
  <div class="card" data-tier="cyan"><h3>${T('Voice assistant (Assist)', 'Sprachassistent (Assist)')}</h3>
    <p>${T('The speaker becomes a voice satellite for Home Assistant (Wyoming protocol, port 10700). Home Assistant finds it in the Wyoming integration. Wake word, speech recognition and answers come from Home Assistant.', 'Der Lautsprecher wird ein Sprach-Satellit für Home Assistant (Wyoming-Protokoll, Port 10700). Home Assistant findet ihn in der Wyoming-Integration. Aktivierungswort, Spracherkennung und Antworten kommen von Home Assistant.')}</p>
    <div id="vo-st">${voiceHTML(false)}</div>
    ${sw('vo-en', V.enabled, 'Enabled', 'Aktiv')}${sw('vo-mute', V.muted, 'Microphone off', 'Mikrofon aus')}
    <div class="field"><span class="field-label">${T('Listening', 'Zuhören')}</span>${seg('vo-mode', [['wake', 'Wake word (Home Assistant)', 'Aktivierungswort (Home Assistant)'], ['button', 'Only after a button press', 'Nur nach Tastendruck']], V.mode || 'wake')}</div>
    <div class="alarm">${fld('vo-mic', 'Microphone (ALSA device, see scripts/smoke.sh)', 'Mikrofon (ALSA-Gerät, siehe scripts/smoke.sh)', V.mic || '', 'placeholder="plughw:1,0"')}
      ${fld('vo-area', 'Area in Home Assistant', 'Bereich in Home Assistant', V.area || '')}
      ${fld('vo-duck', 'Lower music meanwhile (dB)', 'Musik solange absenken (dB)', V.duckDB ?? 20, 'type="number" min="0" max="40"')}
      ${fld('vo-port', 'Port', 'Port', V.port || 10700, 'type="number" min="1024" max="65535"')}</div>
    <p class="small">${T('No echo cancellation: while it answers, the microphone sends silence. Map the action "Voice assistant: listen" to a button for push-to-talk.', 'Keine Echounterdrückung: Während der Antwort schickt das Mikrofon Stille. Für Drücken-und-Sprechen die Aktion „Sprachassistent: zuhören“ auf eine Taste legen.')}</p>
    <div class="row"><button class="btn btn-accent btn-sm" id="vo-save">${ico('save')}${T('Save', 'Speichern')}</button><button class="btn btn-outline btn-sm" id="vo-listen">${ico('mic')}${T('Listen now', 'Jetzt zuhören')}</button></div></div>
  <div class="card"><h3>${T('Home Assistant for the speaker', 'Home Assistant für den Lautsprecher')}</h3>
    <p>${T('So that the briefing can speak (text-to-speech) and read templates, the speaker needs the address of Home Assistant and a long-lived access token (profile > security; for templates of an administrator).', 'Damit das Briefing sprechen (Sprachausgabe) und Vorlagen lesen kann, braucht der Lautsprecher die Adresse von Home Assistant und ein langlebiges Zugriffstoken (Profil > Sicherheit; für Vorlagen von einem Administrator).')}</p>
    ${fld('ha-url', 'Address', 'Adresse', H.url || '', 'placeholder="http://homeassistant.local:8123"')}
    <div class="field"><label for="ha-tok">${T('Token (empty = keep)', 'Token (leer = behalten)')}</label><input class="input" id="ha-tok" type="password" autocomplete="off"></div>
    ${fld('ha-tts', 'Text-to-speech entity', 'Sprachausgabe-Entität', H.ttsEngine || '', 'placeholder="tts.piper"')}
    <div class="row"><button class="btn btn-accent btn-sm" id="ha-save">${ico('save')}${T('Save', 'Speichern')}</button></div></div>
  <div class="card"><h3>Home Assistant (MQTT)</h3>
    <p><span id="mq-st">${mqttHTML()}</span>
    ${T('Enter the broker that Home Assistant uses (Mosquitto add-on). The speaker then shows up automatically as a device with volume, mute, web radio, Bluetooth pairing, timers, alarm buttons, sensors (temperature, Wi-Fi, uptime) and button events.', 'Trage den Broker ein, den Home Assistant nutzt (Mosquitto-Add-on). Der Lautsprecher erscheint dann automatisch als Gerät mit Lautstärke, Stumm, Webradio, Bluetooth-Kopplung, Timern, Wecker-Tasten, Sensoren (Temperatur, WLAN, Laufzeit) und Tasten-Ereignissen.')}</p>
    ${sw('m-en', m.enabled, 'Enabled', 'Aktiv')}
    <div class="alarm">${fld('m-host', 'Broker host', 'Broker-Adresse', m.host)}${fld('m-port', 'Port', 'Port', m.port, 'type="number"')}
      ${fld('m-user', 'User', 'Benutzer', m.user)}<div class="field"><label for="m-pass">${T('Password (empty = keep)', 'Passwort (leer = behalten)')}</label><input class="input" id="m-pass" type="password" autocomplete="new-password"></div>
      ${fld('m-disc', 'Discovery prefix', 'Erkennungs-Präfix', m.discovery)}</div>
    ${sw('m-tls', m.tls, 'Encrypted (TLS, port 8883)', 'Verschlüsselt (TLS, Port 8883)')}${sw('m-ins', m.insecure, 'Accept self-signed broker certificate', 'Selbst signiertes Broker-Zertifikat annehmen')}
    <p>${can('ring') ? T('Also offered: the light ring as a light (colour, brightness, effects), announcements (text entity: audio address or chime / bell / beep), sleep timer, now playing, briefing and voice buttons.', 'Außerdem: der Leuchtring als Licht (Farbe, Helligkeit, Effekte), Durchsagen (Text-Entität: Audio-Adresse oder chime / bell / beep), Schlummertimer, „Läuft gerade“, Knöpfe für Briefing und Sprachassistent.') : T('Also offered: announcements (text entity: audio address or chime / bell / beep), sleep timer, now playing, briefing and voice buttons.', 'Außerdem: Durchsagen (Text-Entität: Audio-Adresse oder chime / bell / beep), Schlummertimer, „Läuft gerade“, Knöpfe für Briefing und Sprachassistent.')}</p>
    <div class="row"><button class="btn btn-accent btn-sm" id="m-save">${ico('save')}${T('Save', 'Speichern')}</button></div></div></div>`;
}
function bindHA() {
  bindSw($('#view')); bindSeg($('#view'));
  $('#m-save').onclick = () => act(async () => {
    await api('/api/settings/mqtt', 'PUT', { enabled: swVal('m-en'), host: $('#m-host').value.trim(), port: +$('#m-port').value || 1883, user: $('#m-user').value, pass: $('#m-pass').value, discovery: $('#m-disc').value.trim() || 'homeassistant', tls: swVal('m-tls'), insecure: swVal('m-ins') });
    await loadCfg();
  }, tt('Saved', 'Gespeichert'));
  $('#ha-save').onclick = () => act(async () => { await api('/api/settings/homeAssistant', 'PUT', { url: $('#ha-url').value.trim(), token: $('#ha-tok').value.trim(), ttsEngine: $('#ha-tts').value.trim() }); $('#ha-tok').value = ''; await loadCfg(); }, tt('Saved', 'Gespeichert'));
  $('#vo-save').onclick = () => act(async () => {
    await api('/api/settings/voice', 'PUT', { enabled: swVal('vo-en'), muted: swVal('vo-mute'), mode: segVal('vo-mode'), mic: $('#vo-mic').value.trim(), area: $('#vo-area').value.trim(), duckDB: +$('#vo-duck').value || 0, port: +$('#vo-port').value || 10700 });
    await loadCfg();
  }, tt('Saved', 'Gespeichert'));
  $('#vo-listen').onclick = () => act(() => api('/api/voice/listen', 'POST', {}));
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
    <div class="field"><span class="field-label">${T('Bluetooth pairing', 'Bluetooth-Kopplung')}</span>${seg('s-bt', [['button', can('buttons') ? 'Only after button press' : 'Only after opening (here or Home Assistant)', can('buttons') ? 'Nur nach Knopfdruck' : 'Nur nach Öffnen (hier oder Home Assistant)'], ['always', 'Always open', 'Immer offen']], d.bluetoothPairing)}</div>
    <p>${T('A name change applies after restarting the services below.', 'Eine Namensänderung gilt nach dem Neustart der Dienste weiter unten.')}</p>
    <div class="row"><button class="btn btn-accent btn-sm" id="s-save">${ico('save')}${T('Save', 'Speichern')}</button></div></div>
  <div class="card" data-tier="yellow"><h3>${T('Sound', 'Klang')}</h3>
    <p>${T('Applies to every source. Loudness adds bass and treble the quieter the speaker plays; night mode evens out loud and quiet passages.', 'Gilt für alle Quellen. Loudness hebt Bass und Höhen an, je leiser der Lautsprecher spielt; der Nachtmodus gleicht laute und leise Stellen an.')}</p>
    <div class="range field"><label for="e-bass">${T('Bass (dB)', 'Bass (dB)')}</label><div class="range-row"><input id="e-bass" type="range" min="-12" max="12" step="1" value="${e.bass || 0}"><output class="range-out" id="e-bass-o">${e.bass || 0}</output></div></div>
    <div class="range field"><label for="e-treb">${T('Treble (dB)', 'Höhen (dB)')}</label><div class="range-row"><input id="e-treb" type="range" min="-12" max="12" step="1" value="${e.treble || 0}"><output class="range-out" id="e-treb-o">${e.treble || 0}</output></div></div>
    ${sw('e-loud', e.loudness, 'Loudness', 'Loudness')}${sw('e-night', e.night, 'Night mode', 'Nachtmodus')}
    <div class="row"><button class="btn btn-accent btn-sm" id="e-save">${ico('save')}${T('Save', 'Speichern')}</button></div></div>
  ${roomHTML()}
  ${soundsHTML()}
  <div class="card" data-tier="cyan"><h3>${T('Sources and volume', 'Quellen und Lautstärke')}</h3>
    <div class="field"><span class="field-label">${T('When a second source starts', 'Wenn eine zweite Quelle beginnt')}</span>${seg('q-pol', [['last', 'The newest plays, the others pause', 'Die neueste spielt, die anderen pausieren'], ['mix', 'All play together', 'Alle spielen zusammen']], so.policy)}</div>
    <div class="alarm">${fld('q-max', 'Highest volume % (0 = no limit)', 'Höchste Lautstärke % (0 = keine Grenze)', so.max || 0, 'type="number" min="0" max="100"')}
      ${fld('q-duck', 'Lower music during announcements (dB)', 'Musik bei Durchsagen absenken (dB)', so.duckDB || 0, 'type="number" min="0" max="40"')}</div>
    <p>${T('Per source: highest volume, the volume it starts with (0 = no rule) and a level trim that makes loud sources quieter (dB). When a source takes over, the others fade out instead of stopping hard.', 'Je Quelle: höchste Lautstärke, Lautstärke beim Start (0 = keine Regel) und ein Pegelausgleich, der laute Quellen leiser macht (dB). Übernimmt eine Quelle, blenden die anderen aus, statt hart zu verstummen.')}</p>
    <div class="list">${CFG.sourceNames.map(n => `<div class="item"><div class="t">${srcName(n)}</div><div class="row">
      <input class="input q-lmax" data-n="${n}" type="number" min="0" max="100" value="${(lim[n] || {}).max || 0}" style="width:3.9rem" aria-label="${tt('max', 'höchstens')}" title="${tt('Highest %', 'Höchstens %')}">
      <input class="input q-lst" data-n="${n}" type="number" min="0" max="100" value="${(lim[n] || {}).start || 0}" style="width:3.9rem" aria-label="${tt('start', 'Start')}" title="${tt('Start %', 'Start %')}">
      <input class="input q-trim" data-n="${n}" type="number" min="0" max="20" value="${(lim[n] || {}).trimDB || 0}" style="width:3.9rem" aria-label="${tt('trim dB', 'Ausgleich dB')}" title="${tt('Quieter by dB', 'Leiser um dB')}"></div></div>`).join('')}</div>
    <p class="small mono">${T('highest % · start % · quieter by dB', 'höchstens % · Start % · leiser um dB')}</p>
    <div class="row"><button class="btn btn-accent btn-sm" id="q-save">${ico('save')}${T('Save', 'Speichern')}</button></div></div>
  ${can('ring') ? `<div class="card" data-tier="cyan"><h3>${T('Light ring', 'Leuchtring')}</h3>
    <p>${T('The ring can follow the music of all receivers or glow as a lamp. Volume knob, mute, alarm, timer and buttons keep showing their own animations.', 'Der Ring kann der Musik aller Empfänger folgen oder als Lampe leuchten. Drehrad, Stumm, Wecker, Timer und Tasten zeigen weiter ihre eigenen Animationen.')} <span id="v-st">${vizHTML()}</span></p>
    <div class="field"><span class="field-label">${T('Display', 'Anzeige')}</span>${seg('v-mode', [['off', 'Off', 'Aus'], ['spectrum', 'Spectrum', 'Spektrum'], ['level', 'Level', 'Pegel'], ['pulse', 'Pulse', 'Puls'], ['static', 'Lamp', 'Lampe']], v.mode)}</div>
    <div class="field"><span class="field-label">${T('Colour', 'Farbe')}</span>${seg('v-col', [['rainbow', 'Rainbow', 'Regenbogen'], ['white', 'White', 'Weiß'], ['warm', 'Warm', 'Warm'], ['blue', 'Blue', 'Blau'], ['green', 'Green', 'Grün'], ['red', 'Red', 'Rot'], ['purple', 'Purple', 'Lila'], ['custom', 'Own', 'Eigene']], v.color)}</div>
    <div class="alarm"><div class="field"><label for="v-rgb">${T('Own colour', 'Eigene Farbe')}</label><input class="input" id="v-rgb" type="color" value="${rgbHex}" style="height:2.4rem;padding:.2rem"></div>
      <div class="range field"><label for="v-bri">${T('Brightness %', 'Helligkeit %')}</label>
        <div class="range-row"><input id="v-bri" type="range" min="5" max="100" step="5" value="${v.brightness}"><output class="range-out" id="v-bri-o">${v.brightness}</output></div></div>
      ${fld('v-rot', 'Start LED (turns the display)', 'Start-LED (dreht die Anzeige)', v.rotate, 'type="number" min="0" max="11"')}
      <div class="wide">${sw('v-timer', v.timerRing, 'Show the remaining time of a timer on the ring', 'Restzeit eines Timers auf dem Ring zeigen')}</div></div>
    <div class="row"><button class="btn btn-accent btn-sm" id="v-save">${ico('save')}${T('Save', 'Speichern')}</button><button class="btn btn-outline btn-sm" id="l-test">${ico('test')}${T('Test ring', 'Ring testen')}</button></div></div>` : ''}
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
      : `<p>${T('Updates from here need the release signing key (UPDATE_PUBKEY in the config file, see README). Until then update with ' + (can('vendorVolume') ? './install.sh.' : 'setup.sh from a newer package.'), 'Updates von hier brauchen den Signaturschlüssel der Releases (UPDATE_PUBKEY in der Datei config, siehe README). Bis dahin ' + (can('vendorVolume') ? 'mit ./install.sh aktualisieren.' : 'mit setup.sh aus einem neueren Paket aktualisieren.'))}</p>`}
    <div class="row"><button class="btn btn-outline btn-sm" id="u-back">${ico('restart')}${T('Roll back last update', 'Letztes Update zurücknehmen')}</button></div></div>
  </div>
  <div class="card" style="margin-top:1.2rem" id="logcard"><h3 id="log-t">${T('Log', 'Protokoll')}</h3><div class="log" id="logbox">${tt('Choose a service above.', 'Wähle oben einen Dienst.')}</div></div>`;
}
// ---------------------------------------------------------------- Raumkorrektur
let roomCurves = [], roomNoise = null, roomSuggest = null, roomBusy = false;
const eqBody = () => { const e = CFG.settings.eq || {}; return { bass: e.bass || 0, treble: e.treble || 0, loudness: !!e.loudness, night: !!e.night, roomOn: !!e.roomOn, room: e.room || [] }; };
function roomBandsHTML(bands) {
  return bands.map((b, i) => `<div class="item" data-rb="${i}"><div class="row">
    <input class="input rb-hz" type="number" min="20" max="20000" step="1" value="${b.hz}" style="width:4.9rem" aria-label="Hz" title="Hz"><span class="small mono">Hz</span>
    <input class="input rb-db" type="number" min="-15" max="6" step="0.1" value="${b.db}" style="width:4.4rem" aria-label="dB" title="dB"><span class="small mono">dB</span>
    <input class="input rb-q" type="number" min="0.3" max="10" step="0.1" value="${b.q}" style="width:3.9rem" aria-label="Q" title="${tt('Q (width: higher = narrower)', 'Güte (Breite: höher = schmaler)')}"><span class="small mono">Q</span></div>
    <button class="btn btn-outline btn-sm" data-rbdel="${i}" aria-label="${tt('Delete', 'Löschen')}">${ico('trash')}</button></div>`).join('') || `<p class="small">${T('No filters.', 'Keine Filter.')}</p>`;
}
function roomHTML() {
  const e = CFG.settings.eq || {};
  const bands = draft.room || (draft.room = JSON.parse(JSON.stringify(e.room || [])));
  return `<div class="card" data-tier="yellow"><h3>${T('Room correction', 'Raumkorrektur')}</h3>
    <p>${T('Rooms boost single bass notes (room modes): they boom. Measure with your phone at the listening place; the speaker plays pink noise and the page suggests filters that only cut.', 'Räume verstärken einzelne Basstöne (Raummoden): es dröhnt. Miss mit dem Handy am Hörplatz; der Lautsprecher spielt rosa Rauschen, die Seite schlägt Filter vor, die nur absenken.')}</p>
    ${window.isSecureContext ? '' : `<div class="callout callout-warn">${T('Measuring needs the microphone, and browsers allow it only over HTTPS: set WEB_TLS="on" (see README). Filters can still be entered by hand.', 'Messen braucht das Mikrofon, und Browser geben es nur über HTTPS frei: WEB_TLS="on" setzen (siehe README). Filter lassen sich auch von Hand eintragen.')}</div>`}
    ${sw('rq-on', e.roomOn, 'Room correction on', 'Raumkorrektur an')}
    <div class="list" id="rq-bands">${roomBandsHTML(bands)}</div>
    <div class="row"><button class="btn btn-outline btn-sm" id="rq-add" ${bands.length >= 6 ? 'disabled' : ''}>${ico('plus')}${T('Add filter', 'Filter hinzufügen')}</button>
      <button class="btn btn-accent btn-sm" id="rq-save">${ico('save')}${T('Save', 'Speichern')}</button></div>
    <p class="small">${T('1. Usual listening volume. 2. Phone at ear height at your seat, room quiet. 3. Measure; 2 or 3 positions make it more reliable.', '1. Übliche Lautstärke. 2. Handy in Ohrhöhe am Sitzplatz, Raum ruhig. 3. Messen; 2 oder 3 Positionen machen es verlässlicher.')}</p>
    <div class="row"><button class="btn btn-outline btn-sm" id="rq-measure" ${window.isSecureContext && navigator.mediaDevices ? '' : 'disabled'}>${ico('wave')}${T('Measure position', 'Position messen')} ${roomCurves.length + 1}</button>
      ${roomCurves.length ? `<button class="btn btn-outline btn-sm" id="rq-reset">${T('Discard measurements', 'Messungen verwerfen')}</button>` : ''}</div>
    <div id="rq-result">${roomResultHTML()}</div></div>`;
}
function chartSVG(lines) {
  const W = 600, H = 200, x = f => Math.log10(f / 20) / 3 * W, y = d => H / 2 - d * (H / 2) / 15;
  const grid = [50, 100, 200, 500, 1000, 2000, 5000, 10000].map(f => `<line class="chart-grid" x1="${x(f)}" x2="${x(f)}" y1="0" y2="${H}"/><text class="chart-label" x="${x(f) + 3}" y="${H - 4}">${f >= 1000 ? f / 1000 + 'k' : f}</text>`).join('')
    + [-10, 10].map(d => `<line class="chart-grid" x1="0" x2="${W}" y1="${y(d)}" y2="${y(d)}"/><text class="chart-label" x="3" y="${y(d) - 3}">${d > 0 ? '+' : ''}${d} dB</text>`).join('');
  const pl = (pts, cls) => `<polyline class="${cls}" points="${pts.filter(p => p.hz >= 20 && p.hz <= 20000).map(p => `${x(p.hz).toFixed(1)},${y(Math.max(-15, Math.min(15, p.db))).toFixed(1)}`).join(' ')}"/>`;
  return `<svg class="chart" viewBox="0 0 ${W} ${H}" role="img" aria-label="${tt('Frequency response', 'Frequenzgang')}"><line class="chart-zero" x1="0" x2="${W}" y1="${y(0)}" y2="${y(0)}"/>${grid}${lines.map(l => pl(l[0], l[1])).join('')}</svg>`;
}
function roomResultHTML() {
  if (!roomSuggest) return '';
  const s = roomSuggest;
  return `${chartSVG([[s.before, 'chart-before'], [s.after, 'chart-after']])}
    <p class="small"><span class="chart-key chart-before"></span>${T('measured', 'gemessen')} (${roomCurves.length} ${tt('position(s)', 'Position(en)')}) <span class="chart-key chart-after"></span>${T('with the suggested filters', 'mit den vorgeschlagenen Filtern')}</p>
    ${s.warn ? `<div class="callout callout-warn">${s.warn}</div>` : ''}
    <p class="mono small">${s.bands.length ? s.bands.map(b => `${b.hz} Hz ${b.db} dB Q ${b.q}`).join(' · ') : T('No boom found - nothing to correct.', 'Kein Dröhnen gefunden - nichts zu korrigieren.')}</p>
    ${s.bands.length ? `<div class="row"><button class="btn btn-accent btn-sm" id="rq-apply">${T('Use these filters', 'Diese Filter übernehmen')}</button></div>` : ''}`;
}
function collectRoom() {
  draft.room = $$('#rq-bands [data-rb]').map(el => ({ hz: +$('.rb-hz', el).value || 100, db: +$('.rb-db', el).value || 0, q: +$('.rb-q', el).value || 1 }));
  return draft.room;
}
// Mikrofon aufnehmen: mittlere Leistung je FFT-Bin über secs Sekunden
async function capturePower(an, secs) {
  const n = an.frequencyBinCount, acc = new Float64Array(n), buf = new Float32Array(n);
  let k = 0;
  const end = performance.now() + secs * 1000;
  while (performance.now() < end) {
    await new Promise(r => setTimeout(r, 120));
    an.getFloatFrequencyData(buf);
    for (let i = 0; i < n; i++) acc[i] += Math.pow(10, buf[i] / 10);
    k++;
  }
  for (let i = 0; i < n; i++) acc[i] /= Math.max(1, k);
  return acc;
}
async function measureRoom() {
  if (roomBusy) return;
  roomBusy = true;
  const btn = $('#rq-measure'), say = m => { if (btn) btn.textContent = m; };
  let stream = null, ctx = null;
  try {
    stream = await navigator.mediaDevices.getUserMedia({ audio: { echoCancellation: false, noiseSuppression: false, autoGainControl: false } });
    ctx = new (window.AudioContext || window.webkitAudioContext)();
    const an = ctx.createAnalyser(); an.fftSize = 32768; an.smoothingTimeConstant = 0;
    ctx.createMediaStreamSource(stream).connect(an);
    const binHz = ctx.sampleRate / an.fftSize;
    say(tt('Quiet please …', 'Bitte Ruhe …'));
    const noise = RoomEQ.smooth(binHz, await capturePower(an, 2));
    await api('/api/measure/start', 'POST', { seconds: 12 });
    say(tt('Measuring …', 'Misst …'));
    await new Promise(r => setTimeout(r, 1500));
    const curve = RoomEQ.smooth(binHz, await capturePower(an, 8));
    await api('/api/measure/stop', 'POST', {});
    const snr = RoomEQ.snr(curve, noise);
    roomCurves.push(curve); roomNoise = noise;
    roomSuggest = RoomEQ.suggest(RoomEQ.average(roomCurves));
    roomSuggest.warn = snr < 10 ? tt(`Only ${snr.toFixed(0)} dB above the room noise: turn the speaker up and measure again.`, `Nur ${snr.toFixed(0)} dB über dem Raumgeräusch: lauter stellen und neu messen.`) : '';
  } catch (e) {
    api('/api/measure/stop', 'POST', {}).catch(() => {});
    toast(e.message || String(e), 'error');
  } finally {
    if (stream) stream.getTracks().forEach(t => t.stop());
    if (ctx) ctx.close();
    roomBusy = false;
    collectRoomSafe(); render();
  }
}
const collectRoomSafe = () => { if ($('#rq-bands')) collectRoom(); };
function bindRoom() {
  const saveRoom = async () => { collectRoom(); await api('/api/settings/eq', 'PUT', { ...eqBody(), roomOn: swVal('rq-on'), room: draft.room }); delete draft.room; await loadCfg(); };
  $('#rq-add').onclick = () => { collectRoom(); draft.room.push({ hz: 100, db: -3, q: 4 }); dirty = true; render(); };
  $$('[data-rbdel]').forEach(b => b.onclick = () => { collectRoom(); draft.room.splice(+b.dataset.rbdel, 1); dirty = true; render(); });
  $('#rq-save').onclick = () => act(saveRoom, tt('Saved', 'Gespeichert'));
  $('#rq-measure').onclick = () => measureRoom();
  const r = $('#rq-reset'); if (r) r.onclick = () => { roomCurves = []; roomSuggest = null; render(); };
  const ap = $('#rq-apply'); if (ap) ap.onclick = () => act(async () => { draft.room = roomSuggest.bands.slice(0, 6); $('#rq-bands').innerHTML = roomBandsHTML(draft.room); $('#rq-on').setAttribute('aria-checked', 'true'); await saveRoom(); }, tt('Room correction on', 'Raumkorrektur an'));
}

// ---------------------------------------------------------------- Klänge
let soundsData = null, soundsBusy = false;
const TONES = { alarm: ['Alarm', 'Wecker'], timer: ['Timer', 'Timer'], chime: ['Chime (also briefing)', 'Gong (auch Briefing)'], bell: ['Door bell', 'Türklingel'], beep: ['Beep', 'Piep'] };
function soundRowHTML(target, title, sub, replaced, canOriginal) {
  const t = esc(target);
  return `<div class="item"><div><div class="t">${title}${replaced ? ` <span class="pill" data-state="applied">${T('own', 'eigener')}</span>` : ''}</div><div class="s">${sub}</div></div>
    <div class="row"><button class="btn btn-outline btn-sm" data-snplay="${t}" aria-label="${tt('Play on the speaker', 'Auf dem Lautsprecher abspielen')}" title="${tt('Play on the speaker', 'Auf dem Lautsprecher abspielen')}">${ico('play')}</button>
      <label class="btn btn-outline btn-sm" title="${tt('Replace with a file (WAV, MP3, OGG, FLAC)', 'Durch eine Datei ersetzen (WAV, MP3, OGG, FLAC)')}">${ico('up')}${T('Replace', 'Ersetzen')}<input type="file" accept="audio/*,.wav,.mp3,.ogg,.flac" data-snup="${t}" hidden></label>
      ${replaced && canOriginal ? `<button class="btn btn-outline btn-sm" data-snreset="${t}">${T('Original', 'Original')}</button>` : ''}</div></div>`;
}
function soundsHTML() {
  const d = soundsData;
  return `<div class="card wide-card"><h3>${T('Sounds', 'Klänge')}</h3>
    <p>${!can('vendorSounds') ? T('Replace the tones of Leuchtfeuer (alarm, timer, chime …) with your own files. Uploads are converted to the format of the original; "Original" brings it back.', 'Die Töne von Leuchtfeuer (Wecker, Timer, Gong …) durch eigene Dateien ersetzen. Hochgeladenes wird ins Format des Originals gewandelt; „Original“ stellt es wieder her.') : T('Replace the speaker\'s own sounds (start, error, pairing … of the vendor software) and the tones of Leuchtfeuer with your own files. Uploads are converted to the format of the original; "Original" brings it back.', 'Die Klänge des Lautsprechers (Start, Fehler, Kopplung … der Hersteller-Software) und die Töne von Leuchtfeuer durch eigene Dateien ersetzen. Hochgeladenes wird ins Format des Originals gewandelt; „Original“ stellt es wieder her.')}</p>
    <h4 class="mono muted small">${T('LEUCHTFEUER TONES', 'TÖNE VON LEUCHTFEUER')}</h4>
    <div class="list">${d ? d.tones.map(x => soundRowHTML('tone:' + x.id, T(...TONES[x.id]), x.replaced ? `${x.seconds} s` : T('built in', 'eingebaut'), x.replaced, true)).join('') : `<p>${T('Loading …', 'Lädt …')}</p>`}</div>
    ${can('vendorSounds') ? `<h4 class="mono muted small">${T('SOUNDS OF THE SPEAKER', 'KLÄNGE DES LAUTSPRECHERS')}</h4>
    <p class="small">${T('Found on the speaker (WAV). Which one is the start or the error sound: listen. They take effect when the vendor software plays them next; at the latest after a restart.', 'Auf dem Lautsprecher gefunden (WAV). Welcher der Start- oder Fehlerton ist: anhören. Sie gelten, sobald die Hersteller-Software sie das nächste Mal spielt, spätestens nach einem Neustart.')}</p>
    <div class="list" id="sn-vendor">${d ? (d.vendor.map(v => soundRowHTML('vendor:' + v.path, esc(v.name), `${esc(v.path)} · ${v.format.rate} Hz · ${v.format.channels === 1 ? tt('mono', 'mono') : tt('stereo', 'stereo')} · ${v.seconds} s${v.replaced && !v.mounted ? ' · ' + tt('not mounted yet', 'noch nicht eingehängt') : ''}`, v.replaced, true)).join('') || `<p class="small">${T('No WAV sounds found.', 'Keine WAV-Klänge gefunden.')}</p>`) : ''}</div>
    <div class="row"><button class="btn btn-outline btn-sm" id="sn-scan">${ico('search')}${T('Search again', 'Neu suchen')}</button></div>` : ''}</div>`;
}
async function loadSounds(rescan) {
  try { soundsData = await api('/api/sounds' + (rescan ? '?rescan=1' : '')); } catch (e) { toast(e.message, 'error'); return; }
  if (route === 'settings' && !inFormFocus()) render();
}
function bindSounds() {
  if (!soundsData) loadSounds(false);
  $$('[data-snplay]').forEach(b => b.onclick = () => act(() => api('/api/sounds/play', 'POST', { target: b.dataset.snplay })));
  $$('[data-snreset]').forEach(b => b.onclick = () => act(async () => { await api('/api/sounds/reset', 'POST', { target: b.dataset.snreset }); await loadSounds(false); }, tt('Original restored', 'Original wiederhergestellt')));
  $$('[data-snup]').forEach(inp => inp.onchange = async () => {
    const f = inp.files[0]; if (!f || soundsBusy) return;
    soundsBusy = true;
    try {
      const r = await fetch('/api/sounds/upload?target=' + encodeURIComponent(inp.dataset.snup), { method: 'POST', headers: { 'Content-Type': f.type || 'application/octet-stream' }, body: f });
      const j = await r.json().catch(() => ({}));
      if (!r.ok) throw new Error(j.error || r.statusText);
      toast(tt('Replaced', 'Ersetzt'), 'ok'); await loadSounds(false);
    } catch (e) { toast(e.message, 'error'); }
    soundsBusy = false;
  });
  if ($('#sn-scan')) $('#sn-scan').onclick = () => act(() => loadSounds(true));
}

function svcListHTML() {
  const st = Object.fromEntries((S.sys.services || []).map(v => [v.name, v]));
  return [...CFG.services.map(d => d.name), 'hook'].map(n => {
    const v = st[n] || {};
    const label = n === 'hook' ? T('Supervisor (hook)', 'Überwacher (Hook)') : esc(v.title || n);
    const state = n === 'hook' ? '' : !v.enabled ? T('off', 'aus') : v.missing ? T(`not installed (needs ${v.requires})`, `nicht installiert (braucht ${v.requires})`) : v.failing ? T(`keeps failing (${v.fails}×), retry in ${v.waitSecs} s`, `fällt aus (${v.fails}×), neuer Versuch in ${v.waitSecs} s`) : v.running ? T('running', 'läuft') + (v.restarts ? ` · ${v.restarts}× ${tt('restarted', 'neu gestartet')}` : '') : T('not running', 'läuft nicht');
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
  const rng = (id) => { $('#' + id).oninput = () => { $('#' + id + '-o').textContent = $('#' + id).value; }; };
  rng('e-bass'); rng('e-treb');
  if (can('ring')) {
    $('#l-test').onclick = () => act(() => api('/api/led/test', 'POST', {}));
    rng('v-bri');
    $('#v-rgb').oninput = () => $$('#v-col button').forEach(x => x.setAttribute('aria-pressed', x.dataset.v === 'custom'));
    $('#v-save').onclick = () => act(async () => {
      const h = $('#v-rgb').value, rgb = [1, 3, 5].map(i => parseInt(h.slice(i, i + 2), 16));
      await api('/api/settings/viz', 'PUT', { mode: segVal('v-mode'), color: segVal('v-col'), rgb, brightness: +$('#v-bri').value, rotate: +$('#v-rot').value || 0, timerRing: swVal('v-timer') }); await loadCfg();
    }, tt('Saved', 'Gespeichert'));
  }
  $('#e-save').onclick = () => act(async () => { await api('/api/settings/eq', 'PUT', { ...eqBody(), bass: +$('#e-bass').value, treble: +$('#e-treb').value, loudness: swVal('e-loud'), night: swVal('e-night') }); await loadCfg(); }, tt('Saved', 'Gespeichert'));
  $('#q-save').onclick = () => act(async () => {
    const limits = {};
    CFG.sourceNames.forEach(n => { limits[n] = { max: +$(`.q-lmax[data-n="${n}"]`).value || 0, start: +$(`.q-lst[data-n="${n}"]`).value || 0, trimDB: +$(`.q-trim[data-n="${n}"]`).value || 0 }; });
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
  bindRoom();
  bindSounds();
}

// ---------------------------------------------------------------- Geräte (andere Leuchtfeuer)
let peersData = null, peersAt = 0;
const SECTIONS = { radio: ['Stations', 'Sender'], alarms: ['Alarms', 'Wecker'], buttons: ['Buttons', 'Tasten'], viz: ['Light ring', 'Leuchtring'], sources: ['Sources', 'Quellen'], eq: ['Sound', 'Klang'], holidays: ['Holidays', 'Feiertage'], timezone: ['Time zone', 'Zeitzone'], briefing: ['Briefing', 'Briefing'], homeAssistant: ['Home Assistant (API)', 'Home Assistant (API)'], mqtt: ['MQTT', 'MQTT'], syslog: ['Syslog', 'Syslog'], wifi: ['Wi-Fi guard', 'WLAN-Wächter'] };
function peersHTML() {
  if (!peersData) return `<p>${T('Loading …', 'Lädt …')}</p>`;
  return peersData.peers.map((p, i) => `<div class="item"><div><div class="t">${esc(p.name)} <span class="pill" data-state="${p.online ? 'applied' : 'pending'}">${p.online ? T('online', 'erreichbar') : T('offline', 'nicht erreichbar')}</span></div>
      <div class="s">${esc(p.url)}${p.online ? ` · ${esc(p.version)} · ${p.muted ? tt('muted', 'stumm') : p.volume + ' %'} · ${p.tempC} °C${p.playing ? ' · ' + esc(p.playing) : ''}` : p.error ? ' · ' + esc(p.error) : ''}</div></div>
    <div class="row"><button class="btn btn-outline btn-sm" data-pstop="${i}" ${p.online ? '' : 'disabled'} aria-label="${tt('Stop everything', 'Alles stoppen')}">${ico('stop')}</button>
      <button class="btn btn-outline btn-sm" data-pupd="${i}" ${p.online ? '' : 'disabled'}>${ico('down')}${T('Update', 'Update')}</button>
      <button class="btn btn-outline btn-sm" data-pdel="${i}" aria-label="${tt('Remove', 'Entfernen')}">${ico('trash')}</button></div></div>`).join('') || `<p>${T('No other speaker added yet.', 'Noch kein anderer Lautsprecher eingetragen.')}</p>`;
}
function viewDevices() {
  const found = peersData ? peersData.found.filter(f => !f.known) : [];
  return `<div class="grid">
  <div class="card" data-tier="yellow"><h3>${T('Speakers', 'Lautsprecher')}</h3>
    <p>${T('Other Leuchtfeuer speakers in the network: state at a glance, copy settings, start updates. Each one is added with one of its API keys (there: System > Access, scope "full").', 'Andere Leuchtfeuer im Netz: Zustand auf einen Blick, Einstellungen kopieren, Updates anstoßen. Eingetragen wird jedes mit einem seiner API-Schlüssel (dort: System > Zugang, Umfang „voll“).')}</p>
    <div class="list" id="pe-list">${peersHTML()}</div>
    <div class="row"><button class="btn btn-outline btn-sm" id="pe-upd-all" ${peersData && peersData.peers.some(p => p.online) ? '' : 'disabled'}>${ico('down')}${T('Update all', 'Alle aktualisieren')}</button></div></div>
  <div class="card" data-tier="cyan"><h3>${T('Copy settings', 'Einstellungen kopieren')}</h3>
    <p>${T('Copies the chosen sections of this speaker to the others (device name, password and voice assistant stay).', 'Kopiert die gewählten Bereiche dieses Lautsprechers auf die anderen (Gerätename, Passwort und Sprachassistent bleiben).')}</p>
    <div class="chips">${(CFG.copySections || []).map(k => `<button type="button" class="chip is-filter pe-sec" data-s="${k}" aria-pressed="${['radio', 'alarms', 'eq'].includes(k)}">${T(...(SECTIONS[k] || [k, k]))}</button>`).join('')}</div>
    <div class="field"><label for="pe-target">${T('To', 'Nach')}</label><select class="select" id="pe-target"><option value="*">${tt('all online speakers', 'alle erreichbaren Lautsprecher')}</option>${peersData ? peersData.peers.map(p => `<option value="${esc(p.url)}">${esc(p.name)}</option>`).join('') : ''}</select></div>
    <div class="row"><button class="btn btn-accent btn-sm" id="pe-copy">${ico('copy')}${T('Copy', 'Kopieren')}</button></div></div>
  <div class="card"><h3>${T('Add speaker', 'Lautsprecher eintragen')}</h3>
    ${found.length ? `<p>${T('Found in the network:', 'Im Netz gefunden:')}</p><div class="list">${found.map(f => `<div class="item"><div><div class="t">${esc(f.name)}</div><div class="s">${esc(f.url)} · ${esc(f.version)}</div></div><button class="btn btn-outline btn-sm" data-pfill="${esc(f.url)}" data-pname="${esc(f.name)}">${T('Use', 'Übernehmen')}</button></div>`).join('')}</div>` : `<p class="small">${T('None found automatically (the router may not pass mDNS on). Enter the address by hand.', 'Keiner automatisch gefunden (der Router reicht mDNS evtl. nicht weiter). Adresse von Hand eintragen.')}</p>`}
    ${fld('pe-url', 'Address', 'Adresse', '', 'placeholder="http://invoke-kueche.lan"')}${fld('pe-name', 'Name (optional)', 'Name (optional)', '')}
    <div class="field"><label for="pe-tok">${T('API key of that speaker', 'API-Schlüssel dieses Lautsprechers')}</label><input class="input" id="pe-tok" type="password" autocomplete="off" placeholder="lf_…"></div>
    <div class="row"><button class="btn btn-accent btn-sm" id="pe-add">${ico('plus')}${T('Add', 'Eintragen')}</button></div></div></div>`;
}
async function loadPeers(force) {
  if (!force && peersData && Date.now() - peersAt < 15000) return;
  try { peersData = await api('/api/peers'); peersAt = Date.now(); } catch (e) { toast(e.message, 'error'); peersData = { peers: [], found: [] }; }
  if (route === 'devices' && !inFormFocus()) render();
}
function bindDevices() {
  if (!peersData) loadPeers(true);
  $$('.pe-sec').forEach(b => b.onclick = () => b.setAttribute('aria-pressed', b.getAttribute('aria-pressed') !== 'true'));
  const P = () => (peersData ? peersData.peers : []);
  $$('[data-pstop]').forEach(b => b.onclick = () => act(() => api('/api/peers/action', 'POST', { url: P()[+b.dataset.pstop].url, action: 'stop_all' })));
  $$('[data-pupd]').forEach(b => b.onclick = () => { if (confirm(tt('Start the update on this speaker?', 'Update auf diesem Lautsprecher starten?'))) act(() => api('/api/peers/update', 'POST', { url: P()[+b.dataset.pupd].url }), tt('Update started', 'Update gestartet')); });
  $$('[data-pdel]').forEach(b => b.onclick = () => act(async () => { await api('/api/peers/delete', 'POST', { url: P()[+b.dataset.pdel].url }); await loadPeers(true); }));
  $('#pe-upd-all').onclick = () => { if (confirm(tt('Start the update on all reachable speakers?', 'Update auf allen erreichbaren Lautsprechern starten?'))) act(async () => { for (const p of P().filter(x => x.online)) await api('/api/peers/update', 'POST', { url: p.url }); }, tt('Updates started', 'Updates gestartet')); };
  $('#pe-copy').onclick = () => {
    const sections = $$('.pe-sec[aria-pressed="true"]').map(b => b.dataset.s), to = $('#pe-target').value;
    if (!sections.length) { toast(tt('Choose at least one section', 'Mindestens einen Bereich wählen'), 'error'); return; }
    const targets = to === '*' ? P().filter(p => p.online).map(p => p.url) : [to];
    act(async () => { for (const u of targets) await api('/api/peers/copy', 'POST', { url: u, sections }); }, tt('Copied', 'Kopiert'));
  };
  $$('[data-pfill]').forEach(b => b.onclick = () => { $('#pe-url').value = b.dataset.pfill; $('#pe-name').value = b.dataset.pname; $('#pe-tok').focus(); });
  $('#pe-add').onclick = () => act(async () => { await api('/api/peers/add', 'POST', { url: $('#pe-url').value.trim(), name: $('#pe-name').value.trim(), token: $('#pe-tok').value.trim() }); await loadPeers(true); }, tt('Added', 'Eingetragen'));
}

// ---------------------------------------------------------------- System
let tokensList = null, sshKeys = null, newToken = '', logES = null, logLines = [], logSvc = '', logFilter = '', logNames = [];
function viewSystem() {
  const sl = CFG.settings.syslog || {};
  return `<div class="grid">
  <div class="card" data-tier="yellow"><h3>${T('API keys', 'API-Schlüssel')}</h3>
    <p>${T('For Home Assistant, scripts, other speakers and Prometheus: header "Authorization: Bearer <key>". "read" may only read (status, metrics). Keys cannot manage access. See docs/API.md.', 'Für Home Assistant, Skripte, andere Lautsprecher und Prometheus: Kopfzeile „Authorization: Bearer <Schlüssel>“. „lesen“ darf nur lesen (Status, Metriken). Schlüssel können den Zugang nicht verwalten. Siehe docs/API.md.')}</p>
    ${newToken ? `<div class="callout"><b>${T('New key - shown only now:', 'Neuer Schlüssel - nur jetzt sichtbar:')}</b><div class="log" id="tk-new">${esc(newToken)}</div><div class="row"><button class="btn btn-outline btn-sm" id="tk-copy">${ico('copy')}${T('Copy', 'Kopieren')}</button></div></div>` : ''}
    <div class="list" id="tk-list">${tokensList ? tokensList.map(k => `<div class="item"><div><div class="t">${esc(k.name)} <span class="pill" data-state="${k.scope === 'full' ? 'pending' : 'applied'}">${k.scope === 'full' ? T('full', 'voll') : T('read', 'lesen')}</span></div>
      <div class="s">${T('created', 'angelegt')} ${new Date(k.created).toLocaleDateString()} · ${k.lastUsed && k.lastUsed > '0001-01-02' ? T('last used', 'zuletzt benutzt') + ' ' + new Date(k.lastUsed).toLocaleString() : T('never used', 'nie benutzt')}</div></div>
      <button class="btn btn-outline btn-sm" data-tkdel="${esc(k.id)}" aria-label="${tt('Delete', 'Löschen')}">${ico('trash')}</button></div>`).join('') || `<p class="small">${T('No keys.', 'Keine Schlüssel.')}</p>` : `<p>${T('Loading …', 'Lädt …')}</p>`}</div>
    <div class="row"><input class="input" id="tk-name" placeholder="${tt('Name, e.g. Home Assistant', 'Name, z. B. Home Assistant')}" style="flex:1;min-width:9rem">
      ${seg('tk-scope', [['full', 'full', 'voll'], ['read', 'read', 'lesen']], 'full')}
      <button class="btn btn-accent btn-sm" id="tk-add">${ico('key')}${T('Create', 'Anlegen')}</button></div></div>
  <div class="card" data-tier="yellow"><h3>${T('SSH keys', 'SSH-Schlüssel')}</h3>
    <p>${T('Public keys that may log in as root (ed25519, ECDSA or RSA). Active within 30 seconds. The last key stays, so access is never lost.', 'Öffentliche Schlüssel, die sich als root anmelden dürfen (ed25519, ECDSA oder RSA). Gilt binnen 30 Sekunden. Der letzte Schlüssel bleibt, damit der Zugang nie verloren geht.')}</p>
    <div class="list" id="ssh-list">${sshKeys ? sshKeys.map(k => `<div class="item"><div><div class="t">${esc(k.comment || k.type)}</div><div class="s">${esc(k.type)} · ${esc(k.fingerprint)}</div></div>
      <button class="btn btn-outline btn-sm" data-sshdel="${esc(k.fingerprint)}" ${sshKeys.length > 1 ? '' : 'disabled'} aria-label="${tt('Delete', 'Löschen')}">${ico('trash')}</button></div>`).join('') : `<p>${T('Loading …', 'Lädt …')}</p>`}</div>
    <div class="field"><label for="ssh-new">${T('Add public key (one line from .pub)', 'Öffentlichen Schlüssel hinzufügen (eine Zeile aus der .pub)')}</label><textarea class="input mono" id="ssh-new" rows="2" placeholder="ssh-ed25519 AAAA… name@rechner"></textarea></div>
    <div class="row"><button class="btn btn-accent btn-sm" id="ssh-add">${ico('plus')}${T('Add', 'Hinzufügen')}</button></div></div>
  <div class="card"><h3>${T('Web password', 'Web-Passwort')}</h3>
    <p>${T('At least 6 characters. Stored only as a salted hash. Changing it signs out every browser.', 'Mindestens 6 Zeichen. Wird nur als gesalzener Hash gespeichert. Ein Wechsel meldet alle Browser ab.')}</p>
    <div class="field"><label for="pw">${T('New password', 'Neues Passwort')}</label><input class="input" id="pw" type="password" autocomplete="new-password"></div>
    <div class="row"><button class="btn btn-accent btn-sm" id="pw-save">${ico('save')}${T('Change', 'Ändern')}</button><button class="btn btn-outline btn-sm" id="logout">${T('Sign out', 'Abmelden')}</button></div></div>
  <div class="card" data-tier="cyan"><h3>${T('Monitoring', 'Überwachung')}</h3>
    <p>${T('Prometheus metrics at /metrics (temperature, Wi-Fi, CPU, services, restarts, audio dropouts, volume …), with a "read" key:', 'Prometheus-Metriken unter /metrics (Temperatur, WLAN, CPU, Dienste, Neustarts, Tonaussetzer, Lautstärke …), mit einem Schlüssel „lesen“:')}</p>
    <div class="log">scrape_configs:
  - job_name: leuchtfeuer
    authorization: { credentials: lf_… }
    static_configs: [{ targets: ["${esc(location.host)}"] }]</div>
    <p>${T('Send all logs to a syslog server (RFC 5424):', 'Alle Protokolle an einen Syslog-Server schicken (RFC 5424):')}</p>
    ${sw('sl-en', sl.enabled, 'Enabled', 'Aktiv')}
    <div class="alarm">${fld('sl-host', 'Server', 'Server', sl.host || '', 'placeholder="192.168.1.10"')}${fld('sl-port', 'Port', 'Port', sl.port || 514, 'type="number" min="1" max="65535"')}</div>
    ${seg('sl-proto', [['udp', 'UDP', 'UDP'], ['tcp', 'TCP', 'TCP']], sl.proto || 'udp')}
    <div class="row"><button class="btn btn-accent btn-sm" id="sl-save">${ico('save')}${T('Save', 'Speichern')}</button></div></div></div>
  <div class="card" style="margin-top:1.2rem"><h3>${T('Live log', 'Protokoll live')}</h3>
    <div class="row"><select class="select" id="lg-svc" style="max-width:14rem"><option value="">${tt('All services', 'Alle Dienste')}</option>${logNames.map(n => `<option ${n === logSvc ? 'selected' : ''}>${esc(n)}</option>`).join('')}</select>
      <input class="input" id="lg-filter" placeholder="${tt('Filter', 'Filter')}" value="${esc(logFilter)}" style="flex:1;min-width:8rem">
      <button class="btn btn-outline btn-sm" id="lg-clear">${T('Clear', 'Leeren')}</button></div>
    <div class="log" id="lg-box" style="max-height:28rem">${logBoxHTML()}</div></div>`;
}
function logBoxHTML() {
  const f = logFilter.toLowerCase();
  return logLines.filter(l => !f || (l.service + ' ' + l.text).toLowerCase().includes(f)).slice(-400).map(l => `<span class="muted">${new Date(l.time).toLocaleTimeString()} ${esc(l.service)}</span> ${esc(l.text)}`).join('\n') || esc(tt('Waiting for log lines …', 'Warte auf Protokollzeilen …'));
}
function startLog() {
  stopLog(); logLines = [];
  if (!window.EventSource) return;
  logES = new EventSource('/api/logs/stream?service=' + encodeURIComponent(logSvc));
  let pending = false;
  logES.addEventListener('line', ev => {
    logLines.push(JSON.parse(ev.data)); if (logLines.length > 1000) logLines.splice(0, logLines.length - 1000);
    if (!pending) { pending = true; requestAnimationFrame(() => { pending = false; const b = $('#lg-box'); if (!b) return; const atEnd = b.scrollTop + b.clientHeight >= b.scrollHeight - 20; b.innerHTML = logBoxHTML(); if (atEnd) b.scrollTop = 1e9; }); }
  });
}
function stopLog() { if (logES) { logES.close(); logES = null; } }
async function loadSystem() {
  try { [tokensList, sshKeys, logNames] = await Promise.all([api('/api/tokens'), api('/api/ssh-keys'), api('/api/logs/names')]); } catch (e) { toast(e.message, 'error'); }
  if (route === 'system' && !inFormFocus()) { const box = $('#lg-box'), keep = box && box.scrollTop; render(); if (box && $('#lg-box')) $('#lg-box').scrollTop = keep; }
}
function bindSystem() {
  bindSw($('#view')); bindSeg($('#view'));
  if (!tokensList) loadSystem();
  if (!logES) startLog();
  $('#tk-add').onclick = () => act(async () => { const r = await api('/api/tokens', 'POST', { name: $('#tk-name').value, scope: segVal('tk-scope') }); newToken = r.token; await loadSystem(); });
  const cp = $('#tk-copy'); if (cp) cp.onclick = () => { navigator.clipboard ? navigator.clipboard.writeText(newToken).then(() => toast(tt('Copied', 'Kopiert'), 'ok')) : toast(newToken); };
  $$('[data-tkdel]').forEach(b => b.onclick = () => { if (confirm(tt('Delete this key? Programs using it lose access.', 'Diesen Schlüssel löschen? Programme damit verlieren den Zugang.'))) act(async () => { await api('/api/tokens/delete', 'POST', { id: b.dataset.tkdel }); newToken = ''; await loadSystem(); }); });
  $('#ssh-add').onclick = () => act(async () => { await api('/api/ssh-keys', 'POST', { key: $('#ssh-new').value }); $('#ssh-new').value = ''; await loadSystem(); }, tt('Key added, active within 30 s', 'Schlüssel eingetragen, gilt binnen 30 s'));
  $$('[data-sshdel]').forEach(b => b.onclick = () => { if (confirm(tt('Remove this SSH key?', 'Diesen SSH-Schlüssel entfernen?'))) act(async () => { await api('/api/ssh-keys/delete', 'POST', { fingerprint: b.dataset.sshdel }); await loadSystem(); }); });
  $('#pw-save').onclick = () => act(async () => { await api('/api/settings/device', 'PUT', { ...CFG.device, webPassword: $('#pw').value }); $('#pw').value = ''; }, tt('Password changed - sign in again', 'Passwort geändert - bitte neu anmelden'));
  $('#logout').onclick = async () => { await fetch('/api/logout', { method: 'POST' }); location.reload(); };
  $('#sl-save').onclick = () => act(async () => { await api('/api/settings/syslog', 'PUT', { enabled: swVal('sl-en'), host: $('#sl-host').value.trim(), port: +$('#sl-port').value || 514, proto: segVal('sl-proto') }); await loadCfg(); }, tt('Saved', 'Gespeichert'));
  $('#lg-svc').onchange = () => { logSvc = $('#lg-svc').value; startLog(); $('#lg-box').innerHTML = logBoxHTML(); };
  $('#lg-filter').oninput = () => { logFilter = $('#lg-filter').value; $('#lg-box').innerHTML = logBoxHTML(); };
  $('#lg-clear').onclick = () => { logLines = []; $('#lg-box').innerHTML = logBoxHTML(); };
  const b = $('#lg-box'); if (b) b.scrollTop = 1e9;
}

const VIEWS = {
  overview: [viewOverview, fillOverview], radio: [viewRadio, bindRadio], alarms: [viewAlarms, bindAlarms], briefing: [viewBriefing, bindBriefing],
  buttons: [viewButtons, bindButtons], network: [viewNetwork, bindNetwork], ha: [viewHA, bindHA], devices: [viewDevices, bindDevices],
  settings: [viewSettings, bindSettings], system: [viewSystem, bindSystem],
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
  const link = S.device ? S.device.link : 'audio-ui';
  $('#conn').innerHTML = !link ? esc(S.device.model || '') : S.wamp ? T('connected', 'verbunden') : T(`${link} not reachable`, `${link} nicht erreichbar`);
  $$('#tabs [data-r="buttons"]').forEach(b => b.classList.toggle('hidden', !can('buttons')));
  if (route === 'buttons' && !can('buttons')) location.hash = '#/';
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
    const v = $('#vo-st'); if (v) v.innerHTML = voiceHTML(false);
  } else if (route === 'radio') {
    const sig = JSON.stringify(S.player);
    if (sig !== playSig) { playSig = sig; if (!dirty && !inFormFocus()) { draft = {}; render(); } }
  } else if (route === 'buttons') {
    const e = $('#ev-list'); if (e) e.innerHTML = evListHTML();
  } else if (route === 'devices') {
    loadPeers(false);
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
$('#view').addEventListener('click', e => { if (e.target.closest('.switch, .seg button, .a-day, [data-del], [data-adel], [data-bdel], #r-add, #a-add, #b-add, [data-bdel2], [data-bmv], #bi-add, [data-mv], #rq-add, [data-rbdel]') && route !== 'overview' && route !== 'devices' && route !== 'system') dirty = true; });
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
  if (r !== 'system') stopLog();
  if (r !== 'system') newToken = '';
  route = VIEWS[r] ? r : 'overview'; draft = {}; lastSig = ''; dirty = false; playSig = JSON.stringify(S.player); render();
}
$$('#tabs button').forEach(b => b.addEventListener('click', () => { const h = '#/' + (b.dataset.r === 'overview' ? '' : b.dataset.r); if (location.hash !== h) location.hash = h; }));
window.addEventListener('hashchange', onHash);
new MutationObserver(() => render()).observe(document.documentElement, { attributes: true, attributeFilter: ['lang'] });
start();
