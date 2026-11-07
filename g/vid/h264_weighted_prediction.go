package vid

import "fmt"

type PredictionWeight struct {
	Weights [3]int16
	Offsets [3]int16
}

type PredictionWeightTable struct {
	Denominators [3]uint8
	Lists        [2][]PredictionWeight
}

func readPredictionWeightTable(reader *BitReader, sps SPSInfo, maxima [2]uint32, bSlice bool) (*PredictionWeightTable, error) {
	luma, err := reader.ReadUE()
	if err != nil {
		return nil, fmt.Errorf("luma_log2_weight_denom: %w", err)
	}
	if luma > 7 {
		return nil, fmt.Errorf("luma_log2_weight_denom exceeds 7")
	}
	chromaPresent := sps.ChromaFormatIDC != 0 && !sps.SeparateColourPlane
	chroma := uint32(0)
	if chromaPresent {
		chroma, err = reader.ReadUE()
		if err != nil {
			return nil, fmt.Errorf("chroma_log2_weight_denom: %w", err)
		}
	}
	if chroma > 7 {
		return nil, fmt.Errorf("chroma_log2_weight_denom exceeds 7")
	}
	table := &PredictionWeightTable{Denominators: [3]uint8{uint8(luma), uint8(chroma), uint8(chroma)}}
	count := 1
	if bSlice {
		count = 2
	}
	for list := 0; list < count; list++ {
		if maxima[list] > 31 {
			return nil, fmt.Errorf("prediction weight reference maximum exceeds 31")
		}
		for index := uint32(0); index <= maxima[list]; index++ {
			entry := PredictionWeight{Weights: [3]int16{1 << luma, 1 << chroma, 1 << chroma}}
			present, err := reader.ReadBit()
			if err != nil {
				return nil, fmt.Errorf("luma_weight_flag: %w", err)
			}
			if present {
				if err := readPredictionWeightComponent(reader, &entry, 0); err != nil {
					return nil, err
				}
			}
			if chromaPresent {
				present, err = reader.ReadBit()
				if err != nil {
					return nil, fmt.Errorf("chroma_weight_flag: %w", err)
				}
				if present {
					for component := 1; component < 3; component++ {
						if err := readPredictionWeightComponent(reader, &entry, component); err != nil {
							return nil, err
						}
					}
				}
			}
			table.Lists[list] = append(table.Lists[list], entry)
		}
	}
	return table, nil
}

func readPredictionWeightComponent(reader *BitReader, entry *PredictionWeight, component int) error {
	weight, err := reader.ReadSE()
	if err != nil {
		return fmt.Errorf("prediction weight: %w", err)
	}
	offset, err := reader.ReadSE()
	if err != nil {
		return fmt.Errorf("prediction offset: %w", err)
	}
	if weight < -128 || weight > 127 || offset < -128 || offset > 127 {
		return fmt.Errorf("prediction weight or offset exceeds [-128,127]")
	}
	entry.Weights[component], entry.Offsets[component] = int16(weight), int16(offset)
	return nil
}

type nativeWeighting struct {
	table    *PredictionWeightTable
	implicit bool
	poc      int64
}

type samplePredictionWeights struct {
	denominators [3]uint8
	lists        [2]PredictionWeight
}

func (weighting nativeWeighting) selectWeights(compensations [2]*nativeCompensation) (*samplePredictionWeights, error) {
	if weighting.table != nil {
		weights := &samplePredictionWeights{denominators: weighting.table.Denominators}
		for list, compensation := range compensations {
			if compensation != nil {
				index := compensation.refIndex
				if index < 0 || int(index) >= len(weighting.table.Lists[list]) {
					return nil, fmt.Errorf("prediction weight reference is unavailable")
				}
				weights.lists[list] = weighting.table.Lists[list][index]
			}
		}
		return weights, nil
	}
	if weighting.implicit && compensations[0] != nil && compensations[1] != nil {
		first, second := compensations[0].reference, compensations[1].reference
		td := min(int64(127), max(int64(-128), second.PictureOrderCnt-first.PictureOrderCnt))
		w1 := int64(32)
		if td != 0 && !first.IsLongTerm && !second.IsLongTerm {
			tb := min(int64(127), max(int64(-128), weighting.poc-first.PictureOrderCnt))
			half := td / 2
			if half < 0 {
				half = -half
			}
			tx := (16384 + half) / td
			candidate := min(int64(1023), max(int64(-1024), (tb*tx+32)>>6)) >> 2
			if candidate >= -64 && candidate <= 128 {
				w1 = candidate
			}
		}
		return &samplePredictionWeights{
			denominators: [3]uint8{5, 5, 5},
			lists: [2]PredictionWeight{
				{Weights: [3]int16{int16(64 - w1), int16(64 - w1), int16(64 - w1)}},
				{Weights: [3]int16{int16(w1), int16(w1), int16(w1)}},
			},
		}, nil
	}
	return nil, nil
}

func combineWeightedPrediction(first, second uint8, active [2]bool, weights *samplePredictionWeights, component int) (uint8, error) {
	if !active[0] && !active[1] {
		return 0, fmt.Errorf("inter partition has no active prediction list")
	}
	if weights == nil {
		if active[0] && active[1] {
			return uint8((uint16(first) + uint16(second) + 1) >> 1), nil
		}
		if active[0] {
			return first, nil
		}
		return second, nil
	}
	denominator := weights.denominators[component]
	a, b := weights.lists[0], weights.lists[1]
	var value int32
	if active[0] && active[1] {
		value = ((int32(first)*int32(a.Weights[component]) + int32(second)*int32(b.Weights[component]) + (1 << denominator)) >> (denominator + 1)) + ((int32(a.Offsets[component]) + int32(b.Offsets[component]) + 1) >> 1)
	} else {
		entry, sample := a, first
		if active[1] {
			entry, sample = b, second
		}
		rounding := int32(0)
		if denominator > 0 {
			rounding = 1 << (denominator - 1)
		}
		value = ((int32(sample)*int32(entry.Weights[component]) + rounding) >> denominator) + int32(entry.Offsets[component])
	}
	return uint8(min(int32(255), max(int32(0), value))), nil
}
