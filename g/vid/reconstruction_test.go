package vid

import (
	"errors"
	"testing"
)

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
