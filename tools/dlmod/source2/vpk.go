package source2

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const (
	vpkSignature  = 0x55AA1234
	inlineArchive = 0x7FFF // the file's data follows the tree inside the _dir.vpk itself
)

// VPKEntry is where one file's bytes are.
type VPKEntry struct {
	Archive uint16 // pakNN_ARCHIVE.vpk, or inline
	Offset  uint32
	Length  uint32
	Preload []byte // bytes stored in the tree before the rest
}

// VPKIndex lists the files of a VPK and reads them.
type VPKIndex struct {
	dir     string // the _dir.vpk file
	dataEnd int64  // where the tree ends inside the _dir.vpk, for inline data
	Files   map[string]VPKEntry
}

// OpenVPK reads the tree of a _dir.vpk, keeping each file's location.
func OpenVPK(dirFile string) (*VPKIndex, error) { return OpenVPKFiltered(dirFile, nil) }

// OpenVPKFiltered keeps only the files keep accepts, which saves memory on the game's own archive
// (about 160,000 files).
func OpenVPKFiltered(dirFile string, keep func(name string) bool) (*VPKIndex, error) {
	file, err := os.Open(dirFile)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	header := make([]byte, 28)
	if _, err := io.ReadFull(file, header[:12]); err != nil || binary.LittleEndian.Uint32(header) != vpkSignature {
		return nil, errors.New("not a VPK file")
	}
	headerSize := int64(12)
	if version := binary.LittleEndian.Uint32(header[4:]); version == 2 {
		if _, err := io.ReadFull(file, header[12:]); err != nil {
			return nil, err
		}
		headerSize = 28
	} else if version != 1 {
		return nil, fmt.Errorf("unsupported VPK version %d", version)
	}
	tree := make([]byte, binary.LittleEndian.Uint32(header[8:]))
	if _, err := io.ReadFull(file, tree); err != nil {
		return nil, err
	}
	index := &VPKIndex{dir: dirFile, dataEnd: headerSize + int64(len(tree)), Files: map[string]VPKEntry{}}
	read := func() (string, error) {
		end := bytes.IndexByte(tree, 0)
		if end < 0 {
			return "", errors.New("damaged VPK tree")
		}
		text := string(tree[:end])
		tree = tree[end+1:]
		return text, nil
	}
	for {
		extension, err := read()
		if err != nil || extension == "" {
			return index, err
		}
		for {
			directory, err := read()
			if err != nil {
				return nil, err
			}
			if directory == "" {
				break
			}
			for {
				name, err := read()
				if err != nil {
					return nil, err
				}
				if name == "" {
					break
				}
				if len(tree) < 18 {
					return nil, errors.New("damaged VPK tree")
				}
				preload := int(binary.LittleEndian.Uint16(tree[4:]))
				entry := VPKEntry{
					Archive: binary.LittleEndian.Uint16(tree[6:]),
					Offset:  binary.LittleEndian.Uint32(tree[8:]),
					Length:  binary.LittleEndian.Uint32(tree[12:]),
				}
				tree = tree[18:]
				if preload > len(tree) {
					return nil, errors.New("damaged VPK tree")
				}
				entry.Preload, tree = tree[:preload], tree[preload:]
				full := name + "." + extension
				if directory != " " {
					full = directory + "/" + full
				}
				if keep == nil || keep(full) {
					index.Files[full] = entry
				}
			}
		}
	}
}

// Read returns a file's bytes from the VPK and its numbered archives.
func (v *VPKIndex) Read(name string) ([]byte, error) {
	entry, ok := v.Files[name]
	if !ok {
		return nil, fmt.Errorf("%s is not in %s", name, filepath.Base(v.dir))
	}
	data := append([]byte(nil), entry.Preload...)
	if entry.Length == 0 {
		return data, nil
	}
	archive := v.dir
	offset := int64(entry.Offset)
	if entry.Archive == inlineArchive {
		offset += v.dataEnd
	} else {
		archive = strings.TrimSuffix(v.dir, "_dir.vpk") + fmt.Sprintf("_%03d.vpk", entry.Archive)
	}
	file, err := os.Open(archive)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	rest := make([]byte, entry.Length)
	if _, err := file.ReadAt(rest, offset); err != nil {
		return nil, fmt.Errorf("%s: %v", name, err)
	}
	return append(data, rest...), nil
}

// ReadHead returns up to the first n bytes of a file, for its headers: a hero's background
// image is 37 MB, its size is in the first kilobyte.
func (v *VPKIndex) ReadHead(name string, n int) ([]byte, error) {
	entry, ok := v.Files[name]
	if !ok {
		return nil, fmt.Errorf("%s is not in %s", name, filepath.Base(v.dir))
	}
	data := append([]byte(nil), entry.Preload[:min(len(entry.Preload), n)]...)
	if entry.Length == 0 || len(data) >= n {
		return data, nil
	}
	archive := v.dir
	offset := int64(entry.Offset)
	if entry.Archive == inlineArchive {
		offset += v.dataEnd
	} else {
		archive = strings.TrimSuffix(v.dir, "_dir.vpk") + fmt.Sprintf("_%03d.vpk", entry.Archive)
	}
	file, err := os.Open(archive)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	rest := make([]byte, min(int(entry.Length), n-len(data)))
	if _, err := file.ReadAt(rest, offset); err != nil {
		return nil, fmt.Errorf("%s: %v", name, err)
	}
	return append(data, rest...), nil
}

// WriteVPK packs files into one VPK version 2 with the data inline, like tools/build.py does.
func WriteVPK(files map[string][]byte) []byte {
	type item struct{ extension, directory, stem, name string }
	var items []item
	for name := range files {
		clean := path.Clean(strings.ReplaceAll(name, "\\", "/"))
		extension := strings.TrimPrefix(path.Ext(clean), ".")
		directory := path.Dir(clean)
		if directory == "." {
			directory = " "
		}
		items = append(items, item{extension, directory, strings.TrimSuffix(path.Base(clean), "."+extension), name})
	}
	sort.Slice(items, func(a, b int) bool {
		x, y := items[a], items[b]
		if x.extension != y.extension {
			return x.extension < y.extension
		}
		if x.directory != y.directory {
			return x.directory < y.directory
		}
		return x.stem < y.stem
	})
	var tree, data []byte
	for i := 0; i < len(items); {
		extension := items[i].extension
		tree = append(append(tree, extension...), 0)
		for i < len(items) && items[i].extension == extension {
			directory := items[i].directory
			tree = append(append(tree, directory...), 0)
			for i < len(items) && items[i].extension == extension && items[i].directory == directory {
				payload := files[items[i].name]
				tree = append(append(tree, items[i].stem...), 0)
				tree = binary.LittleEndian.AppendUint32(tree, crc32.ChecksumIEEE(payload))
				tree = binary.LittleEndian.AppendUint16(tree, 0)
				tree = binary.LittleEndian.AppendUint16(tree, inlineArchive)
				tree = binary.LittleEndian.AppendUint32(tree, uint32(len(data)))
				tree = binary.LittleEndian.AppendUint32(tree, uint32(len(payload)))
				tree = binary.LittleEndian.AppendUint16(tree, 0xFFFF)
				data = append(data, payload...)
				i++
			}
			tree = append(tree, 0)
		}
		tree = append(tree, 0)
	}
	tree = append(tree, 0)
	out := make([]byte, 28)
	binary.LittleEndian.PutUint32(out, vpkSignature)
	binary.LittleEndian.PutUint32(out[4:], 2)
	binary.LittleEndian.PutUint32(out[8:], uint32(len(tree)))
	binary.LittleEndian.PutUint32(out[12:], uint32(len(data)))
	return append(append(out, tree...), data...)
}

// PlayableSound turns a compiled sound back into a file a media player opens: MP3 frames as they
// are, PCM with the WAV header the compiler removed. It returns the data and its extension.
func PlayableSound(compiled []byte) ([]byte, string, error) {
	resource, err := DecodeResource(compiled)
	if err != nil {
		return nil, "", err
	}
	ctrl, _, err := ReadKV3(resource.Block("CTRL"))
	if err != nil {
		return nil, "", fmt.Errorf("this sound is in an older layout: %v", err)
	}
	sound := ctrl.Get("m_vSound")
	if sound == nil {
		return nil, "", errors.New("this sound file holds no single sound")
	}
	number := func(key string) int {
		v := sound.Get(key)
		if v == nil {
			return 0
		}
		return int(v.I) + int(v.U)
	}
	switch format := sound.Get("m_nFormat"); {
	case format != nil && format.S == "MP3":
		return resource.Streaming, ".mp3", nil
	case format != nil && (format.S == "PCM16" || format.S == "PCM8"):
		bits := 16
		if format.S == "PCM8" {
			bits = 8
		}
		channels, rate := number("m_nChannels"), number("m_nRate")
		var out bytes.Buffer
		out.WriteString("RIFF")
		binary.Write(&out, binary.LittleEndian, uint32(36+len(resource.Streaming)))
		out.WriteString("WAVEfmt ")
		for _, field := range []any{uint32(16), uint16(1), uint16(channels), uint32(rate),
			uint32(rate * channels * bits / 8), uint16(channels * bits / 8), uint16(bits)} {
			binary.Write(&out, binary.LittleEndian, field)
		}
		out.WriteString("data")
		binary.Write(&out, binary.LittleEndian, uint32(len(resource.Streaming)))
		out.Write(resource.Streaming)
		return out.Bytes(), ".wav", nil
	default:
		return nil, "", errors.New("this sound uses a format dlmod cannot play back (ADPCM)")
	}
}
