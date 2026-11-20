package source2

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// ImportResult is a finished model import: the compiled files to pack, by their path in the game,
// and what happened along the way.
type ImportResult struct {
	Files    map[string][]byte
	Pose     Pose
	Vertices int
	Notes    []string // things the user should know, such as textures that could not be read
}

var unsafeName = regexp.MustCompile(`[^a-z0-9_]+`)

func safeName(s string) string {
	s = strings.Trim(unsafeName.ReplaceAllString(strings.ToLower(s), "_"), "_")
	if s == "" {
		s = "part"
	}
	return s
}

// ImportModel puts an imported model on a hero: it fits and rigs the model, compiles a material
// and texture for each of the model's materials, and rewrites the hero's model file around the
// new mesh. raw is the hero's compiled model and modelPath where it lives in the game, such as
// "models/heroes_wip/wraith/wraith.vmdl". pose may be nil to match the model's pose
// automatically. Materials already named in opts.Materials are used as they are.
func ImportModel(raw []byte, modelPath string, mesh *ImportedMesh, opts ImportOptions, pose *Pose) (*ImportResult, error) {
	model, err := LoadModel(raw)
	if err != nil {
		return nil, err
	}
	fitted, err := model.Fit(mesh, opts)
	if err != nil {
		return nil, err
	}
	rigged, err := model.AutoRig(fitted, opts, pose, 4)
	if err != nil {
		return nil, err
	}
	result := &ImportResult{Files: map[string][]byte{}, Pose: rigged.Pose, Vertices: len(fitted.Positions)}
	hero := strings.TrimSuffix(path.Base(modelPath), path.Ext(modelPath))
	folder := "models/dlmod/" + safeName(hero)
	materials := map[string]string{}
	for name, material := range opts.Materials {
		materials[name] = material
	}
	used := map[string]bool{}
	for _, part := range mesh.Parts {
		if materials[part.Name] != "" {
			continue
		}
		name := safeName(part.Name)
		for used[name] {
			name += "_"
		}
		used[name] = true
		img := SolidImage(part.Color)
		tint := [4]float32{1, 1, 1, 1}
		if part.Texture != nil {
			decoded, err := DecodeImage(part.Texture)
			if err != nil {
				result.Notes = append(result.Notes, fmt.Sprintf("%s: %v; it is drawn in its plain colour", part.Name, err))
			} else {
				img, tint = decoded, part.Color
			}
		}
		texture, err := CompileTexture(img, 2048, tint)
		if err != nil {
			return nil, fmt.Errorf("%s: %v", part.Name, err)
		}
		texturePath := folder + "/" + name + "_color.vtex"
		materialPath := folder + "/" + name + ".vmat"
		size := max(img.Bounds().Dx(), img.Bounds().Dy())
		material, err := CompileMaterial(materialPath, texturePath, powerOfTwo(size, 2048), part.Color[3] < 1)
		if err != nil {
			return nil, err
		}
		result.Files[texturePath+"_c"] = texture
		result.Files[materialPath+"_c"] = material
		materials[part.Name] = materialPath
	}
	opts.Materials = materials
	built, err := model.Replace(raw, rigged.Mesh, rigged.Skin, opts)
	if err != nil {
		return nil, err
	}
	result.Files[modelPath+"_c"] = built
	return result, nil
}
