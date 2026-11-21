package source2

import "math"

// structuralJoints maps a rigged model's joints onto the hero's bones by the shape of its
// skeleton, for rigs whose joint names say nothing (Meshy's SmartRig calls them Bone_000 to
// Bone_033). Voting by nearness fails on a body unlike the hero's: a round model's arms hang
// beside the hero's legs, so they followed the legs, and its neck sat nearest the low spine.
//
// A humanoid skeleton forks twice: at the chest into a neck and two arms, and near the hips into
// two legs that reach the floor. Those forks are found from where the joints stand (after Fit, in
// the hero's space: Z up, the hero's left side found from its own skeleton), then each chain is
// laid along the hero's: hips to chest over the spine, the neck chain onto neck and head, arms
// onto clavicle, upper arm, forearm and hand, legs onto thigh, shin, ankle and ball. Joints off
// those chains (fingers, twist bones, hair, a cape) are left out: they follow their parent.
//
// It returns nil when the model has no joint positions or no chest fork, so the caller keeps
// voting for skeletons that are not humanoid.
func structuralJoints(mesh *ImportedMesh, skeleton *Skeleton) map[int]string {
	n := len(mesh.JointParents)
	if n == 0 || len(mesh.JointPositions) != n || skeleton == nil {
		return nil
	}
	parent := func(j int) int {
		if p := mesh.JointParents[j]; p >= 0 && p < n {
			return p
		}
		return -1
	}
	// Only joints with skinned joints at or below them matter; the rest are empty helpers.
	relevant, skinned := make([]bool, n), make([]bool, n)
	for _, joints := range mesh.Joints {
		for _, w := range joints {
			if w.Weight <= 0 || w.Joint < 0 || w.Joint >= n {
				continue
			}
			skinned[w.Joint] = true
			for k, depth := w.Joint, 0; k >= 0 && !relevant[k] && depth < 256; k, depth = parent(k), depth+1 {
				relevant[k] = true
			}
		}
	}
	children := make([][]int, n)
	depth := make([]int, n)
	minZ, maxZ := math.Inf(1), math.Inf(-1)
	for j := 0; j < n; j++ {
		if !relevant[j] {
			continue
		}
		if p := parent(j); p >= 0 {
			children[p] = append(children[p], j)
		}
		for k, d := parent(j), 0; k >= 0 && d < 256; k, d = parent(k), d+1 {
			depth[j]++
		}
		z := float64(mesh.JointPositions[j][2])
		minZ, maxZ = math.Min(minZ, z), math.Max(maxZ, z)
	}
	height := maxZ - minZ
	if height <= 0 {
		return nil
	}
	// Each relevant joint's subtree: its size, the middle of its joints and its lowest point.
	size := make([]int, n)
	sum := make([][3]float64, n)
	low := make([]float64, n)
	var gather func(j, d int)
	gather = func(j, d int) {
		size[j], sum[j], low[j] = 1, vec64(mesh.JointPositions[j]), float64(mesh.JointPositions[j][2])
		for _, c := range children[j] {
			if d < 256 {
				gather(c, d+1)
			}
			size[j] += size[c]
			for k := 0; k < 3; k++ {
				sum[j][k] += sum[c][k]
			}
			low[j] = math.Min(low[j], low[c])
		}
	}
	for j := 0; j < n; j++ {
		if relevant[j] && parent(j) < 0 {
			gather(j, 0)
		}
	}
	// Which way is the hero's left: the side its left arm or leg is on.
	left := 1.0
	if pelvis, ok := skeleton.position("pelvis"); ok {
		for _, name := range []string{"arm_upper_L", "clavicle_L", "leg_upper_L"} {
			if p, ok := skeleton.position(name); ok && math.Abs(p[1]-pelvis[1]) > 1e-3 {
				left = math.Copysign(1, p[1]-pelvis[1])
				break
			}
		}
	}
	// offset is where a child's subtree lies from a joint: across (positive to the left) and up.
	offset := func(j, c int) (across, up float64) {
		at := mesh.JointPositions[j]
		middle := [3]float64{sum[c][0] / float64(size[c]), sum[c][1] / float64(size[c]), sum[c][2] / float64(size[c])}
		return (middle[1] - float64(at[1])) * left, middle[2] - float64(at[2])
	}
	floor := minZ + 0.2*height
	side := 0.04 * height
	// sides splits a joint's children into the biggest branch to its left and to its right.
	sides := func(j int) (l, r int) {
		l, r = -1, -1
		for _, c := range children[j] {
			across, _ := offset(j, c)
			switch {
			case across >= side && (l < 0 || size[c] > size[l]):
				l = c
			case across <= -side && (r < 0 || size[c] > size[r]):
				r = c
			}
		}
		return l, r
	}
	var chest, hips = -1, -1
	for j := 0; j < n; j++ {
		if !relevant[j] || len(children[j]) < 2 {
			continue
		}
		l, r := sides(j)
		if l < 0 || r < 0 {
			continue
		}
		if low[l] <= floor && low[r] <= floor { // legs reach the floor
			if hips < 0 || depth[j] < depth[hips] {
				hips = j
			}
		} else if low[l] > floor && low[r] > floor { // arms hang above it
			if chest < 0 || depth[j] < depth[chest] {
				chest = j
			}
		}
	}
	if chest < 0 {
		return nil
	}
	ancestors := func(j int) []int { // j and every joint above it, j first
		var out []int
		for k, d := j, 0; k >= 0 && d < 256; k, d = parent(k), d+1 {
			out = append(out, k)
		}
		return out
	}
	// The pelvis is where the legs' and the chest's lines meet. Without legs it is the first joint
	// down the chest's line that carries weight or branches (not an empty armature node).
	pelvis := chest
	if hips >= 0 {
		above := map[int]bool{}
		for _, k := range ancestors(hips) {
			above[k] = true
		}
		for _, k := range ancestors(chest) {
			if above[k] {
				pelvis = k
				break
			}
		}
	} else {
		line := ancestors(chest)
		for i := len(line) - 1; i >= 0; i-- {
			if skinned[line[i]] || len(children[line[i]]) >= 2 {
				pelvis = line[i]
				break
			}
		}
	}

	out := map[int]string{}
	has := func(name string) bool { _, ok := skeleton.index[name]; return ok }
	first := func(names ...string) string {
		for _, name := range names {
			if has(name) {
				return name
			}
		}
		return ""
	}
	set := func(j int, bone string) {
		if bone != "" {
			out[j] = bone
		}
	}
	// chain follows a branch's main line: through joints with one sizeable child (a twist bone
	// beside the upper arm is a single joint), up to a joint that splits (a hand into fingers).
	chain := func(start int) []int {
		var line []int
		for j, d := start, 0; j >= 0 && d < 256; d++ {
			line = append(line, j)
			next, big := -1, 0
			for _, c := range children[j] {
				if size[c] >= 2 {
					big++
				}
				if next < 0 || size[c] > size[next] {
					next = c
				}
			}
			if big >= 2 {
				break
			}
			j = next
		}
		return line
	}
	// along lays a chain over hero bones in order; extra joints at the end (a hand's tip, toes)
	// stay on the last bone. An arm's clavicle is optional: a chain one joint short starts at the
	// upper arm, while a short leg simply has no ball.
	along := func(line []int, bones []string, optionalFirst bool) {
		var present []string
		for _, b := range bones {
			if has(b) {
				present = append(present, b)
			}
		}
		if len(present) == 0 {
			return
		}
		skip := 0
		if optionalFirst && len(line) < len(present) {
			skip = 1
		}
		for i, j := range line {
			set(j, present[min(skip+i, len(present)-1)])
		}
	}

	for _, k := range ancestors(pelvis) {
		set(k, first("pelvis"))
	}
	if hips >= 0 {
		for _, k := range ancestors(hips) {
			if k == pelvis {
				break
			}
			set(k, first("pelvis"))
		}
	}
	// The hero's spine: bones from its pelvis up to the one its clavicles hang from.
	var spine []string
	if top := first("clavicle_L", "arm_upper_L", "neck_0", "head"); top != "" {
		for k, d := skeleton.Parent[skeleton.index[top]], 0; k >= 0 && d < 64; k, d = skeleton.Parent[k], d+1 {
			if skeleton.Names[k] == "pelvis" {
				break
			}
			if coreBone(skeleton.Names[k]) {
				spine = append([]string{skeleton.Names[k]}, spine...)
			}
		}
	}
	var line []int // the model's spine, pelvis excluded, chest included
	for _, k := range ancestors(chest) {
		if k == pelvis {
			break
		}
		line = append([]int{k}, line...)
	}
	for i, j := range line { // spread over the hero's spine so the chest lands on its top
		if len(spine) == 0 {
			set(j, first("pelvis"))
		} else {
			set(j, spine[((i+1)*len(spine)+len(line)-1)/len(line)-1])
		}
	}
	l, r := sides(chest)
	along(chain(l), []string{"clavicle_L", "arm_upper_L", "arm_lower_L", "hand_L"}, true)
	along(chain(r), []string{"clavicle_R", "arm_upper_R", "arm_lower_R", "hand_R"}, true)
	// The neck: the chest's highest branch that is not an arm.
	neck := -1
	for _, c := range children[chest] {
		if c == l || c == r {
			continue
		}
		if _, up := offset(chest, c); up > 0 && (neck < 0 || size[c] > size[neck]) {
			neck = c
		}
	}
	if neck >= 0 {
		line := chain(neck)
		head := len(line) - 1
		if len(line) >= 3 && len(children[line[head]]) == 0 {
			head-- // a leaf at the end of a long neck chain is the top of the head
		}
		var necks []string
		for _, name := range []string{"neck_0", "neck_1", "neck_2"} {
			if has(name) {
				necks = append(necks, name)
			}
		}
		for i, j := range line {
			switch {
			case i >= head || len(necks) == 0:
				set(j, first("head", "neck_0"))
			default:
				set(j, necks[min(i*len(necks)/max(1, head), len(necks)-1)])
			}
		}
	}
	if hips >= 0 {
		l, r := sides(hips)
		along(chain(l), []string{"leg_upper_L", "leg_lower_L", "ankle_L", "ball_L"}, false)
		along(chain(r), []string{"leg_upper_R", "leg_lower_R", "ankle_R", "ball_R"}, false)
	}
	return out
}
