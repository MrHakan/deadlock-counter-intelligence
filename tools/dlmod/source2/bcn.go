package source2

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
)

// Additional compiled texture formats used by Deadlock's materials and particle effects.
const (
	textureBC7   = 20 // BC7 (BPTC): 16 bytes per 4x4 block, high-quality RGBA
	textureATI2N = 21 // BC5 (RGTC2): 16 bytes per 4x4 block, two independent 8-bit channels
	textureATI1N = 27 // BC4 (RGTC1): 8 bytes per 4x4 block, one 8-bit channel
)

// TextureFormatName returns a human-readable name for a Source 2 VTex format number.
func TextureFormatName(format int) string {
	switch format {
	case textureDXT1:
		return "DXT1"
	case textureDXT5:
		return "DXT5"
	case textureRGBA8888:
		return "RGBA8888"
	case textureBC7:
		return "BC7"
	case textureATI2N:
		return "BC5"
	case textureATI1N:
		return "BC4"
	case textureBGRA8888:
		return "BGRA8888"
	default:
		return fmt.Sprintf("format(%d)", format)
	}
}

// decodeBC4Channel decodes one 8-byte BC4 (RGTC1 / BC3 alpha) block into 16 single-channel values.
func decodeBC4Channel(block []byte) [16]uint8 {
	a0, a1 := int(block[0]), int(block[1])
	var lut [8]uint8
	lut[0], lut[1] = uint8(a0), uint8(a1)
	if a0 > a1 {
		for i := 1; i <= 6; i++ {
			lut[i+1] = uint8(((7-i)*a0 + i*a1 + 3) / 7)
		}
	} else {
		for i := 1; i <= 4; i++ {
			lut[i+1] = uint8(((5-i)*a0 + i*a1 + 2) / 5)
		}
		lut[6] = 0
		lut[7] = 255
	}
	var bits uint64
	for k := 0; k < 6; k++ {
		bits |= uint64(block[2+k]) << (8 * k)
	}
	var out [16]uint8
	for i := range out {
		out[i] = lut[(bits>>(3*i))&7]
	}
	return out
}

func decodeBC4Block(block []byte, out *[16]color.NRGBA) {
	v := decodeBC4Channel(block)
	for i := 0; i < 16; i++ {
		out[i] = color.NRGBA{v[i], v[i], v[i], 255}
	}
}

func decodeBC5Block(block []byte, out *[16]color.NRGBA) {
	r := decodeBC4Channel(block[:8])
	g := decodeBC4Channel(block[8:16])
	for i := 0; i < 16; i++ {
		out[i] = color.NRGBA{r[i], g[i], 0, 255}
	}
}

// BC7 (BPTC) decoder per the Khronos Data Format / Direct3D 11 specification.

type bc7Mode struct {
	subsets       int // NS: 1, 2, or 3
	partitionBits int // PB: 0, 4, or 6
	rotationBits  int // RB: 0 or 2
	indexSelBits  int // ISB: 0 or 1
	colorBits     int // CB: 4, 5, 6, or 7
	alphaBits     int // AB: 0, 5, 6, 7, or 8
	endpointPBits bool
	sharedPBits   bool
	indexBits     int // IB: 2, 3, or 4
	indexBits2    int // IB2: 0, 2, or 3
}

var bc7Modes = [8]bc7Mode{
	{subsets: 3, partitionBits: 4, colorBits: 4, endpointPBits: true, indexBits: 3},
	{subsets: 2, partitionBits: 6, colorBits: 6, sharedPBits: true, indexBits: 3},
	{subsets: 3, partitionBits: 6, colorBits: 5, indexBits: 2},
	{subsets: 2, partitionBits: 6, colorBits: 7, endpointPBits: true, indexBits: 2},
	{subsets: 1, rotationBits: 2, indexSelBits: 1, colorBits: 5, alphaBits: 6, indexBits: 2, indexBits2: 3},
	{subsets: 1, rotationBits: 2, colorBits: 7, alphaBits: 8, indexBits: 2, indexBits2: 2},
	{subsets: 1, colorBits: 7, alphaBits: 7, endpointPBits: true, indexBits: 4},
	{subsets: 2, partitionBits: 6, colorBits: 5, alphaBits: 5, endpointPBits: true, indexBits: 2},
}

var (
	bc7Weight2 = [4]int{0, 21, 43, 64}
	bc7Weight3 = [8]int{0, 9, 18, 27, 37, 46, 55, 64}
	bc7Weight4 = [16]int{0, 4, 9, 13, 17, 21, 26, 30, 34, 38, 43, 47, 51, 55, 60, 64}
)

// bc7Partitions2 maps each of the 64 2-subset partition indices to subset IDs (0 or 1) for the 16 texels.
var bc7Partitions2 = [64][16]uint8{
	{0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1},
	{0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1},
	{0, 1, 1, 1, 0, 1, 1, 1, 0, 1, 1, 1, 0, 1, 1, 1},
	{0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 1, 1, 1},
	{0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 1, 1},
	{0, 0, 1, 1, 0, 1, 1, 1, 0, 1, 1, 1, 1, 1, 1, 1},
	{0, 0, 0, 1, 0, 0, 1, 1, 0, 1, 1, 1, 1, 1, 1, 1},
	{0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 1, 1, 0, 1, 1, 1},
	{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 1, 1},
	{0, 0, 1, 1, 0, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
	{0, 0, 0, 0, 0, 0, 0, 1, 0, 1, 1, 1, 1, 1, 1, 1},
	{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 1, 1, 1},
	{0, 0, 0, 1, 0, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
	{0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1},
	{0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
	{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1},
	{0, 0, 0, 0, 1, 0, 0, 0, 1, 1, 1, 0, 1, 1, 1, 1},
	{0, 1, 1, 1, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0},
	{0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 1, 1, 1, 0},
	{0, 1, 1, 1, 0, 0, 1, 1, 0, 0, 0, 1, 0, 0, 0, 0},
	{0, 0, 1, 1, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0},
	{0, 0, 0, 0, 1, 0, 0, 0, 1, 1, 0, 0, 1, 1, 1, 0},
	{0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 1, 1, 0, 0},
	{0, 1, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 0, 1},
	{0, 0, 1, 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 0},
	{0, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1, 1, 0, 0},
	{0, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1, 0},
	{0, 0, 1, 1, 0, 1, 1, 0, 0, 1, 1, 0, 1, 1, 0, 0},
	{0, 0, 0, 1, 0, 1, 1, 1, 1, 1, 1, 0, 1, 0, 0, 0},
	{0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0},
	{0, 1, 1, 1, 0, 0, 0, 1, 1, 0, 0, 0, 1, 1, 1, 0},
	{0, 0, 1, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1, 1, 0, 0},
	{0, 1, 0, 1, 0, 1, 0, 1, 0, 1, 0, 1, 0, 1, 0, 1},
	{0, 0, 0, 0, 1, 1, 1, 1, 0, 0, 0, 0, 1, 1, 1, 1},
	{0, 1, 0, 1, 1, 0, 1, 0, 0, 1, 0, 1, 1, 0, 1, 0},
	{0, 0, 1, 1, 0, 0, 1, 1, 1, 1, 0, 0, 1, 1, 0, 0},
	{0, 0, 1, 1, 1, 1, 0, 0, 0, 0, 1, 1, 1, 1, 0, 0},
	{0, 1, 0, 1, 0, 1, 0, 1, 1, 0, 1, 0, 1, 0, 1, 0},
	{0, 1, 1, 0, 1, 0, 0, 1, 0, 1, 1, 0, 1, 0, 0, 1},
	{0, 1, 0, 1, 1, 0, 1, 0, 1, 0, 1, 0, 0, 1, 0, 1},
	{0, 1, 1, 1, 0, 0, 1, 1, 1, 1, 0, 0, 1, 1, 1, 0},
	{0, 0, 0, 1, 0, 0, 1, 1, 1, 1, 0, 0, 1, 0, 0, 0},
	{0, 0, 1, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 1, 0, 0},
	{0, 0, 1, 1, 1, 0, 1, 1, 1, 1, 0, 1, 1, 1, 0, 0},
	{0, 1, 1, 0, 1, 0, 0, 1, 1, 0, 0, 1, 0, 1, 1, 0},
	{0, 0, 1, 1, 1, 1, 0, 0, 1, 1, 0, 0, 0, 0, 1, 1},
	{0, 1, 1, 0, 0, 1, 1, 0, 1, 0, 0, 1, 1, 0, 0, 1},
	{0, 0, 0, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 0, 0, 0},
	{0, 1, 0, 0, 1, 1, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0},
	{0, 0, 1, 0, 0, 1, 1, 1, 0, 0, 1, 0, 0, 0, 0, 0},
	{0, 0, 0, 0, 0, 0, 1, 0, 0, 1, 1, 1, 0, 0, 1, 0},
	{0, 0, 0, 0, 0, 1, 0, 0, 1, 1, 1, 0, 0, 1, 0, 0},
	{0, 1, 1, 0, 1, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 1},
	{0, 0, 1, 1, 0, 1, 1, 0, 1, 1, 0, 0, 1, 0, 0, 1},
	{0, 1, 1, 0, 0, 0, 1, 1, 1, 0, 0, 1, 1, 1, 0, 0},
	{0, 0, 1, 1, 1, 0, 0, 1, 1, 1, 0, 0, 0, 1, 1, 0},
	{0, 1, 1, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1, 0, 0, 1},
	{0, 1, 1, 0, 0, 0, 1, 1, 0, 0, 1, 1, 1, 0, 0, 1},
	{0, 1, 1, 1, 1, 1, 1, 0, 1, 0, 0, 0, 0, 0, 0, 1},
	{0, 0, 0, 1, 1, 0, 0, 0, 1, 1, 1, 0, 0, 1, 1, 1},
	{0, 0, 0, 0, 1, 1, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1},
	{0, 0, 1, 1, 0, 0, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0},
	{0, 0, 1, 0, 0, 0, 1, 0, 1, 1, 1, 0, 1, 1, 1, 0},
	{0, 1, 0, 0, 0, 1, 0, 0, 0, 1, 1, 1, 0, 1, 1, 1},
}

// bc7Partitions3 maps each of the 64 3-subset partition indices to subset IDs (0, 1, or 2).
var bc7Partitions3 = [64][16]uint8{
	{0, 0, 1, 1, 0, 0, 1, 1, 0, 2, 2, 1, 2, 2, 2, 2},
	{0, 0, 0, 1, 0, 0, 1, 1, 2, 2, 1, 1, 2, 2, 2, 1},
	{0, 0, 0, 0, 2, 0, 0, 1, 2, 2, 1, 1, 2, 2, 1, 1},
	{0, 2, 2, 2, 0, 0, 2, 2, 0, 0, 1, 1, 0, 1, 1, 1},
	{0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 2, 2, 1, 1, 2, 2},
	{0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 2, 2, 0, 0, 2, 2},
	{0, 0, 2, 2, 0, 0, 2, 2, 1, 1, 1, 1, 1, 1, 1, 1},
	{0, 0, 1, 1, 0, 0, 1, 1, 2, 2, 1, 1, 2, 2, 1, 1},
	{0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2},
	{0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1, 2, 2, 2, 2},
	{0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2, 2, 2, 2, 2},
	{0, 0, 1, 2, 0, 0, 1, 2, 0, 0, 1, 2, 0, 0, 1, 2},
	{0, 1, 1, 2, 0, 1, 1, 2, 0, 1, 1, 2, 0, 1, 1, 2},
	{0, 1, 2, 2, 0, 1, 2, 2, 0, 1, 2, 2, 0, 1, 2, 2},
	{0, 0, 1, 1, 0, 1, 1, 2, 1, 1, 2, 2, 1, 2, 2, 2},
	{0, 0, 1, 1, 2, 0, 0, 1, 2, 2, 0, 0, 2, 2, 2, 0},
	{0, 0, 0, 1, 0, 0, 1, 1, 0, 1, 1, 2, 1, 1, 2, 2},
	{0, 1, 1, 1, 0, 0, 1, 1, 2, 0, 0, 1, 2, 2, 0, 0},
	{0, 0, 0, 0, 1, 1, 2, 2, 1, 1, 2, 2, 1, 1, 2, 2},
	{0, 0, 2, 2, 0, 0, 2, 2, 0, 0, 2, 2, 1, 1, 1, 1},
	{0, 1, 1, 1, 0, 1, 1, 1, 0, 2, 2, 2, 0, 2, 2, 2},
	{0, 0, 0, 1, 0, 0, 0, 1, 2, 2, 2, 1, 2, 2, 2, 1},
	{0, 0, 0, 0, 0, 0, 1, 1, 0, 1, 2, 2, 0, 1, 2, 2},
	{0, 0, 0, 0, 1, 1, 0, 0, 2, 2, 1, 0, 2, 2, 1, 0},
	{0, 1, 2, 2, 0, 1, 2, 2, 0, 0, 1, 1, 0, 0, 0, 0},
	{0, 0, 1, 2, 0, 0, 1, 2, 1, 1, 2, 2, 2, 2, 2, 2},
	{0, 1, 1, 0, 1, 2, 2, 1, 1, 2, 2, 1, 0, 1, 1, 0},
	{0, 0, 0, 0, 0, 1, 1, 0, 1, 2, 2, 1, 1, 2, 2, 1},
	{0, 0, 2, 2, 1, 1, 0, 2, 1, 1, 0, 2, 0, 0, 2, 2},
	{0, 1, 1, 0, 0, 1, 1, 0, 2, 0, 0, 2, 2, 2, 2, 2},
	{0, 0, 1, 1, 0, 1, 2, 2, 0, 1, 2, 2, 0, 0, 1, 1},
	{0, 0, 0, 0, 2, 0, 0, 0, 2, 2, 1, 1, 2, 2, 2, 1},
	{0, 0, 0, 0, 0, 0, 0, 2, 1, 1, 2, 2, 1, 2, 2, 2},
	{0, 2, 2, 2, 0, 0, 2, 2, 0, 0, 1, 2, 0, 0, 1, 1},
	{0, 0, 1, 1, 0, 0, 1, 2, 0, 0, 2, 2, 0, 2, 2, 2},
	{0, 1, 2, 0, 0, 1, 2, 0, 0, 1, 2, 0, 0, 1, 2, 0},
	{0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2, 0, 0, 0, 0},
	{0, 1, 2, 0, 1, 2, 0, 1, 2, 0, 1, 2, 0, 1, 2, 0},
	{0, 1, 2, 0, 2, 0, 1, 2, 1, 2, 0, 1, 0, 1, 2, 0},
	{0, 0, 1, 1, 2, 2, 0, 0, 1, 1, 2, 2, 0, 0, 1, 1},
	{0, 0, 1, 1, 1, 1, 2, 2, 2, 2, 0, 0, 0, 0, 1, 1},
	{0, 1, 0, 1, 0, 1, 0, 1, 2, 2, 2, 2, 2, 2, 2, 2},
	{0, 0, 0, 0, 0, 0, 0, 0, 2, 1, 2, 1, 2, 1, 2, 1},
	{0, 0, 2, 2, 1, 1, 2, 2, 0, 0, 2, 2, 1, 1, 2, 2},
	{0, 0, 2, 2, 0, 0, 1, 1, 0, 0, 2, 2, 0, 0, 1, 1},
	{0, 2, 2, 0, 1, 2, 2, 1, 0, 2, 2, 0, 1, 2, 2, 1},
	{0, 1, 0, 1, 2, 2, 2, 2, 2, 2, 2, 2, 0, 1, 0, 1},
	{0, 0, 0, 0, 2, 1, 2, 1, 2, 1, 2, 1, 2, 1, 2, 1},
	{0, 1, 0, 1, 0, 1, 0, 1, 0, 1, 0, 1, 2, 2, 2, 2},
	{0, 2, 2, 2, 0, 1, 1, 1, 0, 2, 2, 2, 0, 1, 1, 1},
	{0, 0, 0, 2, 1, 1, 1, 2, 0, 0, 0, 2, 1, 1, 1, 2},
	{0, 0, 0, 0, 2, 1, 1, 2, 2, 1, 1, 2, 2, 1, 1, 2},
	{0, 2, 2, 2, 0, 1, 1, 1, 0, 1, 1, 1, 0, 2, 2, 2},
	{0, 0, 0, 2, 1, 1, 1, 2, 1, 1, 1, 2, 0, 0, 0, 2},
	{0, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 2, 2, 2, 2},
	{0, 0, 0, 0, 0, 0, 0, 0, 2, 1, 1, 2, 2, 1, 1, 2},
	{0, 1, 1, 0, 0, 1, 1, 0, 2, 2, 2, 2, 2, 2, 2, 2},
	{0, 0, 2, 2, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 2, 2},
	{0, 0, 2, 2, 1, 1, 2, 2, 1, 1, 2, 2, 0, 0, 2, 2},
	{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2, 1, 1, 2},
	{0, 0, 0, 2, 0, 0, 0, 1, 0, 0, 0, 2, 0, 0, 0, 1},
	{0, 2, 2, 2, 1, 2, 2, 2, 0, 2, 2, 2, 1, 2, 2, 2},
	{0, 1, 0, 1, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2},
	{0, 1, 1, 1, 2, 0, 1, 1, 2, 2, 0, 1, 2, 2, 2, 0},
}

// Anchor texel indices for subsets 1 and 2 (subset 0 is always anchored at texel 0).
var (
	bc7Anchor2 = [64]uint8{
		15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15,
		15, 2, 8, 2, 2, 8, 8, 15, 2, 8, 2, 2, 8, 8, 2, 2,
		15, 15, 6, 8, 2, 8, 15, 15, 2, 8, 2, 2, 2, 15, 15, 6,
		6, 2, 6, 8, 15, 15, 2, 2, 15, 15, 15, 15, 15, 2, 2, 15,
	}
	bc7Anchor3a = [64]uint8{
		3, 3, 15, 15, 8, 3, 15, 15, 8, 8, 6, 6, 6, 5, 3, 3,
		3, 3, 8, 15, 3, 3, 6, 10, 5, 8, 8, 6, 8, 5, 15, 15,
		8, 15, 3, 5, 6, 10, 8, 15, 15, 3, 15, 5, 15, 15, 15, 15,
		3, 15, 5, 5, 5, 8, 5, 10, 5, 10, 8, 13, 15, 12, 3, 3,
	}
	bc7Anchor3b = [64]uint8{
		15, 8, 8, 3, 15, 15, 3, 8, 15, 15, 15, 15, 15, 15, 15, 8,
		15, 8, 15, 3, 15, 8, 15, 8, 3, 15, 6, 10, 15, 15, 10, 8,
		15, 3, 15, 10, 10, 8, 9, 10, 6, 15, 8, 15, 3, 6, 6, 8,
		15, 3, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 3, 15, 15, 8,
	}
)

type bitReader struct {
	lo, hi uint64
	pos    uint
}

func newBitReader(b []byte) bitReader {
	return bitReader{
		lo: binary.LittleEndian.Uint64(b[0:8]),
		hi: binary.LittleEndian.Uint64(b[8:16]),
	}
}

func (r *bitReader) read(n uint) int {
	if n == 0 {
		return 0
	}
	var v uint64
	if r.pos+n <= 64 {
		v = (r.lo >> r.pos) & ((1 << n) - 1)
	} else if r.pos >= 64 {
		v = (r.hi >> (r.pos - 64)) & ((1 << n) - 1)
	} else {
		loBits := 64 - r.pos
		hiBits := n - loBits
		v = (r.lo >> r.pos) | ((r.hi & ((1 << hiBits) - 1)) << loBits)
	}
	r.pos += n
	return int(v)
}

func bc7Unquantize(val, bits int) int {
	return (val << (8 - bits)) | (val >> (2*bits - 8))
}

func bc7Interpolate(e0, e1, idx, bits int) uint8 {
	var w int
	switch bits {
	case 2:
		w = bc7Weight2[idx]
	case 3:
		w = bc7Weight3[idx]
	default:
		w = bc7Weight4[idx]
	}
	return uint8(((64-w)*e0 + w*e1 + 32) >> 6)
}

func decodeBC7Block(block []byte, out *[16]color.NRGBA) {
	if block[0] == 0 {
		for i := range out {
			out[i] = color.NRGBA{}
		}
		return
	}
	modeIdx := 0
	for (block[0]>>modeIdx)&1 == 0 {
		modeIdx++
	}
	m := bc7Modes[modeIdx]
	br := newBitReader(block)
	br.read(uint(modeIdx + 1))

	partition := br.read(uint(m.partitionBits))
	rotation := br.read(uint(m.rotationBits))
	indexSel := br.read(uint(m.indexSelBits))

	numEndpoints := m.subsets * 2
	var ep [6][4]int
	for c := 0; c < 3; c++ {
		for i := 0; i < numEndpoints; i++ {
			ep[i][c] = br.read(uint(m.colorBits))
		}
	}
	if m.alphaBits > 0 {
		for i := 0; i < numEndpoints; i++ {
			ep[i][3] = br.read(uint(m.alphaBits))
		}
	}
	colorBits := m.colorBits
	alphaBits := m.alphaBits
	if m.endpointPBits {
		for i := 0; i < numEndpoints; i++ {
			p := br.read(1)
			for c := 0; c < 3; c++ {
				ep[i][c] = (ep[i][c] << 1) | p
			}
			if m.alphaBits > 0 {
				ep[i][3] = (ep[i][3] << 1) | p
			}
		}
		colorBits++
		if m.alphaBits > 0 {
			alphaBits++
		}
	} else if m.sharedPBits {
		for s := 0; s < m.subsets; s++ {
			p := br.read(1)
			for e := 0; e < 2; e++ {
				for c := 0; c < 3; c++ {
					ep[s*2+e][c] = (ep[s*2+e][c] << 1) | p
				}
			}
		}
		colorBits++
	}

	for i := 0; i < numEndpoints; i++ {
		for c := 0; c < 3; c++ {
			ep[i][c] = bc7Unquantize(ep[i][c], colorBits)
		}
		if m.alphaBits > 0 {
			ep[i][3] = bc7Unquantize(ep[i][3], alphaBits)
		} else {
			ep[i][3] = 255
		}
	}

	isAnchor := func(i int) bool {
		if i == 0 {
			return true
		}
		if m.subsets == 2 {
			return i == int(bc7Anchor2[partition])
		}
		if m.subsets == 3 {
			return i == int(bc7Anchor3a[partition]) || i == int(bc7Anchor3b[partition])
		}
		return false
	}

	var idx1 [16]int
	for i := 0; i < 16; i++ {
		bits := m.indexBits
		if isAnchor(i) {
			bits--
		}
		idx1[i] = br.read(uint(bits))
	}
	var idx2 [16]int
	if m.indexBits2 > 0 {
		for i := 0; i < 16; i++ {
			bits := m.indexBits2
			if i == 0 {
				bits--
			}
			idx2[i] = br.read(uint(bits))
		}
	}

	for i := 0; i < 16; i++ {
		subset := 0
		if m.subsets == 2 {
			subset = int(bc7Partitions2[partition][i])
		} else if m.subsets == 3 {
			subset = int(bc7Partitions3[partition][i])
		}
		e0, e1 := ep[subset*2], ep[subset*2+1]
		var r, g, b, a uint8
		if m.indexBits2 == 0 {
			r = bc7Interpolate(e0[0], e1[0], idx1[i], m.indexBits)
			g = bc7Interpolate(e0[1], e1[1], idx1[i], m.indexBits)
			b = bc7Interpolate(e0[2], e1[2], idx1[i], m.indexBits)
			if m.alphaBits > 0 {
				a = bc7Interpolate(e0[3], e1[3], idx1[i], m.indexBits)
			} else {
				a = 255
			}
		} else {
			cIdx, cBits := idx1[i], m.indexBits
			aIdx, aBits := idx2[i], m.indexBits2
			if indexSel == 1 {
				cIdx, cBits, aIdx, aBits = aIdx, aBits, cIdx, cBits
			}
			r = bc7Interpolate(e0[0], e1[0], cIdx, cBits)
			g = bc7Interpolate(e0[1], e1[1], cIdx, cBits)
			b = bc7Interpolate(e0[2], e1[2], cIdx, cBits)
			a = bc7Interpolate(e0[3], e1[3], aIdx, aBits)
		}
		switch rotation {
		case 1:
			r, a = a, r
		case 2:
			g, a = a, g
		case 3:
			b, a = a, b
		}
		out[i] = color.NRGBA{r, g, b, a}
	}
}

// RepaintTexture writes a repainted image back into a compiled .vtex_c while preserving its
// dimensions, mip count, flags, sprite-sheet metadata, and non-DATA blocks. Uncompressed
// textures stay in their original format; block-compressed textures (DXT1, DXT5, BC7, BC4, BC5)
// are written as DXT1 (if opaque and originally DXT1) or DXT5, with updated linear reflectivity
// and 32x32 DXT5 fallback thumbnail when present.
func RepaintTexture(original []byte, img image.Image) ([]byte, error) {
	r, err := DecodeResource(original)
	if err != nil {
		return nil, err
	}
	info, err := textureHeader(r)
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return nil, errors.New("the image is empty")
	}
	var level []byte
	if b.Dx() == info.Width && b.Dy() == info.Height {
		level = make([]byte, info.Width*info.Height*4)
		for y := 0; y < info.Height; y++ {
			for x := 0; x < info.Width; x++ {
				c := color.NRGBAModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
				off := (y*info.Width + x) * 4
				level[off], level[off+1], level[off+2], level[off+3] = c.R, c.G, c.B, c.A
			}
		}
	} else {
		level = resample(img, info.Width, info.Height, [4]float32{1, 1, 1, 1})
	}

	opaque := true
	var sum [4]float64
	for i := 0; i < len(level); i += 4 {
		if level[i+3] < 250 {
			opaque = false
		}
		for k := 0; k < 3; k++ {
			sum[k] += math.Pow(float64(level[i+k])/255, 2.2)
		}
		sum[3] += float64(level[i+3]) / 255
	}
	pixels := float64(len(level) / 4)

	outFormat := info.Format
	switch info.Format {
	case textureRGBA8888, textureBGRA8888:
		// Keep uncompressed format.
	case textureDXT1:
		if !opaque {
			outFormat = textureDXT5
		}
	case textureDXT5, textureBC7, textureATI1N, textureATI2N:
		outFormat = textureDXT5
	default:
		return nil, fmt.Errorf("texture format %d is not supported for repainting", info.Format)
	}

	// Build mip chain (largest first, then pack smallest first).
	var mips [][]byte
	cur := level
	w, h := info.Width, info.Height
	for i := 0; i < info.Mips; i++ {
		switch outFormat {
		case textureRGBA8888:
			out := make([]byte, len(cur))
			copy(out, cur)
			mips = append(mips, out)
		case textureBGRA8888:
			out := make([]byte, len(cur))
			copy(out, cur)
			for k := 0; k < len(out); k += 4 {
				out[k], out[k+2] = out[k+2], out[k]
			}
			mips = append(mips, out)
		case textureDXT1:
			mips = append(mips, encodeBlocks(cur, w, h, textureDXT1, 8))
		case textureDXT5:
			mips = append(mips, encodeBlocks(cur, w, h, textureDXT5, 16))
		}
		if i+1 < info.Mips {
			cur, w, h = halve(cur, w, h)
		}
	}
	var streaming []byte
	for i := len(mips) - 1; i >= 0; i-- {
		streaming = append(streaming, mips[i]...)
	}

	// Copy and update the DATA block: format, reflectivity, and any 32x32 fallback thumbnail.
	blocks := make([]Block, len(r.Blocks))
	copy(blocks, r.Blocks)
	for i := range blocks {
		if blocks[i].Type != "DATA" {
			continue
		}
		hdr := append([]byte(nil), blocks[i].Data...)
		for k := 0; k < 4; k++ {
			binary.LittleEndian.PutUint32(hdr[4+4*k:], math.Float32bits(float32(sum[k]/pixels)))
		}
		hdr[26] = byte(outFormat)
		hdr[27] = byte(info.Mips)
		updateFallbackBits(hdr, img)
		blocks[i].Data = hdr
	}
	return (&Resource{Version: r.Version, Blocks: blocks, Streaming: streaming}).Encode(), nil
}

// updateFallbackBits refreshes the 32x32 DXT5 streaming fallback thumbnail (VTexExtraData type 1)
// inside a VTex DATA block if one is present.
func updateFallbackBits(hdr []byte, img image.Image) {
	if len(hdr) < 40 {
		return
	}
	extraCount := int(binary.LittleEndian.Uint32(hdr[36:]))
	if extraCount <= 0 {
		return
	}
	extraOff := 32 + int(binary.LittleEndian.Uint32(hdr[32:]))
	for i := 0; i < extraCount && extraOff+12*i+12 <= len(hdr); i++ {
		e := extraOff + 12*i
		et := binary.LittleEndian.Uint32(hdr[e:])
		if et != 1 {
			continue
		}
		eo := e + 4 + int(binary.LittleEndian.Uint32(hdr[e+4:]))
		es := int(binary.LittleEndian.Uint32(hdr[e+8:]))
		if es == 1024 && eo >= 0 && eo+1024 <= len(hdr) {
			thumb := resample(img, 32, 32, [4]float32{1, 1, 1, 1})
			copy(hdr[eo:eo+1024], encodeBlocks(thumb, 32, 32, textureDXT5, 16))
		}
	}
}
