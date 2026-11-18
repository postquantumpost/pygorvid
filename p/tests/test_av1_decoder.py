from pathlib import Path

import pytest

from pygorvid import (
    AV1Decoder,
    CodecError,
    CodecErrorCode,
    VideoCodec,
    VideoSampleReader,
    create_decoder,
)


def test_av1_decoder_is_explicitly_unimplemented():
    decoder = AV1Decoder()
    with pytest.raises(CodecError) as caught:
        decoder.check_available()
    assert caught.value.code == CodecErrorCode.NOT_IMPLEMENTED
    assert str(caught.value) == "AV1 decoding is not implemented"


def test_drone_sample_selects_av1_decoder():
    path = Path(__file__).resolve().parents[2] / "samples" / "drone64.mp4"
    with VideoSampleReader(path) as reader:
        assert reader.codec == VideoCodec.AV1
        assert reader.configuration.profile == 0
        assert reader.configuration.level == 19
        assert reader.configuration.bit_depth == 8
        with pytest.raises(CodecError, match="AV1 decoding is not implemented"):
            create_decoder(reader)
