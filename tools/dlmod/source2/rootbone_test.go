package source2

import (
	"math"
	"testing"
)

// The game places the root with no turn; rootUnturn's output, skinned that way with the other
// bones at rest, must land back on the positions and normals it was given.
func TestRootUnturn(t *testing.T) {
	sk := &Skeleton{
		Names:  []string{"root_motion", "pelvis"},
		Parent: []int{-1, 0},
		// The heroes' root at rest: x->y, y->z, z->x, moved a little to test the shift too.
		World: []mat34{{0, 0, 1, 2, 1, 0, 0, -1, 0, 1, 0, 0.5}, identity34()},
	}
	positions := [][3]float32{{10, 4, 30}, {3, -7, 60}, {1, 2, 3}}
	normals := [][3]float32{{1, 0, 0}, {0, 0.6, 0.8}, {0, 0, 1}}
	skin := [][]BoneWeight{
		{{"root_motion", 1}},
		{{"root_motion", 0.7}, {"pelvis", 0.3}},
		{{"pelvis", 1}},
	}
	gotP, gotN := rootUnturn(sk, positions, normals, skin)
	if gotP[2] != positions[2] || gotN[2] != normals[2] {
		t.Errorf("a vertex off the root moved: %v %v", gotP[2], gotN[2])
	}
	root := sk.World[0]
	place := func(v [3]float32, w float64, point bool) [3]float64 {
		var out [3]float64
		for r := 0; r < 3; r++ {
			var turned float64
			for c := 0; c < 3; c++ {
				turned += root[c*4+r] * (float64(v[c]))
				if point {
					turned -= root[c*4+r] * root[c*4+3]
				}
			}
			out[r] = w*turned + (1-w)*float64(v[r])
		}
		return out
	}
	for i, w := range []float64{1, 0.7} {
		p := place(gotP[i], w, true)
		n := place(gotN[i], w, false)
		l := math.Sqrt(n[0]*n[0] + n[1]*n[1] + n[2]*n[2])
		for k := 0; k < 3; k++ {
			if math.Abs(p[k]-float64(positions[i][k])) > 1e-3 {
				t.Errorf("vertex %d lands at %v, want %v", i, p, positions[i])
				break
			}
			if math.Abs(n[k]/l-float64(normals[i][k])) > 1e-3 {
				t.Errorf("normal %d turns to %v, want %v", i, n, normals[i])
				break
			}
		}
	}
}
