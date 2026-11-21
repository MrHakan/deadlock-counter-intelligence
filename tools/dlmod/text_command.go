package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// textCommand changes game text from the command line, as the studio's Text tab does:
// dlmod text KEY=VALUE [KEY=VALUE ...] sets lines of the English hero text (for example
// "hero_haze:n=Shade" or "Citadel_Haze_HeroTag_1=Sneaky"); KEY= with nothing after it
// puts the game's own line back.
func textCommand(ui *UI, game Game, args []string) error {
	if len(args) == 0 {
		return errors.New("name lines to change, for example: dlmod text hero_haze:n=Shade Citadel_Haze_HeroTag_1=Sneaky")
	}
	if gameRunning() {
		return errRunning
	}
	project, err := loadTextProject(game)
	if err != nil {
		return err
	}
	tokens := map[string]map[string]bool{} // file -> keys it has
	for _, name := range textFiles {
		file := localizationFile(name, "english")
		data, err := os.ReadFile(filepath.Join(game.Citadel(), filepath.FromSlash(file)))
		if err != nil {
			return fmt.Errorf("the game's %s could not be read: %v", file, err)
		}
		tokens[file] = map[string]bool{}
		for _, t := range parseTokens(string(data)) {
			tokens[file][t.Key] = true
		}
	}
	for _, arg := range args {
		key, value, ok := strings.Cut(arg, "=")
		if !ok || key == "" {
			return fmt.Errorf("%s is not KEY=VALUE", arg)
		}
		found := false
		for file, keys := range tokens {
			if !keys[key] {
				continue
			}
			found = true
			if value == "" {
				delete(project.Edits[file], key)
				if len(project.Edits[file]) == 0 {
					delete(project.Edits, file)
				}
				ui.say("%s is the game's own text again.", key)
				continue
			}
			if project.Edits[file] == nil {
				project.Edits[file] = map[string]string{}
			}
			project.Edits[file][key] = value
			ui.say("%s: %s", key, value)
		}
		if !found {
			return fmt.Errorf("the game's hero text has no line %s", key)
		}
	}
	if err := project.save(game); err != nil {
		return err
	}
	return buildTextMod(ui, game, project)
}
