package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	vpkSignature = 0x55AA1234
	maxTreeSize  = 256 << 20 // far above any real mod; guards against damaged headers
	entrySize    = 18        // CRC, preload size, archive index, offset, length, terminator
)

var (
	errNotVPK     = errors.New("not a VPK file")
	errDamagedVPK = errors.New("the VPK directory is damaged")
)

// vpkFiles lists the game files inside a VPK directory file (pakNN_dir.vpk or a single-file VPK).
// It reads only the header and the directory tree, never the file data.
func vpkFiles(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readVPKTree(file)
}

func readVPKTree(r io.Reader) ([]string, error) {
	var header [28]byte
	if _, err := io.ReadFull(r, header[:12]); err != nil || binary.LittleEndian.Uint32(header[:]) != vpkSignature {
		return nil, errNotVPK
	}
	switch version := binary.LittleEndian.Uint32(header[4:]); version {
	case 1:
	case 2:
		if _, err := io.ReadFull(r, header[12:]); err != nil {
			return nil, errNotVPK
		}
	default:
		return nil, fmt.Errorf("unsupported VPK version %d", version)
	}
	size := binary.LittleEndian.Uint32(header[8:])
	if size > maxTreeSize {
		return nil, errDamagedVPK
	}
	tree := make([]byte, size)
	if _, err := io.ReadFull(r, tree); err != nil {
		return nil, errDamagedVPK
	}
	return parseTree(tree)
}

// parseTree walks the extension > directory > file tree. Each path costs one allocation:
// the directory and extension strings are shared by every file below them.
func parseTree(tree []byte) ([]string, error) {
	pos := 0
	next := func() ([]byte, bool) {
		end := bytes.IndexByte(tree[pos:], 0)
		if end < 0 {
			return nil, false
		}
		text := tree[pos : pos+end]
		pos += end + 1
		return text, true
	}
	files := make([]string, 0, len(tree)/(entrySize+24))
	for {
		extension, ok := next()
		if !ok {
			return nil, errDamagedVPK
		}
		if len(extension) == 0 {
			return files, nil
		}
		suffix := ""
		if string(extension) != " " {
			suffix = "." + string(extension)
		}
		for {
			directory, ok := next()
			if !ok {
				return nil, errDamagedVPK
			}
			if len(directory) == 0 {
				break
			}
			prefix := ""
			if string(directory) != " " {
				prefix = string(directory) + "/"
			}
			for {
				name, ok := next()
				if !ok {
					return nil, errDamagedVPK
				}
				if len(name) == 0 {
					break // an empty name ends the directory
				}
				if pos+entrySize > len(tree) {
					return nil, errDamagedVPK
				}
				preload := int(binary.LittleEndian.Uint16(tree[pos+4:]))
				if binary.LittleEndian.Uint16(tree[pos+16:]) != 0xFFFF {
					return nil, errDamagedVPK
				}
				pos += entrySize + preload
				if pos > len(tree) {
					return nil, errDamagedVPK
				}
				files = append(files, strings.ToLower(prefix+string(name)+suffix))
			}
		}
	}
}
