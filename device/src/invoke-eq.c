/*
 * LADSPA-Plugin "invoke_eq": Klang für alle Musikdienste (asound-music.conf, nach dem Visualizer-Abgriff):
 * Bass (Kuhschwanz unten), Höhen (Kuhschwanz oben), Vorverstärkung gegen Übersteuern, Kompressor (Nachtmodus)
 * und eine weiche Begrenzung am Ende.
 *
 * Die Werte setzt invoked live in /dev/shm/invoke-eq (src/invoked/eq.go); das Plugin blendet die Datei nur lesend ein
 * und übernimmt neue Werte, sobald sich der Folgezähler ändert (ungerade = wird gerade geschrieben: warten).
 * Fehlt die Datei, reicht es den Ton unverändert durch und sieht etwa jede Sekunde nach. Es wartet nie und ruft im
 * Tonpfad nichts auf, das blockieren kann. INVOKE_EQ_PATH ersetzt den Pfad (Tests).
 *
 * Dateiaufbau (little endian, 64 Byte): u32 Magie "IEQ1", u32 Folgezähler, float: Bass dB, Bass Hz, Höhen dB,
 * Höhen Hz, Vorverstärkung dB, Kompressor (0/1), Schwelle dB, Verhältnis, Aufholverstärkung dB.
 */
#include <fcntl.h>
#include <math.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mman.h>
#include <unistd.h>

#include "ladspa.h"

#define EQ_PATH "/dev/shm/invoke-eq"
#define EQ_MAGIC 0x31514549u /* "IEQ1" */
#define EQ_SIZE 64

struct eq_shm {
    uint32_t magic, seq;
    float bass_db, bass_hz, treble_db, treble_hz, preamp_db, comp, thr_db, ratio, makeup_db;
};

struct biquad {
    float b0, b1, b2, a1, a2;
    float z1[2], z2[2]; /* Transposed Direct Form II, je Kanal */
};

enum { P_IN_L, P_IN_R, P_OUT_L, P_OUT_R, P_COUNT };

struct eq {
    LADSPA_Data *port[P_COUNT];
    float rate;
    const struct eq_shm *shm;
    unsigned long retry; /* Abtastwerte bis zum nächsten Versuch, die Datei einzublenden */
    uint32_t seq;
    int active;          /* 0 = Durchreichen */
    struct biquad bass, treble;
    float pre;           /* lineare Vorverstärkung */
    int comp;
    float thr_db, slope, makeup, env, gain, att, rel;
    unsigned ctr;
};

/* RBJ-Kochbuch: Kuhschwanzfilter mit Steilheit S = 1 */
static void shelf(struct biquad *f, int high, float db, float hz, float rate) {
    if (hz <= 10.0f || hz >= rate * 0.45f) hz = high ? 6000.0f : 120.0f;
    float A = powf(10.0f, db / 40.0f), w = 2.0f * (float)M_PI * hz / rate;
    float c = cosf(w), s = sinf(w), alpha = s / 2.0f * sqrtf(2.0f), sa = 2.0f * sqrtf(A) * alpha;
    float b0, b1, b2, a0, a1, a2;
    if (!high) {
        b0 = A * ((A + 1) - (A - 1) * c + sa);
        b1 = 2 * A * ((A - 1) - (A + 1) * c);
        b2 = A * ((A + 1) - (A - 1) * c - sa);
        a0 = (A + 1) + (A - 1) * c + sa;
        a1 = -2 * ((A - 1) + (A + 1) * c);
        a2 = (A + 1) + (A - 1) * c - sa;
    } else {
        b0 = A * ((A + 1) + (A - 1) * c + sa);
        b1 = -2 * A * ((A - 1) + (A + 1) * c);
        b2 = A * ((A + 1) + (A - 1) * c - sa);
        a0 = (A + 1) - (A - 1) * c + sa;
        a1 = 2 * ((A - 1) - (A + 1) * c);
        a2 = (A + 1) - (A - 1) * c - sa;
    }
    f->b0 = b0 / a0; f->b1 = b1 / a0; f->b2 = b2 / a0; f->a1 = a1 / a0; f->a2 = a2 / a0;
}

static inline float bq(struct biquad *f, int ch, float x) {
    float y = f->b0 * x + f->z1[ch];
    f->z1[ch] = f->b1 * x - f->a1 * y + f->z2[ch];
    f->z2[ch] = f->b2 * x - f->a2 * y;
    return y;
}

static void map_shm(struct eq *e) {
    const char *p = getenv("INVOKE_EQ_PATH");
    int fd = open(p && *p ? p : EQ_PATH, O_RDONLY | O_CLOEXEC);
    e->retry = (unsigned long)e->rate; /* nächster Versuch in etwa 1 s */
    if (fd < 0) return;
    void *m = mmap(NULL, EQ_SIZE, PROT_READ, MAP_SHARED, fd, 0);
    close(fd);
    if (m == MAP_FAILED) return;
    if (((const struct eq_shm *)m)->magic != EQ_MAGIC) { munmap(m, EQ_SIZE); return; }
    e->shm = m;
    e->seq = 0;
}

/* Neue Werte übernehmen (Folgezähler gerade und unverändert während des Lesens). */
static void load(struct eq *e) {
    const struct eq_shm *s = e->shm;
    uint32_t a = __atomic_load_n(&s->seq, __ATOMIC_ACQUIRE);
    if ((a & 1) || a == e->seq) return;
    struct eq_shm v;
    memcpy(&v, (const void *)s, sizeof v);
    __atomic_thread_fence(__ATOMIC_ACQUIRE);
    if (__atomic_load_n(&s->seq, __ATOMIC_RELAXED) != a) return;
    e->seq = a;
    shelf(&e->bass, 0, v.bass_db, v.bass_hz, e->rate);
    shelf(&e->treble, 1, v.treble_db, v.treble_hz, e->rate);
    e->pre = powf(10.0f, v.preamp_db / 20.0f);
    e->comp = v.comp > 0.5f;
    e->thr_db = v.thr_db;
    e->slope = v.ratio > 1.0f ? 1.0f - 1.0f / v.ratio : 0.0f;
    e->makeup = powf(10.0f, v.makeup_db / 20.0f);
    e->active = fabsf(v.bass_db) > 0.05f || fabsf(v.treble_db) > 0.05f || fabsf(v.preamp_db) > 0.05f || e->comp;
}

static LADSPA_Handle instantiate(const LADSPA_Descriptor *d, unsigned long rate) {
    (void)d;
    struct eq *e = calloc(1, sizeof *e);
    if (!e) return NULL;
    e->rate = (float)rate;
    e->pre = e->makeup = e->gain = 1.0f;
    e->att = 1.0f - expf(-1.0f / (0.010f * e->rate)); /* 10 ms */
    e->rel = 1.0f - expf(-1.0f / (0.250f * e->rate)); /* 250 ms */
    return e;
}

static void connect_port(LADSPA_Handle h, unsigned long port, LADSPA_Data *data) {
    if (port < P_COUNT) ((struct eq *)h)->port[port] = data;
}

static void activate(LADSPA_Handle h) {
    struct eq *e = h;
    memset(e->bass.z1, 0, sizeof e->bass.z1); memset(e->bass.z2, 0, sizeof e->bass.z2);
    memset(e->treble.z1, 0, sizeof e->treble.z1); memset(e->treble.z2, 0, sizeof e->treble.z2);
    e->env = 0.0f; e->gain = 1.0f;
    if (!e->shm) map_shm(e);
}

/* weiche Begrenzung oberhalb von 0,9 (statt hartem Abschneiden) */
static inline float soft(float x) {
    float a = fabsf(x);
    if (a <= 0.9f) return x;
    float y = 0.9f + 0.1f * tanhf((a - 0.9f) / 0.1f);
    return x < 0 ? -y : y;
}

static void run(LADSPA_Handle h, unsigned long n) {
    struct eq *e = h;
    const LADSPA_Data *l = e->port[P_IN_L], *r = e->port[P_IN_R];
    LADSPA_Data *ol = e->port[P_OUT_L], *or_ = e->port[P_OUT_R];
    if (!e->shm) {
        if (e->retry > n) e->retry -= n; else map_shm(e);
    }
    if (e->shm) load(e);
    if (!e->active) {
        if (ol != l) memmove(ol, l, n * sizeof *ol);
        if (or_ != r) memmove(or_, r, n * sizeof *or_);
        return;
    }
    for (unsigned long i = 0; i < n; i++) {
        float x0 = l[i] * e->pre, x1 = r[i] * e->pre;
        x0 = bq(&e->treble, 0, bq(&e->bass, 0, x0));
        x1 = bq(&e->treble, 1, bq(&e->bass, 1, x1));
        if (e->comp) {
            float pk = fmaxf(fabsf(x0), fabsf(x1));
            e->env += (pk > e->env ? e->att : e->rel) * (pk - e->env);
            if ((e->ctr++ & 15) == 0) { /* Verstärkung nur alle 16 Werte neu berechnen */
                float db = 20.0f * log10f(e->env + 1e-9f), over = db - e->thr_db;
                e->gain = over > 0 ? powf(10.0f, -over * e->slope / 20.0f) : 1.0f;
            }
            float g = e->gain * e->makeup;
            x0 *= g; x1 *= g;
        }
        ol[i] = soft(x0);
        or_[i] = soft(x1);
    }
    /* Denormale vermeiden: Filterzustand bei Stille auf 0 */
    for (int c = 0; c < 2; c++) {
        if (fabsf(e->bass.z1[c]) < 1e-15f) e->bass.z1[c] = e->bass.z2[c] = 0;
        if (fabsf(e->treble.z1[c]) < 1e-15f) e->treble.z1[c] = e->treble.z2[c] = 0;
    }
}

static void cleanup(LADSPA_Handle h) {
    struct eq *e = h;
    if (e->shm) munmap((void *)e->shm, EQ_SIZE);
    free(e);
}

static const LADSPA_PortDescriptor port_desc[P_COUNT] = {
    LADSPA_PORT_INPUT | LADSPA_PORT_AUDIO, LADSPA_PORT_INPUT | LADSPA_PORT_AUDIO,
    LADSPA_PORT_OUTPUT | LADSPA_PORT_AUDIO, LADSPA_PORT_OUTPUT | LADSPA_PORT_AUDIO,
};
static const char *const port_names[P_COUNT] = {"In L", "In R", "Out L", "Out R"};
static const LADSPA_PortRangeHint port_hints[P_COUNT];

static const LADSPA_Descriptor desc = {
    .UniqueID = 0x1e5a2, /* privat, nur auf dem Lautsprecher benutzt */
    .Label = "invoke_eq",
    .Properties = LADSPA_PROPERTY_HARD_RT_CAPABLE,
    .Name = "Invoke tone (bass, treble, loudness, night mode)",
    .Maker = "Leuchtfeuer",
    .Copyright = "MIT",
    .PortCount = P_COUNT,
    .PortDescriptors = port_desc,
    .PortNames = port_names,
    .PortRangeHints = port_hints,
    .instantiate = instantiate,
    .connect_port = connect_port,
    .activate = activate,
    .run = run,
    .cleanup = cleanup,
};

const LADSPA_Descriptor *ladspa_descriptor(unsigned long i) { return i == 0 ? &desc : NULL; }
