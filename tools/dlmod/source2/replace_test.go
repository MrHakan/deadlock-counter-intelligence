package source2

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"math"
	"strings"
	"testing"
)

// stickHero builds a small compiled hero model the way Valve's look: a skeleton with the standard
// bone names, a skinned body mesh made of points along each bone, and a gun mesh, each with its
// buffers in MVTX and MIDX blocks described by CTRL and an MDAT block.
type stickBone struct {
	name, parent string
	at           [3]float32 // world position; every bone is unrotated
}

var stickBones = []stickBone{
	{"pelvis", "", [3]float32{0, 0, 40}},
	{"spine_3", "pelvis", [3]float32{0, 0, 60}},
	{"head", "spine_3", [3]float32{0, 0, 72}},
	{"clavicle_L", "spine_3", [3]float32{0, 4, 64}},
	{"arm_upper_L", "clavicle_L", [3]float32{0, 8, 64}},
	{"arm_lower_L", "arm_upper_L", [3]float32{0, 18, 54}},
	{"hand_L", "arm_lower_L", [3]float32{0, 26, 46}},
	{"clavicle_R", "spine_3", [3]float32{0, -4, 64}},
	{"arm_upper_R", "clavicle_R", [3]float32{0, -8, 64}},
	{"arm_lower_R", "arm_upper_R", [3]float32{0, -18, 54}},
	{"hand_R", "arm_lower_R", [3]float32{0, -26, 46}},
	{"leg_upper_L", "pelvis", [3]float32{0, 5, 38}},
	{"leg_lower_L", "leg_upper_L", [3]float32{0, 5, 20}},
	{"ankle_L", "leg_lower_L", [3]float32{0, 5, 3}},
	{"leg_upper_R", "pelvis", [3]float32{0, -5, 38}},
	{"leg_lower_R", "leg_upper_R", [3]float32{0, -5, 20}},
	{"ankle_R", "leg_lower_R", [3]float32{0, -5, 3}},
	{"weapon_jnt", "hand_R", [3]float32{4, -26, 46}},
}

func stickBoneIndex(name string) int {
	for i, b := range stickBones {
		if b.name == name {
			return i
		}
	}
	return -1
}

// stickBody is points around each bone from its parent, weighted to the parent bone. Points at
// the ends of a segment sit where the next segment starts, with another bone.
func stickBody() (positions [][3]float32, bones []int, interior []bool) {
	for _, b := range stickBones {
		if b.parent == "" || b.name == "weapon_jnt" {
			continue
		}
		p := stickBones[stickBoneIndex(b.parent)].at
		for step := 0; step <= 4; step++ {
			t := float32(step) / 4
			c := [3]float32{p[0] + (b.at[0]-p[0])*t, p[1] + (b.at[1]-p[1])*t, p[2] + (b.at[2]-p[2])*t}
			for ring := 0; ring < 4; ring++ {
				angle := float64(ring) * math.Pi / 2
				positions = append(positions, [3]float32{c[0] + 1.5*float32(math.Cos(angle)), c[1], c[2] + 1.5*float32(math.Sin(angle))})
				bones = append(bones, stickBoneIndex(b.parent))
				interior = append(interior, step > 0 && step < 4)
			}
		}
	}
	return
}

func buildStickHero(t *testing.T) []byte {
	t.Helper()
	write := func(v *Value) []byte {
		data, err := WriteKV3(v, FormatGeneric)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	var names, parents, positions, rotations, scales []*Value
	for _, b := range stickBones {
		names = append(names, NewString(b.name))
		parent := stickBoneIndex(b.parent)
		parents = append(parents, NewInt32(int32(parent)))
		local := b.at
		if parent >= 0 {
			p := stickBones[parent].at
			local = [3]float32{b.at[0] - p[0], b.at[1] - p[1], b.at[2] - p[2]}
		}
		positions = append(positions, NewArray(NewFloat32(local[0]), NewFloat32(local[1]), NewFloat32(local[2])))
		rotations = append(rotations, NewArray(NewFloat32(0), NewFloat32(0), NewFloat32(0), NewFloat32(1)))
		scales = append(scales, NewFloat32(1))
	}
	data := NewObject().
		Set("m_modelSkeleton", NewObject().Set("m_boneName", NewArray(names...)).Set("m_nParent", NewArray(parents...)).
			Set("m_bonePosParent", NewArray(positions...)).Set("m_boneRotParent", NewArray(rotations...)).
			Set("m_boneScaleParent", NewArray(scales...))).
		Set("m_refLODGroupMasks", NewArray(NewUint32(1), NewUint32(1)))

	layout := func(fields ...[3]int) *Value { // name index, format, offset
		semantic := []string{"position", "normal", "texcoord", "blendindices", "blendweights"}
		var out []*Value
		for _, f := range fields {
			out = append(out, NewObject().Set("m_pSemanticName", NewString(semantic[f[0]])).
				Set("m_nSemanticIndex", NewInt32(0)).Set("m_Format", NewUint32(uint32(f[1]))).Set("m_nOffset", NewInt32(int32(f[2]))))
		}
		return NewArray(out...)
	}
	stride := 32
	body, bodyBones, _ := stickBody()
	var vertices []byte
	for i, p := range body {
		row := make([]byte, stride)
		for k := 0; k < 3; k++ {
			binary.LittleEndian.PutUint32(row[4*k:], math.Float32bits(p[k]))
		}
		binary.LittleEndian.PutUint32(row[12:], EncodeNormal([3]float32{1, 0, 0}))
		row[24] = byte(bodyBones[i])
		row[28] = 255
		vertices = append(vertices, row...)
	}
	var indices []byte
	for i := 0; i+2 < len(body); i += 3 {
		for _, v := range []int{i, i + 1, i + 2} {
			indices = binary.LittleEndian.AppendUint16(indices, uint16(v))
		}
	}
	gun := make([]byte, 3*stride)
	for v := 0; v < 3; v++ {
		gun[v*stride+24] = byte(stickBoneIndex("weapon_jnt"))
		gun[v*stride+28] = 255
	}
	gunIndices := []byte{0, 0, 1, 0, 2, 0}

	bufferDesc := func(block, count, size int, fields *Value) *Value {
		d := NewObject().Set("m_nBlockIndex", NewInt32(int32(block))).Set("m_nElementCount", NewUint32(uint32(count))).
			Set("m_nElementSizeInBytes", NewUint32(uint32(size))).Set("m_bMeshoptCompressed", NewBool(false)).
			Set("m_bMeshoptIndexSequence", NewBool(false)).Set("m_bCompressedZSTD", NewBool(false))
		if fields != nil {
			d.Set("m_inputLayoutFields", fields)
		}
		return d
	}
	fields := layout([3]int{0, FormatR32G32B32Float, 0}, [3]int{1, FormatR32Uint, 12}, [3]int{2, FormatR32G32Float, 16},
		[3]int{3, FormatR8G8B8A8Uint, 24}, [3]int{4, FormatR8G8B8A8Unorm, 28})
	mesh := func(name string, index, dataBlock, vb, ib, verts, idx int) *Value {
		return NewObject().Set("m_Name", NewString(name)).Set("m_nMeshIndex", NewInt32(int32(index))).
			Set("m_nDataBlock", NewInt32(int32(dataBlock))).Set("m_nMorphBlock", NewInt32(-1)).
			Set("m_vertexBuffers", NewArray(bufferDesc(vb, verts, stride, cloneValue(fields)))).
			Set("m_indexBuffers", NewArray(bufferDesc(ib, idx, 2, nil)))
	}
	ctrl := NewObject().Set("embedded_meshes", NewArray(
		mesh("stick_body", 0, 2, 0, 1, len(body), len(indices)/2),
		mesh("gun_model", 1, 5, 3, 4, 3, 3)))
	var boneList []*Value
	for _, b := range stickBones {
		boneList = append(boneList, NewObject().Set("m_boneName", NewString(b.name)))
	}
	render := func(material string, verts, idx int) *Value {
		call := NewObject().Set("m_nBaseVertex", NewInt32(0)).Set("m_nVertexCount", NewInt32(int32(verts))).
			Set("m_nStartIndex", NewInt32(0)).Set("m_nIndexCount", NewInt32(int32(idx))).
			Set("m_indexBuffer", NewObject().Set("m_hBuffer", NewInt32(0))).
			Set("m_vertexBuffers", NewArray(NewObject().Set("m_hBuffer", NewInt32(0)))).
			Set("m_material", resourceRef(material))
		object := NewObject().Set("m_vMinBounds", NewArray(NewFloat32(0), NewFloat32(0), NewFloat32(0))).
			Set("m_vMaxBounds", NewArray(NewFloat32(0), NewFloat32(0), NewFloat32(0))).Set("m_drawCalls", NewArray(call))
		return NewObject().Set("_class", NewString("CRenderMesh")).Set("m_sceneObjects", NewArray(object)).
			Set("m_skeleton", NewObject().Set("m_bones", NewArray(boneList...)))
	}
	resource := &Resource{Version: 1, Blocks: []Block{
		{"MVTX", vertices},
		{"MIDX", indices},
		{"MDAT", write(render("models/stick/body.vmat", len(body), len(indices)/2))},
		{"MVTX", gun},
		{"MIDX", gunIndices},
		{"MDAT", write(render("models/stick/gun.vmat", 3, 3))},
		{"PHYS", []byte("physics stays as it is")},
		{"CTRL", write(ctrl)},
		{"RERL", WriteReferences([]Reference{{ResourceID("models/stick/body.vmat"), "models/stick/body.vmat"}})},
		{"DATA", write(data)},
	}}
	return resource.Encode()
}

func TestImportModelOntoHero(t *testing.T) {
	raw := buildStickHero(t)
	model, err := LoadModel(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parts := model.Parts(); len(parts) != 2 || !parts[1].Keep || parts[0].Keep {
		t.Fatalf("the gun is kept and the body replaced by default: %+v", parts)
	}
	// The new model: the hero's own body points, as a Y-up file twice the size, with two
	// materials, one of them textured.
	body, _, _ := stickBody()
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
	result, err := ImportModel(raw, "models/stick/stick.vmdl", mesh, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"models/stick/stick.vmdl_c", "models/dlmod/stick/skin.vmat_c", "models/dlmod/stick/skin_color.vtex_c",
		"models/dlmod/stick/cloth_2.vmat_c", "models/dlmod/stick/cloth_2_color.vtex_c"} {
		if result.Files[name] == nil {
			t.Errorf("%s is missing from %v", name, keysOf(result.Files))
		}
	}
	if result.Pose.Arms != 0 || result.Pose.Legs != 0 {
		t.Errorf("a model in the hero's own pose needs no bending, got %+v", result.Pose)
	}
	built, err := LoadModel(result.Files["models/stick/stick.vmdl_c"])
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
	if !strings.Contains(strings.Join(refNames, ","), "models/dlmod/stick/skin.vmat") {
		t.Errorf("the new material is not referenced: %v", refNames)
	}
	newBody, gun := built.Meshes[0], built.Meshes[1]
	if len(newBody.DrawCalls) != 2 || newBody.DrawCalls[1].Material != "models/dlmod/stick/cloth_2.vmat" {
		t.Fatalf("one draw call per material: %+v", newBody.DrawCalls)
	}
	if gun.VertexBuffers[0].Count != 3 || gun.DrawCalls[0].Material != "models/stick/gun.vmat" {
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
	_, originalBones, interior := stickBody()
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
			if newBody.Bones[skin[i][0].Bone] != stickBones[originalBones[i]].name {
				wrong++
			}
		}
	}
	if wrong > checked/20 {
		t.Errorf("%d of %d vertices away from the joints follow another bone than the hero's point they copy", wrong, checked)
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
