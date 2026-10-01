package main

import (
	"encoding/json"
	"os"
)

// btagent schreibt seinen Zustand nach /run/leuchtfeuer-bt-state.json (Pairing-Fenster offen oder nicht) und nimmt
// Befehle über das WAMP-Thema "leuchtfeuer.bt.pairing" entgegen ("toggle" | "open" | "close").

type btState struct {
	Open  bool  `json:"open"`
	Until int64 `json:"until"`
}

func readBT() btState {
	var s btState
	if b, err := os.ReadFile(btStateFile); err == nil {
		json.Unmarshal(b, &s)
	}
	return s
}

func btPairingOpen() bool { return readBT().Open }
