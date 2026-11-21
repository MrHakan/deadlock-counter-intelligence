package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/MrHakan/deadlock-modkit/tools/dlmod/source2"
)

// applyCommand installs what a hero's Meshy workspace folder holds (dlmod meshy writes it): each
// step whose file is in assets/ goes in, in the order model, hero select screen, pictures, ability
// icons, ability models. Cards drawn on green are keyed first; the rest is done by the commands
// that install each kind by hand (dlmod model, select, image, icons, prop).
func applyCommand(ui *UI, store *Store, game Game, args []string, opts options) error {
	if len(args) != 1 {
		return errors.New("name a hero's workspace folder, for example: dlmod apply meshy/ivy")
	}
	if gameRunning() {
		return errRunning
	}
	dir := args[0]
	data, err := os.ReadFile(filepath.Join(dir, "workspace.json"))
	if err != nil {
		return fmt.Errorf("%s has no workspace.json (make one with dlmod meshy <hero> <folder>): %v", dir, err)
	}
	var w workspace
	if err := json.Unmarshal(data, &w); err != nil {
		return fmt.Errorf("workspace.json: %v", err)
	}
	hero, err := findHero(w.Hero)
	if err != nil {
		return err
	}
	have := func(s workspaceStep) (string, bool) {
		file := filepath.Join(dir, filepath.FromSlash(s.Output))
		_, err := os.Stat(file)
		return file, err == nil
	}
	done, missing := 0, []string{}
	var images, icons []string
	selectArgs := []string{hero}
	for _, s := range w.Steps {
		file, ok := have(s)
		if s.Kind == "" {
			continue
		}
		if !ok {
			missing = append(missing, s.Output)
			continue
		}
		switch s.Kind {
		case "model":
			ui.say("Model: %s", s.Output)
			if err := modelCommand(ui, store, game, []string{hero, file}, opts); err != nil {
				return err
			}
			done++
		case "select":
			selectArgs = append(selectArgs, "bg="+file)
		case "image":
			keyed, err := keyedCopy(file)
			if err != nil {
				return err
			}
			images = append(images, s.Target, keyed)
		case "icon":
			icons = append(icons, strings.TrimSuffix(path.Base(s.Target), "_psd.vtex_c")+"="+file)
		}
	}
	sel := w.Select
	if logo := filepath.Join(dir, "assets", "logo.svg"); fileExists(logo) {
		selectArgs = append(selectArgs, "logo="+logo) // a logo drawn by hand wins over the built-in font
	}
	if sel.Name != "" {
		selectArgs = append(selectArgs, "name="+sel.Name)
	}
	if len(sel.Tags) > 0 && !sameStrings(sel.Tags, w.Game.Tags) {
		selectArgs = append(selectArgs, "tags="+strings.Join(sel.Tags, ","))
	}
	if len(sel.Colors) > 0 {
		selectArgs = append(selectArgs, "colors="+strings.Join(sel.Colors, ","))
	}
	if len(selectArgs) > 1 {
		if sel.HideScene && hasPrefix(selectArgs, "bg=") {
			selectArgs = append(selectArgs, "scene=hide")
		}
		ui.say("Hero select screen: %s", strings.Join(selectArgs[1:], " "))
		if err := selectCommand(ui, game, selectArgs); err != nil {
			return err
		}
		done++
	}
	if len(images) > 0 {
		if err := imageCommand(ui, store, game, images); err != nil {
			return err
		}
		done += len(images) / 2
	}
	if len(icons) > 0 {
		if err := iconsCommand(ui, store, game, append([]string{hero}, icons...)); err != nil {
			return err
		}
		done += len(icons)
	}
	for _, s := range w.Steps {
		if s.Kind != "prop" {
			continue
		}
		if file, ok := have(s); ok {
			if err := propCommand(ui, store, game, []string{s.Target, file}, opts); err != nil {
				return err
			}
			done++
		}
	}
	ui.say("%s: %d pieces installed.", w.Name, done)
	if len(missing) > 0 {
		ui.say("Not generated yet: %s", strings.Join(missing, ", "))
	}
	return nil
}

// keyedCopy returns a picture drawn on green as a copy with the green keyed out (next to it,
// *_keyed.png), or the picture itself when it has no green background.
func keyedCopy(file string) (string, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	img, err := source2.DecodeImage(data)
	if err != nil {
		return "", fmt.Errorf("%s: %v", file, err)
	}
	if !hasGreenBackground(img) {
		return file, nil
	}
	out := strings.TrimSuffix(file, filepath.Ext(file)) + "_keyed.png"
	f, err := os.Create(out)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return out, png.Encode(f, keyGreen(img))
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func hasPrefix(list []string, prefix string) bool {
	for _, s := range list {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}

func fileExists(name string) bool {
	_, err := os.Stat(name)
	return err == nil
}
