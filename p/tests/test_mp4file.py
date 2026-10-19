import io
import struct

import pytest

from pygorvid import Mp4File, openfile
from pygorvid.mp4boxes import HevcSampleEntryBox, HvcCBox, Mp4FormatError, read_boxes

MP4 = b"\0\0\0\x18ftypisom" + b"\0" * 16


def test_hevc_sample_entries_and_configuration():
    data = bytearray(23)
    data[0], data[1], data[12] = 1, 2, 120
    data[13], data[15], data[16] = 0xF0, 0xFC, 0xFD
    data[17], data[18], data[21], data[22] = 0xFA, 0xFA, 3, 3
    for nal_type in (32, 33, 34):
        data.extend((0x80 | nal_type, 0, 1, 0, 3, nal_type << 1, 1, 0x80))

    def parse(kind, payload):
        config = struct.pack(">I4s", 8 + len(payload), b"hvcC") + payload
        encoded = struct.pack(">I4s", 8 + 78 + len(config), kind) + bytes(78) + config
        return read_boxes(io.BytesIO(encoded), 0, len(encoded))

    for kind in (b"hvc1", b"hev1"):
        entry = parse(kind, data)[0]
        assert isinstance(entry, HevcSampleEntryBox)
        config = entry.children[0]
        assert isinstance(config, HvcCBox)
        assert (config.profile, config.level, config.bit_depth_luma,
                config.bit_depth_chroma, config.nal_length_size) == (2, 120, 10, 10, 4)
        assert config.arrays == [
            (True, nal_type, [bytes((nal_type << 1, 1, 0x80))])
            for nal_type in (32, 33, 34)
        ]
    for length in range(len(data)):
        with pytest.raises(Mp4FormatError):
            parse(b"hvc1", data[:length])
    for offset, value in ((0, 2), (13, 0), (15, 0), (16, 0), (17, 0), (18, 0),
                          (21, 2), (23, 0x60), (26, 0xFF), (28, 0), (29, 0)):
        invalid = data.copy()
        invalid[offset] = value
        with pytest.raises(Mp4FormatError):
            parse(b"hvc1", invalid)
    with pytest.raises(Mp4FormatError):
        parse(b"hvc1", data + b"\0")


def test_mp4file(tmp_path):
    p = tmp_path / "a.mp4"
    p.write_bytes(MP4)
    m = Mp4File()
    assert not m.isopen() and m.errorinfo() == ("",)
    assert m.open(str(p)) and m.isopen()
    m.close()
    assert not m.isopen()


def test_mp4file_rejects_other(tmp_path):
    p = tmp_path / "a.mkv"
    p.write_bytes(b"\x1a\x45\xdf\xa3rest")
    m = Mp4File()
    assert not m.open(str(p))
    assert m.errorinfo()[0] != ""


def test_vidfile_uses_mp4file(tmp_path):
    p = tmp_path / "a.mp4"
    p.write_bytes(MP4)
    v = openfile(str(p))
    assert v.isopen() and v.format == "mp4"
    assert isinstance(v._mp4, Mp4File) and v._mp4.isopen()
    v.close()
    assert not v.isopen()
