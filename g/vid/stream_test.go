package vid

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestVideoStreamDecoderEmitsPresentationOrder(t *testing.T) {
	path := filepath.Join("..", "..", "samples", "decoder-smoke-16x16.mp4")
	decoder := NewVideoStreamDecoder(path)
	decoder.SetThreads(1)
	indices := make([]int, 0)
	previousPTS := int64(-1)
	count, err := decoder.Decode(func(frame StreamFrame) error {
		indices = append(indices, frame.Index)
		if frame.PTSTicks < previousPTS {
			t.Fatalf("PTS decreased from %d to %d", previousPTS, frame.PTSTicks)
		}
		previousPTS = frame.PTSTicks
		if frame.Timescale == 0 || frame.DurationTicks == 0 {
			t.Fatalf("missing timing metadata: %+v", frame)
		}
		if frame.Frame.Width != 16 || frame.Frame.Height != 16 {
			t.Fatalf("decoded frame size = %dx%d", frame.Frame.Width, frame.Frame.Height)
		}
		return nil
	})
	if err != nil || count != 3 {
		t.Fatalf("Decode() = %d, %v; want 3, nil", count, err)
	}
	if want := []int{0, 1, 2}; !reflect.DeepEqual(indices, want) {
		t.Fatalf("frame indices = %v; want %v", indices, want)
	}
}

func TestVideoStreamDecoderReadaheadMatchesForeground(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gops.mp4")
	if err := os.WriteFile(path, NewMp4Writer().Encode(NewH264Encoder()), 0o600); err != nil {
		t.Fatal(err)
	}
	decode := func(readahead bool) ([][]byte, error) {
		decoder := NewVideoStreamDecoder(path)
		decoder.SetThreads(2)
		decoder.SetReadahead(readahead)
		var frames [][]byte
		_, err := decoder.Decode(func(frame StreamFrame) error {
			y, err := frame.Frame.LumaPlaneBytes()
			if err != nil {
				return err
			}
			frames = append(frames, y)
			return nil
		})
		return frames, err
	}
	foreground, err := decode(false)
	if err != nil {
		t.Fatal(err)
	}
	readahead, err := decode(true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(readahead, foreground) {
		t.Fatal("readahead changed decoded frames")
	}
}

func TestVideoStreamDecoderStopsAtConsumerError(t *testing.T) {
	path := filepath.Join("..", "..", "samples", "decoder-smoke-16x16.mp4")
	decoder := NewVideoStreamDecoder(path)
	stop := errors.New("stop")
	count := 0
	decoded, err := decoder.Decode(func(StreamFrame) error {
		count++
		return stop
	})
	if decoded != 0 || count != 1 || !errors.Is(err, stop) {
		t.Fatalf("Decode() = %d callbacks, %v; count=%d", decoded, err, count)
	}
}

func TestMemoryWorkerLimitCapsQueuedGOPs(t *testing.T) {
	path := filepath.Join("..", "..", "samples", "decoder-smoke-16x16.mp4")
	reader, err := OpenVideoSampleReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	groups := []closedGOP{{frames: make([]int, 30)}}
	if got := memoryWorkerLimitForAvailable(reader, groups, 4, 1); got != 0 {
		t.Fatalf("memoryWorkerLimitForAvailable() = %d; want 0", got)
	}
	if got := memoryWorkerLimitForAvailable(reader, groups, 4, ^uint64(0)); got != 4 {
		t.Fatalf("uncapped memory limit = %d; want 4", got)
	}
}
