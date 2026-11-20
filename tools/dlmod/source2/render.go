package source2

import (
	"image"
	"image/color"
	"math"
)

// Triangles is a plain triangle soup, which previews are drawn from.
type Triangles struct {
	Positions [][3]float32
	Indices   []uint32
}

// Triangles collects the geometry a mesh draws.
func (mesh *Mesh) Triangles() (Triangles, error) {
	var out Triangles
	bases := map[int]uint32{} // draw calls sharing a vertex buffer share its vertices
	for _, draw := range mesh.DrawCalls {
		if draw.VertexBuffer >= len(mesh.VertexBuffers) || draw.IndexBuffer >= len(mesh.IndexBuffers) {
			continue
		}
		vb := mesh.VertexBuffers[draw.VertexBuffer]
		position := vb.Attribute("position", 0)
		if position == nil {
			continue
		}
		base, seen := bases[draw.VertexBuffer]
		if !seen {
			values, err := vb.Floats(position)
			if err != nil {
				return out, err
			}
			base = uint32(len(out.Positions))
			bases[draw.VertexBuffer] = base
			for _, v := range values {
				out.Positions = append(out.Positions, [3]float32{v[0], v[1], v[2]})
			}
		}
		indices := mesh.IndexBuffers[draw.IndexBuffer].Indices()
		end := min(len(indices), draw.StartIndex+draw.IndexCount)
		for _, i := range indices[min(draw.StartIndex, end):end] {
			out.Indices = append(out.Indices, base+i+uint32(draw.BaseVertex))
		}
	}
	return out, nil
}

// RenderPreview draws triangles with flat shading, viewed from yaw degrees around the up (Z)
// axis, into a width x height image with a transparent background.
func RenderPreview(sets []Triangles, width, height int, yaw float64) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	sin, cos := math.Sincos(yaw * math.Pi / 180)
	// Camera space: x to the right, y up, z towards the viewer.
	view := func(p [3]float32) [3]float64 {
		x, y, z := float64(p[0]), float64(p[1]), float64(p[2])
		return [3]float64{x*sin + y*cos, z, x*cos - y*sin}
	}
	lo := [3]float64{math.Inf(1), math.Inf(1), math.Inf(1)}
	hi := [3]float64{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	for _, set := range sets {
		for _, p := range set.Positions {
			v := view(p)
			for k := 0; k < 3; k++ {
				lo[k], hi[k] = math.Min(lo[k], v[k]), math.Max(hi[k], v[k])
			}
		}
	}
	if math.IsInf(lo[0], 1) {
		return img
	}
	scale := 0.94 * math.Min(float64(width)/math.Max(hi[0]-lo[0], 1e-6), float64(height)/math.Max(hi[1]-lo[1], 1e-6))
	cx, cy := (lo[0]+hi[0])/2, (lo[1]+hi[1])/2
	depth := make([]float64, width*height)
	for i := range depth {
		depth[i] = math.Inf(-1)
	}
	light := normalize([3]float64{0.4, 0.6, 0.7})
	for _, set := range sets {
		screen := make([][3]float64, len(set.Positions))
		for i, p := range set.Positions {
			v := view(p)
			screen[i] = [3]float64{float64(width)/2 + (v[0]-cx)*scale, float64(height)/2 - (v[1]-cy)*scale, v[2]}
		}
		for t := 0; t+2 < len(set.Indices); t += 3 {
			a, b, c := set.Indices[t], set.Indices[t+1], set.Indices[t+2]
			if int(a) >= len(screen) || int(b) >= len(screen) || int(c) >= len(screen) {
				continue
			}
			pa, pb, pc := view(set.Positions[a]), view(set.Positions[b]), view(set.Positions[c])
			n := normalize(cross(sub(pb, pa), sub(pc, pa)))
			shade := math.Abs(dot(n, light)) // models are not always wound the same way
			r := uint8(math.Min(255, 60+170*shade))
			fill(img, depth, screen[a], screen[b], screen[c], color.RGBA{r, uint8(float64(r) * 0.93), uint8(float64(r) * 0.85), 255})
		}
	}
	return img
}

func fill(img *image.RGBA, depth []float64, a, b, c [3]float64, col color.RGBA) {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	minX := max(0, int(math.Floor(math.Min(a[0], math.Min(b[0], c[0])))))
	maxX := min(w-1, int(math.Ceil(math.Max(a[0], math.Max(b[0], c[0])))))
	minY := max(0, int(math.Floor(math.Min(a[1], math.Min(b[1], c[1])))))
	maxY := min(h-1, int(math.Ceil(math.Max(a[1], math.Max(b[1], c[1])))))
	area := (b[0]-a[0])*(c[1]-a[1]) - (b[1]-a[1])*(c[0]-a[0])
	if area == 0 {
		return
	}
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			w0 := ((b[0]-px)*(c[1]-py) - (b[1]-py)*(c[0]-px)) / area
			w1 := ((c[0]-px)*(a[1]-py) - (c[1]-py)*(a[0]-px)) / area
			w2 := 1 - w0 - w1
			if w0 < 0 || w1 < 0 || w2 < 0 {
				continue
			}
			z := w0*a[2] + w1*b[2] + w2*c[2]
			if z <= depth[y*w+x] {
				continue
			}
			depth[y*w+x] = z
			img.SetRGBA(x, y, col)
		}
	}
}

func sub(a, b [3]float64) [3]float64 { return [3]float64{a[0] - b[0], a[1] - b[1], a[2] - b[2]} }
func dot(a, b [3]float64) float64    { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }
func cross(a, b [3]float64) [3]float64 {
	return [3]float64{a[1]*b[2] - a[2]*b[1], a[2]*b[0] - a[0]*b[2], a[0]*b[1] - a[1]*b[0]}
}
func normalize(v [3]float64) [3]float64 {
	l := math.Sqrt(dot(v, v))
	if l == 0 {
		return v
	}
	return [3]float64{v[0] / l, v[1] / l, v[2] / l}
}
