package source2

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	_ "image/jpeg" // decoders for the textures models carry
	_ "image/png"
	"math"
)

// Texture formats of compiled textures dlmod writes.
const (
	textureDXT1 = 1 // BC1: 4 bits per pixel, no alpha
	textureDXT5 = 2 // BC3: 8 bits per pixel, with alpha
)

// DecodeImage reads a PNG or JPEG texture.
func DecodeImage(data []byte) (image.Image, error) {
	if len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return nil, errors.New("WebP textures are not supported: export the model with PNG or JPEG textures")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, err
}

// SolidImage is a small image of one colour, for parts without a texture.
func SolidImage(c [4]float32) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	to8 := func(f float32) uint8 { return uint8(math.Round(math.Max(0, math.Min(1, float64(f))) * 255)) }
	for i := 0; i < 16; i++ {
		img.SetNRGBA(i%4, i/4, color.NRGBA{to8(c[0]), to8(c[1]), to8(c[2]), to8(c[3])})
	}
	return img
}

// CompileTexture turns an image into a compiled .vtex_c: block-compressed (BC1, or BC3 when it
// has transparency), a power of two on each side no larger than maxSize, with every mip level.
// tint multiplies the colours, as a material's base colour factor does.
func CompileTexture(img image.Image, maxSize int, tint [4]float32) ([]byte, error) {
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return nil, errors.New("the texture is empty")
	}
	width, height := powerOfTwo(b.Dx(), maxSize), powerOfTwo(b.Dy(), maxSize)
	level := resample(img, width, height, tint)
	opaque := true
	for i := 3; i < len(level); i += 4 {
		if level[i] < 250 {
			opaque = false
			break
		}
	}
	format, blockBytes := textureDXT1, 8
	if !opaque {
		format, blockBytes = textureDXT5, 16
	}
	var mips [][]byte
	var sum [4]float64
	for i := 0; i < len(level); i += 4 {
		for k := 0; k < 3; k++ {
			sum[k] += math.Pow(float64(level[i+k])/255, 2.2)
		}
	}
	pixels := float64(len(level) / 4)
	w, h := width, height
	for {
		mips = append(mips, encodeBlocks(level, w, h, format, blockBytes))
		if w <= 4 && h <= 4 || len(mips) >= 16 {
			break
		}
		level, w, h = halve(level, w, h)
	}
	header := make([]byte, 40)
	binary.LittleEndian.PutUint16(header, 1) // version
	for k := 0; k < 3; k++ {                 // reflectivity: the average colour, linear
		binary.LittleEndian.PutUint32(header[4+4*k:], math.Float32bits(float32(sum[k]/pixels)))
	}
	binary.LittleEndian.PutUint16(header[20:], uint16(width))
	binary.LittleEndian.PutUint16(header[22:], uint16(height))
	binary.LittleEndian.PutUint16(header[24:], 1)
	header[26] = byte(format)
	header[27] = byte(len(mips))
	var streaming []byte
	for i := len(mips) - 1; i >= 0; i-- { // the smallest mip comes first
		streaming = append(streaming, mips[i]...)
	}
	editInfo, err := WriteKV3(editInfoFor("CompileTexture", "Texture Compiler Version", 11, nil,
		NewObject().Set("IsChildResource", &Value{Kind: Int64, I: 1})), FormatGeneric)
	if err != nil {
		return nil, err
	}
	resource := &Resource{Version: 1, Blocks: []Block{{"RED2", editInfo}, {"DATA", header}}, Streaming: streaming}
	return resource.Encode(), nil
}

func powerOfTwo(n, limit int) int {
	if limit <= 0 {
		limit = 2048
	}
	p := 4
	for p < n && p < limit {
		p *= 2
	}
	return p
}

// resample scales an image to width x height (bilinear) as straight RGBA bytes.
func resample(img image.Image, width, height int, tint [4]float32) []byte {
	b := img.Bounds()
	src := image.NewNRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			src.Set(x, y, img.At(x, y))
		}
	}
	at := func(x, y int) []uint8 {
		x = max(0, min(b.Dx()-1, x))
		y = max(0, min(b.Dy()-1, y))
		i := y*src.Stride + x*4
		return src.Pix[i : i+4]
	}
	out := make([]byte, width*height*4)
	for y := 0; y < height; y++ {
		fy := (float64(y)+0.5)*float64(b.Dy())/float64(height) - 0.5
		y0 := int(math.Floor(fy))
		ty := fy - float64(y0)
		for x := 0; x < width; x++ {
			fx := (float64(x)+0.5)*float64(b.Dx())/float64(width) - 0.5
			x0 := int(math.Floor(fx))
			tx := fx - float64(x0)
			p00, p10, p01, p11 := at(x0, y0), at(x0+1, y0), at(x0, y0+1), at(x0+1, y0+1)
			for k := 0; k < 4; k++ {
				v := (float64(p00[k])*(1-tx)+float64(p10[k])*tx)*(1-ty) + (float64(p01[k])*(1-tx)+float64(p11[k])*tx)*ty
				v *= float64(tint[k])
				out[(y*width+x)*4+k] = uint8(math.Round(math.Max(0, math.Min(255, v))))
			}
		}
	}
	return out
}

// halve averages 2x2 pixels into one.
func halve(pix []byte, w, h int) ([]byte, int, int) {
	nw, nh := max(1, w/2), max(1, h/2)
	out := make([]byte, nw*nh*4)
	for y := 0; y < nh; y++ {
		for x := 0; x < nw; x++ {
			for k := 0; k < 4; k++ {
				total := 0
				for dy := 0; dy < 2; dy++ {
					for dx := 0; dx < 2; dx++ {
						sx, sy := min(w-1, 2*x+dx), min(h-1, 2*y+dy)
						total += int(pix[(sy*w+sx)*4+k])
					}
				}
				out[(y*nw+x)*4+k] = uint8((total + 2) / 4)
			}
		}
	}
	return out, nw, nh
}

// encodeBlocks compresses an image into 4x4 BC1 or BC3 blocks; edges repeat the last pixel.
func encodeBlocks(pix []byte, w, h, format, blockBytes int) []byte {
	bw, bh := (w+3)/4, (h+3)/4
	out := make([]byte, 0, bw*bh*blockBytes)
	var block [16][4]uint8
	for by := 0; by < bh; by++ {
		for bx := 0; bx < bw; bx++ {
			for i := 0; i < 16; i++ {
				x, y := min(w-1, bx*4+i%4), min(h-1, by*4+i/4)
				copy(block[i][:], pix[(y*w+x)*4:(y*w+x)*4+4])
			}
			if format == textureDXT5 {
				out = append(out, encodeAlpha(&block)...)
			}
			out = append(out, encodeColor(&block)...)
		}
	}
	return out
}

func to565(c [3]float64) uint16 {
	r := uint16(math.Round(math.Max(0, math.Min(255, c[0])) * 31 / 255))
	g := uint16(math.Round(math.Max(0, math.Min(255, c[1])) * 63 / 255))
	b := uint16(math.Round(math.Max(0, math.Min(255, c[2])) * 31 / 255))
	return r<<11 | g<<5 | b
}

func from565(v uint16) [3]float64 {
	r, g, b := float64(v>>11&31), float64(v>>5&63), float64(v&31)
	return [3]float64{r * 255 / 31, g * 255 / 63, b * 255 / 31}
}

// encodeColor writes a BC1 colour block: two endpoints along the colours' main direction and a
// 2-bit choice between them per pixel.
func encodeColor(block *[16][4]uint8) []byte {
	var mean [3]float64
	for _, p := range block {
		for k := 0; k < 3; k++ {
			mean[k] += float64(p[k]) / 16
		}
	}
	// The main direction of the colours, by a few rounds of power iteration on their covariance.
	var cov [3][3]float64
	for _, p := range block {
		d := [3]float64{float64(p[0]) - mean[0], float64(p[1]) - mean[1], float64(p[2]) - mean[2]}
		for i := 0; i < 3; i++ {
			for j := 0; j < 3; j++ {
				cov[i][j] += d[i] * d[j]
			}
		}
	}
	axis := [3]float64{1, 1, 1}
	for iter := 0; iter < 8; iter++ {
		next := [3]float64{}
		for i := 0; i < 3; i++ {
			for j := 0; j < 3; j++ {
				next[i] += cov[i][j] * axis[j]
			}
		}
		l := math.Sqrt(next[0]*next[0] + next[1]*next[1] + next[2]*next[2])
		if l < 1e-9 {
			break
		}
		axis = [3]float64{next[0] / l, next[1] / l, next[2] / l}
	}
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, p := range block {
		t := (float64(p[0])-mean[0])*axis[0] + (float64(p[1])-mean[1])*axis[1] + (float64(p[2])-mean[2])*axis[2]
		lo, hi = math.Min(lo, t), math.Max(hi, t)
	}
	end0 := to565([3]float64{mean[0] + axis[0]*hi, mean[1] + axis[1]*hi, mean[2] + axis[2]*hi})
	end1 := to565([3]float64{mean[0] + axis[0]*lo, mean[1] + axis[1]*lo, mean[2] + axis[2]*lo})
	if end0 < end1 {
		end0, end1 = end1, end0
	}
	out := make([]byte, 8)
	binary.LittleEndian.PutUint16(out, end0)
	binary.LittleEndian.PutUint16(out[2:], end1)
	if end0 == end1 {
		return out // every pixel takes endpoint 0
	}
	c0, c1 := from565(end0), from565(end1)
	palette := [4][3]float64{c0, c1}
	for k := 0; k < 3; k++ {
		palette[2][k] = (2*c0[k] + c1[k]) / 3
		palette[3][k] = (c0[k] + 2*c1[k]) / 3
	}
	var indices uint32
	for i, p := range block {
		best, bestDist := 0, math.Inf(1)
		for c, q := range palette {
			d := sq64(float64(p[0])-q[0]) + sq64(float64(p[1])-q[1]) + sq64(float64(p[2])-q[2])
			if d < bestDist {
				best, bestDist = c, d
			}
		}
		indices |= uint32(best) << (2 * i)
	}
	binary.LittleEndian.PutUint32(out[4:], indices)
	return out
}

// encodeAlpha writes a BC3 alpha block: two alpha endpoints and 3-bit choices among eight steps.
func encodeAlpha(block *[16][4]uint8) []byte {
	lo, hi := uint8(255), uint8(0)
	for _, p := range block {
		lo, hi = min(lo, p[3]), max(hi, p[3])
	}
	out := make([]byte, 8)
	out[0], out[1] = hi, lo
	if hi == lo {
		return out
	}
	var palette [8]float64
	palette[0], palette[1] = float64(hi), float64(lo)
	for i := 1; i < 7; i++ {
		palette[i+1] = (float64(7-i)*float64(hi) + float64(i)*float64(lo)) / 7
	}
	var bits uint64
	for i, p := range block {
		best, bestDist := 0, math.Inf(1)
		for c, q := range palette {
			if d := math.Abs(float64(p[3]) - q); d < bestDist {
				best, bestDist = c, d
			}
		}
		bits |= uint64(best) << (3 * i)
	}
	for k := 0; k < 6; k++ {
		out[2+k] = byte(bits >> (8 * k))
	}
	return out
}

func sq64(f float64) float64 { return f * f }
