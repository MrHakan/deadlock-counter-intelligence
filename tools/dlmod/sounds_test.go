package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MrHakan/deadlock-modkit/tools/dlmod/source2"
)

func testWAV(t *testing.T, samples int) []byte {
	t.Helper()
	var out bytes.Buffer
	out.WriteString("RIFF")
	binary.Write(&out, binary.LittleEndian, uint32(36+samples*2))
	out.WriteString("WAVEfmt ")
	for _, field := range []any{uint32(16), uint16(1), uint16(1), uint32(44100), uint32(88200), uint16(2), uint16(16)} {
		binary.Write(&out, binary.LittleEndian, field)
	}
	out.WriteString("data")
	binary.Write(&out, binary.LittleEndian, uint32(samples*2))
	out.Write(make([]byte, samples*2))
	return out.Bytes()
}

func TestReplaceGameSoundsFromTheSoundsScreen(t *testing.T) {
	game := newFakeGame(t)
	original, _, err := source2.CompileSound(testWAV(t, 10))
	if err != nil {
		t.Fatal(err)
	}
	game.write("game/citadel/pak01_dir.vpk", source2.WriteVPK(map[string][]byte{
		"sounds/vo/haze/haze_kill_01.vsnd_c":            original,
		"sounds/abilities/atlas/shoulder_charge.vsnd_c": original,
		"sounds/ui/menu_click.vsnd_c":                   original,
		"models/heroes/haze/haze.vmdl_c":                []byte("not a sound"),
	}))
	played := ""
	saved := openInPlayer
	openInPlayer = func(file string) error { played = file; return nil }
	defer func() { openInPlayer = saved }()

	ui := newTestTUI(t, game.Game)
	ui.handle([]byte("s"))
	screen := ui.screen()
	if ui.mode != modeSounds || !strings.Contains(screen, "3 sounds") || strings.Contains(screen, "haze.vmdl") {
		t.Fatalf("the sounds screen lists the game's sounds only:\n%s", screen)
	}
	ui.handle([]byte("\x1b[C")) // first hero by name: Abrams (atlas)
	if screen = ui.screen(); !strings.Contains(screen, "Hero Abrams") || !strings.Contains(screen, "shoulder_charge") ||
		strings.Contains(screen, "haze_kill") {
		t.Fatalf("hero filter with the player-facing name:\n%s", screen)
	}
	ui.handle([]byte("\x1b[C")) // Haze
	ui.handle([]byte("\r"))
	if !strings.HasSuffix(played, "haze_kill_01.wav") {
		t.Fatalf("Enter plays the game's own sound as a WAV: %q", played)
	}

	replacement := filepath.Join(t.TempDir(), "my line.wav")
	os.WriteFile(replacement, testWAV(t, 200), 0o644)
	ui.handle([]byte("\x1b[200~" + replacement + "\x1b[201~")) // dropped onto the window
	screen = ui.screen()
	if !strings.Contains(screen, "haze_kill_01.vsnd_c now plays my line.wav") || !strings.Contains(screen, "← my line.wav") {
		t.Fatalf("a dropped file replaces the selected sound:\n%s", screen)
	}
	store, _ := openStore(game.Game)
	entries, _ := store.entries()
	if len(entries) != 1 || entries[0].Name() != customSoundsName {
		t.Fatalf("the Custom sounds mod is installed: %v", entries)
	}
	installed, err := source2.OpenVPK(filepath.Join(game.Citadel(), "addons", "pak01_dir.vpk"))
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := installed.Read("sounds/vo/haze/haze_kill_01.vsnd_c")
	if err != nil {
		t.Fatal(err)
	}
	if _, info, err := source2.CompileSound(mustPlayable(t, compiled)); err != nil || info.Samples != 200 {
		t.Fatalf("the mod holds the compiled replacement: %v %+v", err, info)
	}
	ui.handle([]byte("p"))
	if !strings.Contains(played, "listen") {
		t.Fatalf("p plays the replacement: %q", played)
	}

	ui.handle([]byte("x"))
	if entries, _ := store.entries(); len(entries) != 0 {
		t.Fatalf("restoring the last sound removes the mod: %v", entries)
	}
	if project, _ := loadSoundProject(game.Game); len(project.Replaced) != 0 {
		t.Fatal("the list of replaced sounds is saved")
	}
	ui.handle([]byte("\x1b"))
	if ui.mode != modeList {
		t.Fatal("Esc returns to the mods")
	}
}

func mustPlayable(t *testing.T, compiled []byte) []byte {
	t.Helper()
	audio, _, err := source2.PlayableSound(compiled)
	if err != nil {
		t.Fatal(err)
	}
	return audio
}
