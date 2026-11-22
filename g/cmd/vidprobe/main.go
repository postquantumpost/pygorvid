package main

import (
	"bufio"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"pygorvid/vid"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func generationMetadata() vid.Mp4Metadata {
	return vid.Mp4Metadata{
		Title: "Title", Artist: "Artist", Album: "Album", Publisher: "Publisher",
		Language: "Language", NowPlaying: "Now Playing",
		AlbumArtist: "Album Artist", Genre: "Genre",
		Track: "1", Disc: "1", Date: "2016-12-31 23:59:60 UTC", Composer: "Composer",
		Comment: "Comments", Description: "Description", Copyright: "Copyright",
		Make: "Camera make", Model: "Camera model", Firmware: "firmware version",
		Location: "11.3733\u00b0 N 142.5917\u00b0 E", Altitude: "-10925.0 meters",
		Heading: "180.0", Encoder: "pygorvid",
	}
}

func run(args []string) int {
	flags := flag.NewFlagSet("vidprobe", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	input := flags.String("input", "", "first input video file")
	generate := flags.String("generate", "", "generate 64x64 H.264 feature-test video with stereo silence (.mp4)")
	output := flags.String("output", "", "output image path (.png or .jpg)")
	frame := flags.Int("extractframe", -1, "zero-based frame index to extract")
	readAll := flags.Bool("readall", false, "decode all video frames without writing images")
	hashFrames := flags.Bool("hash", false, "print SHA-256 hashes for frames decoded by --readall or --extractframe")
	slideshow := flags.Bool("slideshow", false, "display decoded frames in a window")
	verbose := flags.Bool("verbose", false, "print decoder progress and debug information")
	readahead := flags.Bool("readahead", false, "decode upcoming closed GOPs ahead in order")
	prefix := flags.String("prefix", "", "prefix prepended to the output filename")
	threads := flags.Int("threads", vid.DefaultDecoderThreads(), "number of decoder threads")
	if err := flags.Parse(flagsFirst(args)); err != nil {
		return 2
	}
	if *threads < 1 {
		fmt.Fprintln(os.Stderr, "--threads must be a positive integer")
		return 2
	}
	frameSet := false
	inputSet := false
	generateSet := false
	outputSet := false
	flags.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "extractframe":
			frameSet = true
		case "input":
			inputSet = true
		case "generate":
			generateSet = true
		case "output":
			outputSet = true
		}
	})
	paths := flags.Args()
	if inputSet {
		paths = append([]string{*input}, paths...)
	}
	if generateSet {
		if len(paths) != 0 || *readAll || *hashFrames || *slideshow || frameSet || outputSet {
			fmt.Fprintln(os.Stderr, "--generate cannot be combined with inputs, --readall, --hash, --slideshow, --extractframe or --output")
			return 2
		}
		if strings.ToLower(filepath.Ext(*generate)) != ".mp4" {
			fmt.Fprintln(os.Stderr, "--generate must end in .mp4")
			return 2
		}
		outputPath := prefixedOutput(*generate, *prefix)
		metadata := generationMetadata()
		payload, err := vid.NewMp4Writer().EncodeWithMetadata(vid.NewH264Encoder(), &metadata)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", outputPath, err)
			return 1
		}
		companionBase := strings.TrimSuffix(outputPath, filepath.Ext(outputPath))
		companions := vid.GenerateH264CompanionFiles()
		companionPaths := make([]string, 0, len(vid.H264CompanionSuffixes))
		destinations := []string{outputPath}
		for _, suffix := range vid.H264CompanionSuffixes {
			companionPath := companionBase + "-" + suffix
			companionPaths = append(companionPaths, companionPath)
			destinations = append(destinations, companionPath)
		}
		for _, destination := range destinations {
			if err := precheckOutputFile(destination); err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", destination, err)
				return 1
			}
		}
		for index, suffix := range vid.H264CompanionSuffixes {
			companionPath := companionPaths[index]
			if err := writeOutputFile(companionPath, companions[suffix]); err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", companionPath, err)
				return 1
			}
		}
		if err := writeOutputFile(outputPath, payload); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", outputPath, err)
			return 1
		}
		fmt.Printf("%s: generated one second of 64x64 H.264 feature-test video with stereo silence\n", outputPath)
		return 0
	}
	if len(paths) == 0 {
		printUsage()
		return 2
	}
	if (*readAll && *slideshow) || ((*readAll || *slideshow) && (frameSet || outputSet)) {
		fmt.Fprintln(os.Stderr, "--slideshow, --readall and extraction options cannot be combined")
		return 2
	}
	if *hashFrames && !*readAll && !frameSet {
		fmt.Fprintln(os.Stderr, "--hash requires --readall or --extractframe")
		return 2
	}
	if frameSet || *output != "" {
		if !frameSet || *output == "" {
			fmt.Fprintln(os.Stderr, "--extractframe and --output must be used together")
			return 2
		}
		if *frame < 0 {
			fmt.Fprintln(os.Stderr, "--extractframe must be non-negative")
			return 2
		}
		if len(paths) != 1 {
			fmt.Fprintln(os.Stderr, "frame extraction requires exactly one input file")
			return 2
		}
		outputPath := prefixedOutput(*output, *prefix)
		if err := extractFrame(paths[0], *frame, outputPath, *threads, *verbose, *hashFrames); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", paths[0], err)
			return 1
		}
		fmt.Printf("%s: extracted frame %d to %s\n", paths[0], *frame, outputPath)
		return 0
	}
	status := 0
	for _, p := range paths {
		if *slideshow {
			count, err := showSlideshow(p, *threads, *verbose, *readahead)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", p, err)
				status = 1
			} else {
				fmt.Printf("%s: displayed %d frames\n", p, count)
			}
			continue
		}
		if *readAll {
			count, err := readAllFrames(p, *threads, *verbose, *hashFrames, *readahead)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", p, err)
				status = 1
			} else {
				fmt.Printf("%s: read %d frames\n", p, count)
			}
			continue
		}
		v := vid.OpenFile(p)
		if v.IsOpen() {
			info := v.GetBasicInfo()
			if err := v.ErrorInfo(); err != "" {
				fmt.Fprintf(os.Stderr, "%s: %s: %s\n", p, v.Format, err)
				status = 1
			} else {
				fmt.Printf("%s: %s %+v%s\n", p, v.Format, info, formatVideoDurations(info.VideoStreams))
			}
		} else {
			fmt.Fprintf(os.Stderr, "%s: %s\n", p, v.ErrorInfo())
			status = 1
		}
		v.Close()
	}
	return status
}

func formatVideoDurations(streams []vid.BasicVideoStreamInfo) string {
	if len(streams) == 0 {
		return ""
	}
	values := make([]string, len(streams))
	for i, stream := range streams {
		values[i] = fmt.Sprintf("%g seconds (%s)", stream.DurationSeconds, formatDurationHMS(stream.DurationSeconds))
	}
	return " video_durations=[" + strings.Join(values, ", ") + "]"
}

func formatDurationHMS(seconds float64) string {
	total := uint64(math.Ceil(seconds))
	hours := total / 3600
	minutes := (total / 60) % 60
	secondsPart := total % 60
	switch {
	case hours > 0:
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, secondsPart)
	case minutes > 0:
		return fmt.Sprintf("%02d:%02d", minutes, secondsPart)
	default:
		return fmt.Sprintf("%02d", secondsPart)
	}
}

func flagsFirst(args []string) []string {
	var options, paths []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			paths = append(paths, args[i:]...)
			break
		}
		if arg == "--input" || arg == "--output" || arg == "--extractframe" || arg == "--prefix" || arg == "--threads" || arg == "--generate" {
			options = append(options, arg)
			if i+1 < len(args) {
				i++
				options = append(options, args[i])
			}
		} else if strings.HasPrefix(arg, "--input=") || strings.HasPrefix(arg, "--output=") ||
			strings.HasPrefix(arg, "--extractframe=") || strings.HasPrefix(arg, "--prefix=") ||
			strings.HasPrefix(arg, "-") {
			options = append(options, arg)
		} else {
			paths = append(paths, arg)
		}
	}
	return append(options, paths...)
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "       vidprobe --generate OUTPUT.mp4")
	fmt.Fprintln(os.Stderr, "usage: vidprobe [--input FILE] [--threads N] [--readahead] [--verbose] [--hash] [--slideshow | --readall | --extractframe INDEX --output IMAGE [--prefix TEXT]] FILE...")
}

type frameResult struct {
	index int
	frame vid.Yuv420Frame
	err   error
}

type closedGOP struct {
	frames []int
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

func memoryWorkerLimit(reader *vid.VideoSampleReader, groups []closedGOP, requested int) int {
	available := availableMemoryBytes()
	config := reader.Configuration()
	if available == 0 || len(config.SequenceSets) == 0 {
		return requested
	}
	sps, err := vid.ParseSPS(config.SequenceSets[0])
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
	return min(requested, int((available/2)/(frameBytes*uint64(maxFrames))))
}

func closedGOPs(reader *vid.VideoSampleReader) ([]closedGOP, error) {
	count := reader.SampleCount()
	order := make([]int, count)
	positions := make([]int, count)
	pts := make([]int64, count)
	closed := make([]bool, count)
	nalLengthSize := reader.Configuration().NALLengthSize
	for index := 0; index < count; index++ {
		sample, err := reader.SampleAt(uint64(index))
		if err != nil {
			return nil, err
		}
		order[index] = index
		pts[index] = sample.PTSTicks
		if sample.IsSync {
			nals, err := vid.SplitH264SampleNALs(sample.Data, nalLengthSize)
			if err != nil {
				return nil, err
			}
			for _, nal := range nals {
				header, err := vid.ParseNALHeader(nal)
				if err != nil {
					return nil, err
				}
				if header.UnitType == 5 {
					closed[index] = true
					break
				}
			}
		}
	}
	sort.SliceStable(order, func(left, right int) bool {
		if pts[order[left]] != pts[order[right]] {
			return pts[order[left]] < pts[order[right]]
		}
		return order[left] < order[right]
	})
	for presentation, sampleIndex := range order {
		positions[sampleIndex] = presentation
	}
	starts := make([]int, 0)
	for index := 0; index < count; index++ {
		if closed[index] {
			starts = append(starts, index)
		}
	}
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
		for sampleIndex := start; sampleIndex < end; sampleIndex++ {
			frames = append(frames, positions[sampleIndex])
		}
		sort.Ints(frames)
		groups[groupIndex] = closedGOP{frames: frames}
	}
	return groups, nil
}

func decodeSequentialFrames(input string, threads int, verbose bool, consume func(frameResult) error) (int, error) {
	reader, err := vid.OpenVideoSampleReader(input)
	if err != nil {
		return 0, err
	}
	defer reader.Close()
	groups, err := closedGOPs(reader)
	if err != nil {
		return 0, err
	}
	if len(groups) < 2 || threads < 2 {
		return decodeSequentialNoReadahead(input, threads, verbose, consume)
	}
	workerLimit := memoryWorkerLimit(
		reader, groups, min(threads-1, len(groups)-1),
	)
	if workerLimit == 0 {
		return decodeSequentialNoReadahead(input, threads, verbose, consume)
	}
	pending := make(map[int]chan []frameResult)
	var pendingMu sync.Mutex
	nextJob := 1
	startJob := func(group int) {
		result := make(chan []frameResult, 1)
		pendingMu.Lock()
		pending[group] = result
		pendingMu.Unlock()
		go func() {
			workerReader, openErr := vid.OpenVideoSampleReader(input)
			if openErr != nil {
				result <- []frameResult{{err: openErr}}
				return
			}
			defer workerReader.Close()
			decoder, createErr := vid.NewVideoDecoder(workerReader)
			if createErr != nil {
				result <- []frameResult{{err: createErr}}
				return
			}
			decoder.SetThreads(threads)
			decoder.SetVerbose(verbose)
			frames := make([]frameResult, 0, len(groups[group].frames))
			for _, index := range groups[group].frames {
				frame, decodeErr := decoder.DecodeFrame(uint64(index))
				frames = append(frames, frameResult{index: index, frame: frame, err: decodeErr})
				if decodeErr != nil {
					break
				}
			}
			result <- frames
		}()
	}
	for nextJob <= workerLimit {
		startJob(nextJob)
		nextJob++
	}
	decoder, err := vid.NewVideoDecoder(reader)
	if err != nil {
		return 0, err
	}
	decoder.SetThreads(threads)
	decoder.SetVerbose(verbose)
	consumed := 0
	for groupIndex, group := range groups {
		results := make([]frameResult, 0, len(group.frames))
		if groupIndex == 0 {
			for _, index := range group.frames {
				frame, decodeErr := decoder.DecodeFrame(uint64(index))
				results = append(results, frameResult{index: index, frame: frame, err: decodeErr})
				if decodeErr != nil {
					break
				}
			}
		} else {
			pendingMu.Lock()
			result := pending[groupIndex]
			delete(pending, groupIndex)
			pendingMu.Unlock()
			results = <-result
		}
		for _, result := range results {
			if result.err != nil {
				return consumed, fmt.Errorf("decode frame %d: %w", result.index, result.err)
			}
			if err := consume(result); err != nil {
				return consumed, err
			}
			consumed++
		}
		if nextJob < len(groups) {
			startJob(nextJob)
			nextJob++
		}
	}
	return consumed, nil
}

func decodeSequentialNoReadahead(input string, threads int, verbose bool, consume func(frameResult) error) (int, error) {
	reader, err := vid.OpenVideoSampleReader(input)
	if err != nil {
		return 0, err
	}
	defer reader.Close()
	decoder, err := vid.NewVideoDecoder(reader)
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
				decoder, err = vid.NewVideoDecoder(reader)
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
		if err := consume(frameResult{index: index, frame: frame}); err != nil {
			return index, err
		}
	}
	return count, nil
}

func readAllFrames(input string, threads int, verbose, hashFrames bool, readaheadOption ...bool) (int, error) {
	consume := func(result frameResult) error {
		if !hashFrames {
			return nil
		}
		digest, err := frameHash(result.frame)
		if err != nil {
			return fmt.Errorf("decode frame %d: %w", result.index, err)
		}
		fmt.Printf("%s: frame %d sha256=%x\n", input, result.index, digest)
		return nil
	}
	if len(readaheadOption) != 0 && readaheadOption[0] {
		return decodeSequentialFrames(input, threads, verbose, consume)
	}
	return decodeSequentialNoReadahead(input, threads, verbose, consume)
}

func prefixedOutput(output, prefix string) string {
	if prefix == "" {
		return output
	}
	return filepath.Join(filepath.Dir(output), prefix+filepath.Base(output))
}

func writeOutputFile(output string, payload []byte) error {
	tempDir, err := os.MkdirTemp(filepath.Dir(output), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)
	tempPath := filepath.Join(tempDir, "image")
	if err := os.WriteFile(tempPath, payload, 0o600); err != nil {
		return err
	}
	return os.Rename(tempPath, output)
}

func precheckOutputFile(output string) error {
	info, err := os.Stat(output)
	if err == nil {
		if info.IsDir() {
			return fmt.Errorf("is a directory")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func extractFrame(input string, frame int, output string, threads int, verbose, hashFrame bool) error {
	extension := strings.ToLower(filepath.Ext(output))
	if extension != ".png" && extension != ".jpg" {
		return errors.New("--output must end in .png or .jpg")
	}
	reader, err := vid.OpenVideoSampleReader(input)
	if err != nil {
		return err
	}
	defer reader.Close()
	decoder, err := vid.NewVideoDecoder(reader)
	if err != nil {
		return err
	}
	decoder.SetThreads(threads)
	decoder.SetVerbose(verbose)
	decoded, err := decoder.DecodeFrame(uint64(frame))
	if err != nil {
		return err
	}
	if hashFrame {
		digest, err := frameHash(decoded)
		if err != nil {
			return err
		}
		fmt.Printf("%s: frame %d sha256=%x\n", input, frame, digest)
	}
	rgb, err := decoded.ConvertToRGB(vid.ColorRangeLimited)
	if err != nil {
		return err
	}
	var payload []byte
	if extension == ".jpg" {
		payload, err = vid.NewJPEGEncoder().Encode(rgb)
	} else {
		payload, err = vid.NewPNGEncoder().Encode(rgb)
	}
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return err
	}
	return writeOutputFile(output, payload)
}

func frameHash(frame vid.Yuv420Frame) ([]byte, error) {
	y, err := frame.LumaPlaneBytes()
	if err != nil {
		return nil, err
	}
	u, err := frame.UPlaneBytes()
	if err != nil {
		return nil, err
	}
	v, err := frame.VPlaneBytes()
	if err != nil {
		return nil, err
	}
	digest := sha256.New()
	_, _ = digest.Write(y)
	_, _ = digest.Write(u)
	_, _ = digest.Write(v)
	return digest.Sum(nil), nil
}
