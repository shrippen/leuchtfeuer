package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	"github.com/libdns/libdns"
)

// fakeNetcup: Zone mit Einträgen; login/logout/infoDnsRecords/updateDnsRecords wie der CCP-Webservice.
type fakeNetcup struct {
	mu      sync.Mutex
	recs    []ncRecord
	next    int
	actions []string
}

func (f *fakeNetcup) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action string `json:"action"`
		Param  struct {
			Password string      `json:"apipassword"`
			Set      ncRecordSet `json:"dnsrecordset"`
		} `json:"param"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actions = append(f.actions, req.Action)
	ok := func(data any) { json.NewEncoder(w).Encode(map[string]any{"status": "success", "responsedata": data}) }
	switch req.Action {
	case "login":
		if req.Param.Password != "pw" {
			json.NewEncoder(w).Encode(map[string]any{"status": "error", "shortmessage": "Login failed"})
			return
		}
		ok(map[string]string{"apisessionid": "sid"})
	case "logout":
		ok("")
	case "infoDnsRecords":
		ok(ncRecordSet{f.recs})
	case "updateDnsRecords":
		for _, n := range req.Param.Set.Records {
			if n.Delete {
				for i, o := range f.recs {
					if o.ID == n.ID {
						f.recs = append(f.recs[:i], f.recs[i+1:]...)
						break
					}
				}
				continue
			}
			f.next++
			n.ID = strconv.Itoa(f.next)
			f.recs = append(f.recs, n)
		}
		ok(ncRecordSet{f.recs})
	}
}

func TestNetcupReplacesStaleChallenge(t *testing.T) {
	f := &fakeNetcup{recs: []ncRecord{
		{ID: "a", Hostname: "invoke", Type: "A", Destination: "192.168.1.5"},
		{ID: "b", Hostname: "_acme-challenge.invoke", Type: "TXT", Destination: "alt-1"},
		{ID: "c", Hostname: "_acme-challenge.invoke", Type: "TXT", Destination: "alt-2"},
		{ID: "d", Hostname: "_acme-challenge.anders", Type: "TXT", Destination: "bleibt"},
	}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	p := &netcupDNS{Customer: "1", Key: "k", Password: "pw", BaseURL: srv.URL}
	ctx := context.Background()
	rec := libdns.RR{Type: "TXT", Name: "_acme-challenge.invoke", Data: "neu"}
	if got, err := p.AppendRecords(ctx, "example.de.", []libdns.Record{rec}); err != nil || len(got) != 1 {
		t.Fatalf("Append: %v %v", got, err)
	}
	var txt []string
	for _, r := range f.recs {
		if r.Hostname == "_acme-challenge.invoke" {
			txt = append(txt, r.Destination)
		}
	}
	if len(txt) != 1 || txt[0] != "neu" || len(f.recs) != 3 {
		t.Fatalf("alte Challenge-Einträge nicht ersetzt: %+v", f.recs)
	}
	if got, err := p.DeleteRecords(ctx, "example.de.", []libdns.Record{rec}); err != nil || len(got) != 1 {
		t.Fatalf("Delete: %v %v", got, err)
	}
	if len(f.recs) != 2 {
		t.Fatalf("nach Delete: %+v", f.recs)
	}
	// schon weg: kein Fehler, keine Änderung
	if got, err := p.DeleteRecords(ctx, "example.de.", []libdns.Record{rec}); err != nil || len(got) != 0 {
		t.Fatalf("Delete doppelt: %v %v", got, err)
	}
	if f.actions[len(f.actions)-1] != "logout" {
		t.Fatalf("keine Abmeldung: %v", f.actions)
	}
	p.Password = "falsch"
	if _, err := p.AppendRecords(ctx, "example.de.", []libdns.Record{rec}); err == nil {
		t.Fatal("falsches Passwort: Fehler erwartet")
	}
	if _, err := p.AppendRecords(ctx, "example.de.", []libdns.Record{libdns.RR{Type: "A", Name: "x", Data: "1.2.3.4"}}); err == nil {
		t.Fatal("A-Eintrag: Fehler erwartet")
	}
}
