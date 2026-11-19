package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// dlmod only goes online for "dlmod update": it downloads the Counter Intelligence HUD built for
// the installed game build from the project's GitHub releases. Everything else works offline.
//
// Downloads go through the system's curl, which Windows 10 and later include and Linux systems
// have, rather than Go's HTTP and TLS stack, which would double the size of dlmod. curl also
// brings proxy settings, redirects and the system's certificate store.

// releasesURL lists the project's releases, newest first; tests point it at a local server.
var releasesURL = "https://api.github.com/repos/MrHakan/deadlock-counter-intelligence/releases?per_page=20"

var hudAssetName = regexp.MustCompile(`^deadlock-counter-intelligence-(\d+)-hud-en\.zip$`)

type releaseAsset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"` // "sha256:<hex>" on GitHub releases
}

type release struct {
	Tag        string         `json:"tag_name"`
	Draft      bool           `json:"draft"`
	Prerelease bool           `json:"prerelease"`
	Assets     []releaseAsset `json:"assets"`
}

// allowedURL accepts HTTPS addresses, and plain HTTP only for this computer (tests).
func allowedURL(address string) error {
	parsed, err := url.Parse(address)
	if err != nil {
		return err
	}
	host := parsed.Hostname()
	if parsed.Scheme == "https" || (parsed.Scheme == "http" && (host == "127.0.0.1" || host == "localhost")) {
		return nil
	}
	return fmt.Errorf("refusing to download from %s", address)
}

// fetch streams an address into w through curl.
func fetch(address string, w io.Writer) error {
	if err := allowedURL(address); err != nil {
		return err
	}
	command := exec.Command("curl", "--fail", "--silent", "--show-error", "--location", "--proto-redir", "=https",
		"--max-time", "300", "--user-agent", "dlmod/"+version, "--header", "Accept: application/vnd.github+json", address)
	var problems bytes.Buffer
	command.Stdout, command.Stderr = w, &problems
	if err := command.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return errors.New("curl was not found; it comes with Windows 10 and later. Download the HUD from the " +
				"project's GitHub releases instead and drag it onto dlmod")
		}
		return fmt.Errorf("download failed: %s", strings.TrimSpace(problems.String()))
	}
	return nil
}

// hudRelease finds the newest published release with a HUD for the game build, and otherwise
// reports the newest build any release covers.
func hudRelease(releases []release, build int) (release, releaseAsset, int, string) {
	newest, newestTag := 0, ""
	for _, candidate := range releases {
		if candidate.Draft || candidate.Prerelease {
			continue
		}
		for _, asset := range candidate.Assets {
			match := hudAssetName.FindStringSubmatch(asset.Name)
			if match == nil {
				continue
			}
			assetBuild, _ := strconv.Atoi(match[1])
			if assetBuild == build {
				return candidate, asset, assetBuild, candidate.Tag
			}
			if assetBuild > newest {
				newest, newestTag = assetBuild, candidate.Tag
			}
		}
	}
	return release{}, releaseAsset{}, newest, newestTag
}

// downloadFile writes a file to disk and returns its SHA-256.
func downloadFile(address, target string, total int64, label string, p prompter) (string, error) {
	file, err := os.Create(target)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	sinks := []io.Writer{file, hash}
	if total > 0 {
		sinks = append(sinks, &progressWriter{label: label, total: total, report: p})
	}
	err = fetch(address, io.MultiWriter(sinks...))
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return hex.EncodeToString(hash.Sum(nil)), err
}

// updateHUD installs the HUD release for the installed game build, replacing an older version
// in its slot. Installing the same version again changes nothing.
func (s *Store) updateHUD(p prompter) error {
	build := s.game.Version()
	if build == 0 {
		return errors.New("the game build is unknown because game/citadel/steam.inf is missing")
	}
	var listing bytes.Buffer
	if err := fetch(releasesURL, &listing); err != nil {
		return err
	}
	var releases []release
	if err := json.Unmarshal(listing.Bytes(), &releases); err != nil {
		return fmt.Errorf("unexpected answer from GitHub: %v", err)
	}
	found, asset, newest, newestTag := hudRelease(releases, build)
	if asset.Name == "" {
		if newest > 0 {
			return fmt.Errorf("there is no HUD for game build %d yet; the newest release (%s) is for build %d. "+
				"A new one usually follows a game update within a day", build, newestTag, newest)
		}
		return fmt.Errorf("no release has a HUD for game build %d", build)
	}
	folder := filepath.Join(s.game.StateDir(), "downloads")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return err
	}
	target := filepath.Join(folder, asset.Name)
	defer os.Remove(target)
	sum, err := downloadFile(asset.URL, target, asset.Size, asset.Name, p)
	if err != nil {
		return err
	}
	if want := strings.TrimPrefix(asset.Digest, "sha256:"); want != "" && !strings.EqualFold(want, sum) {
		return errors.New("the download does not match the checksum GitHub published for it; nothing was installed")
	}
	p.say("Downloaded the HUD for build %d from release %s.", build, found.Tag)
	return s.install(p, target, "Deadlock Counter Intelligence "+found.Tag)
}
