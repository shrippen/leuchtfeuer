package main

import (
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// Hardware-Watchdog (Aufruf vom Hook: invoked -watchdog <Lebenszeichen-Datei> [-watchdog-dev /dev/watchdog]).
// Öffnet den Watchdog, setzt die Frist auf 60 s und füttert ihn alle 5 s - aber nur, solange der Hook lebt: Er schreibt
// bei jedem Durchlauf (alle 30 s) die Laufzeit des Geräts in die Datei. Ist das Lebenszeichen älter als 150 s, hört
// das Füttern auf und das Gerät startet nach der Frist neu. Bei SIGTERM (Watchdog ausgeschaltet) wird er mit dem
// Zeichen "V" ordentlich geschlossen (magic close), damit kein Neustart folgt.

const (
	wdiocKeepalive  = 0x80045705 // _IOR('W', 5, int)
	wdiocSetTimeout = 0xc0045706 // _IOWR('W', 6, int)
	wdTimeout       = 60
	wdMaxAge        = 150
)

func readUptime(path string) (int, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	f := strings.Fields(string(b))
	if len(f) == 0 {
		return 0, false
	}
	v, err := strconv.Atoi(strings.SplitN(f[0], ".", 2)[0])
	return v, err == nil
}

// heartbeatFresh: Lebenszeichen höchstens wdMaxAge Sekunden alt (gemessen an der Laufzeit, unabhängig von der Uhr).
func heartbeatFresh(alive, uptime string) bool {
	hb, ok1 := readUptime(alive)
	now, ok2 := readUptime(uptime)
	return ok1 && ok2 && now-hb <= wdMaxAge && now >= hb
}

func runWatchdog(alive, dev string) {
	log.SetFlags(log.LstdFlags)
	f, err := os.OpenFile(dev, os.O_WRONLY, 0)
	if err != nil {
		log.Fatalf("Watchdog %s: %v", dev, err)
	}
	t := int32(wdTimeout)
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), wdiocSetTimeout, uintptr(unsafe.Pointer(&t))); e != 0 {
		log.Printf("Watchdog: Frist lässt sich nicht setzen (%v), Gerätevorgabe gilt", e)
	} else {
		log.Printf("Watchdog: Frist %d s", t)
	}
	feed := func() {
		if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), wdiocKeepalive, 0); e != 0 {
			f.Write([]byte{'.'})
		}
	}
	feed()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	starving := false
	for {
		select {
		case <-sig:
			f.Write([]byte{'V'})
			f.Close()
			log.Printf("Watchdog ordentlich geschlossen")
			return
		case <-tick.C:
			if heartbeatFresh(alive, "/proc/uptime") {
				if starving {
					log.Printf("Watchdog: Hook lebt wieder")
					starving = false
				}
				feed()
			} else if !starving {
				log.Printf("Watchdog: kein Lebenszeichen vom Hook - Gerät startet in %d s neu", wdTimeout)
				starving = true
			}
		}
	}
}
