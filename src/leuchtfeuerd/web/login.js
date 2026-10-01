'use strict';
// Anmeldeseite (eigene Datei: die Sicherheitsrichtlinie der Oberfläche erlaubt keine eingebetteten Skripte).
const f = document.getElementById('f'), err = document.getElementById('err');
f.addEventListener('submit', async e => {
  e.preventDefault();
  err.hidden = true;
  const go = document.getElementById('go'); go.disabled = true;
  try {
    const r = await fetch('/api/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ password: document.getElementById('pw').value }) });
    if (r.ok) { location.replace('/'); return; }
    const j = await r.json().catch(() => ({}));
    const m = (j.error || 'Error').split(' / ');
    const de = document.documentElement.lang === 'de';
    err.textContent = de ? m[0] : (m[1] || m[0]);
    err.hidden = false;
    document.getElementById('pw').select();
  } catch (x) { err.textContent = String(x); err.hidden = false; }
  go.disabled = false;
});
