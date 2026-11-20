package source2

import (
	"encoding/binary"
	"errors"
	"sort"
	"strings"
)

// Reference is one entry of a resource's external reference list (the RERL block): another
// resource the engine loads along with it.
type Reference struct {
	ID   uint64
	Name string
}

// ResourceID is the id the engine gives a resource path: MurmurHash64B of the lowercase path.
func ResourceID(name string) uint64 { return murmur64B([]byte(strings.ToLower(name)), 0xEDABCDEF) }

// ReadReferences decodes a RERL block.
func ReadReferences(block []byte) ([]Reference, error) {
	if len(block) < 8 {
		return nil, nil
	}
	start := int(binary.LittleEndian.Uint32(block))
	count := int(binary.LittleEndian.Uint32(block[4:]))
	var out []Reference
	for i := 0; i < count; i++ {
		entry := start + 16*i
		if entry+16 > len(block) {
			return nil, errShort
		}
		at := entry + 8 + int(binary.LittleEndian.Uint32(block[entry+8:]))
		end := at
		for end < len(block) && block[end] != 0 {
			end++
		}
		if at < 0 || end >= len(block) {
			return nil, errors.New("damaged reference list")
		}
		out = append(out, Reference{binary.LittleEndian.Uint64(block[entry:]), string(block[at:end])})
	}
	return out, nil
}

// WriteReferences encodes a RERL block: a header, 16-byte entries holding each id and the
// offset of its name, then the names.
func WriteReferences(refs []Reference) []byte {
	if len(refs) == 0 {
		return make([]byte, 8)
	}
	out := binary.LittleEndian.AppendUint32(nil, 8)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(refs)))
	names := 8 + 16*len(refs)
	text := 0
	for i, r := range refs {
		out = binary.LittleEndian.AppendUint64(out, r.ID)
		field := 8 + 16*i + 8
		out = binary.LittleEndian.AppendUint32(out, uint32(names+text-field))
		out = binary.LittleEndian.AppendUint32(out, 0)
		text += len(r.Name) + 1
	}
	for _, r := range refs {
		out = append(append(out, r.Name...), 0)
	}
	return out
}

// addReferences lists materials the replaced mesh draws with in the resource's external
// reference block, so the engine loads them with the model.
func (m *Model) addReferences(blocks []Block, opts ImportOptions) error {
	if len(opts.Materials) == 0 {
		return nil
	}
	index := -1
	for i, b := range blocks {
		if b.Type == "RERL" {
			index = i
		}
	}
	if index < 0 {
		return errors.New("the model has no reference list to add its new materials to")
	}
	refs, err := ReadReferences(blocks[index].Data)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, r := range refs {
		have[strings.ToLower(r.Name)] = true
	}
	for _, material := range opts.Materials {
		if material != "" && !have[strings.ToLower(material)] {
			refs = append(refs, Reference{ResourceID(material), material})
			have[strings.ToLower(material)] = true
		}
	}
	sort.Slice(refs, func(a, b int) bool { return refs[a].Name < refs[b].Name })
	blocks[index].Data = WriteReferences(refs)
	return nil
}

func murmur64B(key []byte, seed uint64) uint64 {
	const m = 0x5bd1e995
	const r = 24
	h1 := uint32(seed) ^ uint32(len(key))
	h2 := uint32(seed >> 32)
	word := func(b []byte) uint32 { return binary.LittleEndian.Uint32(b) }
	mix := func(h, k uint32) uint32 {
		k *= m
		k ^= k >> r
		k *= m
		h *= m
		return h ^ k
	}
	for len(key) >= 8 {
		h1 = mix(h1, word(key))
		h2 = mix(h2, word(key[4:]))
		key = key[8:]
	}
	if len(key) >= 4 {
		h1 = mix(h1, word(key))
		key = key[4:]
	}
	switch len(key) {
	case 3:
		h2 ^= uint32(key[2]) << 16
		fallthrough
	case 2:
		h2 ^= uint32(key[1]) << 8
		fallthrough
	case 1:
		h2 ^= uint32(key[0])
		h2 *= m
	}
	h1 ^= h2 >> 18
	h1 *= m
	h2 ^= h1 >> 22
	h2 *= m
	h1 ^= h2 >> 17
	h1 *= m
	h2 ^= h1 >> 19
	h2 *= m
	return uint64(h1)<<32 | uint64(h2)
}
