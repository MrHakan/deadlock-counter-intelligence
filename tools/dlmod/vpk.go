package main

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const vpkSignature = 0x55AA1234

var errNotVPK = errors.New("not a VPK file")

// vpkFiles lists the game files inside a VPK directory file (pakNN_dir.vpk or a single-file VPK).
func vpkFiles(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readVPKTree(file)
}

func readVPKTree(r io.Reader) ([]string, error) {
	var header [3]uint32 // signature, version, tree size
	if err := binary.Read(r, binary.LittleEndian, &header); err != nil || header[0] != vpkSignature {
		return nil, errNotVPK
	}
	switch header[1] {
	case 1:
	case 2:
		if _, err := io.CopyN(io.Discard, r, 16); err != nil {
			return nil, errNotVPK
		}
	default:
		return nil, fmt.Errorf("unsupported VPK version %d", header[1])
	}

	tree := bufio.NewReader(io.LimitReader(r, int64(header[2])))
	truncated := errors.New("the VPK directory is damaged")
	next := func() (string, error) {
		text, err := tree.ReadString(0)
		if err != nil {
			return "", truncated
		}
		return text[:len(text)-1], nil
	}
	var files []string
	for {
		extension, err := next()
		if err != nil {
			return nil, err
		}
		if extension == "" {
			return files, nil
		}
		for {
			directory, err := next()
			if err != nil {
				return nil, err
			}
			if directory == "" {
				break
			}
			for {
				name, err := next()
				if err != nil {
					return nil, err
				}
				if name == "" {
					break
				}
				var entry struct {
					CRC        uint32
					Preload    uint16
					Archive    uint16
					Offset     uint32
					Length     uint32
					Terminator uint16
				}
				if err := binary.Read(tree, binary.LittleEndian, &entry); err != nil || entry.Terminator != 0xFFFF {
					return nil, truncated
				}
				if _, err := tree.Discard(int(entry.Preload)); err != nil {
					return nil, truncated
				}
				files = append(files, vpkPath(directory, name, extension))
			}
		}
	}
}

func vpkPath(directory, name, extension string) string {
	path := name
	if extension != " " {
		path += "." + extension
	}
	if directory != " " {
		path = directory + "/" + path
	}
	return strings.ToLower(path)
}
