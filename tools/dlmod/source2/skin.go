package source2

import (
	"encoding/binary"
	"fmt"
	"math"
)

// More DXGI formats skinned meshes use: newer hero models carry 8 bone weights per vertex, one
// byte each, in 8-byte attributes.
const (
	FormatR32G32B32A32Sint  = 4
	FormatR16G16B16A16Unorm = 11
)

// MaxInfluences is the most bones a vertex can follow.
const MaxInfluences = 8

// Influence is one bone a vertex follows and how much.
type Influence struct {
	Bone   int // index into the mesh's Bones
	Weight float32
}

// Skin reads every vertex's bone influences, strongest first. A buffer without bone weights
// follows its first bone index fully, or bone 0 when it has no bone indices either.
func (b *VertexBuffer) Skin() ([][]Influence, error) {
	out := make([][]Influence, b.Count)
	indices := b.Attribute("blendindices", 0)
	weights := b.Attribute("blendweights", 0)
	for i := range out {
		row := b.Data[i*b.Stride:]
		var bones [MaxInfluences]int
		var amounts [MaxInfluences]float32
		count := 0
		if indices != nil {
			p := row[indices.Offset:]
			switch indices.Format {
			case FormatR8G8B8A8Uint:
				count = 4
				for k := 0; k < 4; k++ {
					bones[k] = int(p[k])
				}
			case FormatR16G16B16A16Uint: // 8 bones, one byte each
				count = 8
				for k := 0; k < 8; k++ {
					bones[k] = int(p[k])
				}
			case FormatR16G16B16A16Sint:
				count = 4
				for k := 0; k < 4; k++ {
					bones[k] = int(binary.LittleEndian.Uint16(p[2*k:]))
				}
			case FormatR32G32B32A32Sint: // 8 bones, two bytes each
				count = 8
				for k := 0; k < 8; k++ {
					bones[k] = int(binary.LittleEndian.Uint16(p[2*k:]))
				}
			case FormatR16G16Sint:
				count = 2
				bones[0] = int(binary.LittleEndian.Uint16(p))
				bones[1] = int(binary.LittleEndian.Uint16(p[2:]))
			default:
				return nil, fmt.Errorf("bone indices use format %d, which dlmod does not read", indices.Format)
			}
		} else {
			count = 1
		}
		if weights != nil {
			p := row[weights.Offset:]
			switch weights.Format {
			case FormatR8G8B8A8Unorm:
				for k := 0; k < 4; k++ {
					amounts[k] = float32(p[k]) / 255
				}
			case FormatR16G16B16A16Unorm: // 8 weights, one byte each
				for k := 0; k < 8; k++ {
					amounts[k] = float32(p[k]) / 255
				}
			case FormatR16G16Unorm:
				amounts[0] = float32(binary.LittleEndian.Uint16(p)) / 65535
				amounts[1] = float32(binary.LittleEndian.Uint16(p[2:])) / 65535
			default:
				return nil, fmt.Errorf("bone weights use format %d, which dlmod does not read", weights.Format)
			}
		} else {
			amounts[0] = 1
		}
		var list []Influence
		for k := 0; k < count; k++ {
			if amounts[k] > 0 {
				list = append(list, Influence{bones[k], amounts[k]})
			}
		}
		if len(list) == 0 {
			list = []Influence{{bones[0], 1}}
		}
		out[i] = list
	}
	return out, nil
}

// VertexData holds what the importer writes for each vertex.
type VertexData struct {
	Positions [][3]float32
	Normals   [][3]float32
	UVs       [][2]float32
	Skin      [][]Influence // bone indices into the target mesh's Bones
}

// Encode writes vertices in this buffer's layout. Attributes dlmod has no data for (cloth
// settings, extra UV sets, morph lookups) are written as zero, colours as white.
func (b *VertexBuffer) Encode(v VertexData) ([]byte, error) {
	out := make([]byte, len(v.Positions)*b.Stride)
	for _, attribute := range b.Attributes {
		for i := range v.Positions {
			p := out[i*b.Stride+attribute.Offset:]
			if err := writeAttribute(p, attribute, v, i); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

func putFloats(p []byte, values ...float32) {
	for k, f := range values {
		binary.LittleEndian.PutUint32(p[4*k:], math.Float32bits(f))
	}
}

func floatToHalf(f float32) uint16 {
	bits := math.Float32bits(f)
	sign := uint16(bits>>16) & 0x8000
	exponent := int(bits>>23&0xff) - 127 + 15
	mantissa := bits & 0x7fffff
	switch {
	case exponent >= 31:
		return sign | 0x7c00
	case exponent <= 0:
		if exponent < -10 {
			return sign
		}
		mantissa |= 0x800000
		return sign | uint16(mantissa>>uint(14-exponent))
	}
	return sign | uint16(exponent)<<10 | uint16(mantissa>>13)
}

func clampUnit(f float32, lo float32) float32 {
	return float32(math.Max(float64(lo), math.Min(1, float64(f))))
}

func writeAttribute(p []byte, a VertexAttribute, v VertexData, i int) error {
	switch a.Name {
	case "position":
		if a.Format != FormatR32G32B32Float && a.Format != FormatR32G32B32A32Float {
			return fmt.Errorf("positions use format %d, which dlmod does not write", a.Format)
		}
		putFloats(p, v.Positions[i][0], v.Positions[i][1], v.Positions[i][2])
	case "normal":
		n := v.Normals[i]
		switch a.Format {
		case FormatR32Uint:
			binary.LittleEndian.PutUint32(p, EncodeNormal(n))
		case FormatR32G32B32Float, FormatR32G32B32A32Float:
			putFloats(p, n[0], n[1], n[2])
		default:
			return fmt.Errorf("normals use format %d, which dlmod does not write", a.Format)
		}
	case "tangent":
		if a.Format == FormatR32G32B32A32Float {
			t := tangentFor(v.Normals[i])
			putFloats(p, t[0], t[1], t[2], 1)
		}
	case "texcoord":
		if a.Index != 0 {
			return nil
		}
		uv := v.UVs[i]
		switch a.Format {
		case FormatR32G32Float:
			putFloats(p, uv[0], uv[1])
		case FormatR16G16Float:
			binary.LittleEndian.PutUint16(p, floatToHalf(uv[0]))
			binary.LittleEndian.PutUint16(p[2:], floatToHalf(uv[1]))
		case FormatR16G16Snorm:
			binary.LittleEndian.PutUint16(p, uint16(int16(math.Round(float64(clampUnit(uv[0], -1)*32767)))))
			binary.LittleEndian.PutUint16(p[2:], uint16(int16(math.Round(float64(clampUnit(uv[1], -1)*32767)))))
		case FormatR16G16Unorm:
			binary.LittleEndian.PutUint16(p, uint16(math.Round(float64(clampUnit(uv[0], 0)*65535))))
			binary.LittleEndian.PutUint16(p[2:], uint16(math.Round(float64(clampUnit(uv[1], 0)*65535))))
		default:
			return fmt.Errorf("texture coordinates use format %d, which dlmod does not write", a.Format)
		}
	case "color":
		switch a.Format {
		case FormatR8G8B8A8Unorm:
			copy(p, []byte{255, 255, 255, 255})
		case FormatR32G32B32A32Float:
			putFloats(p, 1, 1, 1, 1)
		}
	case "blendindices", "blendweights":
		return writeSkin(p, a, v.Skin[i])
	}
	return nil
}

// writeSkin stores influences as whole bytes (or 16-bit for the 2-bone format) that add up exactly.
func writeSkin(p []byte, a VertexAttribute, influences []Influence) error {
	slots, wide := 4, false
	switch a.Format {
	case FormatR8G8B8A8Uint, FormatR8G8B8A8Unorm:
	case FormatR16G16B16A16Uint, FormatR16G16B16A16Unorm:
		slots = 8
	case FormatR16G16B16A16Sint:
		wide = true
	case FormatR32G32B32A32Sint:
		slots, wide = 8, true
	case FormatR16G16Sint, FormatR16G16Unorm:
		slots, wide = 2, true
	default:
		return fmt.Errorf("bone %s use format %d, which dlmod does not write", a.Name[5:], a.Format)
	}
	influences = influences[:min(len(influences), slots)]
	unit := 255
	if a.Format == FormatR16G16Unorm {
		unit = 65535
	}
	quantized := quantizeWeights(influences, unit)
	for k := 0; k < slots; k++ {
		bone, weight := 0, 0
		if k < len(influences) {
			bone, weight = influences[k].Bone, quantized[k]
		}
		value := bone
		if a.Name == "blendweights" {
			value = weight
		}
		if wide {
			binary.LittleEndian.PutUint16(p[2*k:], uint16(value))
		} else {
			p[k] = byte(value)
		}
	}
	return nil
}

// quantizeWeights turns weights into integers summing to unit, giving the rounding remainder to
// the strongest bone.
func quantizeWeights(influences []Influence, unit int) []int {
	var total float32
	for _, in := range influences {
		total += in.Weight
	}
	out := make([]int, len(influences))
	if total <= 0 || len(influences) == 0 {
		if len(out) > 0 {
			out[0] = unit
		}
		return out
	}
	sum := 0
	for k, in := range influences {
		out[k] = int(math.Round(float64(in.Weight / total * float32(unit))))
		sum += out[k]
	}
	out[0] += unit - sum
	return out
}

// tangentFor picks a tangent perpendicular to a normal, the one the compressed normal format
// derives when its stored angle is zero.
func tangentFor(n [3]float32) [3]float32 {
	s := float32(1)
	if n[2] < 0 {
		s = -1
	}
	r := 1 / (s + n[2])
	return [3]float32{-s*n[0]*n[0]*r + 1, -s * n[0] * n[1] * r, -s * n[0]}
}
