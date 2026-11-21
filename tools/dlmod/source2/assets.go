package source2

import (
	"errors"
	"math"
	"sort"
	"strings"
)

// ResourceReferences decodes the external reference list (RERL) of any compiled resource.
func ResourceReferences(data []byte) ([]Reference, error) {
	r, err := DecodeResource(data)
	if err != nil {
		return nil, err
	}
	return ReadReferences(r.Block("RERL"))
}

// ParticleRefs holds the active (non-preview) assets referenced by a compiled particle system.
type ParticleRefs struct {
	Textures  []string
	Materials []string
	Models    []string
	Children  []string
}

// ParticleAssetRefs extracts textures, materials, models and child particle systems referenced by
// a compiled .vpcf_c, from both its RERL block and its DATA KV3 (excluding editor preview state
// such as m_previewState and m_previewModel). All returned paths are lowercase without "_c".
func ParticleAssetRefs(data []byte) (ParticleRefs, error) {
	r, err := DecodeResource(data)
	if err != nil {
		return ParticleRefs{}, err
	}
	texSet := map[string]bool{}
	matSet := map[string]bool{}
	mdlSet := map[string]bool{}
	chSet := map[string]bool{}
	add := func(raw string) {
		p := strings.ToLower(strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/"), "/"))
		p = strings.TrimSuffix(p, "_c")
		switch {
		case strings.HasSuffix(p, ".vtex"):
			texSet[p] = true
		case strings.HasSuffix(p, ".vmat"):
			matSet[p] = true
		case strings.HasSuffix(p, ".vmdl"):
			mdlSet[p] = true
		case strings.HasSuffix(p, ".vpcf"):
			chSet[p] = true
		}
	}
	if refs, err := ReadReferences(r.Block("RERL")); err == nil {
		for _, ref := range refs {
			add(ref.Name)
		}
	}
	if block := r.Block("DATA"); block != nil {
		if v, _, err := ReadKV3(block); err == nil {
			var walk func(n *Value, key string)
			walk = func(n *Value, key string) {
				if n == nil || key == "m_previewState" || key == "m_previewModel" || key == "m_hFallback" {
					return
				}
				if n.Kind == String {
					add(n.S)
					return
				}
				for i, k := range n.Keys {
					if k == "m_previewState" || k == "m_previewModel" {
						continue
					}
					walk(n.Fields[i], k)
				}
				for _, it := range n.Items {
					walk(it, key)
				}
			}
			walk(v, "")
		}
	}
	return ParticleRefs{
		Textures:  sortedKeys(texSet),
		Materials: sortedKeys(matSet),
		Models:    sortedKeys(mdlSet),
		Children:  sortedKeys(chSet),
	}, nil
}

// ModelMaterials returns the material paths (.vmat, lowercase, without "_c") used by a compiled
// model (.vmdl_c), working for both modern models and older-layout (MBUF) props.
func ModelMaterials(data []byte) ([]string, error) {
	r, err := DecodeResource(data)
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	add := func(raw string) {
		p := strings.ToLower(strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/"), "/"))
		p = strings.TrimSuffix(p, "_c")
		if strings.HasSuffix(p, ".vmat") {
			set[p] = true
		}
	}
	if refs, err := ReadReferences(r.Block("RERL")); err == nil {
		for _, ref := range refs {
			add(ref.Name)
		}
	}
	for _, b := range r.Blocks {
		if b.Type != "DATA" && b.Type != "MDAT" {
			continue
		}
		v, _, err := ReadKV3(b.Data)
		if err != nil {
			continue
		}
		var walk func(n *Value)
		walk = func(n *Value) {
			if n == nil {
				return
			}
			if n.Kind == String {
				add(n.S)
				return
			}
			for _, f := range n.Fields {
				walk(f)
			}
			for _, it := range n.Items {
				walk(it)
			}
		}
		walk(v)
	}
	return sortedKeys(set), nil
}

// MaterialTextureParams reads a compiled material's (.vmat_c) shader name and texture parameters
// (parameter name -> lowercase texture path without "_c").
func MaterialTextureParams(data []byte) (shader string, params map[string]string, err error) {
	r, err := DecodeResource(data)
	if err != nil {
		return "", nil, err
	}
	block := r.Block("DATA")
	if block == nil {
		return "", nil, errors.New("not a compiled material")
	}
	v, _, err := ReadKV3(block)
	if err != nil {
		return "", nil, err
	}
	if s := v.Get("m_shaderName"); s != nil {
		shader = s.S
	}
	params = map[string]string{}
	if tp := v.Get("m_textureParams"); tp != nil {
		for _, item := range tp.Items {
			nm, val := item.Get("m_name"), item.Get("m_pValue")
			if nm == nil || val == nil || val.S == "" {
				continue
			}
			p := strings.ToLower(strings.TrimPrefix(strings.ReplaceAll(val.S, "\\", "/"), "/"))
			params[nm.S] = strings.TrimSuffix(p, "_c")
		}
	}
	return shader, params, nil
}

// IsMaterialColorParam reports whether a material texture parameter holds colour data rather than
// normals, roughness, AO, metalness, opacity masks, or single-channel self-illum masks.
func IsMaterialColorParam(paramName string) bool {
	switch paramName {
	case "g_tColor", "g_tAltColor_Reveal", "g_tNprTransmissiveColor":
		return true
	}
	return false
}

// isMaterialVectorColorParam reports whether a material m_vectorParams entry is a colour/tint
// vector whose chromatic value should be remapped.
func isMaterialVectorColorParam(name string) bool {
	low := strings.ToLower(name)
	if !strings.Contains(low, "color") && !strings.Contains(low, "tint") {
		return false
	}
	for _, skip := range []string{
		"contrast", "scroll", "texcoord", "offset", "scale", "mask", "mode",
		"outline", "normal", "roughness", "metalness", "ambientocclusion",
		"translucency", "position", "sphere", "range", "strength",
	} {
		if strings.Contains(low, skip) {
			return false
		}
	}
	return true
}

// RecolorMaterial rewrites chromatic RGB color/tint vectors in a compiled material's (.vmat_c)
// m_vectorParams using recolor (RGB in 0..255). Neutral grey/white/black multipliers such as
// [1, 1, 1] are left untouched so textured materials keep their intended contrast and accents.
func RecolorMaterial(data []byte, recolor func([3]float64) [3]float64) ([]byte, int, error) {
	r, err := DecodeResource(data)
	if err != nil {
		return nil, 0, err
	}
	block := r.Block("DATA")
	if block == nil {
		return nil, 0, errors.New("not a compiled material")
	}
	v, format, err := ReadKV3(block)
	if err != nil {
		return nil, 0, err
	}
	vp := v.Get("m_vectorParams")
	if vp == nil {
		return data, 0, nil
	}
	changed := 0
	for _, item := range vp.Items {
		nm := item.Get("m_name")
		val := item.Get("m_value")
		if nm == nil || val == nil || val.Kind != Array || len(val.Items) < 3 {
			continue
		}
		if !isMaterialVectorColorParam(nm.S) {
			continue
		}
		r0, g0, b0 := val.Items[0].F, val.Items[1].F, val.Items[2].F
		// Skip neutral white/black/grey multipliers and HDR non-color vectors.
		if r0 < 0 || g0 < 0 || b0 < 0 || r0 > 2 || g0 > 2 || b0 > 2 {
			continue
		}
		hi := math.Max(r0, math.Max(g0, b0))
		lo := math.Min(r0, math.Min(g0, b0))
		if hi <= 0.01 || (hi-lo)/hi < 0.06 {
			continue
		}
		scale := 1.0
		if hi > 1.0 {
			scale = hi
		}
		in := [3]float64{r0 / scale * 255, g0 / scale * 255, b0 / scale * 255}
		out := recolor(in)
		nr, ng, nb := out[0]/255*scale, out[1]/255*scale, out[2]/255*scale
		if math.Abs(nr-r0)+math.Abs(ng-g0)+math.Abs(nb-b0) < 1e-3 {
			continue
		}
		val.Items[0].F = nr
		val.Items[1].F = ng
		val.Items[2].F = nb
		changed++
	}
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

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
