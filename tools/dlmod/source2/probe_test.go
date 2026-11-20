// Probes against real game files, skipped unless their environment variables are set. They are
// how the model importer was checked against Valve's own files:
//
//	PROBE_VPK=<pak_dir.vpk>                                  TestProbeModel lists a VPK's models and their blocks
//	PROBE_VPK=<vpk> PROBE_FILE=<path> PROBE_OUT=<file>       TestProbeExtract copies one file out
//	PROBE_MODEL=<vmdl_c> PROBE_PNG=<png>                     TestProbeRender draws a model from three sides
//	PROBE_MODEL=<vmdl_c>                                     TestProbeRoundTrip rewrites every KV3 block
//	PROBE_MODEL=<vmdl_c> PROBE_INPUT=<obj/glb> PROBE_PNG=<png>  TestProbeImport imports a model and draws it
//	PROBE_MODEL=<vmdl_c> PROBE_BUILT=<vmdl_c> PROBE_PNG=<png>   TestProbeBend bends a built model's limbs
//	PROBE_MODEL=<vmdl_c> PROBE_INPUT=<obj/glb> PROBE_OUT=<dir>  TestProbeFullImport writes pak99_dir.vpk
//	PROBE_INPUT=<glb> PROBE_OBJ=<obj> PROBE_TEXTURE=<png>    TestProbeWriteOBJ converts a model to OBJ
//	PROBE_IMAGE=<png> PROBE_PNG=<png>                        TestProbeTexture compresses and decodes an image
package source2

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestProbeModel(t *testing.T) {
	dir := os.Getenv("PROBE_VPK")
	if dir == "" {
		t.Skip()
	}
	index, err := OpenVPK(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for name := range index.Files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Println("file:", name, index.Files[name].Length)
	}
	for _, name := range names {
		if !strings.HasSuffix(name, ".vmdl_c") {
			continue
		}
		data, _ := index.Read(name)
		r, err := DecodeResource(data)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Println("==", name, "version", r.Version, "streaming", len(r.Streaming))
		for _, b := range r.Blocks {
			head := ""
			if len(b.Data) >= 4 {
				head = fmt.Sprintf("%x", b.Data[:4])
			}
			_, _, kvErr := ReadKV3(b.Data)
			fmt.Printf("   %s %8d bytes head=%s kv3=%v\n", b.Type, len(b.Data), head, kvErr == nil)
		}
	}
}

func TestProbeExtract(t *testing.T) {
	dir, name, out := os.Getenv("PROBE_VPK"), os.Getenv("PROBE_FILE"), os.Getenv("PROBE_OUT")
	if dir == "" || name == "" {
		t.Skip()
	}
	index, err := OpenVPK(dir)
	if err != nil {
		t.Fatal(err)
	}
	data, err := index.Read(name)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(out, data, 0o644)
}

func TestProbeRender(t *testing.T) {
	file, out := os.Getenv("PROBE_MODEL"), os.Getenv("PROBE_PNG")
	if file == "" || out == "" {
		t.Skip()
	}
	data, _ := os.ReadFile(file)
	model, err := LoadModel(data)
	if err != nil {
		t.Fatal(err)
	}
	var sets []Triangles
	for _, mesh := range model.Meshes {
		if strings.Contains(mesh.Name, "_lod") {
			continue
		}
		tris, err := mesh.Triangles()
		if err != nil {
			t.Fatal(err)
		}
		sets = append(sets, tris)
	}
	canvas := image.NewRGBA(image.Rect(0, 0, 1200, 800))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{color.RGBA{24, 26, 32, 255}}, image.Point{}, draw.Src)
	for i, yaw := range []float64{0, 90, 180} {
		view := RenderPreview(sets, 400, 800, yaw)
		draw.Draw(canvas, image.Rect(400*i, 0, 400*i+400, 800), view, image.Point{}, draw.Over)
	}
	f, _ := os.Create(out)
	defer f.Close()
	png.Encode(f, canvas)
}

func TestProbeRoundTrip(t *testing.T) {
	file := os.Getenv("PROBE_MODEL")
	if file == "" {
		t.Skip()
	}
	data, _ := os.ReadFile(file)
	r, _ := DecodeResource(data)
	for i, b := range r.Blocks {
		v, format, err := ReadKV3(b.Data)
		if err != nil {
			continue
		}
		out, err := WriteKV3(v, format)
		if err != nil {
			t.Fatalf("block %d %s: %v", i, b.Type, err)
		}
		back, _, err := ReadKV3(out)
		if err != nil {
			t.Fatalf("block %d %s reread: %v", i, b.Type, err)
		}
		if !reflect.DeepEqual(v, back) {
			t.Fatalf("block %d %s differs after a round trip", i, b.Type)
		}
	}
}

func TestProbeImport(t *testing.T) {
	file, input, out := os.Getenv("PROBE_MODEL"), os.Getenv("PROBE_INPUT"), os.Getenv("PROBE_PNG")
	if file == "" || input == "" {
		t.Skip()
	}
	raw, _ := os.ReadFile(file)
	model, err := LoadModel(raw)
	if err != nil {
		t.Fatal(err)
	}
	mesh, err := LoadMeshFile(input)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("imported", len(mesh.Positions), "vertices", mesh.TriangleCount(), "triangles", len(mesh.Parts), "parts")
	for _, p := range mesh.Parts {
		fmt.Println("  part", p.Name, len(p.Indices)/3, "triangles, texture", p.TextureName, len(p.Texture))
	}
	for _, p := range model.Parts() {
		fmt.Println("  hero part", p.Name, p.Vertices, p.Keep)
	}
	opts := ImportOptions{Keep: map[string]bool{"gun_model": true}}
	fitted, err := model.Fit(mesh, opts)
	if err != nil {
		t.Fatal(err)
	}
	rigged, err := model.AutoRig(fitted, opts, nil, 4)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("pose", rigged.Pose, "vertex 0 bones", rigged.Skin[0])
	built, err := model.Replace(raw, rigged.Mesh, rigged.Skin, opts)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(strings.TrimSuffix(out, ".png")+".vmdl_c", built, 0o644)
	back, err := LoadModel(built)
	if err != nil {
		t.Fatal(err)
	}
	var sets []Triangles
	for _, m := range back.Meshes {
		if back.lodMask(m)&1 == 0 {
			continue
		}
		tris, err := m.Triangles()
		if err != nil {
			t.Fatal(err)
		}
		sets = append(sets, tris)
	}
	canvas := image.NewRGBA(image.Rect(0, 0, 1200, 800))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{color.RGBA{24, 26, 32, 255}}, image.Point{}, draw.Src)
	for i, yaw := range []float64{0, 90, 180} {
		view := RenderPreview(sets, 400, 800, yaw)
		draw.Draw(canvas, image.Rect(400*i, 0, 400*i+400, 800), view, image.Point{}, draw.Over)
	}
	f, _ := os.Create(out)
	defer f.Close()
	png.Encode(f, canvas)
	fmt.Println("built", len(built), "bytes, was", len(raw))
}

func TestProbeBend(t *testing.T) {
	file, built, out := os.Getenv("PROBE_MODEL"), os.Getenv("PROBE_BUILT"), os.Getenv("PROBE_PNG")
	if file == "" || built == "" {
		t.Skip()
	}
	data, _ := os.ReadFile(built)
	model, err := LoadModel(data)
	if err != nil {
		t.Fatal(err)
	}
	sk, _ := model.Skeleton()
	limbs := sk.limbs()
	canvas := image.NewRGBA(image.Rect(0, 0, 1200, 800))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{color.RGBA{24, 26, 32, 255}}, image.Point{}, draw.Src)
	for col, pose := range []Pose{{Arms: -45}, {Arms: 30, Legs: 15}, {Arms: 60, Legs: -5}} {
		matrices := sk.limbMatrices(pose)
		var sets []Triangles
		for _, mesh := range model.Meshes {
			if model.lodMask(mesh)&1 == 0 {
				continue
			}
			tris, _ := mesh.Triangles()
			vb := mesh.VertexBuffers[0]
			if vb.Attribute("blendindices", 0) != nil && len(tris.Positions) == vb.Count {
				skin, _ := vb.Skin()
				for i, p := range tris.Positions {
					var follow [limbCount]float64
					for _, in := range skin[i] {
						follow[limbs[sk.index[mesh.Bones[in.Bone]]]] += float64(in.Weight)
					}
					q := blend(follow, matrices).apply(vec64(p))
					tris.Positions[i] = [3]float32{float32(q[0]), float32(q[1]), float32(q[2])}
				}
			}
			sets = append(sets, tris)
		}
		view := RenderPreview(sets, 400, 800, 0)
		draw.Draw(canvas, image.Rect(400*col, 0, 400*col+400, 800), view, image.Point{}, draw.Over)
	}
	f, _ := os.Create(out)
	defer f.Close()
	png.Encode(f, canvas)
}

func decodeBC1(data []byte, w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	bw := (w + 3) / 4
	for b := 0; b*8 < len(data); b++ {
		block := data[b*8:]
		c0, c1 := binary.LittleEndian.Uint16(block), binary.LittleEndian.Uint16(block[2:])
		p0, p1 := from565(c0), from565(c1)
		pal := [4][3]float64{p0, p1}
		for k := 0; k < 3; k++ {
			if c0 > c1 {
				pal[2][k] = (2*p0[k] + p1[k]) / 3
				pal[3][k] = (p0[k] + 2*p1[k]) / 3
			} else {
				pal[2][k] = (p0[k] + p1[k]) / 2
			}
		}
		idx := binary.LittleEndian.Uint32(block[4:])
		for i := 0; i < 16; i++ {
			x, y := (b%bw)*4+i%4, (b/bw)*4+i/4
			c := pal[idx>>(2*i)&3]
			img.SetNRGBA(x, y, color.NRGBA{uint8(c[0]), uint8(c[1]), uint8(c[2]), 255})
		}
	}
	return img
}

func TestProbeTexture(t *testing.T) {
	in, out := os.Getenv("PROBE_IMAGE"), os.Getenv("PROBE_PNG")
	if in == "" {
		t.Skip()
	}
	data, _ := os.ReadFile(in)
	img, err := DecodeImage(data)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := CompileTexture(img, 512, [4]float32{1, 1, 1, 1})
	if err != nil {
		t.Fatal(err)
	}
	r, _ := DecodeResource(compiled)
	header := r.Block("DATA")
	w, h := int(binary.LittleEndian.Uint16(header[20:])), int(binary.LittleEndian.Uint16(header[22:]))
	fmt.Println("texture", w, "x", h, "format", header[26], "mips", header[27], "streaming", len(r.Streaming))
	mip0 := r.Streaming[len(r.Streaming)-w*h/2:]
	f, _ := os.Create(out)
	defer f.Close()
	png.Encode(f, decodeBC1(mip0, w, h))
}

func TestProbeFullImport(t *testing.T) {
	file, input, outDir := os.Getenv("PROBE_MODEL"), os.Getenv("PROBE_INPUT"), os.Getenv("PROBE_OUT")
	if file == "" || input == "" {
		t.Skip()
	}
	raw, _ := os.ReadFile(file)
	mesh, err := LoadMeshFile(input)
	if err != nil {
		t.Fatal(err)
	}
	result, err := ImportModel(raw, "models/heroes_wip/wraith/wraith.vmdl", mesh, ImportOptions{Keep: map[string]bool{"gun_model": true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("pose", result.Pose, "vertices", result.Vertices, "notes", result.Notes)
	var names []string
	for name := range result.Files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		data := result.Files[name]
		r, err := DecodeResource(data)
		if err != nil {
			t.Fatal(name, err)
		}
		var kinds []string
		for _, b := range r.Blocks {
			if b.Type == "MVTX" || b.Type == "MIDX" || b.Type == "MDAT" {
				continue
			}
			ok := "raw"
			if _, _, err := ReadKV3(b.Data); err == nil {
				ok = "kv3"
			}
			kinds = append(kinds, b.Type+":"+ok)
		}
		if refs, err := ReadReferences(r.Block("RERL")); err == nil && strings.HasSuffix(name, ".vmat_c") {
			fmt.Println("   refs", len(refs))
		}
		fmt.Printf("%-60s %9d %v\n", name, len(data), kinds)
	}
	os.WriteFile(outDir+"/pak99_dir.vpk", WriteVPK(result.Files), 0o644)
}

func TestProbeWriteOBJ(t *testing.T) {
	input, out, texture := os.Getenv("PROBE_INPUT"), os.Getenv("PROBE_OBJ"), os.Getenv("PROBE_TEXTURE")
	if input == "" || out == "" {
		t.Skip()
	}
	mesh, err := LoadMeshFile(input)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString("mtllib test.mtl\n")
	for _, p := range mesh.Positions {
		fmt.Fprintf(&b, "v %f %f %f\n", p[0], p[1], p[2])
	}
	for _, uv := range mesh.UVs {
		fmt.Fprintf(&b, "vt %f %f\n", uv[0], 1-uv[1])
	}
	for _, n := range mesh.Normals {
		fmt.Fprintf(&b, "vn %f %f %f\n", n[0], n[1], n[2])
	}
	var mtl strings.Builder
	for i, part := range mesh.Parts {
		fmt.Fprintf(&b, "usemtl part%d\n", i)
		fmt.Fprintf(&mtl, "newmtl part%d\nKd 1 1 1\nmap_Kd %s\n", i, texture)
		for t := 0; t+2 < len(part.Indices); t += 3 {
			a, c, d := part.Indices[t]+1, part.Indices[t+1]+1, part.Indices[t+2]+1
			fmt.Fprintf(&b, "f %d/%d/%d %d/%d/%d %d/%d/%d\n", a, a, a, c, c, c, d, d, d)
		}
	}
	os.WriteFile(out, []byte(b.String()), 0o644)
	os.WriteFile(strings.TrimSuffix(out, filepath.Base(out))+"test.mtl", []byte(mtl.String()), 0o644)
}
