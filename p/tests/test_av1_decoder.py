import pytest

from pygorvid import (
    AV1Decoder,
    CodecError,
    CodecErrorCode,
)


def test_av1_decoder_is_explicitly_unimplemented():
    decoder = AV1Decoder()
    with pytest.raises(CodecError) as caught:
        decoder.check_available()
    assert caught.value.code == CodecErrorCode.NOT_IMPLEMENTED
    assert str(caught.value) == "AV1 decoding is not implemented"
