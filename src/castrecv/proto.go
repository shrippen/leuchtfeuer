package main

import (
	"encoding/binary"
	"errors"
	"io"
)

// castMessage ist die Protobuf-Nachricht cast_channel.CastMessage (proto2), von Hand kodiert:
// 1 protocol_version (Enum, 0 = CASTV2_1_0), 2 source_id, 3 destination_id, 4 namespace,
// 5 payload_type (0 = STRING, 1 = BINARY), 6 payload_utf8, 7 payload_binary.
type castMessage struct {
	Source, Dest, Namespace string
	Binary                  bool
	Payload                 []byte // UTF-8-Text oder Binärdaten
}

func appendVarint(b []byte, v uint64) []byte {
	for v >= 0x80 {
		b = append(b, byte(v)|0x80)
		v >>= 7
	}
	return append(b, byte(v))
}

func appendBytesField(b []byte, field int, data []byte) []byte {
	b = appendVarint(b, uint64(field<<3|2))
	b = appendVarint(b, uint64(len(data)))
	return append(b, data...)
}

func (m *castMessage) marshal() []byte {
	var b []byte
	b = appendVarint(b, 1<<3) // protocol_version = 0
	b = appendVarint(b, 0)
	b = appendBytesField(b, 2, []byte(m.Source))
	b = appendBytesField(b, 3, []byte(m.Dest))
	b = appendBytesField(b, 4, []byte(m.Namespace))
	b = appendVarint(b, 5<<3)
	if m.Binary {
		b = appendVarint(b, 1)
		return appendBytesField(b, 7, m.Payload)
	}
	b = appendVarint(b, 0)
	return appendBytesField(b, 6, m.Payload)
}

func unmarshalCast(b []byte) (*castMessage, error) {
	m := &castMessage{}
	for len(b) > 0 {
		key, n := readVarint(b)
		if n == 0 {
			return nil, errors.New("varint")
		}
		b = b[n:]
		field, wire := int(key>>3), int(key&7)
		switch wire {
		case 0:
			v, n := readVarint(b)
			if n == 0 {
				return nil, errors.New("varint")
			}
			b = b[n:]
			if field == 5 {
				m.Binary = v == 1
			}
		case 2:
			l, n := readVarint(b)
			if n == 0 || uint64(len(b)-n) < l {
				return nil, errors.New("länge")
			}
			data := b[n : n+int(l)]
			b = b[n+int(l):]
			switch field {
			case 2:
				m.Source = string(data)
			case 3:
				m.Dest = string(data)
			case 4:
				m.Namespace = string(data)
			case 6, 7:
				m.Payload = append([]byte(nil), data...)
			}
		default:
			return nil, errors.New("wire type")
		}
	}
	return m, nil
}

func readVarint(b []byte) (uint64, int) {
	var v uint64
	for i := 0; i < len(b) && i < 10; i++ {
		v |= uint64(b[i]&0x7f) << (7 * uint(i))
		if b[i] < 0x80 {
			return v, i + 1
		}
	}
	return 0, 0
}

// readFrame liest eine Nachricht: 4 Byte Länge (big endian) + Protobuf.
func readFrame(r io.Reader) (*castMessage, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n > 1<<20 {
		return nil, errors.New("Nachricht zu groß")
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return unmarshalCast(buf)
}

func writeFrame(w io.Writer, m *castMessage) error {
	body := m.marshal()
	out := make([]byte, 4, 4+len(body))
	binary.BigEndian.PutUint32(out, uint32(len(body)))
	_, err := w.Write(append(out, body...))
	return err
}
