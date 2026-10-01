'use strict';
// Raum einmessen: Auswertung der Messung (rosa Rauschen vom Lautsprecher, Mikrofon des Handys).
// Reine Rechnung ohne Seite, damit sie sich mit node prüfen lässt (tests/roomeq_test.js).
//
// 1. Spektrum (Leistung je FFT-Bin) auf 1/6 Oktave glätten, Raster 1/24 Oktave von 20 Hz bis 20 kHz. Mal f: rosa
//    Rauschen hat je Bin eine Leistung proportional 1/f; so wird die Kurve für einen idealen Raum waagrecht.
// 2. Bezugspegel: Mittel über 200 Hz bis 2 kHz (rosa Rauschen ist dort gleich laut je Oktave).
// 3. Im Bass (35 bis 350 Hz, dort wirken Raummoden) die höchste Überhöhung suchen; ab 3 dB ein Glockenfilter, das sie
//    zu etwa 80 % absenkt; Breite aus der Breite der Spitze. Die Wirkung einrechnen und wiederholen (höchstens 4).
// Nur Absenkungen: Handy-Mikrofone messen im Bass oft zu wenig, Anheben würde das verstärken; Senken ist sicher.
(function (root) {
  const RATE = 48000;

  // Betrag eines RBJ-Glockenfilters in dB bei f (wie im Plugin, 48 kHz)
  function peakingDB(f, hz, db, q) {
    const A = Math.pow(10, db / 40), w0 = 2 * Math.PI * hz / RATE, alpha = Math.sin(w0) / (2 * q), c = Math.cos(w0);
    const b0 = 1 + alpha * A, b1 = -2 * c, b2 = 1 - alpha * A, a0 = 1 + alpha / A, a1 = -2 * c, a2 = 1 - alpha / A;
    const w = 2 * Math.PI * f / RATE, cw = Math.cos(w), c2w = Math.cos(2 * w), sw = Math.sin(w), s2w = Math.sin(2 * w);
    const nr = b0 + b1 * cw + b2 * c2w, ni = -(b1 * sw + b2 * s2w);
    const dr = a0 + a1 * cw + a2 * c2w, di = -(a1 * sw + a2 * s2w);
    return 10 * Math.log10((nr * nr + ni * ni) / (dr * dr + di * di));
  }

  // smooth: Leistung je Bin (linear) -> Kurve [{hz, db}] in 1/6 Oktave
  function smooth(binHz, power) {
    const out = [];
    for (let f = 20; f <= 20000; f *= Math.pow(2, 1 / 24)) {
      const lo = f * Math.pow(2, -1 / 12), hi = f * Math.pow(2, 1 / 12);
      let s = 0, n = 0;
      for (let i = Math.max(1, Math.floor(lo / binHz)); i <= Math.ceil(hi / binHz) && i < power.length; i++) {
        const fi = i * binHz;
        if (fi >= lo && fi <= hi) { s += power[i]; n++; }
      }
      if (!n) { // zu wenige Bins (sehr tiefe Frequenzen): nächster Bin
        const i = Math.round(f / binHz);
        if (i > 0 && i < power.length) { s = power[i]; n = 1; }
      }
      if (n) out.push({ hz: f, db: 10 * Math.log10(s / n * f + 1e-20) });
    }
    return out;
  }

  const mean = (c, lo, hi) => { const v = c.filter(p => p.hz >= lo && p.hz <= hi).map(p => p.db); return v.reduce((a, b) => a + b, 0) / (v.length || 1); };

  // suggest: aus der geglätteten Kurve Raumfilter vorschlagen
  function suggest(curve, opts = {}) {
    const lo = opts.lo || 35, hi = opts.hi || 350, max = opts.max || 4, min = opts.minDB || 3;
    const ref = mean(curve, 200, 2000);
    const res = curve.map(p => ({ hz: p.hz, r: p.db - ref }));
    const bands = [];
    for (let k = 0; k < max; k++) {
      let best = -1;
      res.forEach((p, i) => { if (p.hz >= lo && p.hz <= hi && (best < 0 || p.r > res[best].r)) best = i; });
      if (best < 0 || res[best].r < min) break;
      const peak = res[best].r, half = peak / 2;
      let a = best, b = best;
      while (a > 0 && res[a - 1].r > half) a--;
      while (b < res.length - 1 && res[b + 1].r > half) b++;
      const bw = Math.max(1 / 12, Math.log2(res[b].hz / res[a].hz)); // Oktaven
      const q = Math.min(8, Math.max(1, Math.sqrt(Math.pow(2, bw)) / (Math.pow(2, bw) - 1)));
      const band = { hz: Math.round(res[best].hz), db: -Math.round(Math.min(12, peak * 0.8) * 10) / 10, q: Math.round(q * 100) / 100 };
      bands.push(band);
      res.forEach(p => { p.r += peakingDB(p.hz, band.hz, band.db, band.q); });
    }
    const after = res.map(p => ({ hz: p.hz, db: p.r }));
    return { ref, bands, after, before: curve.map(p => ({ hz: p.hz, db: p.db - ref })) };
  }

  // average: mehrere Messpositionen mitteln (in dB, gleiche Raster)
  function average(curves) {
    if (!curves.length) return [];
    return curves[0].map((p, i) => ({ hz: p.hz, db: curves.reduce((a, c) => a + (c[i] ? c[i].db : p.db), 0) / curves.length }));
  }

  // snr: Abstand Messung - Ruhe im Bereich 100 Hz ... 1 kHz (dB)
  function snr(curve, noise) {
    return mean(curve, 100, 1000) - mean(noise, 100, 1000);
  }

  const api = { smooth, suggest, average, snr, peakingDB };
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
  else root.RoomEQ = api;
})(typeof window !== 'undefined' ? window : globalThis);
