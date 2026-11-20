package main

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var sampleCatalog = []RemoteMod{
	{ID: "101", Name: "Curly Hair Wraith", Author: "ana", Category: "Skins", Hero: "Wraith", Downloads: 9500,
		Images: []string{"https://images.example/a.jpg"}, Downloadable: true, URL: "https://gamebanana.com/mods/101"},
	{ID: "102", Name: "Catgirl Wraith", Author: "bo", Category: "Skins", Hero: "Wraith", Downloads: 11000, Downloadable: true},
	{ID: "103", Name: "Haze Recolor", Author: "cy", Category: "Skins", Hero: "Haze", Downloads: 500, Downloadable: true},
	{ID: "104", Name: "Quiet Music", Author: "di", Category: "In-Game Music", Downloads: 40000, Downloadable: true},
	{ID: "105", Name: "Spicy Wraith", Author: "ed", Category: "Skins", Hero: "Wraith", NSFW: true, Downloadable: true},
	{ID: "106", Name: "Removed Mod", Category: "Skins", Hero: "Wraith", Downloadable: false},
}

func writeCatalog(t *testing.T, game Game, mods []RemoteMod) {
	t.Helper()
	data, err := json.Marshal(mods)
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Dir(game.catalogFile()), 0o755)
	if err := os.WriteFile(game.catalogFile(), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCatalogSearchFiltersAndRanks(t *testing.T) {
	data, _ := json.Marshal(sampleCatalog)
	mods, err := parseCatalog(data)
	if err != nil || len(mods) != 5 {
		t.Fatalf("mods without files are dropped: %d %v", len(mods), err)
	}
	names := func(found []RemoteMod) []string {
		var list []string
		for _, mod := range found {
			list = append(list, mod.Name)
		}
		return list
	}
	if got := names(search(mods, filter{category: "Skins", hero: "Wraith"})); strings.Join(got, ",") != "Catgirl Wraith,Curly Hair Wraith" {
		t.Fatalf("hero skins, most downloaded first, NSFW hidden: %v", got)
	}
	if got := names(search(mods, filter{hero: "Wraith", nsfw: true})); len(got) != 3 {
		t.Fatalf("NSFW shown on request: %v", got)
	}
	if got := names(search(mods, filter{text: "curly WRAITH"})); strings.Join(got, ",") != "Curly Hair Wraith" {
		t.Fatalf("every word must match, ignoring case: %v", got)
	}
	if got := names(search(mods, filter{text: "ana"})); len(got) != 1 {
		t.Fatalf("authors are searched: %v", got)
	}
	categories, heroes := facets(mods)
	if strings.Join(categories, ",") != "In-Game Music,Skins" || strings.Join(heroes, ",") != "Haze,Wraith" {
		t.Fatalf("facets: %v %v", categories, heroes)
	}
	for text, want := range map[string]string{"742960": "742960", "https://gamebanana.com/mods/742960": "742960",
		"https://www.gamebanana.com/mods/12/": "12", "gamebanana.com/mods/x": "", "../1": ""} {
		if id, ok := parseModID(text); id != want || ok != (want != "") {
			t.Fatalf("parseModID(%q) = %q %v", text, id, ok)
		}
	}
	if formatCount(9500) != "9.5k" || formatCount(40000) != "40k" || formatCount(999) != "999" {
		t.Fatal("download counts are shortened")
	}
}

func TestInstallRemoteChecksTheDownloadAndInstalls(t *testing.T) {
	game := newFakeGame(t)
	archive := writeZip(t, filepath.Join(t.TempDir(), "skin.zip"), map[string][]byte{
		"pak01_dir.vpk": buildVPK(2, "models/heroes_wip/wraith/wraith.vmdl_c"),
		"setup.exe":     []byte("MZ"),
	})
	data, _ := os.ReadFile(archive)
	sum := md5.Sum(data)
	checksum := hex.EncodeToString(sum[:])
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/dmm/101/download":
			json.NewEncoder(w).Encode([]RemoteFile{{Name: "../skin.zip", URL: server.URL + "/file", Size: int64(len(data)), MD5: checksum}})
		case "/dmm/102/download":
			w.Write([]byte("[]")) // the mirror has nothing: GameBanana is asked
		case "/banana/102":
			json.NewEncoder(w).Encode(map[string]any{"_aFiles": []map[string]any{{"_sFile": "skin.zip",
				"_sDownloadUrl": server.URL + "/file", "_nFilesize": len(data), "_sMd5Checksum": strings.Repeat("0", 32)}}})
		case "/file":
			w.Write(data)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	saved := [2]string{dmmFilesURL, bananaFilesURL}
	defer func() { dmmFilesURL, bananaFilesURL = saved[0], saved[1] }()
	dmmFilesURL, bananaFilesURL = server.URL+"/dmm/%s/download", server.URL+"/banana/%s"

	store, err := openStore(game.Game)
	if err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	p := quietUI("")
	p.out = &log
	if err := store.installRemote(p, sampleCatalog[0]); err != nil {
		t.Fatal(err)
	}
	entries, _ := store.entries()
	if len(entries) != 1 || entries[0].Name() != "Curly Hair Wraith" {
		t.Fatalf("installed: %+v", entries)
	}
	if !strings.Contains(log.String(), "Skipped setup.exe") {
		t.Fatalf("program files in a download are skipped: %s", log.String())
	}
	if leftovers, _ := os.ReadDir(filepath.Join(game.StateDir(), "downloads")); len(leftovers) != 0 {
		t.Fatalf("the download is removed after installing: %v", leftovers)
	}
	err = store.installRemote(p, sampleCatalog[1])
	if err == nil || !strings.Contains(err.Error(), "MD5") {
		t.Fatalf("a download that does not match its checksum is refused: %v", err)
	}
	if entries, _ := store.entries(); len(entries) != 1 {
		t.Fatalf("nothing installed after a failed checksum: %d", len(entries))
	}
}

func testPicture() image.Image {
	picture := image.NewRGBA(image.Rect(0, 0, 40, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 40; x++ {
			c := color.RGBA{255, 0, 0, 255}
			if y >= 10 {
				c = color.RGBA{0, 0, 255, 255}
			}
			picture.SetRGBA(x, y, c)
		}
	}
	return picture
}

func TestPreviewEncodings(t *testing.T) {
	picture := testPicture()
	if w, h := fit(picture.Bounds(), 10, 10); w != 10 || h != 5 {
		t.Fatalf("fit keeps the aspect ratio: %d x %d", w, h)
	}
	lines := blockLines(picture, 8, 4)
	if len(lines) != 2 || len(lines[0]) != 8 {
		t.Fatalf("8 cells wide, two pixels per cell height: %d x %d", len(lines), len(lines[0]))
	}
	if lines[0][0].text != "▀" || !strings.Contains(lines[0][0].style, "38;2;255;0;0") || !strings.Contains(lines[1][0].style, "48;2;0;0;255") {
		t.Fatalf("top pixel in the foreground, bottom in the background: %q %q", lines[0][0].style, lines[1][0].style)
	}
	sixel := encodeSixel(picture, 12, 12)
	if !strings.HasPrefix(sixel, "\x1bP0;1;0q\"1;1;12;12") || !strings.HasSuffix(sixel, "-\x1b\\") || strings.Count(sixel, "-") != 2 {
		t.Fatalf("sixel: header, two bands of six rows, terminator: %q", sixel[len(sixel)-40:])
	}
	// The palette is made for the picture: two colours here, and red fills the whole first band.
	if strings.Count(sixel, ";2;") != 2 || !strings.Contains(sixel, ";2;100;0;0") || !strings.Contains(sixel, ";2;0;0;100") {
		t.Fatalf("a two-colour picture gets a two-colour palette: %q", sixel)
	}
	red := sixel[strings.Index(sixel, ";2;100;0;0")-2 : strings.Index(sixel, ";2;100;0;0")]
	if !strings.Contains(sixel, red+"!12~") {
		t.Fatalf("red fills the first band: %q", sixel)
	}
	gradient := image.NewRGBA(image.Rect(0, 0, 64, 12))
	for x := 0; x < 64; x++ {
		for y := 0; y < 12; y++ {
			gradient.SetRGBA(x, y, color.RGBA{uint8(x * 4), uint8(x * 4), uint8(x * 4), 255})
		}
	}
	palette := quantize(gradient, 8)
	if len(palette) != 8 {
		t.Fatalf("median cut fills the palette: %d", len(palette))
	}
	indexes := ditherIndexes(gradient, palette)
	if indexes[0] == indexes[63] {
		t.Fatal("dark and light ends map to different colours")
	}
	kitty := encodeKitty(picture, 40, 20, 8, 2)
	if !strings.Contains(kitty, "a=T,f=100,i=7,q=2,C=1,c=8,r=2") || !strings.HasPrefix(kitty, "\x1b_Ga=d,d=i,i=7") {
		t.Fatalf("kitty: replaces image 7 and sends a PNG for 8 x 2 cells: %q", kitty[:80])
	}
	env := func(values map[string]string) func(string) string { return func(k string) string { return values[k] } }
	for want, values := range map[int]map[string]string{
		imagesBlocks: {"WT_SESSION": "1"},
		imagesKitty:  {"TERM": "xterm-kitty"},
		imagesSixel:  {"TERM_PROGRAM": "WezTerm"},
		imagesOff:    {"NO_COLOR": "1"},
	} {
		if got := detectImageMode(env(values)); got != want {
			t.Fatalf("%v: %s, want %s", values, imageModeNames[got], imageModeNames[want])
		}
	}
	if detectImageMode(env(map[string]string{"WT_SESSION": "1", "DLMOD_IMAGES": "SIXEL"})) != imagesSixel {
		t.Fatal("DLMOD_IMAGES overrides the guess")
	}
}

func TestOnlineBrowserScreen(t *testing.T) {
	game := newFakeGame(t)
	writeCatalog(t, game.Game, sampleCatalog)
	ui := newTestTUI(t, game.Game)
	var out bytes.Buffer
	ui.out.Reset(&out)
	ui.loadImage = func(address string) (image.Image, error) { return testPicture(), nil }
	t.Setenv("DLMOD_IMAGES", "blocks")
	ui.handle([]byte("b"))
	screen := ui.screen()
	if ui.mode != modeBrowse || !strings.Contains(screen, "Category Skins") || !strings.Contains(screen, "Catgirl Wraith") ||
		strings.Contains(screen, "Quiet Music") || strings.Contains(screen, "Spicy Wraith") {
		t.Fatalf("the browser opens on skins without NSFW mods:\n%s", screen)
	}
	if !strings.Contains(screen, "11k") || !strings.Contains(screen, "3 mods") {
		t.Fatalf("download counts and the result count:\n%s", screen)
	}
	ui.handle([]byte("\x1b[B")) // down to the mod with a preview
	if screen = ui.screen(); !strings.Contains(screen, "▀") || !strings.Contains(screen, "by ana") {
		t.Fatalf("the selected mod's preview and author:\n%s", screen)
	}
	ui.handle([]byte("\x1b[C")) // next hero: Haze
	if screen = ui.screen(); !strings.Contains(screen, "Hero Haze") || !strings.Contains(screen, "Haze Recolor") || strings.Contains(screen, "Catgirl") {
		t.Fatalf("hero filter:\n%s", screen)
	}
	ui.handle([]byte("\x1b[D\x1b[D")) // back past Haze to all heroes
	ui.handle([]byte("/"))
	ui.handle([]byte("curly"))
	if screen = ui.screen(); !strings.Contains(screen, "Search curly") || !strings.Contains(screen, "1 mod") {
		t.Fatalf("search while typing:\n%s", screen)
	}
	ui.handle([]byte("\r"))
	ui.handle([]byte("m"))                // blocks -> sixel
	ui.handle([]byte("\x1b[4;540;1000t")) // window size first: 1000 / 100 columns, 540 / 30 rows
	if ui.browse.cellWidth != 10 || ui.browse.cellHeight != 18 {
		t.Fatalf("the cell size follows from the window size: %d x %d", ui.browse.cellWidth, ui.browse.cellHeight)
	}
	ui.handle([]byte("\x1b[6;18;9t"))
	if ui.browse.cellWidth != 9 || ui.browse.cellHeight != 18 {
		t.Fatalf("the cell-size answer is read, not typed: %d x %d", ui.browse.cellWidth, ui.browse.cellHeight)
	}
	ui.render()
	ui.out.Flush()
	if !strings.Contains(out.String(), "\x1bP0;1;0q") || ui.imageAt.url == "" || ui.imageAt.col < 30 {
		t.Fatalf("sixel previews are drawn after the text, right of the list: %+v", ui.imageAt)
	}
	ui.handle([]byte("\x1b"))
	if ui.mode != modeList {
		t.Fatalf("Esc returns to the installed mods: %v", ui.mode)
	}
}

func TestExtractedFolderSources(t *testing.T) {
	folder := t.TempDir()
	os.MkdirAll(filepath.Join(folder, "Mod", "variant"), 0o755)
	os.WriteFile(filepath.Join(folder, "Mod", "variant", "pak01_dir.vpk"), buildVPK(2, "sounds/a.vsnd_c"), 0o644)
	os.WriteFile(filepath.Join(folder, "Mod", "run.bat"), []byte("@echo"), 0o644)
	sources, skipped, err := folderSources(folder)
	if err != nil || len(sources) != 1 || len(skipped) != 1 || skipped[0] != "Mod/run.bat" {
		t.Fatalf("folder sources: %d %v %v", len(sources), skipped, err)
	}
	if !containsVPK(folder) || containsVPK(t.TempDir()) {
		t.Fatal("containsVPK")
	}
}

func TestTerminalAnswerUpgradesPreviewsToSixel(t *testing.T) {
	for _, name := range []string{"DLMOD_IMAGES", "TERM_PROGRAM", "KITTY_WINDOW_ID", "NO_COLOR"} {
		t.Setenv(name, "")
	}
	t.Setenv("TERM", "xterm-256color")
	game := newFakeGame(t)
	writeCatalog(t, game.Game, sampleCatalog)
	ui := newTestTUI(t, game.Game)
	var out bytes.Buffer
	ui.out.Reset(&out)
	ui.loadImage = func(address string) (image.Image, error) { return testPicture(), nil }
	ui.handle([]byte("b"))
	ui.out.Flush()
	if !strings.Contains(out.String(), "\x1b[c") || ui.browse.imageMode != imagesBlocks {
		t.Fatalf("blocks first, and the terminal is asked what it draws: %q", out.String())
	}
	ui.handle([]byte("\x1b[?61;6;7;14;21;22;23;24;28;32;42c")) // Windows Terminal before 1.22
	if ui.browse.imageMode != imagesBlocks {
		t.Fatal("no sixel attribute, no change")
	}
	ui.handle([]byte("\x1b[?61;4;6;7;14;21;22;23;24;28;32;42c")) // Windows Terminal 1.22+
	if ui.browse.imageMode != imagesSixel || ui.mode != modeBrowse {
		t.Fatalf("the sixel attribute switches previews to full resolution: %s", imageModeNames[ui.browse.imageMode])
	}

	ui.handle([]byte("\x1b[B")) // the mod with a picture
	ui.handle([]byte("m"))      // sixel -> kitty, chosen by hand
	ui.handle([]byte("m"))      // kitty -> off
	ui.handle([]byte("m"))      // off -> blocks
	ui.handle([]byte("\x1b[?61;4c"))
	if ui.browse.imageMode != imagesBlocks {
		t.Fatal("a mode picked with m is kept")
	}
	small := strings.Count(ui.screen(), "▀")
	ui.handle([]byte("v"))
	big := strings.Count(ui.screen(), "▀")
	if big < small*3/2 || strings.Contains(ui.screen(), "Catgirl Wraith") {
		t.Fatalf("the big preview fills the list's place with a larger picture: %d -> %d cells", small, big)
	}
	ui.handle([]byte("v"))
	if !strings.Contains(ui.screen(), "Catgirl Wraith") {
		t.Fatal("v again brings the list back")
	}
}
