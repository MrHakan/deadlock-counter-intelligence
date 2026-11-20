package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestParseKeys(t *testing.T) {
	got := parseKeys([]byte("\x1b[A\x1bOB\x1b[5~\x1b[3~\x1b[1;5C\r\nş \x7f\x03\x1b"))
	var names []string
	for _, k := range got {
		if k.name == "char" {
			names = append(names, string(k.r))
		} else {
			names = append(names, k.name)
		}
	}
	want := []string{"up", "down", "pgup", "delete", "right", "enter", "ş", " ", "backspace", "ctrl+c", "esc"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("got %q", names)
	}
}

func TestOnlyPossiblePathsWaitForMoreInput(t *testing.T) {
	for input, want := range map[string]bool{
		"j": false, " ": false, "\x1b[A": false, "d": false, "?": false,
		"\x1b": true, `"`: true, "C": true, "/": true, `\`: true, "\x1b[200~C:": true, "\x1b[200~x\x1b[201~": false,
	} {
		if got := mightContinue([]byte(input)); got != want {
			t.Errorf("mightContinue(%q) = %v", input, got)
		}
	}
	utf16 := func(text string) []uint16 {
		var out []uint16
		for _, r := range text {
			out = append(out, uint16(r))
		}
		return append(out, 0, 0)
	}
	if !exeNameIs(utf16("Deadlock.EXE"), "deadlock.exe") || exeNameIs(utf16("deadlock.exe2"), "deadlock.exe") ||
		exeNameIs(utf16("steam.exe"), "deadlock.exe") {
		t.Fatal("exeNameIs")
	}
}

func TestSwitchingKeepsOrderAndCursor(t *testing.T) {
	game := newFakeGame(t)
	store, _ := openStore(game.Game)
	for _, name := range []string{"Alpha", "Bravo", "Charlie"} {
		file := game.write("downloads/"+name+".vpk", buildVPK(2, "sounds/"+strings.ToLower(name)+".vsnd_c"))
		if err := store.install(quietUI(""), file, ""); err != nil {
			t.Fatal(err)
		}
	}
	ui := newTestTUI(t, game.Game)
	ui.handle([]byte("j")) // Bravo
	ui.handle([]byte(" ")) // off
	names := func() string {
		var list []string
		for _, entry := range ui.entries {
			state := "on"
			if !entry.Enabled() {
				state = "off"
			}
			list = append(list, entry.Name()+" "+state)
		}
		return strings.Join(list, ", ")
	}
	if got := names(); got != "Alpha on, Bravo off, Charlie on" || ui.current().Name() != "Bravo" {
		t.Fatalf("after switching off: %s; cursor on %s", got, ui.current().Name())
	}
	ui.handle([]byte(" ")) // pressing Space again undoes it
	if got := names(); got != "Alpha on, Bravo on, Charlie on" || ui.current().Name() != "Bravo" {
		t.Fatalf("after switching back: %s; cursor on %s", got, ui.current().Name())
	}
}

func TestDroppedFilesAndPastes(t *testing.T) {
	typed, pasted, ok := splitPaste([]byte("j\x1b[200~\"C:\\My Mods\\a.zip\"\x1b[201~k"))
	if !ok || pasted != `"C:\My Mods\a.zip"` || string(typed) != "jk" {
		t.Fatalf("splitPaste: %q %q %v", typed, pasted, ok)
	}
	if !looksPasted([]byte(`C:\Users\me\mod.zip`)) || looksPasted([]byte("jjj")) || looksPasted([]byte("\x1b[A\x1b[A")) {
		t.Fatal("looksPasted")
	}
	if got := droppedFiles(`"C:\My Mods\a.zip" C:\b.vpk 'D:\c d.zip'`); !reflect.DeepEqual(got, []string{`C:\My Mods\a.zip`, `C:\b.vpk`, `D:\c d.zip`}) {
		t.Fatalf("droppedFiles: %q", got)
	}
	file := filepath.Join(t.TempDir(), "mod with spaces.zip")
	os.WriteFile(file, []byte("x"), 0o644)
	if got := droppedFiles(file); !reflect.DeepEqual(got, []string{file}) {
		t.Fatalf("an existing path with spaces must stay whole: %q", got)
	}
	if got := truncateMiddle(`D:\SteamLibrary\steamapps\common\Deadlock`, 24); got != `D:\SteamLib…mon\Deadlock` || utf8.RuneCountInString(got) != 24 {
		t.Fatalf("truncateMiddle: %q", got)
	}
}

var ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

func newTestTUI(t *testing.T, game Game, input ...string) *tui {
	keys := make(chan []byte, len(input)+1)
	for _, data := range input {
		keys <- []byte(data)
	}
	ui := &tui{game: game, keys: keys, out: bufio.NewWriter(io.Discard), p: newPalette(true)}
	ui.size = func() (int, int) { return ui.width, ui.height }
	ui.width, ui.height = 100, 30
	ui.reload()
	return ui
}

func (t *tui) screen() string {
	return ansi.ReplaceAllString(strings.Join(t.lines(), "\n"), "")
}

func TestFullScreenInterfaceFlow(t *testing.T) {
	game := newFakeGame(t)
	game.write("game/citadel/addons/pak01_dir.vpk", buildVPK(2, "panorama/layout/hud.vxml_c"))
	download := writeZip(t, filepath.Join(t.TempDir(), "Other HUD.zip"), map[string][]byte{
		"pak01_dir.vpk": buildVPK(2, "panorama/layout/hud.vxml_c", "panorama/images/logo.vtex_c"),
	})
	// The answer to the delete question asked below.
	ui := newTestTUI(t, game.Game, "y")

	screen := ui.screen()
	for _, want := range []string{"MODS OFF", "pak01 (added outside dlmod)", "Press r to repair", "Added by hand"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("missing %q in\n%s", want, screen)
		}
	}

	// Dropping a file installs it at once and marks the conflict with the HUD mod in pak01.
	if ui.handle([]byte("\x1b[200~\"" + download + "\"\x1b[201~")) {
		t.Fatal("paste quit the interface")
	}
	screen = ui.screen()
	for _, want := range []string{"MODS ON", "Other HUD", "loads as pak02_dir.vpk", "Changes the same files as pak01", "2 mods, 2 on, 1 conflict"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("after install, missing %q in\n%s", want, screen)
		}
	}
	if ui.selected != 1 {
		t.Fatalf("the new mod should be selected, got row %d", ui.selected)
	}

	// Space switches it off, Space again back on.
	ui.handle([]byte(" "))
	if ui.entries[1].Enabled() || !strings.Contains(ui.screen(), "switched off") {
		t.Fatalf("switching off failed:\n%s", ui.screen())
	}
	ui.handle([]byte(" "))
	if !ui.entries[1].Enabled() {
		t.Fatal("switching on failed")
	}

	// d asks before deleting; the queued "y" answers.
	ui.handle([]byte("d"))
	if len(ui.entries) != 1 || game.addons()[0] != "pak01_dir.vpk" || len(game.addons()) != 1 {
		t.Fatalf("delete failed: %v", game.addons())
	}

	// Help, then back; Esc quits from the list.
	ui.handle([]byte("?"))
	if !strings.Contains(ui.screen(), "How it works") {
		t.Fatal("help did not open")
	}
	ui.handle([]byte("x"))
	if ui.mode != modeList || !ui.handle([]byte("\x1b")) {
		t.Fatal("Esc should quit from the list")
	}
}

func TestPromptAndNarrowScreens(t *testing.T) {
	game := newFakeGame(t)
	for slot := 1; slot <= 30; slot++ {
		game.write(fmt.Sprintf("game/citadel/addons/pak%02d_dir.vpk", slot), buildVPK(2, fmt.Sprintf("sounds/mod%d.vsnd_c", slot)))
	}
	ui := newTestTUI(t, game.Game)
	ui.handle([]byte("i"))
	ui.handle([]byte("nope"))
	if ui.mode != modePrompt || string(ui.input) != "nope" {
		t.Fatalf("prompt: %v %q", ui.mode, string(ui.input))
	}
	ui.handle([]byte("\r"))
	if !strings.Contains(ui.screen(), "nope: nope was not found") && !strings.Contains(ui.screen(), "dlmod installs .zip, .rar, .7z and .vpk files") {
		t.Fatalf("expected an error for a missing file:\n%s", ui.screen())
	}
	ui.handle([]byte("\x1b[F")) // End
	for _, size := range [][2]int{{100, 30}, {60, 16}, {32, 12}} {
		ui.width, ui.height = size[0], size[1]
		lines := ui.lines()
		if len(lines) > size[1] {
			t.Fatalf("%dx%d: %d lines", size[0], size[1], len(lines))
		}
		for _, line := range lines {
			if n := utf8.RuneCountInString(ansi.ReplaceAllString(line, "")); n >= size[0] {
				t.Fatalf("%dx%d: line of %d characters: %q", size[0], size[1], n, line)
			}
		}
		if !strings.Contains(ui.screen(), "pak30") {
			t.Fatalf("%dx%d: the selected last mod must stay visible:\n%s", size[0], size[1], ui.screen())
		}
	}
}

// The keys at the bottom say what they do, over two lines when one is not enough; Tab hides them
// down to a reminder, and dlmod remembers that.
func TestKeyHints(t *testing.T) {
	game := newFakeGame(t)
	ui := newTestTUI(t, game.Game)
	bottom := func() []string {
		lines := strings.Split(ui.screen(), "\n")
		return lines[len(lines)-2:]
	}
	ui.width = 200
	if got := bottom(); !strings.Contains(got[1], "m model studio") || !strings.Contains(got[1], "Tab hide keys") || strings.Contains(got[0], " q ") {
		t.Fatalf("a wide window has one line of keys:\n%s", strings.Join(got, "\n"))
	}
	ui.width = 100
	if got := bottom(); !strings.Contains(got[0], "s sounds") || !strings.Contains(got[1], "q quit") {
		t.Fatalf("a narrower one wraps them over two lines, descriptions kept:\n%s", strings.Join(got, "\n"))
	}
	ui.handle([]byte("\t"))
	if got := bottom(); !strings.Contains(got[1], "Tab show keys") || strings.Contains(ui.screen(), "model studio") {
		t.Fatalf("Tab hides the keys:\n%s", ui.screen())
	}
	again := newTestTUI(t, game.Game)
	if !strings.Contains(again.screen(), "Tab show keys") {
		t.Fatalf("hidden keys stay hidden next time:\n%s", again.screen())
	}
	again.handle([]byte("\t"))
	if !strings.Contains(again.screen(), "Tab hide keys") {
		t.Fatalf("Tab shows them again:\n%s", again.screen())
	}
}

type countingWriter struct{ writes int }

func (w *countingWriter) Write(data []byte) (int, error) { w.writes++; return len(data), nil }

func TestRenderSkipsUnchangedFrames(t *testing.T) {
	ui := newTestTUI(t, newFakeGame(t).Game)
	counter := &countingWriter{}
	ui.out = bufio.NewWriter(counter)
	ui.render()
	ui.render()
	if counter.writes != 1 {
		t.Fatalf("an unchanged screen was written %d times", counter.writes)
	}
	if ui.refresh() {
		t.Fatal("nothing changed outside, so no redraw is needed")
	}
}

// --- benchmarks ---

func bigTree(files int) []byte {
	paths := make([]string, files)
	for i := range paths {
		paths[i] = fmt.Sprintf("models/heroes/hero%d/part%d/mesh_%d.vmdl_c", i%40, i%97, i)
	}
	return buildVPK(2, paths...)[28:]
}

func BenchmarkParseTree(b *testing.B) {
	tree := bigTree(6000)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := parseTree(tree); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkConflicts(b *testing.B) {
	var entries []*Entry
	for mod := 0; mod < 40; mod++ {
		files := 30
		if mod%20 == 7 {
			files = 6000
		}
		entry := &Entry{Slot: mod + 1}
		for i := 0; i < files; i++ {
			entry.Paths = append(entry.Paths, fmt.Sprintf("models/hero%d/mesh_%d.vmdl_c", mod%5, i))
		}
		entries = append(entries, entry)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		conflicts(entries)
	}
}
