package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Many mods on GameBanana ship as .rar or .7z. Go's standard library reads only .zip, so dlmod
// hands those archives to a tool the system already has, the way it uses curl for downloads:
// the tar.exe that comes with Windows 10 and later (libarchive, which reads .rar and .7z), or
// 7-Zip, unrar or bsdtar when installed.

// extractors lists the commands to try, each with arguments that unpack archive into folder.
func extractors(archive, folder string) [][]string {
	var tools [][]string
	if runtime.GOOS == "windows" {
		system := os.Getenv("SystemRoot")
		if system == "" {
			system = `C:\Windows`
		}
		tools = append(tools, []string{filepath.Join(system, "System32", "tar.exe"), "-xf", archive, "-C", folder})
		for _, base := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
			if base != "" {
				tools = append(tools, []string{filepath.Join(base, "7-Zip", "7z.exe"), "x", "-y", "-o" + folder, archive})
			}
		}
	}
	tools = append(tools,
		[]string{"bsdtar", "-xf", archive, "-C", folder},
		[]string{"7z", "x", "-y", "-o" + folder, archive},
		[]string{"7zz", "x", "-y", "-o" + folder, archive},
		[]string{"unrar", "x", "-o+", "-y", archive, folder + string(filepath.Separator)},
	)
	return tools
}

// extractArchive unpacks a .rar or .7z into a new folder under parent and returns the folder.
// The caller removes it.
func extractArchive(archive, parent string) (string, error) {
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", err
	}
	var problems []string
	for i := 0; i < len(extractors(archive, "")); i++ {
		folder, err := os.MkdirTemp(parent, "dlmod-extract-")
		if err != nil {
			return "", err
		}
		tool := extractors(archive, folder)[i]
		command := exec.Command(tool[0], tool[1:]...)
		var output bytes.Buffer
		command.Stdout, command.Stderr = &output, &output
		err = command.Run()
		if err == nil && containsVPK(folder) {
			return folder, nil
		}
		os.RemoveAll(folder)
		if err != nil && !errors.Is(err, exec.ErrNotFound) && !errors.Is(err, fs.ErrNotExist) {
			problems = append(problems, filepath.Base(tool[0])+": "+strings.TrimSpace(lastLine(output.String())))
		}
	}
	hint := "install 7-Zip (7-zip.org), or extract it yourself and drop the .vpk inside onto dlmod"
	if len(problems) > 0 {
		return "", fmt.Errorf("%s could not be extracted (%s); %s", filepath.Base(archive), strings.Join(problems, "; "), hint)
	}
	return "", fmt.Errorf("no tool to extract %s was found; %s", filepath.Base(archive), hint)
}

func lastLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	return lines[len(lines)-1]
}

func containsVPK(folder string) bool {
	found := false
	filepath.WalkDir(folder, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && strings.EqualFold(filepath.Ext(path), ".vpk") {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// folderSources reads the mods in an extracted folder, like the inside of a .zip.
func folderSources(folder string) ([]*source, []string, error) {
	var files []*sourceFile
	var skipped []string
	err := filepath.WalkDir(folder, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		// Links could point outside the folder; real archives contain plain files.
		if entry.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		rel, _ := filepath.Rel(folder, path)
		rel = filepath.ToSlash(rel)
		if !strings.EqualFold(filepath.Ext(path), ".vpk") {
			if riskyFile.MatchString(rel) {
				skipped = append(skipped, rel)
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		full := path
		files = append(files, &sourceFile{rel, func() (io.ReadCloser, error) { return os.Open(full) }, info.Size()})
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	sources, err := groupSources(files)
	return sources, skipped, err
}
