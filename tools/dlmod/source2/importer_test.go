package source2

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"testing"
)

// A KV3 version 5 block built by hand: { a = 1, s = "x", f = [1.5, 2.5] as a packed float array
// in the auxiliary buffer }.
func TestReadKV3Version5(t *testing.T) {
	u32 := func(b []byte, v ...uint32) []byte {
		for _, x := range v {
			b = binary.LittleEndian.AppendUint32(b, x)
		}
		return b
	}
	buffer1 := []byte("a\x00s\x00x\x00f\x00")                               // strings, the 1-byte lane
	buffer1 = u32(buffer1, 4, math.Float32bits(1.5), math.Float32bits(2.5)) // string count, then the auxiliary floats
	buffer2 := u32(nil, 3)                                                  // the object's member count
	buffer2 = append(buffer2, 2, 0, 0, 0)                                   // the 1-byte lane: the array's length, then padding
	buffer2 = u32(buffer2, 0, 1, 1, 2, 3)                                   // a = 1, s = string 2, f's name
	buffer2 = append(buffer2, nodeObject, nodeInt32, nodeString, nodeArrayAuxiliary, nodeFloat)
	buffer2 = u32(buffer2, trailer)
	header := u32(nil, magic5)
	header = append(header, FormatGeneric[:]...)
	header = u32(header, 0) // uncompressed
	header = append(header, 0, 0, 0, 0)
	header = u32(header, 8, 3, 0, 5) // 1-byte, 4-byte and 8-byte lane sizes, types
	header = append(header, 1, 0, 1, 0)
	header = u32(header, uint32(len(buffer1)+len(buffer2)), uint32(len(buffer1)+len(buffer2)), 0, 0, 0, 0,
		uint32(len(buffer1)), uint32(len(buffer1)), uint32(len(buffer2)), uint32(len(buffer2)),
		1, 0, 5, 0, // buffer 2's lanes
		4, 1, 1, 2) // nodes, objects, arrays, array elements
	data := append(append(header, buffer1...), buffer2...)
	v, format, err := ReadKV3(data)
	if err != nil {
		t.Fatal(err)
	}
	if format != FormatGeneric || v.Get("a").I != 1 || v.Get("s").S != "x" {
		t.Fatalf("read %+v", v)
	}
	f := v.Get("f")
	if f == nil || len(f.Items) != 2 || f.Items[0].F != 1.5 || f.Items[1].F != 2.5 {
		t.Fatalf("the auxiliary array reads %+v", f)
	}
}

func TestLoadOBJ(t *testing.T) {
	files := map[string][]byte{
		"skin.mtl":     []byte("newmtl Body Paint\nKd 0.5 0.25 1\nmap_Kd -s 1 1 1 tex/body.png\n"),
		"tex/body.png": []byte("not decoded here"),
	}
	obj := []byte(`# a quad and a triangle
mtllib skin.mtl
v 0 0 0
v 1 0 0
v 1 1 0
v 0 1 0
vt 0 0
vt 1 1
usemtl Body Paint
f 1/1 2/2 3/2 4/1
f -4/1 -2/2 -1/2
`)
	mesh, err := LoadMesh("model.obj", obj, func(name string) ([]byte, error) {
		if data, ok := files[name]; ok {
			return data, nil
		}
		return nil, errMissing
	})
	if err != nil {
		t.Fatal(err)
	}
	if mesh.TriangleCount() != 3 || len(mesh.Parts) != 1 {
		t.Fatalf("got %d triangles in %d parts", mesh.TriangleCount(), len(mesh.Parts))
	}
	part := mesh.Parts[0]
	if part.Name != "Body Paint" || part.TextureName != "body.png" || part.Color != [4]float32{0.5, 0.25, 1, 1} {
		t.Fatalf("material %q texture %q colour %v", part.Name, part.TextureName, part.Color)
	}
	if mesh.UVs[0] != [2]float32{0, 1} {
		t.Errorf("OBJ texture coordinates start at the bottom: got %v", mesh.UVs[0])
	}
	if n := mesh.Normals[0]; math.Abs(float64(n[2])-1) > 1e-6 {
		t.Errorf("computed normal %v, want +Z", n)
	}
}

type missingError struct{}

func (missingError) Error() string { return "missing" }

var errMissing = missingError{}

func TestLoadGLB(t *testing.T) {
	// One triangle under a node moved by (10, 0, 0) and scaled 2x, positions as floats and
	// indices as 16-bit numbers in the binary chunk.
	var bin []byte
	for _, f := range []float32{0, 0, 0, 1, 0, 0, 0, 1, 0} {
		bin = binary.LittleEndian.AppendUint32(bin, math.Float32bits(f))
	}
	bin = append(bin, 0, 0, 1, 0, 2, 0, 0, 0) // indices, padded to 4 bytes
	doc := map[string]any{
		"asset":  map[string]any{"version": "2.0"},
		"scene":  0,
		"scenes": []any{map[string]any{"nodes": []int{0}}},
		"nodes": []any{
			map[string]any{"children": []int{1}, "translation": []float64{10, 0, 0}},
			map[string]any{"mesh": 0, "scale": []float64{2, 2, 2}},
		},
		"meshes": []any{map[string]any{"primitives": []any{map[string]any{
			"attributes": map[string]int{"POSITION": 0}, "indices": 1, "material": 0}}}},
		"materials": []any{map[string]any{"name": "Coat", "pbrMetallicRoughness": map[string]any{
			"baseColorFactor": []float64{1, 0, 0, 1}}}},
		"accessors": []any{
			map[string]any{"bufferView": 0, "componentType": 5126, "count": 3, "type": "VEC3"},
			map[string]any{"bufferView": 1, "componentType": 5123, "count": 3, "type": "SCALAR"},
		},
		"bufferViews": []any{
			map[string]any{"buffer": 0, "byteOffset": 0, "byteLength": 36},
			map[string]any{"buffer": 0, "byteOffset": 36, "byteLength": 6},
		},
		"buffers": []any{map[string]any{"byteLength": len(bin)}},
	}
	mesh, err := LoadMesh("model.glb", glbOf(doc, bin), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(mesh.Positions) != 3 || mesh.Positions[1] != [3]float32{12, 0, 0} || mesh.Positions[2] != [3]float32{10, 2, 0} {
		t.Fatalf("positions %v", mesh.Positions)
	}
	if len(mesh.Parts) != 1 || mesh.Parts[0].Name != "Coat" || mesh.Parts[0].Color != [4]float32{1, 0, 0, 1} {
		t.Fatalf("parts %+v", mesh.Parts)
	}
}

// A skinned triangle under an armature moved up 5 and scaled 2x, with a hip joint and a head
// joint above it: each vertex keeps the joint it follows, and each joint where it stands in the
// scene, the armature's scale included.
func TestLoadSkinnedGLB(t *testing.T) {
	var bin []byte
	floats := func(values ...float32) {
		for _, f := range values {
			bin = binary.LittleEndian.AppendUint32(bin, math.Float32bits(f))
		}
	}
	floats(0, 0, 0, 1, 0, 0, 0, 1, 0)         // positions
	bin = append(bin, 0, 0, 0, 0, 1, 0, 0, 0) // joints: vertex 0 the hip, 1 and 2 the head
	bin = append(bin, 1, 0, 0, 0)
	floats(1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0) // weights
	bin = append(bin, 0, 0, 1, 0, 2, 0, 0, 0)  // indices, padded
	doc := map[string]any{
		"asset":  map[string]any{"version": "2.0"},
		"scene":  0,
		"scenes": []any{map[string]any{"nodes": []int{0}}},
		"nodes": []any{
			map[string]any{"name": "Armature", "children": []int{1, 3}, "translation": []float64{0, 0, 5}, "scale": []float64{2, 2, 2}},
			map[string]any{"name": "Bone_000", "children": []int{2}, "translation": []float64{0, 1, 0}},
			map[string]any{"name": "Bone_001", "translation": []float64{0, 2, 0}},
			map[string]any{"name": "Body", "mesh": 0, "skin": 0},
		},
		"skins": []any{map[string]any{"joints": []int{1, 2}}},
		"meshes": []any{map[string]any{"primitives": []any{map[string]any{
			"attributes": map[string]int{"POSITION": 0, "JOINTS_0": 1, "WEIGHTS_0": 2}, "indices": 3}}}},
		"accessors": []any{
			map[string]any{"bufferView": 0, "componentType": 5126, "count": 3, "type": "VEC3"},
			map[string]any{"bufferView": 1, "componentType": 5121, "count": 3, "type": "VEC4"},
			map[string]any{"bufferView": 2, "componentType": 5126, "count": 3, "type": "VEC4"},
			map[string]any{"bufferView": 3, "componentType": 5123, "count": 3, "type": "SCALAR"},
		},
		"bufferViews": []any{
			map[string]any{"buffer": 0, "byteOffset": 0, "byteLength": 36},
			map[string]any{"buffer": 0, "byteOffset": 36, "byteLength": 12},
			map[string]any{"buffer": 0, "byteOffset": 48, "byteLength": 48},
			map[string]any{"buffer": 0, "byteOffset": 96, "byteLength": 6},
		},
		"buffers": []any{map[string]any{"byteLength": len(bin)}},
	}
	mesh, err := LoadMesh("model.glb", glbOf(doc, bin), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(mesh.Joints) != 3 || mesh.Joints[0][0].Joint != 1 || mesh.Joints[2][0].Joint != 2 {
		t.Fatalf("joints %v", mesh.Joints)
	}
	if mesh.JointNames[2] != "Bone_001" || mesh.JointParents[2] != 1 || mesh.JointParents[1] != 0 {
		t.Fatalf("skeleton %v %v", mesh.JointNames, mesh.JointParents)
	}
	if mesh.JointPositions[1] != [3]float32{0, 2, 5} || mesh.JointPositions[2] != [3]float32{0, 6, 5} {
		t.Fatalf("joint positions %v", mesh.JointPositions)
	}
}

// glbOf packs a glTF document and its binary chunk into a .glb file.
func glbOf(doc map[string]any, bin []byte) []byte {
	text, _ := json.Marshal(doc)
	for len(text)%4 != 0 {
		text = append(text, ' ')
	}
	glb := []byte("glTF")
	glb = binary.LittleEndian.AppendUint32(glb, 2)
	glb = binary.LittleEndian.AppendUint32(glb, uint32(12+8+len(text)+8+len(bin)))
	glb = binary.LittleEndian.AppendUint32(glb, uint32(len(text)))
	glb = append(append(glb, "JSON"...), text...)
	glb = binary.LittleEndian.AppendUint32(glb, uint32(len(bin)))
	return append(append(glb, "BIN\x00"...), bin...)
}

func TestCompileTexture(t *testing.T) {
	compiled, err := CompileTexture(SolidImage([4]float32{0, 1, 0, 1}), 2048, [4]float32{1, 1, 1, 1})
	if err != nil {
		t.Fatal(err)
	}
	r, err := DecodeResource(compiled)
	if err != nil {
		t.Fatal(err)
	}
	header := r.Block("DATA")
	if binary.LittleEndian.Uint16(header[20:]) != 4 || header[26] != textureDXT1 || header[27] != 1 || len(r.Streaming) != 8 {
		t.Fatalf("a 4x4 opaque texture is one BC1 block: header %x, %d bytes", header, len(r.Streaming))
	}
	if c := from565(binary.LittleEndian.Uint16(r.Streaming)); c[1] < 250 || c[0] > 5 {
		t.Errorf("the block's colour is %v, want green", c)
	}
	material, err := CompileMaterial("models/x/a.vmat", "models/x/a_color.vtex", 4, false)
	if err != nil {
		t.Fatal(err)
	}
	m, err := DecodeResource(material)
	if err != nil {
		t.Fatal(err)
	}
	data, _, err := ReadKV3(m.Block("DATA"))
	if err != nil || data.Get("m_shaderName").S != "pbr.vfx" {
		t.Fatalf("material data %v %v", data, err)
	}
	refs, _ := ReadReferences(m.Block("RERL"))
	if len(refs) != 6 { // the colour texture and five distinct stock textures
		t.Errorf("the material references %d textures: %v", len(refs), refs)
	}
}
