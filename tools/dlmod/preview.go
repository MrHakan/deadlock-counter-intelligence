package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
)

// Mod previews are JPEG screenshots from GameBanana. Terminals show pictures in one of three
// ways, all written here with the standard library:
//
//   - blocks: every character cell shows two pixels with the upper-half block "▀", its
//     foreground colour for the top pixel and its background for the bottom one. Any terminal
//     with 24-bit colour shows it (Windows Terminal, the Windows 10+ console, most others).
//   - sixel: the DEC graphics format that Windows Terminal 1.22+, WezTerm, foot, mlterm and
//     xterm -ti vt340 draw at full resolution.
//   - kitty: the graphics protocol of kitty, Ghostty and WezTerm.
const (
	imagesOff = iota
	imagesBlocks
	imagesSixel
	imagesKitty
)

var imageModeNames = []string{"off", "blocks", "sixel", "kitty"}

// detectImageMode picks the best way to draw pictures from the environment. DLMOD_IMAGES
// (off, blocks, sixel or kitty) overrides it, and the browser's m key cycles through them.
func detectImageMode(getenv func(string) string) int {
	for mode, name := range imageModeNames {
		if strings.EqualFold(getenv("DLMOD_IMAGES"), name) {
			return mode
		}
	}
	program := strings.ToLower(getenv("TERM_PROGRAM"))
	switch {
	case getenv("NO_COLOR") != "":
		return imagesOff
	case getenv("KITTY_WINDOW_ID") != "" || strings.Contains(getenv("TERM"), "kitty") || program == "ghostty":
		return imagesKitty
	case program == "wezterm" || strings.HasPrefix(getenv("TERM"), "foot") || strings.Contains(getenv("TERM"), "mlterm"):
		return imagesSixel
	}
	// Windows Terminal draws sixel only from version 1.22, which no environment variable reveals,
	// so it starts with blocks; m switches to sixel.
	return imagesBlocks
}

// previewCache keeps downloaded pictures next to the catalog, named after their address.
func previewCache(game Game, address string) string {
	sum := sha1.Sum([]byte(address))
	return filepath.Join(game.StateDir(), "catalog", "previews", hex.EncodeToString(sum[:8])+filepath.Ext(address))
}

// loadPreview returns a mod's picture, downloading it once.
func loadPreview(game Game, address string) (image.Image, error) {
	file := previewCache(game, address)
	data, err := os.ReadFile(file)
	if err != nil {
		var body bytes.Buffer
		if err := fetch(address, &body); err != nil {
			return nil, err
		}
		data = body.Bytes()
		if os.MkdirAll(filepath.Dir(file), 0o755) == nil {
			os.WriteFile(file, data, 0o644)
		}
	}
	picture, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("the preview cannot be shown (%s)", strings.TrimPrefix(filepath.Ext(address), "."))
	}
	return picture, nil
}

// fit returns the largest size with the picture's aspect ratio inside width x height.
func fit(bounds image.Rectangle, width, height int) (int, int) {
	w, h := bounds.Dx(), bounds.Dy()
	if w <= 0 || h <= 0 || width <= 0 || height <= 0 {
		return 0, 0
	}
	if w*height > h*width {
		return width, max(1, h*width/w)
	}
	return max(1, w*height/h), height
}

// scale resizes by averaging the source pixels under each target pixel (box filter), which
// keeps small previews from shimmering the way nearest-neighbour sampling does.
func scale(src image.Image, width, height int) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, width, height))
	b := src.Bounds()
	for y := 0; y < height; y++ {
		y0, y1 := b.Min.Y+y*b.Dy()/height, b.Min.Y+(y+1)*b.Dy()/height
		y1 = max(y1, y0+1)
		for x := 0; x < width; x++ {
			x0, x1 := b.Min.X+x*b.Dx()/width, b.Min.X+(x+1)*b.Dx()/width
			x1 = max(x1, x0+1)
			var r, g, bl, n uint32
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					cr, cg, cb, _ := src.At(sx, sy).RGBA()
					r, g, bl, n = r+cr, g+cg, bl+cb, n+1
				}
			}
			out.SetRGBA(x, y, color.RGBA{uint8(r / n >> 8), uint8(g / n >> 8), uint8(bl / n >> 8), 255})
		}
	}
	return out
}

// blockLines draws the picture in at most cols x rows character cells with "▀".
func blockLines(picture image.Image, cols, rows int) [][]seg {
	width, height := fit(picture.Bounds(), cols, rows*2)
	if width == 0 {
		return nil
	}
	height += height % 2
	pixels := scale(picture, width, height)
	lines := make([][]seg, 0, height/2)
	for y := 0; y < height; y += 2 {
		line := make([]seg, 0, width)
		for x := 0; x < width; x++ {
			top, bottom := pixels.RGBAAt(x, y), pixels.RGBAAt(x, y+1)
			line = append(line, seg{"▀", fmt.Sprintf("\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm",
				top.R, top.G, top.B, bottom.R, bottom.G, bottom.B)})
		}
		lines = append(lines, line)
	}
	return lines
}

// sixelPalette is 6 levels of red and blue and 7 of green (252 colours), indexed directly.
func sixelIndex(c color.RGBA) int {
	return int(c.R)*6/256*42 + int(c.G)*7/256*6 + int(c.B)*6/256
}

func sixelLevel(index, levels int) int { return (index*100 + (levels-1)/2) / (levels - 1) }

// encodeSixel writes the picture as a sixel image of width x height pixels.
func encodeSixel(picture image.Image, width, height int) string {
	pixels := scale(picture, width, height)
	var b strings.Builder
	b.WriteString("\x1bP0;1;0q") // keep the background, square pixels
	fmt.Fprintf(&b, "\"1;1;%d;%d", width, height)
	for i := 0; i < 252; i++ {
		fmt.Fprintf(&b, "#%d;2;%d;%d;%d", i, sixelLevel(i/42, 6), sixelLevel(i/6%7, 7), sixelLevel(i%6, 6))
	}
	indexes := make([]int, width*height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			indexes[y*width+x] = sixelIndex(pixels.RGBAAt(x, y))
		}
	}
	for band := 0; band < height; band += 6 {
		used := map[int]bool{}
		for y := band; y < min(band+6, height); y++ {
			for x := 0; x < width; x++ {
				used[indexes[y*width+x]] = true
			}
		}
		first := true
		for colour := 0; colour < 252; colour++ {
			if !used[colour] {
				continue
			}
			if !first {
				b.WriteByte('$') // back to the start of the band for the next colour
			}
			first = false
			fmt.Fprintf(&b, "#%d", colour)
			run, last := 0, byte(0)
			flush := func() {
				switch {
				case run > 3:
					fmt.Fprintf(&b, "!%d%c", run, last)
				case run > 0:
					b.WriteString(strings.Repeat(string(last), run))
				}
			}
			for x := 0; x < width; x++ {
				bits := 0
				for row := 0; row < 6 && band+row < height; row++ {
					if indexes[(band+row)*width+x] == colour {
						bits |= 1 << row
					}
				}
				char := byte(63 + bits)
				if char == last {
					run++
					continue
				}
				flush()
				run, last = 1, char
			}
			flush()
		}
		b.WriteByte('-')
	}
	b.WriteString("\x1b\\")
	return b.String()
}

// encodeKitty sends the picture as PNG over the kitty graphics protocol, shown in cols x rows
// cells at the cursor. The image id lets the next picture replace it.
func encodeKitty(picture image.Image, width, height, cols, rows int) string {
	var data bytes.Buffer
	png.Encode(&data, scale(picture, width, height))
	encoded := base64.StdEncoding.EncodeToString(data.Bytes())
	var b strings.Builder
	b.WriteString("\x1b_Ga=d,d=i,i=7,q=2\x1b\\") // remove the previous preview
	for start := 0; start < len(encoded); start += 4096 {
		end := min(start+4096, len(encoded))
		more := 0
		if end < len(encoded) {
			more = 1
		}
		if start == 0 {
			fmt.Fprintf(&b, "\x1b_Ga=T,f=100,i=7,q=2,C=1,c=%d,r=%d,m=%d;%s\x1b\\", cols, rows, more, encoded[start:end])
		} else {
			fmt.Fprintf(&b, "\x1b_Gm=%d;%s\x1b\\", more, encoded[start:end])
		}
	}
	return b.String()
}
