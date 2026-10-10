package vid

import (
	"errors"
	"testing"
)

func TestPOCType0WrapsLSBAndUpdatesReferenceState(t *testing.T) {
	tests := []struct {
		name  string
		state POCType0State
		lsb   uint32
		delta int64
		idr   bool
		want  int64
		msb   int64
		prev  uint32
	}{
		{name: "initial picture", lsb: 3, want: 3, msb: 0, prev: 3},
		{name: "wrap upward at half range", state: POCType0State{PreviousPicOrderCntLSB: 15}, lsb: 7, want: 23, msb: 16, prev: 7},
		{name: "wrap downward above half range", state: POCType0State{PreviousPicOrderCntMSB: 16, PreviousPicOrderCntLSB: 1}, lsb: 10, want: 10, msb: 0, prev: 10},
		{name: "do not wrap upward at exact half range", state: POCType0State{PreviousPicOrderCntLSB: 1}, lsb: 9, want: 9, msb: 0, prev: 9},
		{name: "choose smaller bottom field order", lsb: 5, delta: -8, want: -3, msb: 0, prev: 5},
		{name: "IDR resets prior state", state: POCType0State{PreviousPicOrderCntMSB: 64, PreviousPicOrderCntLSB: 15}, lsb: 2, idr: true, want: 2, msb: 0, prev: 2},
		{name: "IDR ignores stale prior LSB", state: POCType0State{PreviousPicOrderCntLSB: ^uint32(0)}, lsb: 2, idr: true, want: 2, msb: 0, prev: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := test.state
			got, err := state.Calculate(16, test.lsb, test.delta, 1, test.idr)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want || state.PreviousPicOrderCntMSB != test.msb || state.PreviousPicOrderCntLSB != test.prev {
				t.Fatalf("POC/state = %d/%+v; want %d/{MSB:%d LSB:%d}", got, state, test.want, test.msb, test.prev)
			}
		})
	}
}

func TestDeriveLumaDeblockingParametersClipsIndicesAndMapsDisableMode(t *testing.T) {
	tests := []struct {
		name       string
		qPP        int
		qPQ        int
		disableIDC uint8
		alpha      int64
		beta       int64
		want       LumaDeblockingParameters
	}{
		{name: "average QP rounds up before offsets", qPP: 20, qPQ: 21, disableIDC: 0, alpha: -3, beta: 2, want: LumaDeblockingParameters{Mode: LumaDeblockingAllEdges, IndexA: 15, IndexB: 25}},
		{name: "clip lower", qPP: 0, qPQ: 1, disableIDC: 2, alpha: -6, beta: -1, want: LumaDeblockingParameters{Mode: LumaDeblockingAllExceptSliceBoundaries, IndexA: 0, IndexB: 0}},
		{name: "clip upper and disable", qPP: 51, qPQ: 50, disableIDC: 1, alpha: 6, beta: 2, want: LumaDeblockingParameters{Mode: LumaDeblockingDisabled, IndexA: 51, IndexB: 51}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := DeriveLumaDeblockingParameters(test.qPP, test.qPQ, test.disableIDC, test.alpha, test.beta)
			if err != nil || got != test.want {
				t.Fatalf("parameters = %+v, %v; want %+v, nil", got, err, test.want)
			}
		})
	}
}

func TestDeriveLumaDeblockingParametersRejectsOutOfRangeInputs(t *testing.T) {
	tests := []struct {
		qPP, qPQ   int
		disableIDC uint8
		alpha      int64
		beta       int64
	}{
		{qPP: -1, qPQ: 26}, {qPP: 26, qPQ: 52}, {qPP: 26, qPQ: 26, disableIDC: 3},
		{qPP: 26, qPQ: 26, alpha: -7}, {qPP: 26, qPQ: 26, alpha: 7},
		{qPP: 26, qPQ: 26, beta: -7}, {qPP: 26, qPQ: 26, beta: 7},
	}
	for _, test := range tests {
		if _, err := DeriveLumaDeblockingParameters(test.qPP, test.qPQ, test.disableIDC, test.alpha, test.beta); !errors.Is(err, ErrLumaDeblockingParameters) {
			t.Errorf("DeriveLumaDeblockingParameters(%+v) error = %v; want %v", test, err, ErrLumaDeblockingParameters)
		}
	}
}

func TestLookupLumaDeblockingThresholdsMatchesTableVectors(t *testing.T) {
	tests := []struct {
		indexA, indexB uint8
		want           LumaDeblockingThresholds
	}{
		{0, 0, LumaDeblockingThresholds{Alpha: 0, Beta: 0}},
		{16, 17, LumaDeblockingThresholds{Alpha: 4, Beta: 0}},
		{18, 18, LumaDeblockingThresholds{Alpha: 5, Beta: 2}},
		{26, 26, LumaDeblockingThresholds{Alpha: 15, Beta: 4}},
		{40, 40, LumaDeblockingThresholds{Alpha: 80, Beta: 12}},
		{51, 51, LumaDeblockingThresholds{Alpha: 255, Beta: 18}},
	}
	for _, test := range tests {
		got, err := LookupLumaDeblockingThresholds(test.indexA, test.indexB)
		if err != nil || got != test.want {
			t.Errorf("thresholds(%d, %d) = %+v, %v; want %+v, nil", test.indexA, test.indexB, got, err, test.want)
		}
	}
	if _, err := LookupLumaDeblockingThresholds(52, 0); !errors.Is(err, ErrLumaDeblockingParameters) {
		t.Errorf("out-of-range lookup error = %v; want %v", err, ErrLumaDeblockingParameters)
	}
}

func TestLookupLumaTC0MatchesTableVectors(t *testing.T) {
	tests := []struct {
		indexA, boundaryStrength uint8
		want                     uint8
	}{
		{0, 1, 0},
		{23, 1, 1},
		{21, 2, 1},
		{18, 3, 1},
		{33, 1, 2},
		{51, 1, 13},
		{51, 2, 17},
		{51, 3, 25},
	}
	for _, test := range tests {
		got, err := LookupLumaTC0(test.indexA, test.boundaryStrength)
		if err != nil || got != test.want {
			t.Errorf("tC0(%d, %d) = %d, %v; want %d, nil", test.indexA, test.boundaryStrength, got, err, test.want)
		}
	}
	for _, invalid := range [][2]uint8{{52, 1}, {0, 0}, {0, 4}} {
		if _, err := LookupLumaTC0(invalid[0], invalid[1]); !errors.Is(err, ErrLumaDeblockingParameters) {
			t.Errorf("tC0(%d, %d) error = %v; want %v", invalid[0], invalid[1], err, ErrLumaDeblockingParameters)
		}
	}
}

func TestShouldFilterLumaEdgeUsesStrictThresholds(t *testing.T) {
	tests := []struct {
		name             string
		boundaryStrength uint8
		thresholds       LumaDeblockingThresholds
		want             bool
		wantErr          bool
	}{
		{name: "all checks pass", boundaryStrength: 1, thresholds: LumaDeblockingThresholds{Alpha: 5, Beta: 11}, want: true},
		{name: "strong boundary still eligible", boundaryStrength: 4, thresholds: LumaDeblockingThresholds{Alpha: 5, Beta: 11}, want: true},
		{name: "zero strength", boundaryStrength: 0, thresholds: LumaDeblockingThresholds{Alpha: 255, Beta: 255}},
		{name: "alpha equality rejects", boundaryStrength: 1, thresholds: LumaDeblockingThresholds{Alpha: 4, Beta: 11}},
		{name: "p beta equality rejects", boundaryStrength: 1, thresholds: LumaDeblockingThresholds{Alpha: 5, Beta: 10}},
		{name: "q beta equality rejects", boundaryStrength: 1, thresholds: LumaDeblockingThresholds{Alpha: 5, Beta: 9}},
		{name: "invalid strength", boundaryStrength: 5, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ShouldFilterLumaEdge(100, 104, 90, 113, test.boundaryStrength, test.thresholds)
			if (err != nil) != test.wantErr || got != test.want {
				t.Fatalf("filter decision = %v, %v; want %v, error=%v", got, err, test.want, test.wantErr)
			}
		})
	}
}

func TestFilterLumaWeakEdgeMatchesNormativeVectors(t *testing.T) {
	tests := []struct {
		name    string
		samples LumaEdgeSamples
		beta    uint8
		tc0     uint8
		want    LumaEdgeSamples
	}{
		{
			name:    "updates both smooth sides",
			samples: LumaEdgeSamples{P0: 100, P1: 98, P2: 96, Q0: 104, Q1: 105, Q2: 106},
			beta:    10,
			tc0:     2,
			want:    LumaEdgeSamples{P0: 101, P1: 99, P2: 96, Q0: 103, Q1: 104, Q2: 106},
		},
		{
			name:    "clips delta and preserves rough sides",
			samples: LumaEdgeSamples{P0: 110, P1: 110, P2: 120, Q0: 100, Q1: 100, Q2: 90},
			beta:    5,
			tc0:     1,
			want:    LumaEdgeSamples{P0: 109, P1: 110, P2: 120, Q0: 101, Q1: 100, Q2: 90},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := FilterLumaWeakEdge(test.samples, test.beta, test.tc0); got != test.want {
				t.Fatalf("filtered samples = %+v; want %+v", got, test.want)
			}
		})
	}
}

func TestFilterLumaStrongEdgeSelectsStrongAndFallbackBranches(t *testing.T) {
	strong := LumaStrongEdgeSamples{P0: 100, P1: 99, P2: 98, P3: 97, Q0: 102, Q1: 103, Q2: 104, Q3: 105}
	if got, want := FilterLumaStrongEdge(strong, 16, 5), (LumaEdgeSamples{P0: 100, P1: 100, P2: 99, Q0: 102, Q1: 102, Q2: 103}); got != want {
		t.Fatalf("strong-edge result = %+v; want %+v", got, want)
	}

	oneStrongSide := LumaStrongEdgeSamples{P0: 100, P1: 99, P2: 98, P3: 97, Q0: 102, Q1: 103, Q2: 130, Q3: 120}
	if got, want := FilterLumaStrongEdge(oneStrongSide, 16, 5), (LumaEdgeSamples{P0: 100, P1: 100, P2: 99, Q0: 102, Q1: 103, Q2: 130}); got != want {
		t.Fatalf("one-sided strong-edge result = %+v; want %+v", got, want)
	}

	if got, want := FilterLumaStrongEdge(strong, 0, 5), (LumaEdgeSamples{P0: 100, P1: 99, P2: 98, Q0: 102, Q1: 103, Q2: 104}); got != want {
		t.Fatalf("strong-limit fallback result = %+v; want %+v", got, want)
	}
}

func TestApplyLumaDeblockingEdgeSelectsFilterAndHonorsMode(t *testing.T) {
	samples := LumaStrongEdgeSamples{P0: 100, P1: 98, P2: 96, P3: 95, Q0: 104, Q1: 105, Q2: 106, Q3: 107}
	unchanged := LumaEdgeSamples{P0: 100, P1: 98, P2: 96, Q0: 104, Q1: 105, Q2: 106}

	tests := []struct {
		name          string
		strength      uint8
		parameters    LumaDeblockingParameters
		sliceBoundary bool
		want          LumaEdgeSamples
	}{
		{
			name:       "weak edge",
			strength:   2,
			parameters: LumaDeblockingParameters{Mode: LumaDeblockingAllEdges, IndexA: 33, IndexB: 40},
			want:       LumaEdgeSamples{P0: 101, P1: 99, P2: 96, Q0: 103, Q1: 104, Q2: 106},
		},
		{
			name:       "strong edge",
			strength:   4,
			parameters: LumaDeblockingParameters{Mode: LumaDeblockingAllEdges, IndexA: 26, IndexB: 30},
			want:       LumaEdgeSamples{P0: 101, P1: 100, P2: 98, Q0: 103, Q1: 104, Q2: 105},
		},
		{
			name:       "threshold rejection",
			strength:   1,
			parameters: LumaDeblockingParameters{Mode: LumaDeblockingAllEdges},
			want:       unchanged,
		},
		{
			name:       "disabled",
			strength:   4,
			parameters: LumaDeblockingParameters{Mode: LumaDeblockingDisabled, IndexA: 26, IndexB: 30},
			want:       unchanged,
		},
		{
			name:          "idc2 skips slice-boundary edge",
			strength:      4,
			parameters:    LumaDeblockingParameters{Mode: LumaDeblockingAllExceptSliceBoundaries, IndexA: 26, IndexB: 30},
			sliceBoundary: true,
			want:          unchanged,
		},
		{
			name:       "idc2 filters non-slice-boundary edge",
			strength:   4,
			parameters: LumaDeblockingParameters{Mode: LumaDeblockingAllExceptSliceBoundaries, IndexA: 26, IndexB: 30},
			want:       LumaEdgeSamples{P0: 101, P1: 100, P2: 98, Q0: 103, Q1: 104, Q2: 105},
		},
		{
			name:          "idc0 filters slice-boundary edge",
			strength:      4,
			parameters:    LumaDeblockingParameters{Mode: LumaDeblockingAllEdges, IndexA: 26, IndexB: 30},
			sliceBoundary: true,
			want:          LumaEdgeSamples{P0: 101, P1: 100, P2: 98, Q0: 103, Q1: 104, Q2: 105},
		},
		{
			name:       "zero strength",
			parameters: LumaDeblockingParameters{Mode: LumaDeblockingAllEdges, IndexA: 26, IndexB: 30},
			want:       unchanged,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ApplyLumaDeblockingEdge(samples, test.strength, test.parameters, test.sliceBoundary)
			if err != nil || got != test.want {
				t.Fatalf("filtered edge = %+v, %v; want %+v, nil", got, err, test.want)
			}
		})
	}
}

func TestApplyLumaDeblockingEdgeRejectsInvalidInputs(t *testing.T) {
	samples := LumaStrongEdgeSamples{}
	for _, test := range []struct {
		strength   uint8
		parameters LumaDeblockingParameters
		wantErr    error
	}{
		{strength: 5, parameters: LumaDeblockingParameters{Mode: LumaDeblockingAllEdges}, wantErr: ErrLumaDeblockingBoundaryStrength},
		{parameters: LumaDeblockingParameters{Mode: LumaDeblockingMode(3)}, wantErr: ErrLumaDeblockingParameters},
		{parameters: LumaDeblockingParameters{Mode: LumaDeblockingAllEdges, IndexA: 52}, wantErr: ErrLumaDeblockingParameters},
	} {
		if _, err := ApplyLumaDeblockingEdge(samples, test.strength, test.parameters, false); !errors.Is(err, test.wantErr) {
			t.Errorf("ApplyLumaDeblockingEdge(%+v) error = %v; want %v", test, err, test.wantErr)
		}
	}
}

func TestApplyLumaDeblockingEdgeSegmentPreservesOrderAndMode(t *testing.T) {
	samples := [4]LumaStrongEdgeSamples{
		{P0: 100, P1: 98, P2: 96, P3: 95, Q0: 104, Q1: 105, Q2: 106, Q3: 107},
		{P0: 100, P1: 99, P2: 98, P3: 97, Q0: 102, Q1: 103, Q2: 130, Q3: 120},
		{P0: 100, P1: 98, P2: 96, P3: 95, Q0: 120, Q1: 121, Q2: 122, Q3: 123},
		{P0: 100, P1: 98, P2: 96, P3: 95, Q0: 104, Q1: 105, Q2: 106, Q3: 107},
	}
	parameters := LumaDeblockingParameters{Mode: LumaDeblockingAllExceptSliceBoundaries, IndexA: 26, IndexB: 30}
	got, err := ApplyLumaDeblockingEdgeSegment(samples, 4, parameters, false)
	want := [4]LumaEdgeSamples{
		{P0: 101, P1: 100, P2: 98, Q0: 103, Q1: 104, Q2: 105},
		{P0: 100, P1: 100, P2: 99, Q0: 102, Q1: 103, Q2: 130},
		{P0: 100, P1: 98, P2: 96, Q0: 120, Q1: 121, Q2: 122},
		{P0: 101, P1: 100, P2: 98, Q0: 103, Q1: 104, Q2: 105},
	}
	if err != nil || got != want {
		t.Fatalf("filtered edge segment = %+v, %v; want %+v, nil", got, err, want)
	}
	unchanged, err := ApplyLumaDeblockingEdgeSegment(samples, 4, parameters, true)
	if err != nil {
		t.Fatal(err)
	}
	for index, edgeSamples := range samples {
		wantUnchanged := LumaEdgeSamples{P0: edgeSamples.P0, P1: edgeSamples.P1, P2: edgeSamples.P2, Q0: edgeSamples.Q0, Q1: edgeSamples.Q1, Q2: edgeSamples.Q2}
		if unchanged[index] != wantUnchanged {
			t.Errorf("slice-boundary sample %d = %+v; want %+v", index, unchanged[index], wantUnchanged)
		}
	}
}

func TestApplyLumaDeblockingPlaneEdgeSegmentHandlesOrientationsAndBounds(t *testing.T) {
	inputs := [4]LumaStrongEdgeSamples{
		{P0: 100, P1: 98, P2: 96, P3: 95, Q0: 104, Q1: 105, Q2: 106, Q3: 107},
		{P0: 100, P1: 99, P2: 98, P3: 97, Q0: 102, Q1: 103, Q2: 130, Q3: 120},
		{P0: 100, P1: 98, P2: 96, P3: 95, Q0: 120, Q1: 121, Q2: 122, Q3: 123},
		{P0: 100, P1: 98, P2: 96, P3: 95, Q0: 104, Q1: 105, Q2: 106, Q3: 107},
	}
	want := [4]LumaEdgeSamples{
		{P0: 101, P1: 100, P2: 98, Q0: 103, Q1: 104, Q2: 105},
		{P0: 100, P1: 100, P2: 99, Q0: 102, Q1: 103, Q2: 130},
		{P0: 100, P1: 98, P2: 96, Q0: 120, Q1: 121, Q2: 122},
		{P0: 101, P1: 100, P2: 98, Q0: 103, Q1: 104, Q2: 105},
	}
	parameters := LumaDeblockingParameters{Mode: LumaDeblockingAllEdges, IndexA: 26, IndexB: 30}
	for _, vertical := range []bool{true, false} {
		width, height, stride := 12, 12, 15
		x, y := 6, 2
		if !vertical {
			x, y = 2, 6
		}
		plane := make([]uint8, stride*height)
		for index := range plane {
			plane[index] = 250
		}
		for lane, samples := range inputs {
			q0Index := y*stride + x + lane
			if vertical {
				q0Index = (y+lane)*stride + x
				plane[q0Index-4], plane[q0Index-3], plane[q0Index-2], plane[q0Index-1] = samples.P3, samples.P2, samples.P1, samples.P0
				plane[q0Index], plane[q0Index+1], plane[q0Index+2], plane[q0Index+3] = samples.Q0, samples.Q1, samples.Q2, samples.Q3
			} else {
				plane[q0Index-4*stride], plane[q0Index-3*stride], plane[q0Index-2*stride], plane[q0Index-stride] = samples.P3, samples.P2, samples.P1, samples.P0
				plane[q0Index], plane[q0Index+stride], plane[q0Index+2*stride], plane[q0Index+3*stride] = samples.Q0, samples.Q1, samples.Q2, samples.Q3
			}
		}
		if err := ApplyLumaDeblockingPlaneEdgeSegment(plane, width, height, stride, x, y, vertical, 4, parameters, false); err != nil {
			t.Fatalf("vertical=%t: %v", vertical, err)
		}
		for lane, expected := range want {
			q0Index := y*stride + x + lane
			if vertical {
				q0Index = (y+lane)*stride + x
				got := LumaEdgeSamples{P0: plane[q0Index-1], P1: plane[q0Index-2], P2: plane[q0Index-3], Q0: plane[q0Index], Q1: plane[q0Index+1], Q2: plane[q0Index+2]}
				if got != expected || plane[q0Index-4] != inputs[lane].P3 || plane[q0Index+3] != inputs[lane].Q3 {
					t.Errorf("vertical=%t lane=%d: got %+v; want %+v", vertical, lane, got, expected)
				}
			} else {
				got := LumaEdgeSamples{P0: plane[q0Index-stride], P1: plane[q0Index-2*stride], P2: plane[q0Index-3*stride], Q0: plane[q0Index], Q1: plane[q0Index+stride], Q2: plane[q0Index+2*stride]}
				if got != expected || plane[q0Index-4*stride] != inputs[lane].P3 || plane[q0Index+3*stride] != inputs[lane].Q3 {
					t.Errorf("vertical=%t lane=%d: got %+v; want %+v", vertical, lane, got, expected)
				}
			}
		}
		for row := 0; row < height; row++ {
			for column := width; column < stride; column++ {
				if plane[row*stride+column] != 250 {
					t.Fatalf("padding changed at row=%d column=%d", row, column)
				}
			}
		}
	}

	plane := make([]uint8, 12*12)
	before := append([]uint8(nil), plane...)
	if err := ApplyLumaDeblockingPlaneEdgeSegment(plane, 12, 12, 12, 3, 2, true, 4, parameters, false); !errors.Is(err, ErrLumaDeblockingPlaneLayout) {
		t.Fatalf("invalid edge error = %v; want %v", err, ErrLumaDeblockingPlaneLayout)
	}
	for index := range plane {
		if plane[index] != before[index] {
			t.Fatalf("invalid edge modified plane at index %d", index)
		}
	}
}

func TestApplyLumaDeblockingPlaneMacroblockEdgeUsesPerSegmentStrengths(t *testing.T) {
	inputs := LumaStrongEdgeSamples{P0: 100, P1: 98, P2: 96, P3: 95, Q0: 104, Q1: 105, Q2: 106, Q3: 107}
	parameters := LumaDeblockingParameters{Mode: LumaDeblockingAllEdges, IndexA: 33, IndexB: 40}
	boundaryStrengths := [4]uint8{4, 0, 2, 0}
	for _, vertical := range []bool{true, false} {
		const width, height, stride = 24, 24, 27
		x, y := 8, 2
		if !vertical {
			x, y = 2, 8
		}
		plane := make([]uint8, stride*height)
		for index := range plane {
			plane[index] = 250
		}
		for lane := 0; lane < 16; lane++ {
			q0Index := y*stride + x + lane
			if vertical {
				q0Index = (y+lane)*stride + x
				plane[q0Index-4], plane[q0Index-3], plane[q0Index-2], plane[q0Index-1] = inputs.P3, inputs.P2, inputs.P1, inputs.P0
				plane[q0Index], plane[q0Index+1], plane[q0Index+2], plane[q0Index+3] = inputs.Q0, inputs.Q1, inputs.Q2, inputs.Q3
			} else {
				plane[q0Index-4*stride], plane[q0Index-3*stride], plane[q0Index-2*stride], plane[q0Index-stride] = inputs.P3, inputs.P2, inputs.P1, inputs.P0
				plane[q0Index], plane[q0Index+stride], plane[q0Index+2*stride], plane[q0Index+3*stride] = inputs.Q0, inputs.Q1, inputs.Q2, inputs.Q3
			}
		}
		if err := ApplyLumaDeblockingPlaneMacroblockEdge(plane, width, height, stride, x, y, vertical, boundaryStrengths, parameters, false); err != nil {
			t.Fatalf("vertical=%t: %v", vertical, err)
		}
		for lane := 0; lane < 16; lane++ {
			q0Index := y*stride + x + lane
			if vertical {
				q0Index = (y+lane)*stride + x
			}
			expected := LumaEdgeSamples{P0: 100, P1: 98, P2: 96, Q0: 104, Q1: 105, Q2: 106}
			switch boundaryStrengths[lane/4] {
			case 4:
				expected = LumaEdgeSamples{P0: 101, P1: 100, P2: 98, Q0: 103, Q1: 104, Q2: 105}
			case 2:
				expected = LumaEdgeSamples{P0: 101, P1: 99, P2: 96, Q0: 103, Q1: 104, Q2: 106}
			}
			var got LumaEdgeSamples
			if vertical {
				got = LumaEdgeSamples{P0: plane[q0Index-1], P1: plane[q0Index-2], P2: plane[q0Index-3], Q0: plane[q0Index], Q1: plane[q0Index+1], Q2: plane[q0Index+2]}
			} else {
				got = LumaEdgeSamples{P0: plane[q0Index-stride], P1: plane[q0Index-2*stride], P2: plane[q0Index-3*stride], Q0: plane[q0Index], Q1: plane[q0Index+stride], Q2: plane[q0Index+2*stride]}
			}
			if got != expected {
				t.Errorf("vertical=%t lane=%d: got %+v; want %+v", vertical, lane, got, expected)
			}
		}
		before := append([]uint8(nil), plane...)
		invalidStrengths := [4]uint8{4, 0, 5, 0}
		if err := ApplyLumaDeblockingPlaneMacroblockEdge(plane, width, height, stride, x, y, vertical, invalidStrengths, parameters, false); !errors.Is(err, ErrLumaDeblockingBoundaryStrength) {
			t.Fatalf("invalid bS error = %v; want %v", err, ErrLumaDeblockingBoundaryStrength)
		}
		for index := range plane {
			if plane[index] != before[index] {
				t.Fatalf("invalid bS modified plane at index %d", index)
			}
		}
	}
}

func TestApplyLumaDeblockingMacroblockUsesNormativeEdgeOrder(t *testing.T) {
	const width, height, stride = 32, 32, 35
	parameters := LumaDeblockingParameters{Mode: LumaDeblockingAllEdges, IndexA: 40, IndexB: 40}
	makePlane := func() []uint8 {
		plane := make([]uint8, stride*height)
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				plane[y*stride+x] = uint8((x*7+y*11+x*y)%180 + 30)
			}
			for x := width; x < stride; x++ {
				plane[y*stride+x] = 251
			}
		}
		return plane
	}
	for _, transform8x8 := range []bool{false, true} {
		macroblock := LumaDeblockingMacroblock{
			X: 8, Y: 8, LeftNeighborAvailable: true, TopNeighborAvailable: true,
			TransformSize8x8: transform8x8, LeftSliceBoundary: false, TopSliceBoundary: false,
			LeftStrengths: [4]uint8{4, 3, 2, 1}, TopStrengths: [4]uint8{1, 2, 3, 4},
			VerticalInternalStrengths:   [3][4]uint8{{1, 2, 3, 4}, {4, 3, 2, 1}, {2, 1, 4, 3}},
			HorizontalInternalStrengths: [3][4]uint8{{4, 1, 3, 2}, {2, 4, 1, 3}, {3, 2, 4, 1}},
		}
		got := makePlane()
		want := append([]uint8(nil), got...)
		if err := ApplyLumaDeblockingPlaneMacroblockEdge(want, width, height, stride, macroblock.X, macroblock.Y, true, macroblock.LeftStrengths, parameters, false); err != nil {
			t.Fatal(err)
		}
		for edge := 0; edge < 3; edge++ {
			if transform8x8 && edge != 1 {
				continue
			}
			if err := ApplyLumaDeblockingPlaneMacroblockEdge(want, width, height, stride, macroblock.X+4*(edge+1), macroblock.Y, true, macroblock.VerticalInternalStrengths[edge], parameters, false); err != nil {
				t.Fatal(err)
			}
		}
		if err := ApplyLumaDeblockingPlaneMacroblockEdge(want, width, height, stride, macroblock.X, macroblock.Y, false, macroblock.TopStrengths, parameters, false); err != nil {
			t.Fatal(err)
		}
		for edge := 0; edge < 3; edge++ {
			if transform8x8 && edge != 1 {
				continue
			}
			if err := ApplyLumaDeblockingPlaneMacroblockEdge(want, width, height, stride, macroblock.X, macroblock.Y+4*(edge+1), false, macroblock.HorizontalInternalStrengths[edge], parameters, false); err != nil {
				t.Fatal(err)
			}
		}
		if err := ApplyLumaDeblockingMacroblock(got, width, height, stride, macroblock, parameters); err != nil {
			t.Fatalf("transform8x8=%t: %v", transform8x8, err)
		}
		for index := range got {
			if got[index] != want[index] {
				t.Fatalf("transform8x8=%t differs at plane offset %d", transform8x8, index)
			}
		}

		before := append([]uint8(nil), got...)
		invalid := macroblock
		invalid.HorizontalInternalStrengths[2][3] = 5
		if err := ApplyLumaDeblockingMacroblock(got, width, height, stride, invalid, parameters); !errors.Is(err, ErrLumaDeblockingBoundaryStrength) {
			t.Fatalf("invalid later edge strength error = %v; want %v", err, ErrLumaDeblockingBoundaryStrength)
		}
		for index := range got {
			if got[index] != before[index] {
				t.Fatalf("invalid macroblock configuration modified plane at offset %d", index)
			}
		}
	}
}

func TestDeriveLumaDeblockingEdgeFlagsFromModeAndNeighborAvailability(t *testing.T) {
	tests := []struct {
		name              string
		mode              LumaDeblockingMode
		leftAvailable     bool
		topAvailable      bool
		leftSliceBoundary bool
		topSliceBoundary  bool
		want              LumaDeblockingEdgeFlags
		wantErr           bool
	}{
		{name: "idc0 filters available slice neighbors", mode: LumaDeblockingAllEdges, leftAvailable: true, topAvailable: true, leftSliceBoundary: true, topSliceBoundary: true, want: LumaDeblockingEdgeFlags{FilterLeft: true, FilterTop: true, FilterInternal: true}},
		{name: "idc1 disables all edges", mode: LumaDeblockingDisabled, leftAvailable: true, topAvailable: true, want: LumaDeblockingEdgeFlags{}},
		{name: "idc2 filters same-slice neighbors", mode: LumaDeblockingAllExceptSliceBoundaries, leftAvailable: true, topAvailable: true, want: LumaDeblockingEdgeFlags{FilterLeft: true, FilterTop: true, FilterInternal: true}},
		{name: "idc2 suppresses cross-slice neighbors", mode: LumaDeblockingAllExceptSliceBoundaries, leftAvailable: true, topAvailable: true, leftSliceBoundary: true, want: LumaDeblockingEdgeFlags{FilterTop: true, FilterInternal: true}},
		{name: "picture edges unavailable", mode: LumaDeblockingAllEdges, want: LumaDeblockingEdgeFlags{FilterInternal: true}},
		{name: "invalid mode", mode: LumaDeblockingMode(3), wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := DeriveLumaDeblockingEdgeFlags(test.mode, test.leftAvailable, test.topAvailable, test.leftSliceBoundary, test.topSliceBoundary)
			if (err != nil) != test.wantErr || got != test.want {
				t.Fatalf("edge flags = %+v, %v; want %+v, error=%t", got, err, test.want, test.wantErr)
			}
		})
	}
}

func TestDeriveLumaDeblockingNeighborsFromRasterAddressAndSliceIDs(t *testing.T) {
	sliceIDs := []uint32{1, 1, 2, 3}
	tests := []struct {
		index int
		want  LumaDeblockingNeighbors
	}{
		{index: 0, want: LumaDeblockingNeighbors{LeftIndex: -1, TopIndex: -1}},
		{index: 1, want: LumaDeblockingNeighbors{LeftIndex: 0, TopIndex: -1, LeftAvailable: true}},
		{index: 2, want: LumaDeblockingNeighbors{LeftIndex: -1, TopIndex: 0, TopAvailable: true, TopSliceBoundary: true}},
		{index: 3, want: LumaDeblockingNeighbors{LeftIndex: 2, TopIndex: 1, LeftAvailable: true, TopAvailable: true, LeftSliceBoundary: true, TopSliceBoundary: true}},
	}
	for _, test := range tests {
		got, err := DeriveLumaDeblockingNeighbors(test.index, 2, 2, sliceIDs)
		if err != nil || got != test.want {
			t.Errorf("neighbors(%d) = %+v, %v; want %+v, nil", test.index, got, err, test.want)
		}
	}
	for _, test := range []struct {
		index, width, height int
		ids                  []uint32
	}{
		{index: -1, width: 2, height: 2, ids: sliceIDs},
		{index: 4, width: 2, height: 2, ids: sliceIDs},
		{index: 0, width: 2, height: 2, ids: sliceIDs[:3]},
		{index: 0, width: 0, height: 2, ids: sliceIDs},
	} {
		if _, err := DeriveLumaDeblockingNeighbors(test.index, test.width, test.height, test.ids); !errors.Is(err, ErrLumaDeblockingMacroblockAddress) {
			t.Errorf("invalid neighbors(%+v) error = %v; want %v", test, err, ErrLumaDeblockingMacroblockAddress)
		}
	}
}

func TestResolveLumaDeblockingMacroblockFromRasterAddress(t *testing.T) {
	template := LumaDeblockingMacroblock{
		TransformSize8x8: true,
		LeftStrengths:    [4]uint8{1, 2, 3, 4},
	}
	for _, test := range []struct {
		index int
		want  LumaDeblockingMacroblock
	}{
		{index: 0, want: LumaDeblockingMacroblock{X: 0, Y: 0, TransformSize8x8: true, LeftStrengths: [4]uint8{1, 2, 3, 4}}},
		{index: 3, want: LumaDeblockingMacroblock{X: 16, Y: 16, LeftNeighborAvailable: true, TopNeighborAvailable: true, LeftSliceBoundary: true, TopSliceBoundary: true, TransformSize8x8: true, LeftStrengths: [4]uint8{1, 2, 3, 4}}},
	} {
		got, err := ResolveLumaDeblockingMacroblock(test.index, 2, 2, []uint32{1, 1, 2, 3}, template)
		if err != nil || got != test.want {
			t.Errorf("resolved macroblock %d = %+v, %v; want %+v, nil", test.index, got, err, test.want)
		}
	}
	if _, err := ResolveLumaDeblockingMacroblock(4, 2, 2, []uint32{1, 1, 2, 3}, template); !errors.Is(err, ErrLumaDeblockingMacroblockAddress) {
		t.Fatalf("invalid raster address error = %v; want %v", err, ErrLumaDeblockingMacroblockAddress)
	}
}

func TestDeriveLumaBoundaryStrengthUsesNormativePriority(t *testing.T) {
	tests := []struct {
		name                   string
		macroblockEdge         bool
		eitherIntra            bool
		eitherHasCoefficients  bool
		interPredictionDiffers bool
		want                   uint8
	}{
		{name: "intra macroblock edge", macroblockEdge: true, eitherIntra: true, eitherHasCoefficients: true, interPredictionDiffers: true, want: 4},
		{name: "intra internal edge", eitherIntra: true, want: 3},
		{name: "coefficients outrank inter mismatch", eitherHasCoefficients: true, interPredictionDiffers: true, want: 2},
		{name: "different inter prediction", interPredictionDiffers: true, want: 1},
		{name: "matching inter prediction", want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := DeriveLumaBoundaryStrength(test.macroblockEdge, test.eitherIntra, test.eitherHasCoefficients, test.interPredictionDiffers)
			if got != test.want {
				t.Fatalf("boundary strength = %d; want %d", got, test.want)
			}
		})
	}
}

func TestLumaInterPredictionDiffersComparesReferencesAndMotionVectors(t *testing.T) {
	tests := []struct {
		name string
		p    []LumaPredictionVector
		q    []LumaPredictionVector
		want bool
	}{
		{name: "no motion vectors", want: false},
		{name: "matching single prediction", p: []LumaPredictionVector{{ReferencePictureID: 7, MotionVector: MotionVector{X: -3, Y: 3}}}, q: []LumaPredictionVector{{ReferencePictureID: 7, MotionVector: MotionVector{X: 0, Y: 0}}}},
		{name: "reference picture differs", p: []LumaPredictionVector{{ReferencePictureID: 7}}, q: []LumaPredictionVector{{ReferencePictureID: 8}}, want: true},
		{name: "horizontal difference reaches four", p: []LumaPredictionVector{{ReferencePictureID: 7}}, q: []LumaPredictionVector{{ReferencePictureID: 7, MotionVector: MotionVector{X: 4}}}, want: true},
		{name: "vertical difference reaches four", p: []LumaPredictionVector{{ReferencePictureID: 7}}, q: []LumaPredictionVector{{ReferencePictureID: 7, MotionVector: MotionVector{Y: -4}}}, want: true},
		{name: "biprediction list order differs", p: []LumaPredictionVector{{ReferencePictureID: 7, MotionVector: MotionVector{X: 12}}, {ReferencePictureID: 8, MotionVector: MotionVector{Y: 20}}}, q: []LumaPredictionVector{{ReferencePictureID: 8, MotionVector: MotionVector{Y: 22}}, {ReferencePictureID: 7, MotionVector: MotionVector{X: 10}}}},
		{name: "biprediction motion differs for matched reference", p: []LumaPredictionVector{{ReferencePictureID: 7}, {ReferencePictureID: 8}}, q: []LumaPredictionVector{{ReferencePictureID: 8}, {ReferencePictureID: 7, MotionVector: MotionVector{Y: 4}}}, want: true},
		{name: "prediction vector count differs", p: []LumaPredictionVector{{ReferencePictureID: 7}}, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := LumaInterPredictionDiffers(test.p, test.q)
			if err != nil || got != test.want {
				t.Fatalf("prediction difference = %t, %v; want %t, nil", got, err, test.want)
			}
		})
	}
	tooMany := []LumaPredictionVector{{}, {}, {}}
	if _, err := LumaInterPredictionDiffers(tooMany, nil); !errors.Is(err, ErrLumaInterPrediction) {
		t.Fatalf("more than two prediction vectors error = %v; want %v", err, ErrLumaInterPrediction)
	}
}

func TestDeriveLumaBoundaryStrengthFromPredictions(t *testing.T) {
	prediction := func(referenceID uint32, x, y int32) []LumaPredictionVector {
		return []LumaPredictionVector{{ReferencePictureID: referenceID, MotionVector: MotionVector{X: x, Y: y}}}
	}
	tests := []struct {
		name    string
		edge    bool
		intra   bool
		coeff   bool
		p       []LumaPredictionVector
		q       []LumaPredictionVector
		want    uint8
		wantErr bool
	}{
		{name: "intra takes priority", edge: true, intra: true, p: prediction(1, 0, 0), q: prediction(2, 4, 0), want: 4},
		{name: "coefficients take priority", coeff: true, p: prediction(1, 0, 0), q: prediction(2, 4, 0), want: 2},
		{name: "prediction mismatch derives one", p: prediction(1, 0, 0), q: prediction(2, 0, 0), want: 1},
		{name: "matching prediction derives zero", p: prediction(1, 0, 0), q: prediction(1, 3, -3), want: 0},
		{name: "unsupported prediction count", p: []LumaPredictionVector{{}, {}, {}}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := DeriveLumaBoundaryStrengthFromPredictions(test.edge, test.intra, test.coeff, test.p, test.q)
			if (err != nil) != test.wantErr || got != test.want {
				t.Fatalf("boundary strength = %d, %v; want %d, error=%t", got, err, test.want, test.wantErr)
			}
		})
	}
}

func TestPOCType0NonReferenceDoesNotAdvanceState(t *testing.T) {
	state := POCType0State{PreviousPicOrderCntMSB: 16, PreviousPicOrderCntLSB: 14}
	got, err := state.Calculate(16, 1, 0, 0, false)
	if err != nil || got != 33 {
		t.Fatalf("non-reference POC = %d, %v; want 33, nil", got, err)
	}
	if state != (POCType0State{PreviousPicOrderCntMSB: 16, PreviousPicOrderCntLSB: 14}) {
		t.Fatalf("non-reference picture advanced state: %+v", state)
	}
}

func TestPOCType0RejectsInvalidInputsWithoutChangingState(t *testing.T) {
	tests := []struct {
		max, lsb uint32
		nalRef   uint8
		idr      bool
	}{
		{max: 15, lsb: 0, nalRef: 1},
		{max: 24, lsb: 0, nalRef: 1},
		{max: 16, lsb: 16, nalRef: 1},
		{max: 16, lsb: 0, nalRef: 4},
		{max: 16, lsb: 0, nalRef: 0, idr: true},
	}
	for _, test := range tests {
		state := POCType0State{PreviousPicOrderCntMSB: 7, PreviousPicOrderCntLSB: 2}
		before := state
		if _, err := state.Calculate(test.max, test.lsb, 0, test.nalRef, test.idr); !errors.Is(err, ErrPictureOrderCount) {
			t.Errorf("Calculate(%d, %d, %d, %v) error = %v; want %v", test.max, test.lsb, test.nalRef, test.idr, err, ErrPictureOrderCount)
		}
		if state != before {
			t.Errorf("invalid input changed state: %+v; want %+v", state, before)
		}
	}
	state := POCType0State{PreviousPicOrderCntMSB: int64(^uint64(0) >> 1), PreviousPicOrderCntLSB: 15}
	before := state
	if _, err := state.Calculate(16, 0, 0, 1, false); !errors.Is(err, ErrPictureOrderCount) || state != before {
		t.Fatalf("overflow error/state = %v/%+v; want %v and unchanged state", err, state, ErrPictureOrderCount)
	}
}

func TestPOCType1UsesFrameNumberCyclesAndReferenceState(t *testing.T) {
	state := POCType12State{}
	if got, err := state.CalculateType1(16, 0, 0, 0, -1, 1, []int64{2, 3}, 1, true); err != nil || got != 0 {
		t.Fatalf("IDR type-1 POC = %d, %v; want 0, nil", got, err)
	}
	if got, err := state.CalculateType1(16, 1, 0, 0, -1, 1, []int64{2, 3}, 1, false); err != nil || got != 2 {
		t.Fatalf("type-1 POC at cycle start = %d, %v; want 2, nil", got, err)
	}
	before := state
	if got, err := state.CalculateType1(16, 2, 0, 0, -1, 1, []int64{2, 3}, 0, false); err != nil || got != 1 {
		t.Fatalf("non-reference type-1 POC = %d, %v; want 1, nil", got, err)
	}
	if state != before {
		t.Fatalf("non-reference type-1 picture advanced state: %+v; want %+v", state, before)
	}
	if got, err := state.CalculateType1(16, 0, 0, 0, -1, 1, []int64{2, 3}, 1, false); err != nil || got != 40 {
		t.Fatalf("type-1 POC across frame-number wrap = %d, %v; want 40, nil", got, err)
	}
}

func TestPOCType2UsesFrameNumberOffsetAndRejectsOverflowTransactionally(t *testing.T) {
	state := POCType12State{}
	if got, err := state.CalculateType2(16, 0, 1, true); err != nil || got != 0 {
		t.Fatalf("IDR type-2 POC = %d, %v; want 0, nil", got, err)
	}
	if got, err := state.CalculateType2(16, 1, 1, false); err != nil || got != 2 {
		t.Fatalf("type-2 reference POC = %d, %v; want 2, nil", got, err)
	}
	before := state
	if got, err := state.CalculateType2(16, 2, 0, false); err != nil || got != 3 {
		t.Fatalf("type-2 non-reference POC = %d, %v; want 3, nil", got, err)
	}
	if state != before {
		t.Fatalf("non-reference type-2 picture advanced state: %+v; want %+v", state, before)
	}
	wrapState := POCType12State{PreviousFrameNum: 15}
	if got, err := wrapState.CalculateType2(16, 0, 1, false); err != nil || got != 32 {
		t.Fatalf("type-2 POC across frame-number wrap = %d, %v; want 32, nil", got, err)
	}
	overflowState := POCType12State{PreviousFrameNum: 15, PreviousFrameNumOffset: int64(^uint64(0) >> 1)}
	before = overflowState
	if _, err := overflowState.CalculateType2(16, 0, 1, false); !errors.Is(err, ErrPictureOrderCount) || overflowState != before {
		t.Fatalf("type-2 overflow error/state = %v/%+v; want error and unchanged state", err, overflowState)
	}
}

func TestPOCType1RejectsAbsoluteFrameNumberOverflowTransactionally(t *testing.T) {
	state := POCType12State{PreviousFrameNumOffset: int64(^uint64(0) >> 1)}
	before := state
	if _, err := state.CalculateType1(16, 1, 0, 0, 0, 0, []int64{1}, 1, false); !errors.Is(err, ErrPictureOrderCount) || state != before {
		t.Fatalf("type-1 overflow error/state = %v/%+v; want error and unchanged state", err, state)
	}
}

func TestPresentationOrderBufferHoldsAndReleasesPicturesByPOC(t *testing.T) {
	buffer := NewPresentationOrderBuffer(2)
	frame := Yuv420Frame{Width: 2, Height: 2, YStride: 2, UStride: 1, VStride: 1, Y: []uint8{1, 2, 3, 4}, U: []uint8{5}, V: []uint8{6}}
	wantReleased := []*int64{nil, nil, int64Pointer(0), int64Pointer(2)}
	for index, poc := range []int64{0, 6, 2, 4} {
		ready, err := buffer.Push(PresentationPicture{PictureOrderCnt: poc, Frame: frame})
		if err != nil {
			t.Fatal(err)
		}
		if wantReleased[index] == nil {
			if ready != nil {
				t.Fatalf("push POC %d released %d early", poc, ready.PictureOrderCnt)
			}
		} else if ready == nil || ready.PictureOrderCnt != *wantReleased[index] {
			t.Fatalf("push POC %d released %v; want POC %d", poc, ready, *wantReleased[index])
		}
	}
	drained := buffer.Drain()
	if len(drained) != 2 || drained[0].PictureOrderCnt != 4 || drained[1].PictureOrderCnt != 6 {
		t.Fatalf("drained pictures = %+v; want POCs [4 6]", drained)
	}
	if len(buffer.Drain()) != 0 {
		t.Fatal("Drain did not empty the presentation buffer")
	}
}

func TestPresentationOrderBufferIsStableForEqualPOCAndOwnsFrames(t *testing.T) {
	buffer := NewPresentationOrderBuffer(1)
	frame := Yuv420Frame{Width: 2, Height: 2, YStride: 2, UStride: 1, VStride: 1, Y: []uint8{1, 2, 3, 4}, U: []uint8{5}, V: []uint8{6}}
	first, err := buffer.Push(PresentationPicture{PictureOrderCnt: 3, Frame: frame})
	if err != nil || first != nil {
		t.Fatalf("first push = %v, %v; want picture held", first, err)
	}
	frame.Y[0] = 99
	second, err := buffer.Push(PresentationPicture{PictureOrderCnt: 3, Frame: frame})
	if err != nil || second == nil || second.Frame.Y[0] != 1 {
		t.Fatalf("second push = %v, %v; want stable first frame", second, err)
	}
	drained := buffer.Drain()
	if len(drained) != 1 || drained[0].Frame.Y[0] != 99 {
		t.Fatalf("drained equal-POC frame = %+v; want second inserted frame", drained)
	}
	buffer = NewPresentationOrderBuffer(0)
	if _, err := buffer.Push(PresentationPicture{Frame: Yuv420Frame{Width: 1}}); !errors.Is(err, ErrReferencePictureFrameLayout) {
		t.Fatalf("invalid frame push error = %v; want %v", err, ErrReferencePictureFrameLayout)
	}
}

func int64Pointer(value int64) *int64 { return &value }

func TestReferencePictureBufferOwnsFramesAndPreservesInsertionOrder(t *testing.T) {
	buffer := &ReferencePictureBuffer{}
	frame := Yuv420Frame{
		Width: 3, Height: 3, YStride: 4, UStride: 3, VStride: 3,
		Y: []uint8{1, 2, 3, 99, 4, 5, 6, 99, 7, 8, 9},
		U: []uint8{10, 11, 99, 12, 13},
		V: []uint8{20, 21, 99, 22, 23},
	}
	first := ReferencePicture{ID: 1, FrameNum: 4}
	second := ReferencePicture{ID: 2, FrameNum: 5}
	if err := buffer.Store(first, frame); err != nil {
		t.Fatal(err)
	}
	if err := buffer.Store(second, frame); err != nil {
		t.Fatal(err)
	}
	frame.Y[0] = 50

	stored, ok := buffer.Get(first.ID)
	if !ok || stored.Frame.Y[0] != 1 {
		t.Fatalf("stored frame = %#v, found %v; want owned original frame", stored, ok)
	}
	stored.Frame.Y[0] = 60
	storedAgain, _ := buffer.Get(first.ID)
	if storedAgain.Frame.Y[0] != 1 {
		t.Fatalf("mutating returned frame changed stored frame: %d", storedAgain.Frame.Y[0])
	}
	if got, want := buffer.References(), []ReferencePicture{first, second}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("references = %v; want %v", got, want)
	}
	if !buffer.Remove(first.ID) || buffer.Remove(first.ID) {
		t.Fatal("Remove did not remove exactly one stored picture")
	}
}

func TestReferencePictureBufferReplacesAndRejectsInvalidFrames(t *testing.T) {
	buffer := &ReferencePictureBuffer{}
	reference := ReferencePicture{ID: 1, FrameNum: 1}
	frame := Yuv420Frame{Width: 2, Height: 2, YStride: 2, UStride: 1, VStride: 1, Y: []uint8{1, 2, 3, 4}, U: []uint8{5}, V: []uint8{6}}
	if err := buffer.Store(reference, frame); err != nil {
		t.Fatal(err)
	}
	reference.FrameNum = 2
	frame.Y = []uint8{7, 8, 9, 10}
	if err := buffer.Store(reference, frame); err != nil {
		t.Fatal(err)
	}
	stored, ok := buffer.Get(reference.ID)
	if !ok || stored.Reference.FrameNum != 2 || stored.Frame.Y[0] != 7 || len(buffer.References()) != 1 {
		t.Fatalf("replacement = %#v; want one updated picture", stored)
	}
	frame.Y = []uint8{1}
	if err := buffer.Store(reference, frame); !errors.Is(err, ErrReferencePictureFrameLayout) {
		t.Fatalf("Store error = %v; want %v", err, ErrReferencePictureFrameLayout)
	}
}

func TestMotionVectorDifferenceNeighborMagnitudes(t *testing.T) {
	tests := []struct {
		name string
		left *MotionVector
		top  *MotionVector
		want [2]uint64
	}{
		{name: "both unavailable", want: [2]uint64{}},
		{name: "left only", left: &MotionVector{X: -2, Y: 3}, want: [2]uint64{2, 3}},
		{name: "sum absolute components", left: &MotionVector{X: -2, Y: 3}, top: &MotionVector{X: 4, Y: -5}, want: [2]uint64{6, 8}},
		{name: "minimum signed components", left: &MotionVector{X: -1 << 31, Y: -1 << 31}, top: &MotionVector{X: -1 << 31, Y: -1 << 31}, want: [2]uint64{1 << 32, 1 << 32}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			xMagnitude, yMagnitude := MotionVectorDifferenceNeighborMagnitudes(test.left, test.top)
			if got := [2]uint64{xMagnitude, yMagnitude}; got != test.want {
				t.Fatalf("neighbor magnitudes = %v; want %v", got, test.want)
			}
		})
	}
}

func TestInterpolateLumaHalfSampleHorizontalRoundsAndClips(t *testing.T) {
	tests := []struct {
		name    string
		samples [6]uint8
		want    uint8
	}{
		{name: "filtered sample", samples: [6]uint8{10, 40, 80, 120, 200, 240}, want: 95},
		{name: "clip below zero", samples: [6]uint8{0, 255, 0, 0, 0, 0}, want: 0},
		{name: "clip above maximum", samples: [6]uint8{255, 255, 255, 255, 0, 0}, want: 255},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := interpolateLumaHalfSampleHorizontal(test.samples); got != test.want {
				t.Fatalf("interpolated half sample = %d; want %d", got, test.want)
			}
		})
	}
}

func TestInterpolateLumaHalfSampleVerticalRoundsAndClips(t *testing.T) {
	tests := []struct {
		name    string
		samples [6]uint8
		want    uint8
	}{
		{name: "filtered sample", samples: [6]uint8{10, 40, 80, 120, 200, 240}, want: 95},
		{name: "clip below zero", samples: [6]uint8{0, 255, 0, 0, 0, 0}, want: 0},
		{name: "clip above maximum", samples: [6]uint8{255, 255, 255, 255, 0, 0}, want: 255},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := interpolateLumaHalfSampleVertical(test.samples); got != test.want {
				t.Fatalf("interpolated vertical half sample = %d; want %d", got, test.want)
			}
		})
	}
}

func TestInterpolateLumaHalfSampleDiagonalRoundsAndClips(t *testing.T) {
	var centeredImpulse [6][6]uint8
	centeredImpulse[2][2] = 255
	var negativeImpulse [6][6]uint8
	negativeImpulse[2][1] = 255
	var highOvershoot [6][6]uint8
	for _, row := range []int{0, 2, 3, 5} {
		for column := range highOvershoot[row] {
			highOvershoot[row][column] = 255
		}
	}
	tests := []struct {
		name    string
		samples [6][6]uint8
		want    uint8
	}{
		{name: "unrounded two-pass impulse", samples: centeredImpulse, want: 100},
		{name: "clip negative ringing", samples: negativeImpulse, want: 0},
		{name: "clip positive overshoot", samples: highOvershoot, want: 255},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := interpolateLumaHalfSampleDiagonal(test.samples); got != test.want {
				t.Fatalf("interpolated diagonal half sample = %d; want %d", got, test.want)
			}
		})
	}
}

func TestInterpolateLumaQuarterSampleAverageRoundsUp(t *testing.T) {
	tests := []struct {
		first  uint8
		second uint8
		want   uint8
	}{
		{first: 10, second: 20, want: 15},
		{first: 10, second: 21, want: 16},
		{first: 0, second: 255, want: 128},
		{first: 255, second: 255, want: 255},
	}
	for _, test := range tests {
		if got := interpolateLumaQuarterSampleAverage(test.first, test.second); got != test.want {
			t.Errorf("quarter-sample average of %d and %d = %d; want %d", test.first, test.second, got, test.want)
		}
	}
}

func TestInterpolateLumaQuarterSamplePairUsesAdjacentIntegerSamples(t *testing.T) {
	tests := []struct {
		first  uint8
		half   uint8
		second uint8
		want   [2]uint8
	}{
		{first: 10, half: 21, second: 30, want: [2]uint8{16, 26}},
		{first: 0, half: 255, second: 0, want: [2]uint8{128, 128}},
	}
	for _, test := range tests {
		if got := interpolateLumaQuarterSamplePair(test.first, test.half, test.second); got != test.want {
			t.Errorf("quarter-sample pair = %v; want %v", got, test.want)
		}
	}
}

func TestInterpolateLumaQuarterSampleDiagonalUsesNeighboringHalfSamples(t *testing.T) {
	if got, want := interpolateLumaQuarterSampleDiagonal(10, 21, 30, 41), [4]uint8{16, 20, 31, 36}; got != want {
		t.Fatalf("diagonal quarter samples [e, g, p, r] = %v; want %v", got, want)
	}
}

func TestInterpolateLumaQuarterSampleAxialUsesIntegerAndHalfSamples(t *testing.T) {
	if got, want := interpolateLumaQuarterSampleAxial(10, 30, 40, 21, 25), [4]uint8{16, 26, 18, 33}; got != want {
		t.Fatalf("axial quarter samples [a, c, d, n] = %v; want %v", got, want)
	}
}

func TestInterpolateLumaQuarterSampleAroundJUsesNeighboringHalfSamples(t *testing.T) {
	if got, want := interpolateLumaQuarterSampleAroundJ(10, 21, 30, 40, 50), [4]uint8{20, 26, 35, 40}; got != want {
		t.Fatalf("quarter samples [f, i, k, q] around j = %v; want %v", got, want)
	}
}

func TestSelectLumaFractionalSampleUsesXThenYOffsets(t *testing.T) {
	samples := [4][4]uint8{
		{10, 11, 12, 13},
		{20, 21, 22, 23},
		{30, 31, 32, 33},
		{40, 41, 42, 43},
	}
	for xFracL := range samples {
		for yFracL, want := range samples[xFracL] {
			got, err := selectLumaFractionalSample(samples, uint8(xFracL), uint8(yFracL))
			if err != nil {
				t.Fatalf("selectLumaFractionalSample(%d, %d) error = %v", xFracL, yFracL, err)
			}
			if got != want {
				t.Errorf("selectLumaFractionalSample(%d, %d) = %d; want %d", xFracL, yFracL, got, want)
			}
		}
	}
}

func TestSelectLumaFractionalSampleRejectsOutOfRangeOffsets(t *testing.T) {
	for _, offsets := range [][2]uint8{{4, 0}, {0, 4}} {
		if _, err := selectLumaFractionalSample([4][4]uint8{}, offsets[0], offsets[1]); !errors.Is(err, ErrLumaFractionalSampleOffset) {
			t.Errorf("selectLumaFractionalSample(%d, %d) error = %v; want %v", offsets[0], offsets[1], err, ErrLumaFractionalSampleOffset)
		}
	}
}

func TestInterpolateLumaQuarterSampleGridBuildsTable8x12Positions(t *testing.T) {
	var neighborhood [6][6]uint8
	for row := range neighborhood {
		for column := range neighborhood[row] {
			neighborhood[row][column] = uint8(10*row + 5*column)
		}
	}
	want := [4][4]uint8{
		{30, 33, 35, 38},
		{32, 34, 37, 39},
		{33, 36, 38, 41},
		{34, 37, 39, 42},
	}
	if got := interpolateLumaQuarterSampleGrid(neighborhood); got != want {
		t.Fatalf("quarter-sample grid [xFracL][yFracL] = %v; want %v", got, want)
	}
}

func TestGatherLumaQuarterSampleNeighborhoodUsesStride(t *testing.T) {
	const width, height, stride = 8, 8, 10
	plane := make([]uint8, stride*height)
	for row := 0; row < height; row++ {
		for column := 0; column < width; column++ {
			plane[row*stride+column] = uint8(row*width + column)
		}
		plane[row*stride+width] = 250
		plane[row*stride+width+1] = 251
	}
	samples, err := gatherLumaQuarterSampleNeighborhood(plane, width, height, stride, 3, 3)
	if err != nil {
		t.Fatal(err)
	}
	for row := 0; row < 6; row++ {
		for column := 0; column < 6; column++ {
			want := uint8((row+1)*width + column + 1)
			if samples[row][column] != want {
				t.Errorf("neighborhood[%d][%d] = %d; want %d", row, column, samples[row][column], want)
			}
		}
	}
}

func TestGatherLumaQuarterSampleNeighborhoodClipsEdgesAndIgnoresPadding(t *testing.T) {
	plane := []uint8{1, 2, 99, 3, 4, 99}
	samples, err := gatherLumaQuarterSampleNeighborhood(plane, 2, 2, 3, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for row := 0; row < 6; row++ {
		want := [6]uint8{1, 1, 1, 2, 2, 2}
		if row >= 3 {
			want = [6]uint8{3, 3, 3, 4, 4, 4}
		}
		if samples[row] != want {
			t.Errorf("edge neighborhood row %d = %v; want %v", row, samples[row], want)
		}
	}
}

func TestGatherLumaQuarterSampleNeighborhoodRejectsInvalidPlaneLayouts(t *testing.T) {
	tests := []struct {
		name          string
		plane         []uint8
		width, height int
		stride        int
	}{
		{name: "zero width", plane: []uint8{1}, width: 0, height: 1, stride: 1},
		{name: "stride smaller than width", plane: []uint8{1}, width: 2, height: 1, stride: 1},
		{name: "truncated plane", plane: []uint8{1, 2, 3}, width: 2, height: 2, stride: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := gatherLumaQuarterSampleNeighborhood(test.plane, test.width, test.height, test.stride, 0, 0); !errors.Is(err, ErrLumaReferencePlaneLayout) {
				t.Fatalf("gather error = %v; want %v", err, ErrLumaReferencePlaneLayout)
			}
		})
	}
}

func TestInterpolateLumaFractionalSampleFromReferencePlane(t *testing.T) {
	const width, height, stride = 6, 6, 6
	plane := make([]uint8, stride*height)
	for row := 0; row < height; row++ {
		for column := 0; column < width; column++ {
			plane[row*stride+column] = uint8(10*row + 5*column)
		}
	}
	want := [4][4]uint8{
		{30, 33, 35, 38},
		{32, 34, 37, 39},
		{33, 36, 38, 41},
		{34, 37, 39, 42},
	}
	for xFracL := 0; xFracL < 4; xFracL++ {
		for yFracL := 0; yFracL < 4; yFracL++ {
			got, err := interpolateLumaFractionalSample(plane, width, height, stride, 2, 2, uint8(xFracL), uint8(yFracL))
			if err != nil {
				t.Fatalf("interpolateLumaFractionalSample(%d, %d) error = %v", xFracL, yFracL, err)
			}
			if got != want[xFracL][yFracL] {
				t.Errorf("interpolateLumaFractionalSample(%d, %d) = %d; want %d", xFracL, yFracL, got, want[xFracL][yFracL])
			}
		}
	}
}

func TestBuildPReferenceListOrdersShortAndLongTermFrames(t *testing.T) {
	pictures := []ReferencePicture{
		{ID: 4, FrameNum: 5},
		{ID: 6, IsLongTerm: true, LongTermFrameIdx: 1},
		{ID: 2, FrameNum: 1},
		{ID: 5, IsLongTerm: true, LongTermFrameIdx: 4},
		{ID: 3, FrameNum: 15},
		{ID: 1, FrameNum: 3},
	}
	ordered, err := BuildPReferenceList(pictures, 2, 16)
	if err != nil {
		t.Fatal(err)
	}
	for index, wantID := range []uint32{2, 3, 4, 1, 6, 5} {
		if ordered[index].ID != wantID {
			t.Errorf("reference list[%d].ID = %d; want %d", index, ordered[index].ID, wantID)
		}
	}
	for _, test := range []struct {
		current uint32
		maximum uint32
		refs    []ReferencePicture
	}{
		{current: 16, maximum: 16},
		{current: 0, maximum: 0},
		{current: 0, maximum: 16, refs: []ReferencePicture{{FrameNum: 16}}},
	} {
		if _, err := BuildPReferenceList(test.refs, test.current, test.maximum); !errors.Is(err, ErrReferenceFrameNum) {
			t.Errorf("invalid frame-number input error = %v; want %v", err, ErrReferenceFrameNum)
		}
	}
}

func TestBuildBReferenceListsOrdersByPictureOrderCount(t *testing.T) {
	pictures := []ReferencePicture{
		{ID: 1, PictureOrderCnt: 6},
		{ID: 2, PictureOrderCnt: 14},
		{ID: 3, PictureOrderCnt: 2},
		{ID: 4, PictureOrderCnt: 20},
		{ID: 5, PictureOrderCnt: 10},
		{ID: 6, IsLongTerm: true, LongTermFrameIdx: 1},
		{ID: 7, IsLongTerm: true, LongTermFrameIdx: 4},
	}
	lists := BuildBReferenceLists(pictures, 10)
	for index, wantID := range []uint32{5, 1, 3, 2, 4, 6, 7} {
		if lists.List0[index].ID != wantID {
			t.Errorf("list0[%d].ID = %d; want %d", index, lists.List0[index].ID, wantID)
		}
	}
	for index, wantID := range []uint32{2, 4, 5, 1, 3, 6, 7} {
		if lists.List1[index].ID != wantID {
			t.Errorf("list1[%d].ID = %d; want %d", index, lists.List1[index].ID, wantID)
		}
	}
}

func TestBuildBReferenceListsSwapsIdenticalLists(t *testing.T) {
	pictures := []ReferencePicture{{ID: 1, PictureOrderCnt: 8}, {ID: 2, PictureOrderCnt: 5}}
	lists := BuildBReferenceLists(pictures, 10)
	if lists.List0[0].ID != 1 || lists.List0[1].ID != 2 || lists.List1[0].ID != 2 || lists.List1[1].ID != 1 {
		t.Fatalf("identical B lists were not disambiguated: list0=%v list1=%v", lists.List0, lists.List1)
	}
}

func TestApplyReferenceListModificationsResolvesAndInsertsTargets(t *testing.T) {
	references := []ReferencePicture{
		{ID: 1, FrameNum: 1},
		{ID: 2, FrameNum: 3},
		{ID: 3, FrameNum: 5},
		{ID: 4, FrameNum: 0},
		{ID: 5, IsLongTerm: true, LongTermFrameIdx: 4},
		{ID: 6, IsLongTerm: true, LongTermFrameIdx: 1},
	}
	initial, err := BuildPReferenceList(references, 2, 16)
	if err != nil {
		t.Fatal(err)
	}
	modifications := []RefPicListModification{
		{ModificationOfPicNumsIDC: 0, Value: 1},
		{ModificationOfPicNumsIDC: 1, Value: 0},
		{ModificationOfPicNumsIDC: 2, Value: 1},
	}
	ordered, err := ApplyReferenceListModifications(initial, references, 2, 16, modifications)
	if err != nil {
		t.Fatal(err)
	}
	for index, wantID := range []uint32{4, 1, 6, 3, 2, 5} {
		if ordered[index].ID != wantID {
			t.Errorf("modified reference list[%d].ID = %d; want %d", index, ordered[index].ID, wantID)
		}
	}
}

func TestApplyReferenceListModificationsRejectsInvalidTargets(t *testing.T) {
	short := ReferencePicture{ID: 1, FrameNum: 1}
	long := ReferencePicture{ID: 2, IsLongTerm: true, LongTermFrameIdx: 1}
	for _, test := range []struct {
		name string
		mod  RefPicListModification
	}{
		{name: "oversized picture-number difference", mod: RefPicListModification{ModificationOfPicNumsIDC: 0, Value: 16}},
		{name: "missing long-term index", mod: RefPicListModification{ModificationOfPicNumsIDC: 2, Value: 2}},
		{name: "invalid idc", mod: RefPicListModification{ModificationOfPicNumsIDC: 3}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ApplyReferenceListModifications([]ReferencePicture{short, long}, []ReferencePicture{short, long}, 2, 16, []RefPicListModification{test.mod}); !errors.Is(err, ErrReferenceListModification) {
				t.Fatalf("ApplyReferenceListModifications() error = %v; want %v", err, ErrReferenceListModification)
			}
		})
	}
}

func TestApplyMotionVectorDifferenceWrapsSignedComponents(t *testing.T) {
	tests := []struct {
		predicted  MotionVector
		difference MotionVector
		want       MotionVector
	}{
		{predicted: MotionVector{X: 120, Y: -240}, difference: MotionVector{X: 8, Y: 15}, want: MotionVector{X: 128, Y: -225}},
		{predicted: MotionVector{X: 32760, Y: -32760}, difference: MotionVector{X: 20, Y: -20}, want: MotionVector{X: -32756, Y: 32756}},
		{predicted: MotionVector{X: -32760, Y: 32760}, difference: MotionVector{X: -20, Y: 20}, want: MotionVector{X: 32756, Y: -32756}},
	}
	for _, test := range tests {
		if got := ApplyMotionVectorDifference(test.predicted, test.difference); got != test.want {
			t.Errorf("ApplyMotionVectorDifference(%+v, %+v) = %+v; want %+v", test.predicted, test.difference, got, test.want)
		}
	}
}

func TestPredictMotionVectorReferenceSelectionAndMedian(t *testing.T) {
	tests := []struct {
		name       string
		currentRef int32
		left       MotionVectorCandidate
		top        MotionVectorCandidate
		topRight   MotionVectorCandidate
		topLeft    MotionVectorCandidate
		want       MotionVector
	}{
		{
			name:       "single matching reference",
			currentRef: 2,
			top:        MotionVectorCandidate{Available: true, RefIdx: 2, Vector: MotionVector{X: 7, Y: -5}},
			left:       MotionVectorCandidate{Available: true, RefIdx: 1, Vector: MotionVector{X: 20}},
			topRight:   MotionVectorCandidate{Available: true, RefIdx: 0, Vector: MotionVector{Y: 30}},
			want:       MotionVector{X: 7, Y: -5},
		},
		{
			name:       "multiple matching references use component median",
			currentRef: 0,
			left:       MotionVectorCandidate{Available: true, RefIdx: 1, Vector: MotionVector{X: 20}},
			top:        MotionVectorCandidate{Available: true, RefIdx: 0, Vector: MotionVector{X: 8, Y: 10}},
			topRight:   MotionVectorCandidate{Available: true, RefIdx: 0, Vector: MotionVector{X: 2, Y: 4}},
			want:       MotionVector{X: 8, Y: 4},
		},
		{
			name:       "no matching references use component median",
			currentRef: 0,
			left:       MotionVectorCandidate{Available: true, RefIdx: 1, Vector: MotionVector{X: 3, Y: 9}},
			top:        MotionVectorCandidate{Available: true, RefIdx: 2, Vector: MotionVector{X: 8, Y: 2}},
			topRight:   MotionVectorCandidate{Available: true, RefIdx: 3, Vector: MotionVector{X: 5, Y: 6}},
			want:       MotionVector{X: 5, Y: 6},
		},
		{
			name:       "unavailable top right falls back to top left",
			currentRef: 3,
			left:       MotionVectorCandidate{Available: true, RefIdx: 1, Vector: MotionVector{X: 20}},
			top:        MotionVectorCandidate{Available: true, RefIdx: 2, Vector: MotionVector{X: 30}},
			topLeft:    MotionVectorCandidate{Available: true, RefIdx: 3, Vector: MotionVector{X: 11, Y: -7}},
			want:       MotionVector{X: 11, Y: -7},
		},
		{
			name:       "unavailable and intra candidates contribute zero",
			currentRef: 0,
			top:        MotionVectorCandidate{Available: true, RefIdx: -1, Vector: MotionVector{X: 99, Y: 99}},
			topRight:   MotionVectorCandidate{Available: true, RefIdx: 1, Vector: MotionVector{X: 9, Y: 6}},
			want:       MotionVector{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := PredictMotionVector(test.currentRef, test.left, test.top, test.topRight, test.topLeft)
			if got != test.want {
				t.Fatalf("PredictMotionVector() = %+v; want %+v", got, test.want)
			}
		})
	}
}

func TestPredictMotionVectorForPartitionUsesShapeSpecificMatches(t *testing.T) {
	candidate := func(ref int32, x, y int32) MotionVectorCandidate {
		return MotionVectorCandidate{Available: true, RefIdx: ref, Vector: MotionVector{X: x, Y: y}}
	}
	tests := []struct {
		name     string
		shape    MotionVectorPartitionShape
		left     MotionVectorCandidate
		top      MotionVectorCandidate
		topRight MotionVectorCandidate
		topLeft  MotionVectorCandidate
		want     MotionVector
	}{
		{name: "16x8 prefers left", shape: MotionVectorPartition16x8, left: candidate(0, 10, 1), top: candidate(0, 20, 2), topRight: candidate(1, 30, 3), want: MotionVector{X: 10, Y: 1}},
		{name: "16x8 selects top when left differs", shape: MotionVectorPartition16x8, left: candidate(1, 10, 1), top: candidate(0, 20, 2), topRight: candidate(1, 30, 3), want: MotionVector{X: 20, Y: 2}},
		{name: "8x16 prefers left", shape: MotionVectorPartition8x16, left: candidate(0, 10, 1), top: candidate(1, 20, 2), topRight: candidate(0, 30, 3), want: MotionVector{X: 10, Y: 1}},
		{name: "8x16 selects top right when left differs", shape: MotionVectorPartition8x16, left: candidate(1, 10, 1), top: candidate(1, 20, 2), topRight: candidate(0, 30, 3), want: MotionVector{X: 30, Y: 3}},
		{name: "8x16 uses top-left fallback", shape: MotionVectorPartition8x16, left: candidate(1, 10, 1), top: candidate(1, 20, 2), topLeft: candidate(0, 40, 4), want: MotionVector{X: 40, Y: 4}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := PredictMotionVectorForPartition(test.shape, 0, test.left, test.top, test.topRight, test.topLeft)
			if got != test.want {
				t.Fatalf("PredictMotionVectorForPartition() = %+v; want %+v", got, test.want)
			}
		})
	}
}

func TestDeriveMotionVectorCombinesPartitionPredictionAndDifference(t *testing.T) {
	left := MotionVectorCandidate{Available: true, RefIdx: 1, Vector: MotionVector{X: 12, Y: 20}}
	topLeft := MotionVectorCandidate{Available: true, RefIdx: 0, Vector: MotionVector{X: 32760, Y: -32760}}
	got := DeriveMotionVector(
		MotionVectorPartition8x16,
		0,
		left,
		MotionVectorCandidate{},
		MotionVectorCandidate{},
		topLeft,
		MotionVector{X: 20, Y: -20},
	)
	want := MotionVector{X: -32756, Y: 32756}
	if got != want {
		t.Fatalf("DeriveMotionVector() = %+v; want %+v", got, want)
	}
}

func TestInverseScaleLuma4x4QPRoundingAndScalingClasses(t *testing.T) {
	var levels [16]int32
	var scalingList [16]uint8
	for index := range levels {
		levels[index] = 1
		scalingList[index] = 16
	}

	tests := []struct {
		name string
		qpy  int
		want [16]int64
	}{
		{
			name: "low QP rounds after right shift",
			qpy:  0,
			want: [16]int64{10, 13, 10, 13, 13, 16, 13, 16, 10, 13, 10, 13, 13, 16, 13, 16},
		},
		{
			name: "high QP left shift",
			qpy:  24,
			want: [16]int64{160, 208, 160, 208, 208, 256, 208, 256, 160, 208, 160, 208, 208, 256, 208, 256},
		},
		{
			name: "maximum QP",
			qpy:  51,
			want: [16]int64{3584, 4608, 3584, 4608, 4608, 5888, 4608, 5888, 3584, 4608, 3584, 4608, 4608, 5888, 4608, 5888},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := inverseScaleLuma4x4(levels, scalingList, test.qpy)
			if err != nil || got != test.want {
				t.Fatalf("inverseScaleLuma4x4() = %v, %v; want %v", got, err, test.want)
			}
		})
	}
}

func TestInverseScaleLuma4x4NegativeLevelAndCustomWeight(t *testing.T) {
	var levels [16]int32
	var scalingList [16]uint8
	levels[0], levels[1] = -1, 1
	for index := range scalingList {
		scalingList[index] = 16
	}
	scalingList[1] = 8

	got, err := inverseScaleLuma4x4(levels, scalingList, 0)
	if err != nil || got[0] != -10 || got[1] != 7 {
		t.Fatalf("inverseScaleLuma4x4() = %v, %v; want coefficients -10 and 7", got, err)
	}
}

func TestInverseScaleLuma4x4RejectsInvalidInputs(t *testing.T) {
	var levels [16]int32
	var scalingList [16]uint8
	for index := range scalingList {
		scalingList[index] = 16
	}
	for _, qpy := range []int{-1, 52} {
		if _, err := inverseScaleLuma4x4(levels, scalingList, qpy); !errors.Is(err, ErrInverseScaleQPYOutOfRange) {
			t.Errorf("qpy %d error = %v; want QPY range error", qpy, err)
		}
	}
	scalingList[7] = 0
	if _, err := inverseScaleLuma4x4(levels, scalingList, 0); !errors.Is(err, ErrInverseScaleListZero) {
		t.Errorf("zero scaling weight error = %v; want scaling-list error", err)
	}
}

func TestInverseScaleLuma8x8ScalingClassesAndQPRounding(t *testing.T) {
	var levels [64]int32
	var scalingList [64]uint8
	for index := range levels {
		levels[index] = 1
		scalingList[index] = 16
	}
	tests := []struct {
		name string
		qpy  int
		want [64]int64
	}{
		{
			name: "low QP covers six scaling classes",
			qpy:  0,
			want: [64]int64{
				20, 19, 25, 19, 20, 19, 25, 19,
				19, 18, 24, 18, 19, 18, 24, 18,
				25, 24, 32, 24, 25, 24, 32, 24,
				19, 18, 24, 18, 19, 18, 24, 18,
				20, 19, 25, 19, 20, 19, 25, 19,
				19, 18, 24, 18, 19, 18, 24, 18,
				25, 24, 32, 24, 25, 24, 32, 24,
				19, 18, 24, 18, 19, 18, 24, 18,
			},
		},
		{
			name: "high QP left shift",
			qpy:  24,
			want: [64]int64{
				320, 304, 400, 304, 320, 304, 400, 304,
				304, 288, 384, 288, 304, 288, 384, 288,
				400, 384, 512, 384, 400, 384, 512, 384,
				304, 288, 384, 288, 304, 288, 384, 288,
				320, 304, 400, 304, 320, 304, 400, 304,
				304, 288, 384, 288, 304, 288, 384, 288,
				400, 384, 512, 384, 400, 384, 512, 384,
				304, 288, 384, 288, 304, 288, 384, 288,
			},
		},
		{
			name: "maximum QP",
			qpy:  51,
			want: [64]int64{
				7168, 6656, 8960, 6656, 7168, 6656, 8960, 6656,
				6656, 6400, 8448, 6400, 6656, 6400, 8448, 6400,
				8960, 8448, 11520, 8448, 8960, 8448, 11520, 8448,
				6656, 6400, 8448, 6400, 6656, 6400, 8448, 6400,
				7168, 6656, 8960, 6656, 7168, 6656, 8960, 6656,
				6656, 6400, 8448, 6400, 6656, 6400, 8448, 6400,
				8960, 8448, 11520, 8448, 8960, 8448, 11520, 8448,
				6656, 6400, 8448, 6400, 6656, 6400, 8448, 6400,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := inverseScaleLuma8x8(levels, scalingList, test.qpy)
			if err != nil || got != test.want {
				t.Fatalf("inverseScaleLuma8x8() = %v, %v; want %v", got, err, test.want)
			}
		})
	}
}

func TestInverseScaleLuma8x8NegativeLevelAndCustomWeight(t *testing.T) {
	var levels [64]int32
	var scalingList [64]uint8
	levels[0], levels[1] = -1, 1
	for index := range scalingList {
		scalingList[index] = 16
	}
	scalingList[1] = 8

	got, err := inverseScaleLuma8x8(levels, scalingList, 0)
	if err != nil || got[0] != -20 || got[1] != 10 {
		t.Fatalf("inverseScaleLuma8x8() = %v, %v; want coefficients -20 and 10", got, err)
	}
}

func TestInverseScaleLuma8x8RejectsInvalidInputs(t *testing.T) {
	var levels [64]int32
	var scalingList [64]uint8
	for index := range scalingList {
		scalingList[index] = 16
	}
	for _, qpy := range []int{-1, 52} {
		if _, err := inverseScaleLuma8x8(levels, scalingList, qpy); !errors.Is(err, ErrInverseScaleQPYOutOfRange) {
			t.Errorf("qpy %d error = %v; want QPY range error", qpy, err)
		}
	}
	scalingList[42] = 0
	if _, err := inverseScaleLuma8x8(levels, scalingList, 0); !errors.Is(err, ErrInverseScaleListZero) {
		t.Errorf("zero scaling weight error = %v; want scaling-list error", err)
	}
}

func TestInverseTransformLuma4x4DCAndImpulseVectors(t *testing.T) {
	var dc [16]int64
	dc[0] = 64
	wantDC := [16]int64{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}
	if got := inverseTransformLuma4x4(dc); got != wantDC {
		t.Fatalf("DC inverse transform = %v; want %v", got, wantDC)
	}

	var horizontalFrequency [16]int64
	horizontalFrequency[1] = 64
	wantHorizontalFrequency := [16]int64{
		1, 1, 0, -1,
		1, 1, 0, -1,
		1, 1, 0, -1,
		1, 1, 0, -1,
	}
	if got := inverseTransformLuma4x4(horizontalFrequency); got != wantHorizontalFrequency {
		t.Fatalf("horizontal-frequency inverse transform = %v; want %v", got, wantHorizontalFrequency)
	}
}

func TestInverseTransformChromaDC2x2MatchesHadamardVectors(t *testing.T) {
	tests := []struct {
		name         string
		coefficients [4]int32
		want         [4]int64
	}{
		{name: "DC only", coefficients: [4]int32{7, 7, 7, 7}, want: [4]int64{28, 0, 0, 0}},
		{name: "top-left impulse", coefficients: [4]int32{1}, want: [4]int64{1, 1, 1, 1}},
		{name: "mixed coefficients", coefficients: [4]int32{1, 2, 3, 4}, want: [4]int64{10, -2, -4, 0}},
		{name: "negative coefficients", coefficients: [4]int32{-1, 2, -3, 4}, want: [4]int64{2, -10, 0, 4}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := inverseTransformChromaDC2x2(test.coefficients); got != test.want {
				t.Fatalf("inverseTransformChromaDC2x2(%v) = %v; want %v", test.coefficients, got, test.want)
			}
		})
	}
}

func TestInverseTransformLuma4x4Rounding(t *testing.T) {
	for _, test := range []struct {
		name  string
		level int64
		want  int64
	}{
		{name: "below positive half", level: 31, want: 0},
		{name: "positive half rounds up", level: 32, want: 1},
		{name: "negative arithmetic shift", level: -33, want: -1},
	} {
		t.Run(test.name, func(t *testing.T) {
			var coefficients [16]int64
			coefficients[0] = test.level
			for index, value := range inverseTransformLuma4x4(coefficients) {
				if value != test.want {
					t.Fatalf("residual[%d] = %d; want %d", index, value, test.want)
				}
			}
		})
	}
}

func TestInverseTransformLuma8x8DCAndImpulseVectors(t *testing.T) {
	var dc [64]int64
	dc[0] = 64
	if got := inverseTransformLuma8x8(dc); got != [64]int64{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1} {
		t.Fatalf("DC inverse transform = %v; want all ones", got)
	}

	frequency := [8]int64{2, -1, 1, 0, 0, -1, 1, -1}
	var horizontalFrequency [64]int64
	horizontalFrequency[1] = 64
	wantHorizontal := [64]int64{}
	for row := 0; row < 8; row++ {
		copy(wantHorizontal[row*8:row*8+8], frequency[:])
	}
	if got := inverseTransformLuma8x8(horizontalFrequency); got != wantHorizontal {
		t.Fatalf("horizontal-frequency inverse transform = %v; want %v", got, wantHorizontal)
	}

	var verticalFrequency [64]int64
	verticalFrequency[8] = 64
	wantVertical := [64]int64{}
	for row, value := range frequency {
		for column := 0; column < 8; column++ {
			wantVertical[row*8+column] = value
		}
	}
	if got := inverseTransformLuma8x8(verticalFrequency); got != wantVertical {
		t.Fatalf("vertical-frequency inverse transform = %v; want %v", got, wantVertical)
	}
}

func TestInverseTransformLuma8x8Rounding(t *testing.T) {
	for _, test := range []struct {
		name  string
		level int64
		want  int64
	}{
		{name: "below positive half", level: 31, want: 0},
		{name: "positive half rounds up", level: 32, want: 1},
		{name: "negative arithmetic shift", level: -33, want: -1},
	} {
		t.Run(test.name, func(t *testing.T) {
			var coefficients [64]int64
			coefficients[0] = test.level
			for index, value := range inverseTransformLuma8x8(coefficients) {
				if value != test.want {
					t.Fatalf("residual[%d] = %d; want %d", index, value, test.want)
				}
			}
		})
	}
}

func TestPredictLumaIntra8x8VerticalRepeatsFilteredTopSamplesAcrossRows(t *testing.T) {
	top := [8]uint8{0, 17, 63, 129, 190, 220, 254, 255}
	prediction := predictLumaIntra8x8Vertical(top)
	for row := 0; row < 8; row++ {
		for column, sample := range top {
			if got := prediction[row*8+column]; got != sample {
				t.Fatalf("prediction[%d][%d] = %d; want %d", row, column, got, sample)
			}
		}
	}
}

func TestPredictLumaIntra16x16VerticalRepeatsFilteredTopSamplesAcrossRows(t *testing.T) {
	top := [16]uint8{0, 17, 33, 51, 68, 85, 102, 119, 136, 153, 170, 187, 204, 221, 238, 255}
	prediction := predictLumaIntra16x16Vertical(top)
	for row := 0; row < 16; row++ {
		for column, sample := range top {
			if got := prediction[row*16+column]; got != sample {
				t.Fatalf("prediction[%d][%d] = %d; want %d", row, column, got, sample)
			}
		}
	}
}

func TestPredictLumaIntra16x16HorizontalRepeatsFilteredLeftSamplesAcrossRows(t *testing.T) {
	left := [16]uint8{0, 17, 33, 51, 68, 85, 102, 119, 136, 153, 170, 187, 204, 221, 238, 255}
	prediction := predictLumaIntra16x16Horizontal(left)
	for row, sample := range left {
		for column := 0; column < 16; column++ {
			if got := prediction[row*16+column]; got != sample {
				t.Fatalf("prediction[%d][%d] = %d; want %d", row, column, got, sample)
			}
		}
	}
}

func TestPredictLumaIntra16x16DCReferenceAvailabilityAndRounding(t *testing.T) {
	var top, left [16]uint8
	for index := range top {
		top[index] = uint8(index)
		left[index] = uint8(index + 16)
	}
	repeat := func(value uint8) [256]uint8 {
		var prediction [256]uint8
		for index := range prediction {
			prediction[index] = value
		}
		return prediction
	}
	tests := []struct {
		name string
		top  *[16]uint8
		left *[16]uint8
		want uint8
	}{
		{name: "both edges", top: &top, left: &left, want: 16},
		{name: "top only", top: &top, want: 8},
		{name: "left only", left: &left, want: 24},
		{name: "no edges", want: 128},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := predictLumaIntra16x16DC(test.top, test.left); got != repeat(test.want) {
				t.Fatalf("DC intra prediction = %v; want every sample %d", got, test.want)
			}
		})
	}
}

func TestPredictLumaIntra16x16PlaneGradientAndClipping(t *testing.T) {
	var top, left [16]uint8
	for index := 0; index < 16; index++ {
		top[index] = uint8(80 + 2*(index+1))
		left[index] = uint8(80 + 3*(index+1))
	}
	for row := 0; row < 16; row++ {
		for column := 0; column < 16; column++ {
			want := uint8(85 + 2*column + 3*row)
			if got := predictLumaIntra16x16Plane(top, left, 80)[row*16+column]; got != want {
				t.Fatalf("plane prediction[%d][%d] = %d; want %d", row, column, got, want)
			}
		}
	}

	var highTop, highLeft [16]uint8
	highTop[15], highLeft[15] = 255, 255
	if got := predictLumaIntra16x16Plane(highTop, highLeft, 0)[255]; got != 255 {
		t.Fatalf("high plane prediction = %d; want clipped value 255", got)
	}
	var lowTop, lowLeft [16]uint8
	lowTop[0], lowLeft[0] = 255, 255
	if got := predictLumaIntra16x16Plane(lowTop, lowLeft, 0)[255]; got != 0 {
		t.Fatalf("low plane prediction = %d; want clipped value 0", got)
	}
}

func TestPredictLumaIntra8x8HorizontalRepeatsFilteredLeftSamplesAcrossRows(t *testing.T) {
	left := [8]uint8{0, 17, 63, 129, 190, 220, 254, 255}
	prediction := predictLumaIntra8x8Horizontal(left)
	for row, sample := range left {
		for column := 0; column < 8; column++ {
			if got := prediction[row*8+column]; got != sample {
				t.Fatalf("prediction[%d][%d] = %d; want %d", row, column, got, sample)
			}
		}
	}
}

func TestPredictLumaIntra8x8DCReferenceAvailabilityAndRounding(t *testing.T) {
	top := [8]uint8{10, 40, 90, 160, 20, 50, 80, 110}
	left := [8]uint8{20, 60, 100, 140, 30, 70, 110, 150}
	repeat := func(value uint8) [64]uint8 {
		var prediction [64]uint8
		for index := range prediction {
			prediction[index] = value
		}
		return prediction
	}
	tests := []struct {
		name string
		top  *[8]uint8
		left *[8]uint8
		want uint8
	}{
		{name: "both edges", top: &top, left: &left, want: 78},
		{name: "top only", top: &top, want: 70},
		{name: "left only", left: &left, want: 85},
		{name: "no edges", want: 128},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := predictLumaIntra8x8DC(test.top, test.left); got != repeat(test.want) {
				t.Fatalf("DC intra prediction = %v; want every sample %d", got, test.want)
			}
		})
	}
}

func TestPredictLumaIntra8x8DiagonalDownLeftInterpolatesFilteredTopReferences(t *testing.T) {
	top := [16]uint8{10, 20, 30, 40, 50, 60, 70, 80, 90, 100, 110, 120, 130, 140, 150, 160}
	want := [64]uint8{
		20, 30, 40, 50, 60, 70, 80, 90,
		30, 40, 50, 60, 70, 80, 90, 100,
		40, 50, 60, 70, 80, 90, 100, 110,
		50, 60, 70, 80, 90, 100, 110, 120,
		60, 70, 80, 90, 100, 110, 120, 130,
		70, 80, 90, 100, 110, 120, 130, 140,
		80, 90, 100, 110, 120, 130, 140, 150,
		90, 100, 110, 120, 130, 140, 150, 158,
	}
	if got := predictLumaIntra8x8DiagonalDownLeft(top); got != want {
		t.Fatalf("diagonal-down-left 8x8 prediction = %v; want %v", got, want)
	}
}

func TestPredictLumaIntra4x4VerticalRepeatsTopSamplesAcrossRows(t *testing.T) {
	tests := []struct {
		name string
		top  [4]uint8
		want [16]uint8
	}{
		{
			name: "distinct top samples",
			top:  [4]uint8{10, 40, 90, 160},
			want: [16]uint8{10, 40, 90, 160, 10, 40, 90, 160, 10, 40, 90, 160, 10, 40, 90, 160},
		},
		{
			name: "sample endpoints",
			top:  [4]uint8{0, 255, 0, 255},
			want: [16]uint8{0, 255, 0, 255, 0, 255, 0, 255, 0, 255, 0, 255, 0, 255, 0, 255},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := predictLumaIntra4x4Vertical(test.top); got != test.want {
				t.Fatalf("vertical intra prediction = %v; want %v", got, test.want)
			}
		})
	}
}

func TestPredictLumaIntra4x4HorizontalRepeatsLeftSamplesAcrossRows(t *testing.T) {
	tests := []struct {
		name string
		left [4]uint8
		want [16]uint8
	}{
		{
			name: "distinct left samples",
			left: [4]uint8{10, 40, 90, 160},
			want: [16]uint8{10, 10, 10, 10, 40, 40, 40, 40, 90, 90, 90, 90, 160, 160, 160, 160},
		},
		{
			name: "sample endpoints",
			left: [4]uint8{0, 255, 0, 255},
			want: [16]uint8{0, 0, 0, 0, 255, 255, 255, 255, 0, 0, 0, 0, 255, 255, 255, 255},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := predictLumaIntra4x4Horizontal(test.left); got != test.want {
				t.Fatalf("horizontal intra prediction = %v; want %v", got, test.want)
			}
		})
	}
}

func TestPredictLumaIntra4x4DCReferenceAvailability(t *testing.T) {
	top := [4]uint8{10, 40, 90, 161}
	left := [4]uint8{20, 60, 100, 141}
	repeat := func(value uint8) [16]uint8 {
		var prediction [16]uint8
		for index := range prediction {
			prediction[index] = value
		}
		return prediction
	}
	tests := []struct {
		name string
		top  *[4]uint8
		left *[4]uint8
		want uint8
	}{
		{name: "both neighbors", top: &top, left: &left, want: 78},
		{name: "top only", top: &top, want: 75},
		{name: "left only", left: &left, want: 80},
		{name: "no neighbors", want: 128},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := predictLumaIntra4x4DC(test.top, test.left); got != repeat(test.want) {
				t.Fatalf("DC intra prediction = %v; want every sample %d", got, test.want)
			}
		})
	}
}

func TestPredictLumaIntra4x4DiagonalDownLeftInterpolatesTopReferences(t *testing.T) {
	top := [8]uint8{10, 20, 30, 40, 50, 60, 70, 80}
	want := [16]uint8{
		20, 30, 40, 50,
		30, 40, 50, 60,
		40, 50, 60, 70,
		50, 60, 70, 78,
	}
	if got := predictLumaIntra4x4DiagonalDownLeft(top); got != want {
		t.Fatalf("diagonal-down-left intra prediction = %v; want %v", got, want)
	}
}

func TestPredictLumaIntra4x4DiagonalDownRightUsesTopLeftTopAndLeftReferences(t *testing.T) {
	top := [8]uint8{20, 40, 80, 120, 160, 200, 220, 240}
	left := [4]uint8{60, 100, 140, 180}
	want := [16]uint8{
		28, 45, 80, 120,
		35, 28, 45, 80,
		63, 35, 28, 45,
		100, 63, 35, 28,
	}
	if got := predictLumaIntra4x4DiagonalDownRight(top, left, 30); got != want {
		t.Fatalf("diagonal-down-right intra prediction = %v; want %v", got, want)
	}
}

func TestPredictLumaIntra8x8DiagonalDownRightUsesAllFilteredReferenceRegions(t *testing.T) {
	top := [16]uint8{20, 40, 60, 80, 100, 120, 140, 160, 180, 200, 220, 240, 250, 230, 210, 190}
	left := [8]uint8{60, 100, 140, 180, 220, 240, 200, 160}
	want := [64]uint8{
		28, 40, 60, 80, 100, 120, 140, 160,
		35, 28, 40, 60, 80, 100, 120, 140,
		63, 35, 28, 40, 60, 80, 100, 120,
		100, 63, 35, 28, 40, 60, 80, 100,
		140, 100, 63, 35, 28, 40, 60, 80,
		180, 140, 100, 63, 35, 28, 40, 60,
		215, 180, 140, 100, 63, 35, 28, 40,
		225, 215, 180, 140, 100, 63, 35, 28,
	}
	if got := predictLumaIntra8x8DiagonalDownRight(top, left, 30); got != want {
		t.Fatalf("diagonal-down-right 8x8 prediction = %v; want %v", got, want)
	}
}

func TestPredictLumaIntra8x8VerticalRightUsesAllFilteredReferenceRegions(t *testing.T) {
	top := [16]uint8{20, 40, 60, 80, 100, 120, 140, 160, 180, 200, 220, 240, 250, 230, 210, 190}
	left := [8]uint8{60, 100, 140, 180, 220, 240, 200, 160}
	want := [64]uint8{
		28, 40, 60, 80, 100, 120, 140, 160,
		45, 25, 30, 50, 70, 90, 110, 130,
		35, 28, 40, 60, 80, 100, 120, 140,
		80, 45, 25, 30, 50, 70, 90, 110,
		100, 35, 28, 40, 60, 80, 100, 120,
		120, 80, 45, 25, 30, 50, 70, 90,
		140, 100, 35, 28, 40, 60, 80, 100,
		160, 120, 80, 45, 25, 30, 50, 70,
	}
	if got := predictLumaIntra8x8VerticalRight(top, left, 30); got != want {
		t.Fatalf("vertical-right 8x8 prediction = %v; want %v", got, want)
	}
}

func TestPredictLumaIntra8x8HorizontalDownTransposesVerticalRight(t *testing.T) {
	top := [8]uint8{60, 100, 140, 180, 220, 240, 200, 160}
	left := [16]uint8{20, 40, 60, 80, 100, 120, 140, 160, 180, 200, 220, 240, 250, 230, 210, 190}
	want := [64]uint8{
		28, 45, 35, 80, 100, 120, 140, 160,
		40, 25, 28, 45, 35, 80, 100, 120,
		60, 30, 40, 25, 28, 45, 35, 80,
		80, 50, 60, 30, 40, 25, 28, 45,
		100, 70, 80, 50, 60, 30, 40, 25,
		120, 90, 100, 70, 80, 50, 60, 30,
		140, 110, 120, 90, 100, 70, 80, 50,
		160, 130, 140, 110, 120, 90, 100, 70,
	}
	if got := predictLumaIntra8x8HorizontalDown(top, left, 30); got != want {
		t.Fatalf("horizontal-down 8x8 prediction = %v; want %v", got, want)
	}
}

func TestPredictLumaIntra8x8VerticalLeftAlternatesTopReferencePhases(t *testing.T) {
	top := [16]uint8{10, 20, 30, 40, 50, 60, 70, 80, 90, 100, 110, 120, 130, 140, 150, 160}
	want := [64]uint8{
		15, 25, 35, 45, 55, 65, 75, 85,
		20, 30, 40, 50, 60, 70, 80, 90,
		25, 35, 45, 55, 65, 75, 85, 95,
		30, 40, 50, 60, 70, 80, 90, 100,
		35, 45, 55, 65, 75, 85, 95, 105,
		40, 50, 60, 70, 80, 90, 100, 110,
		45, 55, 65, 75, 85, 95, 105, 115,
		50, 60, 70, 80, 90, 100, 110, 120,
	}
	if got := predictLumaIntra8x8VerticalLeft(top); got != want {
		t.Fatalf("vertical-left 8x8 prediction = %v; want %v", got, want)
	}
}

func TestPredictLumaIntra8x8HorizontalUpTransposesVerticalLeft(t *testing.T) {
	left := [16]uint8{10, 20, 30, 40, 50, 60, 70, 80, 90, 100, 110, 120, 130, 140, 150, 160}
	want := [64]uint8{
		15, 20, 25, 30, 35, 40, 45, 50,
		25, 30, 35, 40, 45, 50, 55, 60,
		35, 40, 45, 50, 55, 60, 65, 70,
		45, 50, 55, 60, 65, 70, 75, 80,
		55, 60, 65, 70, 75, 80, 85, 90,
		65, 70, 75, 80, 85, 90, 95, 100,
		75, 80, 85, 90, 95, 100, 105, 110,
		85, 90, 95, 100, 105, 110, 115, 120,
	}
	if got := predictLumaIntra8x8HorizontalUp(left); got != want {
		t.Fatalf("horizontal-up 8x8 prediction = %v; want %v", got, want)
	}
}

func TestPredictLumaIntra8x8PlaneReconstructsAffineGradient(t *testing.T) {
	var top, left [16]uint8
	for index := 0; index < 9; index++ {
		top[index] = uint8(80 + 2*index)
		left[index] = uint8(80 + 3*index)
	}
	want := [64]uint8{
		85, 87, 89, 91, 93, 95, 97, 99,
		88, 90, 92, 94, 96, 98, 100, 102,
		91, 93, 95, 97, 99, 101, 103, 105,
		94, 96, 98, 100, 102, 104, 106, 108,
		97, 99, 101, 103, 105, 107, 109, 111,
		100, 102, 104, 106, 108, 110, 112, 114,
		103, 105, 107, 109, 111, 113, 115, 117,
		106, 108, 110, 112, 114, 116, 118, 120,
	}
	if got := predictLumaIntra8x8Plane(top, left); got != want {
		t.Fatalf("plane prediction = %v; want %v", got, want)
	}
}

func TestPredictLumaIntra8x8PlaneClipsOutput(t *testing.T) {
	var highTop, highLeft [16]uint8
	highTop[8], highLeft[8] = 255, 255
	if got := predictLumaIntra8x8Plane(highTop, highLeft)[63]; got != 255 {
		t.Fatalf("high plane prediction = %d; want clipped value 255", got)
	}
	var lowTop, lowLeft [16]uint8
	lowTop[0], lowLeft[0] = 255, 255
	if got := predictLumaIntra8x8Plane(lowTop, lowLeft)[63]; got != 0 {
		t.Fatalf("low plane prediction = %d; want clipped value 0", got)
	}
}

func TestPredictLumaIntra4x4VerticalRightUsesTopLeftTopAndLeftReferences(t *testing.T) {
	top := [8]uint8{20, 40, 80, 120, 160, 200, 220, 240}
	left := [4]uint8{60, 100, 140, 180}
	want := [16]uint8{
		28, 45, 80, 120,
		45, 25, 30, 60,
		35, 28, 45, 80,
		80, 45, 25, 30,
	}
	if got := predictLumaIntra4x4VerticalRight(top, left, 30); got != want {
		t.Fatalf("vertical-right intra prediction = %v; want %v", got, want)
	}
}

func TestPredictLumaIntra4x4HorizontalDownTransposesVerticalRight(t *testing.T) {
	top := [4]uint8{20, 40, 80, 120}
	left := [4]uint8{60, 100, 140, 180}
	want := [16]uint8{
		63, 25, 35, 30,
		100, 45, 63, 25,
		140, 80, 100, 45,
		170, 120, 140, 80,
	}
	if got := predictLumaIntra4x4HorizontalDown(top, left, 30); got != want {
		t.Fatalf("horizontal-down intra prediction = %v; want %v", got, want)
	}
}

func TestPredictLumaIntra4x4VerticalLeftAlternatesTopReferencePhases(t *testing.T) {
	top := [8]uint8{10, 20, 30, 40, 50, 60, 70, 80}
	want := [16]uint8{
		15, 25, 35, 45,
		20, 30, 40, 50,
		25, 35, 45, 55,
		30, 40, 50, 60,
	}
	if got := predictLumaIntra4x4VerticalLeft(top); got != want {
		t.Fatalf("vertical-left intra prediction = %v; want %v", got, want)
	}
}

func TestPredictLumaIntra4x4HorizontalUpTransposesVerticalLeft(t *testing.T) {
	left := [8]uint8{10, 20, 30, 40, 50, 60, 70, 80}
	want := [16]uint8{
		15, 20, 25, 30,
		25, 30, 35, 40,
		35, 40, 45, 50,
		45, 50, 55, 60,
	}
	if got := predictLumaIntra4x4HorizontalUp(left); got != want {
		t.Fatalf("horizontal-up intra prediction = %v; want %v", got, want)
	}
}
