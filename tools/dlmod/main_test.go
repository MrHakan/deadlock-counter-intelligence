package main

import (
	"os"
	"testing"
)

// TestMain makes the tests ignore a Deadlock that is open on this computer: they work on fake
// game folders, and a running game would make dlmod refuse to change mods.
func TestMain(m *testing.M) {
	gameRunning = func() bool { return false }
	os.Exit(m.Run())
}
