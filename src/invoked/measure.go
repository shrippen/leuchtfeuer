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
	return &measurer{a: a, pl: newPlayer(map[string]string{"": "invoke_music"})}
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
		pn := &pinkNoise{r: rand.New(rand.NewSource(time.Now().UnixNano()))}
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
				s := int16(math.Max(-1, math.Min(1, pn.next()*env)) * 32767)
				binary.LittleEndian.PutUint16(buf[j*4:], uint16(s))
				binary.LittleEndian.PutUint16(buf[j*4+2:], uint16(s))
			}
			if _, err := in.Write(buf); err != nil {
				return
			}
		}
	}()
}
