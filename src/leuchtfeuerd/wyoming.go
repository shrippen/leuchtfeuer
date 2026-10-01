package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

// Wyoming-Protokoll (Home Assistant Assist, rhasspy/wyoming): Jedes Ereignis ist eine JSON-Kopfzeile
// {"type", "version", "data_length", "payload_length"} mit "\n", danach optional data_length Bytes JSON-Daten und
// payload_length Bytes Nutzlast (Ton: PCM s16le). Home Assistant schickt die Daten immer als eigenen Abschnitt; ältere
// Programme schreiben sie in die Kopfzeile ("data"). Gelesen wird beides (der Abschnitt ergänzt die Kopfzeile).

const wyomingVersion = "1.10.2"

type wyEvent struct {
	Type    string
	Data    map[string]any
	Payload []byte
}

func readWyoming(r *bufio.Reader) (wyEvent, error) {
	line, err := r.ReadBytes('\n')
	if err != nil {
		return wyEvent{}, err
	}
	var hdr struct {
		Type          string         `json:"type"`
		Data          map[string]any `json:"data"`
		DataLength    int            `json:"data_length"`
		PayloadLength int            `json:"payload_length"`
	}
	if err := json.Unmarshal(line, &hdr); err != nil {
		return wyEvent{}, fmt.Errorf("Wyoming-Kopfzeile: %v", err)
	}
	if hdr.DataLength < 0 || hdr.DataLength > 1<<20 || hdr.PayloadLength < 0 || hdr.PayloadLength > 4<<20 {
		return wyEvent{}, fmt.Errorf("Wyoming: Länge außerhalb der Grenzen")
	}
	ev := wyEvent{Type: hdr.Type, Data: hdr.Data}
	if ev.Data == nil {
		ev.Data = map[string]any{}
	}
	if hdr.DataLength > 0 {
		b := make([]byte, hdr.DataLength)
		if _, err := io.ReadFull(r, b); err != nil {
			return wyEvent{}, err
		}
		var extra map[string]any
		if err := json.Unmarshal(b, &extra); err == nil {
			for k, v := range extra {
				ev.Data[k] = v
			}
		}
	}
	if hdr.PayloadLength > 0 {
		ev.Payload = make([]byte, hdr.PayloadLength)
		if _, err := io.ReadFull(r, ev.Payload); err != nil {
			return wyEvent{}, err
		}
	}
	return ev, nil
}

// writeWyoming schreibt ein Ereignis (Daten als eigener Abschnitt, wie Home Assistant).
func writeWyoming(w io.Writer, typ string, data map[string]any, payload []byte) error {
	hdr := map[string]any{"type": typ, "version": wyomingVersion}
	var db []byte
	if len(data) > 0 {
		db, _ = json.Marshal(data)
		hdr["data_length"] = len(db)
	}
	if len(payload) > 0 {
		hdr["payload_length"] = len(payload)
	}
	hb, _ := json.Marshal(hdr)
	buf := make([]byte, 0, len(hb)+1+len(db)+len(payload))
	buf = append(append(append(append(buf, hb...), '\n'), db...), payload...)
	_, err := w.Write(buf)
	return err
}

func wyInt(m map[string]any, k string, def int) int {
	if v, ok := m[k].(float64); ok {
		return int(v)
	}
	return def
}

func wyStr(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return s
}
