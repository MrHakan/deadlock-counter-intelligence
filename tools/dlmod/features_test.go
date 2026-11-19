package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func installVPK(t *testing.T, game fakeGame, store *Store, name, path string) {
	t.Helper()
	file := game.write("downloads/"+name+".vpk", buildVPK(2, path))
	if err := store.install(quietUI(""), file, ""); err != nil {
		t.Fatal(err)
	}
}

func enabledNames(t *testing.T, store *Store) []string {
	t.Helper()
	entries, err := store.entries()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if entry.Enabled() {
			names = append(names, entry.Name())
		}
	}
	return names
}

func TestProfilesSwitchBetweenSetsOfMods(t *testing.T) {
	game := newFakeGame(t)
	store, _ := openStore(game.Game)
	installVPK(t, game, store, "Alpha", "sounds/alpha.vsnd_c")
	installVPK(t, game, store, "Bravo", "sounds/bravo.vsnd_c")
	game.write("game/citadel/addons/pak03_dir.vpk", buildVPK(2, "sounds/hand.vsnd_c")) // added by hand

	if count, err := store.saveProfile("All"); err != nil || count != 3 {
		t.Fatalf("save All: %d %v", count, err)
	}
	entries, _ := store.entries()
	if err := store.disable(entries[1]); err != nil { // Bravo off
		t.Fatal(err)
	}
	if _, err := store.saveProfile("Ranked"); err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	ui := quietUI("")
	ui.out = &log
	if err := store.useProfile(ui, "all"); err != nil {
		t.Fatal(err)
	}
	if got := enabledNames(t, store); !reflect.DeepEqual(got, []string{"Alpha", "Bravo", "pak03 (added outside dlmod)"}) {
		t.Fatalf("after All: %v", got)
	}
	entries, _ = store.entries()
	if store.activeProfile(entries) != "All" || !strings.Contains(log.String(), "1 switched on, 0 switched off") {
		t.Fatalf("active %q, log %q", store.activeProfile(entries), log.String())
	}
	if err := store.useProfile(ui, "Ranked"); err != nil {
		t.Fatal(err)
	}
	if got := enabledNames(t, store); !reflect.DeepEqual(got, []string{"Alpha", "pak03 (added outside dlmod)"}) {
		t.Fatalf("after Ranked: %v", got)
	}
	if err := store.useProfile(ui, "Ranked"); err != nil || !strings.Contains(log.String(), "already in use") {
		t.Fatalf("using the same profile twice: %v %q", err, log.String())
	}

	// Removing a mod drops it from every profile; deleting a profile keeps the mods.
	entries, _ = store.entries()
	if err := store.remove(entries[0]); err != nil {
		t.Fatal(err)
	}
	store, _ = openStore(game.Game)
	if got := store.manifest.Profiles["All"]; len(got) != 2 {
		t.Fatalf("All should keep 2 mods, has %v", got)
	}
	if err := store.deleteProfile("ALL"); err != nil || !reflect.DeepEqual(store.profileNames(), []string{"Ranked"}) {
		t.Fatalf("delete: %v %v", err, store.profileNames())
	}
	if err := store.useProfile(ui, "nope"); err == nil {
		t.Fatal("an unknown profile must be an error")
	}
	if _, err := store.saveProfile("  "); err == nil {
		t.Fatal("a profile needs a name")
	}
}

// fakeGitHub serves a release list and the files it names.
func fakeGitHub(t *testing.T, files map[string][]byte, releases func(base string) []release) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			http.Error(w, "GitHub requires a User-Agent", http.StatusForbidden)
			return
		}
		if r.URL.Path == "/releases" {
			json.NewEncoder(w).Encode(releases(server.URL))
			return
		}
		if data, ok := files[strings.TrimPrefix(r.URL.Path, "/download/")]; ok {
			w.Write(data)
			return
		}
		http.NotFound(w, r)
	}))
	saved := releasesURL
	releasesURL = server.URL + "/releases"
	t.Cleanup(func() { releasesURL = saved; server.Close() })
	return server
}

func TestUpdateInstallsTheHUDForTheGameBuild(t *testing.T) {
	game := newFakeGame(t) // build 6864
	hud := writeZip(t, filepath.Join(t.TempDir(), "hud.zip"), map[string][]byte{
		"pak01_dir.vpk": buildVPK(2, "panorama/layout/hud.vxml_c", "panorama/scripts/dci_hud.vts_c"),
	})
	zipData, _ := os.ReadFile(hud)
	sum := sha256.Sum256(zipData)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	fakeGitHub(t, map[string][]byte{"6864.zip": zipData, "6870.zip": []byte("newer")}, func(base string) []release {
		return []release{
			{Tag: "v0.4.1-beta", Prerelease: true, Assets: []releaseAsset{{Name: "deadlock-counter-intelligence-6864-hud-en.zip", URL: base + "/download/none"}}},
			{Tag: "v0.4.0", Assets: []releaseAsset{
				{Name: "deadlock-counter-intelligence-6870-hud-en.zip", URL: base + "/download/6870.zip"},
				{Name: "dlmod-windows-amd64.exe", URL: base + "/download/none"},
			}},
			{Tag: "v0.3.0", Assets: []releaseAsset{
				{Name: "deadlock-counter-intelligence-6864-hud-en.zip", URL: base + "/download/6864.zip", Size: int64(len(zipData)), Digest: digest},
			}},
		}
	})
	store, _ := openStore(game.Game)
	var log bytes.Buffer
	ui := quietUI("")
	ui.out = &log
	if err := store.updateHUD(ui); err != nil {
		t.Fatal(err)
	}
	if got := enabledNames(t, store); !reflect.DeepEqual(got, []string{"Deadlock Counter Intelligence v0.3.0"}) {
		t.Fatalf("installed %v; log %q", got, log.String())
	}
	if err := store.updateHUD(ui); err != nil || !strings.Contains(log.String(), "already installed") || len(game.addons()) != 1 {
		t.Fatalf("updating twice: %v %q %v", err, log.String(), game.addons())
	}

	os.WriteFile(filepath.Join(game.Citadel(), "steam.inf"), []byte("ClientVersion=6999\n"), 0o644)
	err := store.updateHUD(ui)
	if err == nil || !strings.Contains(err.Error(), "no HUD for game build 6999 yet; the newest release (v0.4.0) is for build 6870") {
		t.Fatalf("missing build: %v", err)
	}
}

func TestUpdateRejectsADownloadThatFailsItsChecksum(t *testing.T) {
	game := newFakeGame(t)
	fakeGitHub(t, map[string][]byte{"6864.zip": []byte("tampered")}, func(base string) []release {
		return []release{{Tag: "v0.3.0", Assets: []releaseAsset{{Name: "deadlock-counter-intelligence-6864-hud-en.zip",
			URL: base + "/download/6864.zip", Digest: "sha256:" + strings.Repeat("0", 64)}}}}
	})
	store, _ := openStore(game.Game)
	if err := store.updateHUD(quietUI("")); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("got %v", err)
	}
	if len(game.addons()) != 0 {
		t.Fatalf("nothing may be installed: %v", game.addons())
	}
	if leftovers, _ := os.ReadDir(filepath.Join(game.StateDir(), "downloads")); len(leftovers) != 0 {
		t.Fatalf("the download must be deleted: %v", leftovers)
	}
}

func TestProfilesInTheFullScreenView(t *testing.T) {
	game := newFakeGame(t)
	store, _ := openStore(game.Game)
	installVPK(t, game, store, "Alpha", "sounds/alpha.vsnd_c")
	installVPK(t, game, store, "Bravo", "sounds/bravo.vsnd_c")
	// f opens the dialog; Down + Enter picks "Save the mods that are on now…"; then type a name.
	ui := newTestTUI(t, game.Game, "\x1b[B", "\r")
	ui.handle([]byte("f"))
	if ui.mode != modePrompt || ui.purpose != "profile" {
		t.Fatalf("expected the profile name prompt, mode %d purpose %q", ui.mode, ui.purpose)
	}
	ui.handle([]byte("Ranked"))
	ui.handle([]byte("\r"))
	screen := ui.screen()
	if !strings.Contains(screen, "Saved profile 'Ranked' with 2 mods") || !strings.Contains(screen, "profile Ranked") {
		t.Fatalf("after saving:\n%s", screen)
	}
}

func TestOnlySecureAddressesAreDownloaded(t *testing.T) {
	for address, ok := range map[string]bool{
		"https://github.com/MrHakan/deadlock-counter-intelligence/releases/download/v0.3.0/x.zip": true,
		"http://127.0.0.1:8080/releases": true,
		"http://example.com/x.zip":       false,
		"file:///etc/passwd":             false,
		"ftp://example.com/x.zip":        false,
	} {
		if err := allowedURL(address); (err == nil) != ok {
			t.Errorf("allowedURL(%q) = %v", address, err)
		}
	}
}
