package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/MrHakan/deadlock-modkit/tools/dlmod/source2"
)

// replacementProject is a set of the game's files replaced by the player's own (sounds, interface
// images, text), kept in a folder of game/dlmod: replaced.json says which game path plays or shows
// what, and files/ holds the compiled replacements. All of them install together as one mod,
// rebuilt after every change.
type replacementProject struct {
	dir, mod string                 // the folder and the mod's name; not saved
	Replaced map[string]replacement `json:"replaced"` // game path -> replacement
}

type replacement struct {
	Source   string `json:"source"`   // the file the player chose, for display
	Compiled string `json:"compiled"` // the compiled file, inside files/
}

func loadReplacements(dir, mod string) (*replacementProject, error) {
	project := &replacementProject{dir: dir, mod: mod, Replaced: map[string]replacement{}}
	data, err := os.ReadFile(filepath.Join(dir, "replaced.json"))
	if errors.Is(err, os.ErrNotExist) {
		return project, nil
	}
	if err == nil {
		err = json.Unmarshal(data, project)
	}
	if project.Replaced == nil {
		project.Replaced = map[string]replacement{}
	}
	if err != nil {
		err = fmt.Errorf("%s could not be read: %v", filepath.Join(dir, "replaced.json"), err)
	}
	return project, err
}

func (p *replacementProject) save() error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(p.dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(p.dir, "replaced.json"), data, 0o644)
}

// add stores a compiled replacement for a game path, named by its content, and records it.
func (p *replacementProject) add(gamePath, source string, compiled []byte) error {
	sum := sha1.Sum(compiled)
	name := hex.EncodeToString(sum[:10]) + filepath.Ext(gamePath)
	folder := filepath.Join(p.dir, "files")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(folder, name), compiled, 0o644); err != nil {
		return err
	}
	p.Replaced[gamePath] = replacement{Source: source, Compiled: name}
	return p.save()
}

// read returns the compiled replacement of a game path.
func (p *replacementProject) read(gamePath string) ([]byte, error) {
	chosen, ok := p.Replaced[gamePath]
	if !ok {
		return nil, fmt.Errorf("%s is not replaced", gamePath)
	}
	return os.ReadFile(filepath.Join(p.dir, "files", chosen.Compiled))
}

// restore forgets the replacements of the given game paths and says how many there were.
func (p *replacementProject) restore(paths []string) (int, error) {
	count := 0
	for _, name := range paths {
		if _, ok := p.Replaced[name]; ok {
			delete(p.Replaced, name)
			count++
		}
	}
	if count == 0 {
		return 0, nil
	}
	return count, p.save()
}

// installReplacements packs every replacement of a project into one VPK and installs it in place
// of the previous version, or removes the mod when nothing is replaced.
func (s *Store) installReplacements(pr prompter, p *replacementProject) error {
	entries, err := s.entries()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Mod != nil && entry.Mod.Name == p.mod {
			if err := s.remove(entry); err != nil {
				return err
			}
		}
	}
	if len(p.Replaced) == 0 {
		pr.say("Nothing is replaced any more; the %s mod is removed.", p.mod)
		return nil
	}
	files := map[string][]byte{}
	paths := make([]string, 0, len(p.Replaced))
	for gamePath := range p.Replaced {
		paths = append(paths, gamePath)
	}
	sort.Strings(paths)
	for _, gamePath := range paths {
		data, err := p.read(gamePath)
		if err != nil {
			return fmt.Errorf("the replacement for %s is missing: %v", gamePath, err)
		}
		files[gamePath] = data
	}
	vpk := filepath.Join(p.dir, "pak01_dir.vpk")
	if err := os.WriteFile(vpk, source2.WriteVPK(files), 0o644); err != nil {
		return err
	}
	defer os.Remove(vpk)
	return s.install(pr, vpk, p.mod)
}
