package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MrHakan/deadlock-modkit/tools/dlmod/source2"
)

// writePaintContactSheets writes before/after PNG contact sheets for repainted textures and
// particle/material colours into outDir.
func writePaintContactSheets(outDir string, hero string, pal ColorPalette, res *paintResult, index *source2.VPKIndex) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	if len(res.RepaintedTextures) > 0 {
		texSheet := renderTextureContactSheet(hero, pal, res.RepaintedTextures)
		if err := savePNG(filepath.Join(outDir, "textures_before_after.png"), texSheet); err != nil {
			return err
		}
	}
	swatches := collectHeroColorSwatches(index, hero)
	if len(swatches) > 0 {
		palSheet := renderPaletteContactSheet(hero, pal, swatches)
		if err := savePNG(filepath.Join(outDir, "particles_before_after.png"), palSheet); err != nil {
			return err
		}
	}
	return nil
}

func savePNG(dst string, img image.Image) error {
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func renderTextureContactSheet(hero string, pal ColorPalette, items []paintedTexture) *image.RGBA {
	const (
		tile   = 256
		gap    = 16
		pairW  = tile*2 + gap
		header = 64
		labelH = 38
		pad    = 24
	)
	cols := 2
	if len(items) == 1 {
		cols = 1
	}
	rows := (len(items) + cols - 1) / cols
	w := pad*2 + cols*pairW + (cols-1)*pad
	h := header + pad + rows*(tile+labelH+pad)

	canvas := image.NewRGBA(image.Rect(0, 0, w, h))
	fillRect(canvas, canvas.Bounds(), color.RGBA{18, 22, 30, 255})

	// Title bar
	fillRect(canvas, image.Rect(0, 0, w, header), color.RGBA{26, 33, 46, 255})
	title := fmt.Sprintf("%s ABILITY TEXTURES - BEFORE / AFTER (%s PALETTE, %d TEXTURES)",
		strings.ToUpper(heroDisplayName(hero)), strings.ToUpper(pal.Name), len(items))
	drawText(canvas, pad, 16, 2, title, color.RGBA{240, 245, 255, 255})
	drawText(canvas, pad, 42, 1, "LEFT: ORIGINAL GAME TEXTURE   |   RIGHT: REPAINTED (BLUE #2467FF / RED #FF3B3B / WHITE #F5F5F5)", color.RGBA{145, 175, 220, 255})

	for i, it := range items {
		col := i % cols
		row := i / cols
		x0 := pad + col*(pairW+pad)
		y0 := header + pad + row*(tile+labelH+pad)

		// Card background
		fillRect(canvas, image.Rect(x0-6, y0-6, x0+pairW+6, y0+tile+labelH+4), color.RGBA{28, 34, 46, 255})

		// Check if alpha is metalness-in-alpha (all near-zero) so RGB is visible on contact sheet.
		forceOpaque := isAlphaZeroImage(it.Original)
		drawTile(canvas, x0, y0, tile, it.Original, forceOpaque)
		drawTile(canvas, x0+tile+gap, y0, tile, it.Repainted, forceOpaque)

		// Badges (opaque RGBA so premultiplied alpha stays valid in PNG output)
		fillRect(canvas, image.Rect(x0+4, y0+4, x0+64, y0+20), color.RGBA{18, 22, 30, 255})
		drawText(canvas, x0+8, y0+8, 1, "BEFORE", color.RGBA{210, 220, 235, 255})
		fillRect(canvas, image.Rect(x0+tile+gap+4, y0+4, x0+tile+gap+58, y0+20), color.RGBA{36, 103, 255, 255})
		drawText(canvas, x0+tile+gap+8, y0+8, 1, "AFTER", color.RGBA{255, 255, 255, 255})

		// Caption
		name := path.Base(it.Path)
		if len(name) > 58 {
			name = name[:55] + "..."
		}
		meta := fmt.Sprintf("%dx%d %s (%d MIP)  CHANGED %.0f%%",
			it.Width, it.Height, source2.TextureFormatName(it.Format), it.Mips,
			100*float64(it.ChangedPixels)/float64(max(1, it.TotalPixels)))
		if forceOpaque {
			meta += "  [RGB VIEW, ALPHA=METALNESS]"
		}
		drawText(canvas, x0, y0+tile+8, 1, strings.ToUpper(name), color.RGBA{235, 240, 250, 255})
		drawText(canvas, x0, y0+tile+22, 1, meta, color.RGBA{140, 165, 200, 255})
	}
	return canvas
}

type colorSwatch struct {
	Color [3]uint8
	Uses  int
}

func collectHeroColorSwatches(index *source2.VPKIndex, hero string) []colorSwatch {
	names := heroFileNames(hero)
	counts := map[[3]uint8]int{}
	for file := range index.Files {
		low := strings.ToLower(file)
		if !strings.HasSuffix(low, ".vpcf_c") {
			continue
		}
		match := false
		for _, name := range names {
			if strings.HasPrefix(low, "particles/abilities/"+name+"/") ||
				strings.HasPrefix(low, "particles/weapon_fx/"+name+"/") ||
				(strings.HasPrefix(low, "particles/status_fx/") && strings.Contains(path.Base(low), name)) {
				match = true
				break
			}
		}
		if !match {
			continue
		}
		data, err := index.Read(file)
		if err != nil {
			continue
		}
		cols, err := source2.ParticleColors(data)
		if err != nil {
			continue
		}
		for _, c := range cols {
			// Skip pure black (0,0,0) which is mostly default/unused literal fields.
			if c == ([3]uint8{0, 0, 0}) {
				continue
			}
			counts[c]++
		}
	}
	var out []colorSwatch
	for c, n := range counts {
		out = append(out, colorSwatch{c, n})
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Uses != out[b].Uses {
			return out[a].Uses > out[b].Uses
		}
		return fmt.Sprint(out[a].Color) < fmt.Sprint(out[b].Color)
	})
	if len(out) > 48 {
		out = out[:48]
	}
	return out
}

func renderPaletteContactSheet(hero string, pal ColorPalette, swatches []colorSwatch) *image.RGBA {
	const (
		cols   = 6
		cellW  = 170
		cellH  = 78
		gap    = 12
		header = 64
		pad    = 20
	)
	rows := (len(swatches) + cols - 1) / cols
	w := pad*2 + cols*cellW + (cols-1)*gap
	h := header + pad + rows*(cellH+gap) + pad
	canvas := image.NewRGBA(image.Rect(0, 0, w, h))
	fillRect(canvas, canvas.Bounds(), color.RGBA{18, 22, 30, 255})
	fillRect(canvas, image.Rect(0, 0, w, header), color.RGBA{26, 33, 46, 255})
	title := fmt.Sprintf("%s PARTICLE COLOURS - BEFORE / AFTER (%s PALETTE, TOP %d COLOURS)",
		strings.ToUpper(heroDisplayName(hero)), strings.ToUpper(pal.Name), len(swatches))
	drawText(canvas, pad, 16, 2, title, color.RGBA{240, 245, 255, 255})
	drawText(canvas, pad, 42, 1, "LEFT HALF: ORIGINAL PARTICLE COLOUR   |   RIGHT HALF: COBALT PALETTE REPAINT", color.RGBA{145, 175, 220, 255})

	for i, sw := range swatches {
		col := i % cols
		row := i / cols
		x0 := pad + col*(cellW+gap)
		y0 := header + pad + row*(cellH+gap)
		fillRect(canvas, image.Rect(x0, y0, x0+cellW, y0+cellH), color.RGBA{28, 34, 46, 255})

		orig := color.RGBA{sw.Color[0], sw.Color[1], sw.Color[2], 255}
		mapped := pal.Recolor([3]float64{float64(sw.Color[0]), float64(sw.Color[1]), float64(sw.Color[2])})
		after := color.RGBA{
			uint8(math.Round(math.Max(0, math.Min(255, mapped[0])))),
			uint8(math.Round(math.Max(0, math.Min(255, mapped[1])))),
			uint8(math.Round(math.Max(0, math.Min(255, mapped[2])))),
			255,
		}
		half := (cellW - 12) / 2
		fillRect(canvas, image.Rect(x0+6, y0+6, x0+6+half, y0+48), orig)
		fillRect(canvas, image.Rect(x0+6+half, y0+6, x0+cellW-6, y0+48), after)
		// Divider line
		fillRect(canvas, image.Rect(x0+6+half-1, y0+6, x0+6+half+1, y0+48), color.RGBA{18, 22, 30, 255})

		line1 := fmt.Sprintf("#%02X%02X%02X -> #%02X%02X%02X", orig.R, orig.G, orig.B, after.R, after.G, after.B)
		line2 := fmt.Sprintf("%d USES", sw.Uses)
		drawText(canvas, x0+6, y0+54, 1, line1, color.RGBA{225, 232, 245, 255})
		drawText(canvas, x0+6, y0+65, 1, line2, color.RGBA{135, 160, 195, 255})
	}
	return canvas
}

func isAlphaZeroImage(img image.Image) bool {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			if c.A > 16 {
				return false
			}
		}
	}
	return true
}

func drawTile(dst *image.RGBA, x0, y0, size int, src image.Image, forceOpaque bool) {
	// Subtle dark checkerboard background so alpha-cutout vines/leaves stand out clearly.
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			bg := color.RGBA{34, 40, 52, 255}
			if ((x/12)+(y/12))%2 == 1 {
				bg = color.RGBA{46, 54, 68, 255}
			}
			dst.SetRGBA(x0+x, y0+y, bg)
		}
	}
	if src == nil {
		return
	}
	b := src.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return
	}
	// Fit inside size x size preserving aspect ratio (with nearest/bilinear scaling).
	scale := math.Min(float64(size)/float64(b.Dx()), float64(size)/float64(b.Dy()))
	dw := max(1, int(math.Round(float64(b.Dx())*scale)))
	dh := max(1, int(math.Round(float64(b.Dy())*scale)))
	ox := x0 + (size-dw)/2
	oy := y0 + (size-dh)/2

	for y := 0; y < dh; y++ {
		sy := b.Min.Y + min(b.Dy()-1, int(float64(y)*float64(b.Dy())/float64(dh)))
		for x := 0; x < dw; x++ {
			sx := b.Min.X + min(b.Dx()-1, int(float64(x)*float64(b.Dx())/float64(dw)))
			c := color.NRGBAModel.Convert(src.At(sx, sy)).(color.NRGBA)
			a := float64(c.A) / 255.0
			if forceOpaque {
				a = 1.0
			}
			bg := dst.RGBAAt(ox+x, oy+y)
			r := uint8(math.Round(float64(c.R)*a + float64(bg.R)*(1-a)))
			g := uint8(math.Round(float64(c.G)*a + float64(bg.G)*(1-a)))
			bl := uint8(math.Round(float64(c.B)*a + float64(bg.B)*(1-a)))
			dst.SetRGBA(ox+x, oy+y, color.RGBA{r, g, bl, 255})
		}
	}
}

func fillRect(img *image.RGBA, rect image.Rectangle, c color.RGBA) {
	rect = rect.Intersect(img.Bounds())
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			img.SetRGBA(x, y, c)
		}
	}
}

// Tiny 5x7 uppercase ASCII bitmap font (pure stdlib, zero external dependencies).
var font5x7 = map[rune][7]uint8{
	'A': {0x0E, 0x11, 0x11, 0x1F, 0x11, 0x11, 0x11},
	'B': {0x1E, 0x11, 0x11, 0x1E, 0x11, 0x11, 0x1E},
	'C': {0x0E, 0x11, 0x10, 0x10, 0x10, 0x11, 0x0E},
	'D': {0x1C, 0x12, 0x11, 0x11, 0x11, 0x12, 0x1C},
	'E': {0x1F, 0x10, 0x10, 0x1E, 0x10, 0x10, 0x1F},
	'F': {0x1F, 0x10, 0x10, 0x1E, 0x10, 0x10, 0x10},
	'G': {0x0E, 0x11, 0x10, 0x17, 0x11, 0x11, 0x0F},
	'H': {0x11, 0x11, 0x11, 0x1F, 0x11, 0x11, 0x11},
	'I': {0x0E, 0x04, 0x04, 0x04, 0x04, 0x04, 0x0E},
	'J': {0x07, 0x02, 0x02, 0x02, 0x12, 0x12, 0x0C},
	'K': {0x11, 0x12, 0x14, 0x18, 0x14, 0x12, 0x11},
	'L': {0x10, 0x10, 0x10, 0x10, 0x10, 0x10, 0x1F},
	'M': {0x11, 0x1B, 0x15, 0x15, 0x11, 0x11, 0x11},
	'N': {0x11, 0x19, 0x15, 0x13, 0x11, 0x11, 0x11},
	'O': {0x0E, 0x11, 0x11, 0x11, 0x11, 0x11, 0x0E},
	'P': {0x1E, 0x11, 0x11, 0x1E, 0x10, 0x10, 0x10},
	'Q': {0x0E, 0x11, 0x11, 0x11, 0x15, 0x12, 0x0D},
	'R': {0x1E, 0x11, 0x11, 0x1E, 0x14, 0x12, 0x11},
	'S': {0x0F, 0x10, 0x10, 0x0E, 0x01, 0x01, 0x1E},
	'T': {0x1F, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04},
	'U': {0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x0E},
	'V': {0x11, 0x11, 0x11, 0x11, 0x11, 0x0A, 0x04},
	'W': {0x11, 0x11, 0x11, 0x15, 0x15, 0x1B, 0x11},
	'X': {0x11, 0x11, 0x0A, 0x04, 0x0A, 0x11, 0x11},
	'Y': {0x11, 0x11, 0x0A, 0x04, 0x04, 0x04, 0x04},
	'Z': {0x1F, 0x01, 0x02, 0x04, 0x08, 0x10, 0x1F},
	'0': {0x0E, 0x11, 0x13, 0x15, 0x19, 0x11, 0x0E},
	'1': {0x04, 0x0C, 0x04, 0x04, 0x04, 0x04, 0x0E},
	'2': {0x0E, 0x11, 0x01, 0x06, 0x08, 0x10, 0x1F},
	'3': {0x1E, 0x01, 0x01, 0x0E, 0x01, 0x01, 0x1E},
	'4': {0x02, 0x06, 0x0A, 0x12, 0x1F, 0x02, 0x02},
	'5': {0x1F, 0x10, 0x1E, 0x01, 0x01, 0x11, 0x0E},
	'6': {0x0E, 0x10, 0x1E, 0x11, 0x11, 0x11, 0x0E},
	'7': {0x1F, 0x01, 0x02, 0x04, 0x08, 0x08, 0x08},
	'8': {0x0E, 0x11, 0x11, 0x0E, 0x11, 0x11, 0x0E},
	'9': {0x0E, 0x11, 0x11, 0x0F, 0x01, 0x02, 0x0C},
	' ': {0, 0, 0, 0, 0, 0, 0},
	'-': {0x00, 0x00, 0x00, 0x1F, 0x00, 0x00, 0x00},
	'_': {0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x1F},
	'.': {0x00, 0x00, 0x00, 0x00, 0x00, 0x0C, 0x0C},
	',': {0x00, 0x00, 0x00, 0x00, 0x00, 0x04, 0x08},
	':': {0x00, 0x04, 0x04, 0x00, 0x04, 0x04, 0x00},
	'/': {0x01, 0x02, 0x02, 0x04, 0x08, 0x08, 0x10},
	'|': {0x04, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04},
	'#': {0x0A, 0x1F, 0x0A, 0x0A, 0x1F, 0x0A, 0x00},
	'%': {0x19, 0x1A, 0x02, 0x04, 0x08, 0x0B, 0x13},
	'(': {0x02, 0x04, 0x08, 0x08, 0x08, 0x04, 0x02},
	')': {0x08, 0x04, 0x02, 0x02, 0x02, 0x04, 0x08},
	'[': {0x0E, 0x08, 0x08, 0x08, 0x08, 0x08, 0x0E},
	']': {0x0E, 0x02, 0x02, 0x02, 0x02, 0x02, 0x0E},
	'=': {0x00, 0x00, 0x1F, 0x00, 0x1F, 0x00, 0x00},
	'>': {0x10, 0x08, 0x04, 0x02, 0x04, 0x08, 0x10},
	'<': {0x02, 0x04, 0x08, 0x10, 0x08, 0x04, 0x02},
}

func drawText(dst *image.RGBA, x, y, scale int, text string, c color.RGBA) {
	cx := x
	for _, r := range strings.ToUpper(text) {
		g, ok := font5x7[r]
		if !ok {
			g = font5x7[' ']
		}
		for row := 0; row < 7; row++ {
			bits := g[row]
			for col := 0; col < 5; col++ {
				if bits&(1<<(4-col)) == 0 {
					continue
				}
				for sy := 0; sy < scale; sy++ {
					for sx := 0; sx < scale; sx++ {
						px, py := cx+col*scale+sx, y+row*scale+sy
						if image.Pt(px, py).In(dst.Bounds()) {
							dst.SetRGBA(px, py, c)
						}
					}
				}
			}
		}
		cx += 6 * scale
	}
}
