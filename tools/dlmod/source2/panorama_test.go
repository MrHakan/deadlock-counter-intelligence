package source2

import (
	"image"
	"image/color"
	"testing"
)

// A picture of another shape replaces a hero card: it fills the card's own size, cropped in the
// middle, in BGRA with every mip level, and the headers stay the game's.
func TestReplaceTextureImage(t *testing.T) {
	original := SampleInterfaceImage(8, 4, 3)
	picture := image.NewNRGBA(image.Rect(0, 0, 16, 4)) // twice as wide as the card: the sides go
	for y := 0; y < 4; y++ {
		for x := 0; x < 16; x++ {
			c := color.NRGBA{255, 0, 0, 255}
			if x < 4 || x >= 12 {
				c = color.NRGBA{0, 0, 255, 255}
			}
			picture.SetNRGBA(x, y, c)
		}
	}
	replaced, err := ReplaceTextureImage(original, picture)
	if err != nil {
		t.Fatal(err)
	}
	r, err := DecodeResource(replaced)
	if err != nil {
		t.Fatal(err)
	}
	if want := (8*4 + 4*2 + 2*1) * 4; len(r.Streaming) != want {
		t.Fatalf("mips: %d bytes, want %d", len(r.Streaming), want)
	}
	before, _ := DecodeResource(original)
	if string(r.Block("DATA")) != string(before.Block("DATA")) || string(r.Block("RED2")) != string(before.Block("RED2")) {
		t.Fatal("the headers changed")
	}
	top := r.Streaming[len(r.Streaming)-8*4*4:]
	if top[0] != 0 || top[2] != 255 || top[3] != 255 {
		t.Fatalf("the first pixel is not red stored as BGRA: %v", top[:4])
	}
	img, info, err := DecodeTexture(replaced)
	if err != nil {
		t.Fatal(err)
	}
	if info.Width != 8 || info.Height != 4 || info.Mips != 3 {
		t.Fatalf("info %+v", info)
	}
	for x := 0; x < 8; x++ {
		if c := img.At(x, 2).(color.NRGBA); c.R < 200 || c.B > 50 {
			t.Fatalf("pixel %d is %v: the blue sides should be cropped away", x, c)
		}
	}
	// Block-compressed textures (dlmod's own materials) decode too.
	compiled, err := CompileTexture(SolidImage([4]float32{0, 1, 0, 1}), 64, [4]float32{1, 1, 1, 1})
	if err != nil {
		t.Fatal(err)
	}
	if img, _, err = DecodeTexture(compiled); err != nil {
		t.Fatal(err)
	}
	if c := img.At(1, 1).(color.NRGBA); c.G < 250 || c.R > 5 {
		t.Fatalf("BC1 decoded to %v", c)
	}
}
