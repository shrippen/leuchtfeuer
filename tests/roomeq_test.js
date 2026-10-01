'use strict';
// Prüft die Auswertung der Raummessung (src/invoked/web/roomeq.js): node tests/roomeq_test.js
// Ein Raum mit zwei Moden (+9 dB bei 55 Hz, +6 dB bei 140 Hz) auf rosa Rauschen; der Vorschlag muss beide finden
// und die Kurve danach im Bass auf höchstens 3 dB bringen, ohne etwas anzuheben.
const R = require('../src/invoked/web/roomeq.js');

let fails = 0;
const ok = (cond, msg) => { console.log((cond ? 'ok     ' : 'FEHLER ') + msg); if (!cond) fails++; };

const rate = 48000, n = 32768, binHz = rate / n;
const room = f => R.peakingDB(f, 55, 9, 5) + R.peakingDB(f, 140, 6, 4);
const power = new Float64Array(n / 2);
for (let i = 1; i < n / 2; i++) {
  const f = i * binHz;
  const pink = 1 / f; // rosa: Leistung ~ 1/f je Bin
  power[i] = pink * Math.pow(10, room(f) / 10) * (1 + 0.05 * Math.sin(i)); // etwas Welligkeit
}
const curve = R.smooth(binHz, power);
ok(curve.length > 200 && curve[0].hz >= 20, `Raster: ${curve.length} Punkte`);
// rosa Rauschen in 1/6 Oktave: oberhalb 300 Hz gleich hoch (±0,5 dB)
const flat = curve.filter(p => p.hz > 300 && p.hz < 15000).map(p => p.db);
ok(Math.max(...flat) - Math.min(...flat) < 1, `rosa Rauschen flach (Spanne ${(Math.max(...flat) - Math.min(...flat)).toFixed(2)} dB)`);

const s = R.suggest(curve);
ok(s.bands.length >= 2 && s.bands.length <= 4, `${s.bands.length} Filter: ${JSON.stringify(s.bands)}`);
ok(s.bands.some(b => Math.abs(b.hz - 55) <= 4), 'Mode 55 Hz gefunden');
ok(s.bands.some(b => Math.abs(b.hz - 140) <= 10), 'Mode 140 Hz gefunden');
ok(s.bands.every(b => b.db < 0 && b.db >= -12 && b.q >= 1 && b.q <= 8), 'nur Absenkungen, Güte 1 ... 8');
const bass = s.after.filter(p => p.hz >= 35 && p.hz <= 350).map(p => p.db);
ok(Math.max(...bass) < 3, `danach im Bass höchstens ${Math.max(...bass).toFixed(2)} dB`);

// ohne Moden: kein Filter
const flatPow = power.map((_, i) => (i ? 1 / (i * binHz) : 0));
ok(R.suggest(R.smooth(binHz, flatPow)).bands.length === 0, 'flacher Raum: kein Filter');

// Mittelung und Abstand zur Ruhe
const avg = R.average([curve, curve.map(p => ({ hz: p.hz, db: p.db + 2 }))]);
ok(Math.abs(avg[100].db - curve[100].db - 1) < 1e-9, 'Mittelung');
ok(Math.abs(R.snr(curve, curve.map(p => ({ hz: p.hz, db: p.db - 20 }))) - 20) < 1e-9, 'Abstand zur Ruhe');

console.log(fails ? `${fails} Fehler` : 'alles ok');
process.exit(fails ? 1 : 0);
