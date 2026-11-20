package source2

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

func TestKV3RoundTrip(t *testing.T) {
	root := NewObject().
		Set("_class", NewString("Example")).
		Set("count", NewInt32(-7)).
		Set("size", NewUint32(4062)).
		Set("big", &Value{Kind: Int64, I: 1 << 40}).
		Set("ratio", NewFloat32(0.25)).
		Set("precise", &Value{Kind: Float64, F: math.Pi}).
		Set("flags", NewArray(NewBool(true), NewBool(false), &Value{Kind: Int16, I: -3}, &Value{Kind: Uint16, U: 9})).
		Set("child", NewObject().Set("name", NewString("inner")).Set("empty", NewString(""))).
		Set("ref", &Value{Kind: String, S: "sounds/a.vsnd", Flag: 1}).
		Set("blob", &Value{Kind: Blob, Blob: []byte{1, 2, 3}}).
		Set("none", &Value{Kind: Null})
	data, err := WriteKV3(root, FormatGeneric)
	if err != nil {
		t.Fatal(err)
	}
	back, format, err := ReadKV3(data)
	if err != nil || format != FormatGeneric {
		t.Fatalf("read back: %v", err)
	}
	if dumpValue(back, "") != dumpValue(root, "") {
		t.Fatalf("round trip differs:\n%s\n%s", dumpValue(back, ""), dumpValue(root, ""))
	}
	if back.Get("ref").Flag != 1 || back.Get("child").Get("empty").S != "" {
		t.Fatal("flags and empty strings survive")
	}
}

func wav(channels, bits, rate int, samples []int16) []byte {
	var data bytes.Buffer
	for _, s := range samples {
		binary.Write(&data, binary.LittleEndian, s)
	}
	var out bytes.Buffer
	out.WriteString("RIFF")
	binary.Write(&out, binary.LittleEndian, uint32(36+data.Len()))
	out.WriteString("WAVEfmt ")
	for _, field := range []any{uint32(16), uint16(1), uint16(channels), uint32(rate),
		uint32(rate * channels * bits / 8), uint16(channels * bits / 8), uint16(bits)} {
		binary.Write(&out, binary.LittleEndian, field)
	}
	out.WriteString("LIST")
	binary.Write(&out, binary.LittleEndian, uint32(3)) // an odd-sized chunk to skip, with its pad byte
	out.Write([]byte{1, 2, 3, 0})
	out.WriteString("data")
	binary.Write(&out, binary.LittleEndian, uint32(data.Len()))
	out.Write(data.Bytes())
	return out.Bytes()
}

func TestCompileWAV(t *testing.T) {
	compiled, info, err := CompileSound(wav(2, 16, 48000, []int16{1, -1, 2, -2, 3, -3}))
	if err != nil {
		t.Fatal(err)
	}
	if info.Format != "PCM16" || info.Channels != 2 || info.Samples != 3 || info.Rate != 48000 {
		t.Fatalf("%+v", info)
	}
	resource, err := DecodeResource(compiled)
	if err != nil || resource.Version != 5 || len(resource.Streaming) != 12 {
		t.Fatalf("resource: %v %+v", err, resource)
	}
	if binary.LittleEndian.Uint32(compiled) != uint32(len(compiled)-12) {
		t.Fatal("the header size ends where the streamed audio begins")
	}
	if resource.Blocks[0].Type != "DATA" || len(resource.Blocks[0].Data) != 0 || resource.Blocks[1].Type != "CTRL" {
		t.Fatalf("blocks: %+v", resource.Blocks)
	}
	ctrl, _, err := ReadKV3(resource.Block("CTRL"))
	if err != nil {
		t.Fatal(err)
	}
	sound := ctrl.Get("m_vSound")
	if ctrl.Get("_class").S != "CVoiceContainerDefault" || sound.Get("m_nFormat").S != "PCM16" ||
		sound.Get("m_nRate").I != 48000 || sound.Get("m_nSampleCount").U != 3 || sound.Get("m_nStreamingSize").U != 12 {
		t.Fatalf("description: %s", dumpValue(ctrl, ""))
	}
	if _, _, err := CompileSound(wav(1, 24, 44100, []int16{1, 2, 3})); err == nil {
		t.Fatal("24-bit audio asks for a 16-bit export")
	}
}

// mp3Frame builds one MPEG-1 layer III frame header at 128 kbit/s, 44.1 kHz, with an empty body.
func mp3Frame(mono bool) []byte {
	header := uint32(0xFFFB9000) // sync, MPEG-1, layer III, no CRC, 128 kbit/s, 44.1 kHz
	if mono {
		header |= 3 << 6
	}
	frame := make([]byte, 417) // 144 * 128000 / 44100
	binary.BigEndian.PutUint32(frame, header)
	return frame
}

func TestCompileMP3(t *testing.T) {
	var audio []byte
	audio = append(audio, "ID3\x04\x00\x00\x00\x00\x00\x05hello"...) // a tag the game does not need
	for i := 0; i < 10; i++ {
		audio = append(audio, mp3Frame(true)...)
	}
	_, info, err := CompileSound(audio)
	if err != nil {
		t.Fatal(err)
	}
	if info.Format != "MP3" || info.Channels != 1 || info.Rate != 44100 || info.Samples != 11520 || len(info.Data) != 4170 {
		t.Fatalf("%+v", info)
	}
	if _, _, err := CompileSound([]byte("not audio at all")); err == nil {
		t.Fatal("other files are refused")
	}
}
