/// Basic facts about a video file.
#[derive(Clone, Debug, Default, PartialEq)]
pub struct BasicInfo {
    /// One or more video tracks.
    pub hasvideo: bool,
    /// One or more audio tracks.
    pub hasaudio: bool,
    /// One entry for each video track.
    pub videostreams: Vec<BasicVideoStreamInfo>,
    /// One entry for each audio track.
    pub audiostreams: Vec<BasicAudioStreamInfo>,
}

/// Basic facts about one video stream.
#[derive(Clone, Debug, Default, PartialEq)]
pub struct BasicVideoStreamInfo {
    pub width: u32,
    pub height: u32,
    pub framerate: f64,
    pub frame_count: u64,
    pub duration_seconds: f64,
}

/// Basic facts about one audio stream.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct BasicAudioStreamInfo {
    pub sample_rate: u32,
    pub channels: u16,
}
