package main

import (
	"fmt"
	"io"
	"net/http"
	"path"
	"sort"
	"strings"

	"github.com/MrHakan/deadlock-modkit/tools/dlmod/source2"
)

// The studio's Sounds tab: a hero's voice lines, ability sounds, footsteps and movement, to play
// and replace. It shares the custom sounds mod and its project file with the sounds screen of the
// full-screen view, and reads both from disk on every change so neither undoes the other.

// download is a file an API call sends back as it is, such as a sound to play.
type download struct {
	data        []byte
	contentType string
}

// heroSoundGroup names the kind of a hero's sound from the folders around the hero's own:
// sounds/vo/wraith/ping/x.vsnd_c is a ping, sounds/abilities/wraith/x.vsnd_c an ability.
func heroSoundGroup(name, hero string) string {
	parts := strings.Split(name, "/")
	for i, part := range parts[:len(parts)-1] {
		if part != hero || i == 0 {
			continue
		}
		rest := parts[i+1 : len(parts)-1]
		switch kind := parts[i-1]; {
		case kind == "vo" && len(rest) > 0 && rest[0] == "ping":
			return "Pings"
		case kind == "vo":
			return "Voice"
		case kind == "abilities":
			return "Abilities"
		case kind == "footsteps":
			return "Footsteps"
		case kind == "movement":
			return "Movement"
		case strings.HasPrefix(kind, "weapon"):
			return "Weapon"
		default:
			return "Other"
		}
	}
	return ""
}

var soundGroupOrder = map[string]int{"Voice": 0, "Pings": 1, "Abilities": 2, "Weapon": 3, "Movement": 4, "Footsteps": 5, "Other": 6}

// soundIndex reads the game's sounds once per studio.
func (s *studio) soundIndex() (*source2.VPKIndex, []string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sounds == nil {
		index, all, err := gameSounds(s.game)
		if err != nil {
			return nil, nil, err
		}
		s.sounds, s.allSounds = index, all
	}
	return s.sounds, s.allSounds, nil
}

// heroSounds lists one hero's sounds, by group.
func (s *studio) heroSounds(hero string) ([]string, map[string]string, error) {
	_, all, err := s.soundIndex()
	if err != nil {
		return nil, nil, err
	}
	var names []string
	groups := map[string]string{}
	for _, name := range all {
		if group := heroSoundGroup(name, hero); group != "" {
			names = append(names, name)
			groups[name] = group
		}
	}
	sort.SliceStable(names, func(a, b int) bool {
		ga, gb := soundGroupOrder[groups[names[a]]], soundGroupOrder[groups[names[b]]]
		if ga != gb {
			return ga < gb
		}
		return names[a] < names[b]
	})
	return names, groups, nil
}

func (s *studio) soundList(r *http.Request) (any, error) {
	var request struct {
		Hero string `json:"hero"`
	}
	if err := readJSON(r, &request); err != nil {
		return nil, err
	}
	names, groups, err := s.heroSounds(request.Hero)
	if err != nil {
		return nil, err
	}
	project, err := loadSoundProject(s.game)
	if err != nil {
		return nil, err
	}
	type sound struct {
		Path     string `json:"path"`
		Name     string `json:"name"`
		Group    string `json:"group"`
		Replaced string `json:"replaced,omitempty"` // the file it plays instead
	}
	out := make([]sound, 0, len(names))
	for _, name := range names {
		out = append(out, sound{name, strings.TrimSuffix(path.Base(name), ".vsnd_c"), groups[name], project.Replaced[name].Source})
	}
	return map[string]any{"sounds": out, "replacedTotal": len(project.Replaced)}, nil
}

// soundAudio sends a sound to play in the page: the game's own, or the one replacing it.
func (s *studio) soundAudio(r *http.Request) (any, error) {
	name := r.URL.Query().Get("path")
	var compiled []byte
	if r.URL.Query().Get("which") == "yours" {
		project, err := loadSoundProject(s.game)
		if err != nil {
			return nil, err
		}
		if compiled, err = project.read(name); err != nil {
			return nil, err
		}
	} else {
		index, _, err := s.soundIndex()
		if err != nil {
			return nil, err
		}
		if _, ok := index.Files[name]; !ok {
			return nil, fmt.Errorf("the game has no sound %s", name)
		}
		if compiled, err = index.Read(name); err != nil {
			return nil, err
		}
	}
	audio, extension, err := source2.PlayableSound(compiled)
	if err != nil {
		return nil, err
	}
	contentType := "audio/wav"
	if extension == ".mp3" {
		contentType = "audio/mpeg"
	}
	return download{audio, contentType}, nil
}

// soundReplace takes dropped audio files. With a path it replaces that sound; without one each
// file replaces the hero's sound of the same name (vo_wraith_kill_01.wav replaces
// vo_wraith_kill_01.vsnd_c), so a whole voice pack can be dropped at once. The custom sounds mod
// is then rebuilt once.
func (s *studio) soundReplace(r *http.Request) (any, error) {
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		return nil, fmt.Errorf("the upload failed: %v", err)
	}
	if gameRunning() {
		return nil, errRunning
	}
	target := r.FormValue("path")
	byName := map[string]string{}
	if target == "" {
		names, _, err := s.heroSounds(r.FormValue("hero"))
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			byName[strings.ToLower(strings.TrimSuffix(path.Base(name), ".vsnd_c"))] = name
		}
	} else if index, _, err := s.soundIndex(); err != nil {
		return nil, err
	} else if _, ok := index.Files[target]; !ok {
		return nil, fmt.Errorf("the game has no sound %s", target)
	}
	project, err := loadSoundProject(s.game)
	if err != nil {
		return nil, err
	}
	p := &webPrompter{}
	var problems []string // files that replaced nothing, shown apart from what went well
	replaced := 0
	for _, header := range r.MultipartForm.File["file"] {
		name := path.Base(strings.ReplaceAll(header.Filename, "\\", "/"))
		gamePath := target
		if gamePath == "" {
			if gamePath = byName[strings.ToLower(strings.TrimSuffix(name, path.Ext(name)))]; gamePath == "" {
				problems = append(problems, name+": no sound of this hero has that name, so it was skipped.")
				continue
			}
		}
		f, err := header.Open()
		if err != nil {
			return nil, err
		}
		audio, err := io.ReadAll(io.LimitReader(f, 256<<20))
		f.Close()
		if err != nil {
			return nil, err
		}
		info, err := replaceSoundData(project, gamePath, name, audio)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		replaced++
		p.say("%s now plays %s (%s, %.1f s).", path.Base(gamePath), name, strings.ToLower(info.Format), info.Duration)
	}
	if replaced > 0 {
		if err := s.rebuild(p, project); err != nil {
			return nil, err
		}
	}
	return map[string]any{"messages": p.messages, "problems": problems, "replaced": replaced}, nil
}

func (s *studio) soundRestore(r *http.Request) (any, error) {
	project, err := loadSoundProject(s.game)
	if err != nil {
		return nil, err
	}
	return s.restore(r, project, "%d sound plays the game's own again.", "%d sounds play the game's own again.")
}

// restore takes {"paths": [...]} and puts the game's own files back for them.
func (s *studio) restore(r *http.Request, project *replacementProject, one, many string) (any, error) {
	var request struct {
		Paths []string `json:"paths"`
	}
	if err := readJSON(r, &request); err != nil {
		return nil, err
	}
	if gameRunning() {
		return nil, errRunning
	}
	restored, err := project.restore(request.Paths)
	if err != nil {
		return nil, err
	}
	if restored == 0 {
		return map[string]any{"messages": []string{"Nothing to restore."}}, nil
	}
	p := &webPrompter{}
	p.say("%s", plural(restored, one, many))
	if err := s.rebuild(p, project); err != nil {
		return nil, err
	}
	return map[string]any{"messages": p.messages}, nil
}

// rebuild reinstalls a project's mod from a fresh read of the mod list.
func (s *studio) rebuild(p *webPrompter, project *replacementProject) error {
	store, err := openStore(s.game)
	if err != nil {
		return err
	}
	if err := store.installReplacements(p, project); err != nil {
		return err
	}
	return ensurePatched(p, s.game)
}
