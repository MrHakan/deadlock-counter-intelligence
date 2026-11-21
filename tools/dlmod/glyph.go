package main

import (
	"image"
	"image/color"
	"math"
)

// Ability icons (panorama/images/hud/abilities/<hero>/*_psd) are white glyphs on transparency,
// which the HUD tints and puts in its round frames. Image generators draw such glyphs best as
// white on black, so glyphImage turns a picture into one: brightness becomes opacity (or the
// picture's own transparency is kept), the glyph is trimmed to what is drawn and centred, filling
// fill of the square.

func glyphImage(src image.Image, size int, fill float64) image.Image {
	b := src.Bounds()
	transparent := false
	for y := b.Min.Y; y < b.Max.Y && !transparent; y += max(1, b.Dy()/64) {
		for x := b.Min.X; x < b.Max.X; x += max(1, b.Dx()/64) {
			if _, _, _, a := src.At(x, y).RGBA(); a < 0xf000 {
				transparent = true
				break
			}
		}
	}
	// coverage of one source pixel: its alpha when the picture has transparency, else brightness
	cover := func(x, y int) float64 {
		c := color.NRGBAModel.Convert(src.At(x, y)).(color.NRGBA)
		if transparent {
			return float64(c.A) / 255
		}
		l := (0.299*float64(c.R) + 0.587*float64(c.G) + 0.114*float64(c.B)) / 255
		return smoothstep(0.25, 0.75, l)
	}
	lo, hi := image.Point{b.Max.X, b.Max.Y}, image.Point{b.Min.X - 1, b.Min.Y - 1}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if cover(x, y) > 0.5 {
				lo.X, lo.Y = min(lo.X, x), min(lo.Y, y)
				hi.X, hi.Y = max(hi.X, x), max(hi.Y, y)
			}
		}
	}
	out := image.NewNRGBA(image.Rect(0, 0, size, size))
	if hi.X < lo.X {
		return out // nothing drawn
	}
	w, h := float64(hi.X-lo.X+1), float64(hi.Y-lo.Y+1)
	cx, cy := float64(lo.X)+w/2, float64(lo.Y)+h/2
	scale := math.Max(w, h) / (float64(size) * fill) // source pixels per output pixel
	const n = 4                                      // supersampling per axis
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			sum := 0.0
			for j := 0; j < n; j++ {
				for i := 0; i < n; i++ {
					sx := cx + (float64(x)+(float64(i)+0.5)/n-float64(size)/2)*scale
					sy := cy + (float64(y)+(float64(j)+0.5)/n-float64(size)/2)*scale
					px, py := int(math.Floor(sx)), int(math.Floor(sy))
					if px >= b.Min.X && px < b.Max.X && py >= b.Min.Y && py < b.Max.Y {
						sum += cover(px, py)
					}
				}
			}
			out.SetNRGBA(x, y, color.NRGBA{255, 255, 255, uint8(sum/(n*n)*255 + 0.5)})
		}
	}
	return out
}
