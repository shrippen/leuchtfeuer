/*
 * LADSPA-Plugin "invoke_viz_tap": reicht Stereo-Ton unverändert durch und legt eine Mono-Kopie in einen
 * Ringpuffer im gemeinsamen Speicher (/dev/shm/invoke-viz). invoked liest ihn für den Leuchtring-Visualizer.
 *
 * Eingebunden in asound-music.conf vor dem Softvol-Regler "Invoke Music" (Pegel unabhängig von der Lautstärke).
 * Schreibt nur in den Speicher, wartet nie und ruft nichts auf, das blockieren kann; fehlt /dev/shm, wird nur
 * durchgereicht. Mehrere gleichzeitige Quellen schreiben in denselben Puffer (für eine Anzeige ausreichend).
 *
 * Pufferaufbau (little endian): u32 Magie "IVZ1", u32 Abtastrate, u32 Schreibposition (Anzahl geschriebener
 * Werte, läuft über), u32 reserviert, danach VIZ_N float-Werte.
 */
#include <fcntl.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mman.h>
#include <sys/stat.h>
#include <unistd.h>

#include "ladspa.h"

#define VIZ_PATH "/dev/shm/invoke-viz"
#define VIZ_N 8192 /* Zweierpotenz */
#define VIZ_MAGIC 0x315a5649u /* "IVZ1" */

struct viz_shm {
    uint32_t magic, rate, pos, reserved;
    float buf[VIZ_N];
};

enum { P_IN_L, P_IN_R, P_OUT_L, P_OUT_R, P_COUNT };

struct tap {
    LADSPA_Data *port[P_COUNT];
    struct viz_shm *shm;
    unsigned long rate;
};

static struct viz_shm *map_shm(unsigned long rate) {
    mode_t old = umask(0);
    int fd = open(VIZ_PATH, O_RDWR | O_CREAT | O_CLOEXEC, 0666);
    umask(old);
    if (fd < 0) return NULL;
    if (ftruncate(fd, sizeof(struct viz_shm)) < 0) { close(fd); return NULL; }
    void *p = mmap(NULL, sizeof(struct viz_shm), PROT_READ | PROT_WRITE, MAP_SHARED, fd, 0);
    close(fd);
    if (p == MAP_FAILED) return NULL;
    struct viz_shm *s = p;
    s->rate = (uint32_t)rate;
    __atomic_store_n(&s->magic, VIZ_MAGIC, __ATOMIC_RELEASE);
    return s;
}

static LADSPA_Handle instantiate(const LADSPA_Descriptor *d, unsigned long rate) {
    (void)d;
    struct tap *t = calloc(1, sizeof *t);
    if (t) t->rate = rate;
    return t;
}

static void connect_port(LADSPA_Handle h, unsigned long port, LADSPA_Data *data) {
    if (port < P_COUNT) ((struct tap *)h)->port[port] = data;
}

static void activate(LADSPA_Handle h) {
    struct tap *t = h;
    if (!t->shm) t->shm = map_shm(t->rate);
}

static void run(LADSPA_Handle h, unsigned long n) {
    struct tap *t = h;
    const LADSPA_Data *l = t->port[P_IN_L], *r = t->port[P_IN_R];
    LADSPA_Data *ol = t->port[P_OUT_L], *or_ = t->port[P_OUT_R];
    if (ol != l) memmove(ol, l, n * sizeof *ol);
    if (or_ != r) memmove(or_, r, n * sizeof *or_);
    struct viz_shm *s = t->shm;
    if (!s) return;
    /* aus den Ausgängen lesen: bei Puffern an derselben Stelle sind die Eingänge schon überschrieben */
    uint32_t pos = __atomic_load_n(&s->pos, __ATOMIC_RELAXED);
    for (unsigned long i = 0; i < n; i++) s->buf[(pos + i) & (VIZ_N - 1)] = 0.5f * (ol[i] + or_[i]);
    s->rate = (uint32_t)t->rate;
    __atomic_store_n(&s->pos, pos + (uint32_t)n, __ATOMIC_RELEASE);
}

static void cleanup(LADSPA_Handle h) {
    struct tap *t = h;
    if (t->shm) munmap(t->shm, sizeof(struct viz_shm));
    free(t);
}

static const LADSPA_PortDescriptor port_desc[P_COUNT] = {
    LADSPA_PORT_INPUT | LADSPA_PORT_AUDIO, LADSPA_PORT_INPUT | LADSPA_PORT_AUDIO,
    LADSPA_PORT_OUTPUT | LADSPA_PORT_AUDIO, LADSPA_PORT_OUTPUT | LADSPA_PORT_AUDIO,
};
static const char *const port_names[P_COUNT] = {"In L", "In R", "Out L", "Out R"};
static const LADSPA_PortRangeHint port_hints[P_COUNT];

static const LADSPA_Descriptor desc = {
    .UniqueID = 0x1e5a1, /* privat, nur auf dem Lautsprecher benutzt */
    .Label = "invoke_viz_tap",
    .Properties = LADSPA_PROPERTY_HARD_RT_CAPABLE,
    .Name = "Invoke visualizer tap",
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
