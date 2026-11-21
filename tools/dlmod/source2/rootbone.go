package source2

import "math"

// The game does not pose a skeleton's root bone (root_motion on the heroes) from its rest pose:
// it puts the root where the character stands, with no turn, while the rest pose turns it a
// quarter about two axes. A vertex left on the root as it rests lies on its back in game. So the
// parts of a vertex that follow the root are written turned the other way, and every other bone
// is taken at rest: then, posed at rest, the vertex is where the model put it.

// rootMotion is the heroes' root bone, where the game puts the character. Only it is turned: a
// model whose root is some other bone (a pelvis) is left as it is.
const rootMotion = "root_motion"

// rootUnturn returns the vertices' positions and normals rewritten for the game's root, or the
// ones given when no vertex follows the root.
func rootUnturn(sk *Skeleton, positions, normals [][3]float32, skin [][]BoneWeight) ([][3]float32, [][3]float32) {
	roots := map[string]mat34{}
	for i, name := range sk.Names {
		if sk.Parent[i] < 0 && name == rootMotion {
			roots[name] = sk.World[i]
		}
	}
	if len(roots) == 0 {
		return positions, normals
	}
	var outP, outN [][3]float32
	for i, bones := range skin {
		var a [3][3]float64 // how the vertex's position moves when the root is placed: sum of w*M
		var shift [3]float64
		var total, onRoot float64
		for _, b := range bones {
			w := float64(b.Weight)
			total += w
			world, ok := roots[b.Bone]
			if !ok {
				continue
			}
			onRoot += w
			// The root's skin matrix in game is its rest pose inverted: R^T, and -R^T t.
			for r := 0; r < 3; r++ {
				for c := 0; c < 3; c++ {
					a[r][c] += w * world[c*4+r]
				}
				for c := 0; c < 3; c++ {
					shift[r] -= w * world[c*4+r] * world[c*4+3]
				}
			}
		}
		if onRoot == 0 || total == 0 {
			continue
		}
		for r := 0; r < 3; r++ {
			a[r][r] += total - onRoot // the other bones, at rest
			for c := 0; c < 3; c++ {
				a[r][c] /= total
			}
			shift[r] /= total
		}
		inv, ok := invert3(a)
		if !ok {
			continue
		}
		if outP == nil {
			outP = append([][3]float32{}, positions...)
			outN = append([][3]float32{}, normals...)
		}
		p := positions[i]
		q := [3]float64{float64(p[0]) - shift[0], float64(p[1]) - shift[1], float64(p[2]) - shift[2]}
		outP[i] = mul3(inv, q)
		if i < len(normals) {
			n := normals[i]
			t := mul3(inv, [3]float64{float64(n[0]), float64(n[1]), float64(n[2])})
			l := math.Sqrt(float64(t[0]*t[0] + t[1]*t[1] + t[2]*t[2]))
			if l > 0 {
				outN[i] = [3]float32{t[0] / float32(l), t[1] / float32(l), t[2] / float32(l)}
			}
		}
	}
	if outP == nil {
		return positions, normals
	}
	return outP, outN
}

func mul3(m [3][3]float64, v [3]float64) [3]float32 {
	var out [3]float32
	for r := 0; r < 3; r++ {
		out[r] = float32(m[r][0]*v[0] + m[r][1]*v[1] + m[r][2]*v[2])
	}
	return out
}

func invert3(m [3][3]float64) ([3][3]float64, bool) {
	det := m[0][0]*(m[1][1]*m[2][2]-m[1][2]*m[2][1]) -
		m[0][1]*(m[1][0]*m[2][2]-m[1][2]*m[2][0]) +
		m[0][2]*(m[1][0]*m[2][1]-m[1][1]*m[2][0])
	if math.Abs(det) < 1e-6 {
		return m, false
	}
	var out [3][3]float64
	for r := 0; r < 3; r++ {
		for c := 0; c < 3; c++ {
			r1, r2 := (c+1)%3, (c+2)%3
			c1, c2 := (r+1)%3, (r+2)%3
			out[r][c] = (m[r1][c1]*m[r2][c2] - m[r1][c2]*m[r2][c1]) / det
		}
	}
	return out, true
}
