package main

import (
	"fmt"
	"math"
	"strings"
	"unicode"
)

// A small stroke font for hero name logos: the hero select screen shows each hero's name as an
// SVG (panorama/images/heroes/hero_names/ivy.vsvg_c), drawn in white and tinted by the game.
// Glyphs are polylines on a grid 4 wide and 6 tall (y up); each stroke becomes a filled band with
// square ends, all wound the same way so overlaps merge, as the game's own logos are filled paths.

var strokeGlyphs = map[rune][][][2]float64{
	'A':  {{{0, 0}, {2, 6}, {4, 0}}, {{0.8, 2.2}, {3.2, 2.2}}},
	'B':  {{{0, 0}, {0, 6}, {2.8, 6}, {3.6, 5.2}, {3.6, 3.8}, {2.8, 3}, {0, 3}}, {{2.8, 3}, {4, 2}, {4, 0.8}, {3.2, 0}, {0, 0}}},
	'C':  {{{4, 5.2}, {3, 6}, {1, 6}, {0, 5}, {0, 1}, {1, 0}, {3, 0}, {4, 0.8}}},
	'D':  {{{0, 0}, {0, 6}, {2.5, 6}, {4, 4.5}, {4, 1.5}, {2.5, 0}, {0, 0}}},
	'E':  {{{4, 6}, {0, 6}, {0, 0}, {4, 0}}, {{0, 3}, {3, 3}}},
	'F':  {{{4, 6}, {0, 6}, {0, 0}}, {{0, 3}, {3, 3}}},
	'G':  {{{4, 5.2}, {3, 6}, {1, 6}, {0, 5}, {0, 1}, {1, 0}, {3, 0}, {4, 1}, {4, 2.8}, {2.2, 2.8}}},
	'H':  {{{0, 0}, {0, 6}}, {{4, 0}, {4, 6}}, {{0, 3}, {4, 3}}},
	'I':  {{{0.8, 6}, {3.2, 6}}, {{2, 6}, {2, 0}}, {{0.8, 0}, {3.2, 0}}},
	'J':  {{{1.5, 6}, {4, 6}}, {{3, 6}, {3, 1}, {2, 0}, {1, 0}, {0, 1}}},
	'K':  {{{0, 0}, {0, 6}}, {{4, 6}, {0, 2.5}}, {{1.4, 3.6}, {4, 0}}},
	'L':  {{{0, 6}, {0, 0}, {4, 0}}},
	'M':  {{{0, 0}, {0, 6}, {2, 3}, {4, 6}, {4, 0}}},
	'N':  {{{0, 0}, {0, 6}, {4, 0}, {4, 6}}},
	'O':  {{{1, 0}, {0, 1}, {0, 5}, {1, 6}, {3, 6}, {4, 5}, {4, 1}, {3, 0}, {1, 0}}},
	'P':  {{{0, 0}, {0, 6}, {3, 6}, {4, 5}, {4, 3.8}, {3, 2.8}, {0, 2.8}}},
	'Q':  {{{1, 0}, {0, 1}, {0, 5}, {1, 6}, {3, 6}, {4, 5}, {4, 1}, {3, 0}, {1, 0}}, {{2.5, 1.5}, {4.2, -0.4}}},
	'R':  {{{0, 0}, {0, 6}, {3, 6}, {4, 5}, {4, 3.8}, {3, 2.8}, {0, 2.8}}, {{2, 2.8}, {4, 0}}},
	'S':  {{{4, 5.2}, {3, 6}, {1, 6}, {0, 5}, {0, 4}, {1, 3}, {3, 3}, {4, 2}, {4, 1}, {3, 0}, {1, 0}, {0, 0.8}}},
	'T':  {{{0, 6}, {4, 6}}, {{2, 6}, {2, 0}}},
	'U':  {{{0, 6}, {0, 1}, {1, 0}, {3, 0}, {4, 1}, {4, 6}}},
	'V':  {{{0, 6}, {2, 0}, {4, 6}}},
	'W':  {{{0, 6}, {1, 0}, {2, 3.5}, {3, 0}, {4, 6}}},
	'X':  {{{0, 6}, {4, 0}}, {{0, 0}, {4, 6}}},
	'Y':  {{{0, 6}, {2, 3}, {4, 6}}, {{2, 3}, {2, 0}}},
	'Z':  {{{0, 6}, {4, 6}, {0, 0}, {4, 0}}},
	'0':  {{{1, 0}, {0, 1}, {0, 5}, {1, 6}, {3, 6}, {4, 5}, {4, 1}, {3, 0}, {1, 0}}, {{0.6, 1.2}, {3.4, 4.8}}},
	'1':  {{{0.8, 4.8}, {2, 6}, {2, 0}}, {{0.8, 0}, {3.2, 0}}},
	'2':  {{{0, 5}, {1, 6}, {3, 6}, {4, 5}, {4, 4}, {0, 0}, {4, 0}}},
	'3':  {{{0, 5.2}, {1, 6}, {3, 6}, {4, 5}, {4, 4}, {3, 3}, {1.5, 3}}, {{3, 3}, {4, 2}, {4, 1}, {3, 0}, {1, 0}, {0, 0.8}}},
	'4':  {{{3, 0}, {3, 6}, {0, 2}, {4, 2}}},
	'5':  {{{4, 6}, {0, 6}, {0, 3.2}, {3, 3.2}, {4, 2.2}, {4, 1}, {3, 0}, {0, 0}}},
	'6':  {{{3.8, 6}, {1, 6}, {0, 5}, {0, 1}, {1, 0}, {3, 0}, {4, 1}, {4, 2.2}, {3, 3.2}, {0, 3.2}}},
	'7':  {{{0, 6}, {4, 6}, {1.5, 0}}},
	'8':  {{{1, 3}, {0, 4}, {0, 5}, {1, 6}, {3, 6}, {4, 5}, {4, 4}, {3, 3}, {1, 3}, {0, 2}, {0, 1}, {1, 0}, {3, 0}, {4, 1}, {4, 2}, {3, 3}}},
	'9':  {{{0.2, 0}, {3, 0}, {4, 1}, {4, 5}, {3, 6}, {1, 6}, {0, 5}, {0, 3.8}, {1, 2.8}, {4, 2.8}}},
	'-':  {{{0.8, 3}, {3.2, 3}}},
	'.':  {{{1.7, 0}, {2.3, 0}}},
	'!':  {{{2, 6}, {2, 1.8}}, {{1.7, 0}, {2.3, 0}}},
	'?':  {{{0, 5}, {1, 6}, {3, 6}, {4, 5}, {4, 4}, {2, 2.6}, {2, 1.8}}, {{1.7, 0}, {2.3, 0}}},
	'&':  {{{4, 0}, {0.8, 4.4}, {0.8, 5.4}, {1.6, 6}, {2.6, 6}, {3, 5.2}, {0, 2}, {0, 0.8}, {1, 0}, {2.6, 0}, {4, 2}}},
	'\'': {{{2, 6}, {2, 4.6}}},
}

const (
	logoUnit    = 60.0 // pixels per grid unit
	logoStroke  = 1.15 // stroke width in grid units
	logoAdvance = 5.6  // grid units from one letter to the next
	logoSpace   = 3.0  // grid units a space adds
)

// textLogoSVG draws text as a filled white SVG name logo. Letters are upper-cased; characters the
// font has no glyph for are an error.
func textLogoSVG(text string) (string, error) {
	text = strings.ToUpper(strings.TrimSpace(text))
	if text == "" {
		return "", fmt.Errorf("the name is empty")
	}
	var bands []string
	x := 0.0
	for _, r := range text {
		if unicode.IsSpace(r) {
			x += logoSpace
			continue
		}
		strokes, ok := strokeGlyphs[r]
		if !ok {
			return "", fmt.Errorf("the logo font has no %q; use letters A-Z, digits and - . ! ? & ' (or pass -logo with your own SVG)", r)
		}
		for _, line := range strokes {
			for i := 0; i+1 < len(line); i++ {
				a := [2]float64{line[i][0] + x, line[i][1]}
				b := [2]float64{line[i+1][0] + x, line[i+1][1]}
				bands = append(bands, strokeBand(a, b))
			}
		}
		x += logoAdvance
	}
	pad := logoStroke
	width := (x - logoAdvance + 4 + 2*pad) * logoUnit
	height := (6 + 2*pad + 0.4) * logoUnit
	var sb strings.Builder
	fmt.Fprintf(&sb, `<svg width="%.0f" height="%.0f" viewBox="0 0 %.0f %.0f" fill="none" xmlns="http://www.w3.org/2000/svg">`+"\n", width, height, width, height)
	fmt.Fprintf(&sb, `<path transform="translate(%.1f %.1f)" d="%s" fill="white"/>`+"\n", pad*logoUnit, (6+pad)*logoUnit, strings.Join(bands, " "))
	sb.WriteString("</svg>\n")
	return sb.String(), nil
}

// strokeBand is a stroke from a to b as a closed quad with square ends, in pixels with y down
// (the path is placed by a translate). Every band winds the same way, so overlaps merge.
func strokeBand(a, b [2]float64) string {
	dx, dy := b[0]-a[0], b[1]-a[1]
	l := math.Hypot(dx, dy)
	if l < 1e-9 {
		dx, dy, l = 1, 0, 1
	}
	dx, dy = dx/l, dy/l
	nx, ny := -dy, dx
	h := logoStroke / 2
	corner := func(p [2]float64, along, side float64) string {
		x := (p[0] + dx*along + nx*side) * logoUnit
		y := -(p[1] + dy*along + ny*side) * logoUnit
		return fmt.Sprintf("%.1f %.1f", x, y)
	}
	return "M" + corner(a, -h, h) + "L" + corner(b, h, h) + "L" + corner(b, h, -h) + "L" + corner(a, -h, -h) + "Z"
}
