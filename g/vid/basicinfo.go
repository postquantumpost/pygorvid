package vid

// BasicInfo holds basic facts about a video file.
type BasicInfo struct {
	HasVideo bool // one or more video tracks
	HasAudio bool // one or more audio tracks
}
