package main

import (
	"encoding/json"
	"os"
)

// btagent schreibt seinen Zustand nach /run/invoke-bt-state.json (Pairing-Fenster offen oder nicht) und nimmt
// Befehle über das WAMP-Thema "invoke.bt.pairing" entgegen ("toggle" | "open" | "close").

type btState struct {
	Open  bool  `json:"open"`
	Until int64 `json:"until"`
}

func readBT() btState {
	var s btState
	if b, err := os.ReadFile("/run/invoke-bt-state.json"); err == nil {
		json.Unmarshal(b, &s)
	}
	return s
}

func btPairingOpen() bool { return readBT().Open }
