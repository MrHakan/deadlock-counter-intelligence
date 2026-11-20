package source2

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
)

// ModelPart is one of a hero model's meshes as the importer offers it: replaced by the new
// model (and hidden) or kept as it is, like the gun.
type ModelPart struct {
	Name     string // without the _lod1/_lod2 suffix
	Vertices int
	Keep     bool // kept unless the user says otherwise: weapons
}

var lodSuffix = regexp.MustCompile(`_lod\d+$`)

func baseName(name string) string { return lodSuffix.ReplaceAllString(name, "") }

func (m *Model) lodMask(mesh *Mesh) int {
	masks := m.Data.Get("m_refLODGroupMasks")
	if masks == nil || mesh.MeshIndex >= len(masks.Items) {
		return 1
	}
	return intOf(masks.Items[mesh.MeshIndex])
}

func vertexCount(mesh *Mesh) int {
	n := 0
	for _, vb := range mesh.VertexBuffers {
		n += vb.Count
	}
	return n
}

// Parts lists the model's meshes as they appear at full detail.
func (m *Model) Parts() []ModelPart {
	var out []ModelPart
	for _, mesh := range m.Meshes {
		if m.lodMask(mesh)&1 == 0 {
			continue
		}
		lower := strings.ToLower(mesh.Name)
		out = append(out, ModelPart{
			Name:     baseName(mesh.Name),
			Vertices: vertexCount(mesh),
			Keep:     strings.Contains(lower, "gun") || strings.Contains(lower, "weapon"),
		})
	}
	return out
}

// ImportOptions says how a model is placed on a hero.
type ImportOptions struct {
	Keep      map[string]bool   // part names (see Parts) left as they are
	Scale     float64           // on top of fitting the model to the hero's height; 0 means 1
	Yaw       float64           // degrees to turn the model around the up axis
	Offset    [3]float64        // moved after fitting, in game units (Z is up)
	ZUp       bool              // the file is Z-up (Blender's axes) rather than Y-up
	Materials map[string]string // imported part name to the material it is drawn with
}

func (o ImportOptions) kept(name string) bool { return o.Keep[baseName(name)] }

// replaced are the full-detail meshes the new model stands in for.
func (m *Model) replaced(opts ImportOptions) []*Mesh {
	var out []*Mesh
	for _, mesh := range m.Meshes {
		if m.lodMask(mesh)&1 != 0 && !opts.kept(mesh.Name) {
			out = append(out, mesh)
		}
	}
	return out
}

// Bounds is the box around the meshes the new model replaces, in game units.
func (m *Model) Bounds(opts ImportOptions) (lo, hi [3]float64, err error) {
	lo = [3]float64{math.Inf(1), math.Inf(1), math.Inf(1)}
	hi = [3]float64{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	for _, mesh := range m.replaced(opts) {
		tris, err := mesh.Triangles()
		if err != nil {
			return lo, hi, err
		}
		for _, p := range tris.Positions {
			for k := 0; k < 3; k++ {
				lo[k], hi[k] = math.Min(lo[k], float64(p[k])), math.Max(hi[k], float64(p[k]))
			}
		}
	}
	if math.IsInf(lo[0], 1) {
		return lo, hi, errors.New("every part of the hero is kept, so there is nothing to replace")
	}
	return lo, hi, nil
}

// trimmedCentre is the middle of points across X and Y: the average of each, leaving out the
// outer 5% on either side, so a gun held out or a cape does not pull a model off its feet.
func trimmedCentre(points [][3]float64) [2]float64 {
	var out [2]float64
	if len(points) == 0 {
		return out
	}
	values := make([]float64, len(points))
	for k := 0; k < 2; k++ {
		for i, p := range points {
			values[i] = p[k]
		}
		sort.Float64s(values)
		cut := len(values) * 5 / 100
		middle := values[cut : len(values)-cut]
		sum := 0.0
		for _, v := range middle {
			sum += v
		}
		out[k] = sum / float64(len(middle))
	}
	return out
}

// Anchor is the point between the hero's feet that a model stands on, across X and Y: the pelvis
// bone when the skeleton has one, else the middle of the replaced parts.
func (m *Model) Anchor(opts ImportOptions) ([2]float64, error) {
	if skeleton, err := m.Skeleton(); err == nil {
		if p, ok := skeleton.position("pelvis"); ok {
			return [2]float64{p[0], p[1]}, nil
		}
	}
	var points [][3]float64
	for _, mesh := range m.replaced(opts) {
		tris, err := mesh.Triangles()
		if err != nil {
			return [2]float64{}, err
		}
		for _, p := range tris.Positions {
			points = append(points, vec64(p))
		}
	}
	return trimmedCentre(points), nil
}

// Fit turns an imported model into the hero's space: Z up, facing +X, as tall as the parts it
// replaces, its middle over the hero's pelvis and its feet on the hero's floor.
func (m *Model) Fit(in *ImportedMesh, opts ImportOptions) (*ImportedMesh, error) {
	lo, hi, err := m.Bounds(opts)
	if err != nil {
		return nil, err
	}
	sin, cos := math.Sincos(opts.Yaw * math.Pi / 180)
	turn := func(v [3]float32) [3]float64 {
		x, y, z := float64(v[0]), float64(v[1]), float64(v[2])
		if opts.ZUp {
			x, y = -y, x // Blender's front faces -Y
		} else {
			x, y, z = z, x, y // glTF's front faces +Z, up is +Y
		}
		return [3]float64{x*cos - y*sin, x*sin + y*cos, z}
	}
	out := &ImportedMesh{Parts: in.Parts, UVs: in.UVs, Joints: in.Joints, JointNames: in.JointNames, JointParents: in.JointParents}
	low := [3]float64{math.Inf(1), math.Inf(1), math.Inf(1)}
	high := [3]float64{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	turned := make([][3]float64, len(in.Positions))
	for i, p := range in.Positions {
		turned[i] = turn(p)
		for k := 0; k < 3; k++ {
			low[k], high[k] = math.Min(low[k], turned[i][k]), math.Max(high[k], turned[i][k])
		}
	}
	height := high[2] - low[2]
	if height <= 0 {
		return nil, errors.New("the model is flat: it has no height")
	}
	scale := (hi[2] - lo[2]) / height
	if opts.Scale > 0 {
		scale *= opts.Scale
	}
	middle := trimmedCentre(turned)
	centre := [3]float64{middle[0], middle[1], low[2]}
	anchor, err := m.Anchor(opts)
	if err != nil {
		return nil, err
	}
	target := [3]float64{anchor[0], anchor[1], lo[2]}
	out.Positions = make([][3]float32, len(turned))
	for i, p := range turned {
		for k := 0; k < 3; k++ {
			out.Positions[i][k] = float32((p[k]-centre[k])*scale + target[k] + opts.Offset[k])
		}
	}
	out.Normals = make([][3]float32, len(in.Normals))
	for i, n := range in.Normals {
		t := normalize(turn(n))
		out.Normals[i] = [3]float32{float32(t[0]), float32(t[1]), float32(t[2])}
	}
	return out, nil
}

// BoneWeight is a bone, by name, and how much a vertex follows it.
type BoneWeight struct {
	Bone   string
	Weight float32
}

type pointGrid struct {
	cell  float64
	cells map[[3]int][]int
	pts   [][3]float32
}

type gridHit struct {
	index    int
	distance float64
}

func newPointGrid(cell float64) *pointGrid {
	return &pointGrid{cell: cell, cells: map[[3]int][]int{}}
}

func (g *pointGrid) key(p [3]float32) [3]int {
	return [3]int{int(math.Floor(float64(p[0]) / g.cell)), int(math.Floor(float64(p[1]) / g.cell)), int(math.Floor(float64(p[2]) / g.cell))}
}

func (g *pointGrid) add(index int, p [3]float32) {
	for len(g.pts) <= index {
		g.pts = append(g.pts, [3]float32{})
	}
	g.pts[index] = p
	k := g.key(p)
	g.cells[k] = append(g.cells[k], index)
}

// nearest finds up to n points closest to p, searching outward ring by ring and keeping only the
// best n seen so far.
func (g *pointGrid) nearest(p [3]float32, n int) []gridHit {
	center := g.key(p)
	best := make([]gridHit, 0, n) // ordered by squared distance until the end
	consider := func(i int) {
		q := g.pts[i]
		d := sq(p[0]-q[0]) + sq(p[1]-q[1]) + sq(p[2]-q[2])
		if len(best) == n && d >= best[n-1].distance {
			return
		}
		if len(best) < n {
			best = append(best, gridHit{})
		}
		at := len(best) - 1
		for at > 0 && best[at-1].distance > d {
			best[at] = best[at-1]
			at--
		}
		best[at] = gridHit{i, d}
	}
	for ring := 0; ring < 64; ring++ {
		for dx := -ring; dx <= ring; dx++ {
			for dy := -ring; dy <= ring; dy++ {
				edge := abs(dx) == ring || abs(dy) == ring
				for dz := -ring; dz <= ring; dz++ {
					if !edge && abs(dz) != ring {
						dz = ring - 1 // inside the shell: jump to its far face
						continue
					}
					for _, i := range g.cells[[3]int{center[0] + dx, center[1] + dy, center[2] + dz}] {
						consider(i)
					}
				}
			}
		}
		// Every point within ring cells has been seen once n points lie within that distance.
		if reach := float64(ring) * g.cell; len(best) == n && best[n-1].distance <= reach*reach {
			break
		}
	}
	for k := range best {
		best[k].distance = math.Sqrt(best[k].distance)
	}
	return best
}

func abs(i int) int {
	if i < 0 {
		return -i
	}
	return i
}

func sq(f float32) float64 { return float64(f) * float64(f) }

// --- writing ---

func setNumber(v *Value, key string, n int) {
	field := v.Get(key)
	if field == nil {
		v.Set(key, NewInt32(int32(n)))
		return
	}
	switch field.Kind {
	case Uint16, Uint32, Uint64:
		field.U, field.I = uint64(n), 0
	case Float32, Float64:
		field.F = float64(n)
	case Bool:
		field.B = n != 0
	default:
		if field.Kind == Null {
			field.Kind = Int32
		}
		field.I = int64(n)
	}
}

func setBool(v *Value, key string, b bool) {
	if field := v.Get(key); field != nil {
		switch field.Kind {
		case Bool:
			field.B = b
		default:
			n := 0
			if b {
				n = 1
			}
			setNumber(v, key, n)
		}
	}
}

func cloneValue(v *Value) *Value {
	if v == nil {
		return nil
	}
	c := *v
	c.Items = nil
	for _, item := range v.Items {
		c.Items = append(c.Items, cloneValue(item))
	}
	c.Keys = append([]string(nil), v.Keys...)
	c.Fields = nil
	for _, f := range v.Fields {
		c.Fields = append(c.Fields, cloneValue(f))
	}
	return &c
}

// boneIndex maps bone names to a mesh's own bone numbers. A bone the mesh lacks falls back to its
// nearest ancestor the mesh has, through the model's skeleton.
func (m *Model) boneIndex(mesh *Mesh) func(string) int {
	own := map[string]int{}
	for i, name := range mesh.Bones {
		own[name] = i
	}
	parents := map[string]string{}
	if skeleton := m.Data.Get("m_modelSkeleton"); skeleton != nil {
		names, links := skeleton.Get("m_boneName"), skeleton.Get("m_nParent")
		if names != nil && links != nil {
			for i, n := range names.Items {
				if i < len(links.Items) {
					if p := intOf(links.Items[i]); p >= 0 && p < len(names.Items) {
						parents[n.S] = names.Items[p].S
					}
				}
			}
		}
	}
	return func(name string) int {
		for depth := 0; depth < 256 && name != ""; depth++ {
			if i, ok := own[name]; ok {
				return i
			}
			name = parents[name]
		}
		return 0
	}
}

// Replace writes the model with the fitted, rigged mesh in place of the parts it replaces: the
// mesh is drawn by the biggest replaced part at every level of detail and the other replaced
// parts are emptied. Skeleton, animations, hitboxes, physics and attachments stay the hero's.
func (m *Model) Replace(raw []byte, fitted *ImportedMesh, skin [][]BoneWeight, opts ImportOptions) ([]byte, error) {
	fresh, err := LoadModel(raw) // edit a copy so the caller's model stays as it was
	if err != nil {
		return nil, err
	}
	targets := map[*Mesh]bool{}
	covered := 0
	for bit := 0; bit < 8; bit++ {
		if covered&(1<<bit) != 0 {
			continue
		}
		var best *Mesh
		for _, mesh := range fresh.Meshes {
			if fresh.lodMask(mesh)&(1<<bit) == 0 || opts.kept(mesh.Name) || len(mesh.VertexBuffers) == 0 {
				continue
			}
			vb := mesh.VertexBuffers[0]
			if vb.Attribute("position", 0) == nil || vb.Attribute("blendindices", 0) == nil || vb.Attribute("normal", 0) == nil {
				continue
			}
			if best == nil || vertexCount(mesh) > vertexCount(best) {
				best = mesh
			}
		}
		if best != nil {
			targets[best] = true
			covered |= fresh.lodMask(best)
		}
	}
	if len(targets) == 0 {
		return nil, errors.New("none of the replaced parts can hold a skinned mesh")
	}
	blocks := make([]Block, len(fresh.Resource.Blocks))
	copy(blocks, fresh.Resource.Blocks)
	setBlock := func(index int, data []byte) error {
		if index < 0 || index >= len(blocks) {
			return fmt.Errorf("block %d is missing", index)
		}
		blocks[index].Data = data
		return nil
	}
	plain := func(desc *Value, count, size int) {
		setNumber(desc, "m_nElementCount", count)
		setNumber(desc, "m_nElementSizeInBytes", size)
		setBool(desc, "m_bMeshoptCompressed", false)
		setBool(desc, "m_bMeshoptIndexSequence", false)
		setBool(desc, "m_bCompressedZSTD", false)
		setBool(desc, "m_bCompressedZ3", false)
	}
	empty := func(mesh *Mesh, from int) error {
		for _, vb := range mesh.VertexBuffers[min(from, len(mesh.VertexBuffers)):] {
			plain(vb.Desc, 3, vb.Stride)
			if err := setBlock(vb.Block, make([]byte, 3*vb.Stride)); err != nil {
				return err
			}
		}
		for _, ib := range mesh.IndexBuffers[min(from, len(mesh.IndexBuffers)):] {
			plain(ib.Desc, 3, 2)
			if err := setBlock(ib.Block, make([]byte, 6)); err != nil {
				return err
			}
		}
		return nil
	}
	for _, mesh := range fresh.Meshes {
		if opts.kept(mesh.Name) {
			continue
		}
		setNumber(mesh.Desc, "m_nMorphBlock", -1)
		objects := mesh.Render.Get("m_sceneObjects")
		if !targets[mesh] {
			if err := empty(mesh, 0); err != nil {
				return nil, err
			}
			if objects != nil {
				for _, object := range objects.Items {
					for _, call := range object.Get("m_drawCalls").Items {
						setNumber(call, "m_nBaseVertex", 0)
						setNumber(call, "m_nVertexCount", 3)
						setNumber(call, "m_nStartIndex", 0)
						setNumber(call, "m_nIndexCount", 3)
						for _, h := range call.Get("m_vertexBuffers").Items {
							setNumber(h, "m_hBuffer", min(intOf(h.Get("m_hBuffer")), len(mesh.VertexBuffers)-1))
						}
					}
				}
			}
		} else {
			if err := fresh.writeTarget(mesh, fitted, skin, opts, setBlock, plain); err != nil {
				return nil, err
			}
			if err := empty(mesh, 1); err != nil {
				return nil, err
			}
		}
		format := [16]byte{}
		copy(format[:], blocks[mesh.DataBlock].Data[4:20])
		encoded, err := WriteKV3(mesh.Render, format)
		if err != nil {
			return nil, err
		}
		blocks[mesh.DataBlock].Data = encoded
	}
	ctrlIndex := -1
	for i, b := range blocks {
		if b.Type == "CTRL" {
			ctrlIndex = i
		}
	}
	format := [16]byte{}
	copy(format[:], blocks[ctrlIndex].Data[4:20])
	ctrl, err := WriteKV3(fresh.Ctrl, format)
	if err != nil {
		return nil, err
	}
	blocks[ctrlIndex].Data = ctrl
	if err := fresh.addReferences(blocks, opts); err != nil {
		return nil, err
	}
	out := &Resource{Version: fresh.Resource.Version, Blocks: blocks, Streaming: fresh.Resource.Streaming}
	return out.Encode(), nil
}

func (m *Model) writeTarget(mesh *Mesh, fitted *ImportedMesh, skin [][]BoneWeight, opts ImportOptions,
	setBlock func(int, []byte) error, plain func(*Value, int, int)) error {
	vb := mesh.VertexBuffers[0]
	ib := mesh.IndexBuffers[0]
	index := m.boneIndex(mesh)
	data := VertexData{Positions: fitted.Positions, Normals: fitted.Normals, UVs: fitted.UVs, Skin: make([][]Influence, len(skin))}
	for i, bones := range skin {
		for _, b := range bones {
			data.Skin[i] = append(data.Skin[i], Influence{index(b.Bone), b.Weight})
		}
	}
	vertices, err := vb.Encode(data)
	if err != nil {
		return fmt.Errorf("%s: %v", mesh.Name, err)
	}
	size := 2
	if len(fitted.Positions) > 65535 {
		size = 4
	}
	var indices []byte
	type rangeOf struct{ start, count int }
	var ranges []rangeOf
	for _, part := range fitted.Parts {
		ranges = append(ranges, rangeOf{len(indices) / size, len(part.Indices)})
		for _, i := range part.Indices {
			if size == 2 {
				indices = append(indices, byte(i), byte(i>>8))
			} else {
				indices = append(indices, byte(i), byte(i>>8), byte(i>>16), byte(i>>24))
			}
		}
	}
	plain(vb.Desc, len(fitted.Positions), vb.Stride)
	plain(ib.Desc, len(indices)/size, size)
	if err := setBlock(vb.Block, vertices); err != nil {
		return err
	}
	if err := setBlock(ib.Block, indices); err != nil {
		return err
	}
	objects := mesh.Render.Get("m_sceneObjects")
	if objects == nil || len(objects.Items) == 0 || len(mesh.DrawCalls) == 0 {
		return fmt.Errorf("%s has no draw calls to copy", mesh.Name)
	}
	template := mesh.DrawCalls[0].Desc
	for _, d := range mesh.DrawCalls {
		if d.VertexBuffer == 0 {
			template = d.Desc
			break
		}
	}
	var calls []*Value
	for p, part := range fitted.Parts {
		call := cloneValue(template)
		setNumber(call, "m_nBaseVertex", 0)
		setNumber(call, "m_nVertexCount", len(fitted.Positions))
		setNumber(call, "m_nStartIndex", ranges[p].start)
		setNumber(call, "m_nIndexCount", ranges[p].count)
		setNumber(call.Get("m_indexBuffer"), "m_hBuffer", 0)
		for _, h := range call.Get("m_vertexBuffers").Items {
			setNumber(h, "m_hBuffer", 0)
		}
		if material := opts.Materials[part.Name]; material != "" {
			call.Get("m_material").S = material
		}
		calls = append(calls, call)
	}
	lo := [3]float32{float32(math.Inf(1)), float32(math.Inf(1)), float32(math.Inf(1))}
	hi := [3]float32{float32(math.Inf(-1)), float32(math.Inf(-1)), float32(math.Inf(-1))}
	for _, p := range fitted.Positions {
		for k := 0; k < 3; k++ {
			lo[k], hi[k] = min(lo[k], p[k]), max(hi[k], p[k])
		}
	}
	for o, object := range objects.Items {
		if o == 0 {
			object.Get("m_drawCalls").Items = calls
			for key, bound := range map[string][3]float32{"m_vMinBounds": lo, "m_vMaxBounds": hi} {
				if v := object.Get(key); v != nil && len(v.Items) == 3 {
					for k := 0; k < 3; k++ {
						v.Items[k].F = float64(bound[k])
					}
				}
			}
			continue
		}
		for _, call := range object.Get("m_drawCalls").Items {
			setNumber(call, "m_nVertexCount", 3)
			setNumber(call, "m_nIndexCount", 3)
			setNumber(call, "m_nStartIndex", 0)
		}
	}
	return nil
}

// PartGeometry is a part's full-detail triangles, for previews.
type PartGeometry struct {
	ModelPart
	Positions [][3]float32
	Indices   []uint32
}

// Geometry returns every part at full detail, in the order Parts lists them.
func (m *Model) Geometry() ([]PartGeometry, error) {
	var out []PartGeometry
	parts := m.Parts()
	i := 0
	for _, mesh := range m.Meshes {
		if m.lodMask(mesh)&1 == 0 {
			continue
		}
		tris, err := mesh.Triangles()
		if err != nil {
			return nil, fmt.Errorf("%s: %v", mesh.Name, err)
		}
		out = append(out, PartGeometry{ModelPart: parts[i], Positions: tris.Positions, Indices: tris.Indices})
		i++
	}
	return out, nil
}
