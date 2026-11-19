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

// prompter is how installing and switching mods talks to the person: the plain console
// interface and the full-screen one both implement it.
type prompter interface {
	say(format string, args ...any)
	confirm(question string, fallback bool) bool
	// choose returns the chosen option, or all=true for every option, or ok=false to cancel.
	choose(question string, options []string) (index int, all, ok bool)
	progress(label string, done, total int64)
}

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
	NextID   int              `json:"nextId"`
	Mods     []*Mod           `json:"mods"`
	Profiles map[string][]int `json:"profiles,omitempty"` // profile name -> IDs of the mods it switches on
}

// Entry is one mod: a filled slot in citadel/addons, or a switched-off mod in dlmod's folder.
type Entry struct {
	Mod   *Mod // nil when the slot was filled by hand or by another tool
	Slot  int
	Files []string // full paths, pakNN_dir.vpk first
	Paths []string // game files the mod changes
	Size  int64    // bytes on disk, all parts together
	Err   error    // the VPK could not be read
}

func (e *Entry) Enabled() bool { return e.Mod == nil || e.Mod.Enabled }

func (e *Entry) Name() string {
	if e.Mod != nil {
		return e.Mod.Name
	}
	return fmt.Sprintf("pak%02d (added outside dlmod)", e.Slot)
}

func isDirFile(file string) bool { return strings.HasSuffix(strings.ToLower(file), "_dir.vpk") }

// parsed remembers VPK directories between refreshes. A file is read again only when its size
// or modification time changes, so redrawing the screen costs a few stat calls.
type parsedVPK struct {
	size    int64
	modTime time.Time
	paths   []string
	err     error
}

var vpkCache = map[string]parsedVPK{}

func (e *Entry) load() {
	e.Size = 0
	var dirInfo os.FileInfo
	for i, file := range e.Files {
		if info, err := os.Stat(file); err == nil {
			e.Size += info.Size()
			if i == 0 {
				dirInfo = info
			}
		}
	}
	if len(e.Files) == 0 || !isDirFile(e.Files[0]) || dirInfo == nil {
		e.Err = errors.New("its pakNN_dir.vpk file is missing")
		return
	}
	if cached, ok := vpkCache[e.Files[0]]; ok && cached.size == dirInfo.Size() && cached.modTime.Equal(dirInfo.ModTime()) {
		e.Paths, e.Err = cached.paths, cached.err
		return
	}
	e.Paths, e.Err = vpkFiles(e.Files[0])
	vpkCache[e.Files[0]] = parsedVPK{dirInfo.Size(), dirInfo.ModTime(), e.Paths, e.Err}
}

// moveFile renames a mod file and carries its parsed directory along (renames keep the timestamp).
func moveFile(from, to string) error {
	if err := os.Rename(from, to); err != nil {
		return err
	}
	if cached, ok := vpkCache[from]; ok {
		delete(vpkCache, from)
		vpkCache[to] = cached
	}
	return nil
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
	known := map[int]bool{}
	for _, mod := range kept {
		known[mod.ID] = true
	}
	for name, ids := range s.manifest.Profiles {
		s.manifest.Profiles[name] = slices.DeleteFunc(ids, func(id int) bool { return !known[id] })
	}
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
		if isDirFile(files[i]) != isDirFile(files[j]) {
			return isDirFile(files[i])
		}
		return strings.ToLower(files[i]) < strings.ToLower(files[j])
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

// entries lists every mod by slot; switched-off mods keep the slot they had.
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
	list := make([]*Entry, 0, len(numbers)+len(s.manifest.Mods))
	for _, slot := range numbers {
		list = append(list, &Entry{Mod: bySlot[slot], Slot: slot, Files: used[slot]})
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
		list = append(list, entry)
	}
	// A stable order by slot: switching a mod off must not move it in the list.
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Slot != list[j].Slot {
			return list[i].Slot < list[j].Slot
		}
		return list[i].Enabled() && !list[j].Enabled()
	})
	seen := make(map[string]bool, len(list))
	for _, entry := range list {
		entry.load()
		if len(entry.Files) > 0 {
			seen[entry.Files[0]] = true
		}
	}
	for file := range vpkCache {
		if !seen[file] {
			delete(vpkCache, file)
		}
	}
	return list, nil
}

// Conflict is a pair of switched-on mods that change the same game files.
type Conflict struct {
	A, B  *Entry
	Paths []string
}

func overlap(a, b []string) []string {
	seen := make(map[string]bool, len(a))
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

// conflicts indexes every changed file once, so the cost grows with the number of files rather
// than with the number of mod pairs.
func conflicts(entries []*Entry) []Conflict {
	total := 0
	for _, entry := range entries {
		total += len(entry.Paths)
	}
	// Most files belong to one mod: they cost a map slot and nothing else. Only files that a
	// second mod also changes get a list of further owners.
	first := make(map[string]int, total)
	more := map[string][]int{}
	shared := map[[2]int][]string{}
	for i, entry := range entries {
		if !entry.Enabled() {
			continue
		}
		for _, path := range entry.Paths {
			owner, seen := first[path]
			if !seen {
				first[path] = i
				continue
			}
			others := more[path]
			if owner == i || (len(others) > 0 && others[len(others)-1] == i) {
				continue // listed twice in the same mod
			}
			pair := [2]int{owner, i}
			shared[pair] = append(shared[pair], path)
			for _, earlier := range others {
				pair = [2]int{earlier, i}
				shared[pair] = append(shared[pair], path)
			}
			more[path] = append(others, i)
		}
	}
	result := make([]Conflict, 0, len(shared))
	for pair, paths := range shared {
		sort.Strings(paths)
		result = append(result, Conflict{entries[pair[0]], entries[pair[1]], paths})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].A.Slot != result[j].A.Slot {
			return result[i].A.Slot < result[j].A.Slot
		}
		return result[i].B.Slot < result[j].B.Slot
	})
	return result
}

// --- installing ---

type sourceFile struct {
	name string
	open func() (io.ReadCloser, error)
	size int64
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
			files = append(files, &sourceFile{entry.Name, entry.Open, int64(entry.UncompressedSize64)})
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
	if isDirFile(base) {
		prefix = base[:len(base)-len("_dir.vpk")]
	} else if match := archivePart.FindStringSubmatch(base); match != nil {
		prefix = match[1]
	}
	open := func(name string, size int64) *sourceFile {
		full := filepath.Join(folder, name)
		return &sourceFile{name, func() (io.ReadCloser, error) { return os.Open(full) }, size}
	}
	info, _ := os.Stat(file)
	files := []*sourceFile{open(base, info.Size())}
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
				size := int64(0)
				if info, err := item.Info(); err == nil {
					size = info.Size()
				}
				files = append(files, open(name, size))
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

// progressWriter reports copied bytes at most ten times a second.
type progressWriter struct {
	label       string
	done, total int64
	last        time.Time
	report      prompter
}

func (w *progressWriter) Write(data []byte) (int, error) {
	w.done += int64(len(data))
	if now := time.Now(); w.done >= w.total || now.Sub(w.last) >= 100*time.Millisecond {
		w.last = now
		w.report.progress(w.label, w.done, w.total)
	}
	return len(data), nil
}

// copyTo streams a file through one buffer, so memory use does not grow with the mod's size.
func copyTo(file *sourceFile, target string, progress io.Writer, buffer []byte) (string, error) {
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
	sinks := []io.Writer{writer, hash}
	if progress != nil {
		sinks = append(sinks, progress)
	}
	_, err = io.CopyBuffer(io.MultiWriter(sinks...), reader, buffer)
	if closeErr := writer.Close(); err == nil {
		err = closeErr
	}
	return hex.EncodeToString(hash.Sum(nil)), err
}

func modName(file string) string {
	return strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
}

// install copies the mods in file into free slots. When an archive holds several mods (often
// colour variants of one mod) it asks which to install; without a console it installs all.
func (s *Store) install(p prompter, file, name string) error {
	sources, skipped, closeSources, err := readSources(file)
	if err != nil {
		return err
	}
	defer closeSources()
	for _, risky := range skipped {
		p.say("Skipped %s: mods never need program files.", risky)
	}
	if len(sources) == 0 {
		return fmt.Errorf("%s contains no .vpk file", filepath.Base(file))
	}
	chosen := sources
	if len(sources) > 1 {
		labels := make([]string, len(sources))
		for i, src := range sources {
			labels[i] = src.label
		}
		index, all, ok := p.choose(fmt.Sprintf("%s contains %d mods, often colour or style variants of the same mod. "+
			"Install which one?", filepath.Base(file), len(sources)), labels)
		if !ok {
			return errors.New("nothing installed")
		}
		if !all {
			chosen = sources[index : index+1]
		}
	}
	if name == "" {
		name = modName(file)
	}
	buffer := make([]byte, 1<<20)
	for _, src := range chosen {
		label := name
		if len(sources) > 1 {
			label = fmt.Sprintf("%s (%s)", name, src.label)
		}
		if err := s.installOne(p, src, label, filepath.Base(file), buffer); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) installOne(p prompter, src *source, name, origin string, buffer []byte) error {
	if err := os.MkdirAll(s.game.StateDir(), 0o755); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(s.game.StateDir(), "install-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)

	parts := make([]string, 0, len(src.parts))
	total := src.dir.size
	for part, file := range src.parts {
		parts = append(parts, part)
		total += file.size
	}
	sort.Strings(parts)
	var progress io.Writer
	if total > 0 {
		progress = &progressWriter{label: name, total: total, report: p}
	}
	dirFile := filepath.Join(staging, "dir.vpk")
	sum, err := copyTo(src.dir, dirFile, progress, buffer)
	if err != nil {
		return err
	}
	paths, err := vpkFiles(dirFile)
	if err != nil {
		return fmt.Errorf("%s: %v", src.label, err)
	}
	for _, part := range parts {
		if _, err := copyTo(src.parts[part], filepath.Join(staging, part+".vpk"), progress, buffer); err != nil {
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
				p.say("'%s' is already installed as pak%02d.", entry.Name(), entry.Slot)
			} else {
				p.say("'%s' is already installed but switched off; switch it on instead.", entry.Name())
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
		if slot == 0 && sameFiles && p.confirm(fmt.Sprintf(
			"'%s' (pak%02d) changes exactly the same files. Is this a newer version? Replace it?", entry.Name(), entry.Slot), true) {
			if err := s.remove(entry); err != nil {
				return err
			}
			slot = entry.Slot
			p.say("Removed '%s'.", entry.Name())
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
	p.say("Installed '%s' as pak%02d_dir.vpk%s.", name, slot, plural(len(parts), " with %d archive file", " with %d archive files"))
	for _, clash := range clashes {
		p.say("Conflict: '%s' (pak%02d) also changes %s. Only one of the two can take effect; "+
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
		delete(vpkCache, file)
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
		if err := moveFile(file, filepath.Join(folder, filepath.Base(file))); err != nil {
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
		if err := moveFile(file, filepath.Join(s.game.Addons(), renamed)); err != nil {
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
