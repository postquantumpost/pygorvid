package vid

import (
	"errors"
	"sort"
)

var ErrReferenceFrameNum = errors.New("reference frame number is outside the configured range")
var ErrReferenceListModification = errors.New("reference-list modification is invalid or unresolved")
var ErrLumaFractionalSampleOffset = errors.New("luma fractional-sample offset is outside [0,3]")
var ErrLumaReferencePlaneLayout = errors.New("luma reference plane layout is invalid or truncated")
var ErrPictureOrderCount = errors.New("picture order count input or arithmetic is invalid")
var ErrLumaDeblockingParameters = errors.New("luma deblocking parameters are invalid")
var ErrLumaDeblockingBoundaryStrength = errors.New("luma deblocking boundary strength is outside [0,4]")
var ErrLumaInterPrediction = errors.New("luma inter-prediction input is invalid")
var ErrLumaDeblockingPlaneLayout = errors.New("luma deblocking plane layout or edge coordinates are invalid")
var ErrLumaDeblockingMacroblockAddress = errors.New("luma deblocking macroblock address or slice map is invalid")
var ErrChromaIntraPredictionMode = errors.New("chroma intra prediction mode is outside [0,3]")
var ErrChromaIntraPredictionReference = errors.New("required chroma intra prediction reference is unavailable")

var lumaAlphaTable = [52]uint8{
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	4, 4, 5, 6, 7, 8, 9, 10, 12, 13, 15, 17, 20, 22, 25, 28,
	32, 36, 40, 45, 50, 56, 63, 71, 80, 90, 101, 113, 127, 144, 162, 182,
	203, 226, 255, 255,
}

var lumaBetaTable = [52]uint8{
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	2, 2, 2, 3, 3, 3, 3, 4, 4, 4, 6, 6, 7, 7,
	8, 8, 9, 9, 10, 10, 11, 11, 12, 12, 13, 13, 14, 14, 15, 15,
	16, 16, 17, 18,
}

var lumaTC0Table = [3][52]uint8{
	{
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1,
		1, 1, 1, 1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 4, 4, 4, 5, 6, 6, 7, 8, 9, 10, 11, 13,
	},
	{
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 1,
		1, 1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 4, 4, 5, 5, 6, 7, 8, 8, 10, 11, 12, 13, 15, 17,
	},
	{
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1,
		1, 2, 2, 2, 2, 3, 3, 3, 4, 4, 4, 5, 6, 6, 7, 8, 9, 10, 11, 13, 14, 16, 18, 20, 23, 25,
	},
}

type LumaDeblockingMode uint8

const (
	LumaDeblockingAllEdges LumaDeblockingMode = iota
	LumaDeblockingDisabled
	LumaDeblockingAllExceptSliceBoundaries
)

type LumaDeblockingParameters struct {
	Mode   LumaDeblockingMode
	IndexA uint8
	IndexB uint8
}

type LumaDeblockingThresholds struct {
	Alpha uint8
	Beta  uint8
}

type LumaEdgeSamples struct {
	P0 uint8
	P1 uint8
	P2 uint8
	Q0 uint8
	Q1 uint8
	Q2 uint8
}

type LumaStrongEdgeSamples struct {
	P0 uint8
	P1 uint8
	P2 uint8
	P3 uint8
	Q0 uint8
	Q1 uint8
	Q2 uint8
	Q3 uint8
}

type ChromaEdgeSamples struct {
	P0 uint8
	P1 uint8
	Q0 uint8
	Q1 uint8
}

func FilterLumaWeakEdge(samples LumaEdgeSamples, beta, tc0 uint8) LumaEdgeSamples {
	ap := absLumaDifference(samples.P2, samples.P0)
	aq := absLumaDifference(samples.Q2, samples.Q0)
	tc := int(tc0)
	if ap < beta {
		tc++
	}
	if aq < beta {
		tc++
	}

	delta := (((int(samples.Q0) - int(samples.P0)) << 2) + (int(samples.P1) - int(samples.Q1)) + 4) >> 3
	delta = clipLumaDeblockingDelta(delta, tc)
	filtered := samples
	filtered.P0 = clipLumaSample(int(samples.P0) + delta)
	filtered.Q0 = clipLumaSample(int(samples.Q0) - delta)
	if ap < beta {
		deltaP1 := (int(samples.P2) + ((int(samples.P0) + int(samples.Q0) + 1) >> 1) - (int(samples.P1) << 1)) >> 1
		deltaP1 = clipLumaDeblockingDelta(deltaP1, int(tc0))
		filtered.P1 = clipLumaSample(int(samples.P1) + deltaP1)
	}
	if aq < beta {
		deltaQ1 := (int(samples.Q2) + ((int(samples.P0) + int(samples.Q0) + 1) >> 1) - (int(samples.Q1) << 1)) >> 1
		deltaQ1 = clipLumaDeblockingDelta(deltaQ1, int(tc0))
		filtered.Q1 = clipLumaSample(int(samples.Q1) + deltaQ1)
	}
	return filtered
}

func FilterChromaWeakEdge(samples ChromaEdgeSamples, tc0 uint8) ChromaEdgeSamples {
	tc := int(tc0) + 1
	delta := (((int(samples.Q0) - int(samples.P0)) << 2) + (int(samples.P1) - int(samples.Q1)) + 4) >> 3
	delta = clipLumaDeblockingDelta(delta, tc)
	filtered := samples
	filtered.P0 = clipLumaSample(int(samples.P0) + delta)
	filtered.Q0 = clipLumaSample(int(samples.Q0) - delta)
	return filtered
}

func FilterLumaStrongEdge(samples LumaStrongEdgeSamples, alpha, beta uint8) LumaEdgeSamples {
	filtered := LumaEdgeSamples{
		P0: samples.P0, P1: samples.P1, P2: samples.P2,
		Q0: samples.Q0, Q1: samples.Q1, Q2: samples.Q2,
	}
	strongLimit := int(alpha>>2) + 2
	alphaPasses := absLumaDifference(samples.P0, samples.Q0) < uint8(strongLimit)
	if absLumaDifference(samples.P2, samples.P0) < beta && alphaPasses {
		filtered.P0 = clipLumaSample((int(samples.P2) + 2*int(samples.P1) + 2*int(samples.P0) + 2*int(samples.Q0) + int(samples.Q1) + 4) >> 3)
		filtered.P1 = clipLumaSample((int(samples.P2) + int(samples.P1) + int(samples.P0) + int(samples.Q0) + 2) >> 2)
		filtered.P2 = clipLumaSample((2*int(samples.P3) + 3*int(samples.P2) + int(samples.P1) + int(samples.P0) + int(samples.Q0) + 4) >> 3)
	} else {
		filtered.P0 = clipLumaSample((2*int(samples.P1) + int(samples.P0) + int(samples.Q1) + 2) >> 2)
	}
	if absLumaDifference(samples.Q2, samples.Q0) < beta && alphaPasses {
		filtered.Q0 = clipLumaSample((int(samples.P1) + 2*int(samples.P0) + 2*int(samples.Q0) + 2*int(samples.Q1) + int(samples.Q2) + 4) >> 3)
		filtered.Q1 = clipLumaSample((int(samples.P0) + int(samples.Q0) + int(samples.Q1) + int(samples.Q2) + 2) >> 2)
		filtered.Q2 = clipLumaSample((2*int(samples.Q3) + 3*int(samples.Q2) + int(samples.Q1) + int(samples.Q0) + int(samples.P0) + 4) >> 3)
	} else {
		filtered.Q0 = clipLumaSample((2*int(samples.Q1) + int(samples.Q0) + int(samples.P1) + 2) >> 2)
	}
	return filtered
}

func clipLumaDeblockingDelta(value, limit int) int {
	if value < -limit {
		return -limit
	}
	if value > limit {
		return limit
	}
	return value
}

func LookupLumaDeblockingThresholds(indexA, indexB uint8) (LumaDeblockingThresholds, error) {
	if indexA > 51 || indexB > 51 {
		return LumaDeblockingThresholds{}, ErrLumaDeblockingParameters
	}
	return LumaDeblockingThresholds{Alpha: lumaAlphaTable[indexA], Beta: lumaBetaTable[indexB]}, nil
}

func LookupLumaTC0(indexA, boundaryStrength uint8) (uint8, error) {
	if indexA > 51 || boundaryStrength < 1 || boundaryStrength > 3 {
		return 0, ErrLumaDeblockingParameters
	}
	return lumaTC0Table[boundaryStrength-1][indexA], nil
}

func ShouldFilterLumaEdge(p0, q0, p1, q1, boundaryStrength uint8, thresholds LumaDeblockingThresholds) (bool, error) {
	if boundaryStrength > 4 {
		return false, ErrLumaDeblockingBoundaryStrength
	}
	if boundaryStrength == 0 {
		return false, nil
	}
	return absLumaDifference(p0, q0) < thresholds.Alpha &&
		absLumaDifference(p1, p0) < thresholds.Beta &&
		absLumaDifference(q1, q0) < thresholds.Beta, nil
}

func ApplyLumaDeblockingEdge(samples LumaStrongEdgeSamples, boundaryStrength uint8, parameters LumaDeblockingParameters, sliceBoundary bool) (LumaEdgeSamples, error) {
	unchanged := LumaEdgeSamples{P0: samples.P0, P1: samples.P1, P2: samples.P2, Q0: samples.Q0, Q1: samples.Q1, Q2: samples.Q2}
	if parameters.Mode > LumaDeblockingAllExceptSliceBoundaries || parameters.IndexA > 51 || parameters.IndexB > 51 {
		return LumaEdgeSamples{}, ErrLumaDeblockingParameters
	}
	if boundaryStrength > 4 {
		return LumaEdgeSamples{}, ErrLumaDeblockingBoundaryStrength
	}
	if parameters.Mode == LumaDeblockingDisabled || (parameters.Mode == LumaDeblockingAllExceptSliceBoundaries && sliceBoundary) || boundaryStrength == 0 {
		return unchanged, nil
	}
	thresholds, err := LookupLumaDeblockingThresholds(parameters.IndexA, parameters.IndexB)
	if err != nil {
		return LumaEdgeSamples{}, err
	}
	filter, err := ShouldFilterLumaEdge(samples.P0, samples.Q0, samples.P1, samples.Q1, boundaryStrength, thresholds)
	if err != nil || !filter {
		return unchanged, err
	}
	if boundaryStrength == 4 {
		return FilterLumaStrongEdge(samples, thresholds.Alpha, thresholds.Beta), nil
	}
	tc0, err := LookupLumaTC0(parameters.IndexA, boundaryStrength)
	if err != nil {
		return LumaEdgeSamples{}, err
	}
	return FilterLumaWeakEdge(unchanged, thresholds.Beta, tc0), nil
}

func ApplyLumaDeblockingEdgeSegment(samples [4]LumaStrongEdgeSamples, boundaryStrength uint8, parameters LumaDeblockingParameters, sliceBoundary bool) ([4]LumaEdgeSamples, error) {
	var filtered [4]LumaEdgeSamples
	for index, edgeSamples := range samples {
		result, err := ApplyLumaDeblockingEdge(edgeSamples, boundaryStrength, parameters, sliceBoundary)
		if err != nil {
			return [4]LumaEdgeSamples{}, err
		}
		filtered[index] = result
	}
	return filtered, nil
}

// ApplyLumaDeblockingPlaneEdgeSegment filters four adjacent plane samples; (x,y) is the first q0 and vertical selects a top-to-bottom edge.
func ApplyLumaDeblockingPlaneEdgeSegment(plane []uint8, width, height, stride, x, y int, vertical bool, boundaryStrength uint8, parameters LumaDeblockingParameters, sliceBoundary bool) error {
	if !validYuvPlane(plane, width, height, stride) || x < 0 || y < 0 {
		return ErrLumaDeblockingPlaneLayout
	}
	if vertical {
		if width < 8 || height < 4 || x < 4 || x > width-4 || y > height-4 {
			return ErrLumaDeblockingPlaneLayout
		}
	} else if width < 4 || height < 8 || x > width-4 || y < 4 || y > height-4 {
		return ErrLumaDeblockingPlaneLayout
	}

	var samples [4]LumaStrongEdgeSamples
	for lane := range samples {
		q0Index := y*stride + x + lane
		if vertical {
			q0Index = (y+lane)*stride + x
			samples[lane] = LumaStrongEdgeSamples{
				P0: plane[q0Index-1], P1: plane[q0Index-2], P2: plane[q0Index-3], P3: plane[q0Index-4],
				Q0: plane[q0Index], Q1: plane[q0Index+1], Q2: plane[q0Index+2], Q3: plane[q0Index+3],
			}
		} else {
			samples[lane] = LumaStrongEdgeSamples{
				P0: plane[q0Index-stride], P1: plane[q0Index-2*stride], P2: plane[q0Index-3*stride], P3: plane[q0Index-4*stride],
				Q0: plane[q0Index], Q1: plane[q0Index+stride], Q2: plane[q0Index+2*stride], Q3: plane[q0Index+3*stride],
			}
		}
	}
	filtered, err := ApplyLumaDeblockingEdgeSegment(samples, boundaryStrength, parameters, sliceBoundary)
	if err != nil {
		return err
	}
	for lane, result := range filtered {
		q0Index := y*stride + x + lane
		if vertical {
			q0Index = (y+lane)*stride + x
			plane[q0Index-1], plane[q0Index-2], plane[q0Index-3] = result.P0, result.P1, result.P2
			plane[q0Index], plane[q0Index+1], plane[q0Index+2] = result.Q0, result.Q1, result.Q2
		} else {
			plane[q0Index-stride], plane[q0Index-2*stride], plane[q0Index-3*stride] = result.P0, result.P1, result.P2
			plane[q0Index], plane[q0Index+stride], plane[q0Index+2*stride] = result.Q0, result.Q1, result.Q2
		}
	}
	return nil
}

// ApplyLumaDeblockingPlaneMacroblockEdge filters a 16-sample edge as four ordered 4-sample segments.
func ApplyLumaDeblockingPlaneMacroblockEdge(plane []uint8, width, height, stride, x, y int, vertical bool, boundaryStrengths [4]uint8, parameters LumaDeblockingParameters, sliceBoundary bool) error {
	if !validYuvPlane(plane, width, height, stride) || x < 0 || y < 0 {
		return ErrLumaDeblockingPlaneLayout
	}
	if vertical {
		if width < 8 || height < 16 || x < 4 || x > width-4 || y > height-16 {
			return ErrLumaDeblockingPlaneLayout
		}
	} else if width < 16 || height < 8 || x > width-16 || y < 4 || y > height-4 {
		return ErrLumaDeblockingPlaneLayout
	}
	if parameters.Mode > LumaDeblockingAllExceptSliceBoundaries || parameters.IndexA > 51 || parameters.IndexB > 51 {
		return ErrLumaDeblockingParameters
	}
	for _, boundaryStrength := range boundaryStrengths {
		if boundaryStrength > 4 {
			return ErrLumaDeblockingBoundaryStrength
		}
	}
	for segment, boundaryStrength := range boundaryStrengths {
		segmentX, segmentY := x, y
		if vertical {
			segmentY += 4 * segment
		} else {
			segmentX += 4 * segment
		}
		if err := ApplyLumaDeblockingPlaneEdgeSegment(plane, width, height, stride, segmentX, segmentY, vertical, boundaryStrength, parameters, sliceBoundary); err != nil {
			return err
		}
	}
	return nil
}

type LumaDeblockingMacroblock struct {
	X, Y                        int
	LeftNeighborAvailable       bool
	TopNeighborAvailable        bool
	LeftSliceBoundary           bool
	TopSliceBoundary            bool
	TransformSize8x8            bool
	LeftStrengths               [4]uint8
	TopStrengths                [4]uint8
	VerticalInternalStrengths   [3][4]uint8
	HorizontalInternalStrengths [3][4]uint8
}

type LumaDeblockingEdgeFlags struct {
	FilterLeft     bool
	FilterTop      bool
	FilterInternal bool
}

type LumaDeblockingNeighbors struct {
	LeftIndex         int
	TopIndex          int
	LeftAvailable     bool
	TopAvailable      bool
	LeftSliceBoundary bool
	TopSliceBoundary  bool
}

func DeriveLumaDeblockingNeighbors(macroblockIndex, pictureWidthInMacroblocks, pictureHeightInMacroblocks int, sliceIDs []uint32) (LumaDeblockingNeighbors, error) {
	if macroblockIndex < 0 || pictureWidthInMacroblocks <= 0 || pictureHeightInMacroblocks <= 0 {
		return LumaDeblockingNeighbors{}, ErrLumaDeblockingMacroblockAddress
	}
	maxInt := int(^uint(0) >> 1)
	if pictureWidthInMacroblocks > maxInt/pictureHeightInMacroblocks {
		return LumaDeblockingNeighbors{}, ErrLumaDeblockingMacroblockAddress
	}
	pictureSize := pictureWidthInMacroblocks * pictureHeightInMacroblocks
	if macroblockIndex >= pictureSize || len(sliceIDs) != pictureSize {
		return LumaDeblockingNeighbors{}, ErrLumaDeblockingMacroblockAddress
	}
	neighbors := LumaDeblockingNeighbors{LeftIndex: -1, TopIndex: -1}
	column := macroblockIndex % pictureWidthInMacroblocks
	if column > 0 {
		neighbors.LeftIndex = macroblockIndex - 1
		neighbors.LeftAvailable = true
		neighbors.LeftSliceBoundary = sliceIDs[neighbors.LeftIndex] != sliceIDs[macroblockIndex]
	}
	if macroblockIndex >= pictureWidthInMacroblocks {
		neighbors.TopIndex = macroblockIndex - pictureWidthInMacroblocks
		neighbors.TopAvailable = true
		neighbors.TopSliceBoundary = sliceIDs[neighbors.TopIndex] != sliceIDs[macroblockIndex]
	}
	return neighbors, nil
}

func ResolveLumaDeblockingMacroblock(macroblockIndex, pictureWidthInMacroblocks, pictureHeightInMacroblocks int, sliceIDs []uint32, macroblock LumaDeblockingMacroblock) (LumaDeblockingMacroblock, error) {
	neighbors, err := DeriveLumaDeblockingNeighbors(macroblockIndex, pictureWidthInMacroblocks, pictureHeightInMacroblocks, sliceIDs)
	if err != nil {
		return LumaDeblockingMacroblock{}, err
	}
	maxInt := int(^uint(0) >> 1)
	if pictureWidthInMacroblocks > maxInt/16 || pictureHeightInMacroblocks > maxInt/16 {
		return LumaDeblockingMacroblock{}, ErrLumaDeblockingMacroblockAddress
	}
	macroblock.X = (macroblockIndex % pictureWidthInMacroblocks) * 16
	macroblock.Y = (macroblockIndex / pictureWidthInMacroblocks) * 16
	macroblock.LeftNeighborAvailable = neighbors.LeftAvailable
	macroblock.TopNeighborAvailable = neighbors.TopAvailable
	macroblock.LeftSliceBoundary = neighbors.LeftSliceBoundary
	macroblock.TopSliceBoundary = neighbors.TopSliceBoundary
	return macroblock, nil
}

func DeriveLumaDeblockingEdgeFlags(mode LumaDeblockingMode, leftNeighborAvailable, topNeighborAvailable, leftSliceBoundary, topSliceBoundary bool) (LumaDeblockingEdgeFlags, error) {
	if mode > LumaDeblockingAllExceptSliceBoundaries {
		return LumaDeblockingEdgeFlags{}, ErrLumaDeblockingParameters
	}
	if mode == LumaDeblockingDisabled {
		return LumaDeblockingEdgeFlags{}, nil
	}
	filterLeft := leftNeighborAvailable && !(mode == LumaDeblockingAllExceptSliceBoundaries && leftSliceBoundary)
	filterTop := topNeighborAvailable && !(mode == LumaDeblockingAllExceptSliceBoundaries && topSliceBoundary)
	return LumaDeblockingEdgeFlags{FilterLeft: filterLeft, FilterTop: filterTop, FilterInternal: true}, nil
}

func ApplyLumaDeblockingMacroblock(plane []uint8, width, height, stride int, macroblock LumaDeblockingMacroblock, parameters LumaDeblockingParameters) error {
	if !validYuvPlane(plane, width, height, stride) || macroblock.X < 0 || macroblock.Y < 0 ||
		macroblock.X > width-16 || macroblock.Y > height-16 {
		return ErrLumaDeblockingPlaneLayout
	}
	if parameters.Mode > LumaDeblockingAllExceptSliceBoundaries || parameters.IndexA > 51 || parameters.IndexB > 51 {
		return ErrLumaDeblockingParameters
	}
	flags, err := DeriveLumaDeblockingEdgeFlags(parameters.Mode, macroblock.LeftNeighborAvailable, macroblock.TopNeighborAvailable, macroblock.LeftSliceBoundary, macroblock.TopSliceBoundary)
	if err != nil {
		return err
	}
	if flags.FilterLeft && macroblock.X < 4 || flags.FilterTop && macroblock.Y < 4 {
		return ErrLumaDeblockingPlaneLayout
	}
	if flags.FilterLeft {
		if err := validateLumaBoundaryStrengths(macroblock.LeftStrengths); err != nil {
			return err
		}
	}
	if flags.FilterTop {
		if err := validateLumaBoundaryStrengths(macroblock.TopStrengths); err != nil {
			return err
		}
	}
	if flags.FilterInternal {
		for _, strengths := range macroblock.VerticalInternalStrengths {
			if err := validateLumaBoundaryStrengths(strengths); err != nil {
				return err
			}
		}
		for _, strengths := range macroblock.HorizontalInternalStrengths {
			if err := validateLumaBoundaryStrengths(strengths); err != nil {
				return err
			}
		}
	}

	if parameters.Mode != LumaDeblockingDisabled {
		if flags.FilterLeft {
			if err := ApplyLumaDeblockingPlaneMacroblockEdge(plane, width, height, stride, macroblock.X, macroblock.Y, true, macroblock.LeftStrengths, parameters, macroblock.LeftSliceBoundary); err != nil {
				return err
			}
		}
		if flags.FilterInternal {
			for edge := 0; edge < 3; edge++ {
				if macroblock.TransformSize8x8 && edge != 1 {
					continue
				}
				if err := ApplyLumaDeblockingPlaneMacroblockEdge(plane, width, height, stride, macroblock.X+4*(edge+1), macroblock.Y, true, macroblock.VerticalInternalStrengths[edge], parameters, false); err != nil {
					return err
				}
			}
		}
		if flags.FilterTop {
			if err := ApplyLumaDeblockingPlaneMacroblockEdge(plane, width, height, stride, macroblock.X, macroblock.Y, false, macroblock.TopStrengths, parameters, macroblock.TopSliceBoundary); err != nil {
				return err
			}
		}
		if flags.FilterInternal {
			for edge := 0; edge < 3; edge++ {
				if macroblock.TransformSize8x8 && edge != 1 {
					continue
				}
				if err := ApplyLumaDeblockingPlaneMacroblockEdge(plane, width, height, stride, macroblock.X, macroblock.Y+4*(edge+1), false, macroblock.HorizontalInternalStrengths[edge], parameters, false); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func validateLumaBoundaryStrengths(strengths [4]uint8) error {
	for _, strength := range strengths {
		if strength > 4 {
			return ErrLumaDeblockingBoundaryStrength
		}
	}
	return nil
}

// DeriveLumaBoundaryStrength applies the progressive-frame bS decision order.
// interPredictionDiffers summarizes the reference-picture and motion-vector comparison for the two blocks.
func DeriveLumaBoundaryStrength(macroblockEdge, eitherIntra, eitherHasNonzeroCoefficients, interPredictionDiffers bool) uint8 {
	if eitherIntra {
		if macroblockEdge {
			return 4
		}
		return 3
	}
	if eitherHasNonzeroCoefficients {
		return 2
	}
	if interPredictionDiffers {
		return 1
	}
	return 0
}

// LumaInterPredictionDiffers compares reference-picture identity and motion vectors, ignoring list order.
func LumaInterPredictionDiffers(p, q []LumaPredictionVector) (bool, error) {
	if len(p) > 2 || len(q) > 2 {
		return false, ErrLumaInterPrediction
	}
	if len(p) != len(q) {
		return true, nil
	}
	if len(p) == 0 {
		return false, nil
	}
	if len(p) == 1 {
		return !lumaPredictionVectorMatches(p[0], q[0]), nil
	}
	return !(lumaPredictionVectorMatches(p[0], q[0]) && lumaPredictionVectorMatches(p[1], q[1]) ||
		lumaPredictionVectorMatches(p[0], q[1]) && lumaPredictionVectorMatches(p[1], q[0])), nil
}

// DeriveLumaBoundaryStrengthFromPredictions combines edge facts with reference/MV comparison.
func DeriveLumaBoundaryStrengthFromPredictions(macroblockEdge, eitherIntra, eitherHasNonzeroCoefficients bool, p, q []LumaPredictionVector) (uint8, error) {
	if eitherIntra || eitherHasNonzeroCoefficients {
		return DeriveLumaBoundaryStrength(macroblockEdge, eitherIntra, eitherHasNonzeroCoefficients, false), nil
	}
	differs, err := LumaInterPredictionDiffers(p, q)
	if err != nil {
		return 0, err
	}
	return DeriveLumaBoundaryStrength(macroblockEdge, false, false, differs), nil
}

func lumaPredictionVectorMatches(p, q LumaPredictionVector) bool {
	if p.ReferencePictureID != q.ReferencePictureID {
		return false
	}
	return absLumaMotionDifference(p.MotionVector.X, q.MotionVector.X) < 4 &&
		absLumaMotionDifference(p.MotionVector.Y, q.MotionVector.Y) < 4
}

func absLumaMotionDifference(first, second int32) int64 {
	difference := int64(first) - int64(second)
	if difference < 0 {
		return -difference
	}
	return difference
}

func absLumaDifference(first, second uint8) uint8 {
	if first > second {
		return first - second
	}
	return second - first
}

func DeriveLumaDeblockingParameters(qPP, qPQ int, disableIDC uint8, alphaC0OffsetDiv2, betaOffsetDiv2 int64) (LumaDeblockingParameters, error) {
	if qPP < 0 || qPP > 51 || qPQ < 0 || qPQ > 51 || disableIDC > 2 ||
		alphaC0OffsetDiv2 < -6 || alphaC0OffsetDiv2 > 6 || betaOffsetDiv2 < -6 || betaOffsetDiv2 > 6 {
		return LumaDeblockingParameters{}, ErrLumaDeblockingParameters
	}
	mode := LumaDeblockingAllEdges
	switch disableIDC {
	case 1:
		mode = LumaDeblockingDisabled
	case 2:
		mode = LumaDeblockingAllExceptSliceBoundaries
	}
	qPav := (qPP + qPQ + 1) >> 1
	indexA := qPav + 2*int(alphaC0OffsetDiv2)
	indexB := qPav + 2*int(betaOffsetDiv2)
	if indexA < 0 {
		indexA = 0
	} else if indexA > 51 {
		indexA = 51
	}
	if indexB < 0 {
		indexB = 0
	} else if indexB > 51 {
		indexB = 51
	}
	return LumaDeblockingParameters{Mode: mode, IndexA: uint8(indexA), IndexB: uint8(indexB)}, nil
}

type POCType0State struct {
	PreviousPicOrderCntMSB int64
	PreviousPicOrderCntLSB uint32
}

// Calculate returns the progressive frame POC for POC type 0 and advances state for reference pictures.
func (state *POCType0State) Calculate(maxPicOrderCntLSB, picOrderCntLSB uint32, deltaPicOrderBottom int64, nalRefIDC uint8, idr bool) (int64, error) {
	if maxPicOrderCntLSB < 16 || maxPicOrderCntLSB > 1<<16 || maxPicOrderCntLSB&(maxPicOrderCntLSB-1) != 0 ||
		picOrderCntLSB >= maxPicOrderCntLSB || (!idr && state.PreviousPicOrderCntLSB >= maxPicOrderCntLSB) || nalRefIDC > 3 || (idr && nalRefIDC == 0) {
		return 0, ErrPictureOrderCount
	}

	previousMSB := state.PreviousPicOrderCntMSB
	previousLSB := state.PreviousPicOrderCntLSB
	if idr {
		previousMSB = 0
		previousLSB = 0
	}
	currentMSB := previousMSB
	halfRange := maxPicOrderCntLSB / 2
	if picOrderCntLSB < previousLSB && previousLSB-picOrderCntLSB >= halfRange {
		var ok bool
		currentMSB, ok = checkedPOCAdd(previousMSB, int64(maxPicOrderCntLSB))
		if !ok {
			return 0, ErrPictureOrderCount
		}
	} else if picOrderCntLSB > previousLSB && picOrderCntLSB-previousLSB > halfRange {
		var ok bool
		currentMSB, ok = checkedPOCAdd(previousMSB, -int64(maxPicOrderCntLSB))
		if !ok {
			return 0, ErrPictureOrderCount
		}
	}

	topFieldOrderCnt, ok := checkedPOCAdd(currentMSB, int64(picOrderCntLSB))
	if !ok {
		return 0, ErrPictureOrderCount
	}
	bottomFieldOrderCnt, ok := checkedPOCAdd(topFieldOrderCnt, deltaPicOrderBottom)
	if !ok {
		return 0, ErrPictureOrderCount
	}
	if nalRefIDC != 0 {
		state.PreviousPicOrderCntMSB = currentMSB
		state.PreviousPicOrderCntLSB = picOrderCntLSB
	}
	if bottomFieldOrderCnt < topFieldOrderCnt {
		return bottomFieldOrderCnt, nil
	}
	return topFieldOrderCnt, nil
}

func checkedPOCAdd(left, right int64) (int64, bool) {
	maxInt64 := int64(^uint64(0) >> 1)
	minInt64 := -maxInt64 - 1
	if (right > 0 && left > maxInt64-right) || (right < 0 && left < minInt64-right) {
		return 0, false
	}
	return left + right, true
}

func checkedPOCMul(left, right int64) (int64, bool) {
	maxInt64 := int64(^uint64(0) >> 1)
	minInt64 := -maxInt64 - 1
	if left == 0 || right == 0 {
		return 0, true
	}
	if (left == -1 && right == minInt64) || (right == -1 && left == minInt64) {
		return 0, false
	}
	product := left * right
	return product, product/right == left
}

type POCType12State struct {
	PreviousFrameNum       uint32
	PreviousFrameNumOffset int64
}

func (state *POCType12State) CalculateType1(maxFrameNum, frameNum uint32, deltaPicOrderCnt0, deltaPicOrderCnt1, offsetForNonRefPic, offsetForTopToBottomField int64, offsetForRefFrame []int64, nalRefIDC uint8, idr bool) (int64, error) {
	frameNumOffset, err := state.frameNumOffset(maxFrameNum, frameNum, nalRefIDC, idr)
	if err != nil || len(offsetForRefFrame) > 255 {
		return 0, ErrPictureOrderCount
	}

	absFrameNum := int64(0)
	if len(offsetForRefFrame) != 0 {
		var ok bool
		absFrameNum, ok = checkedPOCAdd(frameNumOffset, int64(frameNum))
		if !ok {
			return 0, ErrPictureOrderCount
		}
		if absFrameNum < 0 {
			return 0, ErrPictureOrderCount
		}
	}
	if nalRefIDC == 0 && absFrameNum > 0 {
		absFrameNum--
	}

	expectedDeltaPerCycle, ok := sumPOCOffsets(offsetForRefFrame)
	if !ok {
		return 0, ErrPictureOrderCount
	}
	expectedPOC := int64(0)
	if absFrameNum > 0 {
		cycleLength := int64(len(offsetForRefFrame))
		cycleCount := (absFrameNum - 1) / cycleLength
		frameNumInCycle := int((absFrameNum - 1) % cycleLength)
		expectedPOC, ok = checkedPOCMul(cycleCount, expectedDeltaPerCycle)
		if !ok {
			return 0, ErrPictureOrderCount
		}
		cycleOffset, valid := sumPOCOffsets(offsetForRefFrame[:frameNumInCycle+1])
		if !valid {
			return 0, ErrPictureOrderCount
		}
		expectedPOC, ok = checkedPOCAdd(expectedPOC, cycleOffset)
		if !ok {
			return 0, ErrPictureOrderCount
		}
	}
	if nalRefIDC == 0 {
		expectedPOC, ok = checkedPOCAdd(expectedPOC, offsetForNonRefPic)
		if !ok {
			return 0, ErrPictureOrderCount
		}
	}
	topFieldOrderCnt, ok := checkedPOCAdd(expectedPOC, deltaPicOrderCnt0)
	if !ok {
		return 0, ErrPictureOrderCount
	}
	bottomFieldOrderCnt, ok := checkedPOCAdd(topFieldOrderCnt, offsetForTopToBottomField)
	if !ok {
		return 0, ErrPictureOrderCount
	}
	bottomFieldOrderCnt, ok = checkedPOCAdd(bottomFieldOrderCnt, deltaPicOrderCnt1)
	if !ok {
		return 0, ErrPictureOrderCount
	}
	state.advance(frameNum, frameNumOffset, nalRefIDC)
	if bottomFieldOrderCnt < topFieldOrderCnt {
		return bottomFieldOrderCnt, nil
	}
	return topFieldOrderCnt, nil
}

func (state *POCType12State) CalculateType2(maxFrameNum, frameNum uint32, nalRefIDC uint8, idr bool) (int64, error) {
	frameNumOffset, err := state.frameNumOffset(maxFrameNum, frameNum, nalRefIDC, idr)
	if err != nil {
		return 0, err
	}
	pictureOrderCnt := int64(0)
	if !idr {
		absoluteFrameNum, ok := checkedPOCAdd(frameNumOffset, int64(frameNum))
		if !ok {
			return 0, ErrPictureOrderCount
		}
		pictureOrderCnt, ok = checkedPOCMul(2, absoluteFrameNum)
		if !ok {
			return 0, ErrPictureOrderCount
		}
		if nalRefIDC == 0 {
			pictureOrderCnt, ok = checkedPOCAdd(pictureOrderCnt, -1)
			if !ok {
				return 0, ErrPictureOrderCount
			}
		}
	}
	state.advance(frameNum, frameNumOffset, nalRefIDC)
	return pictureOrderCnt, nil
}

func (state *POCType12State) frameNumOffset(maxFrameNum, frameNum uint32, nalRefIDC uint8, idr bool) (int64, error) {
	if maxFrameNum < 16 || maxFrameNum > 1<<16 || maxFrameNum&(maxFrameNum-1) != 0 || frameNum >= maxFrameNum || nalRefIDC > 3 || (idr && (nalRefIDC == 0 || frameNum != 0)) {
		return 0, ErrPictureOrderCount
	}
	if idr {
		return 0, nil
	}
	if state.PreviousFrameNum >= maxFrameNum || state.PreviousFrameNumOffset < 0 {
		return 0, ErrPictureOrderCount
	}
	frameNumOffset := state.PreviousFrameNumOffset
	if state.PreviousFrameNum > frameNum {
		var ok bool
		frameNumOffset, ok = checkedPOCAdd(frameNumOffset, int64(maxFrameNum))
		if !ok {
			return 0, ErrPictureOrderCount
		}
	}
	return frameNumOffset, nil
}

func (state *POCType12State) advance(frameNum uint32, frameNumOffset int64, nalRefIDC uint8) {
	if nalRefIDC != 0 {
		state.PreviousFrameNum = frameNum
		state.PreviousFrameNumOffset = frameNumOffset
	}
}

func sumPOCOffsets(offsets []int64) (int64, bool) {
	total := int64(0)
	for _, offset := range offsets {
		var ok bool
		total, ok = checkedPOCAdd(total, offset)
		if !ok {
			return 0, false
		}
	}
	return total, true
}

func interpolateLumaHalfSampleHorizontal(samples [6]uint8) uint8 {
	return interpolateLumaHalfSample(samples)
}

func interpolateLumaHalfSampleVertical(samples [6]uint8) uint8 {
	return interpolateLumaHalfSample(samples)
}

func interpolateLumaHalfSample(samples [6]uint8) uint8 {
	var integerSamples [6]int
	for index, sample := range samples {
		integerSamples[index] = int(sample)
	}
	return clipLumaSample((filterLumaSixTap(integerSamples) + 16) >> 5)
}

func interpolateLumaHalfSampleDiagonal(samples [6][6]uint8) uint8 {
	var vertical [6]int
	for column := 0; column < len(vertical); column++ {
		vertical[column] = filterLumaSixTap([6]int{
			int(samples[0][column]),
			int(samples[1][column]),
			int(samples[2][column]),
			int(samples[3][column]),
			int(samples[4][column]),
			int(samples[5][column]),
		})
	}
	return clipLumaSample((filterLumaSixTap(vertical) + 512) >> 10)
}

func interpolateLumaQuarterSampleAverage(first, second uint8) uint8 {
	return uint8((int(first) + int(second) + 1) >> 1)
}

func interpolateLumaQuarterSamplePair(first, half, second uint8) [2]uint8 {
	return [2]uint8{
		interpolateLumaQuarterSampleAverage(first, half),
		interpolateLumaQuarterSampleAverage(half, second),
	}
}

func interpolateLumaQuarterSampleDiagonal(b, h, m, s uint8) [4]uint8 {
	return [4]uint8{
		interpolateLumaQuarterSampleAverage(b, h),
		interpolateLumaQuarterSampleAverage(b, m),
		interpolateLumaQuarterSampleAverage(h, s),
		interpolateLumaQuarterSampleAverage(m, s),
	}
}

func interpolateLumaQuarterSampleAxial(center, right, lower, horizontalHalf, verticalHalf uint8) [4]uint8 {
	return [4]uint8{
		interpolateLumaQuarterSampleAverage(center, horizontalHalf),
		interpolateLumaQuarterSampleAverage(right, horizontalHalf),
		interpolateLumaQuarterSampleAverage(center, verticalHalf),
		interpolateLumaQuarterSampleAverage(lower, verticalHalf),
	}
}

func interpolateLumaQuarterSampleAroundJ(b, h, j, m, s uint8) [4]uint8 {
	return [4]uint8{
		interpolateLumaQuarterSampleAverage(b, j),
		interpolateLumaQuarterSampleAverage(h, j),
		interpolateLumaQuarterSampleAverage(j, m),
		interpolateLumaQuarterSampleAverage(j, s),
	}
}

func interpolateLumaQuarterSampleGrid(samples [6][6]uint8) [4][4]uint8 {
	var centerColumn, rightColumn [6]uint8
	for row := 0; row < 6; row++ {
		centerColumn[row] = samples[row][2]
		rightColumn[row] = samples[row][3]
	}
	b := interpolateLumaHalfSampleHorizontal(samples[2])
	h := interpolateLumaHalfSampleVertical(centerColumn)
	j := interpolateLumaHalfSampleDiagonal(samples)
	m := interpolateLumaHalfSampleVertical(rightColumn)
	s := interpolateLumaHalfSampleHorizontal(samples[3])
	axial := interpolateLumaQuarterSampleAxial(samples[2][2], samples[2][3], samples[3][2], b, h)
	diagonal := interpolateLumaQuarterSampleDiagonal(b, h, m, s)
	aroundJ := interpolateLumaQuarterSampleAroundJ(b, h, j, m, s)
	return [4][4]uint8{
		{samples[2][2], axial[2], h, axial[3]},
		{axial[0], diagonal[0], aroundJ[1], diagonal[2]},
		{b, aroundJ[0], j, aroundJ[3]},
		{axial[1], diagonal[1], aroundJ[2], diagonal[3]},
	}
}

func gatherLumaQuarterSampleNeighborhood(plane []uint8, width, height, stride, xInt, yInt int) ([6][6]uint8, error) {
	var samples [6][6]uint8
	if width <= 0 || height <= 0 || stride < width {
		return samples, ErrLumaReferencePlaneLayout
	}
	maxInt := int(^uint(0) >> 1)
	if height-1 > (maxInt-width)/stride || len(plane) < (height-1)*stride+width {
		return samples, ErrLumaReferencePlaneLayout
	}
	for row := 0; row < 6; row++ {
		y := clampLumaReferenceCoordinate(yInt, row-2, height)
		for column := 0; column < 6; column++ {
			x := clampLumaReferenceCoordinate(xInt, column-2, width)
			samples[row][column] = plane[y*stride+x]
		}
	}
	return samples, nil
}

func clampLumaReferenceCoordinate(origin, offset, limit int) int {
	if offset < 0 && origin < -offset {
		return 0
	}
	if offset > 0 && origin >= limit-offset {
		return limit - 1
	}
	coordinate := origin + offset
	if coordinate < 0 {
		return 0
	}
	if coordinate >= limit {
		return limit - 1
	}
	return coordinate
}

func selectLumaFractionalSample(samples [4][4]uint8, xFracL, yFracL uint8) (uint8, error) {
	if xFracL > 3 || yFracL > 3 {
		return 0, ErrLumaFractionalSampleOffset
	}
	return samples[xFracL][yFracL], nil
}

func interpolateLumaFractionalSample(plane []uint8, width, height, stride, xInt, yInt int, xFracL, yFracL uint8) (uint8, error) {
	neighborhood, err := gatherLumaQuarterSampleNeighborhood(plane, width, height, stride, xInt, yInt)
	if err != nil {
		return 0, err
	}
	grid := interpolateLumaQuarterSampleGrid(neighborhood)
	return selectLumaFractionalSample(grid, xFracL, yFracL)
}

func filterLumaSixTap(samples [6]int) int {
	return samples[0] - 5*samples[1] + 20*samples[2] + 20*samples[3] - 5*samples[4] + samples[5]
}

func clipLumaSample(value int) uint8 {
	if value < 0 {
		return 0
	}
	if value > 255 {
		return 255
	}
	return uint8(value)
}

type MotionVector struct {
	X int32
	Y int32
}

type LumaPredictionVector struct {
	ReferencePictureID uint32
	MotionVector       MotionVector
}

type MotionVectorCandidate struct {
	Available bool
	RefIdx    int32
	Vector    MotionVector
}

type ReferencePicture struct {
	ID               uint32
	FrameNum         uint32
	PictureOrderCnt  int64
	IsLongTerm       bool
	LongTermFrameIdx uint32
}

type Yuv420Frame struct {
	Width   int
	Height  int
	YStride int
	UStride int
	VStride int
	Y       []uint8
	U       []uint8
	V       []uint8
}

type PresentationPicture struct {
	PictureOrderCnt int64
	Frame           Yuv420Frame
}

type PresentationOrderBuffer struct {
	maxReorderPictures uint
	pending            []PresentationPicture
}

func NewPresentationOrderBuffer(maxReorderPictures uint) *PresentationOrderBuffer {
	return &PresentationOrderBuffer{maxReorderPictures: maxReorderPictures}
}

func (buffer *PresentationOrderBuffer) Push(picture PresentationPicture) (*PresentationPicture, error) {
	frame, err := cloneYuv420Frame(picture.Frame)
	if err != nil {
		return nil, err
	}
	picture.Frame = frame
	buffer.pending = append(buffer.pending, picture)
	sort.SliceStable(buffer.pending, func(left, right int) bool {
		return buffer.pending[left].PictureOrderCnt < buffer.pending[right].PictureOrderCnt
	})
	if uint(len(buffer.pending)) <= buffer.maxReorderPictures {
		return nil, nil
	}
	ready := buffer.pending[0]
	buffer.pending = buffer.pending[1:]
	return &ready, nil
}

func (buffer *PresentationOrderBuffer) Drain() []PresentationPicture {
	sort.SliceStable(buffer.pending, func(left, right int) bool {
		return buffer.pending[left].PictureOrderCnt < buffer.pending[right].PictureOrderCnt
	})
	ready := buffer.pending
	buffer.pending = nil
	return ready
}

func cloneYuv420Frame(frame Yuv420Frame) (Yuv420Frame, error) {
	chromaWidth := frame.Width/2 + frame.Width%2
	chromaHeight := frame.Height/2 + frame.Height%2
	if !validYuvPlane(frame.Y, frame.Width, frame.Height, frame.YStride) ||
		!validYuvPlane(frame.U, chromaWidth, chromaHeight, frame.UStride) ||
		!validYuvPlane(frame.V, chromaWidth, chromaHeight, frame.VStride) {
		return Yuv420Frame{}, ErrReferencePictureFrameLayout
	}
	frame.Y = append([]uint8(nil), frame.Y...)
	frame.U = append([]uint8(nil), frame.U...)
	frame.V = append([]uint8(nil), frame.V...)
	return frame, nil
}

type DecodedReferencePicture struct {
	Reference ReferencePicture
	Frame     Yuv420Frame
}

type ReferencePictureBuffer struct {
	pictures []DecodedReferencePicture
}

var ErrReferencePictureFrameLayout = errors.New("reference picture frame layout is invalid or truncated")

func (buffer *ReferencePictureBuffer) Store(reference ReferencePicture, frame Yuv420Frame) error {
	chromaWidth := frame.Width/2 + frame.Width%2
	chromaHeight := frame.Height/2 + frame.Height%2
	if !validYuvPlane(frame.Y, frame.Width, frame.Height, frame.YStride) ||
		!validYuvPlane(frame.U, chromaWidth, chromaHeight, frame.UStride) ||
		!validYuvPlane(frame.V, chromaWidth, chromaHeight, frame.VStride) {
		return ErrReferencePictureFrameLayout
	}
	stored := DecodedReferencePicture{
		Reference: reference,
		Frame: Yuv420Frame{
			Width: frame.Width, Height: frame.Height,
			YStride: frame.YStride, UStride: frame.UStride, VStride: frame.VStride,
			Y: append([]uint8(nil), frame.Y...),
			U: append([]uint8(nil), frame.U...),
			V: append([]uint8(nil), frame.V...),
		},
	}
	for index := range buffer.pictures {
		if buffer.pictures[index].Reference.ID == reference.ID {
			buffer.pictures[index] = stored
			return nil
		}
	}
	buffer.pictures = append(buffer.pictures, stored)
	return nil
}

func (buffer *ReferencePictureBuffer) Get(identifier uint32) (DecodedReferencePicture, bool) {
	for _, picture := range buffer.pictures {
		if picture.Reference.ID == identifier {
			picture.Frame.Y = append([]uint8(nil), picture.Frame.Y...)
			picture.Frame.U = append([]uint8(nil), picture.Frame.U...)
			picture.Frame.V = append([]uint8(nil), picture.Frame.V...)
			return picture, true
		}
	}
	return DecodedReferencePicture{}, false
}

func (buffer *ReferencePictureBuffer) Remove(identifier uint32) bool {
	for index, picture := range buffer.pictures {
		if picture.Reference.ID == identifier {
			copy(buffer.pictures[index:], buffer.pictures[index+1:])
			buffer.pictures = buffer.pictures[:len(buffer.pictures)-1]
			return true
		}
	}
	return false
}

func (buffer *ReferencePictureBuffer) References() []ReferencePicture {
	references := make([]ReferencePicture, len(buffer.pictures))
	for index, picture := range buffer.pictures {
		references[index] = picture.Reference
	}
	return references
}

func validYuvPlane(plane []uint8, width, height, stride int) bool {
	if width <= 0 || height <= 0 || stride < width {
		return false
	}
	maxInt := int(^uint(0) >> 1)
	if height-1 > (maxInt-width)/stride {
		return false
	}
	return len(plane) >= (height-1)*stride+width
}

type BReferenceLists struct {
	List0 []ReferencePicture
	List1 []ReferencePicture
}

// BuildPReferenceList constructs the initial progressive-frame P-slice reference order.
func BuildPReferenceList(pictures []ReferencePicture, currentFrameNum, maximumFrameNum uint32) ([]ReferencePicture, error) {
	if maximumFrameNum == 0 || currentFrameNum >= maximumFrameNum {
		return nil, ErrReferenceFrameNum
	}
	type wrappedReference struct {
		frameNumWrap int64
		picture      ReferencePicture
	}
	shortTerm := make([]wrappedReference, 0, len(pictures))
	longTerm := make([]ReferencePicture, 0, len(pictures))
	for _, picture := range pictures {
		if picture.IsLongTerm {
			longTerm = append(longTerm, picture)
			continue
		}
		if picture.FrameNum >= maximumFrameNum {
			return nil, ErrReferenceFrameNum
		}
		frameNumWrap := int64(picture.FrameNum)
		if picture.FrameNum > currentFrameNum {
			frameNumWrap -= int64(maximumFrameNum)
		}
		shortTerm = append(shortTerm, wrappedReference{frameNumWrap: frameNumWrap, picture: picture})
	}
	sort.SliceStable(shortTerm, func(left, right int) bool {
		return shortTerm[left].frameNumWrap > shortTerm[right].frameNumWrap
	})
	sort.SliceStable(longTerm, func(left, right int) bool {
		return longTerm[left].LongTermFrameIdx < longTerm[right].LongTermFrameIdx
	})
	ordered := make([]ReferencePicture, 0, len(pictures))
	for _, reference := range shortTerm {
		ordered = append(ordered, reference.picture)
	}
	ordered = append(ordered, longTerm...)
	return ordered, nil
}

// BuildBReferenceLists constructs the initial progressive-frame B-slice lists.
func BuildBReferenceLists(pictures []ReferencePicture, currentPictureOrderCnt int64) BReferenceLists {
	beforeOrEqual := make([]ReferencePicture, 0, len(pictures))
	after := make([]ReferencePicture, 0, len(pictures))
	longTerm := make([]ReferencePicture, 0, len(pictures))
	for _, picture := range pictures {
		if picture.IsLongTerm {
			longTerm = append(longTerm, picture)
		} else if picture.PictureOrderCnt <= currentPictureOrderCnt {
			beforeOrEqual = append(beforeOrEqual, picture)
		} else {
			after = append(after, picture)
		}
	}
	sort.SliceStable(beforeOrEqual, func(left, right int) bool {
		return beforeOrEqual[left].PictureOrderCnt > beforeOrEqual[right].PictureOrderCnt
	})
	sort.SliceStable(after, func(left, right int) bool {
		return after[left].PictureOrderCnt < after[right].PictureOrderCnt
	})
	sort.SliceStable(longTerm, func(left, right int) bool {
		return longTerm[left].LongTermFrameIdx < longTerm[right].LongTermFrameIdx
	})
	list0 := make([]ReferencePicture, 0, len(pictures))
	list0 = append(list0, beforeOrEqual...)
	list0 = append(list0, after...)
	list0 = append(list0, longTerm...)
	list1 := make([]ReferencePicture, 0, len(pictures))
	list1 = append(list1, after...)
	list1 = append(list1, beforeOrEqual...)
	list1 = append(list1, longTerm...)
	identical := len(list0) == len(list1)
	for index := range list0 {
		if list0[index].ID != list1[index].ID {
			identical = false
			break
		}
	}
	if identical && len(list1) > 1 {
		list1[0], list1[1] = list1[1], list1[0]
	}
	return BReferenceLists{List0: list0, List1: list1}
}

// ApplyReferenceListModifications applies parsed frame-coded list commands to an initialized list.
func ApplyReferenceListModifications(initial, references []ReferencePicture, currentFrameNum, maximumFrameNum uint32, modifications []RefPicListModification) ([]ReferencePicture, error) {
	if maximumFrameNum == 0 || currentFrameNum >= maximumFrameNum {
		return nil, ErrReferenceFrameNum
	}
	activeCount := len(initial)
	result := append([]ReferencePicture(nil), initial...)
	picNumPred := uint64(currentFrameNum)
	maxPicNum := uint64(maximumFrameNum)
	refIndex := 0
	for _, modification := range modifications {
		if refIndex >= activeCount {
			return nil, ErrReferenceListModification
		}
		var target ReferencePicture
		found := false
		switch modification.ModificationOfPicNumsIDC {
		case 0, 1:
			absDiffPicNum := uint64(modification.Value) + 1
			if absDiffPicNum > maxPicNum {
				return nil, ErrReferenceListModification
			}
			if modification.ModificationOfPicNumsIDC == 0 {
				picNumPred = (picNumPred + maxPicNum - absDiffPicNum) % maxPicNum
			} else {
				picNumPred = (picNumPred + absDiffPicNum) % maxPicNum
			}
			frameNumWrap := int64(picNumPred)
			if picNumPred > uint64(currentFrameNum) {
				frameNumWrap -= int64(maxPicNum)
			}
			for _, picture := range references {
				if picture.IsLongTerm || picture.FrameNum >= maximumFrameNum {
					continue
				}
				candidateWrap := int64(picture.FrameNum)
				if picture.FrameNum > currentFrameNum {
					candidateWrap -= int64(maximumFrameNum)
				}
				if candidateWrap == frameNumWrap {
					target, found = picture, true
					break
				}
			}
		case 2:
			for _, picture := range references {
				if picture.IsLongTerm && picture.LongTermFrameIdx == modification.Value {
					target, found = picture, true
					break
				}
			}
		default:
			return nil, ErrReferenceListModification
		}
		if !found {
			return nil, ErrReferenceListModification
		}
		for index := refIndex; index < len(result); index++ {
			if result[index].ID == target.ID {
				result = append(result[:index], result[index+1:]...)
				break
			}
		}
		result = append(result, ReferencePicture{})
		copy(result[refIndex+1:], result[refIndex:len(result)-1])
		result[refIndex] = target
		result = result[:activeCount]
		refIndex++
	}
	return result, nil
}

// MotionVectorDifferenceNeighborMagnitudes sums absolute left/top MVD components, treating absent neighbors as zero.
func MotionVectorDifferenceNeighborMagnitudes(left, top *MotionVector) (uint64, uint64) {
	var leftX, leftY, topX, topY int32
	if left != nil {
		leftX, leftY = left.X, left.Y
	}
	if top != nil {
		topX, topY = top.X, top.Y
	}
	return absoluteMotionVectorComponent(leftX) + absoluteMotionVectorComponent(topX),
		absoluteMotionVectorComponent(leftY) + absoluteMotionVectorComponent(topY)
}

func absoluteMotionVectorComponent(component int32) uint64 {
	value := int64(component)
	if value < 0 {
		return uint64(-value)
	}
	return uint64(value)
}

type MotionVectorPartitionShape uint8

const (
	MotionVectorPartitionOther MotionVectorPartitionShape = iota
	MotionVectorPartition16x8
	MotionVectorPartition8x16
)

// PredictMotionVectorForPartition applies H.264's shape-specific reference matches before the median rule.
func PredictMotionVectorForPartition(shape MotionVectorPartitionShape, currentRefIdx int32, left, top, topRight, topLeft MotionVectorCandidate) MotionVector {
	if currentRefIdx >= 0 {
		topRightCandidate := topRight
		if !topRightCandidate.Available {
			topRightCandidate = topLeft
		}
		switch shape {
		case MotionVectorPartition16x8:
			if left.Available && left.RefIdx == currentRefIdx {
				return left.Vector
			}
			if top.Available && top.RefIdx == currentRefIdx {
				return top.Vector
			}
		case MotionVectorPartition8x16:
			if left.Available && left.RefIdx == currentRefIdx {
				return left.Vector
			}
			if topRightCandidate.Available && topRightCandidate.RefIdx == currentRefIdx {
				return topRightCandidate.Vector
			}
		}
	}
	return PredictMotionVector(currentRefIdx, left, top, topRight, topLeft)
}

// PredictMotionVector selects the unique matching reference vector or component median.
func PredictMotionVector(currentRefIdx int32, left, top, topRight, topLeft MotionVectorCandidate) MotionVector {
	if !topRight.Available {
		topRight = topLeft
	}

	candidates := [3]MotionVectorCandidate{left, top, topRight}
	matchingCount := 0
	var matching MotionVector
	var vectors [3]MotionVector
	for index, candidate := range candidates {
		if !candidate.Available || candidate.RefIdx < 0 {
			continue
		}
		vectors[index] = candidate.Vector
		if candidate.RefIdx == currentRefIdx {
			matchingCount++
			matching = candidate.Vector
		}
	}
	if matchingCount == 1 {
		return matching
	}
	return MotionVector{
		X: medianMotionComponent(vectors[0].X, vectors[1].X, vectors[2].X),
		Y: medianMotionComponent(vectors[0].Y, vectors[1].Y, vectors[2].Y),
	}
}

func medianMotionComponent(first, second, third int32) int32 {
	if first > second {
		first, second = second, first
	}
	if second > third {
		second, third = third, second
	}
	if first > second {
		first, second = second, first
	}
	return second
}

// ApplyMotionVectorDifference adds a decoded difference and wraps each component to signed 16-bit range.
func ApplyMotionVectorDifference(predicted, difference MotionVector) MotionVector {
	return MotionVector{
		X: wrapMotionVectorComponent(int64(predicted.X) + int64(difference.X)),
		Y: wrapMotionVectorComponent(int64(predicted.Y) + int64(difference.Y)),
	}
}

// DeriveMotionVector predicts a partition vector, adds its decoded difference, and wraps components.
func DeriveMotionVector(shape MotionVectorPartitionShape, currentRefIdx int32, left, top, topRight, topLeft MotionVectorCandidate, difference MotionVector) MotionVector {
	predicted := PredictMotionVectorForPartition(shape, currentRefIdx, left, top, topRight, topLeft)
	return ApplyMotionVectorDifference(predicted, difference)
}

func wrapMotionVectorComponent(value int64) int32 {
	value = (value + 1<<15) % (1 << 16)
	if value < 0 {
		value += 1 << 16
	}
	return int32(value - (1 << 15))
}

var (
	ErrInverseScaleQPYOutOfRange      = errors.New("inverse scaling QPY is outside [0,51]")
	ErrInverseScaleQPCOutOfRange      = errors.New("inverse scaling QPC is outside [0,39]")
	ErrInverseScaleListZero           = errors.New("inverse scaling list contains zero")
	ErrInverseScaleChromaDCOutOfRange = errors.New("chroma DC inverse-scaling value is outside the 8-bit 4:2:0 range")
	ErrInverseScaleChromaBlockRange   = errors.New("chroma block inverse-scaling value is outside the 8-bit 4:2:0 range")
	ErrChromaQPYOutOfRange            = errors.New("chroma QP luma input is outside [0,51]")
	ErrChromaQPIndexOffsetOutOfRange  = errors.New("chroma QP index offset is outside [-12,12]")
)

var inverseScale4x4Factors = [6][3]int64{
	{10, 13, 16},
	{11, 14, 18},
	{13, 16, 20},
	{14, 18, 23},
	{16, 20, 25},
	{18, 23, 29},
}

var chromaQPCFromQPI = [22]uint8{
	29, 30, 31, 32, 32, 33, 34, 34, 35, 35, 36,
	36, 37, 37, 37, 38, 38, 38, 38, 39, 39, 39,
}

var inverseScale8x8Factors = [6][6]int64{
	{20, 18, 32, 19, 25, 24},
	{22, 19, 35, 21, 28, 26},
	{26, 23, 42, 24, 33, 31},
	{28, 25, 45, 26, 35, 33},
	{32, 28, 51, 30, 40, 38},
	{36, 32, 58, 34, 46, 43},
}

var inverseScale8x8Classes = [64]uint8{
	0, 3, 4, 3, 0, 3, 4, 3,
	3, 1, 5, 1, 3, 1, 5, 1,
	4, 5, 2, 5, 4, 5, 2, 5,
	3, 1, 5, 1, 3, 1, 5, 1,
	0, 3, 4, 3, 0, 3, 4, 3,
	3, 1, 5, 1, 3, 1, 5, 1,
	4, 5, 2, 5, 4, 5, 2, 5,
	3, 1, 5, 1, 3, 1, 5, 1,
}

func inverseScaleLuma4x4(levels [16]int32, scalingList [16]uint8, qpy int) ([16]int64, error) {
	var scaled [16]int64
	if qpy < 0 || qpy > 51 {
		return scaled, ErrInverseScaleQPYOutOfRange
	}
	for index, weight := range scalingList {
		if weight == 0 {
			return scaled, ErrInverseScaleListZero
		}
		row, column := index/4, index%4
		factorClass := row%2 + column%2
		value := int64(levels[index]) * inverseScale4x4Factors[qpy%6][factorClass] * int64(weight)
		if qpy >= 24 {
			scaled[index] = value << (qpy/6 - 4)
		} else {
			shift := 4 - qpy/6
			rounding := int64(1) << (shift - 1)
			scaled[index] = (value + rounding) >> shift
		}
	}
	return scaled, nil
}

func inverseScaleChromaDC2x2(transformed [4]int64, qpc int) ([4]int64, error) {
	var scaled [4]int64
	if qpc < 0 || qpc > 39 {
		return scaled, ErrInverseScaleQPCOutOfRange
	}
	for _, coefficient := range transformed {
		if coefficient < -(1<<15) || coefficient > (1<<15)-1 {
			return scaled, ErrInverseScaleChromaDCOutOfRange
		}
	}
	factor := inverseScale4x4Factors[qpc%6][0]
	shift := qpc / 6
	for index, coefficient := range transformed {
		scaled[index] = (coefficient * factor << shift) >> 5
		if scaled[index] < -(1<<15) || scaled[index] > (1<<15)-1 {
			return [4]int64{}, ErrInverseScaleChromaDCOutOfRange
		}
	}
	return scaled, nil
}

func inverseScaleChroma4x4(levels [16]int32, scalingList [16]uint8, qpc int) ([16]int64, error) {
	var scaled [16]int64
	if qpc < 0 || qpc > 39 {
		return scaled, ErrInverseScaleQPCOutOfRange
	}
	for _, level := range levels {
		if level < -(1<<15) || level > (1<<15)-1 {
			return scaled, ErrInverseScaleChromaBlockRange
		}
	}
	for _, weight := range scalingList {
		if weight == 0 {
			return scaled, ErrInverseScaleListZero
		}
	}

	scaled[0] = int64(levels[0])
	for index := 1; index < len(levels); index++ {
		row, column := index/4, index%4
		factorClass := row%2 + column%2
		value := int64(levels[index]) * inverseScale4x4Factors[qpc%6][factorClass] * int64(scalingList[index])
		if qpc >= 24 {
			scaled[index] = value << (qpc/6 - 4)
		} else {
			shift := 4 - qpc/6
			rounding := int64(1) << (shift - 1)
			scaled[index] = (value + rounding) >> shift
		}
		if scaled[index] < -(1<<15) || scaled[index] > (1<<15)-1 {
			return [16]int64{}, ErrInverseScaleChromaBlockRange
		}
	}
	return scaled, nil
}

func reconstructChroma4x4Residual(dcC int64, acScanLevels [15]int32, scalingList [16]uint8, qpc int) ([16]int64, error) {
	if qpc < 0 || qpc > 39 {
		return [16]int64{}, ErrInverseScaleQPCOutOfRange
	}
	if dcC < -(1<<15) || dcC > (1<<15)-1 {
		return [16]int64{}, ErrInverseScaleChromaBlockRange
	}
	levels := PlaceChroma4x4ScanLevels(int32(dcC), acScanLevels)
	scaled, err := inverseScaleChroma4x4(levels, scalingList, qpc)
	if err != nil {
		return [16]int64{}, err
	}
	return inverseTransformLuma4x4(scaled), nil
}

func assembleChroma420ResidualMacroblock(blocks [4][16]int64) (macroblock [64]int64) {
	for blockIndex, block := range blocks {
		xOffset := blockIndex % 2 * 4
		yOffset := blockIndex / 2 * 4
		for row := 0; row < 4; row++ {
			destination := (yOffset+row)*8 + xOffset
			copy(macroblock[destination:destination+4], block[row*4:row*4+4])
		}
	}
	return macroblock
}

func reconstructChroma420Macroblock(prediction [64]uint8, residual [64]int64) (samples [64]uint8) {
	for index, predictedSample := range prediction {
		value := int64(predictedSample) + residual[index]
		if value < 0 {
			value = 0
		} else if value > 255 {
			value = 255
		}
		samples[index] = uint8(value)
	}
	return samples
}

func deriveChromaQPC(qpy, qpOffset int) (int, error) {
	if qpy < 0 || qpy > 51 {
		return 0, ErrChromaQPYOutOfRange
	}
	if qpOffset < -12 || qpOffset > 12 {
		return 0, ErrChromaQPIndexOffsetOutOfRange
	}
	qpi := qpy + qpOffset
	if qpi < 0 {
		qpi = 0
	} else if qpi > 51 {
		qpi = 51
	}
	if qpi < 30 {
		return qpi, nil
	}
	return int(chromaQPCFromQPI[qpi-30]), nil
}

func inverseScaleLuma8x8(levels [64]int32, scalingList [64]uint8, qpy int) ([64]int64, error) {
	var scaled [64]int64
	if qpy < 0 || qpy > 51 {
		return scaled, ErrInverseScaleQPYOutOfRange
	}
	for index, weight := range scalingList {
		if weight == 0 {
			return scaled, ErrInverseScaleListZero
		}
		factorClass := inverseScale8x8Classes[index]
		value := int64(levels[index]) * inverseScale8x8Factors[qpy%6][factorClass] * int64(weight)
		if qpy >= 24 {
			scaled[index] = value << (qpy/6 - 4)
		} else {
			shift := 4 - qpy/6
			rounding := int64(1) << (shift - 1)
			scaled[index] = (value + rounding) >> shift
		}
	}
	return scaled, nil
}

func inverseTransformLuma4x4(coefficients [16]int64) (residual [16]int64) {
	var horizontal [16]int64
	for row := 0; row < 4; row++ {
		rowOffset := row * 4
		transformed := inverseTransform4x4Line([4]int64{
			coefficients[rowOffset],
			coefficients[rowOffset+1],
			coefficients[rowOffset+2],
			coefficients[rowOffset+3],
		})
		copy(horizontal[rowOffset:rowOffset+4], transformed[:])
	}
	for column := 0; column < 4; column++ {
		transformed := inverseTransform4x4Line([4]int64{
			horizontal[column],
			horizontal[4+column],
			horizontal[8+column],
			horizontal[12+column],
		})
		for row, value := range transformed {
			residual[row*4+column] = (value + 32) >> 6
		}
	}
	return residual
}

func inverseTransformChromaDC2x2(coefficients [4]int32) (transformed [4]int64) {
	c00, c01 := int64(coefficients[0]), int64(coefficients[1])
	c10, c11 := int64(coefficients[2]), int64(coefficients[3])
	transformed[0] = c00 + c01 + c10 + c11
	transformed[1] = c00 - c01 + c10 - c11
	transformed[2] = c00 + c01 - c10 - c11
	transformed[3] = c00 - c01 - c10 + c11
	return transformed
}

func inverseTransform4x4Line(coefficients [4]int64) (samples [4]int64) {
	evenSum := coefficients[0] + coefficients[2]
	evenDifference := coefficients[0] - coefficients[2]
	oddDifference := (coefficients[1] >> 1) - coefficients[3]
	oddSum := coefficients[1] + (coefficients[3] >> 1)
	samples[0] = evenSum + oddSum
	samples[1] = evenDifference + oddDifference
	samples[2] = evenDifference - oddDifference
	samples[3] = evenSum - oddSum
	return samples
}

func inverseTransformLuma8x8(coefficients [64]int64) (residual [64]int64) {
	var horizontal [64]int64
	for row := 0; row < 8; row++ {
		rowOffset := row * 8
		transformed := inverseTransform8x8Line([8]int64{
			coefficients[rowOffset],
			coefficients[rowOffset+1],
			coefficients[rowOffset+2],
			coefficients[rowOffset+3],
			coefficients[rowOffset+4],
			coefficients[rowOffset+5],
			coefficients[rowOffset+6],
			coefficients[rowOffset+7],
		})
		copy(horizontal[rowOffset:rowOffset+8], transformed[:])
	}
	for column := 0; column < 8; column++ {
		transformed := inverseTransform8x8Line([8]int64{
			horizontal[column],
			horizontal[8+column],
			horizontal[16+column],
			horizontal[24+column],
			horizontal[32+column],
			horizontal[40+column],
			horizontal[48+column],
			horizontal[56+column],
		})
		for row, value := range transformed {
			residual[row*8+column] = (value + 32) >> 6
		}
	}
	return residual
}

func inverseTransform8x8Line(coefficients [8]int64) (samples [8]int64) {
	a0 := coefficients[0] + coefficients[4]
	a2 := coefficients[0] - coefficients[4]
	a4 := (coefficients[2] >> 1) - coefficients[6]
	a6 := coefficients[2] + (coefficients[6] >> 1)
	b0 := a0 + a6
	b2 := a2 + a4
	b4 := a2 - a4
	b6 := a0 - a6

	a1 := -coefficients[3] + coefficients[5] - coefficients[7] - (coefficients[7] >> 1)
	a3 := coefficients[1] + coefficients[7] - coefficients[3] - (coefficients[3] >> 1)
	a5 := -coefficients[1] + coefficients[7] + coefficients[5] + (coefficients[5] >> 1)
	a7 := coefficients[3] + coefficients[5] + coefficients[1] + (coefficients[1] >> 1)
	b1 := a1 + (a7 >> 2)
	b3 := a3 + (a5 >> 2)
	b5 := a5 - (a3 >> 2)
	b7 := a7 - (a1 >> 2)

	samples = [8]int64{
		b0 + b7,
		b2 + b5,
		b4 + b3,
		b6 + b1,
		b6 - b1,
		b4 - b3,
		b2 - b5,
		b0 - b7,
	}
	return samples
}

func predictLumaIntra8x8Vertical(top [8]uint8) (prediction [64]uint8) {
	for row := 0; row < 8; row++ {
		copy(prediction[row*8:row*8+8], top[:])
	}
	return prediction
}

func predictLumaIntra16x16Vertical(top [16]uint8) (prediction [256]uint8) {
	for row := 0; row < 16; row++ {
		copy(prediction[row*16:row*16+16], top[:])
	}
	return prediction
}

func predictLumaIntra16x16Horizontal(left [16]uint8) (prediction [256]uint8) {
	for row, sample := range left {
		for column := 0; column < 16; column++ {
			prediction[row*16+column] = sample
		}
	}
	return prediction
}

func predictLumaIntra16x16DC(top, left *[16]uint8) (prediction [256]uint8) {
	dcValue := 128
	switch {
	case top != nil && left != nil:
		sum := 0
		for index := 0; index < 16; index++ {
			sum += int(top[index]) + int(left[index])
		}
		dcValue = (sum + 16) >> 5
	case top != nil:
		sum := 0
		for _, sample := range top {
			sum += int(sample)
		}
		dcValue = (sum + 8) >> 4
	case left != nil:
		sum := 0
		for _, sample := range left {
			sum += int(sample)
		}
		dcValue = (sum + 8) >> 4
	}
	for index := range prediction {
		prediction[index] = uint8(dcValue)
	}
	return prediction
}

func predictLumaIntra16x16Plane(top [16]uint8, left [16]uint8, topLeft uint8) (prediction [256]uint8) {
	horizontalGradient := 0
	verticalGradient := 0
	for index := 1; index <= 7; index++ {
		horizontalGradient += index * (int(top[7+index]) - int(top[7-index]))
		verticalGradient += index * (int(left[7+index]) - int(left[7-index]))
	}
	horizontalGradient += 8 * (int(top[15]) - int(topLeft))
	verticalGradient += 8 * (int(left[15]) - int(topLeft))
	a := 16 * (int(top[15]) + int(left[15]))
	b := (5*horizontalGradient + 32) >> 6
	c := (5*verticalGradient + 32) >> 6
	for row := 0; row < 16; row++ {
		for column := 0; column < 16; column++ {
			value := (a + b*(column-7) + c*(row-7) + 16) >> 5
			if value < 0 {
				value = 0
			} else if value > 255 {
				value = 255
			}
			prediction[row*16+column] = uint8(value)
		}
	}
	return prediction
}

func predictLumaIntra8x8Horizontal(left [8]uint8) (prediction [64]uint8) {
	for row, sample := range left {
		for column := 0; column < 8; column++ {
			prediction[row*8+column] = sample
		}
	}
	return prediction
}

func predictLumaIntra8x8DC(top, left *[8]uint8) (prediction [64]uint8) {
	dcValue := 128
	switch {
	case top != nil && left != nil:
		sum := 0
		for index := 0; index < 8; index++ {
			sum += int(top[index]) + int(left[index])
		}
		dcValue = (sum + 8) >> 4
	case top != nil:
		sum := 0
		for _, sample := range top {
			sum += int(sample)
		}
		dcValue = (sum + 4) >> 3
	case left != nil:
		sum := 0
		for _, sample := range left {
			sum += int(sample)
		}
		dcValue = (sum + 4) >> 3
	}
	for index := range prediction {
		prediction[index] = uint8(dcValue)
	}
	return prediction
}

func predictChromaIntra8x8DC(top, left *[8]uint8) (prediction [64]uint8) {
	averageEdge := func(samples *[8]uint8, offset int) int {
		sum := 0
		for index := offset; index < offset+4; index++ {
			sum += int(samples[index])
		}
		return (sum + 2) >> 2
	}
	for blockRow := 0; blockRow < 2; blockRow++ {
		for blockColumn := 0; blockColumn < 2; blockColumn++ {
			topOffset, leftOffset := blockColumn*4, blockRow*4
			dcValue := 128
			switch {
			case blockColumn == 1 && blockRow == 0:
				if top != nil {
					dcValue = averageEdge(top, topOffset)
				} else if left != nil {
					dcValue = averageEdge(left, leftOffset)
				}
			case blockColumn == 0 && blockRow == 1:
				if left != nil {
					dcValue = averageEdge(left, leftOffset)
				} else if top != nil {
					dcValue = averageEdge(top, topOffset)
				}
			default:
				switch {
				case top != nil && left != nil:
					sum := 0
					for index := 0; index < 4; index++ {
						sum += int(top[topOffset+index]) + int(left[leftOffset+index])
					}
					dcValue = (sum + 4) >> 3
				case left != nil:
					dcValue = averageEdge(left, leftOffset)
				case top != nil:
					dcValue = averageEdge(top, topOffset)
				}
			}
			for row := 0; row < 4; row++ {
				for column := 0; column < 4; column++ {
					prediction[(blockRow*4+row)*8+blockColumn*4+column] = uint8(dcValue)
				}
			}
		}
	}
	return prediction
}

func predictChromaIntra8x8Horizontal(left [8]uint8) (prediction [64]uint8) {
	for row, sample := range left {
		for column := 0; column < 8; column++ {
			prediction[row*8+column] = sample
		}
	}
	return prediction
}

func predictChromaIntra8x8Vertical(top [8]uint8) (prediction [64]uint8) {
	for row := 0; row < 8; row++ {
		copy(prediction[row*8:row*8+8], top[:])
	}
	return prediction
}

func predictChromaIntra8x8Plane(top, left [8]uint8, topLeft uint8) (prediction [64]uint8) {
	horizontalGradient := 0
	verticalGradient := 0
	for index := 0; index < 4; index++ {
		topReference := int(topLeft)
		leftReference := int(topLeft)
		if index < 3 {
			topReference = int(top[2-index])
			leftReference = int(left[2-index])
		}
		horizontalGradient += (index + 1) * (int(top[4+index]) - topReference)
		verticalGradient += (index + 1) * (int(left[4+index]) - leftReference)
	}
	a := 16 * (int(top[7]) + int(left[7]))
	b := (34*horizontalGradient + 32) >> 6
	c := (34*verticalGradient + 32) >> 6
	for row := 0; row < 8; row++ {
		for column := 0; column < 8; column++ {
			value := (a + b*(column-3) + c*(row-3) + 16) >> 5
			if value < 0 {
				value = 0
			} else if value > 255 {
				value = 255
			}
			prediction[row*8+column] = uint8(value)
		}
	}
	return prediction
}

func predictChromaIntra8x8(mode uint8, top, left *[8]uint8, topLeft *uint8) ([64]uint8, error) {
	switch mode {
	case 0:
		return predictChromaIntra8x8DC(top, left), nil
	case 1:
		if left == nil {
			return [64]uint8{}, ErrChromaIntraPredictionReference
		}
		return predictChromaIntra8x8Horizontal(*left), nil
	case 2:
		if top == nil {
			return [64]uint8{}, ErrChromaIntraPredictionReference
		}
		return predictChromaIntra8x8Vertical(*top), nil
	case 3:
		if top == nil || left == nil || topLeft == nil {
			return [64]uint8{}, ErrChromaIntraPredictionReference
		}
		return predictChromaIntra8x8Plane(*top, *left, *topLeft), nil
	default:
		return [64]uint8{}, ErrChromaIntraPredictionMode
	}
}

func predictLumaIntra8x8DiagonalDownLeft(top [16]uint8) (prediction [64]uint8) {
	for row := 0; row < 8; row++ {
		for column := 0; column < 8; column++ {
			index := row + column
			lastIndex := index + 2
			if lastIndex >= len(top) {
				lastIndex = len(top) - 1
			}
			value := (int(top[index]) + 2*int(top[index+1]) + int(top[lastIndex]) + 2) >> 2
			prediction[row*8+column] = uint8(value)
		}
	}
	return prediction
}

func predictLumaIntra4x4Vertical(top [4]uint8) (prediction [16]uint8) {
	for row := 0; row < 4; row++ {
		copy(prediction[row*4:row*4+4], top[:])
	}
	return prediction
}

func predictLumaIntra4x4Horizontal(left [4]uint8) (prediction [16]uint8) {
	for row, sample := range left {
		for column := 0; column < 4; column++ {
			prediction[row*4+column] = sample
		}
	}
	return prediction
}

func predictLumaIntra4x4DC(top, left *[4]uint8) (prediction [16]uint8) {
	dcValue := 128
	switch {
	case top != nil && left != nil:
		sum := 0
		for index := 0; index < 4; index++ {
			sum += int(top[index]) + int(left[index])
		}
		dcValue = (sum + 4) >> 3
	case top != nil:
		sum := 0
		for _, sample := range top {
			sum += int(sample)
		}
		dcValue = (sum + 2) >> 2
	case left != nil:
		sum := 0
		for _, sample := range left {
			sum += int(sample)
		}
		dcValue = (sum + 2) >> 2
	}
	for index := range prediction {
		prediction[index] = uint8(dcValue)
	}
	return prediction
}

func predictLumaIntra4x4DiagonalDownLeft(top [8]uint8) (prediction [16]uint8) {
	for row := 0; row < 4; row++ {
		for column := 0; column < 4; column++ {
			index := row + column
			nextIndex := index + 1
			lastIndex := index + 2
			if lastIndex >= len(top) {
				lastIndex = len(top) - 1
			}
			value := (int(top[index]) + 2*int(top[nextIndex]) + int(top[lastIndex]) + 2) >> 2
			prediction[row*4+column] = uint8(value)
		}
	}
	return prediction
}

func predictLumaIntra4x4DiagonalDownRight(top [8]uint8, left [4]uint8, topLeft uint8) (prediction [16]uint8) {
	referenceAt := func(position int) int {
		switch {
		case position == -1:
			return int(topLeft)
		case position < -1:
			return int(left[-position-2])
		default:
			return int(top[position])
		}
	}
	for row := 0; row < 4; row++ {
		for column := 0; column < 4; column++ {
			position := column - row
			value := (referenceAt(position-1) + 2*referenceAt(position) + referenceAt(position+1) + 2) >> 2
			prediction[row*4+column] = uint8(value)
		}
	}
	return prediction
}

func predictLumaIntra8x8DiagonalDownRight(top [16]uint8, left [8]uint8, topLeft uint8) (prediction [64]uint8) {
	referenceAt := func(position int) int {
		switch {
		case position == -1:
			return int(topLeft)
		case position < -1:
			return int(left[-position-2])
		default:
			return int(top[position])
		}
	}
	for row := 0; row < 8; row++ {
		for column := 0; column < 8; column++ {
			position := column - row
			value := (referenceAt(position-1) + 2*referenceAt(position) + referenceAt(position+1) + 2) >> 2
			prediction[row*8+column] = uint8(value)
		}
	}
	return prediction
}

func predictLumaIntra8x8VerticalRight(top [16]uint8, left [8]uint8, topLeft uint8) (prediction [64]uint8) {
	topReferenceAt := func(position int) int {
		if position == -1 {
			return int(topLeft)
		}
		return int(top[position])
	}
	for row := 0; row < 8; row++ {
		for column := 0; column < 8; column++ {
			phase := 2*column - row
			var value int
			switch {
			case phase >= 0 && phase%2 == 0:
				center := phase / 2
				value = (topReferenceAt(center-1) + 2*topReferenceAt(center) + topReferenceAt(center+1) + 2) >> 2
			case phase >= 0:
				center := (phase - 1) / 2
				value = (topReferenceAt(center-1) + topReferenceAt(center) + 1) >> 1
			case phase == -1:
				value = (int(left[0]) + int(topLeft) + 1) >> 1
			case phase == -2:
				value = (int(left[0]) + 2*int(topLeft) + int(top[0]) + 2) >> 2
			case -phase%2 == 1:
				center := (-phase - 3) / 2
				value = (int(left[center]) + int(left[center+1]) + 1) >> 1
			default:
				center := (-phase - 4) / 2
				value = (int(left[center]) + 2*int(left[center+1]) + int(left[center+2]) + 2) >> 2
			}
			prediction[row*8+column] = uint8(value)
		}
	}
	return prediction
}

func predictLumaIntra8x8HorizontalDown(top [8]uint8, left [16]uint8, topLeft uint8) (prediction [64]uint8) {
	verticalPrediction := predictLumaIntra8x8VerticalRight(left, top, topLeft)
	for row := 0; row < 8; row++ {
		for column := 0; column < 8; column++ {
			prediction[row*8+column] = verticalPrediction[column*8+row]
		}
	}
	return prediction
}

func predictLumaIntra8x8VerticalLeft(top [16]uint8) (prediction [64]uint8) {
	for row := 0; row < 8; row++ {
		for column := 0; column < 8; column++ {
			phase := 2*column + row
			var value int
			if phase%2 == 0 {
				center := phase / 2
				value = (int(top[center]) + int(top[center+1]) + 1) >> 1
			} else {
				center := (phase - 1) / 2
				value = (int(top[center]) + 2*int(top[center+1]) + int(top[center+2]) + 2) >> 2
			}
			prediction[row*8+column] = uint8(value)
		}
	}
	return prediction
}

func predictLumaIntra8x8HorizontalUp(left [16]uint8) (prediction [64]uint8) {
	verticalPrediction := predictLumaIntra8x8VerticalLeft(left)
	for row := 0; row < 8; row++ {
		for column := 0; column < 8; column++ {
			prediction[row*8+column] = verticalPrediction[column*8+row]
		}
	}
	return prediction
}

func predictLumaIntra8x8Plane(top, left [16]uint8) (prediction [64]uint8) {
	horizontalGradient := 0
	verticalGradient := 0
	for index := 1; index <= 4; index++ {
		horizontalGradient += index * (int(top[4+index]) - int(top[4-index]))
		verticalGradient += index * (int(left[4+index]) - int(left[4-index]))
	}
	a := 16 * (int(top[8]) + int(left[8]))
	b := (17*horizontalGradient + 16) >> 5
	c := (17*verticalGradient + 16) >> 5
	for row := 0; row < 8; row++ {
		for column := 0; column < 8; column++ {
			value := (a + b*(column-3) + c*(row-3) + 16) >> 5
			if value < 0 {
				value = 0
			} else if value > 255 {
				value = 255
			}
			prediction[row*8+column] = uint8(value)
		}
	}
	return prediction
}

func predictLumaIntra4x4VerticalRight(top [8]uint8, left [4]uint8, topLeft uint8) (prediction [16]uint8) {
	topReferenceAt := func(position int) int {
		if position == -1 {
			return int(topLeft)
		}
		return int(top[position])
	}
	for row := 0; row < 4; row++ {
		for column := 0; column < 4; column++ {
			phase := 2*column - row
			var value int
			switch {
			case phase >= 0 && phase%2 == 0:
				center := phase / 2
				value = (topReferenceAt(center-1) + 2*topReferenceAt(center) + topReferenceAt(center+1) + 2) >> 2
			case phase >= 0:
				center := (phase - 1) / 2
				value = (topReferenceAt(center-1) + topReferenceAt(center) + 1) >> 1
			case phase == -1:
				value = (int(left[0]) + int(topLeft) + 1) >> 1
			case phase == -2:
				value = (int(left[0]) + 2*int(topLeft) + int(top[0]) + 2) >> 2
			default:
				value = (int(left[0]) + int(left[1]) + 1) >> 1
			}
			prediction[row*4+column] = uint8(value)
		}
	}
	return prediction
}

func predictLumaIntra4x4HorizontalDown(top [4]uint8, left [4]uint8, topLeft uint8) (prediction [16]uint8) {
	verticalTop := [8]uint8{left[0], left[1], left[2], left[3], left[3], left[3], left[3], left[3]}
	verticalPrediction := predictLumaIntra4x4VerticalRight(verticalTop, top, topLeft)
	for row := 0; row < 4; row++ {
		for column := 0; column < 4; column++ {
			prediction[row*4+column] = verticalPrediction[column*4+row]
		}
	}
	return prediction
}

func predictLumaIntra4x4VerticalLeft(top [8]uint8) (prediction [16]uint8) {
	for row := 0; row < 4; row++ {
		for column := 0; column < 4; column++ {
			phase := 2*column + row
			var value int
			if phase%2 == 0 {
				center := phase / 2
				value = (int(top[center]) + int(top[center+1]) + 1) >> 1
			} else {
				center := (phase - 1) / 2
				value = (int(top[center]) + 2*int(top[center+1]) + int(top[center+2]) + 2) >> 2
			}
			prediction[row*4+column] = uint8(value)
		}
	}
	return prediction
}

func predictLumaIntra4x4HorizontalUp(left [8]uint8) (prediction [16]uint8) {
	for row := 0; row < 4; row++ {
		for column := 0; column < 4; column++ {
			phase := 2*row + column
			var value int
			if phase%2 == 0 {
				center := phase / 2
				value = (int(left[center]) + int(left[center+1]) + 1) >> 1
			} else {
				center := (phase - 1) / 2
				value = (int(left[center]) + 2*int(left[center+1]) + int(left[center+2]) + 2) >> 2
			}
			prediction[row*4+column] = uint8(value)
		}
	}
	return prediction
}
