package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The search paths that make Deadlock load citadel/addons. Game citadel/addons must come before
// Game citadel so mods override game files. Without explicit Mod and Write keys the engine would
// treat the first Game path (addons) as the MOD and write path, and settings such as keybinds
// would be saved into addons, so the Mod and Write lines keep them in citadel and core.
type kv struct{ key, value string }

var (
	citadelLines = []kv{{"Game", "citadel/addons"}, {"Mod", "citadel"}, {"Write", "citadel"}}
	coreLines    = []kv{{"Mod", "core"}, {"Write", "core"}}
	markerLine   = regexp.MustCompile(`^// dlmod: the next (\d+) lines? (?:was|were) added by dlmod`)
)

func (k kv) normalized() kv { return kv{strings.ToLower(k.key), strings.ToLower(k.value)} }

func parseLine(line string) kv {
	if index := strings.Index(line, "//"); index >= 0 {
		line = line[:index]
	}
	fields := strings.Fields(line)
	switch len(fields) {
	case 0:
		return kv{}
	case 1:
		return kv{strings.ToLower(strings.Trim(fields[0], `"`)), ""}
	default:
		return kv{strings.ToLower(strings.Trim(fields[0], `"`)), strings.ToLower(strings.Trim(fields[1], `"`))}
	}
}

func splitLines(text string) ([]string, string) {
	eol := "\n"
	if strings.Contains(text, "\r\n") {
		eol = "\r\n"
	}
	return strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n"), eol
}

// searchPaths returns the line range inside the braces of the SearchPaths block.
func searchPaths(lines []string) (int, int, error) {
	for i, line := range lines {
		if parseLine(line).key != "searchpaths" {
			continue
		}
		depth, start := 0, -1
		for j := i; j < len(lines); j++ {
			code := lines[j]
			if index := strings.Index(code, "//"); index >= 0 {
				code = code[:index]
			}
			for _, char := range code {
				switch char {
				case '{':
					depth++
					if depth == 1 {
						start = j + 1
					}
				case '}':
					depth--
					if depth == 0 && start >= 0 {
						return start, j, nil
					}
				}
			}
		}
		return 0, 0, errors.New("gameinfo.gi: the SearchPaths block is not closed")
	}
	return 0, 0, errors.New("gameinfo.gi: the SearchPaths block was not found")
}

type gameinfoState struct {
	Loaded  bool     // citadel/addons is a search path
	Missing []string // Mod and Write lines that keep settings out of addons
}

func inspectGameinfo(text string) (gameinfoState, error) {
	lines, _ := splitLines(text)
	start, end, err := searchPaths(lines)
	if err != nil {
		return gameinfoState{}, err
	}
	present := map[kv]bool{}
	for _, line := range lines[start:end] {
		present[parseLine(line)] = true
	}
	state := gameinfoState{Loaded: present[citadelLines[0].normalized()]}
	for _, line := range append(slices.Clone(citadelLines[1:]), coreLines...) {
		if !present[line.normalized()] {
			state.Missing = append(state.Missing, line.key+" "+line.value)
		}
	}
	return state, nil
}

// patchGameinfo adds the missing search paths, each group under a marker comment that
// unpatchGameinfo uses to remove exactly what dlmod added.
func patchGameinfo(text string) (string, bool, error) {
	lines, eol := splitLines(text)
	start, end, err := searchPaths(lines)
	if err != nil {
		return "", false, err
	}
	find := func(want kv) int {
		for i := start; i < end; i++ {
			if parseLine(lines[i]) == want.normalized() {
				return i
			}
		}
		return -1
	}
	missing := func(group []kv) []kv {
		var result []kv
		for _, line := range group {
			if find(line) < 0 {
				result = append(result, line)
			}
		}
		return result
	}
	citadel := find(kv{"Game", "citadel"})
	if citadel < 0 {
		return "", false, errors.New("gameinfo.gi has no \"Game citadel\" search path; its layout changed, " +
			"so dlmod will not edit it")
	}
	type insertion struct {
		at    int
		lines []kv
	}
	insertions := []insertion{{citadel, missing(citadelLines)}}
	if core := find(kv{"Game", "core"}); core >= 0 {
		insertions = append(insertions, insertion{core, missing(coreLines)})
	}
	slices.SortFunc(insertions, func(a, b insertion) int { return b.at - a.at })
	changed := false
	for _, insert := range insertions {
		if len(insert.lines) == 0 {
			continue
		}
		indent := lines[insert.at][:len(lines[insert.at])-len(strings.TrimLeft(lines[insert.at], " \t"))]
		block := []string{indent + marker(len(insert.lines))}
		for _, line := range insert.lines {
			block = append(block, indent+line.key+"\t\t\t\t"+line.value)
		}
		lines = slices.Insert(lines, insert.at, block...)
		changed = true
	}
	return strings.Join(lines, eol), changed, nil
}

func marker(count int) string {
	if count == 1 {
		return "// dlmod: the next line was added by dlmod (\"dlmod unpatch\" removes it)"
	}
	return fmt.Sprintf("// dlmod: the next %d lines were added by dlmod (\"dlmod unpatch\" removes them)", count)
}

func unpatchGameinfo(text string) (string, bool, error) {
	allowed := map[kv]bool{}
	for _, line := range append(slices.Clone(citadelLines), coreLines...) {
		allowed[line.normalized()] = true
	}
	lines, eol := splitLines(text)
	var kept []string
	changed := false
	for i := 0; i < len(lines); i++ {
		match := markerLine.FindStringSubmatch(strings.TrimSpace(lines[i]))
		if match == nil {
			kept = append(kept, lines[i])
			continue
		}
		count, _ := strconv.Atoi(match[1])
		if i+count >= len(lines) {
			return "", false, errEditedByHand
		}
		for _, line := range lines[i+1 : i+1+count] {
			if !allowed[parseLine(line)] {
				return "", false, errEditedByHand
			}
		}
		i += count
		changed = true
	}
	return strings.Join(kept, eol), changed, nil
}

var errEditedByHand = errors.New("gameinfo.gi was edited after dlmod changed it, so dlmod will not touch it. " +
	"In Steam, use Properties > Installed Files > Verify integrity of game files to restore it")

func (g Game) readGameinfo() (string, gameinfoState, error) {
	data, err := os.ReadFile(g.GameInfo())
	if err != nil {
		return "", gameinfoState{}, err
	}
	state, err := inspectGameinfo(string(data))
	return string(data), state, err
}

// patch makes the game load citadel/addons and keeps a copy of the previous gameinfo.gi.
func (g Game) patch() (bool, error) {
	text, _, err := g.readGameinfo()
	if err != nil {
		return false, err
	}
	patched, changed, err := patchGameinfo(text)
	if err != nil || !changed {
		return false, err
	}
	if err := os.MkdirAll(g.StateDir(), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(filepath.Join(g.StateDir(), "gameinfo.gi.bak"), []byte(text), 0o644); err != nil {
		return false, err
	}
	if err := os.MkdirAll(g.Addons(), 0o755); err != nil {
		return false, err
	}
	_ = os.Chmod(g.GameInfo(), 0o644) // clears the read-only flag on Windows
	return true, writeFileAtomic(g.GameInfo(), []byte(patched))
}

func (g Game) unpatch() (bool, error) {
	text, _, err := g.readGameinfo()
	if err != nil {
		return false, err
	}
	restored, changed, err := unpatchGameinfo(text)
	if err != nil || !changed {
		return false, err
	}
	_ = os.Chmod(g.GameInfo(), 0o644)
	return true, writeFileAtomic(g.GameInfo(), []byte(restored))
}

func writeFileAtomic(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".dlmod-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Chmod(file.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
