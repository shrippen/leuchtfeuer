package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"math/rand"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// Raum einmessen: Der Lautsprecher spielt rosa Rauschen (gleiche Energie je Oktave) über die Musik-Kette, der Klang
// ist solange neutral, die anderen Quellen pausieren (Quelle "measure"). Das Handy misst im Browser mit seinem
// Mikrofon (web/roomeq.js: Spektrum mitteln, Spitzen im Bass finden, Absenkungen vorschlagen) - das braucht HTTPS
// (WEB_TLS), sonst geben Browser das Mikrofon nicht frei. Höchstens 40 s am Stück.

const measureMaxSecs = 40

type measurer struct {
	a   *app
	mu  sync.Mutex
	end time.Time
	pl  *player
}

func newMeasurer(a *app) *measurer {
	return &measurer{a: a, pl: newPlayer(map[string]string{"": "leuchtfeuer_music"})}
}

func (m *measurer) Start(secs int) error {
	secs = clamp(secs, 3, measureMaxSecs)
	if m.a.eq == nil {
		return fmt.Errorf("Klang-Plugin nicht aktiv")
	}
	m.mu.Lock()
	m.end = time.Now().Add(time.Duration(secs) * time.Second)
	m.mu.Unlock()
	m.a.eq.measuring.Store(true)
	m.a.eq.Kick()
	m.a.src.Update("measure", "playing", map[string]string{"title": "Messrauschen"})
	time.Sleep(200 * time.Millisecond) // neutraler Klang wirkt, bevor das Rauschen beginnt
	m.pl.PlayNoise("measure", "pink", secs)
	log.Printf("Raum einmessen: rosa Rauschen für %d s", secs)
	go func() {
		time.Sleep(time.Duration(secs)*time.Second + 300*time.Millisecond)
		m.mu.Lock()
		done := !time.Now().Before(m.end)
		m.mu.Unlock()
		if done {
			m.Stop()
		}
	}()
	return nil
}

func (m *measurer) Stop() {
	m.pl.Stop()
	if m.a.eq != nil {
		m.a.eq.measuring.Store(false)
		m.a.eq.Kick()
	}
	m.a.src.Update("measure", "idle", nil)
}

// pinkNoise: Paul Kellets Filter (rosa aus weißem Rauschen), Pegel etwa -18 dBFS RMS.
type pinkNoise struct {
	b0, b1, b2, b3, b4, b5, b6 float64
	r                          *rand.Rand
}

func (p *pinkNoise) next() float64 {
	w := p.r.Float64()*2 - 1
	p.b0 = 0.99886*p.b0 + w*0.0555179
	p.b1 = 0.99332*p.b1 + w*0.0750759
	p.b2 = 0.96900*p.b2 + w*0.1538520
	p.b3 = 0.86650*p.b3 + w*0.3104856
	p.b4 = 0.55000*p.b4 + w*0.5329522
	p.b5 = -0.7616*p.b5 - w*0.0168980
	out := p.b0 + p.b1 + p.b2 + p.b3 + p.b4 + p.b5 + p.b6 + w*0.5362
	p.b6 = w * 0.115926
	return out * 0.05
}

// PlayNoise spielt secs Sekunden rosa Rauschen (48 kHz, Stereo, ein- und ausgeblendet).
func (p *player) PlayNoise(kind, name string, secs int) {
	pn := &pinkNoise{r: rand.New(rand.NewSource(time.Now().UnixNano()))}
	p.PlayGen(kind, name, secs, func(int) float64 { return pn.next() })
}

// PlayGen spielt secs Sekunden aus gen (Abtastwert Nr. i bei 48 kHz, -1..1), Stereo, ein- und ausgeblendet.
func (p *player) PlayGen(kind, name string, secs int, gen func(i int) float64) {
	p.mu.Lock()
	p.stopLocked()
	cmd := exec.Command("aplay", "-q", "-D", p.sink(kind), "-f", "S16_LE", "-r", "48000", "-c", "2", "-t", "raw")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	in, err := cmd.StdinPipe()
	if err != nil || cmd.Start() != nil {
		p.mu.Unlock()
		log.Printf("aplay startet nicht")
		return
	}
	sess := &playSession{stop: make(chan struct{})}
	p.cmd, p.sess, p.Kind, p.Name, p.State = cmd, sess, kind, name, "playing"
	p.mu.Unlock()
	p.changed()
	go func() {
		defer func() {
			in.Close()
			cmd.Wait()
			p.mu.Lock()
			if p.sess == sess {
				p.cmd, p.sess = nil, nil
				p.Kind, p.Name, p.State = "", "", "idle"
			}
			p.mu.Unlock()
			p.changed()
		}()
		const rate, block = 48000, 4800
		total, fade := secs*rate, rate/2
		buf := make([]byte, block*4)
		for i := 0; i < total; i += block {
			select {
			case <-sess.stop:
				return
			default:
			}
			for j := 0; j < block; j++ {
				env := 1.0
				if k := i + j; k < fade {
					env = float64(k) / float64(fade)
				} else if k > total-fade {
					env = math.Max(0, float64(total-k)/float64(fade))
				}
				s := int16(math.Max(-1, math.Min(1, gen(i+j)*env)) * 32767)
				binary.LittleEndian.PutUint16(buf[j*4:], uint16(s))
				binary.LittleEndian.PutUint16(buf[j*4+2:], uint16(s))
			}
			if _, err := in.Write(buf); err != nil {
				return
			}
		}
	}()
}

// Preview spielt eine leise Hörprobe von 6 s über die Tonkette mit Klang und Raumkorrektur: je 2 s Bass, Mitten,
// Höhen. So hört man beim Einstellen, was Bass, Höhen oder ein Filter bewirken.
func (m *measurer) Preview() error {
	if demoMode {
		return nil
	}
	m.pl.PlayGen("preview", "Hörprobe", 6, previewSample(rand.New(rand.NewSource(1))))
	return nil
}

// previewSample: Bassnoten (A1, E2, A2, E2), ein Dur-Akkord (C4 E4 G4), dann Becken-artige Rauschstöße; Spitze ~0,2.
func previewSample(r *rand.Rand) func(i int) float64 {
	const rate = 48000.0
	bass := []float64{55, 82.41, 110, 82.41}
	return func(i int) float64 {
		t := float64(i) / rate
		switch {
		case t < 2:
			k := int(t / 0.5)
			ph := t - float64(k)*0.5
			env := math.Min(1, ph/0.01) * math.Exp(-ph*3)
			f := bass[k%len(bass)]
			return 0.2 * env * (math.Sin(2*math.Pi*f*t) + 0.3*math.Sin(4*math.Pi*f*t))
		case t < 4:
			env := math.Min(1, (t-2)/0.05) * math.Min(1, (4-t)/0.1)
			return 0.07 * env * (math.Sin(2*math.Pi*261.63*t) + math.Sin(2*math.Pi*329.63*t) + math.Sin(2*math.Pi*392*t))
		default:
			ph := math.Mod(t-4, 0.25)
			return 0.15 * math.Exp(-ph*30) * (r.Float64()*2 - 1)
		}
	}
}
