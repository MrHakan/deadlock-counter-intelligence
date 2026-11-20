package main

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MrHakan/deadlock-modkit/tools/dlmod/source2"
)

func TestHeroSoundGroups(t *testing.T) {
	for name, want := range map[string]string{
		"sounds/vo/wraith/wraith_kill_01.vsnd_c":          "Voice",
		"sounds/vo/wraith/ping/wraith_ping_attack.vsnd_c": "Pings",
		"sounds/abilities/wraith/fullauto_cast.vsnd_c":    "Abilities",
		"sounds/player/footsteps/wraith/paper_01.vsnd_c":  "Footsteps",
		"sounds/player/movement/wraith/land_01.vsnd_c":    "Movement",
		"sounds/vo/astro/astro_kill_wraith_01.vsnd_c":     "", // Astro talking about Wraith
		"sounds/vo/haze/haze_kill_01.vsnd_c":              "",
	} {
		if got := heroSoundGroup(name, "wraith"); got != want {
			t.Errorf("%s: %q, want %q", name, got, want)
		}
	}
}

// The Sounds tab lists a hero's sounds, plays them, replaces one by path and others by name from
// a dropped pack, installs them all as the custom sounds mod, and restores them.
func TestStudioSounds(t *testing.T) {
	game := newFakeGame(t)
	original, _, err := source2.CompileSound(testWAV(t, 10))
	if err != nil {
		t.Fatal(err)
	}
	game.write("game/citadel/pak01_dir.vpk", source2.WriteVPK(map[string][]byte{
		heroModels["wraith"] + "_c":                    source2.SampleHero(),
		"sounds/vo/wraith/wraith_kill_01.vsnd_c":       original,
		"sounds/vo/wraith/wraith_kill_02.vsnd_c":       original,
		"sounds/abilities/wraith/fullauto_cast.vsnd_c": original,
		"sounds/vo/haze/haze_kill_01.vsnd_c":           original,
	}))
	index, err := openHeroModels(game.Game)
	if err != nil {
		t.Fatal(err)
	}
	s := &studio{game: game.Game, index: index, token: "secret"}
	server := httptest.NewServer(s.routes())
	defer server.Close()
	post := func(name, contentType string, body []byte, into any) int {
		request, _ := http.NewRequest("POST", server.URL+"/api/"+name, bytes.NewReader(body))
		request.Header.Set("Content-Type", contentType)
		request.Header.Set("X-Studio", "secret")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		json.NewDecoder(response.Body).Decode(into)
		return response.StatusCode
	}
	type listing struct {
		Sounds []struct{ Path, Name, Group, Replaced string }
	}
	list := func() listing {
		var out listing
		post("sounds", "application/json", []byte(`{"hero":"wraith"}`), &out)
		return out
	}
	if got := list(); len(got.Sounds) != 3 || got.Sounds[0].Group != "Voice" || got.Sounds[2].Group != "Abilities" {
		t.Fatalf("Wraith's own sounds, voice first: %+v", got)
	}
	response, err := http.Get(server.URL + "/api/sound?t=secret&path=sounds/vo/wraith/wraith_kill_01.vsnd_c")
	if err != nil {
		t.Fatal(err)
	}
	audio, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.Header.Get("Content-Type") != "audio/wav" || !bytes.HasPrefix(audio, []byte("RIFF")) {
		t.Fatalf("the page plays the game's sound as a WAV: %q %q", response.Header.Get("Content-Type"), audio[:min(len(audio), 8)])
	}

	upload := func(fields map[string]string, files map[string][]byte) (int, []string) {
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		for k, v := range fields {
			form.WriteField(k, v)
		}
		for name, data := range files {
			part, _ := form.CreateFormFile("file", name)
			part.Write(data)
		}
		form.Close()
		var result struct{ Messages, Problems []string }
		code := post("sounds/replace", form.FormDataContentType(), body.Bytes(), &result)
		return code, append(result.Messages, result.Problems...)
	}
	if code, messages := upload(map[string]string{"path": "sounds/abilities/wraith/fullauto_cast.vsnd_c"},
		map[string][]byte{"my gun.wav": testWAV(t, 20)}); code != 200 || !strings.Contains(strings.Join(messages, " "), "now plays my gun.wav") {
		t.Fatalf("replace by path: %d %v", code, messages)
	}
	// A pack: files named like the hero's sounds replace them; others are reported.
	code, messages := upload(map[string]string{"hero": "wraith"}, map[string][]byte{
		"wraith_kill_02.wav": testWAV(t, 30), "something_else.wav": testWAV(t, 30)})
	if code != 200 || !strings.Contains(strings.Join(messages, " "), "something_else.wav: no sound of this hero") {
		t.Fatalf("replace a pack by name: %d %v", code, messages)
	}
	got := list()
	if got.Sounds[1].Replaced != "wraith_kill_02.wav" || got.Sounds[2].Replaced != "my gun.wav" || got.Sounds[0].Replaced != "" {
		t.Fatalf("the list shows what each sound plays: %+v", got)
	}
	vpk := game.installed(t, customSoundsName)
	for _, name := range []string{"sounds/vo/wraith/wraith_kill_02.vsnd_c", "sounds/abilities/wraith/fullauto_cast.vsnd_c"} {
		if _, ok := vpk.Files[name]; !ok {
			t.Errorf("%s is not in the custom sounds mod", name)
		}
	}
	if response, err = http.Get(server.URL + "/api/sound?t=secret&which=yours&path=sounds/vo/wraith/wraith_kill_02.vsnd_c"); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("the page plays the replacement: %d", response.StatusCode)
	}

	var restored struct{ Messages []string }
	post("sounds/restore", "application/json", []byte(`{"paths":["sounds/vo/wraith/wraith_kill_02.vsnd_c","sounds/abilities/wraith/fullauto_cast.vsnd_c"]}`), &restored)
	if got := list(); got.Sounds[1].Replaced != "" || got.Sounds[2].Replaced != "" {
		t.Fatalf("restored: %+v %v", got, restored.Messages)
	}
	if game.hasMod(t, customSoundsName) {
		t.Fatal("with nothing replaced the custom sounds mod is removed")
	}
}
