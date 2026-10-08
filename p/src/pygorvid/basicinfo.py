"""Result of Mp4File.getbasicinfo() / VidFile.getbasicinfo()."""

from dataclasses import dataclass


@dataclass
class BasicInfo:
    hasvideo: bool = False  # one or more video tracks
    hasaudio: bool = False  # one or more audio tracks
