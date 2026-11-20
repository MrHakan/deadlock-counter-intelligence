package main

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Online mods come from the same places Deadlock Mod Manager uses. Its public API mirrors the
// GameBanana catalog with each mod's hero and category; GameBanana's own API lists a mod's files
// when the mirror cannot.
var (
	catalogURL     = "https://api.deadlockmods.app/api/v2/mods"
	dmmFilesURL    = "https://api.deadlockmods.app/api/v2/mods/%s/download"
	bananaFilesURL = "https://gamebanana.com/apiv11/Mod/%s/DownloadPage"
)

const catalogLifetime = 6 * time.Hour

// RemoteMod is one mod in the online catalog.
type RemoteMod struct {
	ID           string   `json:"remoteId"`
	Name         string   `json:"name"`
	Author       string   `json:"author"`
	Category     string   `json:"category"`
	Hero         string   `json:"hero"`
	Images       []string `json:"images"`
	Downloads    int      `json:"downloadCount"`
	Likes        int      `json:"likes"`
	NSFW         bool     `json:"isNSFW"`
	Downloadable bool     `json:"downloadable"`
	Updated      string   `json:"remoteUpdatedAt"`
	URL          string   `json:"remoteUrl"`
	Description  string   `json:"description"`
}

// RemoteFile is one downloadable file of a mod.
type RemoteFile struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Size int64  `json:"size"`
	MD5  string `json:"md5Checksum"`
}

func (g Game) catalogFile() string { return filepath.Join(g.StateDir(), "catalog", "mods.json") }

// loadCatalog returns the online catalog, downloading it again when the saved copy is older
// than catalogLifetime or refresh is set. Without a connection a saved copy of any age is used.
func loadCatalog(game Game, refresh bool) ([]RemoteMod, time.Time, error) {
	file := game.catalogFile()
	info, statErr := os.Stat(file)
	if refresh || statErr != nil || time.Since(info.ModTime()) > catalogLifetime {
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			return nil, time.Time{}, err
		}
		pending := file + ".download"
		_, err := downloadFile(catalogURL, pending, 0, "", nil)
		if err == nil {
			err = os.Rename(pending, file)
		}
		os.Remove(pending)
		if err != nil && statErr != nil {
			return nil, time.Time{}, fmt.Errorf("the mod list could not be downloaded: %v", err)
		}
		info, _ = os.Stat(file)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, time.Time{}, err
	}
	mods, err := parseCatalog(data)
	return mods, info.ModTime(), err
}

func parseCatalog(data []byte) ([]RemoteMod, error) {
	var mods []RemoteMod
	if err := json.Unmarshal(data, &mods); err != nil {
		return nil, fmt.Errorf("the mod list could not be read: %v", err)
	}
	kept := mods[:0]
	for _, mod := range mods {
		if mod.ID != "" && mod.Downloadable {
			kept = append(kept, mod)
		}
	}
	return kept, nil
}

// filter is what the browser shows: a category, a hero and words that must all appear in the
// mod's name, author, hero or category.
type filter struct {
	category, hero, text string
	nsfw                 bool
}

func fold(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return ' '
	}, text)
}

// search returns the matching mods, most downloaded first.
func search(mods []RemoteMod, f filter) []RemoteMod {
	words := strings.Fields(fold(f.text))
	var found []RemoteMod
	for _, mod := range mods {
		if (mod.NSFW && !f.nsfw) || (f.category != "" && mod.Category != f.category) || (f.hero != "" && mod.Hero != f.hero) {
			continue
		}
		haystack := " " + fold(mod.Name+" "+mod.Author+" "+mod.Hero+" "+mod.Category) + " "
		match := true
		for _, word := range words {
			if !strings.Contains(haystack, word) {
				match = false
				break
			}
		}
		if match {
			found = append(found, mod)
		}
	}
	sort.SliceStable(found, func(a, b int) bool {
		if found[a].Downloads != found[b].Downloads {
			return found[a].Downloads > found[b].Downloads
		}
		return found[a].Likes > found[b].Likes
	})
	return found
}

// facets lists the categories and the heroes that have mods, each sorted by name.
func facets(mods []RemoteMod) (categories, heroes []string) {
	seenCategory, seenHero := map[string]bool{}, map[string]bool{}
	for _, mod := range mods {
		if mod.Category != "" && !seenCategory[mod.Category] {
			seenCategory[mod.Category] = true
			categories = append(categories, mod.Category)
		}
		if mod.Hero != "" && !seenHero[mod.Hero] {
			seenHero[mod.Hero] = true
			heroes = append(heroes, mod.Hero)
		}
	}
	sort.Strings(categories)
	sort.Strings(heroes)
	return categories, heroes
}

var modID = regexp.MustCompile(`^(?:https?://(?:www\.)?gamebanana\.com/mods/)?(\d+)/?$`)

// parseModID accepts a GameBanana mod number or its page address.
func parseModID(text string) (string, bool) {
	match := modID.FindStringSubmatch(strings.TrimSpace(text))
	if match == nil {
		return "", false
	}
	return match[1], true
}

func fetchJSON(address string, value any) error {
	var body bytes.Buffer
	if err := fetch(address, &body); err != nil {
		return err
	}
	return json.Unmarshal(body.Bytes(), value)
}

// modFiles lists a mod's files from the Deadlock Mod Manager API, or from GameBanana when that
// fails.
func modFiles(id string) ([]RemoteFile, error) {
	var files []RemoteFile
	err := fetchJSON(fmt.Sprintf(dmmFilesURL, id), &files)
	if err == nil && len(files) > 0 {
		return files, nil
	}
	var page struct {
		Files []struct {
			Name string `json:"_sFile"`
			URL  string `json:"_sDownloadUrl"`
			Size int64  `json:"_nFilesize"`
			MD5  string `json:"_sMd5Checksum"`
		} `json:"_aFiles"`
	}
	if bananaErr := fetchJSON(fmt.Sprintf(bananaFilesURL, id), &page); bananaErr != nil {
		if err == nil {
			err = bananaErr
		}
		return nil, fmt.Errorf("the files of mod %s could not be listed: %v", id, err)
	}
	for _, file := range page.Files {
		files = append(files, RemoteFile{file.Name, file.URL, file.Size, file.MD5})
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("mod %s has no files to download", id)
	}
	return files, nil
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._ -]+`)

// downloadMod saves one file of a mod and checks its MD5 when the catalog gives one. The caller
// removes the returned file.
func downloadMod(game Game, file RemoteFile, p prompter) (string, error) {
	folder := filepath.Join(game.StateDir(), "downloads")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return "", err
	}
	name := strings.TrimSpace(unsafeName.ReplaceAllString(filepath.Base(file.Name), "_"))
	if name == "" || name == "." || name == ".." {
		name = "mod.zip"
	}
	target := filepath.Join(folder, name)
	out, err := os.Create(target)
	if err != nil {
		return "", err
	}
	sum := md5.New()
	sinks := []io.Writer{out, sum}
	if file.Size > 0 && p != nil {
		sinks = append(sinks, &progressWriter{label: "Downloading " + name, total: file.Size, report: p})
	}
	err = fetch(file.URL, io.MultiWriter(sinks...))
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err == nil && file.MD5 != "" && !strings.EqualFold(hex.EncodeToString(sum.Sum(nil)), file.MD5) {
		err = fmt.Errorf("%s did not match its published MD5 checksum; nothing was installed", name)
	}
	if err != nil {
		os.Remove(target)
		return "", err
	}
	return target, nil
}

// installRemote downloads a mod and installs it like a dropped file. A mod with several files
// asks which one to install.
func (s *Store) installRemote(p prompter, mod RemoteMod) error {
	files, err := modFiles(mod.ID)
	if err != nil {
		return err
	}
	file := files[0]
	if len(files) > 1 {
		labels := make([]string, len(files))
		for i, candidate := range files {
			labels[i] = fmt.Sprintf("%s (%s)", candidate.Name, formatSize(candidate.Size))
		}
		index, all, ok := p.choose(fmt.Sprintf("%s has %d files. Download which one?", mod.Name, len(files)), labels)
		switch {
		case !ok:
			return errors.New("nothing installed")
		case all:
			// The files are usually alternatives (with or without voice lines, colour variants), so
			// "all" and a console without questions take the first, which authors list as the main one.
			p.say("Downloading the first file, %s; choose another in the full-screen view (b).", files[0].Name)
		default:
			file = files[index]
		}
	}
	saved, err := downloadMod(s.game, file, p)
	if err != nil {
		return err
	}
	defer os.Remove(saved)
	name := mod.Name
	if mod.Hero != "" && !strings.Contains(strings.ToLower(name), strings.ToLower(mod.Hero)) {
		name += " (" + mod.Hero + ")"
	}
	return s.install(p, saved, name)
}

func formatCount(n int) string {
	switch {
	case n >= 1_000_000:
		return strconv.FormatFloat(float64(n)/1e6, 'f', 1, 64) + "M"
	case n >= 10_000:
		return strconv.Itoa(n/1000) + "k"
	case n >= 1000:
		return strconv.FormatFloat(float64(n)/1e3, 'f', 1, 64) + "k"
	}
	return strconv.Itoa(n)
}
