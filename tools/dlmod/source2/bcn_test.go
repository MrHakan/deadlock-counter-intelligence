package source2

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"math"
	"testing"
)

type bitWriter struct {
	lo, hi uint64
	pos    uint
}

func (w *bitWriter) write(val int, n uint) {
	v := uint64(val) & ((1 << n) - 1)
	if w.pos+n <= 64 {
		w.lo |= v << w.pos
	} else if w.pos >= 64 {
		w.hi |= v << (w.pos - 64)
	} else {
		loBits := 64 - w.pos
		w.lo |= v << w.pos
		w.hi |= v >> loBits
	}
	w.pos += n
}

func (w *bitWriter) bytes() []byte {
	out := make([]byte, 16)
	binary.LittleEndian.PutUint64(out[0:8], w.lo)
	binary.LittleEndian.PutUint64(out[8:16], w.hi)
	return out
}

func makeRawVTex(w, h, format, mips int, flags uint16, extra []byte, extraCount int, streaming []byte) []byte {
	hdr := make([]byte, 40)
	binary.LittleEndian.PutUint16(hdr[0:], 1)
	binary.LittleEndian.PutUint16(hdr[2:], flags)
	binary.LittleEndian.PutUint16(hdr[20:], uint16(w))
	binary.LittleEndian.PutUint16(hdr[22:], uint16(h))
	binary.LittleEndian.PutUint16(hdr[24:], 1)
	hdr[26] = byte(format)
	hdr[27] = byte(mips)
	if extraCount > 0 {
		binary.LittleEndian.PutUint32(hdr[32:], 8) // extra table starts at byte 40 (32+8)
		binary.LittleEndian.PutUint32(hdr[36:], uint32(extraCount))
		hdr = append(hdr, extra...)
	}
	return (&Resource{
		Version:   1,
		Blocks:    []Block{{"DATA", hdr}},
		Streaming: streaming,
	}).Encode()
}

func TestDecodeBC4(t *testing.T) {
	// Mode 1: a0 > a1 (8 interpolated values from 210 down to 70).
	block := make([]byte, 8)
	block[0], block[1] = 210, 70
	// Pixel 0 -> idx 0 (210), pixel 1 -> idx 1 (70), pixel 2 -> idx 2 (190), pixel 3 -> idx 7 (90).
	var bits uint64 = 0 | (1 << 3) | (2 << 6) | (7 << 9)
	for k := 0; k < 6; k++ {
		block[2+k] = byte(bits >> (8 * k))
	}
	vtex := makeRawVTex(4, 4, textureATI1N, 1, 0, nil, 0, block)
	img, info, err := DecodeTexture(vtex)
	if err != nil {
		t.Fatal(err)
	}
	if info.Format != textureATI1N || TextureFormatName(info.Format) != "BC4" {
		t.Fatalf("unexpected info: %+v", info)
	}
	want := []uint8{210, 70, 190, 90}
	for x, wv := range want {
		got := img.At(x, 0).(color.NRGBA)
		if got.R != wv || got.G != wv || got.B != wv || got.A != 255 {
			t.Errorf("pixel (%d,0) = %v, want gray %d", x, got, wv)
		}
	}

	// Mode 2: a0 <= a1 (6 interpolated values + 0 at idx 6, 255 at idx 7).
	block2 := make([]byte, 8)
	block2[0], block2[1] = 50, 150
	bits = 0 | (1 << 3) | (6 << 6) | (7 << 9)
	for k := 0; k < 6; k++ {
		block2[2+k] = byte(bits >> (8 * k))
	}
	img2, _, err := DecodeTexture(makeRawVTex(4, 4, textureATI1N, 1, 0, nil, 0, block2))
	if err != nil {
		t.Fatal(err)
	}
	want2 := []uint8{50, 150, 0, 255}
	for x, wv := range want2 {
		if got := img2.At(x, 0).(color.NRGBA); got.R != wv {
			t.Errorf("mode2 pixel (%d,0) = %v, want %d", x, got, wv)
		}
	}
}

func TestDecodeBC5(t *testing.T) {
	block := make([]byte, 16)
	block[0], block[1] = 200, 100 // R channel
	block[8], block[9] = 40, 240  // G channel
	// Pixel 0: R idx 0 (200), G idx 1 (240).
	block[10] = 1
	vtex := makeRawVTex(4, 4, textureATI2N, 1, 0, nil, 0, block)
	img, info, err := DecodeTexture(vtex)
	if err != nil {
		t.Fatal(err)
	}
	if info.Format != textureATI2N || TextureFormatName(info.Format) != "BC5" {
		t.Fatalf("unexpected info: %+v", info)
	}
	got0 := img.At(0, 0).(color.NRGBA)
	if got0 != (color.NRGBA{200, 240, 0, 255}) {
		t.Fatalf("pixel (0,0) = %v, want {200 240 0 255}", got0)
	}
	got1 := img.At(1, 0).(color.NRGBA)
	if got1 != (color.NRGBA{200, 40, 0, 255}) {
		t.Fatalf("pixel (1,0) = %v, want {200 40 0 255}", got1)
	}
}

func TestBC7TablesInvariants(t *testing.T) {
	for p := 0; p < 64; p++ {
		if bc7Partitions2[p][0] != 0 {
			t.Errorf("partition2[%d][0] = %d, want 0", p, bc7Partitions2[p][0])
		}
		if got := bc7Partitions2[p][bc7Anchor2[p]]; got != 1 {
			t.Errorf("partition2[%d][anchor=%d] = %d, want 1", p, bc7Anchor2[p], got)
		}
		if bc7Partitions3[p][0] != 0 {
			t.Errorf("partition3[%d][0] = %d, want 0", p, bc7Partitions3[p][0])
		}
		if got := bc7Partitions3[p][bc7Anchor3a[p]]; got != 1 {
			t.Errorf("partition3[%d][anchor1=%d] = %d, want 1", p, bc7Anchor3a[p], got)
		}
		if got := bc7Partitions3[p][bc7Anchor3b[p]]; got != 2 {
			t.Errorf("partition3[%d][anchor2=%d] = %d, want 2", p, bc7Anchor3b[p], got)
		}
	}
}

func TestDecodeBC7Modes(t *testing.T) {
	// Reserved mode (first byte 0) -> transparent black.
	var out [16]color.NRGBA
	decodeBC7Block(make([]byte, 16), &out)
	for i, c := range out {
		if c != (color.NRGBA{}) {
			t.Fatalf("reserved mode pixel %d = %v, want zero", i, c)
		}
	}

	// Mode 6: 1 subset, 7-bit RGBA + 1 endpoint P-bit = exact 8-bit RGBA, 4-bit indices.
	{
		var bw bitWriter
		bw.write(1<<6, 7) // mode 6
		// Endpoints: e0 = (36, 103, 255, 128), e1 = (255, 59, 59, 255)
		// With endpoint P-bits: p0=0 (even channels), p1=1 (odd channels):
		// e0=(36,102,254,128), e1=(255,59,59,255)
		e0 := [4]int{36, 102, 254, 128}
		e1 := [4]int{255, 59, 59, 255}
		for c := 0; c < 4; c++ {
			bw.write(e0[c]>>1, 7)
			bw.write(e1[c]>>1, 7)
		}
		bw.write(0, 1)  // p0
		bw.write(1, 1)  // p1
		bw.write(0, 3)  // pixel 0 (anchor: 3 bits -> idx 0 = e0)
		bw.write(15, 4) // pixel 1 (idx 15 = e1)
		for i := 2; i < 16; i++ {
			bw.write(0, 4)
		}
		if bw.pos != 128 {
			t.Fatalf("mode 6 bit count = %d, want 128", bw.pos)
		}
		img, info, err := DecodeTexture(makeRawVTex(4, 4, textureBC7, 1, 0, nil, 0, bw.bytes()))
		if err != nil {
			t.Fatal(err)
		}
		if info.Format != textureBC7 || TextureFormatName(info.Format) != "BC7" {
			t.Fatalf("unexpected info: %+v", info)
		}
		if got := img.At(0, 0).(color.NRGBA); got != (color.NRGBA{36, 102, 254, 128}) {
			t.Errorf("mode 6 pixel 0 = %v, want {36 102 254 128}", got)
		}
		if got := img.At(1, 0).(color.NRGBA); got != (color.NRGBA{255, 59, 59, 255}) {
			t.Errorf("mode 6 pixel 1 = %v, want {255 59 59 255}", got)
		}
	}

	// Mode 5: 1 subset, 2-bit rotation (rotation=1 swaps R and A), 7-bit RGB, 8-bit A, 2-bit color & alpha indices.
	{
		var bw bitWriter
		bw.write(1<<5, 6) // mode 5
		bw.write(1, 2)    // rotation = 1 (swap R <-> A)
		// e0 RGB=(0, 127, 0) -> (0, 255, 0), A=200; e1 RGB=(0, 127, 0), A=100
		bw.write(0, 7)
		bw.write(0, 7)
		bw.write(127, 7)
		bw.write(127, 7)
		bw.write(0, 7)
		bw.write(0, 7)
		bw.write(200, 8)
		bw.write(100, 8)
		// Color indices: anchor pixel 0 (1 bit), pixels 1..15 (2 bits)
		bw.write(0, 1)
		for i := 1; i < 16; i++ {
			bw.write(0, 2)
		}
		// Alpha indices: pixel 0 -> 0 (200), pixel 1 -> 3 (100)
		bw.write(0, 1)
		bw.write(3, 2)
		for i := 2; i < 16; i++ {
			bw.write(0, 2)
		}
		if bw.pos != 128 {
			t.Fatalf("mode 5 bit count = %d, want 128", bw.pos)
		}
		decodeBC7Block(bw.bytes(), &out)
		// Rotation 1 swapped R (0) and A (200 / 100):
		if out[0] != (color.NRGBA{200, 255, 0, 0}) {
			t.Errorf("mode 5 pixel 0 = %v, want {200 255 0 0}", out[0])
		}
		if out[1] != (color.NRGBA{100, 255, 0, 0}) {
			t.Errorf("mode 5 pixel 1 = %v, want {100 255 0 0}", out[1])
		}
	}

	// Mode 4: 1 subset, rotation=0, indexSel=1 (3-bit color, 2-bit alpha), 5-bit RGB, 6-bit A.
	{
		var bw bitWriter
		bw.write(1<<4, 5) // mode 4
		bw.write(0, 2)    // rotation = 0
		bw.write(1, 1)    // indexSel = 1 (color uses idx2 3-bit, alpha uses idx1 2-bit)
		// e0=(0,0,0,0), e1=(31,31,31,63) -> (255,255,255,255)
		for c := 0; c < 3; c++ {
			bw.write(0, 5)
			bw.write(31, 5)
		}
		bw.write(0, 6)
		bw.write(63, 6)
		// idx1 (2-bit, used for alpha): pixel 0 -> 0 (A=0), pixel 1 -> 3 (A=255)
		bw.write(0, 1)
		bw.write(3, 2)
		for i := 2; i < 16; i++ {
			bw.write(0, 2)
		}
		// idx2 (3-bit, used for color): pixel 0 -> 0 (0), pixel 1 -> 7 (255)
		bw.write(0, 2)
		bw.write(7, 3)
		for i := 2; i < 16; i++ {
			bw.write(0, 3)
		}
		if bw.pos != 128 {
			t.Fatalf("mode 4 bit count = %d, want 128", bw.pos)
		}
		decodeBC7Block(bw.bytes(), &out)
		if out[0] != (color.NRGBA{0, 0, 0, 0}) || out[1] != (color.NRGBA{255, 255, 255, 255}) {
			t.Errorf("mode 4 pixels 0,1 = %v, %v", out[0], out[1])
		}
	}

	// Mode 1: 2 subsets, partition 0 (left two cols subset 0, right two cols subset 1), 6-bit RGB + shared P-bit.
	{
		var bw bitWriter
		bw.write(1<<1, 2) // mode 1
		bw.write(0, 6)    // partition 0: {0,0,1,1, ...}
		// Subset 0 = pure red (63,0,0) with p=1 -> (255,3,3); Subset 1 = pure blue (0,0,63) with p=0 -> (0,0,252)
		// R0,R1,R2,R3
		bw.write(63, 6)
		bw.write(63, 6)
		bw.write(0, 6)
		bw.write(0, 6)
		// G0..G3
		for i := 0; i < 4; i++ {
			bw.write(0, 6)
		}
		// B0..B3
		bw.write(0, 6)
		bw.write(0, 6)
		bw.write(63, 6)
		bw.write(63, 6)
		// Shared P-bits: s0=1, s1=0
		bw.write(1, 1)
		bw.write(0, 1)
		// 3-bit indices (anchors: 0 and bc7Anchor2[0]=15 have 2 bits)
		for i := 0; i < 16; i++ {
			bits := uint(3)
			if i == 0 || i == int(bc7Anchor2[0]) {
				bits = 2
			}
			bw.write(0, bits)
		}
		if bw.pos != 128 {
			t.Fatalf("mode 1 bit count = %d, want 128", bw.pos)
		}
		decodeBC7Block(bw.bytes(), &out)
		if out[0].R < 250 || out[0].B > 5 || out[0].A != 255 {
			t.Errorf("mode 1 subset 0 pixel 0 = %v, want red", out[0])
		}
		if out[2].B < 250 || out[2].R > 5 || out[2].A != 255 {
			t.Errorf("mode 1 subset 1 pixel 2 = %v, want blue", out[2])
		}
	}

	// Mode 0: 3 subsets, partition 0, 4-bit RGB + endpoint P-bits, 3-bit indices.
	{
		var bw bitWriter
		bw.write(1<<0, 1) // mode 0
		bw.write(0, 4)    // partition 0: pixel 0 -> s0, pixel 2 -> s1, pixel 9 -> s2
		// s0=red, s1=green, s2=blue
		rVals := [6]int{15, 15, 0, 0, 0, 0}
		gVals := [6]int{0, 0, 15, 15, 0, 0}
		bVals := [6]int{0, 0, 0, 0, 15, 15}
		for _, v := range rVals {
			bw.write(v, 4)
		}
		for _, v := range gVals {
			bw.write(v, 4)
		}
		for _, v := range bVals {
			bw.write(v, 4)
		}
		for i := 0; i < 6; i++ {
			bw.write(1, 1) // endpoint P-bits
		}
		for i := 0; i < 16; i++ {
			bits := uint(3)
			if i == 0 || i == int(bc7Anchor3a[0]) || i == int(bc7Anchor3b[0]) {
				bits = 2
			}
			bw.write(0, bits)
		}
		if bw.pos != 128 {
			t.Fatalf("mode 0 bit count = %d, want 128", bw.pos)
		}
		decodeBC7Block(bw.bytes(), &out)
		if out[0].R < 250 || out[2].G < 250 || out[9].B < 250 {
			t.Errorf("mode 0 subsets = %v, %v, %v", out[0], out[2], out[9])
		}
	}

	// Mode 2, 3, 7 bit-width sanity checks (all must consume exactly 128 bits and decode endpoints).
	for _, modeIdx := range []int{2, 3, 7} {
		m := bc7Modes[modeIdx]
		var bw bitWriter
		bw.write(1<<modeIdx, uint(modeIdx+1))
		bw.write(0, uint(m.partitionBits))
		for c := 0; c < 3; c++ {
			for i := 0; i < m.subsets*2; i++ {
				if c == 0 {
					bw.write((1<<m.colorBits)-1, uint(m.colorBits))
				} else {
					bw.write(0, uint(m.colorBits))
				}
			}
		}
		if m.alphaBits > 0 {
			for i := 0; i < m.subsets*2; i++ {
				bw.write((1<<m.alphaBits)-1, uint(m.alphaBits))
			}
		}
		if m.endpointPBits {
			for i := 0; i < m.subsets*2; i++ {
				bw.write(1, 1)
			}
		}
		for i := 0; i < 16; i++ {
			bits := uint(m.indexBits)
			if i == 0 || (m.subsets == 2 && i == int(bc7Anchor2[0])) ||
				(m.subsets == 3 && (i == int(bc7Anchor3a[0]) || i == int(bc7Anchor3b[0]))) {
				bits--
			}
			bw.write(0, bits)
		}
		if bw.pos != 128 {
			t.Fatalf("mode %d bit count = %d, want 128", modeIdx, bw.pos)
		}
		decodeBC7Block(bw.bytes(), &out)
		if out[0].R < 245 || out[0].G > 10 || out[0].B > 10 || out[0].A != 255 {
			t.Errorf("mode %d pixel 0 = %v, want pure red opaque", modeIdx, out[0])
		}
	}
}

func TestRepaintTexturePreservesHeaderSheetAndMips(t *testing.T) {
	// Build a fake 8x8 BC7 texture (2 mips) with extra type=1 (1024-byte fallback) and extra type=2 (sprite sheet).
	sheetPayload := []byte("SPRITE_SHEET_DATA_0123456789")
	fallbackPayload := make([]byte, 1024)
	// Extra table: 2 entries * 12 bytes = 24 bytes, followed by fallbackPayload then sheetPayload.
	var extra []byte
	// Entry 0: type=1, offset from entry+4 = 20, size = 1024
	extra = binary.LittleEndian.AppendUint32(extra, 1)
	extra = binary.LittleEndian.AppendUint32(extra, 20)
	extra = binary.LittleEndian.AppendUint32(extra, 1024)
	// Entry 1: type=2, offset from entry+4 = 8 + 1024, size = len(sheetPayload)
	extra = binary.LittleEndian.AppendUint32(extra, 2)
	extra = binary.LittleEndian.AppendUint32(extra, uint32(8+1024))
	extra = binary.LittleEndian.AppendUint32(extra, uint32(len(sheetPayload)))
	extra = append(extra, fallbackPayload...)
	extra = append(extra, sheetPayload...)

	// 2 mips of 8x8 BC7: 4x4 (16 bytes) + 8x8 (64 bytes) = 80 bytes.
	streaming := make([]byte, 80)
	orig := makeRawVTex(8, 8, textureBC7, 2, 0x0008, extra, 2, streaming)

	// Repaint with cobalt blue (#2467FF) and alpha=0 (like PBR metalness-in-alpha!).
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.SetNRGBA(x, y, color.NRGBA{36, 103, 255, 0})
		}
	}
	repainted, err := RepaintTexture(orig, img)
	if err != nil {
		t.Fatal(err)
	}

	decoded, info, err := DecodeTexture(repainted)
	if err != nil {
		t.Fatal(err)
	}
	if info.Width != 8 || info.Height != 8 || info.Mips != 2 || info.Flags != 0x0008 || info.Format != textureDXT5 {
		t.Fatalf("repainted info = %+v", info)
	}
	got := decoded.At(3, 3).(color.NRGBA)
	if math.Abs(float64(got.R)-36) > 8 || math.Abs(float64(got.G)-103) > 8 || math.Abs(float64(got.B)-255) > 8 || got.A != 0 {
		t.Fatalf("decoded repainted pixel = %v, want ~(36,103,255,0)", got)
	}

	// Verify sprite sheet payload preserved verbatim, fallback bits non-zero, reflectivity updated.
	res, err := DecodeResource(repainted)
	if err != nil {
		t.Fatal(err)
	}
	hdr := res.Block("DATA")
	if !bytes.Contains(hdr, sheetPayload) {
		t.Fatal("sprite sheet extra data was not preserved")
	}
	// Fallback bits at offset 40+24 = 64 should now be non-zero DXT5 blocks.
	if bytes.Equal(hdr[64:64+1024], fallbackPayload) {
		t.Fatal("fallback bits were not refreshed")
	}
	// Reflectivity B channel should be near 1.0 (255^2.2).
	reflB := math.Float32frombits(binary.LittleEndian.Uint32(hdr[12:]))
	if reflB < 0.9 {
		t.Fatalf("reflectivity B = %v, want ~1.0", reflB)
	}
}

func TestRecolorMaterial(t *testing.T) {
	// Build a minimal material with one white tint (must stay), one green colour (must change),
	// and one normal vector (must stay).
	mat, err := CompileMaterial("materials/test.vmat", "materials/test_color.vtex", 64, false)
	if err != nil {
		t.Fatal(err)
	}
	res, err := DecodeResource(mat)
	if err != nil {
		t.Fatal(err)
	}
	v, format, err := ReadKV3(res.Block("DATA"))
	if err != nil {
		t.Fatal(err)
	}
	vp := v.Get("m_vectorParams")
	vp.Items = append(vp.Items,
		named("g_vSelfIllumTint1", "m_value", vector(0.2, 0.8, 0.1, 1)),
		named("TextureNormal1", "m_value", vector(0.5, 0.5, 1, 0)),
	)
	encoded, err := WriteKV3(v, format)
	if err != nil {
		t.Fatal(err)
	}
	for i := range res.Blocks {
		if res.Blocks[i].Type == "DATA" {
			res.Blocks[i].Data = encoded
		}
	}
	raw := res.Encode()

	swapGB := func(c [3]float64) [3]float64 { return [3]float64{c[0], c[2], c[1]} }
	out, changed, err := RecolorMaterial(raw, swapGB)
	if err != nil {
		t.Fatal(err)
	}
	if changed != 1 {
		t.Fatalf("changed = %d, want 1 (only g_vSelfIllumTint1)", changed)
	}
	res2, _ := DecodeResource(out)
	v2, _, _ := ReadKV3(res2.Block("DATA"))
	for _, it := range v2.Get("m_vectorParams").Items {
		nm := it.Get("m_name").S
		vec := it.Get("m_value").Items
		switch nm {
		case "g_vColorTint1":
			if vec[0].F != 1 || vec[1].F != 1 || vec[2].F != 1 {
				t.Errorf("neutral g_vColorTint1 changed to %v %v %v", vec[0].F, vec[1].F, vec[2].F)
			}
		case "g_vSelfIllumTint1":
			if math.Abs(vec[1].F-0.1) > 0.01 || math.Abs(vec[2].F-0.8) > 0.01 || vec[3].F != 1 {
				t.Errorf("g_vSelfIllumTint1 = [%v %v %v %v], want [0.2 0.1 0.8 1]", vec[0].F, vec[1].F, vec[2].F, vec[3].F)
			}
		case "TextureNormal1":
			if vec[0].F != 0.5 || vec[1].F != 0.5 || vec[2].F != 1 {
				t.Errorf("TextureNormal1 changed")
			}
		}
	}
}
