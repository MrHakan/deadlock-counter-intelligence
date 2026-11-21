package source2

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"strings"
)

// Panorama text resources (.vsvg_c, .vcss_c) keep their text in the DATA block after a CRC32
// (IEEE) of it and two zero bytes. The hero name logos on the hero select screen are such SVGs
// (panorama/images/heroes/hero_names/ivy.vsvg_c), and so are the interface's stylesheets.

// PanoramaText returns the text of a panorama text resource.
func PanoramaText(compiled []byte) (string, error) {
	r, err := DecodeResource(compiled)
	if err != nil {
		return "", err
	}
	data := r.Block("DATA")
	if len(data) < 6 {
		return "", errors.New("the file has no panorama text")
	}
	return string(data[6:]), nil
}

// ReplacePanoramaText writes a copy of a panorama text resource with new text, keeping its other
// blocks.
func ReplacePanoramaText(original []byte, text string) ([]byte, error) {
	r, err := DecodeResource(original)
	if err != nil {
		return nil, err
	}
	data := binary.LittleEndian.AppendUint32(nil, crc32.ChecksumIEEE([]byte(text)))
	data = append(data, 0, 0)
	data = append(data, text...)
	found := false
	for i := range r.Blocks {
		if r.Blocks[i].Type == "DATA" {
			r.Blocks[i].Data = data
			found = true
		}
	}
	if !found {
		return nil, errors.New("the file has no DATA block")
	}
	return r.Encode(), nil
}

// HideHeroScene empties a hero's hero-select scene (maps/ui/hero_prefabs/<hero>.vpk, given as its
// files): the hero model and the other props (Ivy's leaves) and the world meshes (her ink
// backdrop, wind wisp and shadow disc) shrink to nothing far below the camera. Lights, the camera
// and the sky stay, so the screen shows only the hero's background picture
// (panorama/images/heroes/backgrounds/<hero>_bg_psd), for example a full illustration of a
// replacement character. It returns how many entities and world objects it hid.
func HideHeroScene(files map[string][]byte) (props, objects int, err error) {
	for name, data := range files {
		switch {
		case strings.HasSuffix(name, ".vents_c"):
			out, n, err := editKV3Data(data, hideProps)
			if err != nil {
				return 0, 0, fmt.Errorf("%s: %v", name, err)
			}
			files[name] = out
			props += n
		case strings.HasSuffix(name, ".vwnod_c"):
			out, n, err := editKV3Data(data, hideSceneObjects)
			if err != nil {
				return 0, 0, fmt.Errorf("%s: %v", name, err)
			}
			files[name] = out
			objects += n
		}
	}
	if props == 0 && objects == 0 {
		return 0, 0, errors.New("the scene has no props or world meshes to hide")
	}
	return props, objects, nil
}

const hiddenDrop = 5000 // units below the scene

func editKV3Data(compiled []byte, edit func(*Value) int) ([]byte, int, error) {
	r, err := DecodeResource(compiled)
	if err != nil {
		return nil, 0, err
	}
	for i, b := range r.Blocks {
		if b.Type != "DATA" {
			continue
		}
		root, format, err := ReadKV3(b.Data)
		if err != nil {
			return nil, 0, err
		}
		n := edit(root)
		data, err := WriteKV3(root, format)
		if err != nil {
			return nil, 0, err
		}
		r.Blocks[i].Data = data
		return r.Encode(), n, nil
	}
	return nil, 0, errors.New("no DATA block")
}

// hideProps shrinks every prop_dynamic of an entity lump and drops it below the scene.
func hideProps(root *Value) int {
	n := 0
	for _, ent := range root.Get("m_entityKeyValues").items() {
		values := ent.Get("keyValues3Data").Get("values")
		if values == nil {
			continue
		}
		if class := values.Get("classname"); class == nil || class.S != "prop_dynamic" {
			continue
		}
		for _, s := range values.Get("scales").items() {
			setFloat(s, 0.001)
		}
		if o := values.Get("origin").items(); len(o) == 3 {
			setFloat(o[2], floatOf(o[2])-hiddenDrop)
		}
		n++
	}
	return n
}

// hideSceneObjects shrinks every scene object of a world node (m_vTransform, a 3x4 matrix) and
// drops it below the scene.
func hideSceneObjects(root *Value) int {
	n := 0
	for _, obj := range root.Get("m_sceneObjects").items() {
		rows := obj.Get("m_vTransform").items()
		if len(rows) != 3 {
			continue
		}
		for r, row := range rows {
			for c, cell := range row.items() {
				v := 0.0
				if r == c {
					v = 0.001
				}
				if c == 3 && r == 2 {
					v = -hiddenDrop
				}
				setFloat(cell, v)
			}
		}
		n++
	}
	return n
}

func (v *Value) items() []*Value {
	if v == nil {
		return nil
	}
	return v.Items
}

func floatOf(v *Value) float64 {
	switch v.Kind {
	case Float32, Float64:
		return v.F
	case Int16, Int32, Int64:
		return float64(v.I)
	case Uint16, Uint32, Uint64:
		return float64(v.U)
	}
	return 0
}

// setFloat stores a number in a value, as a float when it was an integer (KV3 readers accept
// either for entity keys and transforms).
func setFloat(v *Value, f float64) {
	if v.Kind != Float32 && v.Kind != Float64 {
		v.Kind = Float32
	}
	v.F = f
}
