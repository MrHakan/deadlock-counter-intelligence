package source2

import "testing"

func TestRecolorParticle(t *testing.T) {
	original := SampleParticle()
	colors, err := ParticleColors(original)
	if err != nil {
		t.Fatal(err)
	}
	if len(colors) != 3 || colors[0] != [3]uint8{200, 40, 255} {
		t.Fatalf("colours %v", colors)
	}
	swap := func(c [3]float64) [3]float64 { return [3]float64{c[2], c[1], c[0]} } // red and blue trade places
	recolored, changed, err := RecolorParticle(original, swap)
	if err != nil || changed != 3 {
		t.Fatalf("changed %d: %v", changed, err)
	}
	if colors, _ = ParticleColors(recolored); colors[0] != [3]uint8{255, 40, 200} || colors[2] != [3]uint8{0, 0, 250} {
		t.Fatalf("recoloured %v", colors)
	}
	_, v, _, err := particleData(recolored)
	if err != nil {
		t.Fatal(err)
	}
	if v.Get("m_vecOffset").Items[0].I != 1 || v.Get("m_Initializers").Items[0].Get("m_ColorMin").Items[3].I != 128 {
		t.Fatal("vectors that are not colours, and alpha, stay")
	}
}
