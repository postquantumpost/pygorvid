package vid

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
)

// AVCConfiguration describes the selected video track's AVC sample entry.
type AVCConfiguration struct {
	Profile       uint8
	ProfileCompat uint8
	Level         uint8
	NALLengthSize uint8
	SequenceSets  [][]byte
	PictureSets   [][]byte
	Timescale     uint32
}

// CompressedSample contains one length-prefixed MP4 video sample.
type CompressedSample struct {
	Index         uint64
	Data          []byte
	DTSTicks      int64
	PTSTicks      int64
	DurationTicks uint32
	IsSync        bool
}

type sampleLocation struct {
	offset        uint64
	size          uint32
	dtsTicks      int64
	ptsTicks      int64
	durationTicks uint32
	isSync        bool
}

// VideoSampleReader owns the MP4 file used to read samples from its first AVC video track.
type VideoSampleReader struct {
	file          *os.File
	configuration AVCConfiguration
	samples       []sampleLocation
	nextIndex     int
}

// OpenVideoSampleReader opens an MP4 and maps the first AVC video track's samples.
func OpenVideoSampleReader(path string) (*VideoSampleReader, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	reader, err := newVideoSampleReader(file)
	if err != nil {
		file.Close()
		return nil, err
	}
	return reader, nil
}

func newVideoSampleReader(file *os.File) (*VideoSampleReader, error) {
	fileInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}
	boxes, err := readBoxes(file, 0, fileInfo.Size())
	if err != nil {
		return nil, err
	}
	track, err := firstVideoTrack(boxes)
	if err != nil {
		return nil, err
	}

	mdhdBoxes := find([]box{track}, "trak", "mdia", "mdhd")
	if len(mdhdBoxes) != 1 {
		return nil, errors.New("video track must contain one mdhd box")
	}
	mdhd, ok := mdhdBoxes[0].(*mdhdBox)
	if !ok || mdhd.timescale == 0 {
		return nil, errors.New("video track has an invalid media timescale")
	}

	stsdBoxes := find([]box{track}, "trak", "mdia", "minf", "stbl", "stsd")
	if len(stsdBoxes) != 1 {
		return nil, errors.New("video track must contain one stsd box")
	}
	stsd, ok := stsdBoxes[0].(*stsdBox)
	if !ok {
		return nil, errors.New("invalid stsd box")
	}
	if len(stsd.entries) != 1 {
		return nil, errors.New("multiple video sample descriptions are unsupported")
	}
	var avcEntry *avc1Box
	for _, entry := range stsd.entries {
		if typed, isAVC := entry.(*avc1Box); isAVC {
			if avcEntry != nil {
				return nil, errors.New("multiple avc1 sample descriptions are unsupported")
			}
			avcEntry = typed
		}
	}
	if avcEntry == nil {
		return nil, errors.New("first video track has no avc1 sample description")
	}
	var avcConfig *avcCBox
	for _, child := range avcEntry.children {
		if typed, isConfig := child.(*avcCBox); isConfig {
			if avcConfig != nil {
				return nil, errors.New("multiple avcC boxes are unsupported")
			}
			avcConfig = typed
		}
	}
	if avcConfig == nil {
		return nil, errors.New("avc1 sample description has no avcC box")
	}
	configuration := AVCConfiguration{
		Profile:       avcConfig.profile,
		ProfileCompat: avcConfig.profileCompat,
		Level:         avcConfig.level,
		NALLengthSize: avcConfig.nalLengthSize,
		SequenceSets:  cloneNALUnits(avcConfig.sequenceSets),
		PictureSets:   cloneNALUnits(avcConfig.pictureSets),
		Timescale:     mdhd.timescale,
	}

	stsz, err := oneVideoBox[*stszBox](track, "stsz")
	if err != nil {
		return nil, err
	}
	stsc, err := oneVideoBox[*stscBox](track, "stsc")
	if err != nil {
		return nil, err
	}
	stts, err := oneVideoBox[*sttsBox](track, "stts")
	if err != nil {
		return nil, err
	}
	durations, err := expandSampleTiming(stts.entries, stsz.sampleCount, "stts")
	if err != nil {
		return nil, err
	}
	compositionOffsets := make([]int64, stsz.sampleCount)
	cttsBoxes := find([]box{track}, "trak", "mdia", "minf", "stbl", "ctts")
	if len(cttsBoxes) > 1 {
		return nil, errors.New("multiple ctts boxes are unsupported")
	}
	if len(cttsBoxes) == 1 {
		ctts, ok := cttsBoxes[0].(*cttsBox)
		if !ok {
			return nil, errors.New("invalid ctts box")
		}
		compositionOffsets, err = expandCompositionOffsets(ctts.entries, stsz.sampleCount)
		if err != nil {
			return nil, err
		}
	}

	var syncSamples map[uint32]struct{}
	stssBoxes := find([]box{track}, "trak", "mdia", "minf", "stbl", "stss")
	if len(stssBoxes) > 1 {
		return nil, errors.New("multiple stss boxes are unsupported")
	}
	if len(stssBoxes) == 1 {
		stss, ok := stssBoxes[0].(*stssBox)
		if !ok {
			return nil, errors.New("invalid stss box")
		}
		syncSamples = make(map[uint32]struct{}, len(stss.sampleNumbers))
		for _, sampleNumber := range stss.sampleNumbers {
			if sampleNumber > stsz.sampleCount {
				return nil, errors.New("stss sample number exceeds sample count")
			}
			syncSamples[sampleNumber] = struct{}{}
		}
	}

	var chunkOffsets []uint64
	offsetTables := append(
		find([]box{track}, "trak", "mdia", "minf", "stbl", "stco"),
		find([]box{track}, "trak", "mdia", "minf", "stbl", "co64")...,
	)
	if len(offsetTables) != 1 {
		return nil, errors.New("video track must contain one stco or co64 box")
	}
	switch table := offsetTables[0].(type) {
	case *stcoBox:
		chunkOffsets = table.offsets
	case *co64Box:
		chunkOffsets = table.offsets
	default:
		return nil, errors.New("invalid chunk offset box")
	}

	mediaRanges := make([][2]uint64, 0)
	for _, media := range find(boxes, "mdat") {
		if mdat, ok := media.(*mdatBox); ok {
			mediaRanges = append(mediaRanges, [2]uint64{uint64(mdat.dataStart), uint64(mdat.dataEnd)})
		}
	}
	if stsz.sampleCount > 0 && len(mediaRanges) == 0 {
		return nil, errors.New("video samples exist but no mdat box was found")
	}
	samples, err := mapVideoSamples(stsz, stsc.entries, chunkOffsets, durations, compositionOffsets, syncSamples, mediaRanges)
	if err != nil {
		return nil, err
	}
	return &VideoSampleReader{file: file, configuration: configuration, samples: samples}, nil
}

func firstVideoTrack(boxes []box) (*trakBox, error) {
	for _, candidate := range find(boxes, "moov", "trak") {
		track, ok := candidate.(*trakBox)
		if !ok {
			continue
		}
		for _, handler := range find([]box{track}, "trak", "mdia", "hdlr") {
			if typed, ok := handler.(*hdlrBox); ok && typed.handlerType == "vide" {
				return track, nil
			}
		}
	}
	return nil, errors.New("MP4 contains no video track")
}

func oneVideoBox[T box](track *trakBox, kind string) (T, error) {
	var zero T
	boxes := find([]box{track}, "trak", "mdia", "minf", "stbl", kind)
	if len(boxes) != 1 {
		return zero, fmt.Errorf("video track must contain one %s box", kind)
	}
	typed, ok := boxes[0].(T)
	if !ok {
		return zero, fmt.Errorf("invalid %s box", kind)
	}
	return typed, nil
}

func expandSampleTiming(entries []sttsEntry, sampleCount uint32, name string) ([]uint32, error) {
	values := make([]uint32, 0, sampleCount)
	for _, entry := range entries {
		if entry.sampleCount > sampleCount-uint32(len(values)) {
			return nil, fmt.Errorf("%s timing entries exceed sample count", name)
		}
		for range entry.sampleCount {
			values = append(values, entry.sampleDelta)
		}
	}
	if uint32(len(values)) != sampleCount {
		return nil, fmt.Errorf("%s timing entries do not cover all samples", name)
	}
	return values, nil
}

func expandCompositionOffsets(entries []cttsEntry, sampleCount uint32) ([]int64, error) {
	values := make([]int64, 0, sampleCount)
	for _, entry := range entries {
		if entry.sampleCount > sampleCount-uint32(len(values)) {
			return nil, errors.New("ctts timing entries exceed sample count")
		}
		for range entry.sampleCount {
			values = append(values, entry.sampleOffset)
		}
	}
	if uint32(len(values)) != sampleCount {
		return nil, errors.New("ctts timing entries do not cover all samples")
	}
	return values, nil
}

func mapVideoSamples(
	sizes *stszBox,
	chunkMap []stscEntry,
	chunkOffsets []uint64,
	durations []uint32,
	compositionOffsets []int64,
	syncSamples map[uint32]struct{},
	mediaRanges [][2]uint64,
) ([]sampleLocation, error) {
	if sizes.sampleCount == 0 {
		return nil, nil
	}
	if len(chunkMap) == 0 || len(chunkOffsets) == 0 {
		return nil, errors.New("sample table lacks chunk mapping entries")
	}
	samples := make([]sampleLocation, 0, sizes.sampleCount)
	sampleIndex := uint32(0)
	chunkRun := 0
	dts := int64(0)
	for chunkIndex, chunkOffset := range chunkOffsets {
		chunkNumber := uint64(chunkIndex + 1)
		for chunkRun+1 < len(chunkMap) && uint64(chunkMap[chunkRun+1].firstChunk) <= chunkNumber {
			chunkRun++
		}
		mapping := chunkMap[chunkRun]
		if uint64(mapping.firstChunk) > chunkNumber || mapping.sampleDescriptionIndex != 1 {
			return nil, errors.New("chunk uses an unsupported sample description")
		}
		offset := chunkOffset
		for range mapping.samplesPerChunk {
			if sampleIndex >= sizes.sampleCount {
				return nil, errors.New("chunk map contains more samples than stsz")
			}
			size := sizes.constantSize
			if size == 0 {
				if int(sampleIndex) >= len(sizes.sizes) {
					return nil, errors.New("stsz sample size is missing")
				}
				size = sizes.sizes[sampleIndex]
			}
			end := offset + uint64(size)
			if end < offset || size == 0 || !insideMediaRange(offset, end, mediaRanges) {
				return nil, errors.New("sample byte range is outside every mdat payload")
			}
			if sampleIndex >= uint32(len(durations)) || sampleIndex >= uint32(len(compositionOffsets)) {
				return nil, errors.New("sample timing is missing")
			}
			pts, ok := checkedAddInt64(dts, compositionOffsets[sampleIndex])
			if !ok {
				return nil, errors.New("presentation timestamp overflows")
			}
			if (compositionOffsets[sampleIndex] > 0 && pts < dts) || (compositionOffsets[sampleIndex] < 0 && pts > dts) {
				return nil, errors.New("presentation timestamp overflows")
			}
			_, isSync := syncSamples[sampleIndex+1]
			if syncSamples == nil {
				isSync = true
			}
			samples = append(samples, sampleLocation{
				offset:        offset,
				size:          size,
				dtsTicks:      dts,
				ptsTicks:      pts,
				durationTicks: durations[sampleIndex],
				isSync:        isSync,
			})
			dts, ok = checkedAddInt64(dts, int64(durations[sampleIndex]))
			if !ok {
				return nil, errors.New("decode timestamp overflows")
			}
			if dts < 0 {
				return nil, errors.New("decode timestamp overflows")
			}
			offset = end
			sampleIndex++
		}
	}
	if sampleIndex != sizes.sampleCount {
		return nil, errors.New("chunk map does not cover every stsz sample")
	}
	return samples, nil
}

func checkedAddInt64(left, right int64) (int64, bool) {
	maxInt64 := int64(^uint64(0) >> 1)
	minInt64 := -maxInt64 - 1
	if right > 0 && left > maxInt64-right || right < 0 && left < minInt64-right {
		return 0, false
	}
	return left + right, true
}

func insideMediaRange(start, end uint64, ranges [][2]uint64) bool {
	for _, media := range ranges {
		if media[0] <= start && end <= media[1] {
			return true
		}
	}
	return false
}

func cloneNALUnits(nals [][]byte) [][]byte {
	clones := make([][]byte, len(nals))
	for index, nal := range nals {
		clones[index] = append([]byte(nil), nal...)
	}
	return clones
}

func validateLengthPrefixedNALs(data []byte) error {
	for offset := 0; offset < len(data); {
		if len(data)-offset < 4 {
			return errors.New("truncated NAL length prefix")
		}
		nalSize := uint64(binary.BigEndian.Uint32(data[offset : offset+4]))
		offset += 4
		if nalSize == 0 {
			return errors.New("NAL unit has zero length")
		}
		if nalSize > uint64(len(data)-offset) {
			return errors.New("NAL unit is truncated")
		}
		offset += int(nalSize)
	}
	return nil
}

// Configuration returns a deep copy so callers cannot mutate reader state.
func (r *VideoSampleReader) Configuration() AVCConfiguration {
	configuration := r.configuration
	configuration.SequenceSets = cloneNALUnits(r.configuration.SequenceSets)
	configuration.PictureSets = cloneNALUnits(r.configuration.PictureSets)
	return configuration
}

func (r *VideoSampleReader) SampleCount() int { return len(r.samples) }

// NextSample returns the next sample in decode order. ok is false at end of stream.
func (r *VideoSampleReader) NextSample() (sample CompressedSample, ok bool, err error) {
	if r == nil || r.file == nil {
		return CompressedSample{}, false, errors.New("sample reader is closed")
	}
	if r.nextIndex >= len(r.samples) {
		return CompressedSample{}, false, nil
	}
	location := r.samples[r.nextIndex]
	if location.offset > math.MaxInt64 {
		return CompressedSample{}, false, errors.New("sample offset exceeds seek range")
	}
	data := make([]byte, location.size)
	n, err := r.file.ReadAt(data, int64(location.offset))
	if err != nil && !(err == io.EOF && n == len(data)) {
		return CompressedSample{}, false, fmt.Errorf("read sample %d: %w", r.nextIndex, err)
	}
	if n != len(data) {
		return CompressedSample{}, false, io.ErrUnexpectedEOF
	}
	if err := validateLengthPrefixedNALs(data); err != nil {
		return CompressedSample{}, false, fmt.Errorf("sample %d: %w", r.nextIndex, err)
	}
	sample = CompressedSample{
		Index:         uint64(r.nextIndex),
		Data:          data,
		DTSTicks:      location.dtsTicks,
		PTSTicks:      location.ptsTicks,
		DurationTicks: location.durationTicks,
		IsSync:        location.isSync,
	}
	r.nextIndex++
	return sample, true, nil
}

// Close releases the owned MP4 file. It is safe to call more than once.
func (r *VideoSampleReader) Close() error {
	if r == nil || r.file == nil {
		return nil
	}
	err := r.file.Close()
	r.file = nil
	return err
}
