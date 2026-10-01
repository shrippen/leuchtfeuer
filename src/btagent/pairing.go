package main

import (
	"log"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

// Pairing-Fenster: Der Lautsprecher ist normalerweise weder sichtbar noch koppelbar (schon gekoppelte,
// vertraute Geräte verbinden sich trotzdem von selbst wieder). Ein kurzer Druck auf den Bluetooth-Knopf
// öffnet das Fenster für -pairing-window; ein zweiter Druck schließt es früher. Nach einer erfolgreichen
// neuen Kopplung schließt es sich kurz darauf von selbst.
//
// Der Knopf meldet sich über den WAMP-Router des Invoke (mcu-interface -> com.harman.test.inputEvent
// ["bluetooth", "0"]; ein langer Druck erzeugt kein Ereignis). Rückmeldung über den Leuchtring:
// com.harman.ledAnimate mit vorhandenen Animationen (eine eigene Pairing-Animation gibt es nicht).

const (
	ledOpen   = "L_106_c_success"  // Fenster geöffnet / Gerät gekoppelt
	ledClosed = "L_312_d_shorttap" // Fenster geschlossen
)

type pairingWindow struct {
	mu      sync.Mutex
	always  bool
	open    bool
	until   time.Time
	known   map[string]bool // beim Öffnen bereits gekoppelte Geräte
	settled time.Time       // Zeitpunkt einer neuen Kopplung (Fenster schließt kurz danach)
}

var pw = &pairingWindow{}

func (p *pairingWindow) isOpen() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.always || p.open
}

// pairedSet liefert die Adressen aller gekoppelten Geräte.
func pairedSet() map[string]bool {
	set := map[string]bool{}
	var objs map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	if err := bus.Object(bluez, "/").Call("org.freedesktop.DBus.ObjectManager.GetManagedObjects", 0).Store(&objs); err != nil {
		return set
	}
	for _, ifs := range objs {
		if d, ok := ifs["org.bluez.Device1"]; ok {
			if paired, _ := d["Paired"].Value().(bool); paired {
				if a, _ := d["Address"].Value().(string); a != "" {
					set[a] = true
				}
			}
		}
	}
	return set
}

// toggle wird vom Knopf aufgerufen.
func (p *pairingWindow) toggle(window time.Duration) {
	p.mu.Lock()
	if p.always {
		p.mu.Unlock()
		return
	}
	if p.open {
		p.open = false
		p.mu.Unlock()
		log.Printf("Pairing-Fenster per Knopf geschlossen")
		led(ledClosed)
		return
	}
	p.mu.Unlock()
	known := pairedSet()
	p.mu.Lock()
	p.open, p.until, p.known, p.settled = true, time.Now().Add(window), known, time.Time{}
	p.mu.Unlock()
	log.Printf("Pairing-Fenster geöffnet für %s (Bluetooth-Knopf)", window)
	led(ledOpen)
}

// check läuft im tick(): schließt das Fenster nach Ablauf oder kurz nach einer neuen Kopplung.
func (p *pairingWindow) check() {
	p.mu.Lock()
	if !p.open {
		p.mu.Unlock()
		return
	}
	expired := time.Now().After(p.until)
	known := p.known
	settled := p.settled
	p.mu.Unlock()
	if expired {
		p.mu.Lock()
		p.open = false
		p.mu.Unlock()
		log.Printf("Pairing-Fenster abgelaufen")
		led(ledClosed)
		return
	}
	if settled.IsZero() {
		for a := range pairedSet() {
			if !known[a] {
				p.mu.Lock()
				p.settled = time.Now()
				p.mu.Unlock()
				log.Printf("Neues Gerät gekoppelt (%s): Fenster schließt in 5 s", a)
				led(ledOpen)
				return
			}
		}
	} else if time.Since(settled) > 5*time.Second {
		p.mu.Lock()
		p.open = false
		p.mu.Unlock()
		log.Printf("Pairing-Fenster nach Kopplung geschlossen")
	}
}

// led spielt eine Leuchtring-Animation ab (best effort; läuft nur, wenn die WAMP-Verbindung steht).
func led(pattern string) {
	vs.mu.Lock()
	w := vs.wamp
	vs.mu.Unlock()
	if w == nil {
		return
	}
	go func() {
		if _, err := w.call("com.harman.ledAnimate", pattern); err != nil {
			log.Printf("ledAnimate %s: %v", pattern, err)
		}
	}()
}
