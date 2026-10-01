/* Prüft das Klang-Plugin auf dem Rechner: cc -O2 -o /tmp/eq_test tests/eq_test.c device/src/invoke-eq.c -lm && /tmp/eq_test
 * (tests/run.sh macht das). Schreibt Werte wie invoked in eine Datei und misst Pegel bei verschiedenen Frequenzen. */
#include <math.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#include "../device/src/ladspa.h"

static const char *path = "/tmp/invoke-eq-test";
static uint32_t seq = 0;

static void put(float bass, float treble, float pre, float comp, float thr, float ratio, float makeup) {
    float v[9] = {bass, 120, treble, 6000, pre, comp, thr, ratio, makeup};
    unsigned char b[64] = {0};
    uint32_t magic = 0x31514549u;
    seq += 2;
    memcpy(b, &magic, 4); memcpy(b + 4, &seq, 4); memcpy(b + 8, v, sizeof v);
    FILE *f = fopen(path, "r+b");
    if (!f) f = fopen(path, "w+b");
    fwrite(b, 1, 64, f); fclose(f);
}

static int fails = 0;
static void expect(const char *what, double got, double want, double tol) {
    int ok = fabs(got - want) <= tol;
    printf("%-34s %7.2f dB (erwartet %6.2f ± %.1f) %s\n", what, got, want, tol, ok ? "ok" : "FEHLER");
    fails += !ok;
}

/* Pegel in dB eines Sinus der Frequenz hz (Amplitude amp) nach dem Plugin, eingeschwungen */
static double level(const LADSPA_Descriptor *d, LADSPA_Handle h, double hz, double amp) {
    enum { N = 4800 };
    static float l[N], r[N], ol[N], or_[N];
    d->connect_port(h, 0, l); d->connect_port(h, 1, r); d->connect_port(h, 2, ol); d->connect_port(h, 3, or_);
    double sum = 0; int cnt = 0;
    for (int blk = 0; blk < 10; blk++) {
        for (int i = 0; i < N; i++) l[i] = r[i] = (float)(amp * sin(2 * M_PI * hz * (blk * N + i) / 48000.0));
        d->run(h, N);
        if (blk >= 6) for (int i = 0; i < N; i++) { sum += (double)ol[i] * ol[i]; cnt++; }
    }
    return 20 * log10(sqrt(sum / cnt) / (amp / sqrt(2)));
}

int main(void) {
    setenv("INVOKE_EQ_PATH", path, 1);
    unlink(path);
    const LADSPA_Descriptor *d = ladspa_descriptor(0);
    if (!d || strcmp(d->Label, "invoke_eq")) { puts("kein Deskriptor"); return 1; }
    LADSPA_Handle h = d->instantiate(d, 48000);
    d->activate(h);
    expect("ohne Datei: unverändert 1 kHz", level(d, h, 1000, 0.5), 0, 0.05);
    put(0, 0, 0, 0, 0, 1, 0);
    d->deactivate ? d->deactivate(h) : (void)0;
    d->cleanup(h);
    h = d->instantiate(d, 48000);
    d->activate(h);
    expect("neutral: 1 kHz", level(d, h, 1000, 0.5), 0, 0.05);
    put(6, 0, -6, 0, 0, 1, 0);
    expect("Bass +6, Vorverst. -6: 40 Hz", level(d, h, 40, 0.3), 0, 0.8);
    expect("Bass +6, Vorverst. -6: 2 kHz", level(d, h, 2000, 0.3), -6, 0.5);
    put(0, 6, -6, 0, 0, 1, 0);
    expect("Höhen +6, Vorverst. -6: 15 kHz", level(d, h, 15000, 0.3), 0, 1.0);
    expect("Höhen +6, Vorverst. -6: 200 Hz", level(d, h, 200, 0.3), -6, 0.5);
    put(0, 0, 0, 1, -30, 4, 0);
    double loud = level(d, h, 1000, 0.5), quiet = level(d, h, 1000, 0.005);
    expect("Kompressor: lauter Ton gedämpft", loud, -0.75 * (20 * log10(0.5 / sqrt(2)) + 30), 2.5);
    expect("Kompressor: leiser Ton unverändert", quiet, 0, 0.5);
    d->cleanup(h);
    unlink(path);
    printf(fails ? "%d Fehler\n" : "alles ok\n", fails);
    return fails != 0;
}
