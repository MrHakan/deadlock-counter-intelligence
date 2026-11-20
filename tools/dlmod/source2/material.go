package source2

import (
	"sort"
	"strings"
)

// DefaultTextures are the game's stock textures a new material uses for everything but its
// colour, by the names Deadlock's own materials compile them to. dlmod checks them against the
// game's archive before building, and swaps in what the game has if a name changed.
var DefaultTextures = map[string]string{
	"g_tAmbientOcclusion":     "materials/default/default_ao_tga_559f1ac6.vtex",
	"g_tNormalRoughness":      "materials/default/default_normal_tga_7be61377.vtex",
	"g_tNprOutlineMask":       "materials/default/default_mask_tga_344101f8.vtex",
	"g_tNprTransmissiveColor": "materials/default/default_black_mask_tga_e7be3cc.vtex",
	"g_tSelfIllumMask":        "materials/default/default_mask_tga_344101f8.vtex",
	"g_tTintMaskRimLightMask": "materials/default/default_mask_tga_8d0774e6.vtex",
}

func named(key string, field string, value *Value) *Value {
	return NewObject().Set("m_name", NewString(key)).Set(field, value)
}

func vector(x, y, z, w float32) *Value {
	return NewArray(NewFloat32(x), NewFloat32(y), NewFloat32(z), NewFloat32(w))
}

func resourceRef(path string) *Value { return &Value{Kind: String, S: path, Flag: 1} }

// CompileMaterial writes a .vmat_c drawing with Deadlock's hero shader (pbr.vfx with its
// stylised lighting, outlines and status effects such as the damage flash) and the given colour
// texture, the stock textures standing in for the rest. size is the colour texture's width and
// height.
func CompileMaterial(name, colorTexture string, size int, alphaTest bool) ([]byte, error) {
	ints := []*Value{
		named("F_USE_NPR_LIGHTING", "m_nValue", &Value{Kind: Int64, I: 1}),
		named("F_USE_STATUS_EFFECTS_PROXY", "m_nValue", &Value{Kind: Int64, I: 1}),
		named("F_WRITE_DEPTH_BEFORE_ALPHA_BLENDING", "m_nValue", &Value{Kind: Int64, I: 1}),
		named("g_bMaskColorTint1", "m_nValue", &Value{Kind: Int64, I: 1}),
		named("g_bMaskVertexColorTint1", "m_nValue", &Value{Kind: Int64, I: 1}),
		named("g_nTextureColorTintMode1", "m_nValue", &Value{Kind: Int64}),
	}
	if alphaTest {
		ints = append(ints, named("F_ALPHA_TEST", "m_nValue", &Value{Kind: Int64, I: 1}))
	}
	floats := []*Value{
		named("g_flHighlightAOStrength1", "m_flValue", NewFloat32(1)),
		named("g_flHighlightNormalStrength1", "m_flValue", NewFloat32(64)),
		named("g_flHighlightRadius1", "m_flValue", NewFloat32(256)),
		named("g_flHighlightTintBrightness1", "m_flValue", NewFloat32(1)),
		named("g_fVertexColorStrength1", "m_flValue", NewFloat32(1)),
	}
	if alphaTest {
		floats = append(floats, named("g_flAlphaTestReference", "m_flValue", NewFloat32(0.5)))
	}
	vectors := []*Value{
		named("g_vAlbedoContrastSaturationBrightness1", "m_value", vector(1, 1, 1, 0)),
		named("g_vColorTint1", "m_value", vector(1, 1, 1, 0)),
		named("g_vHighlightPositionWs1", "m_value", vector(0, 0, 72, 0)),
		named("g_vHighlightTint1", "m_value", vector(1, 1, 1, 0)),
		named("g_vSolidOutlineAdditive", "m_value", vector(0.011765, 0.011765, 0.011765, 0)),
		named("g_vSolidOutlineTint", "m_value", vector(0.101961, 0.101961, 0.101961, 0)),
	}
	textures := map[string]string{"g_tColor": colorTexture}
	for key, path := range DefaultTextures {
		textures[key] = path
	}
	var keys []string
	for key := range textures {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var textureParams []*Value
	for _, key := range keys {
		textureParams = append(textureParams, named(key, "m_pValue", resourceRef(textures[key])))
	}
	data := NewObject().
		Set("m_materialName", NewString(name)).
		Set("m_shaderName", NewString("pbr.vfx")).
		Set("m_intParams", NewArray(ints...)).
		Set("m_floatParams", NewArray(floats...)).
		Set("m_vectorParams", NewArray(vectors...)).
		Set("m_textureParams", NewArray(textureParams...)).
		Set("m_dynamicParams", NewArray()).
		Set("m_dynamicTextureParams", NewArray()).
		Set("m_intAttributes", NewArray(
			named("RepresentativeTextureHeight", "m_nValue", NewInt32(int32(size))),
			named("RepresentativeTextureWidth", "m_nValue", NewInt32(int32(size))))).
		Set("m_floatAttributes", NewArray()).
		Set("m_vectorAttributes", NewArray()).
		Set("m_textureAttributes", NewArray()).
		Set("m_stringAttributes", NewArray())
	dataBlock, err := WriteKV3(data, FormatGeneric)
	if err != nil {
		return nil, err
	}

	// The vertex inputs pbr.vfx reads, as Valve's material compiler records them.
	element := func(name, semantic, d3d string, index int) *Value {
		return NewObject().Set("m_pName", NewString(name)).Set("m_pSemantic", NewString(semantic)).
			Set("m_pD3DSemanticName", NewString(d3d)).Set("m_nD3DSemanticIndex", &Value{Kind: Int64, I: int64(index)})
	}
	signature := NewObject().
		Set("m_elems", NewArray(
			element("vBlendIndices", "BlendIndices", "BLENDINDICES", 0),
			element("nPackedFrame", "CompressedTangentFrame", "NORMAL", 0),
			element("vNormalOs", "Normal", "NORMAL", 0),
			element("vPositionOs", "PosXyz", "POSITION", 0),
			element("nVertexID", "None", "SV_VertexID", 0),
			element("vTangentUOs_flTangentVSign", "TangentU_SignV", "TANGENT", 0),
			element("vTexCoord", "LowPrecisionUv", "TEXCOORD", 0),
			element("nInstanceIdx", "InstanceTransformUv", "TEXCOORD", 13))).
		Set("m_depth_elems", NewArray(
			element("vBlendIndices", "BlendIndices", "BLENDINDICES", 0),
			element("vPositionOs", "PosXyz", "POSITION", 0),
			element("nVertexID", "None", "SV_VertexID", 0),
			element("nInstanceIdx", "InstanceTransformUv", "TEXCOORD", 13)))
	signatureBlock, err := WriteKV3(signature, FormatGeneric)
	if err != nil {
		return nil, err
	}

	var children []*Value
	seen := map[string]bool{}
	var refs []Reference
	for _, key := range keys {
		path := textures[key]
		children = append(children, NewString(path))
		if !seen[strings.ToLower(path)] {
			seen[strings.ToLower(path)] = true
			refs = append(refs, Reference{ResourceID(path), path})
		}
	}
	sort.Slice(refs, func(a, b int) bool { return refs[a].Name < refs[b].Name })
	flag := func(on bool) *Value {
		if on {
			return &Value{Kind: Int64, I: 1}
		}
		return &Value{Kind: Int64}
	}
	searchable := NewObject().
		Set("AllowBackfaceCulling", flag(true)).
		Set("WriteDepthBeforeAlphaBlending", flag(true)).
		Set("ForwardLayerOnly", flag(true)).
		Set("NprLitOutline", flag(true)).
		Set("NprSolidOutline", flag(false)).
		Set("RepresentativeTexture", NewString(colorTexture)).
		Set("alphatest", flag(alphaTest)).
		Set("translucent", flag(false)).
		Set("renderbackfaces", flag(false)).
		Set("MaterialLayerType", NewString("pbr")).
		Set("F_WRITE_DEPTH_BEFORE_ALPHA_BLENDING", flag(true)).
		Set("F_USE_NPR_LIGHTING", flag(true)).
		Set("F_USE_STATUS_EFFECTS_PROXY", flag(true)).
		Set("shader", NewString("pbr.vfx")).
		Set("RepresentativeTextureHeight", NewInt32(int32(size))).
		Set("RepresentativeTextureWidth", NewInt32(int32(size))).
		Set("IsChildResource", flag(false))
	editInfo, err := WriteKV3(editInfoFor("CompileMaterial", "Material Compiler Version", 25, children, searchable), FormatGeneric)
	if err != nil {
		return nil, err
	}
	resource := &Resource{Version: 1, Blocks: []Block{
		{"RERL", WriteReferences(refs)},
		{"RED2", editInfo},
		{"DATA", dataBlock},
		{"INSG", signatureBlock},
	}}
	return resource.Encode(), nil
}

// editInfoFor is a RED2 block naming the compiler a resource came from and its children.
func editInfoFor(compiler, version string, fingerprint uint32, children []*Value, searchable *Value) *Value {
	return NewObject().
		Set("m_InputDependencies", NewArray()).
		Set("m_AdditionalInputDependencies", NewArray()).
		Set("m_ArgumentDependencies", NewArray()).
		Set("m_SpecialDependencies", NewArray(NewObject().
			Set("m_String", NewString(version)).
			Set("m_CompilerIdentifier", NewString(compiler)).
			Set("m_nFingerprint", NewUint32(fingerprint)).
			Set("m_nUserData", &Value{Kind: Int64}))).
		Set("m_AdditionalRelatedFiles", NewArray()).
		Set("m_ChildResourceList", NewArray(children...)).
		Set("m_WeakReferenceList", NewArray()).
		Set("m_SearchableUserData", searchable).
		Set("m_SubassetReferences", &Value{Kind: Null}).
		Set("m_SubassetDefinitions", &Value{Kind: Null})
}
