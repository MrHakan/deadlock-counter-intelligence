package source2

import (
	"errors"
	"fmt"
	"math/bits"
)

// Compiled models store their vertex and index buffers compressed with meshoptimizer's codecs
// (github.com/zeux/meshoptimizer). These decoders follow ValveResourceFormat's port of them.

const (
	vertexHeader         = 0xa0
	indexHeader          = 0xe0
	vertexBlockSizeBytes = 8192
	vertexBlockMaxSize   = 256
	byteGroupSize        = 16
	byteGroupDecodeLimit = 24
)

var (
	vertexBitsV0 = []int{0, 2, 4, 8}
	vertexBitsV1 = []int{0, 1, 2, 4, 8}
)

// DecodeVertexBuffer decodes a meshoptimizer-compressed vertex buffer of count vertices of size
// bytes each.
func DecodeVertexBuffer(count, size int, buffer []byte) ([]byte, error) {
	if size <= 0 || size > 256 || size%4 != 0 {
		return nil, fmt.Errorf("meshopt: vertex size %d is not a multiple of 4 up to 256", size)
	}
	if len(buffer) < 1 || buffer[0]&0xF0 != vertexHeader {
		return nil, errors.New("meshopt: not a compressed vertex buffer")
	}
	version := int(buffer[0] & 0x0F)
	if version > 1 {
		return nil, fmt.Errorf("meshopt: vertex codec version %d is not supported", version)
	}
	buffer = buffer[1:]
	tailSize := size
	tailMin := 32
	if version == 1 {
		tailSize, tailMin = size+size/4, 24
	}
	tailPadded := max(tailSize, tailMin)
	if len(buffer) < tailPadded {
		return nil, errShort
	}
	last := append([]byte(nil), buffer[len(buffer)-tailSize:len(buffer)-tailSize+size]...)
	var channels []byte
	if version == 1 {
		channels = buffer[len(buffer)-tailSize+size : len(buffer)-tailSize+size+size/4]
	}
	blockSize := min((vertexBlockSizeBytes/size)&^(byteGroupSize-1), vertexBlockMaxSize)
	out := make([]byte, count*size)
	d := &vertexDecoder{
		lanes:      make([]byte, vertexBlockMaxSize*4+byteGroupSize),
		transposed: make([]byte, vertexBlockSizeBytes),
	}
	for offset := 0; offset < count; offset += blockSize {
		n := min(blockSize, count-offset)
		var err error
		if buffer, err = d.block(buffer, out[offset*size:], n, size, last, channels, version); err != nil {
			return nil, err
		}
	}
	if len(buffer) != tailPadded {
		return nil, errors.New("meshopt: vertex buffer has the wrong tail size")
	}
	return out, nil
}

type vertexDecoder struct{ lanes, transposed []byte }

func (d *vertexDecoder) block(data, out []byte, count, size int, last, channels []byte, version int) ([]byte, error) {
	aligned := (count + byteGroupSize - 1) &^ (byteGroupSize - 1)
	controlSize := 0
	if version == 1 {
		controlSize = size / 4
	}
	if len(data) < controlSize {
		return nil, errShort
	}
	control := data[:controlSize]
	data = data[controlSize:]
	for k := 0; k < size; k += 4 {
		ctrlByte := byte(0)
		if version == 1 {
			ctrlByte = control[k/4]
		}
		for j := 0; j < 4; j++ {
			lane := d.lanes[j*count:]
			switch ctrl := int(ctrlByte>>(j*2)) & 3; ctrl {
			case 3: // stored as is
				if len(data) < count {
					return nil, errShort
				}
				copy(lane[:count], data[:count])
				data = data[count:]
			case 2: // all zero
				clear(lane[:count])
			default:
				table := vertexBitsV0
				if version == 1 {
					table = vertexBitsV1[ctrl:]
				}
				var err error
				if data, err = decodeBytes(data, lane[:aligned], table); err != nil {
					return nil, err
				}
			}
		}
		channel := 0
		if version == 1 {
			channel = int(channels[k/4])
		}
		switch channel & 3 {
		case 0:
			decodeDeltas(1, d.lanes, d.transposed[k:], count, size, last[k:], 0)
		case 1:
			decodeDeltas(2, d.lanes, d.transposed[k:], count, size, last[k:], 0)
		case 2:
			decodeDeltas(4, d.lanes, d.transposed[k:], count, size, last[k:], (32-(channel>>4))&31)
		default:
			return nil, errors.New("meshopt: bad vertex channel")
		}
	}
	copy(out, d.transposed[:count*size])
	copy(last, d.transposed[size*(count-1):size*count])
	return data, nil
}

func decodeBytes(data, out []byte, table []int) ([]byte, error) {
	groups := len(out) / byteGroupSize
	headerSize := (groups + 3) / 4
	if len(data) < headerSize {
		return nil, errShort
	}
	header := data[:headerSize]
	data = data[headerSize:]
	for g := 0; g < groups; g++ {
		if len(data) < byteGroupDecodeLimit {
			return nil, errShort
		}
		bitsIndex := int(header[g/4]>>((g%4)*2)) & 3
		if bitsIndex >= len(table) {
			return nil, errors.New("meshopt: bad bit width")
		}
		data = decodeBytesGroup(data, out[g*byteGroupSize:], table[bitsIndex])
	}
	return data, nil
}

// decodeBytesGroup decodes 16 values stored with the same bit width; a value of all ones means
// the real byte follows the packed bits.
func decodeBytesGroup(data, out []byte, width int) []byte {
	switch width {
	case 0:
		clear(out[:byteGroupSize])
		return data
	case 8:
		copy(out[:byteGroupSize], data[:byteGroupSize])
		return data[byteGroupSize:]
	}
	packed := byteGroupSize * width / 8
	extra := packed
	perByte := 8 / width
	escape := byte(1<<width - 1)
	for i := 0; i < byteGroupSize; i++ {
		b := data[i/perByte]
		if width == 1 {
			b = bits.Reverse8(b) // version 1 stores 1-bit groups in the opposite order
		}
		value := (b << ((i % perByte) * width)) >> (8 - width)
		if value == escape {
			value = data[extra]
			extra++
		}
		out[i] = value
	}
	return data[extra:]
}

func decodeDeltas(width int, lanes, transposed []byte, count, size int, last []byte, rot int) {
	for k := 0; k < 4; k += width {
		var p uint32
		for j := 0; j < width; j++ {
			p |= uint32(last[k+j]) << (8 * j)
		}
		offset := k
		for i := 0; i < count; i++ {
			var v uint32
			for j := 0; j < width; j++ {
				v |= uint32(lanes[(k+j)*count+i]) << (8 * j)
			}
			switch width {
			case 1:
				v = uint32(byte(-(v&1))^byte(v>>1)) + p
			case 2:
				v = uint32(uint16(-(v&1))^uint16(v>>1)) + p
			default:
				v = bits.RotateLeft32(v, rot) ^ p
			}
			for j := 0; j < width; j++ {
				transposed[offset+j] = byte(v >> (8 * j))
			}
			p = v
			offset += size
		}
	}
}

// DecodeIndexBuffer decodes a meshoptimizer-compressed triangle list of count indices of size
// bytes (2 or 4) each.
func DecodeIndexBuffer(count, size int, buffer []byte) ([]byte, error) {
	if count%3 != 0 || (size != 2 && size != 4) {
		return nil, errors.New("meshopt: bad index buffer shape")
	}
	dataOffset := 1 + count/3
	if len(buffer) < dataOffset+16 || buffer[0]&0xF0 != indexHeader {
		return nil, errors.New("meshopt: not a compressed index buffer")
	}
	version := int(buffer[0] & 0x0F)
	if version > 1 {
		return nil, fmt.Errorf("meshopt: index codec version %d is not supported", version)
	}
	var edges [16][2]uint32
	var vertices [16]uint32
	edgeOffset, vertexOffset := 0, 0
	pushEdge := func(a, b uint32) {
		edges[edgeOffset] = [2]uint32{a, b}
		edgeOffset = (edgeOffset + 1) & 15
	}
	pushVertex := func(v uint32, advance bool) {
		vertices[vertexOffset] = v
		if advance {
			vertexOffset = (vertexOffset + 1) & 15
		}
	}
	var next, last uint32
	fecmax := 15
	if version >= 1 {
		fecmax = 13
	}
	code := buffer[1:dataOffset]
	data := buffer[dataOffset:]
	safeEnd := len(data) - 16
	aux := data[safeEnd:]
	position := 0
	var failed bool
	readIndex := func() uint32 {
		if position >= safeEnd {
			failed = true
			return 0
		}
		lead := uint32(data[position])
		position++
		v := lead
		if lead >= 128 {
			v = lead & 127
			shift := 7
			for i := 0; i < 4 && position < len(data); i++ {
				group := uint32(data[position])
				position++
				v |= (group & 127) << shift
				shift += 7
				if group < 128 {
					break
				}
			}
		}
		last += (v >> 1) ^ -(v & 1)
		return last
	}
	out := make([]byte, count*size)
	write := 0
	emit := func(a, b, c uint32) {
		for _, v := range [3]uint32{a, b, c} {
			if size == 2 {
				out[write], out[write+1] = byte(v), byte(v>>8)
			} else {
				out[write], out[write+1], out[write+2], out[write+3] = byte(v), byte(v>>8), byte(v>>16), byte(v>>24)
			}
			write += size
		}
	}
	for _, codetri := range code {
		switch {
		case codetri < 0xf0:
			fe := int(codetri >> 4)
			edge := edges[(edgeOffset-1-fe)&15]
			a, b := edge[0], edge[1]
			var c uint32
			fec := int(codetri & 15)
			if fec < fecmax {
				c = vertices[(vertexOffset-1-fec)&15]
				if fec == 0 {
					c = next
					next++
				}
				pushVertex(c, fec == 0)
			} else {
				if fec != 15 {
					last += uint32(fec*2 - 27)
					c = last
				} else {
					c = readIndex()
				}
				pushVertex(c, true)
			}
			pushEdge(c, b)
			pushEdge(a, c)
			emit(a, b, c)
		case codetri < 0xfe:
			codeaux := aux[codetri&15]
			feb, fec := int(codeaux>>4), int(codeaux&15)
			a := next
			next++
			b := vertices[(vertexOffset-feb)&15]
			if feb == 0 {
				b = next
				next++
			}
			c := vertices[(vertexOffset-fec)&15]
			if fec == 0 {
				c = next
				next++
			}
			emit(a, b, c)
			pushVertex(a, true)
			pushVertex(b, feb == 0)
			pushVertex(c, fec == 0)
			pushEdge(b, a)
			pushEdge(c, b)
			pushEdge(a, c)
		default:
			if position >= safeEnd {
				return nil, errShort
			}
			codeaux := data[position]
			position++
			fea := 15
			if codetri == 0xfe {
				fea = 0
			}
			feb, fec := int(codeaux>>4), int(codeaux&15)
			if codeaux == 0 {
				next = 0
			}
			var a, b, c uint32
			if fea == 0 {
				a = next
				next++
			}
			if feb == 0 {
				b = next
				next++
			} else {
				b = vertices[(vertexOffset-feb)&15]
			}
			if fec == 0 {
				c = next
				next++
			} else {
				c = vertices[(vertexOffset-fec)&15]
			}
			if fea == 15 {
				a = readIndex()
			}
			if feb == 15 {
				b = readIndex()
			}
			if fec == 15 {
				c = readIndex()
			}
			emit(a, b, c)
			pushVertex(a, true)
			pushVertex(b, feb == 0 || feb == 15)
			pushVertex(c, fec == 0 || fec == 15)
			pushEdge(b, a)
			pushEdge(c, b)
			pushEdge(a, c)
		}
		if failed {
			return nil, errShort
		}
	}
	if position != safeEnd {
		return nil, errors.New("meshopt: index buffer is malformed")
	}
	return out, nil
}

// DecodeIndexSequence decodes meshoptimizer's index sequence codec, used for index lists that
// are not triangle lists.
func DecodeIndexSequence(count, size int, buffer []byte) ([]byte, error) {
	if len(buffer) < 1+count+4 || buffer[0]&0xF0 != 0xd0 || buffer[0]&0x0F > 1 {
		return nil, errors.New("meshopt: not an index sequence")
	}
	data := buffer[1 : len(buffer)-4]
	position := 0
	var last [2]uint32
	out := make([]byte, count*size)
	for i := 0; i < count; i++ {
		if position >= len(data) {
			return nil, errShort
		}
		v := uint32(data[position])
		position++
		if v >= 128 {
			v &= 127
			for shift := 7; shift <= 28 && position < len(data); shift += 7 {
				group := uint32(data[position])
				position++
				v |= (group & 127) << shift
				if group < 128 {
					break
				}
			}
		}
		current := v & 1
		v >>= 1
		index := last[current] + ((v >> 1) ^ -(v & 1))
		last[current] = index
		if size == 2 {
			out[2*i], out[2*i+1] = byte(index), byte(index>>8)
		} else {
			out[4*i], out[4*i+1], out[4*i+2], out[4*i+3] = byte(index), byte(index>>8), byte(index>>16), byte(index>>24)
		}
	}
	if position != len(data) {
		return nil, errors.New("meshopt: index sequence is malformed")
	}
	return out, nil
}
