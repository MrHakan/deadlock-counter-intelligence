package main

import (
	"math"
	"testing"

	"github.com/MrHakan/deadlock-modkit/tools/dlmod/source2"
)

func near(a, b [3]float64) bool {
	for k := 0; k < 3; k++ {
		if math.Abs(a[k]-b[k]) > 1.5 {
			return false
		}
	}
	return true
}

func TestEffectRecolor(t *testing.T) {
	keep := effectSettings{Saturation: 1, Brightness: 1}
	for _, c := range [][3]float64{{200, 40, 255}, {0, 0, 0}, {255, 255, 255}, {12, 200, 90}} {
		if got := keep.recolor(c); !near(got, c) {
			t.Errorf("unchanged settings turned %v into %v", c, got)
		}
	}
	if got := (effectSettings{Hue: 120, Saturation: 1, Brightness: 1}).recolor([3]float64{255, 0, 0}); !near(got, [3]float64{0, 255, 0}) {
		t.Errorf("a third of the wheel turns red into green, got %v", got)
	}
	tint := effectSettings{Saturation: 1, Brightness: 1, Tint: "#00ff00", Strength: 1}
	if got := tint.recolor([3]float64{255, 255, 255}); !near(got, [3]float64{0, 255, 0}) {
		t.Errorf("a full tint makes white sparks green, got %v", got)
	}
	if got := tint.recolor([3]float64{0, 0, 128}); !near(got, [3]float64{0, 128, 0}) {
		t.Errorf("a full tint keeps brightness, got %v", got)
	}
	if got := (effectSettings{Saturation: 0, Brightness: 0.5}).recolor([3]float64{255, 0, 0}); !near(got, [3]float64{127.5, 127.5, 127.5}) {
		t.Errorf("no saturation at half brightness is grey, got %v", got)
	}
}

// The Effects tab lists a hero's effect colours, recolours the effects as a mod from the game's
// files each time (settings never stack), and restores them.
func TestStudioEffects(t *testing.T) {
	game, post, _ := studioServer(t, map[string][]byte{
		"particles/abilities/wraith/card_trick.vpcf_c": source2.SampleParticle(),
		"particles/weapon_fx/wraith/muzzle.vpcf_c":     source2.SampleParticle(),
		"particles/abilities/haze/dagger.vpcf_c":       source2.SampleParticle(),
	})
	var listing struct {
		Abilities, Weapon int
		Colors            []struct {
			Color [3]uint8
			Uses  int
		}
		Settings *effectSettings
	}
	post("effects", "application/json", []byte(`{"hero":"wraith"}`), &listing)
	if listing.Abilities != 1 || listing.Weapon != 1 || len(listing.Colors) != 3 || listing.Colors[0].Uses != 2 || listing.Settings != nil {
		t.Fatalf("Wraith's effects: %+v", listing)
	}
	apply := func(body string) {
		var result struct {
			Messages []string
			Error    string
		}
		if code := post("effects/apply", "application/json", []byte(body), &result); code != 200 {
			t.Fatalf("apply: %d %+v", code, result)
		}
	}
	apply(`{"hero":"wraith","settings":{"hue":120,"saturation":1,"brightness":1}}`)
	apply(`{"hero":"wraith","settings":{"hue":120,"saturation":1,"brightness":1}}`) // again: not 240
	vpk := game.installed(t, customEffectsName)
	if _, ok := vpk.Files["particles/weapon_fx/wraith/muzzle.vpcf_c"]; ok {
		t.Fatal("the weapon's effects stay unless asked")
	}
	data, err := vpk.Read("particles/abilities/wraith/card_trick.vpcf_c")
	if err != nil {
		t.Fatal(err)
	}
	colors, _ := source2.ParticleColors(data)
	if want := (effectSettings{Hue: 120, Saturation: 1, Brightness: 1}).recolor([3]float64{250, 0, 0}); !near([3]float64{float64(colors[2][0]), float64(colors[2][1]), float64(colors[2][2])}, want) {
		t.Fatalf("recoloured once from the game's colours: %v, want %v", colors[2], want)
	}
	post("effects", "application/json", []byte(`{"hero":"wraith"}`), &listing)
	if listing.Settings == nil || listing.Settings.Hue != 120 {
		t.Fatalf("the settings are remembered: %+v", listing.Settings)
	}
	post("effects/restore", "application/json", []byte(`{"hero":"wraith"}`), &listing)
	if game.hasMod(t, customEffectsName) {
		t.Fatal("restoring the only recoloured hero removes the mod")
	}
}
