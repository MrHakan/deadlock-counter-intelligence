package source2

import (
	"encoding/binary"
	"hash/crc32"
	"math"
	"testing"
)

func kv3Resource(t *testing.T, root *Value) []byte {
	t.Helper()
	data, err := WriteKV3(root, FormatGeneric)
	if err != nil {
		t.Fatal(err)
	}
	return (&Resource{Blocks: []Block{{"DATA", data}}}).Encode()
}

func floatArray(values ...float64) *Value {
	a := NewArray()
	for _, v := range values {
		a.Items = append(a.Items, &Value{Kind: Float32, F: v})
	}
	return a
}

func entity(class string, origin ...float64) *Value {
	values := NewObject().Set("classname", NewString(class)).Set("scales", floatArray(1, 1, 1)).Set("origin", floatArray(origin...))
	return NewObject().Set("keyValues3Data", NewObject().Set("values", values))
}

func TestPanoramaTextRoundTrip(t *testing.T) {
	original := (&Resource{Blocks: []Block{{"RED2", []byte("x")}, {"DATA", []byte{0, 0, 0, 0, 0, 0, '<', 's', 'v', 'g', '/', '>'}}}}).Encode()
	text, err := PanoramaText(original)
	if err != nil || text != "<svg/>" {
		t.Fatalf("read %q, %v", text, err)
	}
	out, err := ReplacePanoramaText(original, "<svg>NAME</svg>")
	if err != nil {
		t.Fatal(err)
	}
	r, err := DecodeResource(out)
	if err != nil {
		t.Fatal(err)
	}
	data := r.Block("DATA")
	if string(data[6:]) != "<svg>NAME</svg>" || binary.LittleEndian.Uint32(data) != crc32.ChecksumIEEE([]byte("<svg>NAME</svg>")) {
		t.Fatalf("DATA is %q", data)
	}
	if string(r.Block("RED2")) != "x" {
		t.Fatal("other blocks were not kept")
	}
}

func TestHideHeroScene(t *testing.T) {
	ents := NewObject().Set("m_entityKeyValues", NewArray(
		entity("prop_dynamic", 1, 2, 3),
		entity("light_barn", 4, 5, 6),
		entity("prop_dynamic", 0, 0, 10),
	))
	identity := NewArray(floatArray(1, 0, 0, 7), floatArray(0, 1, 0, 8), floatArray(0, 0, 1, 9))
	node := NewObject().Set("m_sceneObjects", NewArray(NewObject().Set("m_vTransform", identity)))
	files := map[string][]byte{
		"maps/ui/hero_prefabs/x/entities/default_ents.vents_c": kv3Resource(t, ents),
		"maps/ui/hero_prefabs/x/worldnodes/n0.vwnod_c":         kv3Resource(t, node),
		"maps/ui/hero_prefabs/x/lightmaps/a.vtex_c":            []byte("untouched"),
	}
	props, objects, err := HideHeroScene(files)
	if err != nil || props != 2 || objects != 1 {
		t.Fatalf("hid %d props and %d objects, %v", props, objects, err)
	}
	read := func(name string) *Value {
		r, err := DecodeResource(files[name])
		if err != nil {
			t.Fatal(err)
		}
		v, _, err := ReadKV3(r.Block("DATA"))
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	list := read("maps/ui/hero_prefabs/x/entities/default_ents.vents_c").Get("m_entityKeyValues").Items
	hero := list[0].Get("keyValues3Data").Get("values")
	if s := hero.Get("scales").Items[0].F; s > 0.01 {
		t.Errorf("the hero kept scale %v", s)
	}
	if z := hero.Get("origin").Items[2].F; z != 3-hiddenDrop {
		t.Errorf("the hero is at z %v", z)
	}
	if light := list[1].Get("keyValues3Data").Get("values"); light.Get("scales").Items[0].F != 1 || light.Get("origin").Items[2].F != 6 {
		t.Error("the light moved")
	}
	m := read("maps/ui/hero_prefabs/x/worldnodes/n0.vwnod_c").Get("m_sceneObjects").Items[0].Get("m_vTransform").Items
	if math.Abs(m[0].Items[0].F-0.001) > 1e-6 || m[2].Items[3].F != -hiddenDrop || m[0].Items[3].F != 0 {
		t.Errorf("world object transform %v %v %v", m[0].Items[0].F, m[2].Items[3].F, m[0].Items[3].F)
	}
	if string(files["maps/ui/hero_prefabs/x/lightmaps/a.vtex_c"]) != "untouched" {
		t.Error("other files changed")
	}
}
