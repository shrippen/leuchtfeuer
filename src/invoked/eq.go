package main

import (
	"encoding/binary"
	"math"
	"os"
	"sync"
)

// Klang: Bass, Höhen, Loudness und Nachtmodus. Gerechnet wird im LADSPA-Plugin invoke_eq (device/src/invoke-eq.c)
// in der Kette aller Musikdienste (asound-music.conf). invoked schreibt die wirksamen Werte in die gemeinsame Datei
// /dev/shm/invoke-eq; das Plugin liest sie bei jeder Änderung (Folgezähler) ohne Neustart der Dienste.
//
// Aufbau (little endian, 64 Byte): u32 Magie "IEQ1", u32 Folgezähler (ungerade = wird gerade geschrieben),
// dann float32: Bass dB, Bass Hz, Höhen dB, Höhen Hz, Vorverstärkung dB, Kompressor an (0/1), Schwelle dB,
// Verhältnis, Aufholverstärkung dB.
//
// Loudness hebt Bass und Höhen an, je leiser das Gerät spielt (das Ohr hört bei kleiner Lautstärke weniger Tiefen).
// Der Nachtmodus gleicht laute und leise Stellen an (Kompressor) und nimmt etwas Bass heraus.

type EqSettings struct {
	Version  int  `json:"version"`
	Bass     int  `json:"bass"`   // -12 ... +12 dB
	Treble   int  `json:"treble"` // -12 ... +12 dB
	Loudness bool `json:"loudness"`
	Night    bool `json:"night"`
}

func defaultEq() EqSettings { return EqSettings{Version: 1} }

const (
	eqPath  = "/dev/shm/invoke-eq"
	eqMagic = 0x31514549 // "IEQ1"
	eqSize  = 64
)

type eqParams struct {
	BassDB, BassHz, TrebleDB, TrebleHz, PreampDB float64
	Comp                                         bool
	ThresholdDB, Ratio, MakeupDB                 float64
}

// eqEffective berechnet die wirksamen Werte aus Einstellungen und Lautstärke (0 ... 100).
func eqEffective(s EqSettings, vol int) eqParams {
	p := eqParams{BassDB: float64(clamp(s.Bass, -12, 12)), BassHz: 120, TrebleDB: float64(clamp(s.Treble, -12, 12)), TrebleHz: 6000, Ratio: 1}
	if s.Loudness {
		q := 1 - float64(clamp(vol, 0, 100))/100 // 0 bei voller, 1 bei kleinster Lautstärke
		p.BassDB += 9 * q
		p.TrebleDB += 3 * q
	}
	if s.Night {
		p.Comp, p.ThresholdDB, p.Ratio, p.MakeupDB = true, -30, 4, 9
		p.BassDB -= 3
	}
	p.BassDB = math.Max(-15, math.Min(15, p.BassDB))
	p.TrebleDB = math.Max(-15, math.Min(15, p.TrebleDB))
	// Vorverstärkung gegen Übersteuern: um die größte Anhebung absenken
	p.PreampDB = -math.Max(0, math.Max(p.BassDB, p.TrebleDB))
	return p
}

func (p eqParams) encode(seq uint32) []byte {
	b := make([]byte, eqSize)
	binary.LittleEndian.PutUint32(b[0:], eqMagic)
	binary.LittleEndian.PutUint32(b[4:], seq)
	comp := 0.0
	if p.Comp {
		comp = 1
	}
	for i, f := range []float64{p.BassDB, p.BassHz, p.TrebleDB, p.TrebleHz, p.PreampDB, comp, p.ThresholdDB, p.Ratio, p.MakeupDB} {
		binary.LittleEndian.PutUint32(b[8+4*i:], math.Float32bits(float32(f)))
	}
	return b
}

type eqWriter struct {
	a    *app
	path string
	mu   sync.Mutex
	seq  uint32
	last eqParams
	kick chan struct{}
}

func newEqWriter(a *app, path string) *eqWriter {
	return &eqWriter{a: a, path: path, kick: make(chan struct{}, 1)}
}

func (e *eqWriter) Kick() {
	select {
	case e.kick <- struct{}{}:
	default:
	}
}

// write schreibt die Werte an Ort und Stelle (das Plugin hat die Datei eingeblendet): erst den Zähler ungerade,
// dann die Werte, dann den Zähler gerade.
func (e *eqWriter) write(p eqParams, force bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !force && p == e.last {
		return nil
	}
	f, err := os.OpenFile(e.path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.Size() >= 8 {
		var hdr [8]byte
		if _, err := f.ReadAt(hdr[:], 0); err == nil && binary.LittleEndian.Uint32(hdr[0:]) == eqMagic {
			if s := binary.LittleEndian.Uint32(hdr[4:]); s > e.seq {
				e.seq = s &^ 1
			}
		}
	}
	e.seq += 2
	b := p.encode(e.seq)
	var odd [4]byte
	binary.LittleEndian.PutUint32(odd[:], e.seq-1)
	f.WriteAt(b[0:4], 0)
	f.WriteAt(odd[:], 4)
	if _, err := f.WriteAt(b[8:], 8); err != nil {
		return err
	}
	if _, err := f.WriteAt(b[4:8], 4); err != nil {
		return err
	}
	e.last = p
	return nil
}

func (e *eqWriter) Run() {
	e.a.Listen(func(kind string, _ map[string]any) {
		if kind == "volume" || kind == "settings" {
			e.Kick()
		}
	})
	force := true
	for {
		vol, _, _ := e.a.vol.Get()
		if err := e.write(eqEffective(e.a.st.Snapshot().Eq, vol), force); err != nil {
			logf("Klang: %v", err)
		}
		force = false
		<-e.kick
	}
}
