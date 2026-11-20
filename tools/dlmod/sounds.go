package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/MrHakan/deadlock-modkit/tools/dlmod/source2"
)

// Custom sounds: pick any sound of the game, drop a .wav or .mp3 on it, and dlmod compiles it
// (source2.CompileSound, no Valve tools) into one mod, "Custom sounds", which it rebuilds and
// installs after every change.

const customSoundsName = "Custom sounds"

// soundProject is the list of replaced sounds, kept in game/dlmod/sounds.
type soundProject struct {
	Replaced map[string]soundReplacement `json:"replaced"` // game path -> replacement
}

type soundReplacement struct {
	Source   string `json:"source"`   // the file the player chose, for display
	Compiled string `json:"compiled"` // the compiled .vsnd_c, inside game/dlmod/sounds/files
}

func (g Game) soundsDir() string { return filepath.Join(g.StateDir(), "sounds") }

func loadSoundProject(game Game) (*soundProject, error) {
	project := &soundProject{Replaced: map[string]soundReplacement{}}
	data, err := os.ReadFile(filepath.Join(game.soundsDir(), "replaced.json"))
	if errors.Is(err, os.ErrNotExist) {
		return project, nil
	}
	if err == nil {
		err = json.Unmarshal(data, project)
	}
	if project.Replaced == nil {
		project.Replaced = map[string]soundReplacement{}
	}
	return project, err
}

func (p *soundProject) save(game Game) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(game.soundsDir(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(game.soundsDir(), "replaced.json"), data, 0o644)
}

// gameSounds opens the game's own archive and lists its sounds.
func gameSounds(game Game) (*source2.VPKIndex, []string, error) {
	isSound := func(name string) bool {
		return strings.HasPrefix(name, "sounds/") && strings.HasSuffix(name, ".vsnd_c")
	}
	index, err := source2.OpenVPKFiltered(filepath.Join(game.Citadel(), "pak01_dir.vpk"), isSound)
	if err != nil {
		return nil, nil, fmt.Errorf("the game's sounds could not be read: %v", err)
	}
	sounds := make([]string, 0, len(index.Files))
	for name := range index.Files {
		sounds = append(sounds, name)
	}
	sort.Strings(sounds)
	return index, sounds, nil
}

// soundHero returns the hero a sound belongs to (sounds/vo/haze/... or sounds/abilities/haze/...).
func soundHero(name string) string {
	parts := strings.SplitN(name, "/", 4)
	if len(parts) == 4 && (parts[1] == "vo" || parts[1] == "abilities") && heroNames[parts[2]] != "" {
		return parts[2]
	}
	return ""
}

func heroLabel(codename string) string {
	if name := heroNames[codename]; name != "" {
		return name
	}
	return codename
}

// replaceSound compiles audio for one game path and records it.
func replaceSound(game Game, project *soundProject, gamePath, audioFile string) (source2.SoundInfo, error) {
	audio, err := os.ReadFile(audioFile)
	if err != nil {
		return source2.SoundInfo{}, err
	}
	compiled, info, err := source2.CompileSound(audio)
	if err != nil {
		return info, fmt.Errorf("%s: %v", filepath.Base(audioFile), err)
	}
	sum := sha1.Sum(compiled)
	name := hex.EncodeToString(sum[:10]) + ".vsnd_c"
	folder := filepath.Join(game.soundsDir(), "files")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return info, err
	}
	if err := os.WriteFile(filepath.Join(folder, name), compiled, 0o644); err != nil {
		return info, err
	}
	project.Replaced[gamePath] = soundReplacement{Source: filepath.Base(audioFile), Compiled: name}
	return info, project.save(game)
}

// installCustomSounds packs every replacement into one VPK and installs it in place of the
// previous version, or removes the mod when nothing is replaced.
func (s *Store) installCustomSounds(p prompter, project *soundProject) error {
	entries, err := s.entries()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Mod != nil && entry.Mod.Name == customSoundsName {
			if err := s.remove(entry); err != nil {
				return err
			}
		}
	}
	if len(project.Replaced) == 0 {
		p.say("No sounds are replaced; the %s mod is removed.", customSoundsName)
		return nil
	}
	files := map[string][]byte{}
	for gamePath, replacement := range project.Replaced {
		data, err := os.ReadFile(filepath.Join(s.game.soundsDir(), "files", replacement.Compiled))
		if err != nil {
			return fmt.Errorf("the compiled sound for %s is missing: %v", gamePath, err)
		}
		files[gamePath] = data
	}
	vpk := filepath.Join(s.game.soundsDir(), "pak01_dir.vpk")
	if err := os.WriteFile(vpk, source2.WriteVPK(files), 0o644); err != nil {
		return err
	}
	defer os.Remove(vpk)
	return s.install(p, vpk, customSoundsName)
}

// openInPlayer hands a file to the system's default program, which plays audio.
var openInPlayer = func(file string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		command = exec.Command("cmd", "/c", "start", "", file)
	case "darwin":
		command = exec.Command("open", file)
	default:
		command = exec.Command("xdg-open", file)
	}
	return command.Start()
}

// playSound writes a compiled sound as .wav or .mp3 into the state folder and opens it.
func playSound(game Game, compiled []byte, label string) error {
	audio, extension, err := source2.PlayableSound(compiled)
	if err != nil {
		return err
	}
	folder := filepath.Join(game.soundsDir(), "listen")
	os.RemoveAll(folder) // only the last sound played is kept
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return err
	}
	file := filepath.Join(folder, strings.TrimSuffix(filepath.Base(label), ".vsnd_c")+extension)
	if err := os.WriteFile(file, audio, 0o644); err != nil {
		return err
	}
	return openInPlayer(file)
}

// --- full-screen view ---

type soundScreen struct {
	index         *source2.VPKIndex
	all           []string
	heroes        []string
	hero          int // index into heroes, -1 for every sound
	query         []rune
	typing        bool
	results       []string
	selected, top int
	project       *soundProject
	replacing     string // the game path a dropped file will replace
}

func (s *soundScreen) refilter() {
	words := strings.Fields(strings.ToLower(string(s.query)))
	s.results = s.results[:0]
	for _, name := range s.all {
		if s.hero >= 0 && soundHero(name) != s.heroes[s.hero] {
			continue
		}
		lower := strings.ToLower(name)
		match := true
		for _, word := range words {
			if !strings.Contains(lower, word) {
				match = false
				break
			}
		}
		if match {
			s.results = append(s.results, name)
		}
	}
	s.selected, s.top = 0, 0
}

func (t *tui) openSounds() {
	if t.sounds == nil {
		t.busy = "Reading the game's sounds…"
		t.render()
		index, all, err := gameSounds(t.game)
		t.busy = ""
		if err != nil {
			t.note(toneBad, err.Error())
			return
		}
		project, err := loadSoundProject(t.game)
		if err != nil {
			t.note(toneBad, "game/dlmod/sounds/replaced.json could not be read: "+err.Error())
			return
		}
		s := &soundScreen{index: index, all: all, hero: -1, project: project}
		seen := map[string]bool{}
		for _, name := range all {
			if hero := soundHero(name); hero != "" && !seen[hero] {
				seen[hero] = true
				s.heroes = append(s.heroes, hero)
			}
		}
		sort.Slice(s.heroes, func(a, b int) bool { return heroLabel(s.heroes[a]) < heroLabel(s.heroes[b]) })
		t.sounds = s
		s.refilter()
	}
	t.mode = modeSounds
	t.notes = nil
}

func (t *tui) currentSound() string {
	s := t.sounds
	if s.selected < 0 || s.selected >= len(s.results) {
		return ""
	}
	return s.results[s.selected]
}

func (t *tui) pressSounds(k key) bool {
	s := t.sounds
	if s.typing {
		switch k.name {
		case "char":
			s.query = append(s.query, k.r)
		case "backspace":
			if len(s.query) > 0 {
				s.query = s.query[:len(s.query)-1]
			}
		case "ctrl+u":
			s.query = nil
		case "enter", "esc", "up", "down":
			s.typing = false
		}
		s.refilter()
		return false
	}
	move := func(delta int) { s.selected = max(0, min(len(s.results)-1, s.selected+delta)) }
	switch k.name {
	case "up":
		move(-1)
	case "down":
		move(1)
	case "pgup":
		move(-15)
	case "pgdn":
		move(15)
	case "home":
		move(-len(s.results))
	case "end":
		move(len(s.results))
	case "left", "right":
		delta := 1
		if k.name == "left" {
			delta = -1
		}
		s.hero = (s.hero+1+delta+len(s.heroes)+1)%(len(s.heroes)+1) - 1
		s.refilter()
	case "enter":
		t.playSelected(false)
	case "esc", "ctrl+c":
		t.mode, t.notes = modeList, nil
	case "char":
		switch k.r {
		case 'q', 'Q':
			t.mode, t.notes = modeList, nil
		case 'k':
			move(-1)
		case 'j':
			move(1)
		case '/', 's':
			s.typing = true
		case 'r', 'i', '+':
			if name := t.currentSound(); name != "" {
				s.replacing = name
				t.mode, t.purpose, t.input, t.under = modePrompt, "sound", nil, modeSounds
			}
		case 'p':
			t.playSelected(true)
		case 'x', 'd', '-':
			t.restoreSound()
		case '?', 'h':
			t.mode, t.under = modeHelp, modeSounds
		}
	}
	return false
}

func (t *tui) playSelected(replacement bool) {
	name := t.currentSound()
	if name == "" {
		return
	}
	t.notes = nil
	var data []byte
	var err error
	if replacement {
		chosen, ok := t.sounds.project.Replaced[name]
		if !ok {
			t.note(toneInfo, "This sound is not replaced; Enter plays the game's own.")
			return
		}
		data, err = os.ReadFile(filepath.Join(t.game.soundsDir(), "files", chosen.Compiled))
	} else {
		data, err = t.sounds.index.Read(name)
	}
	if err == nil {
		err = playSound(t.game, data, name)
	}
	if err != nil {
		t.note(toneBad, err.Error())
		return
	}
	t.note(toneInfo, "Playing "+filepath.Base(name)+" in your default player.")
}

// replaceWith is called with the file dropped while the sound prompt was open.
func (t *tui) replaceWith(file string) {
	s := t.sounds
	target := s.replacing
	t.mode = modeSounds
	if target == "" {
		return
	}
	t.act(func() error {
		info, err := replaceSound(t.game, s.project, target, file)
		if err != nil {
			return err
		}
		t.note(toneGood, fmt.Sprintf("%s now plays %s (%s, %.1f s).", filepath.Base(target), filepath.Base(file),
			strings.ToLower(info.Format), info.Duration))
		return t.store.installCustomSounds(t, s.project)
	})
	t.mode = modeSounds
}

func (t *tui) restoreSound() {
	s := t.sounds
	name := t.currentSound()
	if _, ok := s.project.Replaced[name]; !ok {
		return
	}
	t.act(func() error {
		delete(s.project.Replaced, name)
		if err := s.project.save(t.game); err != nil {
			return err
		}
		t.note(toneInfo, filepath.Base(name)+" plays the game's own sound again.")
		return t.store.installCustomSounds(t, s.project)
	})
	t.mode = modeSounds
}

func (t *tui) drawSounds(c *canvas, room int) {
	p, s := t.p, t.sounds
	hero := "All sounds"
	if s.hero >= 0 {
		hero = heroLabel(s.heroes[s.hero])
	}
	search, searchStyle := string(s.query), ""
	if s.typing {
		search, searchStyle = search+"▏", p.bold
	} else if search == "" {
		search, searchStyle = "(press /)", p.dim
	}
	c.line(seg{" Hero ", p.dim}, seg{hero, p.bold + p.cyan}, seg{"   Search ", p.dim}, seg{search, searchStyle},
		seg{fmt.Sprintf("   %s · %d replaced", plural(len(s.results), "%d sound", "%d sounds"), len(s.project.Replaced)), p.dim})
	c.line(seg{strings.Repeat("─", c.width), p.dim})
	rows := max(room-2, 1)
	if s.selected < s.top {
		s.top = s.selected
	}
	if s.selected >= s.top+rows {
		s.top = s.selected - rows + 1
	}
	for i := 0; i < rows; i++ {
		index := s.top + i
		if index >= len(s.results) {
			if i == 0 {
				c.line(seg{"  No sounds match. Change the hero (←/→) or the search (/).", p.dim})
			} else {
				c.blank()
			}
			continue
		}
		name := s.results[index]
		style, marker := "", "  "
		if index == s.selected {
			style, marker = p.row+p.bold, "› "
		}
		shown := strings.TrimPrefix(strings.TrimSuffix(name, ".vsnd_c"), "sounds/")
		replaced := ""
		if chosen, ok := s.project.Replaced[name]; ok {
			replaced = " ← " + chosen.Source
		}
		width := c.width - 2 - len([]rune(replaced))
		c.line(seg{marker + pad(truncateMiddle(shown, width), width), style}, seg{replaced, p.green})
	}
}

func (t *tui) soundHints() [][2]string {
	if t.sounds != nil && t.sounds.typing {
		return [][2]string{{"type", "search"}, {"Enter", "done"}, {"Ctrl+U", "clear"}}
	}
	return [][2]string{{"↑↓", "select"}, {"←→", "hero"}, {"/", "search"}, {"Enter", "play original"},
		{"r", "replace with a file"}, {"p", "play yours"}, {"x", "restore"}, {"Esc", "back"}}
}
