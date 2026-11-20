package source2

import (
	"fmt"
	"testing"
)

// A rig with names that say nothing, shaped like Meshy's SmartRig: an empty armature node, hips,
// a joint the legs hang from, a spine, a chest with a neck chain, two arms (one with fingers and a
// twist bone, one with an end joint) and a cape. Each joint lands on the hero bone its place in
// the skeleton says, and the joints off the main chains follow their parent.
func TestStructuralJoints(t *testing.T) {
	model, err := LoadModel(SampleHero())
	if err != nil {
		t.Fatal(err)
	}
	skeleton, err := model.Skeleton()
	if err != nil {
		t.Fatal(err)
	}
	type joint struct {
		parent int
		at     [3]float32
		want   string
	}
	joints := []joint{
		{-1, [3]float32{0, 0, 0}, "pelvis"},      // 0 armature
		{0, [3]float32{0, 0, 40}, "pelvis"},      // 1 hips
		{1, [3]float32{0, 0, 38}, "pelvis"},      // 2 where the legs hang from
		{2, [3]float32{0, 5, 36}, "leg_upper_L"}, // 3
		{3, [3]float32{0, 5, 20}, "leg_lower_L"}, // 4
		{4, [3]float32{0, 5, 3}, "ankle_L"},      // 5
		{2, [3]float32{0, -5, 36}, "leg_upper_R"},
		{6, [3]float32{0, -5, 20}, "leg_lower_R"},
		{7, [3]float32{0, -5, 3}, "ankle_R"},
		{1, [3]float32{0, 0, 50}, "spine_3"},       // 9 spine
		{9, [3]float32{0, 0, 60}, "spine_3"},       // 10 chest
		{10, [3]float32{0, 0, 66}, "head"},         // 11 neck (the sample hero has none)
		{11, [3]float32{0, 0, 72}, "head"},         // 12 head
		{12, [3]float32{0, 0, 85}, "head"},         // 13 top of the head
		{10, [3]float32{0, 4, 62}, "clavicle_L"},   // 14
		{14, [3]float32{0, 8, 62}, "arm_upper_L"},  // 15
		{15, [3]float32{0, 18, 54}, "arm_lower_L"}, // 16
		{16, [3]float32{0, 26, 46}, "hand_L"},      // 17
		{17, [3]float32{0, 28, 45}, "hand_L"},      // 18 finger
		{18, [3]float32{0, 30, 44}, "hand_L"},
		{17, [3]float32{1, 28, 47}, "hand_L"}, // 20 finger
		{20, [3]float32{1, 30, 48}, "hand_L"},
		{10, [3]float32{0, -4, 62}, "clavicle_R"}, // 22
		{22, [3]float32{0, -8, 62}, "arm_upper_R"},
		{23, [3]float32{0, -18, 54}, "arm_lower_R"},
		{24, [3]float32{0, -26, 46}, "hand_R"},
		{25, [3]float32{0, -29, 44}, "hand_R"},     // 26 end of the hand
		{15, [3]float32{0, 12, 58}, "arm_upper_L"}, // 27 twist bone beside the upper arm
		{10, [3]float32{-5, 0, 55}, "spine_3"},     // 28 cape
		{28, [3]float32{-6, 0, 30}, "spine_3"},
	}
	mesh := &ImportedMesh{}
	for i, j := range joints {
		mesh.JointNames = append(mesh.JointNames, fmt.Sprintf("Bone_%03d", i))
		mesh.JointParents = append(mesh.JointParents, j.parent)
		mesh.JointPositions = append(mesh.JointPositions, j.at)
		if i > 0 { // one vertex on every joint but the armature
			mesh.Positions = append(mesh.Positions, j.at)
			mesh.Joints = append(mesh.Joints, []JointWeight{{i, 1}})
		}
	}
	skin := make([][]BoneWeight, len(mesh.Positions))
	for i := range skin {
		skin[i] = []BoneWeight{{"pelvis", 1}}
	}
	mapJoints(mesh, skin, make([]float64, len(skin)), skeleton, 4, 85)
	for i, j := range joints[1:] {
		if got := skin[i][0].Bone; len(skin[i]) != 1 || got != j.want {
			t.Errorf("Bone_%03d went to %v, want %s", i+1, skin[i], j.want)
		}
	}
	// Without a chest fork (a snake, a blob) the shape says nothing and voting stays.
	snake := &ImportedMesh{JointNames: []string{"a", "b", "c"}, JointParents: []int{-1, 0, 1},
		JointPositions: [][3]float32{{0, 0, 0}, {10, 0, 0}, {20, 0, 0}}, Joints: [][]JointWeight{{{0, 1}}, {{1, 1}}, {{2, 1}}}}
	if got := structuralJoints(snake, skeleton); got != nil {
		t.Errorf("a snake was mapped as a body: %v", got)
	}
}
