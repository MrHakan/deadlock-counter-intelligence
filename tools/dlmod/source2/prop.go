package source2

import (
	"errors"
	"fmt"
	"math"
	"path"
	"strings"
)

// BuildProp replaces the mesh of an ability prop in the older layout (a thrown bomb) with an
// imported one: fitted to the prop's size times scale, centred where the prop's mesh was, all of
// it on the bone most of the old mesh followed, drawn with a material made from the import's
// colour texture. The new model is written at newPath (the prop's own path to replace it in
// place). Files holds what to install.
func BuildProp(raw []byte, newPath string, mesh *ImportedMesh, scale float64) (map[string][]byte, error) {
	prop, err := LoadLegacyProp(raw)
	if err != nil {
		return nil, err
	}
	if len(mesh.Positions) == 0 || len(mesh.Parts) == 0 {
		return nil, errors.New("the model has no triangles")
	}
	if scale <= 0 {
		scale = 1
	}
	lo, hi := prop.Bounds()
	size := math.Max(hi[0]-lo[0], math.Max(hi[1]-lo[1], hi[2]-lo[2]))
	centre := [3]float64{(lo[0] + hi[0]) / 2, (lo[1] + hi[1]) / 2, (lo[2] + hi[2]) / 2}
	// glTF is Y up and faces +Z; a model is Z up and faces +X.
	turn := func(v [3]float32) [3]float64 { return [3]float64{float64(v[2]), float64(v[0]), float64(v[1])} }
	nlo := [3]float64{math.Inf(1), math.Inf(1), math.Inf(1)}
	nhi := [3]float64{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	for _, p := range mesh.Positions {
		t := turn(p)
		for k := 0; k < 3; k++ {
			nlo[k], nhi[k] = math.Min(nlo[k], t[k]), math.Max(nhi[k], t[k])
		}
	}
	nsize := math.Max(nhi[0]-nlo[0], math.Max(nhi[1]-nlo[1], nhi[2]-nlo[2]))
	if nsize <= 0 || size <= 0 {
		return nil, errors.New("the model or the prop has no size")
	}
	k := size / nsize * scale
	ncentre := [3]float64{(nlo[0] + nhi[0]) / 2, (nlo[1] + nhi[1]) / 2, (nlo[2] + nhi[2]) / 2}
	positions := make([][3]float32, len(mesh.Positions))
	normals := make([][3]float32, len(mesh.Positions))
	for i, p := range mesh.Positions {
		t := turn(p)
		for j := 0; j < 3; j++ {
			positions[i][j] = float32((t[j]-ncentre[j])*k + centre[j])
		}
		if i < len(mesh.Normals) {
			n := normalize(turn(mesh.Normals[i]))
			normals[i] = [3]float32{float32(n[0]), float32(n[1]), float32(n[2])}
		} else {
			normals[i] = [3]float32{0, 0, 1}
		}
	}
	uvs := mesh.UVs
	if len(uvs) < len(positions) {
		uvs = make([][2]float32, len(positions))
		copy(uvs, mesh.UVs)
	}
	var indices []uint32
	for _, part := range mesh.Parts {
		indices = append(indices, part.Indices...)
	}
	// The bone the old mesh mostly followed (a spinning core, say).
	bone := 0
	if skin, err := prop.vertex[0].vertexBuffer().Skin(); err == nil {
		count := map[int]float32{}
		for _, influences := range skin {
			for _, in := range influences {
				count[in.Bone] += in.Weight
			}
		}
		best := float32(-1)
		for b, w := range count {
			if w > best {
				bone, best = b, w
			}
		}
	}
	// Material and texture, next to the new model.
	name := safeName(strings.TrimSuffix(path.Base(newPath), path.Ext(newPath)))
	folder := "models/dlmod/props"
	part := mesh.Parts[0]
	img := SolidImage(part.Color)
	tint := [4]float32{1, 1, 1, 1}
	if part.Texture != nil {
		decoded, err := DecodeImage(part.Texture)
		if err != nil {
			return nil, fmt.Errorf("the texture: %v", err)
		}
		img, tint = decoded, part.Color
	}
	texture, err := CompileTexture(img, 2048, tint)
	if err != nil {
		return nil, err
	}
	texturePath := folder + "/" + name + "_color.vtex"
	materialPath := folder + "/" + name + ".vmat"
	material, err := CompileMaterial(materialPath, texturePath, powerOfTwo(max(img.Bounds().Dx(), img.Bounds().Dy()), 2048), part.Color[3] < 1)
	if err != nil {
		return nil, err
	}
	built, err := prop.Replace(positions, normals, uvs, indices, bone, materialPath)
	if err != nil {
		return nil, err
	}
	return map[string][]byte{newPath + "_c": built, texturePath + "_c": texture, materialPath + "_c": material}, nil
}

// RepointParticle makes a compiled particle system use model to instead of from: in its
// references and wherever its data names the model.
func RepointParticle(raw []byte, from, to string) ([]byte, int, error) {
	res, err := DecodeResource(raw)
	if err != nil {
		return nil, 0, err
	}
	changed := 0
	blocks := make([]Block, len(res.Blocks))
	copy(blocks, res.Blocks)
	for i, b := range blocks {
		switch b.Type {
		case "RERL":
			refs, err := ReadReferences(b.Data)
			if err != nil {
				return nil, 0, err
			}
			for j := range refs {
				if strings.EqualFold(refs[j].Name, from) {
					refs[j] = Reference{ResourceID(to), to}
					changed++
				}
			}
			blocks[i].Data = WriteReferences(refs)
		case "DATA":
			root, format, err := ReadKV3(b.Data)
			if err != nil {
				return nil, 0, err
			}
			n := 0
			var walk func(v *Value)
			walk = func(v *Value) {
				switch v.Kind {
				case String:
					if strings.EqualFold(v.S, from) {
						v.S = to
						n++
					}
				case Object:
					for _, f := range v.Fields {
						walk(f)
					}
				case Array:
					for _, it := range v.Items {
						walk(it)
					}
				}
			}
			walk(root)
			if n > 0 {
				data, err := WriteKV3(root, format)
				if err != nil {
					return nil, 0, err
				}
				blocks[i].Data = data
				changed += n
			}
		}
	}
	if changed == 0 {
		return nil, 0, fmt.Errorf("the particle system does not use %s", from)
	}
	out := &Resource{Version: res.Version, Blocks: blocks, Streaming: res.Streaming}
	return out.Encode(), changed, nil
}
