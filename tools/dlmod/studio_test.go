package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MrHakan/deadlock-modkit/tools/dlmod/source2"
)

// sampleOBJ is a box standing where the sample hero stands: a body to put on it.
func sampleOBJ() string {
	var b strings.Builder
	b.WriteString("mtllib box.mtl\nusemtl Paint\n")
	for _, z := range []float64{0, 72} {
		for _, xy := range [][2]float64{{-3, -10}, {3, -10}, {3, 10}, {-3, 10}} {
			// Y-up, as most files are: x stays, the hero's height goes to Y and its depth to Z
			fmt.Fprintf(&b, "v %g %g %g\n", xy[1], z, xy[0])
		}
	}
	for _, face := range [][4]int{{1, 2, 3, 4}, {5, 6, 7, 8}, {1, 2, 6, 5}, {2, 3, 7, 6}, {3, 4, 8, 7}, {4, 1, 5, 8}} {
		fmt.Fprintf(&b, "f %d %d %d %d\n", face[0], face[1], face[2], face[3])
	}
	return b.String()
}

func TestStudioPutsAModelOnAHero(t *testing.T) {
	game := newFakeGame(t)
	game.write("game/citadel/pak01_dir.vpk", source2.WriteVPK(map[string][]byte{
		heroModels["wraith"] + "_c": source2.SampleHero(),
	}))
	store, err := openStore(game.Game)
	if err != nil {
		t.Fatal(err)
	}
	index, err := openHeroModels(game.Game)
	if err != nil {
		t.Fatal(err)
	}
	s := &studio{game: game.Game, store: store, index: index, token: "secret"}
	server := httptest.NewServer(s.routes())
	defer server.Close()
	call := func(name string, body any, into any) int {
		var data []byte
		contentType := "application/json"
		switch v := body.(type) {
		case *bytes.Buffer:
			data, contentType = v.Bytes(), into.(string)
			into = nil
		default:
			data, _ = json.Marshal(body)
		}
		request, _ := http.NewRequest("POST", server.URL+"/api/"+name, bytes.NewReader(data))
		request.Header.Set("Content-Type", contentType)
		request.Header.Set("X-Studio", "secret")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if into != nil {
			json.NewDecoder(response.Body).Decode(into)
		}
		return response.StatusCode
	}

	if response, _ := http.Get(server.URL + "/"); response.StatusCode != http.StatusForbidden {
		t.Errorf("the page needs the token, got %d", response.StatusCode)
	}
	if response, _ := http.Get(server.URL + "/?t=secret"); response.StatusCode != http.StatusOK {
		t.Errorf("the page with its token: %d", response.StatusCode)
	}
	var heroes []struct {
		ID        string
		Available bool
	}
	call("heroes", nil, &heroes)
	available := 0
	for _, h := range heroes {
		if h.Available {
			available++
			if h.ID != "wraith" {
				t.Errorf("%s is listed as available", h.ID)
			}
		}
	}
	if available != 1 {
		t.Fatalf("only Wraith's model is in this game: %+v", heroes)
	}
	var hero struct {
		Parts []struct {
			Name string
			Keep bool
		}
		Pelvis []float64
	}
	if code := call("hero", map[string]string{"hero": "wraith"}, &hero); code != 200 || len(hero.Parts) != 2 || !hero.Parts[1].Keep || len(hero.Pelvis) != 2 {
		t.Fatalf("hero: %d %+v", code, hero)
	}

	var form bytes.Buffer
	writer := multipart.NewWriter(&form)
	part, _ := writer.CreateFormFile("file", "box.obj")
	part.Write([]byte(sampleOBJ()))
	part, _ = writer.CreateFormFile("file", "box.mtl")
	part.Write([]byte("newmtl Paint\nKd 0.2 0.4 0.8\n"))
	writer.Close()
	var model struct {
		Name      string
		Triangles int
		Parts     []struct{ Name string }
	}
	request, _ := http.NewRequest("POST", server.URL+"/api/upload", &form)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("X-Studio", "secret")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	json.NewDecoder(response.Body).Decode(&model)
	response.Body.Close()
	if model.Name != "box.obj" || model.Triangles != 12 || len(model.Parts) != 1 || model.Parts[0].Name != "Paint" {
		t.Fatalf("upload: %d %+v", response.StatusCode, model)
	}

	options := studioOptions{Keep: map[string]bool{"sample_body": false, "gun_model": true}}
	var rig struct {
		Pose      source2.Pose
		Positions string
	}
	if code := call("rig", options, &rig); code != 200 || rig.Positions == "" {
		t.Fatalf("rig: %d %+v", code, rig)
	}
	var bent struct{ Positions string }
	if code := call("bend", source2.Pose{Legs: 20}, &bent); code != 200 || bent.Positions == "" || bent.Positions == rig.Positions {
		t.Fatalf("bend: %d", code)
	}
	var installed struct {
		Messages []string
		Error    string
	}
	if code := call("install", options, &installed); code != 200 {
		t.Fatalf("install: %d %+v", code, installed)
	}
	entries, _ := store.entries()
	if len(entries) != 1 || entries[0].Name() != "Custom model: Wraith" {
		t.Fatalf("the custom model is installed as a mod: %v (%v)", entries, installed.Messages)
	}
	vpk, err := source2.OpenVPK(filepath.Join(game.Addons(), "pak01_dir.vpk"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{heroModels["wraith"] + "_c", "models/dlmod/wraith/paint.vmat_c", "models/dlmod/wraith/paint_color.vtex_c"} {
		if _, ok := vpk.Files[name]; !ok {
			t.Errorf("%s is not in the mod", name)
		}
	}
	// Installing again replaces the hero's previous custom model instead of adding a second.
	options.Scale = 0.9
	call("install", options, &installed)
	if entries, _ = store.entries(); len(entries) != 1 {
		t.Fatalf("a second import replaces the first: %v %v", entries, installed.Messages)
	}
}

func TestFindHero(t *testing.T) {
	for text, want := range map[string]string{"wraith": "wraith", "Lady Geist": "ghost", "abrams": "atlas", "pock": "synth", "geist": "ghost"} {
		if got, err := findHero(text); got != want {
			t.Errorf("findHero(%q) = %q, %v; want %q", text, got, err, want)
		}
	}
	if _, err := findHero("v"); err == nil || !strings.Contains(err.Error(), "could be") {
		t.Errorf("an unclear name lists the heroes it could be: %v", err)
	}
}
