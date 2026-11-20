package source2

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

// SoundInfo describes audio as the engine needs it.
type SoundInfo struct {
	Format     string // "PCM16", "PCM8" or "MP3", as m_nFormat names them
	Rate       int
	Channels   int
	Samples    int
	Data       []byte // the bytes the game streams: PCM samples without the WAV header, or MP3 frames
	Duration   float64
	SourceKind string // "wav" or "mp3"
}

// CompileSound turns a .wav (PCM, 8 or 16 bit) or .mp3 file into a compiled .vsnd_c, the version 5
// layout of current Source 2 games: an empty DATA block, a CTRL block with a CVoiceContainerDefault
// describing the audio, and the audio itself after the blocks.
func CompileSound(audio []byte) ([]byte, SoundInfo, error) {
	var info SoundInfo
	var err error
	switch {
	case len(audio) >= 12 && string(audio[:4]) == "RIFF" && string(audio[8:12]) == "WAVE":
		info, err = parseWAV(audio)
	default:
		info, err = parseMP3(audio)
	}
	if err != nil {
		return nil, info, err
	}
	info.Duration = float64(info.Samples) / float64(info.Rate)

	sound := NewObject().
		Set("m_nRate", NewInt32(int32(info.Rate))).
		Set("m_nFormat", NewString(info.Format)).
		Set("m_nChannels", &Value{Kind: Int64, I: int64(info.Channels)}).
		Set("m_nLoopStart", NewInt32(-1)).
		Set("m_nSampleCount", NewUint32(uint32(info.Samples))).
		Set("m_flDuration", &Value{Kind: Float64, F: float64(float32(info.Duration))}).
		Set("m_Sentences", NewArray()).
		Set("m_nStreamingSize", NewUint32(uint32(len(info.Data)))).
		Set("m_nSeekTable", NewArray()).
		Set("m_nLoopEnd", &Value{Kind: Int64}).
		Set("m_encodedHeader", &Value{Kind: Blob})
	root := NewObject().
		Set("_class", NewString("CVoiceContainerDefault")).
		Set("m_vSound", sound).
		Set("m_bHideAnalyzers", NewBool(false)).
		Set("m_analysisContainers", NewArray()).
		Set("m_vsndReference", &Value{Kind: String, Flag: 1}) // an empty resource reference
	ctrl, err := WriteKV3(root, FormatGeneric)
	if err != nil {
		return nil, info, err
	}
	resource := &Resource{Version: 5, Blocks: []Block{{"DATA", nil}, {"CTRL", ctrl}}, Streaming: info.Data}
	return resource.Encode(), info, nil
}

func parseWAV(data []byte) (SoundInfo, error) {
	info := SoundInfo{SourceKind: "wav"}
	var format, bits int
	for offset := 12; offset+8 <= len(data); {
		id := string(data[offset : offset+4])
		size := int(binary.LittleEndian.Uint32(data[offset+4:]))
		body := data[offset+8 : min(len(data), offset+8+size)]
		switch id {
		case "fmt ":
			if len(body) < 16 {
				return info, errors.New("the WAV file's format chunk is too short")
			}
			format = int(binary.LittleEndian.Uint16(body))
			info.Channels = int(binary.LittleEndian.Uint16(body[2:]))
			info.Rate = int(binary.LittleEndian.Uint32(body[4:]))
			bits = int(binary.LittleEndian.Uint16(body[14:]))
			if format == 0xFFFE && len(body) >= 26 { // WAVE_FORMAT_EXTENSIBLE: the real format is in the sub-format GUID
				format = int(binary.LittleEndian.Uint16(body[24:]))
			}
		case "data":
			info.Data = body
		}
		offset += 8 + size + size%2
	}
	switch {
	case info.Data == nil || info.Rate == 0:
		return info, errors.New("the WAV file has no audio data")
	case format != 1 || (bits != 16 && bits != 8):
		return info, fmt.Errorf("the WAV file is %d-bit format %d; export it as 16-bit PCM (or MP3)", bits, format)
	case info.Channels < 1 || info.Channels > 2:
		return info, fmt.Errorf("the WAV file has %d channels; use mono or stereo", info.Channels)
	}
	info.Format = "PCM16"
	if bits == 8 {
		info.Format = "PCM8"
	}
	info.Samples = len(info.Data) / (info.Channels * bits / 8)
	info.Data = info.Data[:info.Samples*info.Channels*bits/8]
	return info, nil
}

var (
	mp3Bitrates = [2][16]int{ // kbit/s for layer III: MPEG-1, then MPEG-2 and 2.5
		{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0},
		{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0},
	}
	mp3Rates = [4][3]int{{11025, 12000, 8000}, {0, 0, 0}, {22050, 24000, 16000}, {44100, 48000, 32000}} // by version bits
)

// parseMP3 walks the MPEG layer III frames: the sample rate, channels and sample count come from
// the frame headers. An ID3v2 tag at the start and an ID3v1 tag at the end are left out, and the
// encoder delay and padding a LAME header records are subtracted, as decoders do.
func parseMP3(data []byte) (SoundInfo, error) {
	info := SoundInfo{Format: "MP3", SourceKind: "mp3"}
	start := 0
	if len(data) >= 10 && string(data[:3]) == "ID3" {
		size := int(data[6]&0x7f)<<21 | int(data[7]&0x7f)<<14 | int(data[8]&0x7f)<<7 | int(data[9]&0x7f)
		start = min(len(data), 10+size)
	}
	end := len(data)
	if end-start >= 128 && string(data[end-128:end-125]) == "TAG" {
		end -= 128
	}
	frames, samples, trim := 0, 0, 0
	first := -1
	for offset := start; offset+4 <= end; {
		header := binary.BigEndian.Uint32(data[offset:])
		if header>>21 != 0x7FF {
			if frames == 0 {
				offset++ // tolerate junk before the first frame
				continue
			}
			break
		}
		version := int(header >> 19 & 3)
		layer := int(header >> 17 & 3)
		bitrateIndex := int(header >> 12 & 15)
		rateIndex := int(header >> 10 & 3)
		padding := int(header >> 9 & 1)
		mode := int(header >> 6 & 3)
		if layer != 1 || version == 1 || bitrateIndex == 0 || bitrateIndex == 15 || rateIndex == 3 {
			if frames == 0 {
				offset++
				continue
			}
			return info, errors.New("the MP3 file is not MPEG layer III all the way through")
		}
		table, perFrame, slot := 0, 1152, 144
		if version != 3 {
			table, perFrame, slot = 1, 576, 72
		}
		rate := mp3Rates[version][rateIndex]
		length := slot*mp3Bitrates[table][bitrateIndex]*1000/rate + padding
		if offset+length > end {
			break
		}
		if first < 0 {
			first = offset
			info.Rate = rate
			info.Channels = 2
			if mode == 3 {
				info.Channels = 1
			}
			// A Xing or Info frame describes the stream and holds no audio; its LAME extension
			// records the encoder's delay and padding.
			side := map[[2]bool]int{{true, false}: 32, {true, true}: 17, {false, false}: 17, {false, true}: 9}[[2]bool{version == 3, mode == 3}]
			tag := offset + 4 + side
			if tag+120 <= offset+length && (string(data[tag:tag+4]) == "Xing" || string(data[tag:tag+4]) == "Info") {
				if lame := bytes.Index(data[tag:offset+length], []byte("LAME")); lame >= 0 && tag+lame+24 <= offset+length {
					d := data[tag+lame+21:]
					delay := int(d[0])<<4 | int(d[1])>>4
					pad := int(d[1]&15)<<8 | int(d[2])
					trim = delay + pad // Valve's compiler subtracts these two, as its own sounds show
				}
				offset += length
				continue
			}
		}
		frames++
		samples += perFrame
		offset += length
	}
	if frames == 0 {
		return info, errors.New("this is neither a WAV nor an MP3 file")
	}
	info.Samples = max(0, samples-trim)
	if trim == 0 {
		info.Samples = samples
	}
	info.Data = data[first:end]
	return info, nil
}

// IsSoundFile reports whether a name looks like audio dlmod can compile.
func IsSoundFile(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".wav") || strings.HasSuffix(lower, ".mp3")
}
