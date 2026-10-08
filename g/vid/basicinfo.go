package vid

// BasicInfo holds basic facts about a video file.
type BasicInfo struct {
	HasVideo     bool // one or more video tracks
	HasAudio     bool // one or more audio tracks
	VideoStreams []BasicVideoStreamInfo
	AudioStreams []BasicAudioStreamInfo
}

// BasicVideoStreamInfo holds basic facts about one video stream.
type BasicVideoStreamInfo struct {
	Width     uint32
	Height    uint32
	FrameRate float64
}

// BasicAudioStreamInfo holds basic facts about one audio stream.
type BasicAudioStreamInfo struct {
	SampleRate uint32
	Channels   uint16
}
