package main

import (
	"errors"
	"fmt"

	"github.com/MrHakan/deadlock-modkit/tools/dlmod/source2"
)

// effectsCommand recolours a hero's ability effects from the command line, as the studio's
// Effects tab does: dlmod effects <hero> <#rrggbb> [strength] pulls every colour towards the
// tint (the weapon's muzzle flashes and tracers too); dlmod effects <hero> palette [name] uses
// a multi-tone palette (default: cobalt); dlmod effects <hero> restore undoes it.
func effectsCommand(ui *UI, store *Store, game Game, args []string) error {
	if len(args) < 2 {
		return errors.New("name a hero and a colour or palette, for example: dlmod effects ivy #2467ff 0.85, dlmod effects ivy palette, or dlmod effects ivy restore")
	}
	if gameRunning() {
		return errRunning
	}
	hero, err := findHero(args[0])
	if err != nil {
		return err
	}
	s := &studio{game: game}
	abilities, weapon, err := s.heroParticles(hero)
	if err != nil {
		return err
	}
	if len(abilities) == 0 {
		return fmt.Errorf("%s has no ability effects of their own in the game", heroDisplayName(hero))
	}
	project, err := loadReplacements(game.effectsDir(), customEffectsName)
	if err != nil {
		return err
	}
	files := append(append([]string{}, abilities...), weapon...)
	project.restore(files) // start again from the game's files
	if args[1] != "restore" {
		settings := effectSettings{Saturation: 1, Brightness: 1, Weapon: true}
		if args[1] == "palette" {
			palName := "cobalt"
			if len(args) > 2 {
				palName = args[2]
			}
			pal, err := LookupPalette(palName)
			if err != nil {
				return err
			}
			settings.Palette = pal.Name
		} else {
			settings.Tint = args[1]
			settings.Strength = 0.85
			if _, ok := parseHex(settings.Tint); !ok {
				return fmt.Errorf("%s is not a colour like #2467ff (or 'palette' / 'restore')", args[1])
			}
			if len(args) > 2 {
				if _, err := fmt.Sscan(args[2], &settings.Strength); err != nil {
					return fmt.Errorf("%s is not a strength from 0 to 1", args[2])
				}
			}
		}
		index, err := s.particleIndex()
		if err != nil {
			return err
		}
		label := settings.describe()
		changedFiles, changedColors := 0, 0
		for _, file := range files {
			data, err := index.Read(file)
			if err != nil {
				return err
			}
			out, changed, err := source2.RecolorParticle(data, settings.recolor)
			if err != nil {
				return fmt.Errorf("%s: %v", file, err)
			}
			if changed == 0 {
				continue
			}
			if err := project.add(file, label, out); err != nil {
				return err
			}
			changedFiles++
			changedColors += changed
		}
		all, err := loadEffectSettings(game)
		if err != nil {
			return err
		}
		all[hero] = settings
		if err := saveEffectSettings(game, all); err != nil {
			return err
		}
		ui.say("%s's effects %s: %d colours in %d effects.", heroDisplayName(hero), label, changedColors, changedFiles)
	}
	if err := store.installReplacements(ui, project); err != nil {
		return err
	}
	return ensurePatched(ui, game)
}
