package lfbus

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

func TestClientEventsAndPost(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "bus.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan string, 4)
	mux := http.NewServeMux()
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, ": hallo\n\nevent: volume\ndata: {\"volume\":42,\"known\":true}\n\nevent: claim\ndata: {\"source\":\"spotify\"}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	mux.HandleFunc("/volume", func(w http.ResponseWriter, r *http.Request) {
		var v map[string]int
		json.NewDecoder(r.Body).Decode(&v)
		got <- fmt.Sprint("volume ", v["volume"])
		w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("/state", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`{"error":"kaputt"}`))
	})
	go http.Serve(ln, mux)

	c := New(sock)
	vol := make(chan Volume, 1)
	claim := make(chan Claim, 1)
	conn := make(chan bool, 1)
	c.On("volume", func(b json.RawMessage) { var v Volume; json.Unmarshal(b, &v); vol <- v })
	c.On("claim", func(b json.RawMessage) { var v Claim; json.Unmarshal(b, &v); claim <- v })
	c.OnConnect(func() { conn <- true })
	go c.Run()
	select {
	case v := <-vol:
		if v.Volume != 42 || !v.Known {
			t.Fatalf("%+v", v)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("kein Ereignis")
	}
	if (<-claim).Source != "spotify" || !<-conn || !c.Connected() {
		t.Fatal("claim/connect")
	}
	if err := c.Post("/volume", map[string]int{"volume": 7}, nil); err != nil || <-got != "volume 7" {
		t.Fatalf("Post: %v", err)
	}
	if err := c.Get("/state", &Volume{}); err == nil || err.Error() != "kaputt" {
		t.Fatalf("Fehler: %v", err)
	}
}
