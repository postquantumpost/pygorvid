import pytest

from pygorvid import CodecError, CodecErrorCode, HEVCDecoder


@pytest.mark.parametrize("value", [None, "", "false", "TRUE", "1", " true", "true ", "true"])
def test_hevc_decoder_licensing(monkeypatch, value):
    if value is None:
        monkeypatch.delenv("HEVC_IS_LICENSED", raising=False)
    else:
        monkeypatch.setenv("HEVC_IS_LICENSED", value)
    decoder = HEVCDecoder()
    for operation in (decoder.check_available, lambda: decoder.decode_frame(0)):
        with pytest.raises(CodecError) as raised:
            operation()
        if value == "true":
            assert raised.value.code == CodecErrorCode.NOT_IMPLEMENTED
            assert str(raised.value) == "HEVC decoding is not implemented"
        else:
            assert raised.value.code == CodecErrorCode.NOT_IMPLEMENTED_AND_NOT_LICENSED
            assert str(raised.value) == "HEVC decoding is not implemented and not licensed"


def test_h264_legacy_imports_retain_identity():
    from pygorvid import decoder, h264_decoder, reconstruction, h264_reconstruction
    assert decoder is h264_decoder
    assert reconstruction is h264_reconstruction