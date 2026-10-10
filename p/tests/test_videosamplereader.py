import struct
from pathlib import Path

import pytest

from pygorvid import VideoSampleReader
from pygorvid.mp4boxes import Mp4FormatError


def box(kind: bytes, payload: bytes = b"") -> bytes:
    return struct.pack(">I4s", len(payload) + 8, kind) + payload


def avc_configuration() -> bytes:
    data = bytearray((1, 100, 0, 42, 0xFF, 0xE1))
    data.extend(struct.pack(">H", 4))
    data.extend(b"\x67\x64\x00\x2a")
    data.append(1)
    data.extend(struct.pack(">H", 2))
    data.extend(b"\x68\xee")
    return bytes(data)


def make_mp4(
    offset_kind: bytes,
    constant_sample_size: bool,
    configuration: bytes | None = None,
) -> tuple[bytes, list[bytes]]:
    samples = [
        b"\0\0\0\x02\x65\x80",
        b"\0\0\0\x03\x41\x80\x80",
        b"\0\0\0\x04\x41\x80\x80\x80",
    ]
    if constant_sample_size:
        samples = [b"\0\0\0\x02\x65\x80"] * 3

    tkhd = bytearray(84)
    mdhd = bytearray(20)
    struct.pack_into(">I", mdhd, 12, 1000)
    hdlr = b"\0" * 8 + b"vide" + b"\0" * 4
    avc1_payload = bytearray(78)
    struct.pack_into(">HH", avc1_payload, 24, 16, 16)
    if configuration is None:
        configuration = avc_configuration()
    avc1 = box(b"avc1", bytes(avc1_payload) + box(b"avcC", configuration))
    stsd = box(b"stsd", b"\0" * 4 + struct.pack(">I", 1) + avc1)

    if constant_sample_size:
        stsz_payload = b"\0" * 4 + struct.pack(">II", len(samples[0]), len(samples))
    else:
        stsz_payload = b"\0" * 4 + struct.pack(">II", 0, len(samples))
        stsz_payload += b"".join(struct.pack(">I", len(item)) for item in samples)
    stsz = box(b"stsz", stsz_payload)
    stsc = box(
        b"stsc",
        b"\0" * 4
        + struct.pack(">I", 2)
        + struct.pack(">III", 1, 2, 1)
        + struct.pack(">III", 2, 1, 1),
    )
    stts = box(b"stts", b"\0" * 4 + struct.pack(">III", 1, 3, 1000))
    ctts = box(
        b"ctts",
        b"\x01\0\0\0"
        + struct.pack(">I", 3)
        + struct.pack(">Ii", 1, 0)
        + struct.pack(">Ii", 1, -500)
        + struct.pack(">Ii", 1, 1000),
    )
    stss = box(b"stss", b"\0" * 4 + struct.pack(">III", 2, 1, 3))
    stbl = box(b"stbl", stsd + stsz + stsc + stts + ctts + stss)
    minf = box(b"minf", stbl)
    mdia = box(b"mdia", box(b"mdhd", mdhd) + box(b"hdlr", hdlr) + minf)
    trak = box(b"trak", box(b"tkhd", tkhd) + mdia)
    ftyp = box(b"ftyp", b"isom\0\0\0\0isom")

    def moov_with_offsets(offsets: tuple[int, int]) -> bytes:
        if offset_kind == b"stco":
            offsets_box = box(b"stco", b"\0" * 4 + struct.pack(">I2I", 2, *offsets))
        else:
            offsets_box = box(b"co64", b"\0" * 4 + struct.pack(">I2Q", 2, *offsets))
        updated_stbl = box(b"stbl", stsd + stsz + stsc + stts + ctts + stss + offsets_box)
        updated_mdia = box(b"mdia", box(b"mdhd", mdhd) + box(b"hdlr", hdlr) + box(b"minf", updated_stbl))
        return box(b"moov", box(b"trak", box(b"tkhd", tkhd) + updated_mdia))

    placeholder = moov_with_offsets((0, 0))
    mdat_data_start = len(ftyp) + len(placeholder) + 8
    offsets = (mdat_data_start, mdat_data_start + len(samples[0]) + len(samples[1]))
    moov = moov_with_offsets(offsets)
    assert len(moov) == len(placeholder)
    return ftyp + moov + box(b"mdat", b"".join(samples)), samples


@pytest.mark.parametrize(
    ("offset_kind", "constant_sample_size"),
    [(b"stco", False), (b"co64", True)],
)
def test_video_sample_reader_maps_samples_and_timing(
    tmp_path, offset_kind, constant_sample_size
):
    data, expected_samples = make_mp4(offset_kind, constant_sample_size)
    path = tmp_path / "sample.mp4"
    path.write_bytes(data)

    reader = VideoSampleReader(path)
    assert reader.configuration.profile == 100
    assert reader.configuration.level == 42
    assert reader.configuration.nal_length_size == 4
    assert reader.configuration.timescale == 1000
    assert reader.sample_count == 3

    samples = list(reader)
    assert [item.data for item in samples] == expected_samples
    assert [item.index for item in samples] == [0, 1, 2]
    assert [item.dts_ticks for item in samples] == [0, 1000, 2000]
    assert [item.pts_ticks for item in samples] == [0, 500, 3000]
    assert [item.duration_ticks for item in samples] == [1000, 1000, 1000]
    assert [item.is_sync for item in samples] == [True, False, True]
    assert reader.next_sample() is None

    reader.close()
    reader.close()
    with pytest.raises(ValueError, match="closed"):
        reader.next_sample()


def test_video_sample_reader_reads_decode_order_dependencies_for_presentation_index():
    fixture = (
        Path(__file__).resolve().parents[2]
        / "testdata"
        / "h264"
        / "high42-1080p.mp4"
    )
    reader = VideoSampleReader(fixture)
    try:
        dependencies = reader.decode_order_dependency_samples(1)
        assert [(sample.index, sample.dts_ticks, sample.pts_ticks) for sample in dependencies] == [
            (0, 0, 256),
            (1, 256, 1024),
            (2, 512, 512),
        ]
        assert reader.next_sample().index == 0
        assert [
            sample.index for sample in reader.decode_order_dependency_samples(3)
        ] == [0, 1, 2, 3]
        with pytest.raises(IndexError, match="presentation frame index"):
            reader.decode_order_dependency_samples(reader.sample_count)
    finally:
        reader.close()


@pytest.mark.parametrize(
    "mutate",
    [
        lambda data: data.__setitem__(4, 0xFE),
        lambda data: data.__setitem__(4, 0x7F),
        lambda data: data.__setitem__(8, 0x68),
        lambda data: data.__setitem__(15, 0x67),
        lambda data: data.pop(),
        lambda data: data.append(0),
    ],
    ids=[
        "unsupported-length-size",
        "invalid-reserved-bits",
        "wrong-sps-type",
        "wrong-pps-type",
        "truncated-pps",
        "trailing-data",
    ],
)
def test_video_sample_reader_rejects_invalid_avc_configuration(tmp_path, mutate):
    configuration = bytearray(avc_configuration())
    mutate(configuration)
    data, _ = make_mp4(b"stco", False, bytes(configuration))
    path = tmp_path / "invalid-avcc.mp4"
    path.write_bytes(data)

    with pytest.raises(Mp4FormatError):
        VideoSampleReader(path)


def test_video_sample_reader_rejects_truncated_nal_unit(tmp_path):
    data, _ = make_mp4(b"stco", False)
    first_sample = b"\0\0\0\x02\x65\x80"
    offset = data.index(first_sample)
    corrupted = bytearray(data)
    corrupted[offset + 3] = 3
    path = tmp_path / "truncated-nal.mp4"
    path.write_bytes(corrupted)

    reader = VideoSampleReader(path)
    with pytest.raises(Mp4FormatError, match="sample 0: NAL unit is truncated"):
        reader.next_sample()
    assert reader._next_index == 0
    reader.close()


@pytest.mark.parametrize(
    ("box_type", "relative_offset", "value", "message"),
    [
        (b"stts", 12, 2, "timing entries do not cover all samples"),
        (b"stco", 12, 0, "outside every mdat payload"),
        (b"stsc", 20, 2, "unsupported sample description"),
    ],
    ids=["malformed-timing-table", "invalid-media-offset", "unsupported-sample-description"],
)
def test_video_sample_reader_reports_invalid_sample_tables(
    tmp_path, box_type, relative_offset, value, message
):
    data, _ = make_mp4(b"stco", False)
    corrupted = bytearray(data)
    marker = corrupted.index(box_type)
    struct.pack_into(">I", corrupted, marker + relative_offset, value)
    path = tmp_path / "invalid-table.mp4"
    path.write_bytes(corrupted)

    with pytest.raises(Mp4FormatError, match=message):
        VideoSampleReader(path)


def test_video_sample_reader_matches_active_corpus():
    repository_root = Path(__file__).resolve().parents[2]
    references = {}
    for line in (repository_root / "silkroad1-demux.tsv").read_text().splitlines():
        if not line or line.startswith("#"):
            continue
        fields = line.split("\t")
        if fields[0] == "S":
            assert len(fields) == 4
            references[fields[1]] = {
                "packet_count": int(fields[2]),
                "parameter_sets_hex": fields[3],
                "packets": [],
            }
        else:
            assert fields[0] == "P" and len(fields) == 9
            references[fields[1]]["packets"].append(
                tuple(int(value) for value in fields[2:])
            )

    active_paths = {
        line.split("  ", 1)[1]
        for line in (repository_root / "silkroad1.sha256").read_text().splitlines()
    }
    assert set(references) == active_paths

    for relative_path, reference in references.items():
        path = repository_root / relative_path
        source = path.read_bytes()
        with VideoSampleReader(path) as reader:
            assert reader.sample_count == reference["packet_count"]
            assert len(reference["packets"]) == reference["packet_count"]
            configuration = reader.configuration
            parameter_sets = b"".join(
                struct.pack(">I", len(parameter_set)) + parameter_set
                for parameter_sets in (
                    configuration.sequence_parameter_sets,
                    configuration.picture_parameter_sets,
                )
                for parameter_set in parameter_sets
            )
            assert parameter_sets.hex() == reference["parameter_sets_hex"]

            for packet_reference in reference["packets"]:
                index, position, size, dts, pts, duration, is_sync = packet_reference
                sample = reader.next_sample()
                assert sample is not None
                assert (
                    sample.index,
                    sample.dts_ticks,
                    sample.pts_ticks,
                    sample.duration_ticks,
                    int(sample.is_sync),
                    len(sample.data),
                ) == (index, dts, pts, duration, is_sync, size)
                end = position + size
                assert end <= len(source)
                assert sample.data == source[position:end]

            assert reader.next_sample() is None
