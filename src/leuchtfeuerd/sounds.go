package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Klänge austauschen.
//
// Hersteller-Klänge (Startton, Fehlerton, Kopplung ...): Die Hersteller-Software spielt WAV-Dateien aus dem
// schreibgeschützten System. leuchtfeuerd sucht sie (hw.SoundDirs), wandelt eine hochgeladene Datei ins Format des
// Originals (Abtastrate, Kanäle, 16 bit) und legt sie per Bind-Mount darüber; "Original" hängt sie wieder ab. Die
// Zuordnung steht in sounds/vendor.map (Zeilen "<Original>\t<Datei>"), der Hook hängt sie beim Start ein, bevor er
// die Hersteller-Dienste neu startet (hook.sh: sounds_mount).
//
// Eigene Töne (Wecker, Timer, Gong, Türklingel, Piep): statt der erzeugten Tonfolge spielt der Player
// sounds/tone-<name>.wav (48 kHz, Stereo, 16 bit).
//
// Hochladen: WAV direkt; MP3, OGG, FLAC u. a. dekodiert GStreamer des Geräts. Höchstens 30 s (Hersteller) bzw. 60 s.

var toneIDs = []string{"alarm", "timer", "chime", "bell", "beep"}

func soundDir() string { return filepath.Join(dataDir, "sounds") }

// toneID ordnet eine Tonfolge ihrem Namen zu (die Folgen sind feste Paketvariablen).
func toneID(seq []note) string {
	if len(seq) == 0 {
		return ""
	}
	for id, s := range map[string][]note{"alarm": toneAlarm, "timer": toneTimer, "chime": toneChime, "bell": toneBell, "beep": toneBeep} {
		if &s[0] == &seq[0] {
			return id
		}
	}
	return ""
}

// customTone liefert die eigene Datei eines Tons ("" = erzeugte Tonfolge).
func customTone(seq []note) string {
	id := toneID(seq)
	if id == "" {
		return ""
	}
	p := filepath.Join(soundDir(), "tone-"+id+".wav")
	if st, err := os.Stat(p); err == nil && st.Size() > 44 {
		return p
	}
	return ""
}

// ---- WAV ----

type pcmFormat struct {
	Rate     int `json:"rate"`
	Channels int `json:"channels"`
	Bits     int `json:"bits"`
}

type pcmData struct {
	pcmFormat
	frames [][]float64 // [Kanal][Abtastwert], -1 ... 1
}

func (p pcmData) seconds() float64 {
	if p.Rate == 0 || len(p.frames) == 0 {
		return 0
	}
	return float64(len(p.frames[0])) / float64(p.Rate)
}

// parseWAV liest PCM-WAV (8/16/24/32 bit Ganzzahl, 32 bit Gleitkomma).
func parseWAV(b []byte) (pcmData, error) {
	var d pcmData
	if len(b) < 12 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return d, fmt.Errorf("kein WAV")
	}
	var format uint16
	var data []byte
	for off := 12; off+8 <= len(b); {
		id, n := string(b[off:off+4]), int(binary.LittleEndian.Uint32(b[off+4:]))
		body := b[off+8:]
		if n > len(body) {
			n = len(body)
		}
		switch id {
		case "fmt ":
			if n < 16 {
				return d, fmt.Errorf("WAV: fmt zu kurz")
			}
			format = binary.LittleEndian.Uint16(body[0:])
			d.Channels = int(binary.LittleEndian.Uint16(body[2:]))
			d.Rate = int(binary.LittleEndian.Uint32(body[4:]))
			d.Bits = int(binary.LittleEndian.Uint16(body[14:]))
			if format == 0xFFFE && n >= 26 { // WAVE_FORMAT_EXTENSIBLE: Untertyp
				format = binary.LittleEndian.Uint16(body[24:])
			}
		case "data":
			data = body[:n]
		}
		off += 8 + n + n%2
	}
	if d.Channels < 1 || d.Channels > 8 || d.Rate < 4000 || d.Rate > 192000 || data == nil {
		return d, fmt.Errorf("WAV: Format nicht lesbar")
	}
	bps := d.Bits / 8
	if (format != 1 && format != 3) || (format == 3 && d.Bits != 32) || bps < 1 || bps > 4 {
		return d, fmt.Errorf("WAV: nur PCM (8/16/24/32 bit) oder Gleitkomma")
	}
	n := len(data) / (bps * d.Channels)
	d.frames = make([][]float64, d.Channels)
	for c := range d.frames {
		d.frames[c] = make([]float64, n)
	}
	for i := 0; i < n; i++ {
		for c := 0; c < d.Channels; c++ {
			o := (i*d.Channels + c) * bps
			var v float64
			switch {
			case format == 3:
				v = float64(math.Float32frombits(binary.LittleEndian.Uint32(data[o:])))
			case bps == 1:
				v = (float64(data[o]) - 128) / 128
			case bps == 2:
				v = float64(int16(binary.LittleEndian.Uint16(data[o:]))) / 32768
			case bps == 3:
				x := int32(data[o]) | int32(data[o+1])<<8 | int32(int8(data[o+2]))<<16
				v = float64(x) / 8388608
			default:
				v = float64(int32(binary.LittleEndian.Uint32(data[o:]))) / 2147483648
			}
			d.frames[c][i] = v
		}
	}
	return d, nil
}

// convert wandelt Abtastrate (linear) und Kanäle (Mitteln bzw. Verdoppeln).
func (p pcmData) convert(f pcmFormat) pcmData {
	out := pcmData{pcmFormat: f}
	if len(p.frames) == 0 {
		return out
	}
	n := len(p.frames[0])
	m := n
	if p.Rate != f.Rate {
		m = int(int64(n) * int64(f.Rate) / int64(p.Rate))
	}
	mono := make([]float64, n)
	for c := range p.frames {
		for i, v := range p.frames[c] {
			mono[i] += v / float64(len(p.frames))
		}
	}
	src := func(c int) []float64 {
		if f.Channels == len(p.frames) {
			return p.frames[c]
		}
		if f.Channels == 2 && len(p.frames) == 1 {
			return p.frames[0]
		}
		return mono
	}
	out.frames = make([][]float64, f.Channels)
	for c := range out.frames {
		s := src(c)
		o := make([]float64, m)
		for i := range o {
			x := float64(i) * float64(p.Rate) / float64(f.Rate)
			j := int(x)
			if j >= n-1 {
				o[i] = s[n-1]
				continue
			}
			fr := x - float64(j)
			o[i] = s[j]*(1-fr) + s[j+1]*fr
		}
		out.frames[c] = o
	}
	return out
}

// wav schreibt 16-bit-PCM (oder 8/24/32 bit, wie das Original).
func (p pcmData) wav() []byte {
	bps := p.Bits / 8
	if bps < 1 || bps > 4 {
		bps, p.Bits = 2, 16
	}
	n := 0
	if len(p.frames) > 0 {
		n = len(p.frames[0])
	}
	data := make([]byte, n*bps*p.Channels)
	for i := 0; i < n; i++ {
		for c := 0; c < p.Channels; c++ {
			v := math.Max(-1, math.Min(1, p.frames[c][i]))
			o := (i*p.Channels + c) * bps
			switch bps {
			case 1:
				data[o] = byte(int(v*127) + 128)
			case 2:
				binary.LittleEndian.PutUint16(data[o:], uint16(int16(math.Round(v*32767))))
			case 3:
				x := int32(math.Round(v * 8388607))
				data[o], data[o+1], data[o+2] = byte(x), byte(x>>8), byte(x>>16)
			default:
				binary.LittleEndian.PutUint32(data[o:], uint32(int32(math.Round(v*2147483647))))
			}
		}
	}
	var b bytes.Buffer
	w := func(v any) { binary.Write(&b, binary.LittleEndian, v) }
	b.WriteString("RIFF")
	w(uint32(36 + len(data)))
	b.WriteString("WAVEfmt ")
	w(uint32(16))
	w(uint16(1))
	w(uint16(p.Channels))
	w(uint32(p.Rate))
	w(uint32(p.Rate * p.Channels * bps))
	w(uint16(p.Channels * bps))
	w(uint16(p.Bits))
	b.WriteString("data")
	w(uint32(len(data)))
	b.Write(data)
	return b.Bytes()
}

// decodeAudio: WAV direkt, sonst über GStreamer (decodebin) zu 48 kHz Stereo 16 bit.
var decodeOther = func(in []byte) (pcmData, error) {
	tmp, err := os.CreateTemp("", "lf-sound-*")
	if err != nil {
		return pcmData{}, err
	}
	defer os.Remove(tmp.Name())
	tmp.Write(in)
	tmp.Close()
	cmd := exec.Command("gst-launch-1.0", "-q", "filesrc", "location="+tmp.Name(), "!", "decodebin", "!", "audioconvert", "!",
		"audioresample", "!", "audio/x-raw,format=S16LE,rate=48000,channels=2,layout=interleaved", "!", "fdsink", "fd=1")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil || out.Len() < 4 {
		return pcmData{}, fmt.Errorf("Datei lässt sich nicht dekodieren (WAV, MP3, OGG oder FLAC)")
	}
	raw := out.Bytes()
	hdr := pcmData{pcmFormat: pcmFormat{Rate: 48000, Channels: 2, Bits: 16}}
	return parseWAV(append(hdr.wavHeader(len(raw)), raw...))
}

func (p pcmData) wavHeader(n int) []byte {
	h := pcmData{pcmFormat: p.pcmFormat}.wav()
	binary.LittleEndian.PutUint32(h[4:], uint32(36+n))
	binary.LittleEndian.PutUint32(h[40:], uint32(n))
	return h
}

func decodeAudio(in []byte) (pcmData, error) {
	if len(in) >= 12 && string(in[0:4]) == "RIFF" && string(in[8:12]) == "WAVE" {
		return parseWAV(in)
	}
	return decodeOther(in)
}

// ---- Hersteller-Klänge ----

type vendorSound struct {
	Path     string    `json:"path"`
	Name     string    `json:"name"`
	Format   pcmFormat `json:"format"`
	Seconds  float64   `json:"seconds"`
	Replaced bool      `json:"replaced"`
	Mounted  bool      `json:"mounted"`
}

type soundStore struct {
	mu      sync.Mutex
	scanned []vendorSound
	scanAt  time.Time
	mount   func(src, dst string) error // austauschbar für Tests
	umount  func(dst string) error
	mounted func(dst string) bool
}

func newSoundStore() *soundStore {
	return &soundStore{
		mount:  func(src, dst string) error { return exec.Command("mount", "--bind", src, dst).Run() },
		umount: func(dst string) error { return exec.Command("umount", dst).Run() },
		mounted: func(dst string) bool {
			b, _ := os.ReadFile("/proc/mounts")
			return bytes.Contains(b, []byte(" "+dst+" "))
		},
	}
}

// soundRoots: Suchorte für Tests; sonst die des Zielgeräts (hw.SoundDirs, bis 5 Ebenen tief).
var soundRoots []string

func vendorFile(path string) string {
	h := sha256.Sum256([]byte(path))
	return "v-" + hex.EncodeToString(h[:6]) + ".wav"
}

func (s *soundStore) readMap() map[string]string {
	m := map[string]string{}
	b, err := os.ReadFile(filepath.Join(soundDir(), "vendor.map"))
	if err != nil {
		return m
	}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		if k, v, ok := strings.Cut(sc.Text(), "\t"); ok {
			m[k] = v
		}
	}
	return m
}

func (s *soundStore) writeMap(m map[string]string) error {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s\t%s\n", k, m[k])
	}
	os.MkdirAll(soundDir(), 0o755)
	tmp := filepath.Join(soundDir(), "vendor.map.new")
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(soundDir(), "vendor.map"))
}

// Scan sucht WAV-Dateien der Hersteller-Software (Ergebnis 10 Minuten gültig).
func (s *soundStore) Scan(force bool) []vendorSound {
	s.mu.Lock()
	if !force && s.scanned != nil && time.Since(s.scanAt) < 10*time.Minute {
		out := s.withState(s.scanned)
		s.mu.Unlock()
		return out
	}
	s.mu.Unlock()
	var found []vendorSound
	roots := soundRoots
	if roots == nil {
		roots = hw.SoundDirs
	}
	for _, root := range roots {
		base := strings.Count(root, "/")
		filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if e.IsDir() {
				if strings.Count(p, "/")-base > 5 || strings.HasPrefix(p, dataDir) {
					return fs.SkipDir
				}
				return nil
			}
			if len(found) >= 300 || !strings.EqualFold(filepath.Ext(p), ".wav") {
				return nil
			}
			info, err := e.Info()
			if err != nil || info.Size() > 10<<20 || info.Size() < 44 {
				return nil
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			d, err := parseWAV(b)
			if err != nil {
				return nil
			}
			found = append(found, vendorSound{Path: p, Name: filepath.Base(p), Format: d.pcmFormat, Seconds: math.Round(d.seconds()*10) / 10})
			return nil
		})
	}
	sort.Slice(found, func(i, j int) bool { return found[i].Path < found[j].Path })
	s.mu.Lock()
	s.scanned, s.scanAt = found, time.Now()
	out := s.withState(found)
	s.mu.Unlock()
	return out
}

func (s *soundStore) withState(in []vendorSound) []vendorSound {
	m := s.readMap()
	out := make([]vendorSound, len(in))
	for i, v := range in {
		_, v.Replaced = m[v.Path]
		v.Mounted = v.Replaced && s.mounted(v.Path)
		out[i] = v
	}
	return out
}

func (s *soundStore) known(path string) (vendorSound, bool) {
	for _, v := range s.Scan(false) {
		if v.Path == path {
			return v, true
		}
	}
	return vendorSound{}, false
}

// ReplaceVendor wandelt die Datei ins Format des Originals und legt sie darüber.
func (s *soundStore) ReplaceVendor(path string, in []byte) error {
	v, ok := s.known(path)
	if !ok {
		return fmt.Errorf("unbekannter Klang %s", path)
	}
	d, err := decodeAudio(in)
	if err != nil {
		return err
	}
	if d.seconds() > 30 {
		return fmt.Errorf("höchstens 30 Sekunden (die Datei hat %.0f s)", d.seconds())
	}
	bits := v.Format.Bits
	if bits != 8 && bits != 24 && bits != 32 {
		bits = 16
	}
	out := d.convert(pcmFormat{Rate: v.Format.Rate, Channels: v.Format.Channels, Bits: bits}).wav()
	name := vendorFile(path)
	if s.mounted(path) {
		if err := s.umount(path); err != nil {
			return fmt.Errorf("alten Ersatz abhängen: %v", err)
		}
	}
	os.MkdirAll(soundDir(), 0o755)
	dst := filepath.Join(soundDir(), name)
	if err := os.WriteFile(dst+".new", out, 0o644); err != nil {
		return err
	}
	if err := os.Rename(dst+".new", dst); err != nil {
		return err
	}
	m := s.readMap()
	m[path] = name
	if err := s.writeMap(m); err != nil {
		return err
	}
	if err := s.mount(dst, path); err != nil {
		return fmt.Errorf("einhängen: %v", err)
	}
	log.Printf("Klang %s ersetzt (%.1f s, %d Hz, %d Kanäle)", path, d.seconds(), v.Format.Rate, v.Format.Channels)
	return nil
}

// ResetVendor stellt das Original wieder her.
func (s *soundStore) ResetVendor(path string) error {
	m := s.readMap()
	name, ok := m[path]
	if !ok {
		return nil
	}
	if s.mounted(path) {
		if err := s.umount(path); err != nil {
			return fmt.Errorf("abhängen: %v", err)
		}
	}
	delete(m, path)
	os.Remove(filepath.Join(soundDir(), name))
	log.Printf("Klang %s: wieder das Original", path)
	return s.writeMap(m)
}

// ---- eigene Töne ----

type toneInfo struct {
	ID       string  `json:"id"`
	Replaced bool    `json:"replaced"`
	Seconds  float64 `json:"seconds"`
}

func listTones() []toneInfo {
	out := []toneInfo{}
	for _, id := range toneIDs {
		t := toneInfo{ID: id}
		if b, err := os.ReadFile(filepath.Join(soundDir(), "tone-"+id+".wav")); err == nil {
			if d, err := parseWAV(b); err == nil {
				t.Replaced, t.Seconds = true, math.Round(d.seconds()*10)/10
			}
		}
		out = append(out, t)
	}
	return out
}

func validTone(id string) bool {
	for _, t := range toneIDs {
		if t == id {
			return true
		}
	}
	return false
}

func replaceTone(id string, in []byte) error {
	if !validTone(id) {
		return fmt.Errorf("unbekannter Ton %q", id)
	}
	d, err := decodeAudio(in)
	if err != nil {
		return err
	}
	if d.seconds() > 60 {
		return fmt.Errorf("höchstens 60 Sekunden (die Datei hat %.0f s)", d.seconds())
	}
	os.MkdirAll(soundDir(), 0o755)
	dst := filepath.Join(soundDir(), "tone-"+id+".wav")
	if err := os.WriteFile(dst+".new", d.convert(pcmFormat{Rate: 48000, Channels: 2, Bits: 16}).wav(), 0o644); err != nil {
		return err
	}
	log.Printf("Ton %s ersetzt (%.1f s)", id, d.seconds())
	return os.Rename(dst+".new", dst)
}

func resetTone(id string) error {
	if !validTone(id) {
		return fmt.Errorf("unbekannter Ton %q", id)
	}
	err := os.Remove(filepath.Join(soundDir(), "tone-"+id+".wav"))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// soundSource liefert die Datei eines Klangs zum Anhören: "tone:<id>" oder "vendor:<Pfad>"; original = das unveränderte.
func (s *soundStore) soundSource(target string, original bool) (string, []byte, error) {
	switch {
	case strings.HasPrefix(target, "tone:"):
		id := strings.TrimPrefix(target, "tone:")
		if !validTone(id) {
			return "", nil, fmt.Errorf("unbekannter Ton")
		}
		if !original {
			if b, err := os.ReadFile(filepath.Join(soundDir(), "tone-"+id+".wav")); err == nil {
				return id + ".wav", b, nil
			}
		}
		seq := map[string][]note{"alarm": toneAlarm, "timer": toneTimer, "chime": toneChime, "bell": toneBell, "beep": toneBeep}[id]
		var raw []byte
		for _, n := range seq {
			raw = append(raw, synth(n)...)
		}
		return id + ".wav", append(pcmData{pcmFormat: pcmFormat{Rate: 48000, Channels: 2, Bits: 16}}.wavHeader(len(raw)), raw...), nil
	case strings.HasPrefix(target, "vendor:"):
		p := strings.TrimPrefix(target, "vendor:")
		if _, ok := s.known(p); !ok {
			return "", nil, fmt.Errorf("unbekannter Klang")
		}
		if !original {
			if name, ok := s.readMap()[p]; ok {
				b, err := os.ReadFile(filepath.Join(soundDir(), name))
				return filepath.Base(p), b, err
			}
		}
		// das Original liegt unter dem Bind-Mount: über einen zweiten Blick auf das Dateisystem ist es nicht
		// erreichbar, deshalb kurz abhängen ist zu riskant - Original nur, wenn nicht ersetzt
		if s.mounted(p) {
			return "", nil, fmt.Errorf("das Original ist gerade überdeckt; erst auf Original zurückstellen")
		}
		b, err := os.ReadFile(p)
		return filepath.Base(p), b, err
	}
	return "", nil, fmt.Errorf("Ziel tone:<Name> oder vendor:<Pfad>")
}

// limitReader für Uploads
func readUpload(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, 12<<20+1))
	if err != nil {
		return nil, err
	}
	if len(b) > 12<<20 {
		return nil, fmt.Errorf("Datei zu groß (höchstens 12 MB)")
	}
	return b, nil
}
