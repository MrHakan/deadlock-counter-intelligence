package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/MrHakan/deadlock-modkit/tools/dlmod/source2"
)

// The studio's Effects tab recolours a hero's ability (and, if asked, weapon) particle effects:
// turn their hue, change saturation and brightness, or tint them towards a colour. Each change
// is made from the game's own files again, so settings never stack, and all heroes' recoloured
// effects install as one mod, "Custom effects".

const customEffectsName = "Custom effects"

func (g Game) effectsDir() string { return filepath.Join(g.StateDir(), "effects") }

// effectSettings say how a hero's effects are recoloured. The page shows the same arithmetic
// on the hero's colours before applying (recolor in studio.html).
type effectSettings struct {
	Hue        float64 `json:"hue"`        // degrees to turn the colour wheel
	Saturation float64 `json:"saturation"` // 1 keeps it
	Brightness float64 `json:"brightness"` // 1 keeps it
	Tint       string  `json:"tint"`       // "#rrggbb" to pull colours towards, or ""
	Strength   float64 `json:"strength"`   // how far towards the tint, 0 to 1
	Weapon     bool    `json:"weapon"`     // also the weapon's muzzle flashes and tracers
}

func rgbToHSV(c [3]float64) (h, s, v float64) {
	r, g, b := c[0]/255, c[1]/255, c[2]/255
	hi, lo := math.Max(r, math.Max(g, b)), math.Min(r, math.Min(g, b))
	v, d := hi, hi-lo
	if hi > 0 {
		s = d / hi
	}
	switch {
	case d == 0:
		h = 0
	case hi == r:
		h = math.Mod((g-b)/d, 6)
	case hi == g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	h *= 60
	if h < 0 {
		h += 360
	}
	return
}

func hsvToRGB(h, s, v float64) [3]float64 {
	h = math.Mod(math.Mod(h, 360)+360, 360) / 60
	c := v * s
	x := c * (1 - math.Abs(math.Mod(h, 2)-1))
	var r, g, b float64
	switch int(h) {
	case 0:
		r, g, b = c, x, 0
	case 1:
		r, g, b = x, c, 0
	case 2:
		r, g, b = 0, c, x
	case 3:
		r, g, b = 0, x, c
	case 4:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	m := v - c
	return [3]float64{(r + m) * 255, (g + m) * 255, (b + m) * 255}
}

func parseHex(text string) ([3]float64, bool) {
	text = strings.TrimPrefix(text, "#")
	if len(text) != 6 {
		return [3]float64{}, false
	}
	n, err := strconv.ParseUint(text, 16, 32)
	if err != nil {
		return [3]float64{}, false
	}
	return [3]float64{float64(n >> 16 & 255), float64(n >> 8 & 255), float64(n & 255)}, true
}

// recolor applies settings to one colour: the tint first (its hue reached along the shorter way
// round, its saturation blended in, so white sparks take the tint too), then the turn of the
// hue, saturation and brightness.
func (e effectSettings) recolor(c [3]float64) [3]float64 {
	h, s, v := rgbToHSV(c)
	if tint, ok := parseHex(e.Tint); ok && e.Strength > 0 {
		th, ts, _ := rgbToHSV(tint)
		t := math.Min(1, e.Strength)
		if s < 0.05 {
			h = th // a grey has no hue of its own to turn from: it takes the tint's
		} else {
			h += (math.Mod(th-h+540, 360) - 180) * t
		}
		s += (ts - s) * t
	}
	h += e.Hue
	s = math.Max(0, math.Min(1, s*e.Saturation))
	v = math.Max(0, math.Min(1, v*e.Brightness))
	return hsvToRGB(h, s, v)
}

func (e effectSettings) describe() string {
	var parts []string
	if e.Tint != "" && e.Strength > 0 {
		parts = append(parts, fmt.Sprintf("tinted %s %d%%", e.Tint, int(e.Strength*100+0.5)))
	}
	if e.Hue != 0 {
		parts = append(parts, fmt.Sprintf("hue %+.0f°", e.Hue))
	}
	if e.Saturation != 1 {
		parts = append(parts, fmt.Sprintf("saturation %d%%", int(e.Saturation*100+0.5)))
	}
	if e.Brightness != 1 {
		parts = append(parts, fmt.Sprintf("brightness %d%%", int(e.Brightness*100+0.5)))
	}
	if len(parts) == 0 {
		return "unchanged"
	}
	return strings.Join(parts, ", ")
}

func loadEffectSettings(game Game) (map[string]effectSettings, error) {
	out := map[string]effectSettings{}
	data, err := os.ReadFile(filepath.Join(game.effectsDir(), "settings.json"))
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err == nil {
		err = json.Unmarshal(data, &out)
	}
	return out, err
}

func saveEffectSettings(game Game, settings map[string]effectSettings) error {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(game.effectsDir(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(game.effectsDir(), "settings.json"), data, 0o644)
}

// particleIndex reads the game's particle systems once per studio.
func (s *studio) particleIndex() (*source2.VPKIndex, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.particles == nil {
		index, err := source2.OpenVPKFiltered(filepath.Join(s.game.Citadel(), "pak01_dir.vpk"), func(name string) bool {
			return strings.HasPrefix(name, "particles/") && strings.HasSuffix(name, ".vpcf_c")
		})
		if err != nil {
			return nil, fmt.Errorf("the game's effects could not be read: %v", err)
		}
		s.particles = index
	}
	return s.particles, nil
}

// heroParticles lists a hero's ability effects and weapon effects.
func (s *studio) heroParticles(hero string) (abilities, weapon []string, err error) {
	index, err := s.particleIndex()
	if err != nil {
		return nil, nil, err
	}
	names := heroFileNames(hero)
	for file := range index.Files {
		for _, name := range names {
			if strings.HasPrefix(file, "particles/abilities/"+name+"/") {
				abilities = append(abilities, file)
				break
			}
			if strings.HasPrefix(file, "particles/weapon_fx/"+name+"/") {
				weapon = append(weapon, file)
				break
			}
		}
	}
	sort.Strings(abilities)
	sort.Strings(weapon)
	return abilities, weapon, nil
}

func (s *studio) effectList(r *http.Request) (any, error) {
	var request struct {
		Hero string `json:"hero"`
	}
	if err := readJSON(r, &request); err != nil {
		return nil, err
	}
	abilities, weapon, err := s.heroParticles(request.Hero)
	if err != nil {
		return nil, err
	}
	index, _ := s.particleIndex()
	// The hero's colours, most used first, for the page to show before and after.
	counts := map[[3]uint8]int{}
	for _, file := range append(append([]string{}, abilities...), weapon...) {
		data, err := index.Read(file)
		if err != nil {
			continue
		}
		colors, err := source2.ParticleColors(data)
		if err != nil {
			continue
		}
		for _, c := range colors {
			counts[c]++
		}
	}
	type swatch struct {
		Color [3]uint8 `json:"color"`
		Uses  int      `json:"uses"`
	}
	var swatches []swatch
	for c, n := range counts {
		swatches = append(swatches, swatch{c, n})
	}
	sort.Slice(swatches, func(a, b int) bool {
		if swatches[a].Uses != swatches[b].Uses {
			return swatches[a].Uses > swatches[b].Uses
		}
		return fmt.Sprint(swatches[a].Color) < fmt.Sprint(swatches[b].Color)
	})
	settings, err := loadEffectSettings(s.game)
	if err != nil {
		return nil, err
	}
	var current *effectSettings
	if e, ok := settings[request.Hero]; ok {
		current = &e
	}
	return map[string]any{"abilities": len(abilities), "weapon": len(weapon), "colors": swatches[:min(len(swatches), 96)], "settings": current}, nil
}

// effectApply recolours the hero's effects from the game's files with the settings given.
func (s *studio) effectApply(r *http.Request) (any, error) {
	var request struct {
		Hero     string         `json:"hero"`
		Settings effectSettings `json:"settings"`
	}
	if err := readJSON(r, &request); err != nil {
		return nil, err
	}
	if gameRunning() {
		return nil, errRunning
	}
	abilities, weapon, err := s.heroParticles(request.Hero)
	if err != nil {
		return nil, err
	}
	if len(abilities) == 0 {
		return nil, fmt.Errorf("%s has no ability effects of their own in the game", heroDisplayName(request.Hero))
	}
	project, err := loadReplacements(s.game.effectsDir(), customEffectsName)
	if err != nil {
		return nil, err
	}
	project.restore(append(append([]string{}, abilities...), weapon...)) // start again from the game's files
	files := abilities
	if request.Settings.Weapon {
		files = append(append([]string{}, abilities...), weapon...)
	}
	index, _ := s.particleIndex()
	label := request.Settings.describe()
	changedFiles, changedColors := 0, 0
	for _, file := range files {
		data, err := index.Read(file)
		if err != nil {
			return nil, err
		}
		out, changed, err := source2.RecolorParticle(data, request.Settings.recolor)
		if err != nil {
			return nil, fmt.Errorf("%s: %v", file, err)
		}
		if changed == 0 {
			continue
		}
		if err := project.add(file, label, out); err != nil {
			return nil, err
		}
		changedFiles++
		changedColors += changed
	}
	settings, err := loadEffectSettings(s.game)
	if err != nil {
		return nil, err
	}
	settings[request.Hero] = request.Settings
	if err := saveEffectSettings(s.game, settings); err != nil {
		return nil, err
	}
	p := &webPrompter{}
	p.say("%s's effects %s: %d colours in %d effects.", heroDisplayName(request.Hero), label, changedColors, changedFiles)
	if err := s.rebuild(p, project); err != nil {
		return nil, err
	}
	return map[string]any{"messages": p.messages}, nil
}

func (s *studio) effectRestore(r *http.Request) (any, error) {
	var request struct {
		Hero string `json:"hero"`
	}
	if err := readJSON(r, &request); err != nil {
		return nil, err
	}
	if gameRunning() {
		return nil, errRunning
	}
	abilities, weapon, err := s.heroParticles(request.Hero)
	if err != nil {
		return nil, err
	}
	project, err := loadReplacements(s.game.effectsDir(), customEffectsName)
	if err != nil {
		return nil, err
	}
	if _, err := project.restore(append(abilities, weapon...)); err != nil {
		return nil, err
	}
	settings, err := loadEffectSettings(s.game)
	if err != nil {
		return nil, err
	}
	delete(settings, request.Hero)
	if err := saveEffectSettings(s.game, settings); err != nil {
		return nil, err
	}
	p := &webPrompter{}
	p.say("%s's effects are the game's own again.", heroDisplayName(request.Hero))
	if err := s.rebuild(p, project); err != nil {
		return nil, err
	}
	return map[string]any{"messages": p.messages}, nil
}
