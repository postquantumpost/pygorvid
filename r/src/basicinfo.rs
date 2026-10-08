/// Basic facts about a video file.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct BasicInfo {
    /// One or more video tracks.
    pub hasvideo: bool,
    /// One or more audio tracks.
    pub hasaudio: bool,
}
