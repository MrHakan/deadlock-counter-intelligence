package source2

import (
	"math"
	"os"
	"testing"
)

// TestAgainstValveSample compiles the MP3 inside a sound Valve's compiler made and compares the
// result's description with Valve's. Set VSND_SAMPLE to a version 5 .vsnd_c to run it (for
// example ValveResourceFormat's Tests/Files/impulse.vsnd_c); game files are not committed.
func TestAgainstValveSample(t *testing.T) {
	file := os.Getenv("VSND_SAMPLE")
	if file == "" {
		t.Skip("VSND_SAMPLE not set")
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	valve, err := DecodeResource(data)
	if err != nil {
		t.Fatal(err)
	}
	want, _, err := ReadKV3(valve.Block("CTRL"))
	if err != nil {
		t.Fatal(err)
	}
	compiled, info, err := CompileSound(valve.Streaming)
	if err != nil {
		t.Fatal(err)
	}
	ours, err := DecodeResource(compiled)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := ReadKV3(ours.Block("CTRL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("ours: %s", dumpValue(got, ""))
	if string(ours.Streaming) != string(valve.Streaming) {
		t.Fatal("the audio must be streamed unchanged")
	}
	w, g := want.Get("m_vSound"), got.Get("m_vSound")
	for _, key := range w.Keys {
		a, b := w.Get(key), g.Get(key)
		if b == nil || a.Kind != b.Kind {
			t.Fatalf("%s: kind %v, Valve wrote %v", key, b, a.Kind)
		}
		if a.I != b.I || a.U != b.U || a.S != b.S || len(a.Items) != len(b.Items) || len(a.Blob) != len(b.Blob) ||
			math.Abs(a.F-b.F) > 1e-6 {
			t.Errorf("%s: %+v, Valve wrote %+v", key, *b, *a)
		}
	}
	for _, key := range want.Keys {
		if got.Get(key) == nil || got.Get(key).Kind != want.Get(key).Kind || got.Get(key).Flag != want.Get(key).Flag {
			t.Errorf("top-level %s differs", key)
		}
	}
	t.Logf("%+v", info.Samples)
}
