package source2

import (
	"errors"
	"strings"
)

// Particle systems (.vpcf_c) keep their colours in KV3 as arrays of three or four whole numbers
// from 0 to 255, under keys such as m_ConstantColor, m_ColorMin and m_ColorMax, m_ColorFade and
// m_LiteralColor. Recolouring rewrites those and leaves everything else as it is.

// colorValue reports whether a value under a key is a colour this package rewrites.
func colorValue(key string, v *Value) bool {
	if v == nil || v.Kind != Array || len(v.Items) < 3 || len(v.Items) > 4 || !strings.Contains(strings.ToLower(key), "color") {
		return false
	}
	for _, item := range v.Items {
		switch item.Kind {
		case Int16, Int32, Int64:
			if item.I < 0 || item.I > 255 {
				return false
			}
		case Uint16, Uint32, Uint64:
			if item.U > 255 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func channel(v *Value) float64 {
	switch v.Kind {
	case Uint16, Uint32, Uint64:
		return float64(v.U)
	}
	return float64(v.I)
}

func setChannel(v *Value, f float64) {
	n := int64(f + 0.5)
	n = max(0, min(255, n))
	switch v.Kind {
	case Uint16, Uint32, Uint64:
		v.U = uint64(n)
	default:
		v.I = n
	}
}

func walkColors(v *Value, key string, visit func(*Value)) {
	if v == nil {
		return
	}
	if colorValue(key, v) {
		visit(v)
		return
	}
	for i, k := range v.Keys {
		walkColors(v.Fields[i], k, visit)
	}
	for _, item := range v.Items {
		walkColors(item, key, visit)
	}
}

func particleData(data []byte) (*Resource, *Value, [16]byte, error) {
	r, err := DecodeResource(data)
	if err != nil {
		return nil, nil, [16]byte{}, err
	}
	block := r.Block("DATA")
	if block == nil {
		return nil, nil, [16]byte{}, errors.New("not a particle system")
	}
	v, format, err := ReadKV3(block)
	return r, v, format, err
}

// ParticleColors lists the colours a compiled particle system uses, as RGB.
func ParticleColors(data []byte) ([][3]uint8, error) {
	_, v, _, err := particleData(data)
	if err != nil {
		return nil, err
	}
	var out [][3]uint8
	walkColors(v, "", func(c *Value) {
		out = append(out, [3]uint8{uint8(channel(c.Items[0])), uint8(channel(c.Items[1])), uint8(channel(c.Items[2]))})
	})
	return out, nil
}

// RecolorParticle rewrites every colour of a compiled particle system with recolor (RGB, 0 to
// 255; alpha stays) and returns the new file and how many colours it changed.
func RecolorParticle(data []byte, recolor func([3]float64) [3]float64) ([]byte, int, error) {
	r, v, format, err := particleData(data)
	if err != nil {
		return nil, 0, err
	}
	changed := 0
	walkColors(v, "", func(c *Value) {
		in := [3]float64{channel(c.Items[0]), channel(c.Items[1]), channel(c.Items[2])}
		out := recolor(in)
		for k := 0; k < 3; k++ {
			setChannel(c.Items[k], out[k])
		}
		changed++
	})
	if changed == 0 {
		return data, 0, nil
	}
	encoded, err := WriteKV3(v, format)
	if err != nil {
		return nil, 0, err
	}
	blocks := make([]Block, len(r.Blocks))
	copy(blocks, r.Blocks)
	for i := range blocks {
		if blocks[i].Type == "DATA" {
			blocks[i].Data = encoded
		}
	}
	return (&Resource{Version: r.Version, Blocks: blocks, Streaming: r.Streaming}).Encode(), changed, nil
}
