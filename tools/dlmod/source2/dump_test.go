package source2

import (
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"testing"
)

func dumpValue(v *Value, indent string) string {
	switch v.Kind {
	case Object:
		var b strings.Builder
		b.WriteString("{\n")
		for i, k := range v.Keys {
			fmt.Fprintf(&b, "%s  %s = %s\n", indent, k, dumpValue(v.Fields[i], indent+"  "))
		}
		return b.String() + indent + "}"
	case Array:
		var parts []string
		for _, item := range v.Items {
			parts = append(parts, dumpValue(item, indent+"  "))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case String:
		return fmt.Sprintf("%q (flag %d)", v.S, v.Flag)
	case Float32, Float64:
		return fmt.Sprintf("%v (%d)", v.F, v.Kind)
	case Int16, Int32, Int64:
		return fmt.Sprintf("%d (%d)", v.I, v.Kind)
	case Uint16, Uint32, Uint64:
		return fmt.Sprintf("%du (%d)", v.U, v.Kind)
	case Bool:
		return fmt.Sprint(v.B)
	case Blob:
		return fmt.Sprintf("blob %d bytes %x", len(v.Blob), v.Blob[:min(len(v.Blob), 24)])
	}
	return fmt.Sprintf("kind %d", v.Kind)
}

func TestDumpSample(t *testing.T) {
	file := os.Getenv("SAMPLE")
	if file == "" {
		t.Skip()
	}
	data, _ := os.ReadFile(file)
	count := binary.LittleEndian.Uint32(data[12:])
	for i := 0; i < int(count); i++ {
		pos := 16 + 12*i
		typ := string(data[pos : pos+4])
		off := pos + 4 + int(binary.LittleEndian.Uint32(data[pos+4:]))
		size := int(binary.LittleEndian.Uint32(data[pos+8:]))
		if size == 0 || typ == "RERL" {
			continue
		}
		v, _, err := ReadKV3(data[off : off+size])
		fmt.Printf("== %s (%d bytes) err=%v\n", typ, size, err)
		if err == nil {
			fmt.Println(dumpValue(v, ""))
		}
	}
}
