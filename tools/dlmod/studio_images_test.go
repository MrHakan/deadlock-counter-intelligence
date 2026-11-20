package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MrHakan/deadlock-modkit/tools/dlmod/source2"
)

// studioServer serves a studio on a fake game holding files, and posts to its API.
func studioServer(t *testing.T, files map[string][]byte) (fakeGame, func(name, contentType string, body []byte, into any) int, string) {
	t.Helper()
	game := newFakeGame(t)
	files[heroModels["wraith"]+"_c"] = source2.SampleHero()
	game.write("game/citadel/pak01_dir.vpk", source2.WriteVPK(files))
	index, err := openHeroModels(game.Game)
	if err != nil {
		t.Fatal(err)
	}
	s := &studio{game: game.Game, index: index, token: "secret"}
	server := httptest.NewServer(s.routes())
	t.Cleanup(server.Close)
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
	return game, post, server.URL
}

func pngFile(t *testing.T, w, h int, c color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h; i++ {
		img.SetNRGBA(i%w, i/w, c)
	}
	var out bytes.Buffer
	png.Encode(&out, img)
	return out.Bytes()
}

// The Images tab finds Abrams' pictures under his file name (abrams, not dlmod's atlas), shows
// them, replaces one with a dropped PNG in its own size, and restores it.
func TestStudioImages(t *testing.T) {
	card := source2.SampleInterfaceImage(28, 38, 3)
	icon := source2.SampleInterfaceImage(16, 16, 1)
	_, post, url := studioServer(t, map[string][]byte{
		heroModels["atlas"] + "_c":                                      source2.SampleHero(),
		"panorama/images/heroes/abrams_card_psd.vtex_c":                 card,
		"panorama/images/heroes/bull_card_gloat_psd.vtex_c":             card, // his old code name
		"panorama/images/heroes/backgrounds/abrams_bg_psd.vtex_c":       card,
		"panorama/images/hud/abilities/abrams/abrams_charge_psd.vtex_c": icon,
		"panorama/images/hud/abilities/haze_dagger_psd.vtex_c":          icon,
		"panorama/images/heroes/haze_card_psd.vtex_c":                   card,
	})
	type listing struct {
		Images []struct {
			Path, Label, Group, Replaced string
			Width, Height                int
			Editable                     bool
		}
	}
	list := func() listing {
		var out listing
		post("images", "application/json", []byte(`{"hero":"atlas"}`), &out)
		return out
	}
	got := list()
	if len(got.Images) != 4 || got.Images[0].Label != "Hero card (hero select, HUD)" || got.Images[0].Width != 28 ||
		got.Images[1].Label != "Card after a kill" || got.Images[2].Label != "Background" ||
		got.Images[3].Group != "Abilities" || got.Images[3].Label != "charge" || !got.Images[0].Editable {
		t.Fatalf("Abrams' images: %+v", got)
	}
	response, err := http.Get(url + "/api/image?t=secret&path=panorama/images/heroes/abrams_card_psd.vtex_c")
	if err != nil {
		t.Fatal(err)
	}
	preview, err := png.Decode(response.Body)
	response.Body.Close()
	if err != nil || preview.Bounds().Dx() != 28 {
		t.Fatalf("the page shows the game's card as a PNG: %v", err)
	}

	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	form.WriteField("path", "panorama/images/heroes/abrams_card_psd.vtex_c")
	part, _ := form.CreateFormFile("file", "my card.png")
	part.Write(pngFile(t, 100, 100, color.NRGBA{0, 200, 0, 255}))
	form.Close()
	var result struct {
		Messages []string
		Error    string
	}
	if code := post("images/replace", form.FormDataContentType(), body.Bytes(), &result); code != 200 ||
		!strings.Contains(strings.Join(result.Messages, " "), "fitted to 28x38") {
		t.Fatalf("replace: %d %+v", code, result)
	}
	if got = list(); got.Images[0].Replaced != "my card.png" {
		t.Fatalf("the list shows the replacement: %+v", got.Images[0])
	}
	if response, err = http.Get(url + "/api/image?t=secret&which=yours&path=panorama/images/heroes/abrams_card_psd.vtex_c"); err != nil {
		t.Fatal(err)
	}
	yours, err := png.Decode(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if c := color.NRGBAModel.Convert(yours.At(5, 5)).(color.NRGBA); c.G < 190 || c.R > 10 {
		t.Fatalf("the replacement shows green, got %v", c)
	}
	post("images/restore", "application/json", []byte(`{"paths":["panorama/images/heroes/abrams_card_psd.vtex_c"]}`), &result)
	if got = list(); got.Images[0].Replaced != "" {
		t.Fatalf("restored: %+v", got.Images[0])
	}
}
