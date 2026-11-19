package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

const maxSlot = 99 // the game loads pak01_dir.vpk to pak99_dir.vpk from citadel/addons

var (
	addonFile   = regexp.MustCompile(`(?i)^pak(\d{2})_(dir|\d{3})\.vpk$`)
	archivePart = regexp.MustCompile(`(?i)^(.*)_(\d{3})\.vpk$`)
	slotName    = regexp.MustCompile(`^pak(\d{1,2})$`)
	riskyFile   = regexp.MustCompile(`(?i)\.(exe|dll|bat|cmd|ps1|scr|vbs|js|msi|com)$`)
)

type Mod struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Slot        int    `json:"slot"`
	Enabled     bool   `json:"enabled"`
	Source      string `json:"source,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	Installed   string `json:"installed,omitempty"`
	GameVersion int    `json:"gameVersion,omitempty"`
}

type Manifest struct {
	NextID int    `json:"nextId"`
	Mods   []*Mod `json:"mods"`
}

// Entry is one mod: a filled slot in citadel/addons, or a switched-off mod in dlmod's folder.
type Entry struct {
	Mod   *Mod // nil when the slot was filled by hand or by another tool
	Slot  int
	Files []string // full paths, pakNN_dir.vpk first
	Paths []string // game files the mod changes
	Err   error    // the VPK could not be read
}

func (e *Entry) Enabled() bool { return e.Mod == nil || e.Mod.Enabled }

func (e *Entry) Name() string {
	if e.Mod != nil {
		return e.Mod.Name
	}
	return fmt.Sprintf("pak%02d (added outside dlmod)", e.Slot)
}

func (e *Entry) load() {
	if len(e.Files) == 0 || !strings.HasSuffix(strings.ToLower(e.Files[0]), "_dir.vpk") {
		e.Err = errors.New("its pakNN_dir.vpk file is missing")
		return
	}
	e.Paths, e.Err = vpkFiles(e.Files[0])
}

type Store struct {
	game     Game
	manifest Manifest
}

func openStore(game Game) (*Store, error) {
	store := &Store{game: game, manifest: Manifest{NextID: 1}}
	data, err := os.ReadFile(store.manifestPath())
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &store.manifest); err != nil {
			return nil, fmt.Errorf("%s is damaged: %v", store.manifestPath(), err)
		}
	case !errors.Is(err, os.ErrNotExist):
		return nil, err
	}
	if store.manifest.NextID < 1 {
		store.manifest.NextID = 1
	}
	return store, nil
}

func (s *Store) manifestPath() string { return filepath.Join(s.game.StateDir(), "mods.json") }

func (s *Store) disabledDir(mod *Mod) string {
	return filepath.Join(s.game.StateDir(), "disabled", strconv.Itoa(mod.ID))
}

func (s *Store) save() error {
	// Forget mods whose files were deleted by hand.
	kept := s.manifest.Mods[:0]
	for _, mod := range s.manifest.Mods {
		folder := s.game.Addons()
		if !mod.Enabled {
			folder = s.disabledDir(mod)
		}
		if isFile(filepath.Join(folder, fmt.Sprintf("pak%02d_dir.vpk", mod.Slot))) {
			kept = append(kept, mod)
		}
	}
	s.manifest.Mods = kept
	if err := os.MkdirAll(s.game.StateDir(), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.manifest, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.manifestPath(), append(data, '\n'))
}

// slots maps each slot number in citadel/addons to its files, pakNN_dir.vpk first.
func (s *Store) slots() (map[int][]string, error) {
	listing, err := os.ReadDir(s.game.Addons())
	if errors.Is(err, os.ErrNotExist) {
		return map[int][]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := map[int][]string{}
	for _, item := range listing {
		match := addonFile.FindStringSubmatch(item.Name())
		if item.IsDir() || match == nil {
			continue
		}
		slot, _ := strconv.Atoi(match[1])
		if slot >= 1 {
			result[slot] = append(result[slot], filepath.Join(s.game.Addons(), item.Name()))
		}
	}
	for _, files := range result {
		sortParts(files)
	}
	return result, nil
}

func sortParts(files []string) {
	sort.Slice(files, func(i, j int) bool {
		a, b := strings.ToLower(files[i]), strings.ToLower(files[j])
		if strings.HasSuffix(a, "_dir.vpk") != strings.HasSuffix(b, "_dir.vpk") {
			return strings.HasSuffix(a, "_dir.vpk")
		}
		return a < b
	})
}

func (s *Store) freeSlot() (int, error) {
	used, err := s.slots()
	if err != nil {
		return 0, err
	}
	for slot := 1; slot <= maxSlot; slot++ {
		if len(used[slot]) == 0 {
			return slot, nil
		}
	}
	return 0, errors.New("all 99 mod slots are in use; remove a mod first")
}

// entries lists switched-on mods by slot, then switched-off mods.
func (s *Store) entries() ([]*Entry, error) {
	used, err := s.slots()
	if err != nil {
		return nil, err
	}
	bySlot := map[int]*Mod{}
	for _, mod := range s.manifest.Mods {
		if mod.Enabled {
			bySlot[mod.Slot] = mod
		}
	}
	numbers := make([]int, 0, len(used))
	for slot := range used {
		numbers = append(numbers, slot)
	}
	sort.Ints(numbers)
	var list []*Entry
	for _, slot := range numbers {
		entry := &Entry{Mod: bySlot[slot], Slot: slot, Files: used[slot]}
		entry.load()
		list = append(list, entry)
	}
	for _, mod := range s.manifest.Mods {
		if mod.Enabled {
			continue
		}
		entry := &Entry{Mod: mod, Slot: mod.Slot}
		if listing, err := os.ReadDir(s.disabledDir(mod)); err == nil {
			for _, item := range listing {
				entry.Files = append(entry.Files, filepath.Join(s.disabledDir(mod), item.Name()))
			}
			sortParts(entry.Files)
		}
		entry.load()
		list = append(list, entry)
	}
	return list, nil
}

// Conflict is a pair of switched-on mods that change the same game files.
type Conflict struct {
	A, B  *Entry
	Paths []string
}

func overlap(a, b []string) []string {
	seen := map[string]bool{}
	for _, path := range a {
		seen[path] = true
	}
	var shared []string
	for _, path := range b {
		if seen[path] {
			shared = append(shared, path)
			seen[path] = false
		}
	}
	return shared
}

func conflicts(entries []*Entry) []Conflict {
	var result []Conflict
	for i, a := range entries {
		for _, b := range entries[i+1:] {
			if a.Enabled() && b.Enabled() {
				if shared := overlap(a.Paths, b.Paths); len(shared) > 0 {
					result = append(result, Conflict{a, b, shared})
				}
			}
		}
	}
	return result
}

// --- installing ---

type sourceFile struct {
	name string
	open func() (io.ReadCloser, error)
}

// source is one mod inside a download: a directory file and its numbered archive parts.
type source struct {
	label string
	dir   *sourceFile
	parts map[string]*sourceFile // "000" -> file
}

// readSources finds the mods in a .zip archive or next to a .vpk file.
func readSources(file string) ([]*source, []string, func(), error) {
	if strings.EqualFold(filepath.Ext(file), ".zip") {
		archive, err := zip.OpenReader(file)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("cannot open %s: %v", filepath.Base(file), err)
		}
		var files []*sourceFile
		var skipped []string
		for _, entry := range archive.File {
			if entry.FileInfo().IsDir() {
				continue
			}
			if !strings.EqualFold(path.Ext(entry.Name), ".vpk") {
				if riskyFile.MatchString(entry.Name) {
					skipped = append(skipped, entry.Name)
				}
				continue
			}
			entry := entry
			files = append(files, &sourceFile{entry.Name, entry.Open})
		}
		sources, err := groupSources(files)
		return sources, skipped, func() { archive.Close() }, err
	}
	if !strings.EqualFold(filepath.Ext(file), ".vpk") {
		return nil, nil, nil, errors.New("dlmod installs .zip and .vpk files; extract other archives " +
			"(.rar, .7z) first and install the .vpk inside")
	}
	if !isFile(file) {
		return nil, nil, nil, fmt.Errorf("%s was not found", file)
	}
	folder := filepath.Dir(file)
	base := filepath.Base(file)
	prefix := ""
	if strings.HasSuffix(strings.ToLower(base), "_dir.vpk") {
		prefix = base[:len(base)-len("_dir.vpk")]
	} else if match := archivePart.FindStringSubmatch(base); match != nil {
		prefix = match[1]
	}
	open := func(name string) *sourceFile {
		full := filepath.Join(folder, name)
		return &sourceFile{name, func() (io.ReadCloser, error) { return os.Open(full) }}
	}
	files := []*sourceFile{open(base)}
	if prefix != "" {
		listing, err := os.ReadDir(folder)
		if err != nil {
			return nil, nil, nil, err
		}
		for _, item := range listing {
			name := item.Name()
			if name == base || item.IsDir() || !strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)+"_") {
				continue
			}
			if match := archivePart.FindStringSubmatch(name); (match != nil && strings.EqualFold(match[1], prefix)) ||
				strings.EqualFold(name, prefix+"_dir.vpk") {
				files = append(files, open(name))
			}
		}
	}
	sources, err := groupSources(files)
	return sources, nil, func() {}, err
}

func groupSources(files []*sourceFile) ([]*source, error) {
	groups := map[string]*source{}
	var order []string
	get := func(key, label string) *source {
		if groups[key] == nil {
			groups[key] = &source{label: label, parts: map[string]*sourceFile{}}
			order = append(order, key)
		}
		return groups[key]
	}
	for _, file := range files {
		name := strings.ReplaceAll(file.name, `\`, "/")
		folder, base := path.Split(name)
		lower := strings.ToLower(base)
		switch match := archivePart.FindStringSubmatch(base); {
		case strings.HasSuffix(lower, "_dir.vpk"):
			get(strings.ToLower(folder)+lower[:len(lower)-len("_dir.vpk")], name).dir = file
		case match != nil:
			get(strings.ToLower(folder+match[1]), name).parts[match[2]] = file
		default:
			get(strings.ToLower(name), name).dir = file
		}
	}
	var result []*source
	for _, key := range order {
		group := groups[key]
		if group.dir == nil {
			return nil, fmt.Errorf("%s has no matching _dir.vpk file", group.label)
		}
		group.label = strings.ReplaceAll(group.dir.name, `\`, "/")
		result = append(result, group)
	}
	return result, nil
}

func copyTo(file *sourceFile, target string) (string, error) {
	reader, err := file.open()
	if err != nil {
		return "", err
	}
	defer reader.Close()
	writer, err := os.Create(target)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, err = io.Copy(io.MultiWriter(writer, hash), reader)
	if closeErr := writer.Close(); err == nil {
		err = closeErr
	}
	return hex.EncodeToString(hash.Sum(nil)), err
}

func modName(file string) string {
	return strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
}

// install copies the mods in file into free slots. With several mods in one archive (often colour
// variants of one mod) it asks which to install when it can ask; otherwise it installs all of them.
func (s *Store) install(ui *UI, file, name string) error {
	sources, skipped, closeSources, err := readSources(file)
	if err != nil {
		return err
	}
	defer closeSources()
	for _, risky := range skipped {
		ui.say("Skipped %s: mods never need program files.", risky)
	}
	if len(sources) == 0 {
		return fmt.Errorf("%s contains no .vpk file", filepath.Base(file))
	}
	chosen := sources
	if len(sources) > 1 && ui.interactive {
		ui.say("%s contains %d mods, often colour or style variants of the same mod:", filepath.Base(file), len(sources))
		for i, src := range sources {
			ui.say("  %d  %s", i+1, src.label)
		}
		answer := ui.ask("Install which one? Enter a number, or \"all\": ")
		if !strings.EqualFold(answer, "all") {
			index, err := strconv.Atoi(answer)
			if err != nil || index < 1 || index > len(sources) {
				return errors.New("nothing installed")
			}
			chosen = sources[index-1 : index]
		}
	}
	if name == "" {
		name = modName(file)
	}
	for _, src := range chosen {
		label := name
		if len(sources) > 1 {
			label = fmt.Sprintf("%s (%s)", name, src.label)
		}
		if err := s.installOne(ui, src, label, filepath.Base(file)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) installOne(ui *UI, src *source, name, origin string) error {
	if err := os.MkdirAll(s.game.StateDir(), 0o755); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(s.game.StateDir(), "install-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)

	dirFile := filepath.Join(staging, "dir.vpk")
	sum, err := copyTo(src.dir, dirFile)
	if err != nil {
		return err
	}
	paths, err := vpkFiles(dirFile)
	if err != nil {
		return fmt.Errorf("%s: %v", src.label, err)
	}
	parts := make([]string, 0, len(src.parts))
	for part := range src.parts {
		parts = append(parts, part)
	}
	sort.Strings(parts)
	for _, part := range parts {
		if _, err := copyTo(src.parts[part], filepath.Join(staging, part+".vpk")); err != nil {
			return err
		}
	}

	entries, err := s.entries()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Mod != nil && entry.Mod.SHA256 == sum {
			if entry.Enabled() {
				ui.say("'%s' is already installed as pak%02d.", entry.Name(), entry.Slot)
			} else {
				ui.say("'%s' is already installed but switched off; switch it on instead.", entry.Name())
			}
			return nil
		}
	}

	slot := 0
	var clashes []Conflict
	for _, entry := range entries {
		if !entry.Enabled() {
			continue
		}
		shared := overlap(entry.Paths, paths)
		if len(shared) == 0 {
			continue
		}
		sameFiles := len(shared) == len(paths) && len(shared) == len(entry.Paths)
		if slot == 0 && sameFiles && ui.confirm(fmt.Sprintf(
			"'%s' (pak%02d) changes exactly the same files. Is this a newer version? Replace it?", entry.Name(), entry.Slot), true) {
			if err := s.remove(entry); err != nil {
				return err
			}
			slot = entry.Slot
			ui.say("Removed '%s'.", entry.Name())
			continue
		}
		clashes = append(clashes, Conflict{entry, nil, shared})
	}
	if slot == 0 {
		if slot, err = s.freeSlot(); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(s.game.Addons(), 0o755); err != nil {
		return err
	}
	target := func(part string) string {
		return filepath.Join(s.game.Addons(), fmt.Sprintf("pak%02d_%s.vpk", slot, part))
	}
	if err := os.Rename(dirFile, target("dir")); err != nil {
		return err
	}
	for _, part := range parts {
		if err := os.Rename(filepath.Join(staging, part+".vpk"), target(part)); err != nil {
			return err
		}
	}
	s.manifest.Mods = append(s.manifest.Mods, &Mod{
		ID: s.manifest.NextID, Name: name, Slot: slot, Enabled: true, Source: origin, SHA256: sum,
		Installed: time.Now().UTC().Format(time.RFC3339), GameVersion: s.game.Version(),
	})
	s.manifest.NextID++
	if err := s.save(); err != nil {
		return err
	}
	ui.say("Installed '%s' as pak%02d_dir.vpk%s.", name, slot, plural(len(parts), " with %d archive file", " with %d archive files"))
	for _, clash := range clashes {
		ui.say("Conflict: '%s' (pak%02d) also changes %s. Only one of the two can take effect; "+
			"switch one off if something looks wrong.", clash.A.Name(), clash.A.Slot, describePaths(clash.Paths))
	}
	return nil
}

func plural(count int, one, many string) string {
	switch count {
	case 0:
		return ""
	case 1:
		return fmt.Sprintf(one, count)
	default:
		return fmt.Sprintf(many, count)
	}
}

func describePaths(paths []string) string {
	shown := slices.Clone(paths)
	sort.Strings(shown)
	if len(shown) > 2 {
		return fmt.Sprintf("%s, %s and %s", shown[0], shown[1], plural(len(shown)-2, "%d more file", "%d more files"))
	}
	return strings.Join(shown, " and ")
}

// --- removing and switching ---

func (s *Store) forget(mod *Mod) {
	s.manifest.Mods = slices.DeleteFunc(s.manifest.Mods, func(m *Mod) bool { return m == mod })
}

func (s *Store) remove(entry *Entry) error {
	for _, file := range entry.Files {
		if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if entry.Mod != nil {
		if !entry.Mod.Enabled {
			os.RemoveAll(s.disabledDir(entry.Mod))
		}
		s.forget(entry.Mod)
	}
	return s.save()
}

// disable moves a mod out of citadel/addons into dlmod's folder, where the game does not look.
func (s *Store) disable(entry *Entry) error {
	if !entry.Enabled() {
		return fmt.Errorf("'%s' is already off", entry.Name())
	}
	if entry.Mod == nil {
		entry.Mod = &Mod{ID: s.manifest.NextID, Name: entry.Name(), Slot: entry.Slot, Enabled: true}
		s.manifest.NextID++
		s.manifest.Mods = append(s.manifest.Mods, entry.Mod)
	}
	folder := s.disabledDir(entry.Mod)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return err
	}
	for _, file := range entry.Files {
		if err := os.Rename(file, filepath.Join(folder, filepath.Base(file))); err != nil {
			return err
		}
	}
	entry.Mod.Enabled = false
	return s.save()
}

// enable moves a mod back, into its old slot when that is still free.
func (s *Store) enable(entry *Entry) error {
	if entry.Enabled() {
		return fmt.Errorf("'%s' is already on", entry.Name())
	}
	used, err := s.slots()
	if err != nil {
		return err
	}
	slot := entry.Mod.Slot
	if slot < 1 || slot > maxSlot || len(used[slot]) > 0 {
		if slot, err = s.freeSlot(); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(s.game.Addons(), 0o755); err != nil {
		return err
	}
	for _, file := range entry.Files {
		match := addonFile.FindStringSubmatch(filepath.Base(file))
		if match == nil {
			continue
		}
		renamed := fmt.Sprintf("pak%02d_%s.vpk", slot, strings.ToLower(match[2]))
		if err := os.Rename(file, filepath.Join(s.game.Addons(), renamed)); err != nil {
			return err
		}
	}
	os.Remove(s.disabledDir(entry.Mod))
	entry.Mod.Slot, entry.Mod.Enabled = slot, true
	return s.save()
}

// resolve finds a mod by its number in the list, its slot (pak03) or part of its name.
func resolve(entries []*Entry, text string) (*Entry, error) {
	query := strings.ToLower(strings.TrimSpace(text))
	if number, err := strconv.Atoi(strings.TrimPrefix(query, "#")); err == nil {
		if number >= 1 && number <= len(entries) {
			return entries[number-1], nil
		}
		return nil, fmt.Errorf("there is no mod number %d", number)
	}
	if match := slotName.FindStringSubmatch(query); match != nil {
		slot, _ := strconv.Atoi(match[1])
		for _, entry := range entries {
			if entry.Enabled() && entry.Slot == slot {
				return entry, nil
			}
		}
		return nil, fmt.Errorf("no mod is installed as pak%02d", slot)
	}
	var found []*Entry
	for _, entry := range entries {
		if query != "" && strings.Contains(strings.ToLower(entry.Name()), query) {
			found = append(found, entry)
		}
	}
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		return nil, fmt.Errorf("no mod matches %q", text)
	default:
		return nil, fmt.Errorf("%q matches %d mods; use the number from the list", text, len(found))
	}
}
