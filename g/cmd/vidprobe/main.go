package main

import (
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"pygorvid/vid"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func generationMetadata() vid.Mp4Metadata {
	return vid.Mp4Metadata{
		Title: "Title", Artist: "Artist", AlbumArtist: "Album Artist", Genre: "Genre",
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
	generate := flags.String("generate", "", "generate one second of 64x64 grey video with stereo silence (.mp4)")
	output := flags.String("output", "", "output image path (.png or .jpg)")
	frame := flags.Int("extractframe", -1, "zero-based frame index to extract")
	readAll := flags.Bool("readall", false, "decode all video frames without writing images")
	verbose := flags.Bool("verbose", false, "print decoder progress and debug information")
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
		if len(paths) != 0 || *readAll || frameSet || outputSet {
			fmt.Fprintln(os.Stderr, "--generate cannot be combined with inputs, --readall, --extractframe or --output")
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
		if err := writeOutputFile(outputPath, payload); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", outputPath, err)
			return 1
		}
		fmt.Printf("%s: generated one second of 64x64 grey video with stereo silence\n", outputPath)
		return 0
	}
	if len(paths) == 0 {
		printUsage()
		return 2
	}
	if *readAll && (frameSet || *output != "") {
		fmt.Fprintln(os.Stderr, "--readall cannot be used with --extractframe or --output")
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
		if err := extractFrame(paths[0], *frame, outputPath, *threads, *verbose); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", paths[0], err)
			return 1
		}
		fmt.Printf("%s: extracted frame %d to %s\n", paths[0], *frame, outputPath)
		return 0
	}
	status := 0
	for _, p := range paths {
		if *readAll {
			count, err := readAllFrames(p, *threads, *verbose)
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
	fmt.Fprintln(os.Stderr, "usage: vidprobe [--input FILE] [--threads N] [--verbose] [--readall | --extractframe INDEX --output IMAGE [--prefix TEXT]] FILE...")
}

func readAllFrames(input string, threads int, verbose bool) (int, error) {
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
		if _, err := decoder.DecodeFrame(uint64(index)); err != nil {
			return index, fmt.Errorf("decode frame %d: %w", index, err)
		}
	}
	return count, nil
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

func extractFrame(input string, frame int, output string, threads int, verbose bool) error {
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
