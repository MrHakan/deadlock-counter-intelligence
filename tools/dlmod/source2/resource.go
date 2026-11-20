package source2

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Block is one block of a compiled resource, such as DATA or CTRL.
type Block struct {
	Type string
	Data []byte
}

// Resource is a compiled Source 2 file: a header, a table of blocks, the blocks, and for some
// types (sounds) data streamed from after the blocks.
type Resource struct {
	Version   uint16
	Blocks    []Block
	Streaming []byte
}

const headerVersion = 12

// Encode writes the resource. Blocks start on 16-byte boundaries like the files Valve's compiler
// writes, and the size in the header covers the blocks but not the streaming data after them.
func (r *Resource) Encode() []byte {
	out := make([]byte, 16+12*len(r.Blocks))
	binary.LittleEndian.PutUint16(out[4:], headerVersion)
	binary.LittleEndian.PutUint16(out[6:], r.Version)
	binary.LittleEndian.PutUint32(out[8:], 8) // the block table follows, relative to this field
	binary.LittleEndian.PutUint32(out[12:], uint32(len(r.Blocks)))
	for i, block := range r.Blocks {
		for len(out)%16 != 0 {
			out = append(out, 0)
		}
		entry := 16 + 12*i
		copy(out[entry:entry+4], block.Type)
		binary.LittleEndian.PutUint32(out[entry+4:], uint32(len(out)-(entry+4)))
		binary.LittleEndian.PutUint32(out[entry+8:], uint32(len(block.Data)))
		out = append(out, block.Data...)
	}
	binary.LittleEndian.PutUint32(out, uint32(len(out)))
	return append(out, r.Streaming...)
}

// DecodeResource reads a compiled resource's blocks and streaming data.
func DecodeResource(data []byte) (*Resource, error) {
	if len(data) < 16 || binary.LittleEndian.Uint16(data[4:]) != headerVersion {
		return nil, errors.New("not a compiled Source 2 resource")
	}
	size := int(binary.LittleEndian.Uint32(data))
	tableStart := 8 + int(binary.LittleEndian.Uint32(data[8:]))
	count := int(binary.LittleEndian.Uint32(data[12:]))
	if size > len(data) || tableStart+12*count > len(data) {
		return nil, errors.New("resource header points past the file")
	}
	r := &Resource{Version: binary.LittleEndian.Uint16(data[6:]), Streaming: data[size:]}
	for i := 0; i < count; i++ {
		entry := tableStart + 12*i
		offset := entry + 4 + int(binary.LittleEndian.Uint32(data[entry+4:]))
		length := int(binary.LittleEndian.Uint32(data[entry+8:]))
		if offset+length > len(data) {
			return nil, fmt.Errorf("block %s points past the file", data[entry:entry+4])
		}
		r.Blocks = append(r.Blocks, Block{string(data[entry : entry+4]), data[offset : offset+length]})
	}
	return r, nil
}

// Block returns the first block of a type, or nil.
func (r *Resource) Block(kind string) []byte {
	for _, block := range r.Blocks {
		if block.Type == kind {
			return block.Data
		}
	}
	return nil
}
