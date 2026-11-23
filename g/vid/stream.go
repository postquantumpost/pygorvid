package vid

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// StreamFrame is one decoded frame in presentation order.
type StreamFrame struct {
	Index         int
	Frame         Yuv420Frame
	PTSTicks      int64
	DurationTicks uint32
	Timescale     uint32
}

// VideoStreamDecoder decodes a video file and emits frames in presentation order.
// Readahead, when enabled, decodes independent future closed GOPs concurrently.
type VideoStreamDecoder struct {
	input     string
	threads   int
	verbose   bool
	readahead bool
}

func NewVideoStreamDecoder(input string) *VideoStreamDecoder {
	return &VideoStreamDecoder{input: input, threads: DefaultDecoderThreads()}
}

func (decoder *VideoStreamDecoder) SetThreads(threads int) {
	if threads < 1 {
		threads = DefaultDecoderThreads()
	}
	decoder.threads = threads
}

func (decoder *VideoStreamDecoder) SetVerbose(verbose bool) {
	decoder.verbose = verbose
}

func (decoder *VideoStreamDecoder) SetReadahead(readahead bool) {
	decoder.readahead = readahead
}

// Decode calls consume once for each frame, in presentation order. At most one
// foreground frame is retained by the callback.
func (decoder *VideoStreamDecoder) Decode(consume func(StreamFrame) error) (int, error) {
	if decoder == nil {
		return 0, errors.New("video stream decoder is nil")
	}
	if consume == nil {
		return 0, errors.New("frame consumer is nil")
	}
	reader, err := OpenVideoSampleReader(decoder.input)
	if err != nil {
		return 0, err
	}
	defer reader.Close()
	frames, err := presentationSamples(reader)
	if err != nil {
		return 0, err
	}
	timescale := videoTimescale(reader)
	emit := func(result streamDecodeResult) error {
		sample := frames[result.index]
		return consume(StreamFrame{
			Index: result.index, Frame: result.frame,
			PTSTicks: sample.ptsTicks, DurationTicks: sample.durationTicks,
			Timescale: timescale,
		})
	}
	if !decoder.readahead {
		return decodeStreamSequential(reader, decoder.threads, decoder.verbose, emit)
	}
	groups, err := closedGOPs(reader, frames)
	if err != nil {
		return 0, err
	}
	return decodeStreamWithReadahead(
		decoder.input, reader, groups, decoder.threads, decoder.verbose, emit,
	)
}

type presentationSample struct {
	decodeIndex   int
	ptsTicks      int64
	durationTicks uint32
}

type closedGOP struct {
	frames []int
}

type streamDecodeResult struct {
	index int
	frame Yuv420Frame
	err   error
}

func videoTimescale(reader *VideoSampleReader) uint32 {
	config := reader.Configuration()
	if config.Timescale != 0 {
		return config.Timescale
	}
	if config := reader.HEVCConfiguration(); config != nil {
		return config.Timescale
	}
	if config := reader.AV1Configuration(); config != nil {
		return config.Timescale
	}
	return 0
}

func presentationSamples(reader *VideoSampleReader) ([]presentationSample, error) {
	count := reader.SampleCount()
	ordered := make([]presentationSample, count)
	for index := range ordered {
		sample := reader.samples[index]
		ordered[index] = presentationSample{
			decodeIndex: index, ptsTicks: sample.ptsTicks,
			durationTicks: sample.durationTicks,
		}
	}
	sort.SliceStable(ordered, func(left, right int) bool {
		if ordered[left].ptsTicks != ordered[right].ptsTicks {
			return ordered[left].ptsTicks < ordered[right].ptsTicks
		}
		return ordered[left].decodeIndex < ordered[right].decodeIndex
	})
	return ordered, nil
}

func closedGOPs(reader *VideoSampleReader, presentation []presentationSample) ([]closedGOP, error) {
	if reader.Codec() != CodecH264 {
		return nil, nil
	}
	positions := make([]int, len(presentation))
	for presentationIndex, sample := range presentation {
		positions[sample.decodeIndex] = presentationIndex
	}
	count := reader.SampleCount()
	starts := reader.SyncSampleIndices()
	if len(starts) < 2 {
		return nil, nil
	}
	if starts[0] != 0 {
		starts = append([]int{0}, starts...)
	}
	groups := make([]closedGOP, len(starts))
	for groupIndex, start := range starts {
		end := count
		if groupIndex+1 < len(starts) {
			end = starts[groupIndex+1]
		}
		frames := make([]int, 0, end-start)
		for decodeIndex := start; decodeIndex < end; decodeIndex++ {
			frames = append(frames, positions[decodeIndex])
		}
		sort.Ints(frames)
		groups[groupIndex] = closedGOP{frames: frames}
	}
	return groups, nil
}

func decodeStreamSequential(
	reader *VideoSampleReader, threads int, verbose bool,
	consume func(streamDecodeResult) error,
) (int, error) {
	decoder, err := NewVideoDecoder(reader)
	if err != nil {
		return 0, err
	}
	decoder.SetThreads(threads)
	decoder.SetVerbose(verbose)
	count := reader.SampleCount()
	for index := 0; index < count; index++ {
		if index > 0 {
			sample, err := reader.SampleAt(uint64(index))
			if err != nil {
				return index, fmt.Errorf("decode frame %d: read sample: %w", index, err)
			}
			if sample.IsSync {
				decoder, err = NewVideoDecoder(reader)
				if err != nil {
					return index, err
				}
				decoder.SetThreads(threads)
				decoder.SetVerbose(verbose)
			}
		}
		frame, err := decoder.DecodeFrame(uint64(index))
		if err != nil {
			return index, fmt.Errorf("decode frame %d: %w", index, err)
		}
		if err := consume(streamDecodeResult{index: index, frame: frame}); err != nil {
			return index, err
		}
	}
	return count, nil
}

func decodeStreamWithReadahead(
	input string, reader *VideoSampleReader, groups []closedGOP,
	threads int, verbose bool, consume func(streamDecodeResult) error,
) (int, error) {
	if len(groups) < 2 || threads < 2 {
		return decodeStreamSequential(reader, threads, verbose, consume)
	}
	workerLimit := memoryWorkerLimit(reader, groups, min(threads-1, len(groups)-1))
	if workerLimit == 0 {
		return decodeStreamSequential(reader, threads, verbose, consume)
	}
	pending := make(map[int]chan []streamDecodeResult)
	var pendingMu sync.Mutex
	nextJob := 1
	startJob := func(group int) error {
		result := make(chan []streamDecodeResult, 1)
		ready := make(chan error, 1)
		pendingMu.Lock()
		pending[group] = result
		pendingMu.Unlock()
		go func() {
			workerReader, err := OpenVideoSampleReader(input)
			if err != nil {
				ready <- err
				return
			}
			defer workerReader.Close()
			worker, err := NewVideoDecoder(workerReader)
			if err != nil {
				ready <- err
				return
			}
			worker.SetThreads(1)
			worker.SetVerbose(verbose)
			ready <- nil
			frames := make([]streamDecodeResult, 0, len(groups[group].frames))
			for _, index := range groups[group].frames {
				frame, decodeErr := worker.DecodeFrame(uint64(index))
				frames = append(frames, streamDecodeResult{
					index: index, frame: frame, err: decodeErr,
				})
				if decodeErr != nil {
					break
				}
			}
			result <- frames
		}()
		return <-ready
	}
	for nextJob <= workerLimit {
		if err := startJob(nextJob); err != nil {
			return 0, err
		}
		nextJob++
	}
	foreground, err := NewVideoDecoder(reader)
	if err != nil {
		return 0, err
	}
	foreground.SetThreads(1)
	foreground.SetVerbose(verbose)
	consumed := 0
	for groupIndex, group := range groups {
		if groupIndex == 0 {
			for _, index := range group.frames {
				frame, err := foreground.DecodeFrame(uint64(index))
				if err != nil {
					return consumed, fmt.Errorf("decode frame %d: %w", index, err)
				}
				if err := consume(streamDecodeResult{index: index, frame: frame}); err != nil {
					return consumed, err
				}
				consumed++
			}
		} else {
			pendingMu.Lock()
			result := pending[groupIndex]
			delete(pending, groupIndex)
			pendingMu.Unlock()
			for _, result := range <-result {
				if result.err != nil {
					return consumed, fmt.Errorf("decode frame %d: %w", result.index, result.err)
				}
				if err := consume(result); err != nil {
					return consumed, err
				}
				consumed++
			}
		}
		if nextJob < len(groups) {
			if err := startJob(nextJob); err != nil {
				return consumed, err
			}
			nextJob++
		}
	}
	return consumed, nil
}

func availableMemoryBytes() uint64 {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[0] == "MemAvailable:" {
			kilobytes, err := strconv.ParseUint(fields[1], 10, 64)
			if err == nil && kilobytes <= ^uint64(0)/1024 {
				return kilobytes * 1024
			}
		}
	}
	if scanner.Err() != nil {
		return 0
	}
	return 0
}

func memoryWorkerLimit(reader *VideoSampleReader, groups []closedGOP, requested int) int {
	available := availableMemoryBytes()
	return memoryWorkerLimitForAvailable(reader, groups, requested, available)
}

func memoryWorkerLimitForAvailable(
	reader *VideoSampleReader, groups []closedGOP, requested int, available uint64,
) int {
	if requested <= 0 {
		return 0
	}
	config := reader.Configuration()
	if available == 0 || len(config.SequenceSets) == 0 {
		return requested
	}
	sps, err := ParseSPS(config.SequenceSets[0])
	if err != nil {
		return requested
	}
	frameBytes := uint64(sps.Width) * uint64(sps.Height) * 3 / 2
	maxFrames := 0
	for _, group := range groups {
		maxFrames = max(maxFrames, len(group.frames))
	}
	if frameBytes == 0 || maxFrames == 0 {
		return requested
	}
	gopBytes := frameBytes * uint64(maxFrames)
	if gopBytes == 0 || gopBytes < frameBytes {
		return 0
	}
	limit := (available / 2) / gopBytes
	if limit > uint64(requested) {
		return requested
	}
	return int(limit)
}
