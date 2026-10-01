//go:build !demo

package main

// Der Demo-Modus (nur zum Erstellen von Screenshots) ist im normalen Build nicht enthalten.
const demoMode = false

func demoRequested() bool { return false }
func runDemo(string)      {}
