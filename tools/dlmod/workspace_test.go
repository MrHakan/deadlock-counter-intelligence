package main

import (
	"image"
	"image/color"
	"strconv"
	"strings"
	"testing"
)

func TestTextLogoSVG(t *testing.T) {
	short, err := textLogoSVG("rook")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(short, "<svg") || !strings.Contains(short, `fill="white"`) || strings.Count(short, "M") < 10 {
		t.Fatalf("not a filled logo: %s", short)
	}
	long, err := textLogoSVG("Mo & Krill!")
	if err != nil {
		t.Fatal(err)
	}
	width := func(svg string) float64 {
		_, rest, _ := strings.Cut(svg, `width="`)
		w, _, _ := strings.Cut(rest, `"`)
		f, _ := strconv.ParseFloat(w, 64)
		return f
	}
	if width(long) <= width(short) {
		t.Errorf("a longer name is not wider: %v <= %v", width(long), width(short))
	}
	if _, err := textLogoSVG("Ünal"); err == nil {
		t.Error("a letter the font lacks was accepted")
	}
	if _, err := textLogoSVG("  "); err == nil {
		t.Error("an empty name was accepted")
	}
}

// Every stroke band must wind the same way, or overlaps would cut holes.
func TestStrokeBandsWindTheSameWay(t *testing.T) {
	area := func(band string) float64 {
		var pts [][2]float64
		for _, part := range strings.FieldsFunc(band, func(r rune) bool { return r == 'M' || r == 'L' || r == 'Z' }) {
			f := strings.Fields(part)
			x, _ := strconv.ParseFloat(f[0], 64)
			y, _ := strconv.ParseFloat(f[1], 64)
			pts = append(pts, [2]float64{x, y})
		}
		s := 0.0
		for i := range pts {
			j := (i + 1) % len(pts)
			s += pts[i][0]*pts[j][1] - pts[j][0]*pts[i][1]
		}
		return s
	}
	first := area(strokeBand([2]float64{0, 0}, [2]float64{4, 6}))
	for _, seg := range [][2][2]float64{{{4, 6}, {0, 0}}, {{0, 0}, {0, 6}}, {{3, 3}, {0, 3}}, {{1, 5}, {4, 1}}} {
		if a := area(strokeBand(seg[0], seg[1])); a*first <= 0 {
			t.Errorf("band %v winds the other way (%v vs %v)", seg, a, first)
		}
	}
}

func disc(size int, fg, bg color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	c := float64(size) / 2
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx, dy := float64(x)+0.5-c, float64(y)+0.5-c
			if dx*dx+dy*dy < c*c/4 {
				img.SetNRGBA(x, y, fg)
			} else {
				img.SetNRGBA(x, y, bg)
			}
		}
	}
	return img
}

func TestGlyphImage(t *testing.T) {
	white, black := color.NRGBA{255, 255, 255, 255}, color.NRGBA{0, 0, 0, 255}
	out := glyphImage(disc(200, white, black), 64, 0.8).(*image.NRGBA)
	if out.Bounds().Dx() != 64 {
		t.Fatalf("size %v", out.Bounds())
	}
	if c := out.NRGBAAt(32, 32); c.A < 250 || c.R != 255 {
		t.Errorf("centre %v, want opaque white", c)
	}
	if c := out.NRGBAAt(1, 1); c.A > 5 {
		t.Errorf("corner %v, want transparent", c)
	}
	// The disc is trimmed and scaled to fill 80%: its top is near 10% of the height (6.4).
	if c := out.NRGBAAt(32, 3); c.A > 128 {
		t.Errorf("above the trimmed glyph %v", c)
	}
	// A picture with its own transparency keeps it.
	clear := color.NRGBA{0, 0, 0, 0}
	out = glyphImage(disc(100, color.NRGBA{20, 40, 200, 255}, clear), 32, 0.8).(*image.NRGBA)
	if c := out.NRGBAAt(16, 16); c.A < 250 || c.R != 255 {
		t.Errorf("transparent picture centre %v, want opaque white", c)
	}
}

func TestKeyGreenAndFade(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 4, 2))
	for x := 0; x < 4; x++ {
		img.SetNRGBA(x, 0, color.NRGBA{0, 255, 0, 255})
		img.SetNRGBA(x, 1, color.NRGBA{200, 30, 30, 255})
	}
	keyed := keyGreen(img).(*image.NRGBA)
	if a := keyed.NRGBAAt(0, 0).A; a != 0 {
		t.Errorf("green kept alpha %d", a)
	}
	if c := keyed.NRGBAAt(0, 1); c.A != 255 || c.R != 200 {
		t.Errorf("red became %v", c)
	}
	green := disc(20, color.NRGBA{255, 0, 0, 255}, color.NRGBA{0, 255, 0, 255})
	if !hasGreenBackground(green) {
		t.Error("a card on green was not recognised")
	}
	if hasGreenBackground(disc(20, color.NRGBA{0, 255, 0, 255}, color.NRGBA{10, 10, 10, 255})) {
		t.Error("a dark picture was taken for a green screen")
	}
	wide := image.NewNRGBA(image.Rect(0, 0, 100, 1))
	for x := 0; x < 100; x++ {
		wide.SetNRGBA(x, 0, color.NRGBA{200, 200, 200, 255})
	}
	faded := fadeLeft(wide, 0.38, 0.52).(*image.NRGBA)
	if c := faded.NRGBAAt(10, 0); c.R != 0 {
		t.Errorf("left side %v, want black", c)
	}
	if c := faded.NRGBAAt(90, 0); c.R != 200 {
		t.Errorf("right side %v, want unchanged", c)
	}
}

func TestClosestAspect(t *testing.T) {
	for _, c := range []struct {
		w, h int
		want string
	}{{280, 380, "3:4"}, {4096, 2312, "16:9"}, {50, 50, "1:1"}, {120, 200, "9:16"}, {600, 300, "16:9"}, {0, 0, "1:1"}} {
		if got := closestAspect(c.w, c.h); got != c.want {
			t.Errorf("%dx%d: %s, want %s", c.w, c.h, got, c.want)
		}
	}
}

func TestPlainText(t *testing.T) {
	got := plainText(`Throw a dagger that <span class="highlight">damages</span> for {s:Damage}<br>and   sleeps.`)
	if got != "Throw a dagger that damages for X and sleeps." {
		t.Errorf("%q", got)
	}
}

func TestTagColorRules(t *testing.T) {
	css := tagColorRules("tengu", []string{"#2467FF", "#FFE066"})
	for _, want := range []string{
		".hero_tengu CitadelHeroTags .heroTag.Tag1 .tagBacker{background-color: #2467FF;",
		".hero_tengu CitadelHeroTags .heroTag.Tag2 .tagBacker{background-color: #FFE066;",
		".hero_tengu CitadelHeroTags .heroTag.Tag3 .tagBacker{background-color: #2467FF;",
		".hero_tengu CitadelHeroTags .heroTag.Tag1 Label{color: #F5F5F5;}",
		".hero_tengu CitadelHeroTags .heroTag.Tag2 Label{color: #1A1A1A;}",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("missing %s in %s", want, css)
		}
	}
	if tagColorRules("tengu", nil) != "" {
		t.Error("rules without colours")
	}
}

func TestSelectFileNames(t *testing.T) {
	has := func(hero, name string) bool {
		for _, n := range selectFileNames(hero) {
			if n == name {
				return true
			}
		}
		return false
	}
	for _, c := range [][2]string{{"krill", "mo_krill"}, {"dynamo", "prof_dynamo"}, {"orion", "grey_talon"}, {"tengu", "ivy"}, {"ghost", "lady_geist"}} {
		if !has(c[0], c[1]) {
			t.Errorf("%s does not go by %s: %v", c[0], c[1], selectFileNames(c[0]))
		}
	}
}

func TestPickIcon(t *testing.T) {
	icons := []string{"panorama/images/hud/abilities/tengu/tengu_storm_flask_psd.vtex_c", "panorama/images/hud/abilities/tengu/tengu_stone_form_psd.vtex_c"}
	if got, err := pickIcon(icons, "2"); err != nil || got != icons[1] {
		t.Errorf("by number: %s %v", got, err)
	}
	if got, err := pickIcon(icons, "storm"); err != nil || got != icons[0] {
		t.Errorf("by name: %s %v", got, err)
	}
	if _, err := pickIcon(icons, "st"); err == nil {
		t.Error("an ambiguous name was accepted")
	}
	if _, err := pickIcon(icons, "3"); err == nil {
		t.Error("a number past the list was accepted")
	}
}

func TestPropOfAbility(t *testing.T) {
	thorns := abilityInfo{Key: "citadel_ability_tengu_urn", Name: "Entangling Thorns", Desc: "Summon a patch of choking thorns."}
	if !propOfAbility("models/abilities/ivy_thornbomb_model.vmdl", thorns, "tengu") {
		t.Error("the thorn bomb is not Entangling Thorns'")
	}
	if propOfAbility("models/abilities/tengu_stone_form_model_fx.vmdl", thorns, "tengu") {
		t.Error("the stone statue was given to Entangling Thorns")
	}
	ink := abilityInfo{Key: "ability_artist_homingbrush", Name: "Living Ink"}
	if propOfAbility("models/abilities/artist_ability1_brush_model.vmdl", ink, "artist") {
		t.Error("ability1 in a file name matched the word ability")
	}
}

func TestBuildWorkspace(t *testing.T) {
	a := &heroAssets{
		ID: "tengu", Name: "Ivy", Role: "Sets up a power duo with a lucky ally", Weapon: "Protector's Hand", Height: 112,
		Tags:  []string{"Team-up", "Disruptor", "Rock Solid"},
		Model: "models/heroes_wip/ivy/ivy.vmdl",
		Images: []assetFile{
			{Path: "panorama/images/heroes/tengu_card_psd.vtex_c", Width: 280, Height: 380},
			{Path: "panorama/images/heroes/backgrounds/ivy_bg_psd.vtex_c", Width: 4096, Height: 2312},
		},
		Icons:     []assetFile{{Path: "panorama/images/hud/abilities/tengu/tengu_stone_form_psd.vtex_c", Width: 137, Height: 137}},
		Abilities: []abilityInfo{{Key: "citadel_ability_tengu_stone_form", Name: "Stone Form", Quip: "Turn into stone.", Icon: "panorama/images/hud/abilities/tengu/tengu_stone_form_psd.vtex_c"}},
		Props:     []string{"models/abilities/tengu_stone_form_model_fx.vmdl"},
		Select:    heroSelectFiles{Background: "panorama/images/heroes/backgrounds/ivy_bg_psd.vtex_c"},
	}
	w := buildWorkspace(a)
	steps := map[string]workspaceStep{}
	for _, s := range w.Steps {
		steps[s.ID] = s
	}
	for _, id := range []string{"design_sheet", "turnaround", "model", "rig", "weapon", "select_art", "card", "icon_stone_form", "prop_tengu_stone_form_model_fx"} {
		if _, ok := steps[id]; !ok {
			t.Errorf("no step %s", id)
		}
	}
	if s := steps["card"]; s.Kind != "image" || s.Aspect != "3:4" || s.Size != "280x380" || s.Target != a.Images[0].Path {
		t.Errorf("card step %+v", s)
	}
	if s := steps["icon_stone_form"]; s.Kind != "icon" || s.Target != a.Icons[0].Path || !strings.Contains(s.Prompt, `"Stone Form"`) || !strings.Contains(s.Prompt, "Turn into stone") {
		t.Errorf("icon step %+v", s)
	}
	if s := steps["select_art"]; s.Kind != "select" || s.Aspect != "16:9" || !strings.Contains(s.Prompt, "RIGHT half") {
		t.Errorf("select step %+v", s)
	}
	if !strings.Contains(steps["design_sheet"].Prompt, "Protector's Hand") || !strings.Contains(steps["design_sheet"].Prompt, conceptMark) {
		t.Error("the design sheet prompt does not name the weapon and the concept")
	}
	if sum, required := w.credits(); required >= sum || required < 50 {
		t.Errorf("credits %d, %d without optional steps", sum, required)
	}
	w.Concept = "Rook, a hulking stone golem with glowing blue runes"
	text := filled(steps["card"].Prompt, w)
	if !strings.Contains(text, "Hero card portrait of Rook from the reference sheet") || strings.Contains(text, characterMark) {
		t.Errorf("filled card prompt: %s", text)
	}
	if !strings.Contains(filled(steps["design_sheet"].Prompt, w), w.Concept) {
		t.Error("the design sheet did not get the concept")
	}
	w.Select.Name = "Golem"
	if characterName(w) != "Golem" {
		t.Errorf("character name %q", characterName(w))
	}
}
