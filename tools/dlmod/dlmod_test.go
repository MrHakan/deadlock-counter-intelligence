package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// The SearchPaths layout of the stock gameinfo.gi (build 6864), with Valve's comments shortened.
const stockGameinfo = "\"GameInfo\"\n{\n\tgame \t\t\"citadel\"\n\tFileSystem\n\t{\n" +
	"\t\t//\n\t\t// Search paths are relative to the exe directory\\..\\\n\t\t//\n" +
	"\t\tSearchPaths\n\t\t{\n" +
	"\t\t\t// These are optional language paths. They must be mounted first.\n" +
	"\t\t\tGame_UILanguage\t\tcitadel_*LANGUAGE*\n\n" +
	"\t\t\t// These are optional low-violence paths.\n" +
	"\t\t\tGame_LowViolence\tcitadel_lv\n\n" +
	"\t\t\tGame\t\t\t\tcitadel\n\t\t\tGame\t\t\t\tcore\n\t\t}\n\n" +
	"\t\t\"UserSettingsPathID\"\t\t\"USRLOCAL\"\n\t}\n}\n"

func searchPathLines(t *testing.T, text string) []string {
	t.Helper()
	lines, _ := splitLines(text)
	start, end, err := searchPaths(lines)
	if err != nil {
		t.Fatal(err)
	}
	var result []string
	for _, line := range lines[start:end] {
		if kv := parseLine(line); kv.key != "" {
			result = append(result, kv.key+" "+kv.value)
		}
	}
	return result
}

func TestPatchLoadsAddonsAndKeepsSettingsOutOfThem(t *testing.T) {
	state, err := inspectGameinfo(stockGameinfo)
	if err != nil || state.Loaded {
		t.Fatalf("stock file: %+v %v", state, err)
	}
	patched, changed, err := patchGameinfo(stockGameinfo)
	if err != nil || !changed {
		t.Fatalf("patch: %v %v", changed, err)
	}
	want := []string{"game_uilanguage citadel_*language*", "game_lowviolence citadel_lv",
		"game citadel/addons", "mod citadel", "write citadel", "game citadel", "mod core", "write core", "game core"}
	if got := searchPathLines(t, patched); !reflect.DeepEqual(got, want) {
		t.Fatalf("search paths\n got %q\nwant %q", got, want)
	}
	if state, _ := inspectGameinfo(patched); !state.Loaded || len(state.Missing) != 0 {
		t.Fatalf("patched state %+v", state)
	}
	if again, changed, _ := patchGameinfo(patched); changed || again != patched {
		t.Fatal("patching twice changed the file again")
	}
	restored, changed, err := unpatchGameinfo(patched)
	if err != nil || !changed || restored != stockGameinfo {
		t.Fatalf("unpatch did not restore the stock file: %v %v\n%s", changed, err, restored)
	}
}

func TestPatchKeepsWindowsLineEndingsAndCompletesOlderEdits(t *testing.T) {
	// An older manual guide added only the addons line.
	manual := strings.ReplaceAll(stockGameinfo, "\t\t\tGame\t\t\t\tcitadel\n", "\t\t\tGame citadel/addons\n\t\t\tGame\t\t\t\tcitadel\n")
	manual = strings.ReplaceAll(manual, "\n", "\r\n")
	state, _ := inspectGameinfo(manual)
	if !state.Loaded || !reflect.DeepEqual(state.Missing, []string{"Mod citadel", "Write citadel", "Mod core", "Write core"}) {
		t.Fatalf("manual edit state %+v", state)
	}
	patched, _, err := patchGameinfo(manual)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(patched, "\r\n") != strings.Count(patched, "\n") {
		t.Fatal("line endings were mixed")
	}
	if strings.Count(strings.ToLower(patched), "citadel/addons") != 1 {
		t.Fatal("the addons path was added twice")
	}
	if state, _ := inspectGameinfo(patched); len(state.Missing) != 0 {
		t.Fatalf("still missing %v", state.Missing)
	}
}

func TestUnpatchLeavesHandEditedFilesAlone(t *testing.T) {
	patched, _, _ := patchGameinfo(stockGameinfo)
	edited := strings.Replace(patched, "\t\t\tWrite\t\t\t\tcitadel\n", "\t\t\tGame\t\t\t\tcitadel_extra\n", 1)
	if _, _, err := unpatchGameinfo(edited); err != errEditedByHand {
		t.Fatalf("got %v", err)
	}
	if _, _, err := patchGameinfo("\"GameInfo\" { FileSystem { SearchPaths { Game core } } }"); err == nil {
		t.Fatal("a file without Game citadel must not be edited")
	}
}

// buildVPK writes a version 1 or 2 VPK directory with inline file data.
func buildVPK(version uint32, files ...string) []byte {
	type item struct{ ext, dir, name string }
	var items []item
	for _, file := range files {
		dir, base := path.Split(file)
		dir = strings.TrimSuffix(dir, "/")
		if dir == "" {
			dir = " "
		}
		ext := strings.TrimPrefix(path.Ext(base), ".")
		name := strings.TrimSuffix(base, path.Ext(base))
		if ext == "" {
			ext = " "
		}
		items = append(items, item{ext, dir, name})
	}
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i], items[j]
		return a.ext+"\x00"+a.dir+"\x00"+a.name < b.ext+"\x00"+b.dir+"\x00"+b.name
	})
	var tree bytes.Buffer
	for i := 0; i < len(items); {
		ext := items[i].ext
		tree.WriteString(ext + "\x00")
		for i < len(items) && items[i].ext == ext {
			dir := items[i].dir
			tree.WriteString(dir + "\x00")
			for i < len(items) && items[i].ext == ext && items[i].dir == dir {
				tree.WriteString(items[i].name + "\x00")
				binary.Write(&tree, binary.LittleEndian, struct {
					CRC              uint32
					Preload, Archive uint16
					Offset, Length   uint32
					Terminator       uint16
				}{0, 0, 0x7FFF, 0, 0, 0xFFFF})
				i++
			}
			tree.WriteByte(0)
		}
		tree.WriteByte(0)
	}
	tree.WriteByte(0)
	var out bytes.Buffer
	binary.Write(&out, binary.LittleEndian, []uint32{vpkSignature, version, uint32(tree.Len())})
	if version == 2 {
		binary.Write(&out, binary.LittleEndian, []uint32{0, 0, 0, 0})
	}
	out.Write(tree.Bytes())
	return out.Bytes()
}

func TestVPKFilesReadsBothVersions(t *testing.T) {
	want := []string{"panorama/layout/hud.vxml_c", "panorama/scripts/dci_hud.vts_c", "readme"}
	for _, version := range []uint32{1, 2} {
		got, err := readVPKTree(bytes.NewReader(buildVPK(version, "panorama/layout/hud.vxml_c",
			"panorama/scripts/dci_hud.vts_c", "README")))
		sort.Strings(got)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("version %d: %q %v", version, got, err)
		}
	}
	if _, err := readVPKTree(strings.NewReader("PK\x03\x04 not a vpk")); err != errNotVPK {
		t.Fatalf("got %v", err)
	}
}

type fakeGame struct {
	Game
	t *testing.T
}

func newFakeGame(t *testing.T) fakeGame {
	root := filepath.Join(t.TempDir(), "Deadlock")
	citadel := filepath.Join(root, "game", "citadel")
	if err := os.MkdirAll(citadel, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(citadel, "gameinfo.gi"), []byte(stockGameinfo), 0o644)
	os.WriteFile(filepath.Join(citadel, "steam.inf"), []byte("ClientVersion=6864\nServerVersion=6864\n"), 0o644)
	return fakeGame{Game{root}, t}
}

func (g fakeGame) write(name string, data []byte) string {
	full := filepath.Join(g.Root, name)
	os.MkdirAll(filepath.Dir(full), 0o755)
	if err := os.WriteFile(full, data, 0o644); err != nil {
		g.t.Fatal(err)
	}
	return full
}

func (g fakeGame) addons() []string {
	listing, _ := os.ReadDir(g.Addons())
	var names []string
	for _, item := range listing {
		names = append(names, item.Name())
	}
	return names
}

func writeZip(t *testing.T, file string, entries map[string][]byte) string {
	t.Helper()
	out, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(out)
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		w, _ := archive.Create(name)
		w.Write(entries[name])
	}
	archive.Close()
	out.Close()
	return file
}

func quietUI(answers string) *UI {
	return &UI{in: bufio.NewReader(strings.NewReader(answers)), out: io.Discard, interactive: answers != ""}
}

func TestInstallSwitchAndRemoveMods(t *testing.T) {
	game := newFakeGame(t)
	hud := buildVPK(2, "panorama/layout/hud.vxml_c")
	// A mod installed by hand already uses pak01.
	game.write("game/citadel/addons/pak01_dir.vpk", hud)
	download := writeZip(t, filepath.Join(t.TempDir(), "Cool Skin.zip"), map[string][]byte{
		"Cool Skin/pak01_dir.vpk": buildVPK(2, "models/heroes/haze/haze.vmdl_c"),
		"Cool Skin/pak01_000.vpk": []byte("archive data"),
		"Cool Skin/readme.txt":    []byte("hello"),
		"Cool Skin/setup.exe":     []byte("MZ"),
	})
	store, _ := openStore(game.Game)
	var log bytes.Buffer
	ui := quietUI("")
	ui.out = &log
	if err := store.install(ui, download, ""); err != nil {
		t.Fatal(err)
	}
	if got := game.addons(); !reflect.DeepEqual(got, []string{"pak01_dir.vpk", "pak02_000.vpk", "pak02_dir.vpk"}) {
		t.Fatalf("addons %v", got)
	}
	if !strings.Contains(log.String(), "Skipped Cool Skin/setup.exe") {
		t.Fatalf("program files must be skipped: %s", log.String())
	}
	if err := store.install(ui, download, ""); err != nil || len(game.addons()) != 3 {
		t.Fatalf("installing the same file twice: %v %v", err, game.addons())
	}

	store, _ = openStore(game.Game)
	entries, _ := store.entries()
	if len(entries) != 2 || entries[0].Mod != nil || entries[1].Name() != "Cool Skin" ||
		entries[1].Mod.GameVersion != 6864 || len(conflicts(entries)) != 0 {
		t.Fatalf("entries %+v", entries)
	}

	// Switch the skin off; another mod takes its slot; switching it on picks the next free slot.
	if err := store.disable(entries[1]); err != nil {
		t.Fatal(err)
	}
	game.write("game/citadel/addons/pak02_dir.vpk", buildVPK(1, "sounds/music.vsnd_c"))
	store, _ = openStore(game.Game)
	entries, _ = store.entries()
	skin, err := resolve(entries, "cool")
	if err != nil || skin.Enabled() {
		t.Fatalf("resolve %v %v", skin, err)
	}
	if err := store.enable(skin); err != nil {
		t.Fatal(err)
	}
	if got := game.addons(); !reflect.DeepEqual(got, []string{"pak01_dir.vpk", "pak02_dir.vpk", "pak03_000.vpk", "pak03_dir.vpk"}) {
		t.Fatalf("after switching on: %v", got)
	}
	store, _ = openStore(game.Game)
	entries, _ = store.entries()
	skin, _ = resolve(entries, "pak03")
	if err := store.remove(skin); err != nil {
		t.Fatal(err)
	}
	store, _ = openStore(game.Game)
	if got := game.addons(); len(got) != 2 || len(store.manifest.Mods) != 0 {
		t.Fatalf("after removing: %v %+v", got, store.manifest.Mods)
	}
}

func TestInstallReportsConflictsAndOffersToReplaceUpdates(t *testing.T) {
	game := newFakeGame(t)
	store, _ := openStore(game.Game)
	old := game.write("downloads/dci-6861.vpk", buildVPK(2, "panorama/layout/hud.vxml_c", "panorama/scripts/dci_hud.vts_c"))
	if err := store.install(quietUI(""), old, "Counter HUD"); err != nil {
		t.Fatal(err)
	}
	other := game.write("downloads/other-hud.vpk", buildVPK(2, "panorama/layout/hud.vxml_c"))
	var log bytes.Buffer
	ui := quietUI("")
	ui.out = &log
	if err := store.install(ui, other, ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log.String(), "Conflict: 'Counter HUD' (pak01) also changes panorama/layout/hud.vxml_c") {
		t.Fatalf("missing conflict warning: %s", log.String())
	}
	entries, _ := store.entries()
	if got := conflicts(entries); len(got) != 1 || got[0].A.Slot != 1 || got[0].B.Slot != 2 {
		t.Fatalf("conflicts %+v", got)
	}

	// A newer build of the same mod changes the same files; answering yes replaces it in place.
	update := game.write("downloads/dci-6864.vpk", append(buildVPK(2, "panorama/layout/hud.vxml_c",
		"panorama/scripts/dci_hud.vts_c"), 0))
	if err := store.install(quietUI("y\n"), update, "Counter HUD 6864"); err != nil {
		t.Fatal(err)
	}
	store, _ = openStore(game.Game)
	entries, _ = store.entries()
	if len(entries) != 2 || entries[0].Slot != 1 || entries[0].Name() != "Counter HUD 6864" {
		t.Fatalf("after update %v %v", entries[0].Slot, entries[0].Name())
	}
}

func TestArchivesWithVariantsInstallTheChosenOne(t *testing.T) {
	game := newFakeGame(t)
	download := writeZip(t, filepath.Join(t.TempDir(), "colors.zip"), map[string][]byte{
		"Red/pak01_dir.vpk":  buildVPK(2, "materials/red.vmat_c"),
		"Blue/pak01_dir.vpk": buildVPK(2, "materials/blue.vmat_c"),
	})
	store, _ := openStore(game.Game)
	if err := store.install(quietUI("2\n"), download, ""); err != nil {
		t.Fatal(err)
	}
	entries, _ := store.entries()
	if len(entries) != 1 || entries[0].Name() != "colors (Red/pak01_dir.vpk)" {
		t.Fatalf("entries %+v", entries)
	}
	if err := store.install(quietUI(""), filepath.Join(t.TempDir(), "x.rar"), ""); err == nil {
		t.Fatal(".rar files are not supported")
	}
	broken := writeZip(t, filepath.Join(t.TempDir(), "broken.zip"), map[string][]byte{"pak01_000.vpk": []byte("x")})
	if err := store.install(quietUI(""), broken, ""); err == nil {
		t.Fatal("an archive part without its _dir file must be rejected")
	}
}

func TestSteamLibrariesFindTheGame(t *testing.T) {
	steam := t.TempDir()
	library := filepath.Join(t.TempDir(), "SteamLibrary")
	game := filepath.Join(library, "steamapps", "common", "Deadlock")
	os.MkdirAll(filepath.Join(game, "game", "citadel"), 0o755)
	os.WriteFile(filepath.Join(game, "game", "citadel", "gameinfo.gi"), []byte(stockGameinfo), 0o644)
	os.WriteFile(filepath.Join(library, "steamapps", "appmanifest_1422450.acf"),
		[]byte("\"AppState\"\n{\n\t\"appid\"\t\t\"1422450\"\n\t\"installdir\"\t\t\"Deadlock\"\n}\n"), 0o644)
	os.MkdirAll(filepath.Join(steam, "steamapps"), 0o755)
	vdf := "\"libraryfolders\"\n{\n\t\"0\"\n\t{\n\t\t\"path\"\t\t\"" + steam + "\"\n\t}\n\t\"1\"\n\t{\n\t\t\"path\"\t\t\"" +
		library + "\"\n\t}\n}\n"
	os.WriteFile(filepath.Join(steam, "steamapps", "libraryfolders.vdf"), []byte(vdf), 0o644)

	var found string
	for _, candidate := range steamLibraries(steam) {
		if root := deadlockIn(candidate); root != "" {
			found = root
		}
	}
	if found != game {
		t.Fatalf("found %q, want %q", found, game)
	}
	root, ok := gameRoot(filepath.Join(game, "game", "citadel", "addons"))
	if !ok || root != game {
		t.Fatalf("gameRoot from a subfolder: %q", root)
	}
	if _, err := findGame(t.TempDir()); err == nil {
		t.Fatal("an empty folder is not a Deadlock install")
	}
}

func TestArgumentsAndDraggedPaths(t *testing.T) {
	args, opts, err := parseArgs([]string{"install", `C:\mods\a.zip`, "-name", "A", "--game=D:\\Deadlock"})
	if err != nil || !reflect.DeepEqual(args, []string{"install", `C:\mods\a.zip`}) || opts.name != "A" || opts.game != `D:\Deadlock` {
		t.Fatalf("%v %+v %v", args, opts, err)
	}
	if _, _, err := parseArgs([]string{"-game"}); err == nil {
		t.Fatal("-game needs a value")
	}
	for input, want := range map[string]string{
		`"C:\Users\me\Downloads\mod one.zip"` + "\r\n": `C:\Users\me\Downloads\mod one.zip`,
		`& 'C:\Users\me\mod.zip'`:                      `C:\Users\me\mod.zip`,
		"  /home/me/mod.vpk \n":                        "/home/me/mod.vpk",
	} {
		if got := cleanInput(input); got != want {
			t.Errorf("cleanInput(%q) = %q", input, got)
		}
	}
}
