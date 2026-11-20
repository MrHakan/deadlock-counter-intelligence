package source2

import (
	"encoding/binary"
	"math"
)

// SampleHero is a small compiled hero model laid out as Valve's are: a skeleton with the standard
// bone names, a skinned body mesh made of points along each bone, and a gun mesh, each with its
// buffers in MVTX and MIDX blocks described by CTRL and an MDAT block. Tests import models onto it.
type sampleBone struct {
	name, parent string
	at           [3]float32 // world position; every bone is unrotated
}

var sampleBones = []sampleBone{
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

func sampleBoneIndex(name string) int {
	for i, b := range sampleBones {
		if b.name == name {
			return i
		}
	}
	return -1
}

// sampleBody is points around each bone from its parent, weighted to the parent bone. Points at
// the ends of a segment sit where the next segment starts, with another bone.
func sampleBody() (positions [][3]float32, bones []int, interior []bool) {
	for _, b := range sampleBones {
		if b.parent == "" || b.name == "weapon_jnt" {
			continue
		}
		p := sampleBones[sampleBoneIndex(b.parent)].at
		for step := 0; step <= 4; step++ {
			t := float32(step) / 4
			c := [3]float32{p[0] + (b.at[0]-p[0])*t, p[1] + (b.at[1]-p[1])*t, p[2] + (b.at[2]-p[2])*t}
			for ring := 0; ring < 4; ring++ {
				angle := float64(ring) * math.Pi / 2
				positions = append(positions, [3]float32{c[0] + 1.5*float32(math.Cos(angle)), c[1], c[2] + 1.5*float32(math.Sin(angle))})
				bones = append(bones, sampleBoneIndex(b.parent))
				interior = append(interior, step > 0 && step < 4)
			}
		}
	}
	return
}

// SampleInterfaceImage is a compiled texture laid out as the game's interface images (hero cards,
// portraits, ability icons) are: uncompressed BGRA with mips levels, here all black.
func SampleInterfaceImage(w, h, mips int) []byte {
	header := make([]byte, 40)
	binary.LittleEndian.PutUint16(header, 1)
	binary.LittleEndian.PutUint16(header[2:], 8)
	binary.LittleEndian.PutUint16(header[20:], uint16(w))
	binary.LittleEndian.PutUint16(header[22:], uint16(h))
	binary.LittleEndian.PutUint16(header[24:], 1)
	header[26], header[27] = textureBGRA8888, byte(mips)
	var streaming []byte
	sizes := mipSizes(w, h, mips)
	for i := len(sizes) - 1; i >= 0; i-- {
		streaming = append(streaming, make([]byte, sizes[i][0]*sizes[i][1]*4)...)
	}
	editInfo, _ := WriteKV3(NewObject().Set("m_name", NewString("card")), FormatGeneric)
	return (&Resource{Version: 1, Blocks: []Block{{"RED2", editInfo}, {"DATA", header}}, Streaming: streaming}).Encode()
}

func SampleHero() []byte {
	write := func(v *Value) []byte {
		data, _ := WriteKV3(v, FormatGeneric) // these values always encode
		return data
	}
	var names, parents, positions, rotations, scales []*Value
	for _, b := range sampleBones {
		names = append(names, NewString(b.name))
		parent := sampleBoneIndex(b.parent)
		parents = append(parents, NewInt32(int32(parent)))
		local := b.at
		if parent >= 0 {
			p := sampleBones[parent].at
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
		semantic := []string{"POSITION", "NORMAL", "TEXCOORD", "BLENDINDICES", "blendweight"} // as the game spells them
		var out []*Value
		for _, f := range fields {
			out = append(out, NewObject().Set("m_pSemanticName", NewString(semantic[f[0]])).
				Set("m_nSemanticIndex", NewInt32(0)).Set("m_Format", NewUint32(uint32(f[1]))).Set("m_nOffset", NewInt32(int32(f[2]))))
		}
		return NewArray(out...)
	}
	stride := 32
	body, bodyBones, _ := sampleBody()
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
		gun[v*stride+24] = byte(sampleBoneIndex("weapon_jnt"))
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
		mesh("sample_body", 0, 2, 0, 1, len(body), len(indices)/2),
		mesh("gun_model", 1, 5, 3, 4, 3, 3)))
	var boneList []*Value
	for _, b := range sampleBones {
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
		{"MDAT", write(render("models/sample/body.vmat", len(body), len(indices)/2))},
		{"MVTX", gun},
		{"MIDX", gunIndices},
		{"MDAT", write(render("models/sample/gun.vmat", 3, 3))},
		{"PHYS", []byte("physics stays as it is")},
		{"CTRL", write(ctrl)},
		{"RERL", WriteReferences([]Reference{{ResourceID("models/sample/body.vmat"), "models/sample/body.vmat"}})},
		{"DATA", write(data)},
	}}
	return resource.Encode()
}
