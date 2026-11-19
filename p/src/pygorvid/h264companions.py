"""Deterministic Annex-B corpora for H.264 profile-extension feature tests."""

from dataclasses import dataclass

from .h264encoder import _BitWriter


_COMPANION_SUFFIXES = (
    "frame-40-422.mp4",
    "frame-41-444.mp4",
    "frame-41-separate.mp4",
    "frame-42-10bit.mp4",
    "frame-42-12bit.mp4",
    "frame-43-fields.mp4",
    "frame-44-mbaff.mp4",
    "frame-45-fmo.mp4",
    "frame-46-redundant.mp4",
    "frame-47-partitions.mp4",
    "frame-48-sp-si.mp4",
    "frame-49-svc.264",
    "frame-49-mvc.264",
    "frame-49-mvcd.264",
    "frame-49-3davc.264",
)


@dataclass(frozen=True)
class _Sequence:
    profile_idc: int
    chroma_format_idc: int = 1
    separate_colour_plane: bool = False
    bit_depth_luma: int = 8
    bit_depth_chroma: int = 8
    frame_mbs_only: bool = True
    mb_adaptive_frame_field: bool = False
    gaps_allowed: bool = False
    poc_type: int = 2
    width_mbs: int = 4
    height_mbs: int = 4
    sps_id: int = 0


def _sps(sequence: _Sequence) -> bytes:
    writer = _BitWriter()
    writer.bits(sequence.profile_idc, 8)
    writer.bits(0, 8)
    writer.bits(20, 8)
    writer.ue(sequence.sps_id)
    if sequence.profile_idc in (44, 83, 86, 100, 110, 118, 122, 128, 134,
                                135, 138, 139, 144, 244):
        writer.ue(sequence.chroma_format_idc)
        if sequence.chroma_format_idc == 3:
            writer.bits(int(sequence.separate_colour_plane), 1)
        writer.ue(sequence.bit_depth_luma - 8)
        writer.ue(sequence.bit_depth_chroma - 8)
        writer.bits(0, 2)
    writer.ue(0)
    writer.ue(sequence.poc_type)
    if sequence.poc_type == 0:
        writer.ue(2)
    elif sequence.poc_type == 1:
        writer.bits(0, 1)
        writer.se(0)
        writer.se(0)
        writer.ue(0)
    writer.ue(2)
    writer.bits(int(sequence.gaps_allowed), 1)
    writer.ue(sequence.width_mbs - 1)
    height_units = (
        sequence.height_mbs
        if sequence.frame_mbs_only
        else sequence.height_mbs // 2
    )
    writer.ue(height_units - 1)
    writer.bits(int(sequence.frame_mbs_only), 1)
    if not sequence.frame_mbs_only:
        writer.bits(int(sequence.mb_adaptive_frame_field), 1)
    writer.bits(1, 1)
    writer.bits(0, 2)
    return writer.nal(0x67)


def _pps(
    sps_id: int = 0, redundant: bool = False, transform_8x8: bool = False,
    slice_groups: int = 0, map_type: int = 0,
) -> bytes:
    writer = _BitWriter()
    writer.ue(0)
    writer.ue(sps_id)
    writer.bits(0, 1)
    writer.bits(0, 1)
    writer.ue(slice_groups)
    if slice_groups:
        writer.ue(map_type)
        if map_type == 0:
            writer.ue(7)
            writer.ue(7)
    writer.ue(0)
    writer.ue(0)
    writer.bits(0, 1)
    writer.bits(0, 2)
    writer.se(0)
    writer.se(0)
    writer.se(0)
    writer.bits(1, 1)
    writer.bits(0, 1)
    writer.bits(int(redundant), 1)
    if transform_8x8:
        writer.bits(1, 1)
        writer.bits(0, 1)
        writer.se(0)
    return writer.nal(0x68)


def _slice_header(
    writer: _BitWriter, first_mb: int, sequence: _Sequence,
    idr: bool = True, field: int | None = None, colour_plane: int | None = None,
    frame_num: int = 0, pps_id: int = 0, redundant_pic_cnt: int | None = None,
    slice_type: int = 2, bottom_field_present: bool = False,
) -> None:
    writer.ue(first_mb)
    writer.ue(slice_type)
    writer.ue(pps_id)
    if sequence.separate_colour_plane:
        writer.bits(0 if colour_plane is None else colour_plane, 2)
    writer.bits(frame_num, 4)
    if not sequence.frame_mbs_only and field is not None:
        writer.bits(1, 1)
        writer.bits(field, 1)
    elif not sequence.frame_mbs_only:
        writer.bits(0, 1)
    if idr:
        writer.ue(0)
    if sequence.poc_type == 0:
        writer.bits(0, 6)
        if bottom_field_present:
            writer.se(0)
    if redundant_pic_cnt is not None:
        writer.ue(redundant_pic_cnt)
    if idr:
        writer.bits(0, 2)
    else:
        writer.bits(0, 1)
    writer.se(0)
    writer.ue(1)


def _pcm_macroblocks(
    writer: _BitWriter, sequence: _Sequence, count: int, field_mbaff: bool = False,
    colour_plane: int | None = None,
) -> None:
    for macroblock in range(count):
        if field_mbaff and macroblock % 2 == 0:
            writer.bits(macroblock // 2 % 2, 1)
        writer.ue(25)
        writer.align()
        planes = 1 if sequence.separate_colour_plane else 3
        for plane in range(planes):
            sample_count = 256
            depth = sequence.bit_depth_luma
            if not sequence.separate_colour_plane and plane:
                sample_count = {
                    1: 64,
                    2: 128,
                    3: 256,
                }[sequence.chroma_format_idc]
                depth = sequence.bit_depth_chroma
            value = (macroblock * 11 + 32 + plane * 47) & ((1 << depth) - 1)
            if depth == 8:
                writer.data.extend(bytes([value]) * sample_count)
                writer.bit_count += sample_count * 8
            else:
                for _ in range(sample_count):
                    writer.bits(value, depth)


def _idr_picture(
    sequence: _Sequence, idr: bool = True, field: int | None = None,
    colour_plane: int | None = None, first_mb: int = 0,
    macroblock_count: int | None = None, frame_num: int = 0,
    pps_id: int = 0, redundant_pic_cnt: int | None = None,
) -> bytes:
    writer = _BitWriter()
    _slice_header(
        writer, first_mb, sequence, idr, field, colour_plane, frame_num,
        pps_id, redundant_pic_cnt,
    )
    count = macroblock_count
    if count is None:
        count = sequence.width_mbs * sequence.height_mbs
        if field is not None:
            count //= 2
    _pcm_macroblocks(
        writer, sequence, count,
        not sequence.frame_mbs_only and sequence.mb_adaptive_frame_field,
        colour_plane,
    )
    return writer.nal(0x65 if idr else 0x21)


def _annex_b(*nals: bytes) -> bytes:
    return b"".join(b"\x00\x00\x00\x01" + nal for nal in nals)


def _aud(primary_picture_type: int = 0) -> bytes:
    writer = _BitWriter()
    writer.bits(primary_picture_type << 5, 8)
    return writer.nal(0x09)


def companion_streams() -> dict[str, bytes]:
    """Return the complete frame 40-49 corpus keyed by companion filename suffix."""
    chroma422 = _Sequence(122, 2, width_mbs=4, height_mbs=4)
    chroma444 = _Sequence(244, 3, width_mbs=4, height_mbs=4)
    separate444 = _Sequence(
        244, 3, separate_colour_plane=True, width_mbs=4, height_mbs=4
    )
    bit10 = _Sequence(110, 1, bit_depth_luma=10, bit_depth_chroma=10)
    bit12 = _Sequence(
        244, 3, bit_depth_luma=12, bit_depth_chroma=12
    )
    fields = _Sequence(
        100, 1, frame_mbs_only=False, mb_adaptive_frame_field=False
    )
    mbaff = _Sequence(
        100, 1, frame_mbs_only=False, mb_adaptive_frame_field=True
    )
    base = _Sequence(100)
    fmo = _annex_b(
        _sps(base), _pps(slice_groups=1, map_type=0),
        _idr_picture(base, macroblock_count=8),
        _idr_picture(base, first_mb=1, macroblock_count=8),
    )
    redundant = _annex_b(
        _sps(base), _pps(redundant=True), _aud(),
        _idr_picture(base, redundant_pic_cnt=0),
        _aud(),
        _idr_picture(
            base, idr=False, frame_num=1, redundant_pic_cnt=0
        ),
        _aud(),
        _idr_picture(
            base, idr=False, frame_num=1, redundant_pic_cnt=1
        ),
    )
    partitions = _annex_b(
        _sps(base), _pps(), _aud(), _idr_picture(base), _aud(),
        *(nal for mb in range(16) for nal in _partition_nals(base, mb)),
    )
    sp_si = _annex_b(
        _sps(_Sequence(88)), _pps(), _aud(), _idr_picture(_Sequence(88)),
        _aud(),
        _switching_slice(True),
        _aud(),
        _switching_slice(False),
    )
    extension_base = _annex_b(
        _sps(_Sequence(100)), _pps(), _idr_picture(_Sequence(100))
    )
    return {
        "frame-40-422.264": _annex_b(
            _sps(chroma422), _pps(), _idr_picture(chroma422)
        ),
        "frame-41-444.264": _annex_b(
            _sps(chroma444), _pps(transform_8x8=True),
            _idr_picture(chroma444),
        ),
        "frame-41-separate.264": _annex_b(
            _sps(separate444), _pps(transform_8x8=True),
            *(
                _idr_picture(
                    separate444, colour_plane=plane,
                )
                for plane in range(3)
            ),
        ),
        "frame-42-10bit.264": _annex_b(
            _sps(bit10), _pps(), _idr_picture(bit10)
        ),
        "frame-42-12bit.264": _annex_b(
            _sps(bit12), _pps(), _idr_picture(bit12)
        ),
        "frame-43-fields.264": _annex_b(
            _sps(fields), _pps(), _idr_picture(fields, field=0),
            _idr_picture(fields, idr=False, field=1, frame_num=1),
        ),
        "frame-44-mbaff.264": _annex_b(
            _sps(mbaff), _pps(), _idr_picture(mbaff)
        ),
        "frame-45-fmo.264": fmo,
        "frame-46-redundant.264": redundant,
        "frame-47-partitions.264": partitions,
        "frame-48-sp-si.264": sp_si,
        "frame-49-svc.264": extension_base + _extension_nal("svc"),
        "frame-49-mvc.264": extension_base + _extension_nal("mvc"),
        "frame-49-mvcd.264": extension_base + _extension_nal("mvcd"),
        "frame-49-3davc.264": extension_base + _extension_nal("3davc"),
    }


def companion_files() -> dict[str, bytes]:
    """Return companion files, using MP4 for AVC frames 40-48."""
    annex = companion_streams()
    sample_counts = {
        "frame-40-422.264": 1,
        "frame-41-444.264": 1,
        "frame-41-separate.264": 1,
        "frame-42-10bit.264": 1,
        "frame-42-12bit.264": 1,
        "frame-43-fields.264": 2,
        "frame-44-mbaff.264": 1,
        "frame-45-fmo.264": 1,
        "frame-46-redundant.264": 3,
        "frame-47-partitions.264": 2,
        "frame-48-sp-si.264": 3,
    }
    result = {}
    for annex_suffix, sample_count in sample_counts.items():
        suffix = annex_suffix.removesuffix(".264") + ".mp4"
        result[suffix] = _mux_avc_mp4(annex[annex_suffix], sample_count)
    for suffix in (
        "frame-49-svc.264", "frame-49-mvc.264", "frame-49-mvcd.264",
        "frame-49-3davc.264",
    ):
        result[suffix] = annex[suffix]
    return result


def _annex_b_nals(data: bytes) -> list[bytes]:
    starts = [
        index for index in range(len(data) - 3)
        if data[index:index + 4] == b"\x00\x00\x00\x01"
    ]
    return [
        data[start + 4:(starts[index + 1] if index + 1 < len(starts) else len(data))]
        for index, start in enumerate(starts)
    ]


def _mux_avc_mp4(annex: bytes, sample_count: int) -> bytes:
    from .mp4writer import _box, _full_box, _matrix, _track, _u16, _u32

    nals = _annex_b_nals(annex)
    sequence_sets = [nal for nal in nals if nal[0] & 0x1F == 7]
    picture_sets = [nal for nal in nals if nal[0] & 0x1F == 8]
    picture_nals = [
        nal for nal in nals if nal[0] & 0x1F != 7 and nal[0] & 0x1F != 8
    ]

    grouped: list[list[bytes]] = [[] for _ in range(sample_count)]
    if sample_count == 1:
        grouped[0] = picture_nals
    else:
        aud_groups: list[list[bytes]] = []
        current: list[bytes] = []
        for nal in picture_nals:
            if nal[0] & 0x1F == 9 and current:
                aud_groups.append(current)
                current = []
            current.append(nal)
        if current:
            aud_groups.append(current)
        if len(aud_groups) == sample_count:
            grouped = aud_groups
        else:
            slice_nals = [
                nal for nal in picture_nals if nal[0] & 0x1F in (1, 5)
            ]
            if len(slice_nals) != sample_count:
                raise ValueError("companion sample grouping is ambiguous")
            grouped = [[nal] for nal in slice_nals]

    samples = [
        b"".join(len(nal).to_bytes(4, "big") + nal for nal in group)
        for group in grouped
    ]
    if len(samples) != sample_count:
        raise ValueError("companion sample count does not match picture plan")
    sps = sequence_sets[0]
    avcc = _box(
        b"avcC", bytes([1, sps[1], sps[2], sps[3], 0xFF, 0xE1]),
        _u16(len(sps)), sps, bytes([len(picture_sets)]),
        *(_u16(len(pps)) + pps for pps in picture_sets),
    )
    visual = _box(
        b"avc1", bytes(6), _u16(1), bytes(16), _u16(64, 64),
        _u32(0x480000, 0x480000, 0), _u16(1), bytes(32),
        _u16(24, 0xFFFF), avcc,
    )
    timescale = 60
    sizes = _full_box(
        b"stsz", _u32(0, len(samples)), _u32(*(len(sample) for sample in samples))
    )
    ftyp = _box(b"ftyp", b"isom", _u32(0x200), b"isomiso2avc1mp41")
    video_offset = len(ftyp) + 8
    track = _track(
        1, visual, sizes, len(samples), timescale, sample_count,
        video_offset, True, tuple(range(1, sample_count + 1)),
    )
    data = b"".join(samples)
    movie_header = _full_box(
        b"mvhd", _u32(0, 0, 600, sample_count * 10),
        _u16(1, 0), bytes(8), _u32(0x00010000, 0x01000000, 0),
        bytes(36), _u32(0, 0, 0, 0, 0, 0), _u32(2),
    )
    movie_offset = len(ftyp) + len(movie_header) + len(track) + 32
    if movie_offset != len(ftyp) + 8:
        track = _track(
            1, visual, sizes, len(samples), timescale, sample_count,
            movie_offset, True, tuple(range(1, sample_count + 1)),
        )
    movie = _box(b"moov", movie_header, track)
    video_offset = len(ftyp) + len(movie) + 8
    track = _track(
        1, visual, sizes, len(samples), timescale, sample_count,
        video_offset, True, tuple(range(1, sample_count + 1)),
    )
    movie = _box(b"moov", movie_header, track)
    return ftyp + movie + _box(b"mdat", data)


def _partition_nals(sequence: _Sequence, mb: int) -> tuple[bytes, bytes, bytes]:
    writer = _BitWriter()
    writer.ue(mb)
    writer.ue(2)
    writer.ue(0)
    writer.bits(1, 4)
    writer.bits(0, 1)
    writer.se(0)
    writer.ue(1)
    writer.ue(mb)
    writer.ue(25)
    partition_a = writer.nal(0x42)

    writer = _BitWriter()
    writer.ue(mb)
    writer.align()
    for value in (112 + mb, 144 + mb, 160 + mb):
        count = 256 if value == 112 + mb else 64
        writer.data.extend(bytes([value]) * count)
        writer.bit_count += count * 8
    partition_b = writer.nal(0x43)

    writer = _BitWriter()
    writer.ue(mb)
    partition_c = writer.nal(0x44)
    return partition_a, partition_b, partition_c


def _switching_slice(sp: bool) -> bytes:
    writer = _BitWriter()
    writer.ue(0)
    writer.ue(3 if sp else 4)
    writer.ue(0)
    writer.bits(1 if sp else 2, 4)
    if sp:
        writer.bits(0, 1)
        writer.bits(0, 1)
    writer.bits(0, 1)
    writer.se(0)
    if sp:
        writer.bits(0, 1)
    writer.se(0)
    writer.ue(1)
    if sp:
        writer.ue(16)
    else:
        for _ in range(16):
            writer.ue(26)
            writer.align()
            writer.data.extend(bytes([128]) * 384)
            writer.bit_count += 384 * 8
    return writer.nal(0x21)


def _extension_nal(family: str) -> bytes:
    if family == "svc":
        prefix = b"\x6e\x80"
        coded = b"\x74\x80\x00\x00\x80"
    elif family == "mvc":
        prefix = b""
        coded = b"\x74\x00\x00\x00\x80"
    elif family == "mvcd":
        prefix = b""
        coded = b"\x75\x80\x00\x00\x80"
    else:
        prefix = b""
        coded = b"\x75\xc0\x00\x00\x80"
    return (
        (b"\x00\x00\x00\x01" + prefix if prefix else b"")
        + b"\x00\x00\x00\x01" + coded
    )

