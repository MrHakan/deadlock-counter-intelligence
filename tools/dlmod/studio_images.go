package main

import (
	"bytes"
	"errors"
	"fmt"
	"image/png"
	"io"
	"net/http"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/MrHakan/deadlock-modkit/tools/dlmod/source2"
)

// The studio's Images tab: a hero's pictures in the game's interface (the card on the hero select
// screen and in the HUD, portraits, the minimap icon, the background, the weapon picture) and
// ability icons, replaced by dropping a PNG or JPEG. They install as one mod, "Custom images".

const customImagesName = "Custom images"

func (g Game) imagesDir() string { return filepath.Join(g.StateDir(), "images") }

func loadImageProject(game Game) (*replacementProject, error) {
	return loadReplacements(game.imagesDir(), customImagesName)
}

// heroImageKinds are the hero's own pictures, by the end of their file names.
var heroImageKinds = []struct{ suffix, label string }{
	{"_card_psd", "Hero card (hero select, HUD)"},
	{"_card_critical_psd", "Card at low health"},
	{"_card_gloat_psd", "Card after a kill"},
	{"_vertical_psd", "Tall portrait"},
	{"_sm_psd", "Small portrait"},
	{"_mm_psd", "Minimap icon"},
	{"_bg_psd", "Background"},
	{"_gun_psd", "Weapon picture"},
}

// heroAliases are older code names some heroes' interface files still use (build 6864): Abrams'
// card is bull_card_psd, Dynamo's abilities are in a sumo folder.
var heroAliases = map[string][]string{
	"atlas": {"bull"}, "forge": {"engineer"}, "ghost": {"spectre"}, "gigawatt": {"giga"}, "krill": {"digger", "duo"},
	"dynamo": {"sumo"}, "viper": {"kali"}, "chessmaster": {"chess"},
}

// heroFileNames are the names a hero's files may go by: dlmod's id, older code names, the folder
// name of its model (abrams for atlas) and the name players know.
func heroFileNames(hero string) []string {
	names := append([]string{hero}, heroAliases[hero]...)
	if model := heroModels[hero]; model != "" {
		names = append(names, path.Base(strings.TrimSuffix(model, ".vmdl")), path.Base(path.Dir(model)))
	}
	names = append(names, strings.ReplaceAll(strings.ToLower(heroDisplayName(hero)), " ", "_"))
	seen := map[string]bool{}
	var out []string
	for _, name := range names {
		if name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

// imageIndex reads the game's interface images once per studio.
func (s *studio) imageIndex() (*source2.VPKIndex, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.images == nil {
		index, err := source2.OpenVPKFiltered(filepath.Join(s.game.Citadel(), "pak01_dir.vpk"), func(name string) bool {
			return strings.HasPrefix(name, "panorama/images/") && strings.HasSuffix(name, ".vtex_c")
		})
		if err != nil {
			return nil, fmt.Errorf("the game's images could not be read: %v", err)
		}
		s.images = index
	}
	return s.images, nil
}

type heroImage struct {
	Path, Label, Group string
}

// heroImages finds a hero's pictures and ability icons.
func (s *studio) heroImages(hero string) ([]heroImage, error) {
	index, err := s.imageIndex()
	if err != nil {
		return nil, err
	}
	var out []heroImage
	seen := map[string]bool{}
	add := func(name, label, group string) {
		if !seen[name] {
			seen[name] = true
			out = append(out, heroImage{name, label, group})
		}
	}
	names := heroFileNames(hero)
	for _, kind := range heroImageKinds {
		for _, name := range names {
			for _, folder := range []string{"", "backgrounds/", "guns/"} {
				file := "panorama/images/heroes/" + folder + name + kind.suffix + ".vtex_c"
				if _, ok := index.Files[file]; ok {
					add(file, kind.label, "Portraits")
				}
			}
		}
	}
	var abilities []string
	for file := range index.Files {
		if !strings.HasPrefix(file, "panorama/images/hud/abilities/") || !strings.HasSuffix(file, "_psd.vtex_c") {
			continue
		}
		rest := strings.TrimPrefix(file, "panorama/images/hud/abilities/")
		for _, name := range names {
			if strings.HasPrefix(rest, name+"/") || (!strings.Contains(rest, "/") && strings.HasPrefix(rest, name+"_")) {
				abilities = append(abilities, file)
				break
			}
		}
	}
	sort.Strings(abilities)
	for _, file := range abilities {
		label := strings.TrimSuffix(path.Base(file), "_psd.vtex_c")
		for _, name := range names {
			label = strings.TrimPrefix(label, name+"_")
		}
		add(file, strings.ReplaceAll(label, "_", " "), "Abilities")
	}
	return out, nil
}

func (s *studio) imageList(r *http.Request) (any, error) {
	var request struct {
		Hero string `json:"hero"`
	}
	if err := readJSON(r, &request); err != nil {
		return nil, err
	}
	images, err := s.heroImages(request.Hero)
	if err != nil {
		return nil, err
	}
	index, _ := s.imageIndex()
	project, err := loadImageProject(s.game)
	if err != nil {
		return nil, err
	}
	type entry struct {
		Path     string `json:"path"`
		Label    string `json:"label"`
		Group    string `json:"group"`
		Width    int    `json:"width"`
		Height   int    `json:"height"`
		Editable bool   `json:"editable"`
		Replaced string `json:"replaced,omitempty"`
	}
	out := []entry{}
	for _, img := range images {
		head, err := index.ReadHead(img.Path, 16<<10)
		if err != nil {
			return nil, err
		}
		info, editable, _ := source2.TextureHeader(head)
		out = append(out, entry{img.Path, img.Label, img.Group, info.Width, info.Height, editable, project.Replaced[img.Path].Source})
	}
	return map[string]any{"images": out}, nil
}

// imageFile sends a picture as PNG, at most size pixels across: the game's own or the one replacing it.
func (s *studio) imageFile(r *http.Request) (any, error) {
	name := r.URL.Query().Get("path")
	var compiled []byte
	if r.URL.Query().Get("which") == "yours" {
		project, err := loadImageProject(s.game)
		if err != nil {
			return nil, err
		}
		if compiled, err = project.read(name); err != nil {
			return nil, err
		}
	} else {
		index, err := s.imageIndex()
		if err != nil {
			return nil, err
		}
		if _, ok := index.Files[name]; !ok {
			return nil, fmt.Errorf("the game has no image %s", name)
		}
		if compiled, err = index.Read(name); err != nil {
			return nil, err
		}
	}
	img, _, err := source2.DecodeTexture(compiled)
	if err != nil {
		return nil, err
	}
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	if size <= 0 {
		size = 1024
	}
	var out bytes.Buffer
	if err := png.Encode(&out, source2.Thumbnail(img, size)); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// imageReplace takes one dropped picture for one game image.
func (s *studio) imageReplace(r *http.Request) (any, error) {
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		return nil, fmt.Errorf("the upload failed: %v", err)
	}
	if gameRunning() {
		return nil, errRunning
	}
	target := r.FormValue("path")
	index, err := s.imageIndex()
	if err != nil {
		return nil, err
	}
	if _, ok := index.Files[target]; !ok {
		return nil, fmt.Errorf("the game has no image %s", target)
	}
	files := r.MultipartForm.File["file"]
	if len(files) != 1 {
		return nil, errors.New("drop one picture on an image")
	}
	f, err := files[0].Open()
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(f, 256<<20))
	f.Close()
	if err != nil {
		return nil, err
	}
	picture, err := source2.DecodeImage(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %v (use a PNG or JPEG)", files[0].Filename, err)
	}
	original, err := index.Read(target)
	if err != nil {
		return nil, err
	}
	compiled, err := source2.ReplaceTextureImage(original, picture)
	if err != nil {
		return nil, err
	}
	project, err := loadImageProject(s.game)
	if err != nil {
		return nil, err
	}
	name := path.Base(strings.ReplaceAll(files[0].Filename, "\\", "/"))
	if err := project.add(target, name, compiled); err != nil {
		return nil, err
	}
	p := &webPrompter{}
	_, info, _ := source2.DecodeTexture(compiled)
	b := picture.Bounds()
	p.say("%s now shows %s (%dx%d, fitted to %dx%d).", path.Base(target), name, b.Dx(), b.Dy(), info.Width, info.Height)
	if err := s.rebuild(p, project); err != nil {
		return nil, err
	}
	return map[string]any{"messages": p.messages}, nil
}

func (s *studio) imageRestore(r *http.Request) (any, error) {
	project, err := loadImageProject(s.game)
	if err != nil {
		return nil, err
	}
	return s.restore(r, project, "%d image is the game's own again.", "%d images are the game's own again.")
}
