"""Result of Mp4File.getbasicinfo() / VidFile.getbasicinfo()."""

from dataclasses import dataclass, field
from typing import List


@dataclass
class BasicVideoStreamInfo:
    width: int = 0
    height: int = 0
    framerate: float = 0.0
    frame_count: int = 0
    duration_seconds: float = 0.0


@dataclass
class BasicAudioStreamInfo:
    sample_rate: int = 0
    channels: int = 0


@dataclass
class BasicInfo:
    hasvideo: bool = False  # one or more video tracks
    hasaudio: bool = False  # one or more audio tracks
    videostreams: List[BasicVideoStreamInfo] = field(default_factory=list)
    audiostreams: List[BasicAudioStreamInfo] = field(default_factory=list)
