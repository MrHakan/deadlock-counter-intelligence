package main

import (
	"image"
	"image/color"
	"testing"
)

func TestCobaltPaletteMapping(t *testing.T) {
	p := CobaltPalette

	// 1. Mid green & yellow-green -> cobalt blue (B clearly dominates, not green).
	for _, in := range [][3]float64{
		{99, 113, 22},  // ivy_vine_cable olive-green (H~69)
		{122, 194, 55}, // ivy_entangling_thorns bright green (H~91)
		{118, 151, 53}, // particle vine green
		{40, 180, 60},  // saturated leaf green
	} {
		got := p.Recolor(in)
		if got[2] <= got[1] || got[2] <= got[0] || got[2] < 100 {
			t.Errorf("green %v -> %v, expected cobalt blue (B dominant)", in, got)
		}
		h, _, _ := rgbToHSV(got)
		if h < 195 || h > 240 {
			t.Errorf("green %v -> %v (H=%.1f), expected blue hue 195..240", in, got, h)
		}
	}

	// 2. Dark green -> deep blue (dark lightness preserved, B dominates).
	dark := p.Recolor([3]float64{39, 59, 18})
	if dark[2] <= dark[1] || dark[2] <= dark[0] {
		t.Errorf("dark green -> %v, expected deep blue", dark)
	}
	if dark[2] > 110 {
		t.Errorf("dark green -> %v, too bright for deep blue", dark)
	}

	// 3. Brown / bark / dark red thorns -> accent red #FF3B3B (R dominates).
	for _, in := range [][3]float64{
		{110, 70, 35}, // bark brown (H~28)
		{140, 20, 30}, // thorn crimson (H~355)
		{78, 68, 48},  // olive-brown bark (H~40)
		{180, 90, 20}, // warm amber-brown (H~26)
	} {
		got := p.Recolor(in)
		if got[0] <= got[1] || got[0] <= got[2] {
			t.Errorf("brown/thorn %v -> %v, expected red (R dominant)", in, got)
		}
	}

	// 4. Pale / bright yellow-green highlights -> near white (high V, low saturation).
	for _, in := range [][3]float64{
		{224, 242, 69},  // bright yellow-green spark
		{230, 240, 190}, // pale highlight
	} {
		got := p.Recolor(in)
		_, s, v := rgbToHSV(got)
		if v < 0.85 || s > 0.35 {
			t.Errorf("pale highlight %v -> %v (S=%.2f V=%.2f), expected near-white", in, got, s, v)
		}
	}

	// 5. Pure black, neutral grey, pure white stay unchanged.
	for _, in := range [][3]float64{
		{0, 0, 0},
		{128, 128, 128},
		{255, 255, 255},
	} {
		got := p.Recolor(in)
		if !near(got, in) {
			t.Errorf("neutral %v changed to %v", in, got)
		}
	}

	// 6. Shading/lightness ordering preserved (lighter green stays lighter than darker green).
	lo := p.Recolor([3]float64{30, 60, 20})
	hi := p.Recolor([3]float64{90, 180, 60})
	if hi[2] <= lo[2] {
		t.Errorf("lightness ordering inverted: dark=%v light=%v", lo, hi)
	}
}

func TestRepaintImagePreservesAlphaZero(t *testing.T) {
	// Simulate a Source 2 PBR colour texture where alpha = 0 (metalness = 0) on opaque RGB pixels.
	src := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	src.SetNRGBA(0, 0, color.NRGBA{80, 140, 30, 0})   // green vine, metalness=0
	src.SetNRGBA(1, 0, color.NRGBA{120, 120, 120, 0}) // neutral grey, metalness=0

	out, changed := RepaintImage(src, CobaltPalette.Recolor)
	if changed != 1 {
		t.Fatalf("changed = %d, want 1", changed)
	}
	p0 := out.NRGBAAt(0, 0)
	if p0.A != 0 {
		t.Fatalf("alpha changed from 0 to %d", p0.A)
	}
	if p0.B <= p0.G || p0.B <= p0.R {
		t.Fatalf("alpha=0 green pixel was not repainted to blue: %v", p0)
	}
	p1 := out.NRGBAAt(1, 0)
	if p1 != (color.NRGBA{120, 120, 120, 0}) {
		t.Fatalf("neutral pixel changed to %v", p1)
	}
}
