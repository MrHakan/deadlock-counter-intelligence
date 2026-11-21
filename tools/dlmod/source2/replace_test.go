package source2

import (
	"bytes"
	"image"
	"image/png"
	"math"
	"math/rand"
	"sort"
	"strings"
	"testing"
)

func TestImportModelOntoHero(t *testing.T) {
	raw := SampleHero()
	model, err := LoadModel(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parts := model.Parts(); len(parts) != 2 || !parts[1].Keep || parts[0].Keep {
		t.Fatalf("the gun is kept and the body replaced by default: %+v", parts)
	}
	// The new model: the hero's own body points, as a Y-up file twice the size, with two
	// materials, one of them textured.
	body, _, _ := sampleBody()
	mesh := &ImportedMesh{YUp: true}
	for _, p := range body {
		mesh.Positions = append(mesh.Positions, [3]float32{p[1] * 2, p[2] * 2, p[0] * 2})
		mesh.UVs = append(mesh.UVs, [2]float32{0.5, 0.5})
	}
	var first, second []uint32
	for i := 0; i+2 < len(body); i += 3 {
		target := &first
		if i > len(body)/2 {
			target = &second
		}
		*target = append(*target, uint32(i), uint32(i+1), uint32(i+2))
	}
	texture := pngOf(t, SolidImage([4]float32{1, 0, 0, 1}))
	mesh.Parts = []ImportedPart{
		{Name: "Skin", Indices: first, Color: [4]float32{1, 1, 1, 1}, Texture: texture},
		{Name: "Cloth #2", Indices: second, Color: [4]float32{0, 0.5, 1, 1}},
	}
	mesh.Normals = computeNormals(mesh)

	opts := ImportOptions{Keep: map[string]bool{"gun_model": true}}
	result, err := ImportModel(raw, "models/sample/sample.vmdl", mesh, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"models/sample/sample.vmdl_c", "models/dlmod/sample/skin.vmat_c", "models/dlmod/sample/skin_color.vtex_c",
		"models/dlmod/sample/cloth_2.vmat_c", "models/dlmod/sample/cloth_2_color.vtex_c"} {
		if result.Files[name] == nil {
			t.Errorf("%s is missing from %v", name, keysOf(result.Files))
		}
	}
	if result.Pose.Arms != 0 || result.Pose.Legs != 0 {
		t.Errorf("a model in the hero's own pose needs no bending, got %+v", result.Pose)
	}
	built, err := LoadModel(result.Files["models/sample/sample.vmdl_c"])
	if err != nil {
		t.Fatal(err)
	}
	if string(built.Resource.Block("PHYS")) != "physics stays as it is" {
		t.Error("the physics block changed")
	}
	refs, _ := ReadReferences(built.Resource.Block("RERL"))
	var refNames []string
	for _, r := range refs {
		refNames = append(refNames, r.Name)
		if r.ID != ResourceID(r.Name) {
			t.Errorf("reference %s has the wrong id", r.Name)
		}
	}
	if !strings.Contains(strings.Join(refNames, ","), "models/dlmod/sample/skin.vmat") {
		t.Errorf("the new material is not referenced: %v", refNames)
	}
	newBody, gun := built.Meshes[0], built.Meshes[1]
	if len(newBody.DrawCalls) != 2 || newBody.DrawCalls[1].Material != "models/dlmod/sample/cloth_2.vmat" {
		t.Fatalf("one draw call per material: %+v", newBody.DrawCalls)
	}
	if gun.VertexBuffers[0].Count != 3 || gun.DrawCalls[0].Material != "models/sample/gun.vmat" {
		t.Error("the kept gun changed")
	}
	vb := newBody.VertexBuffers[0]
	if vb.Count != len(body) {
		t.Fatalf("the body has %d vertices, want %d", vb.Count, len(body))
	}
	positions, _ := vb.Floats(vb.Attribute("position", 0))
	skin, err := vb.Skin()
	if err != nil {
		t.Fatal(err)
	}
	_, originalBones, interior := sampleBody()
	wrong, checked := 0, 0
	for i, p := range positions {
		// Scaled back to the hero's height and standing where it stood.
		if d := math.Abs(float64(p[2] - body[i][2])); d > 0.6 {
			t.Fatalf("vertex %d sits at height %v, the hero's point at %v", i, p[2], body[i][2])
		}
		total := float32(0)
		for _, in := range skin[i] {
			total += in.Weight
		}
		if math.Abs(float64(total-1)) > 0.01 {
			t.Fatalf("vertex %d weights add up to %v", i, total)
		}
		if interior[i] {
			checked++
			if newBody.Bones[skin[i][0].Bone] != sampleBones[originalBones[i]].name {
				wrong++
			}
		}
	}
	if wrong > checked/20 {
		t.Errorf("%d of %d vertices away from the joints follow another bone than the hero's point they copy", wrong, checked)
	}
}

// The grid finds the same nearest points as checking them all, for points among its own and far
// outside them (where it used to walk dozens of rings of empty cells).
func TestPointGridNearest(t *testing.T) {
	random := rand.New(rand.NewSource(1))
	grid := newPointGrid(2)
	var pts [][3]float32
	for i := 0; i < 2000; i++ {
		p := [3]float32{random.Float32()*20 - 10, random.Float32() * 8, random.Float32() * 100}
		if i%2 == 0 { // every other index, as the per-limb grids hold only some samples
			grid.add(i, p)
			pts = append(pts, p)
		}
	}
	for q := 0; q < 300; q++ {
		spread := float32(30)
		if q%3 == 0 {
			spread = 300
		}
		p := [3]float32{(random.Float32()*2 - 1) * spread, (random.Float32()*2 - 1) * spread, random.Float32() * 100}
		hits := grid.nearest(p, 6)
		var all []float64
		for _, o := range pts {
			all = append(all, math.Sqrt(sq(p[0]-o[0])+sq(p[1]-o[1])+sq(p[2]-o[2])))
		}
		sort.Float64s(all)
		if all[5] > 63*2 {
			continue // beyond the 64 rings searched, like before
		}
		if len(hits) != 6 {
			t.Fatalf("%v: %d hits", p, len(hits))
		}
		for k, h := range hits {
			if math.Abs(h.distance-all[k]) > 1e-4 {
				t.Fatalf("%v: hit %d at %v, nearest is %v", p, k, h.distance, all[k])
			}
		}
	}
}

// A point of a cape following both arms, with the arms lowered nearly 90 degrees: undoing the
// mixed turn would throw it hundreds of units away, undoing each arm's turn keeps it in place.
// A point following one limb comes back exactly.
func TestUnbendWhereOppositeArmsMix(t *testing.T) {
	var matrices, inverses [limbCount]mat34
	for limb := range matrices {
		matrices[limb] = identity34()
	}
	matrices[limbArmL] = rotationAbout([3]float64{1, 0, 0}, 88, [3]float64{0, 15, 60})
	matrices[limbArmR] = rotationAbout([3]float64{1, 0, 0}, -88, [3]float64{0, -15, 60})
	for limb := range inverses {
		inverses[limb], _ = matrices[limb].inverse()
	}
	var both [limbCount]float64
	both[limbArmL], both[limbArmR] = 0.5, 0.5
	cape := [3]float64{-5, 0, 40}
	got, _ := unbend(both, inverses, cape, [3]float64{-1, 0, 0})
	if d := distance64(got, cape); d > 60 {
		t.Errorf("the cape point moved %.0f units", d)
	}
	var left [limbCount]float64
	left[limbArmL] = 1
	hand := [3]float64{0, 45, 60}
	posed := matrices[limbArmL].apply(hand)
	back, n := unbend(left, inverses, posed, matrices[limbArmL].rotate([3]float64{0, 0, 1}))
	if distance64(back, hand) > 1e-6 || math.Abs(n[2]-1) > 1e-6 {
		t.Errorf("a hand bent and unbent ends at %v facing %v", back, n)
	}
}

func distance64(a, b [3]float64) float64 {
	return math.Sqrt((a[0]-b[0])*(a[0]-b[0]) + (a[1]-b[1])*(a[1]-b[1]) + (a[2]-b[2])*(a[2]-b[2]))
}

// Deadlock's models spell attributes POSITION and blendweight, other compiled models position and
// blendweights; both must read and write the same.
func TestAttributeSpellings(t *testing.T) {
	for _, names := range [][4]string{
		{"POSITION", "NORMAL", "BLENDINDICES", "blendweight"},
		{"position", "normal", "blendindices", "blendweights"},
	} {
		vb := &VertexBuffer{Count: 1, Stride: 24, Attributes: []VertexAttribute{
			{Name: names[0], Format: FormatR32G32B32Float, Offset: 0},
			{Name: names[1], Format: FormatR32Uint, Offset: 12},
			{Name: names[2], Format: FormatR8G8B8A8Uint, Offset: 16},
			{Name: names[3], Format: FormatR8G8B8A8Unorm, Offset: 20},
		}}
		data, err := vb.Encode(VertexData{
			Positions: [][3]float32{{1, 2, 3}},
			Normals:   [][3]float32{{0, 0, 1}},
			Skin:      [][]Influence{{{Bone: 7, Weight: 0.75}, {Bone: 3, Weight: 0.25}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		vb.Data = data
		position := vb.Attribute("position", 0)
		if position == nil || vb.Attribute("blendweights", 0) == nil {
			t.Fatalf("%v: attributes not found", names)
		}
		positions, _ := vb.Floats(position)
		skin, _ := vb.Skin()
		if positions[0] != [4]float32{1, 2, 3, 0} || len(skin[0]) != 2 || skin[0][0].Bone != 7 || skin[0][1].Bone != 3 {
			t.Errorf("%v: wrote %v and %v", names, positions[0], skin[0])
		}
	}
}

func keysOf(m map[string][]byte) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func pngOf(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// A kept part can be moved at rest, out of a wider body; everything else about it stays.
func TestImportMovesKeptPart(t *testing.T) {
	raw := SampleHero()
	model, err := LoadModel(raw)
	if err != nil {
		t.Fatal(err)
	}
	var before [][4]float32
	for _, mesh := range model.Meshes {
		if mesh.Name == "gun_model" {
			vb := mesh.VertexBuffers[0]
			before, _ = vb.Floats(vb.Attribute("position", 0))
		}
	}
	body, _, _ := sampleBody()
	mesh := &ImportedMesh{YUp: true}
	for _, p := range body {
		mesh.Positions = append(mesh.Positions, [3]float32{p[1], p[2], p[0]})
		mesh.UVs = append(mesh.UVs, [2]float32{0.5, 0.5})
	}
	var tris []uint32
	for i := 0; i+2 < len(body); i += 3 {
		tris = append(tris, uint32(i), uint32(i+1), uint32(i+2))
	}
	mesh.Parts = []ImportedPart{{Name: "Skin", Indices: tris, Color: [4]float32{1, 1, 1, 1}}}
	mesh.Normals = computeNormals(mesh)
	shift := [3]float64{12, -3, 1.5}
	opts := ImportOptions{Keep: map[string]bool{"gun_model": true}, KeptShift: map[string][3]float64{"gun_model": shift}}
	result, err := ImportModel(raw, "models/sample/sample.vmdl", mesh, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	built, err := LoadModel(result.Files["models/sample/sample.vmdl_c"])
	if err != nil {
		t.Fatal(err)
	}
	gun := built.Meshes[1]
	vb := gun.VertexBuffers[0]
	after, err := vb.Floats(vb.Attribute("position", 0))
	if err != nil || len(after) != len(before) || len(before) == 0 {
		t.Fatalf("the gun has %d vertices, had %d (%v)", len(after), len(before), err)
	}
	for i := range after {
		for k := 0; k < 3; k++ {
			if math.Abs(float64(after[i][k]-before[i][k])-shift[k]) > 1e-4 {
				t.Fatalf("gun vertex %d moved %v -> %v, want %v", i, before[i], after[i], shift)
			}
		}
	}
	if gun.DrawCalls[0].Material != "models/sample/gun.vmat" {
		t.Error("the moved gun lost its material")
	}
}

// A bone no clip animates can be moved at rest with the part it carries.
func TestImportMovesBone(t *testing.T) {
	raw := SampleHero()
	model, err := LoadModel(raw)
	if err != nil {
		t.Fatal(err)
	}
	sk, err := model.Skeleton()
	if err != nil {
		t.Fatal(err)
	}
	name := sk.Names[len(sk.Names)-1]
	before, _ := sk.Position(name)
	body, _, _ := sampleBody()
	mesh := &ImportedMesh{YUp: true}
	for _, p := range body {
		mesh.Positions = append(mesh.Positions, [3]float32{p[1], p[2], p[0]})
		mesh.UVs = append(mesh.UVs, [2]float32{0.5, 0.5})
	}
	var tris []uint32
	for i := 0; i+2 < len(body); i += 3 {
		tris = append(tris, uint32(i), uint32(i+1), uint32(i+2))
	}
	mesh.Parts = []ImportedPart{{Name: "Skin", Indices: tris, Color: [4]float32{1, 1, 1, 1}}}
	mesh.Normals = computeNormals(mesh)
	shift := [3]float64{4, -2, 7}
	opts := ImportOptions{Keep: map[string]bool{"gun_model": true}, BoneShift: map[string][3]float64{name: shift}}
	result, err := ImportModel(raw, "models/sample/sample.vmdl", mesh, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	built, err := LoadModel(result.Files["models/sample/sample.vmdl_c"])
	if err != nil {
		t.Fatal(err)
	}
	moved, err := built.Skeleton()
	if err != nil {
		t.Fatal(err)
	}
	after, _ := moved.Position(name)
	for k := 0; k < 3; k++ {
		if math.Abs(after[k]-before[k]-shift[k]) > 1e-4 {
			t.Fatalf("bone %s moved from %v to %v, want %v", name, before, after, shift)
		}
	}
	for i, other := range sk.Names[:len(sk.Names)-1] {
		p, _ := moved.Position(other)
		q, _ := sk.Position(other)
		if p != q {
			t.Errorf("bone %d %s moved too", i, other)
		}
	}
}
