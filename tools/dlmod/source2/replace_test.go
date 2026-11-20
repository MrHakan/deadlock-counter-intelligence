package source2

import (
	"bytes"
	"image"
	"image/png"
	"math"
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
