package source2

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ImportedMesh is a model read from an .obj, .gltf or .glb file, in the file's own axes and units.
type ImportedMesh struct {
	Positions [][3]float32
	Normals   [][3]float32 // nil when the file has none; they are computed then
	UVs       [][2]float32
	Parts     []ImportedPart
	YUp       bool // glTF is always Y-up; OBJ files nearly always are

	// A rigged model's own skinning: each vertex's joints (into JointNames) and weights. The
	// importer maps these joints onto the hero's bones instead of guessing weights from scratch.
	Joints         [][]JointWeight
	JointNames     []string
	JointParents   []int        // -1 for a root
	JointPositions [][3]float32 // where each joint stands at rest, in the same space as Positions
}

// JointWeight is one joint of the imported model's own skeleton a vertex follows.
type JointWeight struct {
	Joint  int
	Weight float32
}

// ImportedPart is the triangles drawn with one of the file's materials.
type ImportedPart struct {
	Name        string
	Indices     []uint32
	Texture     []byte // the base colour image (PNG or JPEG), when the file has one
	TextureName string
	Color       [4]float32 // the base colour factor
}

// LoadMeshFile reads a model file by its extension.
func LoadMeshFile(file string) (*ImportedMesh, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	return LoadMesh(filepath.Base(file), data, func(name string) ([]byte, error) {
		return os.ReadFile(filepath.Join(filepath.Dir(file), filepath.FromSlash(name)))
	})
}

// LoadMesh reads a model from memory. open fetches files it refers to, such as an .obj's .mtl
// and textures or a .gltf's .bin; it may be nil.
func LoadMesh(name string, data []byte, open func(string) ([]byte, error)) (*ImportedMesh, error) {
	if open == nil {
		open = func(string) ([]byte, error) { return nil, os.ErrNotExist }
	}
	var mesh *ImportedMesh
	var err error
	switch strings.ToLower(filepath.Ext(name)) {
	case ".obj":
		mesh, err = loadOBJ(data, open)
	case ".glb", ".gltf":
		mesh, err = loadGLTF(data, open)
	case ".fbx":
		return nil, errors.New("FBX is not supported yet: open it in Blender and export it as .glb (File > Export > glTF 2.0)")
	default:
		return nil, fmt.Errorf("%s is not a model dlmod reads; use .obj, .gltf or .glb", name)
	}
	if err != nil {
		return nil, err
	}
	if len(mesh.Positions) == 0 {
		return nil, errors.New("the file holds no triangles")
	}
	if mesh.UVs == nil {
		mesh.UVs = make([][2]float32, len(mesh.Positions))
	}
	if mesh.Normals == nil {
		mesh.Normals = computeNormals(mesh)
	}
	var parts []ImportedPart
	for _, part := range mesh.Parts {
		if len(part.Indices) >= 3 {
			parts = append(parts, part)
		}
	}
	mesh.Parts = parts
	return mesh, nil
}

// TriangleCount is how many triangles the mesh has.
func (m *ImportedMesh) TriangleCount() int {
	n := 0
	for _, part := range m.Parts {
		n += len(part.Indices) / 3
	}
	return n
}

func computeNormals(mesh *ImportedMesh) [][3]float32 {
	sums := make([][3]float64, len(mesh.Positions))
	for _, part := range mesh.Parts {
		for t := 0; t+2 < len(part.Indices); t += 3 {
			a, b, c := part.Indices[t], part.Indices[t+1], part.Indices[t+2]
			pa, pb, pc := vec64(mesh.Positions[a]), vec64(mesh.Positions[b]), vec64(mesh.Positions[c])
			n := cross(sub(pb, pa), sub(pc, pa)) // weighted by area
			for _, i := range []uint32{a, b, c} {
				sums[i] = [3]float64{sums[i][0] + n[0], sums[i][1] + n[1], sums[i][2] + n[2]}
			}
		}
	}
	out := make([][3]float32, len(sums))
	for i, s := range sums {
		n := normalize(s)
		if n == [3]float64{} {
			n = [3]float64{0, 0, 1}
		}
		out[i] = [3]float32{float32(n[0]), float32(n[1]), float32(n[2])}
	}
	return out
}

func vec64(p [3]float32) [3]float64 { return [3]float64{float64(p[0]), float64(p[1]), float64(p[2])} }

// --- OBJ ---

func loadOBJ(data []byte, open func(string) ([]byte, error)) (*ImportedMesh, error) {
	mesh := &ImportedMesh{YUp: true}
	var positions [][3]float32
	var uvs [][2]float32
	var normals [][3]float32
	type corner struct{ v, t, n int }
	seen := map[corner]uint32{}
	materials := map[string]ImportedPart{}
	partIndex := map[string]int{}
	current := -1
	anyNormals := false
	use := func(name string) {
		if i, ok := partIndex[name]; ok {
			current = i
			return
		}
		part := materials[name]
		part.Name = name
		if part.Color == [4]float32{} {
			part.Color = [4]float32{1, 1, 1, 1}
		}
		mesh.Parts = append(mesh.Parts, part)
		current = len(mesh.Parts) - 1
		partIndex[name] = current
	}
	floats := func(fields []string, n int) ([]float32, error) {
		if len(fields) < n {
			return nil, errors.New("the OBJ file has a short line")
		}
		out := make([]float32, n)
		for i := 0; i < n; i++ {
			f, err := strconv.ParseFloat(fields[i], 32)
			if err != nil {
				return nil, fmt.Errorf("the OBJ file has a bad number %q", fields[i])
			}
			out[i] = float32(f)
		}
		return out, nil
	}
	resolve := func(text string, count int) int {
		if text == "" {
			return -1
		}
		i, err := strconv.Atoi(text)
		if err != nil {
			return -1
		}
		if i < 0 {
			i += count
		} else {
			i--
		}
		if i < 0 || i >= count {
			return -1
		}
		return i
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 1<<20), 1<<24)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line[0] == '#' {
			continue
		}
		fields := strings.Fields(line)
		switch fields[0] {
		case "v":
			f, err := floats(fields[1:], 3)
			if err != nil {
				return nil, err
			}
			positions = append(positions, [3]float32{f[0], f[1], f[2]})
		case "vt":
			f, err := floats(append(fields[1:], "0"), 2)
			if err != nil {
				return nil, err
			}
			uvs = append(uvs, [2]float32{f[0], 1 - f[1]}) // OBJ puts v=0 at the bottom of the image
		case "vn":
			f, err := floats(fields[1:], 3)
			if err != nil {
				return nil, err
			}
			normals = append(normals, [3]float32{f[0], f[1], f[2]})
		case "mtllib":
			if text, err := open(strings.TrimSpace(strings.TrimPrefix(line, "mtllib"))); err == nil {
				for name, part := range parseMTL(text, open) {
					materials[name] = part
				}
			}
		case "usemtl":
			use(strings.TrimSpace(strings.TrimPrefix(line, "usemtl")))
		case "f":
			if current < 0 {
				use("default")
			}
			var face []uint32
			for _, field := range fields[1:] {
				parts := strings.Split(field, "/")
				c := corner{v: resolve(parts[0], len(positions)), t: -1, n: -1}
				if len(parts) > 1 {
					c.t = resolve(parts[1], len(uvs))
				}
				if len(parts) > 2 {
					c.n = resolve(parts[2], len(normals))
				}
				if c.v < 0 {
					return nil, errors.New("the OBJ file has a face pointing at a missing vertex")
				}
				index, ok := seen[c]
				if !ok {
					index = uint32(len(mesh.Positions))
					seen[c] = index
					mesh.Positions = append(mesh.Positions, positions[c.v])
					uv := [2]float32{}
					if c.t >= 0 {
						uv = uvs[c.t]
					}
					mesh.UVs = append(mesh.UVs, uv)
					n := [3]float32{}
					if c.n >= 0 {
						n = normals[c.n]
						anyNormals = true
					}
					mesh.Normals = append(mesh.Normals, n)
				}
				face = append(face, index)
			}
			for k := 1; k+1 < len(face); k++ { // polygons become triangle fans
				mesh.Parts[current].Indices = append(mesh.Parts[current].Indices, face[0], face[k], face[k+1])
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if !anyNormals {
		mesh.Normals = nil
	}
	return mesh, nil
}

func parseMTL(data []byte, open func(string) ([]byte, error)) map[string]ImportedPart {
	out := map[string]ImportedPart{}
	name := ""
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		part := out[name]
		switch strings.ToLower(fields[0]) {
		case "newmtl":
			name = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), fields[0]))
			out[name] = ImportedPart{Name: name, Color: [4]float32{1, 1, 1, 1}}
			continue
		case "kd":
			if len(fields) >= 4 {
				for k := 0; k < 3; k++ {
					f, _ := strconv.ParseFloat(fields[1+k], 32)
					part.Color[k] = float32(f)
				}
			}
		case "map_kd":
			texture := fields[len(fields)-1] // options such as -s come before the file name
			if image, err := open(texture); err == nil {
				part.Texture, part.TextureName = image, filepath.Base(texture)
			}
		}
		if name != "" {
			out[name] = part
		}
	}
	return out
}

// --- glTF ---

type gltfDoc struct {
	Scene  *int `json:"scene"`
	Scenes []struct {
		Nodes []int `json:"nodes"`
	} `json:"scenes"`
	Skins []struct {
		Joints              []int `json:"joints"`
		InverseBindMatrices *int  `json:"inverseBindMatrices"`
	} `json:"skins"`
	Nodes []struct {
		Name        string    `json:"name"`
		Mesh        *int      `json:"mesh"`
		Skin        *int      `json:"skin"`
		Children    []int     `json:"children"`
		Matrix      []float64 `json:"matrix"`
		Translation []float64 `json:"translation"`
		Rotation    []float64 `json:"rotation"`
		Scale       []float64 `json:"scale"`
	} `json:"nodes"`
	Meshes []struct {
		Primitives []struct {
			Attributes map[string]int `json:"attributes"`
			Indices    *int           `json:"indices"`
			Material   *int           `json:"material"`
			Mode       *int           `json:"mode"`
		} `json:"primitives"`
	} `json:"meshes"`
	Accessors []struct {
		BufferView    *int   `json:"bufferView"`
		ByteOffset    int    `json:"byteOffset"`
		ComponentType int    `json:"componentType"`
		Normalized    bool   `json:"normalized"`
		Count         int    `json:"count"`
		Type          string `json:"type"`
	} `json:"accessors"`
	BufferViews []struct {
		Buffer     int `json:"buffer"`
		ByteOffset int `json:"byteOffset"`
		ByteLength int `json:"byteLength"`
		ByteStride int `json:"byteStride"`
		Extensions struct {
			Meshopt *struct {
				Buffer     int    `json:"buffer"`
				ByteOffset int    `json:"byteOffset"`
				ByteLength int    `json:"byteLength"`
				ByteStride int    `json:"byteStride"`
				Count      int    `json:"count"`
				Mode       string `json:"mode"`
				Filter     string `json:"filter"`
			} `json:"EXT_meshopt_compression"`
		} `json:"extensions"`
	} `json:"bufferViews"`
	Buffers []struct {
		URI        string `json:"uri"`
		ByteLength int    `json:"byteLength"`
	} `json:"buffers"`
	Materials []struct {
		Name string `json:"name"`
		PBR  struct {
			BaseColorFactor  []float64 `json:"baseColorFactor"`
			BaseColorTexture *struct {
				Index int `json:"index"`
			} `json:"baseColorTexture"`
		} `json:"pbrMetallicRoughness"`
	} `json:"materials"`
	Textures []struct {
		Source     *int `json:"source"`
		Extensions map[string]struct {
			Source *int `json:"source"`
		} `json:"extensions"` // EXT_texture_webp and the like name their image here
	} `json:"textures"`
	Images []struct {
		URI        string `json:"uri"`
		BufferView *int   `json:"bufferView"`
		Name       string `json:"name"`
	} `json:"images"`
}

func loadGLTF(data []byte, open func(string) ([]byte, error)) (*ImportedMesh, error) {
	var jsonPart, binPart []byte
	if len(data) >= 12 && string(data[:4]) == "glTF" {
		for offset := 12; offset+8 <= len(data); {
			length := int(binary.LittleEndian.Uint32(data[offset:]))
			kind := string(data[offset+4 : offset+8])
			end := offset + 8 + length
			if end > len(data) {
				return nil, errors.New("the GLB file is cut short")
			}
			switch kind {
			case "JSON":
				jsonPart = data[offset+8 : end]
			case "BIN\x00":
				binPart = data[offset+8 : end]
			}
			offset = end
		}
	} else {
		jsonPart = data
	}
	var doc gltfDoc
	if err := json.Unmarshal(jsonPart, &doc); err != nil {
		return nil, fmt.Errorf("the glTF file is damaged: %v", err)
	}
	buffers := make([][]byte, len(doc.Buffers))
	for i, b := range doc.Buffers {
		switch {
		case b.URI == "" && i == 0:
			buffers[i] = binPart
		case b.URI == "": // a placeholder, such as the fallback buffer of meshopt-compressed files
		case strings.HasPrefix(b.URI, "data:"):
			comma := strings.IndexByte(b.URI, ',')
			decoded, err := base64.StdEncoding.DecodeString(b.URI[comma+1:])
			if err != nil {
				return nil, errors.New("the glTF file has a damaged embedded buffer")
			}
			buffers[i] = decoded
		default:
			file, err := open(b.URI)
			if err != nil {
				return nil, fmt.Errorf("the glTF file needs %s next to it", b.URI)
			}
			buffers[i] = file
		}
	}
	decoded := map[int][]byte{}
	view := func(index int) ([]byte, int, error) {
		if index < 0 || index >= len(doc.BufferViews) {
			return nil, 0, errors.New("the glTF file points at a missing buffer view")
		}
		v := doc.BufferViews[index]
		if c := v.Extensions.Meshopt; c != nil {
			if data, ok := decoded[index]; ok {
				return data, v.ByteStride, nil
			}
			if c.Buffer >= len(buffers) || c.ByteOffset+c.ByteLength > len(buffers[c.Buffer]) {
				return nil, 0, errors.New("the glTF file's compressed data is missing")
			}
			data, err := decodeMeshoptView(buffers[c.Buffer][c.ByteOffset:c.ByteOffset+c.ByteLength], c.Count, c.ByteStride, c.Mode, c.Filter)
			if err != nil {
				return nil, 0, err
			}
			decoded[index] = data
			return data, v.ByteStride, nil
		}
		if v.Buffer >= len(buffers) || v.ByteOffset+v.ByteLength > len(buffers[v.Buffer]) {
			return nil, 0, errors.New("the glTF file points past its data")
		}
		return buffers[v.Buffer][v.ByteOffset : v.ByteOffset+v.ByteLength], v.ByteStride, nil
	}
	// accessor reads an accessor as floats (or integers stored in floats), n per element.
	accessor := func(index int) ([][]float64, error) {
		if index < 0 || index >= len(doc.Accessors) {
			return nil, errors.New("the glTF file points at a missing accessor")
		}
		a := doc.Accessors[index]
		n := map[string]int{"SCALAR": 1, "VEC2": 2, "VEC3": 3, "VEC4": 4, "MAT4": 16}[a.Type]
		size := map[int]int{5120: 1, 5121: 1, 5122: 2, 5123: 2, 5125: 4, 5126: 4}[a.ComponentType]
		if n == 0 || size == 0 {
			return nil, fmt.Errorf("the glTF file uses an accessor of type %s/%d", a.Type, a.ComponentType)
		}
		out := make([][]float64, a.Count)
		if a.BufferView == nil {
			for i := range out {
				out[i] = make([]float64, n)
			}
			return out, nil
		}
		raw, stride, err := view(*a.BufferView)
		if err != nil {
			return nil, err
		}
		if stride == 0 {
			stride = n * size
		}
		for i := range out {
			start := a.ByteOffset + i*stride
			if start+n*size > len(raw) {
				return nil, errors.New("the glTF file's accessor runs past its data")
			}
			row := make([]float64, n)
			for k := 0; k < n; k++ {
				p := raw[start+k*size:]
				switch a.ComponentType {
				case 5126:
					row[k] = float64(math.Float32frombits(binary.LittleEndian.Uint32(p)))
				case 5125:
					row[k] = float64(binary.LittleEndian.Uint32(p))
				case 5123:
					row[k] = float64(binary.LittleEndian.Uint16(p))
					if a.Normalized {
						row[k] /= 65535
					}
				case 5122:
					row[k] = float64(int16(binary.LittleEndian.Uint16(p)))
					if a.Normalized {
						row[k] = math.Max(row[k]/32767, -1)
					}
				case 5121:
					row[k] = float64(p[0])
					if a.Normalized {
						row[k] /= 255
					}
				case 5120:
					row[k] = float64(int8(p[0]))
					if a.Normalized {
						row[k] = math.Max(row[k]/127, -1)
					}
				}
			}
			out[i] = row
		}
		return out, nil
	}
	image := func(material int) ([]byte, string, [4]float32) {
		color := [4]float32{1, 1, 1, 1}
		if material < 0 || material >= len(doc.Materials) {
			return nil, "", color
		}
		m := doc.Materials[material]
		for k := 0; k < len(m.PBR.BaseColorFactor) && k < 4; k++ {
			color[k] = float32(m.PBR.BaseColorFactor[k])
		}
		if m.PBR.BaseColorTexture == nil || m.PBR.BaseColorTexture.Index >= len(doc.Textures) {
			return nil, "", color
		}
		texture := doc.Textures[m.PBR.BaseColorTexture.Index]
		source := texture.Source
		for _, ext := range texture.Extensions {
			if source == nil && ext.Source != nil {
				source = ext.Source
			}
		}
		if source == nil || *source >= len(doc.Images) {
			return nil, "", color
		}
		img := doc.Images[*source]
		name := img.Name
		switch {
		case img.BufferView != nil:
			raw, _, err := view(*img.BufferView)
			if err == nil {
				return raw, name, color
			}
		case strings.HasPrefix(img.URI, "data:"):
			comma := strings.IndexByte(img.URI, ',')
			if raw, err := base64.StdEncoding.DecodeString(img.URI[comma+1:]); err == nil {
				return raw, name, color
			}
		case img.URI != "":
			if raw, err := open(img.URI); err == nil {
				return raw, filepath.Base(img.URI), color
			}
		}
		return nil, "", color
	}

	// Every node's world matrix, which skinned meshes need for their joints.
	worlds := make([][16]float64, len(doc.Nodes))
	var place func(node int, parent [16]float64, depth int)
	place = func(node int, parent [16]float64, depth int) {
		if node < 0 || node >= len(doc.Nodes) || depth > 64 {
			return
		}
		n := doc.Nodes[node]
		worlds[node] = mulMatrix(parent, nodeMatrix(n.Matrix, n.Translation, n.Rotation, n.Scale))
		for _, child := range n.Children {
			place(child, worlds[node], depth+1)
		}
	}
	for _, root := range gltfRoots(&doc) {
		place(root, identityMatrix(), 0)
	}
	// jointMatrices gives a skin's joints their rest pose: the joint's world matrix times its
	// inverse bind matrix, which together carry a vertex from the mesh to where it stands.
	jointMatrices := func(skin int) ([][16]float64, error) {
		if skin < 0 || skin >= len(doc.Skins) {
			return nil, nil
		}
		s := doc.Skins[skin]
		var inverse [][]float64
		if s.InverseBindMatrices != nil {
			a := *s.InverseBindMatrices
			if a < len(doc.Accessors) && doc.Accessors[a].Type == "MAT4" {
				rows, err := accessor(a)
				if err != nil {
					return nil, err
				}
				inverse = rows
			}
		}
		out := make([][16]float64, len(s.Joints))
		for i, joint := range s.Joints {
			ibm := identityMatrix()
			if i < len(inverse) {
				copy(ibm[:], inverse[i])
			}
			if joint >= 0 && joint < len(worlds) {
				out[i] = mulMatrix(worlds[joint], ibm)
			} else {
				out[i] = ibm
			}
		}
		return out, nil
	}

	mesh := &ImportedMesh{YUp: true}
	partIndex := map[int]int{}
	var walk func(node int, parent [16]float64, depth int) error
	walk = func(node int, parent [16]float64, depth int) error {
		if node < 0 || node >= len(doc.Nodes) || depth > 64 {
			return nil
		}
		n := doc.Nodes[node]
		world := mulMatrix(parent, nodeMatrix(n.Matrix, n.Translation, n.Rotation, n.Scale))
		if n.Mesh != nil && *n.Mesh < len(doc.Meshes) {
			for _, prim := range doc.Meshes[*n.Mesh].Primitives {
				if prim.Mode != nil && *prim.Mode != 4 {
					continue // points and lines
				}
				posIndex, ok := prim.Attributes["POSITION"]
				if !ok {
					continue
				}
				positions, err := accessor(posIndex)
				if err != nil {
					return err
				}
				var normals, uvs [][]float64
				if i, ok := prim.Attributes["NORMAL"]; ok {
					if normals, err = accessor(i); err != nil {
						return err
					}
				}
				if i, ok := prim.Attributes["TEXCOORD_0"]; ok {
					if uvs, err = accessor(i); err != nil {
						return err
					}
				}
				material := -1
				if prim.Material != nil {
					material = *prim.Material
				}
				pi, ok := partIndex[material]
				if !ok {
					name := fmt.Sprintf("material%d", len(mesh.Parts))
					if material >= 0 && material < len(doc.Materials) && doc.Materials[material].Name != "" {
						name = doc.Materials[material].Name
					}
					texture, textureName, color := image(material)
					mesh.Parts = append(mesh.Parts, ImportedPart{Name: name, Texture: texture, TextureName: textureName, Color: color})
					pi = len(mesh.Parts) - 1
					partIndex[material] = pi
				}
				base := uint32(len(mesh.Positions))
				firstIndex := len(mesh.Parts[pi].Indices)
				// A skinned mesh ignores its node's transform and follows its joints instead.
				var joints, weights [][]float64
				var skinning [][16]float64
				if n.Skin != nil {
					if skinning, err = jointMatrices(*n.Skin); err != nil {
						return err
					}
					ji, okJ := prim.Attributes["JOINTS_0"]
					wi, okW := prim.Attributes["WEIGHTS_0"]
					if okJ && okW && skinning != nil {
						if joints, err = accessor(ji); err != nil {
							return err
						}
						if weights, err = accessor(wi); err != nil {
							return err
						}
					}
				}
				if joints != nil && mesh.Joints == nil {
					mesh.Joints = make([][]JointWeight, len(mesh.Positions))
					mesh.JointNames = make([]string, len(doc.Nodes))
					mesh.JointParents = make([]int, len(doc.Nodes))
					mesh.JointPositions = make([][3]float32, len(doc.Nodes))
					for k, node := range doc.Nodes {
						mesh.JointNames[k] = node.Name
						if node.Name == "" {
							mesh.JointNames[k] = fmt.Sprintf("joint%d", k)
						}
						mesh.JointParents[k] = -1
						mesh.JointPositions[k] = transformPoint(worlds[k], []float64{0, 0, 0})
					}
					for k, node := range doc.Nodes {
						for _, child := range node.Children {
							if child >= 0 && child < len(doc.Nodes) {
								mesh.JointParents[child] = k
							}
						}
					}
				}
				for i, p := range positions {
					matrix := world // fine for rotations and uniform scale; normals are renormalised
					var follows []JointWeight
					if joints != nil && i < len(joints) && i < len(weights) {
						matrix = blendMatrices(skinning, joints[i], weights[i])
						for k := 0; k < len(joints[i]) && k < len(weights[i]); k++ {
							j := int(joints[i][k])
							if weights[i][k] > 0 && j >= 0 && j < len(doc.Skins[*n.Skin].Joints) {
								follows = append(follows, JointWeight{doc.Skins[*n.Skin].Joints[j], float32(weights[i][k])})
							}
						}
					}
					if mesh.Joints != nil {
						mesh.Joints = append(mesh.Joints, follows)
					}
					mesh.Positions = append(mesh.Positions, transformPoint(matrix, p))
					if normals != nil && i < len(normals) {
						n := transformVector(matrix, normals[i])
						mesh.Normals = append(mesh.Normals, n)
					} else {
						mesh.Normals = append(mesh.Normals, [3]float32{})
					}
					uv := [2]float32{}
					if uvs != nil && i < len(uvs) {
						uv = [2]float32{float32(uvs[i][0]), float32(uvs[i][1])}
					}
					mesh.UVs = append(mesh.UVs, uv)
				}
				if prim.Indices != nil {
					indices, err := accessor(*prim.Indices)
					if err != nil {
						return err
					}
					for _, i := range indices {
						if int(i[0]) >= len(positions) {
							return errors.New("the glTF file has an index past its vertices")
						}
						mesh.Parts[pi].Indices = append(mesh.Parts[pi].Indices, base+uint32(i[0]))
					}
				} else {
					for i := range positions {
						mesh.Parts[pi].Indices = append(mesh.Parts[pi].Indices, base+uint32(i))
					}
				}
				if joints == nil && det3(world) < 0 { // a mirroring transform flips the winding
					indices := mesh.Parts[pi].Indices
					for t := firstIndex; t+2 < len(indices); t += 3 {
						indices[t+1], indices[t+2] = indices[t+2], indices[t+1]
					}
				}
			}
		}
		for _, child := range n.Children {
			if err := walk(child, world, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	for _, root := range gltfRoots(&doc) {
		if err := walk(root, identityMatrix(), 0); err != nil {
			return nil, err
		}
	}
	missing := false
	for _, n := range mesh.Normals {
		if n == [3]float32{} {
			missing = true
			break
		}
	}
	if missing {
		mesh.Normals = nil
	}
	return mesh, nil
}

// Column-major 4x4 matrices, as glTF stores them.
func identityMatrix() [16]float64 { return [16]float64{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1} }

func nodeMatrix(matrix, t, r, s []float64) [16]float64 {
	if len(matrix) == 16 {
		var m [16]float64
		copy(m[:], matrix)
		return m
	}
	m := identityMatrix()
	if len(r) == 4 {
		x, y, z, w := r[0], r[1], r[2], r[3]
		m = [16]float64{
			1 - 2*(y*y+z*z), 2 * (x*y + z*w), 2 * (x*z - y*w), 0,
			2 * (x*y - z*w), 1 - 2*(x*x+z*z), 2 * (y*z + x*w), 0,
			2 * (x*z + y*w), 2 * (y*z - x*w), 1 - 2*(x*x+y*y), 0,
			0, 0, 0, 1,
		}
	}
	if len(s) == 3 {
		for col := 0; col < 3; col++ {
			for row := 0; row < 3; row++ {
				m[col*4+row] *= s[col]
			}
		}
	}
	if len(t) == 3 {
		m[12], m[13], m[14] = t[0], t[1], t[2]
	}
	return m
}

func mulMatrix(a, b [16]float64) [16]float64 {
	var out [16]float64
	for col := 0; col < 4; col++ {
		for row := 0; row < 4; row++ {
			var sum float64
			for k := 0; k < 4; k++ {
				sum += a[k*4+row] * b[col*4+k]
			}
			out[col*4+row] = sum
		}
	}
	return out
}

func transformPoint(m [16]float64, p []float64) [3]float32 {
	var out [3]float32
	for row := 0; row < 3; row++ {
		out[row] = float32(m[row]*p[0] + m[4+row]*p[1] + m[8+row]*p[2] + m[12+row])
	}
	return out
}

func transformVector(m [16]float64, v []float64) [3]float32 {
	var out [3]float64
	for row := 0; row < 3; row++ {
		out[row] = m[row]*v[0] + m[4+row]*v[1] + m[8+row]*v[2]
	}
	n := normalize(out)
	return [3]float32{float32(n[0]), float32(n[1]), float32(n[2])}
}

func det3(m [16]float64) float64 {
	return m[0]*(m[5]*m[10]-m[9]*m[6]) - m[4]*(m[1]*m[10]-m[9]*m[2]) + m[8]*(m[1]*m[6]-m[5]*m[2])
}

// decodeMeshoptView undoes EXT_meshopt_compression on one buffer view, as gltfpack and
// glTF-Transform write it.
func decodeMeshoptView(data []byte, count, stride int, mode, filter string) ([]byte, error) {
	var out []byte
	var err error
	switch mode {
	case "ATTRIBUTES":
		out, err = DecodeVertexBuffer(count, stride, data)
	case "TRIANGLES":
		out, err = DecodeIndexBuffer(count, stride, data)
	case "INDICES":
		out, err = DecodeIndexSequence(count, stride, data)
	default:
		return nil, fmt.Errorf("the glTF file uses meshopt mode %s", mode)
	}
	if err != nil {
		return nil, err
	}
	switch filter {
	case "", "NONE":
	case "OCTAHEDRAL":
		octahedralFilter(out, count, stride)
	case "EXPONENTIAL":
		for i := 0; i+4 <= len(out); i += 4 {
			v := int32(binary.LittleEndian.Uint32(out[i:]))
			mantissa := (v << 8) >> 8
			exponent := v >> 24
			binary.LittleEndian.PutUint32(out[i:], math.Float32bits(float32(math.Ldexp(float64(mantissa), int(exponent)))))
		}
	case "QUATERNION":
		quaternionFilter(out, count, stride)
	default:
		return nil, fmt.Errorf("the glTF file uses meshopt filter %s", filter)
	}
	return out, nil
}

// octahedralFilter turns octahedral-encoded normals back into vectors of the same integer width.
func octahedralFilter(data []byte, count, stride int) {
	wide := stride == 8
	limit := 127.0
	if wide {
		limit = 32767
	}
	get := func(i, k int) float64 {
		if wide {
			return float64(int16(binary.LittleEndian.Uint16(data[i*8+2*k:])))
		}
		return float64(int8(data[i*4+k]))
	}
	put := func(i, k int, v float64) {
		r := int(math.Round(v))
		if wide {
			binary.LittleEndian.PutUint16(data[i*8+2*k:], uint16(int16(r)))
		} else {
			data[i*4+k] = byte(int8(r))
		}
	}
	for i := 0; i < count; i++ {
		x, y := get(i, 0), get(i, 1)
		z := get(i, 2) - math.Abs(x) - math.Abs(y)
		t := math.Min(z, 0)
		if x >= 0 {
			x += t
		} else {
			x -= t
		}
		if y >= 0 {
			y += t
		} else {
			y -= t
		}
		l := math.Sqrt(x*x + y*y + z*z)
		if l == 0 {
			continue
		}
		s := limit / l
		put(i, 0, x*s)
		put(i, 1, y*s)
		put(i, 2, z*s)
	}
}

// quaternionFilter turns quaternions stored as three components and the index of the largest
// back into four components.
func quaternionFilter(data []byte, count, stride int) {
	wide := stride == 8
	scale := 1 / math.Sqrt2
	for i := 0; i < count; i++ {
		var c [4]float64
		var largest, bits int
		if wide {
			for k := 0; k < 4; k++ {
				c[k] = float64(int16(binary.LittleEndian.Uint16(data[i*8+2*k:])))
			}
			largest = int(c[3]) & 3
			bits = int(c[3]) | 3
		} else {
			for k := 0; k < 4; k++ {
				c[k] = float64(int8(data[i*4+k]))
			}
			largest = int(c[3]) & 3
			bits = int(c[3]) | 3
		}
		ss := scale / float64(bits)
		x, y, z := c[0]*ss, c[1]*ss, c[2]*ss
		w := math.Sqrt(math.Max(0, 1-x*x-y*y-z*z))
		q := [4]float64{}
		q[(largest+1)&3], q[(largest+2)&3], q[(largest+3)&3], q[largest] = x, y, z, w
		limit := 127.0
		if wide {
			limit = 32767
		}
		for k := 0; k < 4; k++ {
			r := int(math.Round(q[k] * limit))
			if wide {
				binary.LittleEndian.PutUint16(data[i*8+2*k:], uint16(int16(r)))
			} else {
				data[i*4+k] = byte(int8(r))
			}
		}
	}
}

func gltfRoots(doc *gltfDoc) []int {
	switch {
	case doc.Scene != nil && *doc.Scene < len(doc.Scenes):
		return doc.Scenes[*doc.Scene].Nodes
	case len(doc.Scenes) > 0:
		return doc.Scenes[0].Nodes
	}
	isChild := map[int]bool{}
	for _, n := range doc.Nodes {
		for _, c := range n.Children {
			isChild[c] = true
		}
	}
	var roots []int
	for i := range doc.Nodes {
		if !isChild[i] {
			roots = append(roots, i)
		}
	}
	return roots
}

// blendMatrices mixes joint matrices by a vertex's weights.
func blendMatrices(matrices [][16]float64, joints, weights []float64) [16]float64 {
	var out [16]float64
	total := 0.0
	for k := 0; k < len(joints) && k < len(weights); k++ {
		j, w := int(joints[k]), weights[k]
		if w == 0 || j < 0 || j >= len(matrices) {
			continue
		}
		for e := 0; e < 16; e++ {
			out[e] += matrices[j][e] * w
		}
		total += w
	}
	if total == 0 {
		return identityMatrix()
	}
	for e := 0; e < 16; e++ {
		out[e] /= total
	}
	return out
}
