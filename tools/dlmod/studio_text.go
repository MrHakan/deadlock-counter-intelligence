package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MrHakan/deadlock-modkit/tools/dlmod/source2"
)

// The studio's Text tab: a hero's ability names and descriptions, role, playstyle and lore, as
// the game shows them in a language. The game keeps them in loose files under
// citadel/resource/localization; a mod holding a file of the same path wins over it, since
// dlmod puts the addons first. Only the changed lines are kept (game/dlmod/text/edits.json) and
// laid over the game's current file each time the mod is built, so an update to the rest of the
// file is not lost.

const customTextName = "Custom text"

// textFiles are the localization files a hero's text is in, by name; the language completes them.
var textFiles = []string{"citadel_heroes", "citadel_gc_hero_names"}

func (g Game) textDir() string { return filepath.Join(g.StateDir(), "text") }

func localizationFile(name, language string) string {
	return "resource/localization/" + name + "/" + name + "_" + language + ".txt"
}

// textProject holds the edited lines: file path (inside citadel) -> key -> new text.
type textProject struct {
	Edits map[string]map[string]string `json:"edits"`
}

func loadTextProject(game Game) (*textProject, error) {
	project := &textProject{Edits: map[string]map[string]string{}}
	data, err := os.ReadFile(filepath.Join(game.textDir(), "edits.json"))
	if errors.Is(err, os.ErrNotExist) {
		return project, nil
	}
	if err == nil {
		err = json.Unmarshal(data, project)
	}
	if project.Edits == nil {
		project.Edits = map[string]map[string]string{}
	}
	return project, err
}

func (p *textProject) save(game Game) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(game.textDir(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(game.textDir(), "edits.json"), data, 0o644)
}

// kvToken is one "key" "value" line of a localization file: where its value's text sits in the
// file (inside the quotes, still escaped) and the hero section it is under.
type kvToken struct {
	Key, Value string // the value unescaped
	start, end int
	Section    string // the comment heading above it, such as "Wraith"
}

// parseTokens reads the "key" "value" pairs of a KeyValues localization file. A comment line on
// its own, such as // Wraith, starts a section.
func parseTokens(text string) []kvToken {
	var out []kvToken
	section := ""
	pendingKey, pending := "", false
	for i := 0; i < len(text); {
		c := text[i]
		switch {
		case c == '/' && i+1 < len(text) && text[i+1] == '/':
			end := strings.IndexByte(text[i:], '\n')
			if end < 0 {
				end = len(text) - i
			}
			comment := strings.TrimSpace(text[i+2 : i+end])
			if comment != "" && !strings.HasPrefix(comment, "-") && !strings.ContainsAny(comment, "\"") {
				section = comment
			}
			i += end
		case c == '"':
			start := i + 1
			j := start
			for j < len(text) && text[j] != '"' {
				if text[j] == '\\' {
					j++
				}
				j++
			}
			raw := text[start:min(j, len(text))]
			if pending {
				out = append(out, kvToken{Key: pendingKey, Value: unescapeKV(raw), start: start, end: min(j, len(text)), Section: section})
				pending = false
			} else {
				pendingKey, pending = raw, true
			}
			i = j + 1
		case c == '{' || c == '}':
			pending = false
			i++
		default:
			i++
		}
	}
	return out
}

func unescapeKV(s string) string {
	return strings.NewReplacer(`\"`, `"`, `\n`, "\n", `\t`, "\t", `\\`, `\`).Replace(s)
}

func escapeKV(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\t", `\t`).Replace(s)
}

// applyEdits writes new values for keys into a localization file, leaving the rest as it is.
func applyEdits(text string, edits map[string]string) string {
	tokens := parseTokens(text)
	sort.Slice(tokens, func(a, b int) bool { return tokens[a].start > tokens[b].start }) // from the end, so offsets hold
	for _, token := range tokens {
		if value, ok := edits[token.Key]; ok {
			text = text[:token.start] + escapeKV(value) + text[token.end:]
		}
	}
	return text
}

// textLanguages lists the languages the game has its hero text in.
func textLanguages(game Game) []string {
	matches, _ := filepath.Glob(filepath.Join(game.Citadel(), filepath.FromSlash(localizationFile(textFiles[0], "*"))))
	var out []string
	for _, m := range matches {
		base := strings.TrimSuffix(filepath.Base(m), ".txt")
		out = append(out, strings.TrimPrefix(base, textFiles[0]+"_"))
	}
	sort.Strings(out)
	return out
}

// heroTokens picks a hero's lines from a file: those under the hero's heading, and those whose
// key names the hero (hero_wraith, Citadel_Wraith_HeroTag_1).
func heroTokens(tokens []kvToken, hero string) []kvToken {
	names := heroFileNames(hero)
	display := strings.ToLower(heroDisplayName(hero))
	var out []kvToken
	for _, token := range tokens {
		key := strings.ToLower(token.Key)
		mine := strings.ToLower(token.Section) == display
		for _, name := range names {
			if strings.Contains(key, "hero_"+name) || strings.Contains(key, "_"+name+"_") || strings.HasPrefix(key, name+"_") {
				mine = true
			}
		}
		if mine && token.Key != "Language" {
			out = append(out, token)
		}
	}
	return out
}

func (s *studio) textList(r *http.Request) (any, error) {
	var request struct {
		Hero     string `json:"hero"`
		Language string `json:"language"`
	}
	if err := readJSON(r, &request); err != nil {
		return nil, err
	}
	languages := textLanguages(s.game)
	if len(languages) == 0 {
		return nil, errors.New("the game's text files were not found under citadel/resource/localization")
	}
	language := request.Language
	if language == "" || !contains(languages, language) {
		language = "english"
		if !contains(languages, language) {
			language = languages[0]
		}
	}
	project, err := loadTextProject(s.game)
	if err != nil {
		return nil, err
	}
	type entry struct {
		File     string `json:"file"`
		Key      string `json:"key"`
		Text     string `json:"text"`
		Original string `json:"original"`
		Edited   bool   `json:"edited"`
	}
	out := []entry{}
	for _, name := range textFiles {
		file := localizationFile(name, language)
		data, err := os.ReadFile(filepath.Join(s.game.Citadel(), filepath.FromSlash(file)))
		if err != nil {
			continue
		}
		for _, token := range heroTokens(parseTokens(string(data)), request.Hero) {
			text, edited := project.Edits[file][token.Key]
			if !edited {
				text = token.Value
			}
			out = append(out, entry{file, token.Key, text, token.Value, edited})
		}
	}
	return map[string]any{"languages": languages, "language": language, "entries": out}, nil
}

func contains(list []string, item string) bool {
	for _, x := range list {
		if x == item {
			return true
		}
	}
	return false
}

// textSave records edited lines ({"edits": [{file, key, text}]}), where text equal to the game's
// own drops the edit, and rebuilds the mod.
func (s *studio) textSave(r *http.Request) (any, error) {
	var request struct {
		Edits []struct {
			File, Key, Text string
		} `json:"edits"`
	}
	if err := readJSON(r, &request); err != nil {
		return nil, err
	}
	if gameRunning() {
		return nil, errRunning
	}
	project, err := loadTextProject(s.game)
	if err != nil {
		return nil, err
	}
	originals := map[string]map[string]string{}
	changed := 0
	for _, edit := range request.Edits {
		if !strings.HasPrefix(edit.File, "resource/localization/") || strings.Contains(edit.File, "..") {
			return nil, fmt.Errorf("%s is not a text file of the game", edit.File)
		}
		if originals[edit.File] == nil {
			data, err := os.ReadFile(filepath.Join(s.game.Citadel(), filepath.FromSlash(edit.File)))
			if err != nil {
				return nil, err
			}
			originals[edit.File] = map[string]string{}
			for _, token := range parseTokens(string(data)) {
				originals[edit.File][token.Key] = token.Value
			}
		}
		original, ok := originals[edit.File][edit.Key]
		if !ok {
			return nil, fmt.Errorf("%s has no line %s", edit.File, edit.Key)
		}
		if project.Edits[edit.File] == nil {
			project.Edits[edit.File] = map[string]string{}
		}
		if edit.Text == original {
			delete(project.Edits[edit.File], edit.Key)
		} else {
			project.Edits[edit.File][edit.Key] = edit.Text
		}
		changed++
	}
	for file, edits := range project.Edits {
		if len(edits) == 0 {
			delete(project.Edits, file)
		}
	}
	if err := project.save(s.game); err != nil {
		return nil, err
	}
	p := &webPrompter{}
	p.say("%s saved.", plural(changed, "%d line", "%d lines"))
	if err := s.rebuildText(p, project); err != nil {
		return nil, err
	}
	return map[string]any{"messages": p.messages}, nil
}

// rebuildText installs the studio's text edits.
func (s *studio) rebuildText(p *webPrompter, project *textProject) error {
	return buildTextMod(p, s.game, project)
}

// buildTextMod lays the edits over the game's current files and installs them as one mod.
func buildTextMod(p prompter, game Game, project *textProject) error {
	store, err := openStore(game)
	if err != nil {
		return err
	}
	entries, err := store.entries()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Mod != nil && entry.Mod.Name == customTextName {
			if err := store.remove(entry); err != nil {
				return err
			}
		}
	}
	if len(project.Edits) == 0 {
		p.say("No text is changed any more; the %s mod is removed.", customTextName)
		return nil
	}
	files := map[string][]byte{}
	for file, edits := range project.Edits {
		data, err := os.ReadFile(filepath.Join(game.Citadel(), filepath.FromSlash(file)))
		if err != nil {
			return fmt.Errorf("the game's %s could not be read: %v", file, err)
		}
		files[file] = []byte(applyEdits(string(data), edits))
	}
	if err := os.MkdirAll(game.textDir(), 0o755); err != nil {
		return err
	}
	vpk := filepath.Join(game.textDir(), "pak01_dir.vpk")
	if err := os.WriteFile(vpk, source2.WriteVPK(files), 0o644); err != nil {
		return err
	}
	defer os.Remove(vpk)
	if err := store.install(p, vpk, customTextName); err != nil {
		return err
	}
	return ensurePatched(p, game)
}
