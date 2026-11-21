package source2

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
)

// More texture formats, as compiled textures number them: the game's interface images (hero
// cards, portraits, ability icons) are uncompressed BGRA, 4 bytes a pixel.
const (
	textureRGBA8888 = 4
	textureBGRA8888 = 28
)

// TextureInfo is what a compiled texture's header says about its pixels.
type TextureInfo struct {
	Width, Height, Mips int
	Format              int
	Flags               uint16
}

// TextureHeader reads a compiled texture's size and format from its first bytes (see
// VPKIndex.ReadHead); the pixels are not needed. Editable says whether ReplaceTextureImage can
// write it.
func TextureHeader(head []byte) (info TextureInfo, editable bool, err error) {
	r, err := DecodeResource(head)
	if err != nil {
		return info, false, err
	}
	info, err = textureHeader(r)
	return info, err == nil && (info.Format == textureBGRA8888 || info.Format == textureRGBA8888), err
}

func textureHeader(r *Resource) (TextureInfo, error) {
	h := r.Block("DATA")
	if len(h) < 40 {
		return TextureInfo{}, errors.New("not a compiled texture")
	}
	info := TextureInfo{
		Width:  int(binary.LittleEndian.Uint16(h[20:])),
		Height: int(binary.LittleEndian.Uint16(h[22:])),
		Format: int(h[26]),
		Mips:   max(1, int(h[27])),
		Flags:  binary.LittleEndian.Uint16(h[2:]),
	}
	if depth := binary.LittleEndian.Uint16(h[24:]); depth > 1 {
		return info, errors.New("3D textures are not supported")
	}
	return info, nil
}

// mipSizes lists each mip level's size, largest first, halving and rounding down.
func mipSizes(w, h, mips int) [][2]int {
	var out [][2]int
	for i := 0; i < mips; i++ {
		out = append(out, [2]int{w, h})
		w, h = max(1, w/2), max(1, h/2)
	}
	return out
}

func blockBytes(format int) int {
	switch format {
	case textureDXT1, textureATI1N:
		return 8
	case textureDXT5, textureBC7, textureATI2N:
		return 16
	}
	return 0
}

// mipBytes is the size of one mip level in a format.
func mipBytes(format, w, h int) (int, error) {
	switch format {
	case textureRGBA8888, textureBGRA8888:
		return w * h * 4, nil
	case textureDXT1, textureDXT5, textureBC7, textureATI2N, textureATI1N:
		return ((w + 3) / 4) * ((h + 3) / 4) * blockBytes(format), nil
	}
	return 0, fmt.Errorf("texture format %d is not supported", format)
}

func hasCompressedMips(h []byte) bool {
	if len(h) < 40 {
		return false
	}
	extraCount := int(binary.LittleEndian.Uint32(h[36:]))
	if extraCount <= 0 {
		return false
	}
	extraOff := 32 + int(binary.LittleEndian.Uint32(h[32:]))
	for i := 0; i < extraCount && extraOff+12*i+12 <= len(h); i++ {
		if binary.LittleEndian.Uint32(h[extraOff+12*i:]) == 4 {
			return true
		}
	}
	return false
}

// DecodeTexture reads the largest mip level of a compiled texture: uncompressed RGBA or BGRA
// (interface images), or block-compressed BC1 (DXT1), BC3 (DXT5), BC4 (ATI1N), BC5 (ATI2N), or BC7.
func DecodeTexture(data []byte) (image.Image, TextureInfo, error) {
	r, err := DecodeResource(data)
	if err != nil {
		return nil, TextureInfo{}, err
	}
	info, err := textureHeader(r)
	if err != nil {
		return nil, info, err
	}
	if hasCompressedMips(r.Block("DATA")) {
		return nil, info, errors.New("texture uses LZ4-compressed mips, which dlmod does not read")
	}
	size, err := mipBytes(info.Format, info.Width, info.Height)
	if err != nil {
		return nil, info, err
	}
	pixels := r.Streaming
	if len(pixels) < size {
		return nil, info, errors.New("the texture's pixels are missing; it may be compressed in a way dlmod does not read")
	}
	top := pixels[len(pixels)-size:] // the smallest mip comes first, the largest last
	img := image.NewNRGBA(image.Rect(0, 0, info.Width, info.Height))
	switch info.Format {
	case textureRGBA8888:
		copy(img.Pix, top)
	case textureBGRA8888:
		for i := 0; i < len(img.Pix); i += 4 {
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = top[i+2], top[i+1], top[i], top[i+3]
		}
	default:
		decodeBlocks(top, img, info.Format)
	}
	return img, info, nil
}

func decodeBlocks(data []byte, img *image.NRGBA, format int) {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	bw, size := (w+3)/4, blockBytes(format)
	var decoded [16]color.NRGBA
	for b := 0; (b+1)*size <= len(data); b++ {
		block := data[b*size : (b+1)*size]
		switch format {
		case textureATI1N:
			decodeBC4Block(block, &decoded)
		case textureATI2N:
			decodeBC5Block(block, &decoded)
		case textureBC7:
			decodeBC7Block(block, &decoded)
		default:
			var alpha [16]uint8
			for i := range alpha {
				alpha[i] = 255
			}
			cblock := block
			if format == textureDXT5 {
				alpha = decodeBC4Channel(block[:8])
				cblock = block[8:]
			}
			c0, c1 := binary.LittleEndian.Uint16(cblock), binary.LittleEndian.Uint16(cblock[2:])
			p0, p1 := from565(c0), from565(c1)
			palette := [4][3]float64{p0, p1}
			transparent := format == textureDXT1 && c0 <= c1
			for k := 0; k < 3; k++ {
				if !transparent {
					palette[2][k] = (2*p0[k] + p1[k]) / 3
					palette[3][k] = (p0[k] + 2*p1[k]) / 3
				} else {
					palette[2][k] = (p0[k] + p1[k]) / 2
				}
			}
			indices := binary.LittleEndian.Uint32(cblock[4:])
			for i := 0; i < 16; i++ {
				idx := indices >> (2 * i) & 3
				a := alpha[i]
				if transparent && idx == 3 {
					a = 0
				}
				c := palette[idx]
				decoded[i] = color.NRGBA{uint8(math.Round(c[0])), uint8(math.Round(c[1])), uint8(math.Round(c[2])), a}
			}
		}
		bx, by := (b%bw)*4, (b/bw)*4
		for i := 0; i < 16; i++ {
			x, y := bx+i%4, by+i/4
			if x >= w || y >= h {
				continue
			}
			img.SetNRGBA(x, y, decoded[i])
		}
	}
}

// ReplaceTextureImage writes an image into an uncompressed interface texture (a hero card, a
// portrait, an ability icon) in the original's own size, format and number of mip levels, and
// keeps its headers, so the game finds exactly the file it expects with other pixels. An image of
// another shape is scaled to cover the original's and cropped in the middle.
func ReplaceTextureImage(original []byte, img image.Image) ([]byte, error) {
	r, err := DecodeResource(original)
	if err != nil {
		return nil, err
	}
	info, err := textureHeader(r)
	if err != nil {
		return nil, err
	}
	if info.Format != textureBGRA8888 && info.Format != textureRGBA8888 {
		return nil, fmt.Errorf("this image is stored as texture format %d, which dlmod cannot write yet", info.Format)
	}
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return nil, errors.New("the image is empty")
	}
	// Cover: crop the image to the target's shape around its middle, then scale.
	target := float64(info.Width) / float64(info.Height)
	crop := b
	if float64(b.Dx())/float64(b.Dy()) > target {
		w := int(math.Round(float64(b.Dy()) * target))
		crop = image.Rect(b.Min.X+(b.Dx()-w)/2, b.Min.Y, b.Min.X+(b.Dx()-w)/2+w, b.Max.Y)
	} else {
		h := int(math.Round(float64(b.Dx()) / target))
		crop = image.Rect(b.Min.X, b.Min.Y+(b.Dy()-h)/2, b.Max.X, b.Min.Y+(b.Dy()-h)/2+h)
	}
	level := resample(subImage(img, crop), info.Width, info.Height, [4]float32{1, 1, 1, 1})
	var mips [][]byte
	w, h := info.Width, info.Height
	for i := 0; i < info.Mips; i++ {
		out := make([]byte, len(level))
		copy(out, level)
		if info.Format == textureBGRA8888 {
			for k := 0; k < len(out); k += 4 {
				out[k], out[k+2] = out[k+2], out[k]
			}
		}
		mips = append(mips, out)
		if i+1 < info.Mips {
			level, w, h = halve(level, w, h)
		}
	}
	var streaming []byte
	for i := len(mips) - 1; i >= 0; i-- {
		streaming = append(streaming, mips[i]...)
	}
	blocks := make([]Block, len(r.Blocks))
	copy(blocks, r.Blocks)
	return (&Resource{Version: r.Version, Blocks: blocks, Streaming: streaming}).Encode(), nil
}

// Thumbnail scales an image down to fit within size x size, for previews.
func Thumbnail(img image.Image, size int) image.Image {
	b := img.Bounds()
	if b.Dx() <= size && b.Dy() <= size {
		return img
	}
	scale := math.Min(float64(size)/float64(b.Dx()), float64(size)/float64(b.Dy()))
	w, h := max(1, int(float64(b.Dx())*scale)), max(1, int(float64(b.Dy())*scale))
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	copy(out.Pix, resample(img, w, h, [4]float32{1, 1, 1, 1}))
	return out
}

// subImage is the part of an image inside rect, whatever its type.
func subImage(img image.Image, rect image.Rectangle) image.Image {
	if s, ok := img.(interface {
		SubImage(image.Rectangle) image.Image
	}); ok {
		return s.SubImage(rect)
	}
	out := image.NewNRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	for y := 0; y < rect.Dy(); y++ {
		for x := 0; x < rect.Dx(); x++ {
			out.Set(x, y, img.At(rect.Min.X+x, rect.Min.Y+y))
		}
	}
	return out
}
