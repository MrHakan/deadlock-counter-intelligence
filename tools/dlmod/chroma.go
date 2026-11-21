package main

import (
	"image"
	"image/color"
	"math"
)

// keyGreen makes a picture drawn on flat green (#00FF00) transparent where it is green, and
// takes the green spill off the edges, as image generators cannot draw transparency. Hero cards
// and portraits sit on the interface's own backgrounds, so they need it.
func keyGreen(src image.Image) image.Image {
	b := src.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBAModel.Convert(src.At(x, y)).(color.NRGBA)
			r, g, bl := float64(c.R), float64(c.G), float64(c.B)
			other := math.Max(r, bl)
			// how much greener the pixel is than its other channels, 0..255
			green := g - other
			alpha := 1 - smoothstep(40, 110, green)
			if limit := (r+bl)/2 + 10; g > limit && green > -30 {
				g = limit // despill: edges mixed with the green screen lose its tint, not only its excess
			}
			out.SetNRGBA(x-b.Min.X, y-b.Min.Y, color.NRGBA{c.R, uint8(g), c.B, uint8(float64(c.A) * alpha)})
		}
	}
	return out
}

// hasGreenBackground reports whether a picture's corners are flat green, which is how the
// workspace asks for cards to be drawn.
func hasGreenBackground(img image.Image) bool {
	b := img.Bounds()
	green := 0
	for _, p := range []image.Point{{b.Min.X, b.Min.Y}, {b.Max.X - 1, b.Min.Y}, {b.Min.X, b.Max.Y - 1}, {b.Max.X - 1, b.Max.Y - 1}} {
		c := color.NRGBAModel.Convert(img.At(p.X, p.Y)).(color.NRGBA)
		if c.A > 200 && int(c.G)-int(max(c.R, c.B)) > 80 {
			green++
		}
	}
	return green >= 3
}

// fadeLeft darkens the left part of a hero select picture to black, from the left edge to
// share from of the width fully and to share to softly, where the hero grid covers it; the game's
// own backgrounds are black there.
func fadeLeft(src image.Image, from, to float64) image.Image {
	b := src.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBAModel.Convert(src.At(x, y)).(color.NRGBA)
			k := smoothstep(from, to, (float64(x-b.Min.X)+0.5)/float64(b.Dx()))
			out.SetNRGBA(x-b.Min.X, y-b.Min.Y, color.NRGBA{uint8(float64(c.R) * k), uint8(float64(c.G) * k), uint8(float64(c.B) * k), c.A})
		}
	}
	return out
}
