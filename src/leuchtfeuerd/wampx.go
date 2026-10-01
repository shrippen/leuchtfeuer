package main

import "leuchtfeuer/wamp"

// Gemeinsamer WAMP-Client (src/wamp): kurze Namen für diesen Kern.
type hub = wamp.Hub

func newHub() *hub                  { return wamp.NewHub(wamp.Addr) }
func toInt(v any) int               { return wamp.ToInt(v) }
func toStrMap(v any) map[string]any { return wamp.ToStrMap(v) }
