package main

import (
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"pygorvid/vid"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	flags := flag.NewFlagSet("vidprobe", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	input := flags.String("input", "", "first input video file")
	output := flags.String("output", "", "output image path (.png or .jpg)")
	frame := flags.Int("extractframe", -1, "zero-based frame index to extract")
	prefix := flags.String("prefix", "", "prefix prepended to the output image filename")
	if err := flags.Parse(flagsFirst(args)); err != nil {
		return 2
	}
	frameSet := false
	inputSet := false
	flags.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "extractframe":
			frameSet = true
		case "input":
			inputSet = true
		}
	})
	paths := flags.Args()
	if inputSet {
		paths = append([]string{*input}, paths...)
	}
	if len(paths) == 0 {
		printUsage()
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
		if err := extractFrame(paths[0], *frame, outputPath); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", paths[0], err)
			return 1
		}
		fmt.Printf("%s: extracted frame %d to %s\n", paths[0], *frame, outputPath)
		return 0
	}
	status := 0
	for _, p := range paths {
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
		if arg == "--input" || arg == "--output" || arg == "--extractframe" || arg == "--prefix" {
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
	fmt.Fprintln(os.Stderr, "usage: vidprobe [--input FILE] [--extractframe INDEX --output IMAGE [--prefix TEXT]] FILE...")
}

func prefixedOutput(output, prefix string) string {
	if prefix == "" {
		return output
	}
	return filepath.Join(filepath.Dir(output), prefix+filepath.Base(output))
}

func extractFrame(input string, frame int, output string) error {
	extension := strings.ToLower(filepath.Ext(output))
	if extension != ".png" && extension != ".jpg" {
		return errors.New("--output must end in .png or .jpg")
	}
	v := vid.OpenFile(input)
	if !v.IsOpen() {
		return errors.New(v.ErrorInfo())
	}
	defer v.Close()
	if v.Format != "mp4" {
		return errors.New("frame extraction requires an MP4 file")
	}
	v.GetBasicInfo()
	if err := v.ErrorInfo(); err != "" {
		return errors.New(err)
	}
	tempDir, err := os.MkdirTemp(filepath.Dir(output), ".vidprobe-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)
	tempOutput := filepath.Join(tempDir, "frame"+extension)
	filter := fmt.Sprintf("select=eq(n\\,%d)", frame)
	cmd := exec.Command("ffmpeg", "-v", "error", "-i", input,
		"-map", "0:v:0", "-vf", filter, "-fps_mode", "passthrough",
		"-frames:v", "1", "-y", tempOutput)
	if result, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg: %w: %s", err, strings.TrimSpace(string(result)))
	}
	info, err := os.Stat(tempOutput)
	if err != nil || info.Size() == 0 {
		return errors.New("requested frame does not exist or produced an empty image")
	}
	return os.Rename(tempOutput, output)
}
