import pytest

from pygorvid import (
    InverseScaleError,
    InverseTransformError,
    InterPredictionError,
    IntraPredictionError,
    MotionVector,
    MotionVectorCandidate,
    MotionVectorPartitionShape,
    POCType0State,
    POCType12State,
    PictureOrderCountError,
    DeblockingError,
    LumaDeblockingMode,
    LumaDeblockingMacroblock,
    LumaDeblockingEdgeFlags,
    LumaDeblockingNeighbors,
    LumaDeblockingParameters,
    LumaDeblockingThresholds,
    LumaEdgeSamples,
    LumaStrongEdgeSamples,
    LumaPredictionVector,
    derive_luma_deblocking_parameters,
    apply_luma_deblocking_edge,
    apply_luma_deblocking_edge_segment,
    apply_luma_deblocking_plane_edge_segment,
    apply_luma_deblocking_plane_macroblock_edge,
    apply_luma_deblocking_macroblock,
    derive_luma_deblocking_edge_flags,
    derive_luma_deblocking_neighbors,
        resolve_luma_deblocking_macroblock,
    derive_luma_boundary_strength,
    derive_luma_boundary_strength_from_predictions,
    luma_inter_prediction_differs,
    filter_luma_weak_edge,
    filter_luma_strong_edge,
    lookup_luma_deblocking_thresholds,
            lookup_luma_tc0,
        should_filter_luma_edge,
    PresentationOrderBuffer,
    PresentationPicture,
    RefPicListModification,
    ReferencePicture,
    ReferencePictureBuffer,
    Yuv420Frame,
    apply_motion_vector_difference,
    apply_reference_list_modifications,
    build_b_reference_lists,
    build_p_reference_list,
    derive_motion_vector,
    motion_vector_difference_neighbor_magnitudes,
    inverse_scale_luma4x4,
    inverse_scale_luma8x8,
    inverse_transform_luma4x4,
    inverse_transform_luma8x8,
    interpolate_luma_half_sample_horizontal,
    interpolate_luma_half_sample_diagonal,
    interpolate_luma_half_sample_vertical,
    interpolate_luma_quarter_sample_average,
    interpolate_luma_quarter_sample_pair,
    interpolate_luma_quarter_sample_diagonal,
    interpolate_luma_quarter_sample_axial,
    interpolate_luma_quarter_sample_around_j,
    interpolate_luma_quarter_sample_grid,
    select_luma_fractional_sample,
    gather_luma_quarter_sample_neighborhood,
        interpolate_luma_fractional_sample,
    predict_luma_intra4x4_horizontal,
    predict_luma_intra8x8_horizontal,
    predict_luma_intra8x8_dc,
    predict_luma_intra8x8_diagonal_down_left,
    predict_luma_intra8x8_diagonal_down_right,
    predict_luma_intra8x8_vertical_right,
    predict_luma_intra8x8_horizontal_down,
    predict_luma_intra8x8_vertical_left,
    predict_luma_intra8x8_horizontal_up,
    predict_luma_intra8x8_plane,
    predict_luma_intra4x4_vertical,
    predict_luma_intra8x8_vertical,
    predict_luma_intra16x16_vertical,
    predict_luma_intra16x16_horizontal,
    predict_luma_intra16x16_dc,
    predict_luma_intra16x16_plane,
    predict_motion_vector,
    predict_motion_vector_for_partition,
    predict_luma_intra4x4_dc,
    predict_luma_intra4x4_diagonal_down_left,
    predict_luma_intra4x4_diagonal_down_right,
    predict_luma_intra4x4_vertical_right,
    predict_luma_intra4x4_horizontal_down,
    predict_luma_intra4x4_vertical_left,
    predict_luma_intra4x4_horizontal_up,
)


@pytest.mark.parametrize(
    ("index_a", "index_b", "expected"),
    (
        (0, 0, LumaDeblockingThresholds(0, 0)),
        (16, 17, LumaDeblockingThresholds(4, 0)),
        (18, 18, LumaDeblockingThresholds(5, 2)),
        (26, 26, LumaDeblockingThresholds(15, 4)),
        (40, 40, LumaDeblockingThresholds(80, 12)),
        (51, 51, LumaDeblockingThresholds(255, 18)),
    ),
)
def test_lookup_luma_deblocking_thresholds_matches_table_vectors(index_a, index_b, expected):
    assert lookup_luma_deblocking_thresholds(index_a, index_b) == expected


@pytest.mark.parametrize(("index_a", "index_b"), ((-1, 0), (0, 52), (True, 0)))
def test_lookup_luma_deblocking_thresholds_rejects_invalid_indices(index_a, index_b):
    with pytest.raises(DeblockingError, match="table index is outside"):
        lookup_luma_deblocking_thresholds(index_a, index_b)


@pytest.mark.parametrize(
    ("index_a", "boundary_strength", "expected"),
    (
        (0, 1, 0),
        (23, 1, 1),
        (21, 2, 1),
        (18, 3, 1),
        (33, 1, 2),
        (51, 1, 13),
        (51, 2, 17),
        (51, 3, 25),
    ),
)
def test_lookup_luma_tc0_matches_table_vectors(index_a, boundary_strength, expected):
    assert lookup_luma_tc0(index_a, boundary_strength) == expected


@pytest.mark.parametrize(("index_a", "boundary_strength"), ((52, 1), (0, 0), (0, 4), (True, 1)))
def test_lookup_luma_tc0_rejects_invalid_indices(index_a, boundary_strength):
    with pytest.raises(DeblockingError, match="luma tC0 table index is invalid"):
        lookup_luma_tc0(index_a, boundary_strength)


@pytest.mark.parametrize(
    ("boundary_strength", "thresholds", "expected"),
    (
        (1, LumaDeblockingThresholds(5, 11), True),
        (4, LumaDeblockingThresholds(5, 11), True),
        (0, LumaDeblockingThresholds(255, 255), False),
        (1, LumaDeblockingThresholds(4, 11), False),
        (1, LumaDeblockingThresholds(5, 10), False),
        (1, LumaDeblockingThresholds(5, 9), False),
    ),
)
def test_should_filter_luma_edge_applies_strict_thresholds(boundary_strength, thresholds, expected):
    assert should_filter_luma_edge(100, 104, 90, 113, boundary_strength, thresholds) is expected


@pytest.mark.parametrize(
    ("p0", "q0", "p1", "q1", "boundary_strength", "thresholds"),
    (
        (256, 104, 90, 113, 1, LumaDeblockingThresholds(5, 11)),
        (100, 104, 90, 113, 5, LumaDeblockingThresholds(5, 11)),
        (100, 104, 90, 113, 1, LumaDeblockingThresholds(256, 11)),
    ),
)
def test_should_filter_luma_edge_rejects_invalid_inputs(
    p0, q0, p1, q1, boundary_strength, thresholds
):
    with pytest.raises(DeblockingError, match="luma edge-filter inputs are invalid"):
        should_filter_luma_edge(p0, q0, p1, q1, boundary_strength, thresholds)


@pytest.mark.parametrize(
    ("samples", "beta", "tc0", "expected"),
    (
        (
            LumaEdgeSamples(100, 98, 96, 104, 105, 106),
            10,
            2,
            LumaEdgeSamples(101, 99, 96, 103, 104, 106),
        ),
        (
            LumaEdgeSamples(110, 110, 120, 100, 100, 90),
            5,
            1,
            LumaEdgeSamples(109, 110, 120, 101, 100, 90),
        ),
    ),
)
def test_filter_luma_weak_edge_matches_normative_vectors(samples, beta, tc0, expected):
    assert filter_luma_weak_edge(samples, beta, tc0) == expected


def test_filter_luma_weak_edge_rejects_invalid_input():
    with pytest.raises(DeblockingError, match="luma weak-edge filter inputs are invalid"):
        filter_luma_weak_edge(LumaEdgeSamples(-1, 0, 0, 0, 0, 0), 1, 1)


@pytest.mark.parametrize(
    ("samples", "alpha", "beta", "expected"),
    (
        (LumaStrongEdgeSamples(100, 99, 98, 97, 102, 103, 104, 105), 16, 5,
         LumaEdgeSamples(100, 100, 99, 102, 102, 103)),
        (LumaStrongEdgeSamples(100, 99, 98, 97, 102, 103, 130, 120), 16, 5,
         LumaEdgeSamples(100, 100, 99, 102, 103, 130)),
        (LumaStrongEdgeSamples(100, 99, 130, 120, 102, 103, 104, 105), 16, 5,
         LumaEdgeSamples(100, 99, 130, 102, 102, 103)),
        (LumaStrongEdgeSamples(100, 99, 98, 97, 102, 103, 104, 105), 0, 5,
         LumaEdgeSamples(100, 99, 98, 102, 103, 104)),
    ),
)
def test_filter_luma_strong_edge_selects_strong_and_fallback_branches(samples, alpha, beta, expected):
    assert filter_luma_strong_edge(samples, alpha, beta) == expected


@pytest.mark.parametrize(
    ("boundary_strength", "parameters", "slice_boundary", "expected"),
    (
        (
            2,
            LumaDeblockingParameters(LumaDeblockingMode.ALL_EDGES, 33, 40),
            False,
            LumaEdgeSamples(101, 99, 96, 103, 104, 106),
        ),
        (
            4,
            LumaDeblockingParameters(LumaDeblockingMode.ALL_EDGES, 26, 30),
            False,
            LumaEdgeSamples(101, 100, 98, 103, 104, 105),
        ),
        (
            1,
            LumaDeblockingParameters(LumaDeblockingMode.ALL_EDGES, 0, 0),
            False,
            LumaEdgeSamples(100, 98, 96, 104, 105, 106),
        ),
        (
            4,
            LumaDeblockingParameters(LumaDeblockingMode.DISABLED, 26, 30),
            False,
            LumaEdgeSamples(100, 98, 96, 104, 105, 106),
        ),
        (
            4,
            LumaDeblockingParameters(LumaDeblockingMode.ALL_EXCEPT_SLICE_BOUNDARIES, 26, 30),
            True,
            LumaEdgeSamples(100, 98, 96, 104, 105, 106),
        ),
        (
            4,
            LumaDeblockingParameters(LumaDeblockingMode.ALL_EXCEPT_SLICE_BOUNDARIES, 26, 30),
            False,
            LumaEdgeSamples(101, 100, 98, 103, 104, 105),
        ),
        (
            4,
            LumaDeblockingParameters(LumaDeblockingMode.ALL_EDGES, 26, 30),
            True,
            LumaEdgeSamples(101, 100, 98, 103, 104, 105),
        ),
        (
            0,
            LumaDeblockingParameters(LumaDeblockingMode.ALL_EDGES, 26, 30),
            False,
            LumaEdgeSamples(100, 98, 96, 104, 105, 106),
        ),
    ),
)
def test_apply_luma_deblocking_edge_selects_filter_and_honors_mode(
    boundary_strength, parameters, slice_boundary, expected
):
    samples = LumaStrongEdgeSamples(100, 98, 96, 95, 104, 105, 106, 107)
    assert apply_luma_deblocking_edge(samples, boundary_strength, parameters, slice_boundary) == expected


def test_apply_luma_deblocking_edge_segment_preserves_order_and_mode():
    samples = (
        LumaStrongEdgeSamples(100, 98, 96, 95, 104, 105, 106, 107),
        LumaStrongEdgeSamples(100, 99, 98, 97, 102, 103, 130, 120),
        LumaStrongEdgeSamples(100, 98, 96, 95, 120, 121, 122, 123),
        LumaStrongEdgeSamples(100, 98, 96, 95, 104, 105, 106, 107),
    )
    parameters = LumaDeblockingParameters(LumaDeblockingMode.ALL_EXCEPT_SLICE_BOUNDARIES, 26, 30)
    assert apply_luma_deblocking_edge_segment(samples, 4, parameters, False) == (
        LumaEdgeSamples(101, 100, 98, 103, 104, 105),
        LumaEdgeSamples(100, 100, 99, 102, 103, 130),
        LumaEdgeSamples(100, 98, 96, 120, 121, 122),
        LumaEdgeSamples(101, 100, 98, 103, 104, 105),
    )
    assert apply_luma_deblocking_edge_segment(samples, 4, parameters, True) == tuple(
        LumaEdgeSamples(item.p0, item.p1, item.p2, item.q0, item.q1, item.q2)
        for item in samples
    )


def test_apply_luma_deblocking_edge_segment_rejects_wrong_sample_count():
    with pytest.raises(DeblockingError, match="must contain exactly four sample sets"):
        apply_luma_deblocking_edge_segment(
            (), 4, LumaDeblockingParameters(LumaDeblockingMode.ALL_EDGES, 0, 0), False
        )


@pytest.mark.parametrize("vertical", (True, False))
def test_apply_luma_deblocking_plane_edge_segment_handles_orientation_and_bounds(vertical):
    width, height, stride = 12, 12, 15
    x, y = (6, 2) if vertical else (2, 6)
    plane = bytearray([250]) * (stride * height)
    inputs = (
        LumaStrongEdgeSamples(100, 98, 96, 95, 104, 105, 106, 107),
        LumaStrongEdgeSamples(100, 99, 98, 97, 102, 103, 130, 120),
        LumaStrongEdgeSamples(100, 98, 96, 95, 120, 121, 122, 123),
        LumaStrongEdgeSamples(100, 98, 96, 95, 104, 105, 106, 107),
    )
    expected = (
        LumaEdgeSamples(101, 100, 98, 103, 104, 105),
        LumaEdgeSamples(100, 100, 99, 102, 103, 130),
        LumaEdgeSamples(100, 98, 96, 120, 121, 122),
        LumaEdgeSamples(101, 100, 98, 103, 104, 105),
    )
    for lane, samples in enumerate(inputs):
        q0_index = (y + lane) * stride + x if vertical else y * stride + x + lane
        offsets = (-4, -3, -2, -1, 0, 1, 2, 3) if vertical else tuple(
            offset * stride for offset in (-4, -3, -2, -1, 0, 1, 2, 3)
        )
        for offset, value in zip(
            offsets,
            (
                samples.p3,
                samples.p2,
                samples.p1,
                samples.p0,
                samples.q0,
                samples.q1,
                samples.q2,
                samples.q3,
            ),
        ):
            plane[q0_index + offset] = value

    parameters = LumaDeblockingParameters(LumaDeblockingMode.ALL_EDGES, 26, 30)
    apply_luma_deblocking_plane_edge_segment(
        plane, width, height, stride, x, y, vertical, 4, parameters, False
    )
    for lane, expected_edge in enumerate(expected):
        q0_index = (y + lane) * stride + x if vertical else y * stride + x + lane
        if vertical:
            actual = LumaEdgeSamples(
                plane[q0_index - 1],
                plane[q0_index - 2],
                plane[q0_index - 3],
                plane[q0_index],
                plane[q0_index + 1],
                plane[q0_index + 2],
            )
            assert plane[q0_index - 4] == inputs[lane].p3
            assert plane[q0_index + 3] == inputs[lane].q3
        else:
            actual = LumaEdgeSamples(
                plane[q0_index - stride],
                plane[q0_index - 2 * stride],
                plane[q0_index - 3 * stride],
                plane[q0_index],
                plane[q0_index + stride],
                plane[q0_index + 2 * stride],
            )
            assert plane[q0_index - 4 * stride] == inputs[lane].p3
            assert plane[q0_index + 3 * stride] == inputs[lane].q3
        assert actual == expected_edge
    assert all(plane[row * stride + width : (row + 1) * stride] == bytes([250]) * (stride - width) for row in range(height))

    before = plane[:]
    with pytest.raises(DeblockingError, match="plane layout or edge coordinates are invalid"):
        apply_luma_deblocking_plane_edge_segment(
            plane, width, height, stride, 3, 2, True, 4, parameters, False
        )
    assert plane == before


@pytest.mark.parametrize("vertical", (True, False))
def test_apply_luma_deblocking_plane_macroblock_edge_uses_per_segment_strengths(vertical):
    width, height, stride = 24, 24, 27
    x, y = (8, 2) if vertical else (2, 8)
    plane = bytearray([250]) * (stride * height)
    samples = LumaStrongEdgeSamples(100, 98, 96, 95, 104, 105, 106, 107)
    for lane in range(16):
        q0_index = (y + lane) * stride + x if vertical else y * stride + x + lane
        offsets = (-4, -3, -2, -1, 0, 1, 2, 3) if vertical else tuple(
            offset * stride for offset in (-4, -3, -2, -1, 0, 1, 2, 3)
        )
        for offset, value in zip(
            offsets,
            (samples.p3, samples.p2, samples.p1, samples.p0, samples.q0, samples.q1, samples.q2, samples.q3),
        ):
            plane[q0_index + offset] = value

    parameters = LumaDeblockingParameters(LumaDeblockingMode.ALL_EDGES, 33, 40)
    strengths = (4, 0, 2, 0)
    apply_luma_deblocking_plane_macroblock_edge(
        plane, width, height, stride, x, y, vertical, strengths, parameters, False
    )
    expected_by_strength = {
        4: LumaEdgeSamples(101, 100, 98, 103, 104, 105),
        0: LumaEdgeSamples(100, 98, 96, 104, 105, 106),
        2: LumaEdgeSamples(101, 99, 96, 103, 104, 106),
    }
    for lane in range(16):
        q0_index = (y + lane) * stride + x if vertical else y * stride + x + lane
        if vertical:
            actual = LumaEdgeSamples(
                plane[q0_index - 1], plane[q0_index - 2], plane[q0_index - 3],
                plane[q0_index], plane[q0_index + 1], plane[q0_index + 2],
            )
        else:
            actual = LumaEdgeSamples(
                plane[q0_index - stride], plane[q0_index - 2 * stride], plane[q0_index - 3 * stride],
                plane[q0_index], plane[q0_index + stride], plane[q0_index + 2 * stride],
            )
        assert actual == expected_by_strength[strengths[lane // 4]]

    before = plane[:]
    with pytest.raises(DeblockingError, match="luma macroblock edge inputs are invalid"):
        apply_luma_deblocking_plane_macroblock_edge(
            plane, width, height, stride, x, y, vertical, (4, 0, 5, 0), parameters, False
        )
    assert plane == before


@pytest.mark.parametrize("transform_size_8x8", (False, True))
def test_apply_luma_deblocking_macroblock_uses_normative_order(transform_size_8x8):
    width, height, stride = 32, 32, 35
    parameters = LumaDeblockingParameters(LumaDeblockingMode.ALL_EDGES, 40, 40)
    macroblock = LumaDeblockingMacroblock(
        x=8,
        y=8,
        left_neighbor_available=True,
        top_neighbor_available=True,
        transform_size_8x8=transform_size_8x8,
        left_slice_boundary=False,
        top_slice_boundary=False,
        left_strengths=(4, 3, 2, 1),
        top_strengths=(1, 2, 3, 4),
        vertical_internal_strengths=((1, 2, 3, 4), (4, 3, 2, 1), (2, 1, 4, 3)),
        horizontal_internal_strengths=((4, 1, 3, 2), (2, 4, 1, 3), (3, 2, 4, 1)),
    )

    def make_plane():
        plane = bytearray(stride * height)
        for row in range(height):
            for column in range(width):
                plane[row * stride + column] = (column * 7 + row * 11 + column * row) % 180 + 30
            plane[row * stride + width : (row + 1) * stride] = bytes([251]) * (stride - width)
        return plane

    actual = make_plane()
    expected = make_plane()
    apply_luma_deblocking_plane_macroblock_edge(
        expected, width, height, stride, macroblock.x, macroblock.y, True,
        macroblock.left_strengths, parameters, False,
    )
    for edge in range(3):
        if not transform_size_8x8 or edge == 1:
            apply_luma_deblocking_plane_macroblock_edge(
                expected, width, height, stride, macroblock.x + 4 * (edge + 1), macroblock.y, True,
                macroblock.vertical_internal_strengths[edge], parameters, False,
            )
    apply_luma_deblocking_plane_macroblock_edge(
        expected, width, height, stride, macroblock.x, macroblock.y, False,
        macroblock.top_strengths, parameters, False,
    )
    for edge in range(3):
        if not transform_size_8x8 or edge == 1:
            apply_luma_deblocking_plane_macroblock_edge(
                expected, width, height, stride, macroblock.x, macroblock.y + 4 * (edge + 1), False,
                macroblock.horizontal_internal_strengths[edge], parameters, False,
            )
    apply_luma_deblocking_macroblock(actual, width, height, stride, macroblock, parameters)
    assert actual == expected

    before = actual[:]
    invalid = LumaDeblockingMacroblock(
        **{
            **macroblock.__dict__,
            "horizontal_internal_strengths": (
                (4, 1, 3, 2), (2, 4, 1, 3), (3, 2, 5, 1)
            ),
        }
    )
    with pytest.raises(DeblockingError, match="luma boundary strength is outside"):
        apply_luma_deblocking_macroblock(actual, width, height, stride, invalid, parameters)
    assert actual == before


@pytest.mark.parametrize(
    ("mode", "left_available", "top_available", "left_slice", "top_slice", "expected"),
    (
        (LumaDeblockingMode.ALL_EDGES, True, True, True, True, LumaDeblockingEdgeFlags(True, True, True)),
        (LumaDeblockingMode.DISABLED, True, True, False, False, LumaDeblockingEdgeFlags(False, False, False)),
        (LumaDeblockingMode.ALL_EXCEPT_SLICE_BOUNDARIES, True, True, False, False, LumaDeblockingEdgeFlags(True, True, True)),
        (LumaDeblockingMode.ALL_EXCEPT_SLICE_BOUNDARIES, True, True, True, False, LumaDeblockingEdgeFlags(False, True, True)),
        (LumaDeblockingMode.ALL_EDGES, False, False, False, False, LumaDeblockingEdgeFlags(False, False, True)),
    ),
)
def test_derive_luma_deblocking_edge_flags(mode, left_available, top_available, left_slice, top_slice, expected):
    assert derive_luma_deblocking_edge_flags(
        mode, left_available, top_available, left_slice, top_slice
    ) == expected


@pytest.mark.parametrize(
    ("macroblock_index", "expected"),
    (
        (0, LumaDeblockingNeighbors(None, None, False, False, False, False)),
        (1, LumaDeblockingNeighbors(0, None, True, False, False, False)),
        (2, LumaDeblockingNeighbors(None, 0, False, True, False, True)),
        (3, LumaDeblockingNeighbors(2, 1, True, True, True, True)),
    ),
)
def test_derive_luma_deblocking_neighbors_from_raster_address_and_slice_ids(
    macroblock_index, expected
):
    assert derive_luma_deblocking_neighbors(macroblock_index, 2, 2, (1, 1, 2, 3)) == expected


@pytest.mark.parametrize(
    ("macroblock_index", "width", "height", "slice_ids"),
    ((-1, 2, 2, (1, 1, 2, 3)), (4, 2, 2, (1, 1, 2, 3)), (0, 2, 2, (1, 1, 2))),
)
def test_derive_luma_deblocking_neighbors_rejects_invalid_picture_maps(
    macroblock_index, width, height, slice_ids
):
    with pytest.raises(DeblockingError, match="macroblock address or slice map is invalid"):
        derive_luma_deblocking_neighbors(macroblock_index, width, height, slice_ids)


def test_resolve_luma_deblocking_macroblock_from_raster_address():
    template = LumaDeblockingMacroblock(
        x=0,
        y=0,
        left_neighbor_available=False,
        top_neighbor_available=False,
        transform_size_8x8=True,
        left_slice_boundary=False,
        top_slice_boundary=False,
        left_strengths=(1, 2, 3, 4),
        top_strengths=(0, 0, 0, 0),
        vertical_internal_strengths=((0, 0, 0, 0),) * 3,
        horizontal_internal_strengths=((0, 0, 0, 0),) * 3,
    )
    resolved_origin = resolve_luma_deblocking_macroblock(0, 2, 2, (1, 1, 2, 3), template)
    assert (resolved_origin.x, resolved_origin.y, resolved_origin.left_neighbor_available, resolved_origin.top_neighbor_available) == (0, 0, False, False)
    resolved_last = resolve_luma_deblocking_macroblock(3, 2, 2, (1, 1, 2, 3), template)
    assert (resolved_last.x, resolved_last.y) == (16, 16)
    assert resolved_last.left_neighbor_available and resolved_last.top_neighbor_available
    assert resolved_last.left_slice_boundary and resolved_last.top_slice_boundary
    assert resolved_last.left_strengths == template.left_strengths


@pytest.mark.parametrize(
    ("boundary_strength", "parameters", "slice_boundary", "message"),
    (
        (
            5,
            LumaDeblockingParameters(LumaDeblockingMode.ALL_EDGES, 0, 0),
            False,
            r"luma deblocking boundary strength is outside \[0,4\]",
        ),
        (
            -1,
            LumaDeblockingParameters(LumaDeblockingMode.ALL_EDGES, 0, 0),
            False,
            r"luma deblocking boundary strength is outside \[0,4\]",
        ),
        (
            1,
            LumaDeblockingParameters(LumaDeblockingMode.ALL_EDGES, 52, 0),
            False,
            "luma deblocking edge inputs are invalid",
        ),
        (
            1,
            LumaDeblockingParameters(LumaDeblockingMode.ALL_EDGES, 0, 0),
            1,
            "luma deblocking edge inputs are invalid",
        ),
    ),
)
def test_apply_luma_deblocking_edge_rejects_invalid_inputs(
    boundary_strength, parameters, slice_boundary, message
):
    samples = LumaStrongEdgeSamples(0, 0, 0, 0, 0, 0, 0, 0)
    with pytest.raises(DeblockingError, match=message):
        apply_luma_deblocking_edge(samples, boundary_strength, parameters, slice_boundary)


@pytest.mark.parametrize(
    ("macroblock_edge", "either_intra", "either_has_coefficients", "inter_prediction_differs", "expected"),
    (
        (True, True, True, True, 4),
        (False, True, False, False, 3),
        (False, False, True, True, 2),
        (False, False, False, True, 1),
        (False, False, False, False, 0),
    ),
)
def test_derive_luma_boundary_strength_uses_normative_priority(
    macroblock_edge, either_intra, either_has_coefficients, inter_prediction_differs, expected
):
    assert derive_luma_boundary_strength(
        macroblock_edge, either_intra, either_has_coefficients, inter_prediction_differs
    ) == expected


def test_derive_luma_boundary_strength_rejects_non_boolean_inputs():
    with pytest.raises(DeblockingError, match="luma boundary-strength inputs are invalid"):
        derive_luma_boundary_strength(1, False, False, False)


@pytest.mark.parametrize(
    ("prediction_p", "prediction_q", "expected"),
    (
        ((), (), False),
        (
            (LumaPredictionVector(7, MotionVector(-3, 3)),),
            (LumaPredictionVector(7, MotionVector(0, 0)),),
            False,
        ),
        ((LumaPredictionVector(7, MotionVector(0, 0)),), (LumaPredictionVector(8, MotionVector(0, 0)),), True),
        ((LumaPredictionVector(7, MotionVector(0, 0)),), (LumaPredictionVector(7, MotionVector(4, 0)),), True),
        ((LumaPredictionVector(7, MotionVector(0, 0)),), (LumaPredictionVector(7, MotionVector(0, -4)),), True),
        (
            (LumaPredictionVector(7, MotionVector(12, 0)), LumaPredictionVector(8, MotionVector(0, 20))),
            (LumaPredictionVector(8, MotionVector(0, 22)), LumaPredictionVector(7, MotionVector(10, 0))),
            False,
        ),
        (
            (LumaPredictionVector(7, MotionVector(0, 0)), LumaPredictionVector(8, MotionVector(0, 0))),
            (LumaPredictionVector(8, MotionVector(0, 0)), LumaPredictionVector(7, MotionVector(0, 4))),
            True,
        ),
        ((LumaPredictionVector(7, MotionVector(0, 0)),), (), True),
    ),
)
def test_luma_inter_prediction_differs_compares_references_and_motion_vectors(
    prediction_p, prediction_q, expected
):
    assert luma_inter_prediction_differs(prediction_p, prediction_q) is expected


def test_luma_inter_prediction_differs_rejects_unsupported_vector_count():
    with pytest.raises(DeblockingError, match="supports at most two vectors"):
        luma_inter_prediction_differs(
            tuple(LumaPredictionVector(1, MotionVector(0, 0)) for _ in range(3)), ()
        )


@pytest.mark.parametrize(
    ("macroblock_edge", "either_intra", "coefficients", "prediction_p", "prediction_q", "expected"),
    (
        (True, True, False, (LumaPredictionVector(1, MotionVector(0, 0)),), (LumaPredictionVector(2, MotionVector(4, 0)),), 4),
        (False, False, True, (LumaPredictionVector(1, MotionVector(0, 0)),), (LumaPredictionVector(2, MotionVector(4, 0)),), 2),
        (False, False, False, (LumaPredictionVector(1, MotionVector(0, 0)),), (LumaPredictionVector(2, MotionVector(0, 0)),), 1),
        (False, False, False, (LumaPredictionVector(1, MotionVector(0, 0)),), (LumaPredictionVector(1, MotionVector(3, -3)),), 0),
    ),
)
def test_derive_luma_boundary_strength_from_predictions(
    macroblock_edge, either_intra, coefficients, prediction_p, prediction_q, expected
):
    assert derive_luma_boundary_strength_from_predictions(
        macroblock_edge, either_intra, coefficients, prediction_p, prediction_q
    ) == expected


def test_derive_luma_boundary_strength_from_predictions_propagates_comparison_error():
    too_many = tuple(LumaPredictionVector(1, MotionVector(0, 0)) for _ in range(3))
    with pytest.raises(DeblockingError, match="supports at most two vectors"):
        derive_luma_boundary_strength_from_predictions(False, False, False, too_many, ())


@pytest.mark.parametrize(
    ("qp_p", "qp_q", "disable_idc", "alpha_offset", "beta_offset", "expected"),
    (
        (20, 21, 0, -3, 2, (LumaDeblockingMode.ALL_EDGES, 15, 25)),
        (0, 1, 2, -6, -1, (LumaDeblockingMode.ALL_EXCEPT_SLICE_BOUNDARIES, 0, 0)),
        (51, 50, 1, 6, 2, (LumaDeblockingMode.DISABLED, 51, 51)),
    ),
)
def test_derive_luma_deblocking_parameters_clips_indices_and_maps_mode(
    qp_p, qp_q, disable_idc, alpha_offset, beta_offset, expected
):
    parameters = derive_luma_deblocking_parameters(qp_p, qp_q, disable_idc, alpha_offset, beta_offset)
    assert (parameters.mode, parameters.index_a, parameters.index_b) == expected


@pytest.mark.parametrize(
    ("qp_p", "qp_q", "disable_idc", "alpha_offset", "beta_offset"),
    (
        (-1, 26, 0, 0, 0),
        (26, 52, 0, 0, 0),
        (26, 26, 3, 0, 0),
        (26, 26, 0, -7, 0),
        (26, 26, 0, 7, 0),
        (26, 26, 0, 0, -7),
        (26, 26, 0, 0, 7),
        (True, 26, 0, 0, 0),
    ),
)
def test_derive_luma_deblocking_parameters_rejects_invalid_inputs(
    qp_p, qp_q, disable_idc, alpha_offset, beta_offset
):
    with pytest.raises(DeblockingError, match="luma deblocking parameters are invalid"):
        derive_luma_deblocking_parameters(qp_p, qp_q, disable_idc, alpha_offset, beta_offset)


@pytest.mark.parametrize(
    ("previous_msb", "previous_lsb", "lsb", "delta", "idr", "expected", "next_msb", "next_lsb"),
    (
        (0, 0, 3, 0, False, 3, 0, 3),
        (0, 15, 7, 0, False, 23, 16, 7),
        (16, 1, 10, 0, False, 10, 0, 10),
        (0, 1, 9, 0, False, 9, 0, 9),
        (0, 0, 5, -8, False, -3, 0, 5),
        (64, 15, 2, 0, True, 2, 0, 2),
        (0, -1, 2, 0, True, 2, 0, 2),
    ),
)
def test_poc_type0_wraps_lsb_and_updates_reference_state(
    previous_msb, previous_lsb, lsb, delta, idr, expected, next_msb, next_lsb
):
    state = POCType0State(previous_msb, previous_lsb)
    assert state.calculate(16, lsb, delta, 1, idr) == expected
    assert (state.previous_pic_order_cnt_msb, state.previous_pic_order_cnt_lsb) == (
        next_msb,
        next_lsb,
    )


def test_poc_type0_non_reference_does_not_advance_state():
    state = POCType0State(16, 14)
    assert state.calculate(16, 1, nal_ref_idc=0) == 33
    assert (state.previous_pic_order_cnt_msb, state.previous_pic_order_cnt_lsb) == (16, 14)


@pytest.mark.parametrize(
    ("max_lsb", "lsb", "nal_ref_idc", "idr"),
    ((15, 0, 1, False), (24, 0, 1, False), (16, 16, 1, False), (16, 0, 4, False), (16, 0, 0, True)),
)
def test_poc_type0_rejects_invalid_inputs_without_changing_state(
    max_lsb, lsb, nal_ref_idc, idr
):
    state = POCType0State(7, 2)
    with pytest.raises(PictureOrderCountError, match="picture order count input"):
        state.calculate(max_lsb, lsb, nal_ref_idc=nal_ref_idc, idr=idr)
    assert (state.previous_pic_order_cnt_msb, state.previous_pic_order_cnt_lsb) == (7, 2)


def test_poc_type0_rejects_signed_64_bit_overflow_without_changing_state():
    state = POCType0State((1 << 63) - 1, 15)
    with pytest.raises(PictureOrderCountError, match="picture order count input"):
        state.calculate(16, 0)
    assert (state.previous_pic_order_cnt_msb, state.previous_pic_order_cnt_lsb) == (
        (1 << 63) - 1,
        15,
    )


def test_poc_type1_uses_frame_number_cycles_and_reference_state():
    state = POCType12State()
    offsets = (2, 3)
    assert state.calculate_type1(16, 0, offset_for_non_ref_pic=-1, offset_for_top_to_bottom_field=1, offset_for_ref_frame=offsets, idr=True) == 0
    assert state.calculate_type1(16, 1, offset_for_non_ref_pic=-1, offset_for_top_to_bottom_field=1, offset_for_ref_frame=offsets) == 2
    before = (state.previous_frame_num, state.previous_frame_num_offset)
    assert state.calculate_type1(16, 2, offset_for_non_ref_pic=-1, offset_for_top_to_bottom_field=1, offset_for_ref_frame=offsets, nal_ref_idc=0) == 1
    assert (state.previous_frame_num, state.previous_frame_num_offset) == before
    assert state.calculate_type1(16, 0, offset_for_non_ref_pic=-1, offset_for_top_to_bottom_field=1, offset_for_ref_frame=offsets) == 40


def test_poc_type2_uses_frame_number_offset_and_non_reference_adjustment():
    state = POCType12State()
    assert state.calculate_type2(16, 0, idr=True) == 0
    assert state.calculate_type2(16, 1) == 2
    before = (state.previous_frame_num, state.previous_frame_num_offset)
    assert state.calculate_type2(16, 2, nal_ref_idc=0) == 3
    assert (state.previous_frame_num, state.previous_frame_num_offset) == before
    wrap_state = POCType12State(previous_frame_num=15)
    assert wrap_state.calculate_type2(16, 0) == 32


def test_poc_type12_rejects_invalid_inputs_and_overflow_transactionally():
    state = POCType12State(previous_frame_num=15, previous_frame_num_offset=(1 << 63) - 1)
    before = (state.previous_frame_num, state.previous_frame_num_offset)
    with pytest.raises(PictureOrderCountError, match="picture order count input"):
        state.calculate_type2(16, 0)
    assert (state.previous_frame_num, state.previous_frame_num_offset) == before

    type1_state = POCType12State()
    with pytest.raises(PictureOrderCountError, match="picture order count input"):
        type1_state.calculate_type1(16, 0, delta_pic_order_cnt0=1 << 63, offset_for_ref_frame=(1,))
    assert (type1_state.previous_frame_num, type1_state.previous_frame_num_offset) == (0, 0)


def test_presentation_order_buffer_holds_and_releases_pictures_by_poc():
    buffer = PresentationOrderBuffer(2)
    frame = Yuv420Frame(2, 2, 2, 1, 1, bytes([1, 2, 3, 4]), bytes([5]), bytes([6]))
    released = [buffer.push(PresentationPicture(poc, frame)) for poc in (0, 6, 2, 4)]
    assert [picture.picture_order_cnt if picture is not None else None for picture in released] == [None, None, 0, 2]
    assert [picture.picture_order_cnt for picture in buffer.drain()] == [4, 6]
    assert buffer.drain() == []


def test_presentation_order_buffer_is_stable_owns_frames_and_rejects_invalid():
    buffer = PresentationOrderBuffer(1)
    mutable_frame = Yuv420Frame(2, 2, 2, 1, 1, bytearray([1, 2, 3, 4]), bytearray([5]), bytearray([6]))
    first = buffer.push(PresentationPicture(3, mutable_frame))
    assert first is None
    mutable_frame.y[0] = 99
    second = buffer.push(PresentationPicture(3, mutable_frame))
    assert second is not None and second.frame.y[0] == 1
    assert buffer.drain()[0].frame.y[0] == 99

    buffer = PresentationOrderBuffer(0)
    with pytest.raises(InterPredictionError, match="frame layout is invalid or truncated"):
        buffer.push(PresentationPicture(0, Yuv420Frame(1, 1, 1, 1, 1, b"", b"\x00", b"\x00")))

    with pytest.raises(PictureOrderCountError, match="signed 64-bit integer"):
        buffer.push(PresentationPicture(1 << 63, mutable_frame))

    with pytest.raises(PictureOrderCountError, match="nonnegative integer"):
        PresentationOrderBuffer(-1)


def test_reference_picture_buffer_owns_frames_and_preserves_insertion_order():
    buffer = ReferencePictureBuffer()
    frame = Yuv420Frame(
        width=3,
        height=3,
        y_stride=4,
        u_stride=3,
        v_stride=3,
        y=bytearray([1, 2, 3, 99, 4, 5, 6, 99, 7, 8, 9]),
        u=bytearray([10, 11, 99, 12, 13]),
        v=bytearray([20, 21, 99, 22, 23]),
    )
    first = ReferencePicture(1, frame_num=4)
    second = ReferencePicture(2, frame_num=5)
    buffer.store(first, frame)
    buffer.store(second, frame)
    frame.y[0] = 50

    stored = buffer.get(first.identifier)
    assert stored is not None and stored.frame.y[0] == 1
    assert buffer.references() == (first, second)
    assert buffer.remove(first.identifier)
    assert not buffer.remove(first.identifier)


def test_reference_picture_buffer_replaces_and_rejects_invalid_frames():
    buffer = ReferencePictureBuffer()
    reference = ReferencePicture(1, frame_num=1)
    frame = Yuv420Frame(2, 2, 2, 1, 1, bytes([1, 2, 3, 4]), bytes([5]), bytes([6]))
    buffer.store(reference, frame)
    replacement = ReferencePicture(1, frame_num=2)
    buffer.store(replacement, Yuv420Frame(2, 2, 2, 1, 1, bytes([7, 8, 9, 10]), bytes([5]), bytes([6])))
    stored = buffer.get(reference.identifier)
    assert stored is not None
    assert stored.reference == replacement
    assert stored.frame.y[0] == 7
    assert buffer.references() == (replacement,)

    with pytest.raises(InterPredictionError, match="frame layout is invalid or truncated"):
        buffer.store(replacement, Yuv420Frame(2, 2, 2, 1, 1, b"\x01", b"\x02", b"\x03"))


@pytest.mark.parametrize(
    ("left", "top", "expected"),
    (
        (None, None, (0, 0)),
        (MotionVector(-2, 3), None, (2, 3)),
        (MotionVector(-2, 3), MotionVector(4, -5), (6, 8)),
        (
            MotionVector(-(1 << 31), -(1 << 31)),
            MotionVector(-(1 << 31), -(1 << 31)),
            (1 << 32, 1 << 32),
        ),
    ),
)
def test_motion_vector_difference_neighbor_magnitudes(left, top, expected):
    assert motion_vector_difference_neighbor_magnitudes(left, top) == expected


@pytest.mark.parametrize(
    ("samples", "expected"),
    (
        ([10, 40, 80, 120, 200, 240], 95),
        ([0, 255, 0, 0, 0, 0], 0),
        ([255, 255, 255, 255, 0, 0], 255),
    ),
)
def test_interpolate_luma_half_sample_horizontal_rounds_and_clips(
    samples, expected
):
    assert interpolate_luma_half_sample_horizontal(samples) == expected


@pytest.mark.parametrize(
    "samples",
    ([1, 2, 3, 4, 5], [0, 1, 2, 3, 4, 256], [0, 1, 2, 3, 4, True]),
)
def test_interpolate_luma_half_sample_horizontal_rejects_invalid_samples(samples):
    with pytest.raises(InterPredictionError, match="six 8-bit reference samples"):
        interpolate_luma_half_sample_horizontal(samples)


@pytest.mark.parametrize(
    ("samples", "expected"),
    (
        ([10, 40, 80, 120, 200, 240], 95),
        ([0, 255, 0, 0, 0, 0], 0),
        ([255, 255, 255, 255, 0, 0], 255),
    ),
)
def test_interpolate_luma_half_sample_vertical_rounds_and_clips(
    samples, expected
):
    assert interpolate_luma_half_sample_vertical(samples) == expected


@pytest.mark.parametrize(
    "samples",
    ([1, 2, 3, 4, 5], [0, 1, 2, 3, 4, 256], [0, 1, 2, 3, 4, True]),
)
def test_interpolate_luma_half_sample_vertical_rejects_invalid_samples(samples):
    with pytest.raises(InterPredictionError, match="vertical luma interpolation"):
        interpolate_luma_half_sample_vertical(samples)


@pytest.mark.parametrize(
    ("samples", "expected"),
    (
        (
            [
                [255 if row == 2 and column == 2 else 0 for column in range(6)]
                for row in range(6)
            ],
            100,
        ),
        (
            [
                [255 if row == 2 and column == 1 else 0 for column in range(6)]
                for row in range(6)
            ],
            0,
        ),
        ([[255] * 6 if row in (0, 2, 3, 5) else [0] * 6 for row in range(6)], 255),
    ),
)
def test_interpolate_luma_half_sample_diagonal_rounds_and_clips(samples, expected):
    assert interpolate_luma_half_sample_diagonal(samples) == expected


@pytest.mark.parametrize(
    ("first", "second", "expected"),
    ((10, 20, 15), (10, 21, 16), (0, 255, 128), (255, 255, 255)),
)
def test_interpolate_luma_quarter_sample_average_rounds_up(
    first, second, expected
):
    assert interpolate_luma_quarter_sample_average(first, second) == expected


@pytest.mark.parametrize(
    ("first", "second"),
    ((-1, 0), (0, 256), (True, 1), (1, False)),
)
def test_interpolate_luma_quarter_sample_average_rejects_invalid_samples(
    first, second
):
    with pytest.raises(InterPredictionError, match="two 8-bit luma samples"):
        interpolate_luma_quarter_sample_average(first, second)


@pytest.mark.parametrize(
    ("first", "half", "second", "expected"),
    ((10, 21, 30, (16, 26)), (0, 255, 0, (128, 128))),
)
def test_interpolate_luma_quarter_sample_pair_uses_adjacent_integer_samples(
    first, half, second, expected
):
    assert interpolate_luma_quarter_sample_pair(first, half, second) == expected


def test_interpolate_luma_quarter_sample_pair_rejects_invalid_samples():
    with pytest.raises(InterPredictionError, match="two 8-bit luma samples"):
        interpolate_luma_quarter_sample_pair(10, 256, 20)


def test_interpolate_luma_quarter_sample_diagonal_uses_neighboring_half_samples():
    assert interpolate_luma_quarter_sample_diagonal(10, 21, 30, 41) == (
        16,
        20,
        31,
        36,
    )


def test_interpolate_luma_quarter_sample_diagonal_rejects_invalid_samples():
    with pytest.raises(InterPredictionError, match="two 8-bit luma samples"):
        interpolate_luma_quarter_sample_diagonal(10, 21, 256, 41)


def test_interpolate_luma_quarter_sample_axial_uses_integer_and_half_samples():
    assert interpolate_luma_quarter_sample_axial(10, 30, 40, 21, 25) == (
        16,
        26,
        18,
        33,
    )


def test_interpolate_luma_quarter_sample_axial_rejects_invalid_samples():
    with pytest.raises(InterPredictionError, match="two 8-bit luma samples"):
        interpolate_luma_quarter_sample_axial(10, 30, 40, 21, 256)


def test_interpolate_luma_quarter_sample_around_j_uses_neighboring_half_samples():
    assert interpolate_luma_quarter_sample_around_j(10, 21, 30, 40, 50) == (
        20,
        26,
        35,
        40,
    )


def test_interpolate_luma_quarter_sample_around_j_rejects_invalid_samples():
    with pytest.raises(InterPredictionError, match="two 8-bit luma samples"):
        interpolate_luma_quarter_sample_around_j(10, 21, 30, 40, 256)


def test_select_luma_fractional_sample_indexes_x_then_y():
    samples = [
        [10, 11, 12, 13],
        [20, 21, 22, 23],
        [30, 31, 32, 33],
        [40, 41, 42, 43],
    ]
    for x_frac_l, row in enumerate(samples):
        for y_frac_l, expected in enumerate(row):
            assert (
                select_luma_fractional_sample(samples, x_frac_l, y_frac_l)
                == expected
            )


@pytest.mark.parametrize(
    ("x_frac_l", "y_frac_l"),
    ((-1, 0), (4, 0), (0, -1), (0, 4), (True, 0), (0, False)),
)
def test_select_luma_fractional_sample_rejects_out_of_range_offsets(
    x_frac_l, y_frac_l
):
    with pytest.raises(InterPredictionError, match="offset is outside \\[0,3\\]"):
        select_luma_fractional_sample([[0] * 4 for _ in range(4)], x_frac_l, y_frac_l)


@pytest.mark.parametrize(
    "samples",
    (
        [[0] * 4 for _ in range(3)],
        [[0] * 3 for _ in range(4)],
        [[0, 0, 0, True] for _ in range(4)],
        [[0, 0, 0, 256] for _ in range(4)],
    ),
)
def test_select_luma_fractional_sample_rejects_invalid_grid(samples):
    with pytest.raises(InterPredictionError, match="grid"):
        select_luma_fractional_sample(samples, 0, 0)


def test_interpolate_luma_quarter_sample_grid_builds_table_8_12_positions():
    samples = [
        [10 * row + 5 * column for column in range(6)]
        for row in range(6)
    ]
    assert interpolate_luma_quarter_sample_grid(samples) == (
        (30, 33, 35, 38),
        (32, 34, 37, 39),
        (33, 36, 38, 41),
        (34, 37, 39, 42),
    )


@pytest.mark.parametrize(
    "samples",
    (
        [[0] * 6 for _ in range(5)],
        [[0] * 5 for _ in range(6)],
        [[0, 0, 0, 0, 0, True] for _ in range(6)],
        [[0, 0, 0, 0, 0, 256] for _ in range(6)],
    ),
)
def test_interpolate_luma_quarter_sample_grid_rejects_invalid_neighborhood(samples):
    with pytest.raises(InterPredictionError, match="6x6 block of 8-bit"):
        interpolate_luma_quarter_sample_grid(samples)


def test_gather_luma_quarter_sample_neighborhood_uses_stride():
    width, height, stride = 8, 8, 10
    plane = [250] * (stride * height)
    for row in range(height):
        for column in range(width):
            plane[row * stride + column] = row * width + column
    neighborhood = gather_luma_quarter_sample_neighborhood(
        plane, width, height, stride, 3, 3
    )
    assert neighborhood == tuple(
        tuple(plane[(row + 1) * stride + column + 1] for column in range(6))
        for row in range(6)
    )


def test_gather_luma_quarter_sample_neighborhood_clips_edges_and_ignores_padding():
    plane = [1, 2, 99, 3, 4, 99]
    neighborhood = gather_luma_quarter_sample_neighborhood(plane, 2, 2, 3, 0, 0)
    assert neighborhood == (
        (1, 1, 1, 2, 2, 2),
        (1, 1, 1, 2, 2, 2),
        (1, 1, 1, 2, 2, 2),
        (3, 3, 3, 4, 4, 4),
        (3, 3, 3, 4, 4, 4),
        (3, 3, 3, 4, 4, 4),
    )


@pytest.mark.parametrize(
    ("plane", "width", "height", "stride"),
    (
        ([1], 0, 1, 1),
        ([1], 2, 1, 1),
        ([1, 2, 3], 2, 2, 2),
        ([1, 2, 256, 3], 2, 2, 2),
    ),
)
def test_gather_luma_quarter_sample_neighborhood_rejects_invalid_layout(
    plane, width, height, stride
):
    with pytest.raises(InterPredictionError, match="layout|8-bit"):
        gather_luma_quarter_sample_neighborhood(plane, width, height, stride, 0, 0)


def test_gather_luma_quarter_sample_neighborhood_rejects_noninteger_coordinates():
    with pytest.raises(InterPredictionError, match="coordinates must be integers"):
        gather_luma_quarter_sample_neighborhood([0] * 4, 2, 2, 2, True, 0)


def test_interpolate_luma_fractional_sample_selects_all_table_8_12_positions():
    width = height = stride = 6
    plane = [10 * row + 5 * column for row in range(height) for column in range(width)]
    expected = (
        (30, 33, 35, 38),
        (32, 34, 37, 39),
        (33, 36, 38, 41),
        (34, 37, 39, 42),
    )
    for x_frac_l, row in enumerate(expected):
        for y_frac_l, sample in enumerate(row):
            assert (
                interpolate_luma_fractional_sample(
                    plane, width, height, stride, 2, 2, x_frac_l, y_frac_l
                )
                == sample
            )


def test_interpolate_luma_fractional_sample_propagates_offset_and_layout_errors():
    with pytest.raises(InterPredictionError, match="offset is outside"):
        interpolate_luma_fractional_sample([0] * 4, 2, 2, 2, 0, 0, 4, 0)
    with pytest.raises(InterPredictionError, match="layout"):
        interpolate_luma_fractional_sample([0], 2, 2, 2, 0, 0, 0, 0)


@pytest.mark.parametrize(
    "samples",
    (
        [[0] * 6 for _ in range(5)],
        [[0] * 5 for _ in range(6)],
        [[0, 0, 0, 0, 0, True] for _ in range(6)],
        [[0, 0, 0, 0, 0, 256] for _ in range(6)],
    ),
)
def test_interpolate_luma_half_sample_diagonal_rejects_invalid_samples(samples):
    with pytest.raises(InterPredictionError, match="6x6 block of 8-bit"):
        interpolate_luma_half_sample_diagonal(samples)


def test_build_p_reference_list_orders_short_and_long_term_frames():
    pictures = [
        ReferencePicture(4, frame_num=5),
        ReferencePicture(6, long_term_frame_idx=1),
        ReferencePicture(2, frame_num=1),
        ReferencePicture(5, long_term_frame_idx=4),
        ReferencePicture(3, frame_num=15),
        ReferencePicture(1, frame_num=3),
    ]
    assert [
        picture.identifier for picture in build_p_reference_list(pictures, 2, 16)
    ] == [2, 3, 4, 1, 6, 5]


@pytest.mark.parametrize(
    ("pictures", "current_frame_num", "maximum_frame_num"),
    (
        ([], 16, 16),
        ([], 0, 0),
        ([ReferencePicture(1, frame_num=16)], 0, 16),
        ([ReferencePicture(1, long_term_frame_idx=-1)], 0, 16),
    ),
)
def test_build_p_reference_list_rejects_invalid_frame_numbers(
    pictures, current_frame_num, maximum_frame_num
):
    with pytest.raises(ValueError):
        build_p_reference_list(pictures, current_frame_num, maximum_frame_num)


def test_build_b_reference_lists_orders_poc_partitions_and_long_term_references():
    pictures = [
        ReferencePicture(1, picture_order_cnt=6),
        ReferencePicture(2, picture_order_cnt=14),
        ReferencePicture(3, picture_order_cnt=2),
        ReferencePicture(4, picture_order_cnt=20),
        ReferencePicture(5, picture_order_cnt=10),
        ReferencePicture(6, long_term_frame_idx=1),
        ReferencePicture(7, long_term_frame_idx=4),
    ]
    list0, list1 = build_b_reference_lists(pictures, 10)
    assert [picture.identifier for picture in list0] == [5, 1, 3, 2, 4, 6, 7]
    assert [picture.identifier for picture in list1] == [2, 4, 5, 1, 3, 6, 7]


def test_build_b_reference_lists_swaps_identical_lists():
    pictures = [
        ReferencePicture(1, picture_order_cnt=8),
        ReferencePicture(2, picture_order_cnt=5),
    ]
    list0, list1 = build_b_reference_lists(pictures, 10)
    assert [picture.identifier for picture in list0] == [1, 2]
    assert [picture.identifier for picture in list1] == [2, 1]


def test_apply_reference_list_modifications_resolves_and_inserts_targets():
    references = [
        ReferencePicture(1, frame_num=1),
        ReferencePicture(2, frame_num=3),
        ReferencePicture(3, frame_num=5),
        ReferencePicture(4, frame_num=0),
        ReferencePicture(5, long_term_frame_idx=4),
        ReferencePicture(6, long_term_frame_idx=1),
    ]
    initial = build_p_reference_list(references, 2, 16)
    modifications = [
        RefPicListModification(0, 1),
        RefPicListModification(1, 0),
        RefPicListModification(2, 1),
    ]
    ordered = apply_reference_list_modifications(
        initial, references, 2, 16, modifications
    )
    assert [picture.identifier for picture in ordered] == [4, 1, 6, 3, 2, 5]


@pytest.mark.parametrize(
    "modification",
    (
        RefPicListModification(0, 16),
        RefPicListModification(2, 2),
        RefPicListModification(3, 0),
    ),
)
def test_apply_reference_list_modifications_rejects_invalid_targets(modification):
    references = [
        ReferencePicture(1, frame_num=1),
        ReferencePicture(2, long_term_frame_idx=1),
    ]
    with pytest.raises(ValueError):
        apply_reference_list_modifications(
            references, references, 2, 16, [modification]
        )


@pytest.mark.parametrize(
    ("predicted", "difference", "expected"),
    (
        (MotionVector(120, -240), MotionVector(8, 15), MotionVector(128, -225)),
        (
            MotionVector(32760, -32760),
            MotionVector(20, -20),
            MotionVector(-32756, 32756),
        ),
        (
            MotionVector(-32760, 32760),
            MotionVector(-20, 20),
            MotionVector(32756, -32756),
        ),
    ),
)
def test_apply_motion_vector_difference_wraps_signed_components(
    predicted, difference, expected
):
    assert apply_motion_vector_difference(predicted, difference) == expected


@pytest.mark.parametrize(
    ("current_reference_index", "left", "top", "top_right", "top_left", "expected"),
    (
        (
            2,
            MotionVectorCandidate(1, MotionVector(20, 0)),
            MotionVectorCandidate(2, MotionVector(7, -5)),
            MotionVectorCandidate(0, MotionVector(0, 30)),
            None,
            MotionVector(7, -5),
        ),
        (
            0,
            MotionVectorCandidate(1, MotionVector(20, 0)),
            MotionVectorCandidate(0, MotionVector(8, 10)),
            MotionVectorCandidate(0, MotionVector(2, 4)),
            None,
            MotionVector(8, 4),
        ),
        (
            0,
            MotionVectorCandidate(1, MotionVector(3, 9)),
            MotionVectorCandidate(2, MotionVector(8, 2)),
            MotionVectorCandidate(3, MotionVector(5, 6)),
            None,
            MotionVector(5, 6),
        ),
        (
            3,
            MotionVectorCandidate(1, MotionVector(20, 0)),
            MotionVectorCandidate(2, MotionVector(30, 0)),
            None,
            MotionVectorCandidate(3, MotionVector(11, -7)),
            MotionVector(11, -7),
        ),
        (
            0,
            None,
            MotionVectorCandidate(-1, MotionVector(99, 99)),
            MotionVectorCandidate(1, MotionVector(9, 6)),
            None,
            MotionVector(0, 0),
        ),
    ),
)
def test_predict_motion_vector_reference_selection_and_median(
    current_reference_index, left, top, top_right, top_left, expected
):
    assert predict_motion_vector(
        current_reference_index, left, top, top_right, top_left
    ) == expected


@pytest.mark.parametrize(
    ("shape", "left", "top", "top_right", "top_left", "expected"),
    (
        (
            MotionVectorPartitionShape.SIXTEEN_BY_EIGHT,
            MotionVectorCandidate(0, MotionVector(10, 1)),
            MotionVectorCandidate(0, MotionVector(20, 2)),
            MotionVectorCandidate(1, MotionVector(30, 3)),
            None,
            MotionVector(10, 1),
        ),
        (
            MotionVectorPartitionShape.SIXTEEN_BY_EIGHT,
            MotionVectorCandidate(1, MotionVector(10, 1)),
            MotionVectorCandidate(0, MotionVector(20, 2)),
            MotionVectorCandidate(1, MotionVector(30, 3)),
            None,
            MotionVector(20, 2),
        ),
        (
            MotionVectorPartitionShape.EIGHT_BY_SIXTEEN,
            MotionVectorCandidate(0, MotionVector(10, 1)),
            MotionVectorCandidate(1, MotionVector(20, 2)),
            MotionVectorCandidate(0, MotionVector(30, 3)),
            None,
            MotionVector(10, 1),
        ),
        (
            MotionVectorPartitionShape.EIGHT_BY_SIXTEEN,
            MotionVectorCandidate(1, MotionVector(10, 1)),
            MotionVectorCandidate(1, MotionVector(20, 2)),
            MotionVectorCandidate(0, MotionVector(30, 3)),
            None,
            MotionVector(30, 3),
        ),
        (
            MotionVectorPartitionShape.EIGHT_BY_SIXTEEN,
            MotionVectorCandidate(1, MotionVector(10, 1)),
            MotionVectorCandidate(1, MotionVector(20, 2)),
            None,
            MotionVectorCandidate(0, MotionVector(40, 4)),
            MotionVector(40, 4),
        ),
    ),
)
def test_predict_motion_vector_for_partition_uses_shape_specific_matches(
    shape, left, top, top_right, top_left, expected
):
    assert predict_motion_vector_for_partition(
        shape, 0, left, top, top_right, top_left
    ) == expected


def test_derive_motion_vector_combines_partition_prediction_and_difference():
    assert derive_motion_vector(
        MotionVectorPartitionShape.EIGHT_BY_SIXTEEN,
        0,
        MotionVectorCandidate(1, MotionVector(12, 20)),
        None,
        None,
        MotionVectorCandidate(0, MotionVector(32760, -32760)),
        MotionVector(20, -20),
    ) == MotionVector(-32756, 32756)


@pytest.mark.parametrize(
    ("qpy", "expected"),
    (
        (0, [10, 13, 10, 13, 13, 16, 13, 16, 10, 13, 10, 13, 13, 16, 13, 16]),
        (24, [160, 208, 160, 208, 208, 256, 208, 256, 160, 208, 160, 208, 208, 256, 208, 256]),
        (51, [3584, 4608, 3584, 4608, 4608, 5888, 4608, 5888, 3584, 4608, 3584, 4608, 4608, 5888, 4608, 5888]),
    ),
)
def test_inverse_scale_luma4x4_qp_vectors(qpy, expected):
    assert inverse_scale_luma4x4([1] * 16, [16] * 16, qpy) == expected


def test_inverse_scale_luma4x4_negative_level_and_custom_weight():
    levels = [0] * 16
    levels[0:2] = [-1, 1]
    scaling_list = [16] * 16
    scaling_list[1] = 8

    scaled = inverse_scale_luma4x4(levels, scaling_list, 0)

    assert (scaled[0], scaled[1]) == (-10, 7)


@pytest.mark.parametrize("qpy", (-1, 52))
def test_inverse_scale_luma4x4_rejects_qpy_outside_range(qpy):
    with pytest.raises(InverseScaleError, match="QPY is outside"):
        inverse_scale_luma4x4([0] * 16, [16] * 16, qpy)


@pytest.mark.parametrize(
    ("levels", "scaling_list", "message"),
    (
        ([0] * 15, [16] * 16, "16 signed 32-bit integers"),
        ([1 << 31] + [0] * 15, [16] * 16, "16 signed 32-bit integers"),
        ([0] * 16, [16] * 15, "16 values in \\[1,255\\]"),
        ([0] * 16, [16] * 7 + [0] + [16] * 8, "16 values in \\[1,255\\]"),
    ),
)
def test_inverse_scale_luma4x4_rejects_invalid_vectors(levels, scaling_list, message):
    with pytest.raises(InverseScaleError, match=message):
        inverse_scale_luma4x4(levels, scaling_list, 0)


@pytest.mark.parametrize(
    ("qpy", "expected"),
    (
        (
            0,
            [
                20, 19, 25, 19, 20, 19, 25, 19,
                19, 18, 24, 18, 19, 18, 24, 18,
                25, 24, 32, 24, 25, 24, 32, 24,
                19, 18, 24, 18, 19, 18, 24, 18,
                20, 19, 25, 19, 20, 19, 25, 19,
                19, 18, 24, 18, 19, 18, 24, 18,
                25, 24, 32, 24, 25, 24, 32, 24,
                19, 18, 24, 18, 19, 18, 24, 18,
            ],
        ),
        (
            24,
            [
                320, 304, 400, 304, 320, 304, 400, 304,
                304, 288, 384, 288, 304, 288, 384, 288,
                400, 384, 512, 384, 400, 384, 512, 384,
                304, 288, 384, 288, 304, 288, 384, 288,
                320, 304, 400, 304, 320, 304, 400, 304,
                304, 288, 384, 288, 304, 288, 384, 288,
                400, 384, 512, 384, 400, 384, 512, 384,
                304, 288, 384, 288, 304, 288, 384, 288,
            ],
        ),
        (
            51,
            [
                7168, 6656, 8960, 6656, 7168, 6656, 8960, 6656,
                6656, 6400, 8448, 6400, 6656, 6400, 8448, 6400,
                8960, 8448, 11520, 8448, 8960, 8448, 11520, 8448,
                6656, 6400, 8448, 6400, 6656, 6400, 8448, 6400,
                7168, 6656, 8960, 6656, 7168, 6656, 8960, 6656,
                6656, 6400, 8448, 6400, 6656, 6400, 8448, 6400,
                8960, 8448, 11520, 8448, 8960, 8448, 11520, 8448,
                6656, 6400, 8448, 6400, 6656, 6400, 8448, 6400,
            ],
        ),
    ),
)
def test_inverse_scale_luma8x8_scaling_classes_and_qp_vectors(qpy, expected):
    assert inverse_scale_luma8x8([1] * 64, [16] * 64, qpy) == expected


def test_inverse_scale_luma8x8_negative_level_and_custom_weight():
    levels = [0] * 64
    levels[0:2] = [-1, 1]
    scaling_list = [16] * 64
    scaling_list[1] = 8

    scaled = inverse_scale_luma8x8(levels, scaling_list, 0)

    assert (scaled[0], scaled[1]) == (-20, 10)


@pytest.mark.parametrize("qpy", (-1, 52))
def test_inverse_scale_luma8x8_rejects_qpy_outside_range(qpy):
    with pytest.raises(InverseScaleError, match="QPY is outside"):
        inverse_scale_luma8x8([0] * 64, [16] * 64, qpy)


@pytest.mark.parametrize(
    ("levels", "scaling_list", "message"),
    (
        ([0] * 63, [16] * 64, "64 signed 32-bit integers"),
        ([1 << 31] + [0] * 63, [16] * 64, "64 signed 32-bit integers"),
        ([0] * 64, [16] * 63, "64 values in \\[1,255\\]"),
        ([0] * 64, [16] * 42 + [0] + [16] * 21, "64 values in \\[1,255\\]"),
    ),
)
def test_inverse_scale_luma8x8_rejects_invalid_vectors(levels, scaling_list, message):
    with pytest.raises(InverseScaleError, match=message):
        inverse_scale_luma8x8(levels, scaling_list, 0)


def test_inverse_transform_luma4x4_dc_and_frequency_impulse_vectors():
    dc_coefficients = [64] + [0] * 15
    assert inverse_transform_luma4x4(dc_coefficients) == [1] * 16

    horizontal_frequency = [0] * 16
    horizontal_frequency[1] = 64
    expected = [1, 1, 0, -1] * 4
    assert inverse_transform_luma4x4(horizontal_frequency) == expected


@pytest.mark.parametrize(("dc_level", "expected"), ((31, 0), (32, 1), (-33, -1)))
def test_inverse_transform_luma4x4_rounding_vectors(dc_level, expected):
    assert inverse_transform_luma4x4([dc_level] + [0] * 15) == [expected] * 16


@pytest.mark.parametrize(
    "coefficients",
    ([0] * 15, [0] * 15 + [True]),
)
def test_inverse_transform_luma4x4_rejects_invalid_inputs(coefficients):
    with pytest.raises(InverseTransformError, match="16 integer coefficients"):
        inverse_transform_luma4x4(coefficients)


def test_inverse_transform_luma8x8_dc_and_frequency_impulse_vectors():
    dc_coefficients = [64] + [0] * 63
    assert inverse_transform_luma8x8(dc_coefficients) == [1] * 64

    frequency = [2, -1, 1, 0, 0, -1, 1, -1]
    horizontal_frequency = [0] * 64
    horizontal_frequency[1] = 64
    assert inverse_transform_luma8x8(horizontal_frequency) == frequency * 8

    vertical_frequency = [0] * 64
    vertical_frequency[8] = 64
    assert inverse_transform_luma8x8(vertical_frequency) == [
        value for value in frequency for _ in range(8)
    ]


@pytest.mark.parametrize(("dc_level", "expected"), ((31, 0), (32, 1), (-33, -1)))
def test_inverse_transform_luma8x8_rounding_vectors(dc_level, expected):
    assert inverse_transform_luma8x8([dc_level] + [0] * 63) == [expected] * 64


@pytest.mark.parametrize(
    "coefficients",
    ([0] * 63, [0] * 63 + [True]),
)
def test_inverse_transform_luma8x8_rejects_invalid_inputs(coefficients):
    with pytest.raises(InverseTransformError, match="64 integer coefficients"):
        inverse_transform_luma8x8(coefficients)


def test_predict_luma_intra8x8_vertical_repeats_filtered_top_samples():
    top = [0, 17, 63, 129, 190, 220, 254, 255]
    assert predict_luma_intra8x8_vertical(top) == top * 8


def test_predict_luma_intra16x16_vertical_repeats_filtered_top_samples():
    top = [0, 17, 33, 51, 68, 85, 102, 119, 136, 153, 170, 187, 204, 221, 238, 255]
    prediction = predict_luma_intra16x16_vertical(top)
    assert prediction == top * 16


@pytest.mark.parametrize("top", ([1] * 15, [1] * 15 + [256], [1] * 15 + [True]))
def test_predict_luma_intra16x16_vertical_rejects_invalid_top_samples(top):
    with pytest.raises(IntraPredictionError, match="sixteen 8-bit filtered top samples"):
        predict_luma_intra16x16_vertical(top)


def test_predict_luma_intra16x16_horizontal_repeats_filtered_left_samples():
    left = [0, 17, 33, 51, 68, 85, 102, 119, 136, 153, 170, 187, 204, 221, 238, 255]
    prediction = predict_luma_intra16x16_horizontal(left)
    assert prediction == [sample for sample in left for _ in range(16)]


@pytest.mark.parametrize("left", ([1] * 15, [1] * 15 + [256], [1] * 15 + [True]))
def test_predict_luma_intra16x16_horizontal_rejects_invalid_left_samples(left):
    with pytest.raises(IntraPredictionError, match="sixteen 8-bit filtered left samples"):
        predict_luma_intra16x16_horizontal(left)


@pytest.mark.parametrize(
    ("top", "left", "expected"),
    (
        (list(range(16)), list(range(16, 32)), 16),
        (list(range(16)), None, 8),
        (None, list(range(16, 32)), 24),
        (None, None, 128),
    ),
)
def test_predict_luma_intra16x16_dc_reference_availability_and_rounding(
    top, left, expected
):
    assert predict_luma_intra16x16_dc(top, left) == [expected] * 256


@pytest.mark.parametrize(
    ("top", "left", "message"),
    (
        ([1] * 15, None, "sixteen 8-bit filtered top samples"),
        ([1] * 15 + [256], None, "sixteen 8-bit filtered top samples"),
        (None, [1] * 15, "sixteen 8-bit filtered left samples"),
        (None, [1] * 15 + [True], "sixteen 8-bit filtered left samples"),
    ),
)
def test_predict_luma_intra16x16_dc_rejects_invalid_references(
    top, left, message
):
    with pytest.raises(IntraPredictionError, match=message):
        predict_luma_intra16x16_dc(top, left)


def test_predict_luma_intra16x16_plane_gradient_and_clipping():
    top = [80 + 2 * (index + 1) for index in range(16)]
    left = [80 + 3 * (index + 1) for index in range(16)]
    expected = [85 + 2 * column + 3 * row for row in range(16) for column in range(16)]
    assert predict_luma_intra16x16_plane(top, left, 80) == expected

    high_top = [0] * 16
    high_left = [0] * 16
    high_top[15] = high_left[15] = 255
    assert predict_luma_intra16x16_plane(high_top, high_left, 0)[255] == 255

    low_top = [0] * 16
    low_left = [0] * 16
    low_top[0] = low_left[0] = 255
    assert predict_luma_intra16x16_plane(low_top, low_left, 0)[255] == 0


@pytest.mark.parametrize(
    ("top", "left", "top_left", "message"),
    (
        ([1] * 15, [2] * 16, 3, "sixteen 8-bit filtered top samples"),
        ([1] * 16, [2] * 15, 3, "sixteen 8-bit filtered left samples"),
        ([1] * 15 + [256], [2] * 16, 3, "sixteen 8-bit filtered top samples"),
        ([1] * 16, [2] * 15 + [True], 3, "sixteen 8-bit filtered left samples"),
        ([1] * 16, [2] * 16, 256, "8-bit filtered top-left sample"),
        ([1] * 16, [2] * 16, True, "8-bit filtered top-left sample"),
    ),
)
def test_predict_luma_intra16x16_plane_rejects_invalid_references(
    top, left, top_left, message
):
    with pytest.raises(IntraPredictionError, match=message):
        predict_luma_intra16x16_plane(top, left, top_left)


def test_predict_luma_intra8x8_horizontal_repeats_filtered_left_samples():
    left = [0, 17, 63, 129, 190, 220, 254, 255]
    assert predict_luma_intra8x8_horizontal(left) == [sample for sample in left for _ in range(8)]


@pytest.mark.parametrize(
    "left", ([1, 2, 3], [0, 1, 2, 3, 4, 5, 6, 256], [0, 1, 2, 3, 4, 5, 6, True])
)
def test_predict_luma_intra8x8_horizontal_rejects_invalid_left_samples(left):
    with pytest.raises(IntraPredictionError, match="eight 8-bit filtered left samples"):
        predict_luma_intra8x8_horizontal(left)


@pytest.mark.parametrize("top", ([1, 2, 3], [0, 1, 2, 3, 4, 5, 6, 256], [0, 1, 2, 3, 4, 5, 6, True]))
def test_predict_luma_intra8x8_vertical_rejects_invalid_top_samples(top):
    with pytest.raises(IntraPredictionError, match="eight 8-bit filtered top samples"):
        predict_luma_intra8x8_vertical(top)


@pytest.mark.parametrize(
    ("top", "expected"),
    (
        ([10, 40, 90, 160], [10, 40, 90, 160] * 4),
        ([0, 255, 0, 255], [0, 255, 0, 255] * 4),
    ),
)
def test_predict_luma_intra4x4_vertical_repeats_top_samples(top, expected):
    assert predict_luma_intra4x4_vertical(top) == expected


@pytest.mark.parametrize("top", ([1, 2, 3], [0, 1, 2, 256], [0, 1, 2, True]))
def test_predict_luma_intra4x4_vertical_rejects_invalid_top_samples(top):
    with pytest.raises(IntraPredictionError, match="four 8-bit top samples"):
        predict_luma_intra4x4_vertical(top)


@pytest.mark.parametrize(
    ("left", "expected"),
    (
        ([10, 40, 90, 160], [10] * 4 + [40] * 4 + [90] * 4 + [160] * 4),
        ([0, 255, 0, 255], [0] * 4 + [255] * 4 + [0] * 4 + [255] * 4),
    ),
)
def test_predict_luma_intra4x4_horizontal_repeats_left_samples(left, expected):
    assert predict_luma_intra4x4_horizontal(left) == expected


@pytest.mark.parametrize("left", ([1, 2, 3], [0, 1, 2, 256], [0, 1, 2, True]))
def test_predict_luma_intra4x4_horizontal_rejects_invalid_left_samples(left):
    with pytest.raises(IntraPredictionError, match="four 8-bit left samples"):
        predict_luma_intra4x4_horizontal(left)


@pytest.mark.parametrize(
    ("top", "left", "expected"),
    (
        ([10, 40, 90, 160, 20, 50, 80, 110], [20, 60, 100, 140, 30, 70, 110, 150], 78),
        ([10, 40, 90, 160, 20, 50, 80, 110], None, 70),
        (None, [20, 60, 100, 140, 30, 70, 110, 150], 85),
        (None, None, 128),
    ),
)
def test_predict_luma_intra8x8_dc_reference_availability(top, left, expected):
    assert predict_luma_intra8x8_dc(top, left) == [expected] * 64


@pytest.mark.parametrize(
    ("top", "left", "message"),
    (
        ([1, 2, 3], None, "eight 8-bit filtered top samples"),
        ([0, 1, 2, 3, 4, 5, 6, 256], None, "eight 8-bit filtered top samples"),
        (None, [1, 2, 3], "eight 8-bit filtered left samples"),
        (None, [0, 1, 2, 3, 4, 5, 6, True], "eight 8-bit filtered left samples"),
    ),
)
def test_predict_luma_intra8x8_dc_rejects_invalid_references(top, left, message):
    with pytest.raises(IntraPredictionError, match=message):
        predict_luma_intra8x8_dc(top, left)


def test_predict_luma_intra8x8_diagonal_down_left_interpolates_filtered_top_references():
    top = list(range(10, 161, 10))
    assert predict_luma_intra8x8_diagonal_down_left(top) == [
        20, 30, 40, 50, 60, 70, 80, 90,
        30, 40, 50, 60, 70, 80, 90, 100,
        40, 50, 60, 70, 80, 90, 100, 110,
        50, 60, 70, 80, 90, 100, 110, 120,
        60, 70, 80, 90, 100, 110, 120, 130,
        70, 80, 90, 100, 110, 120, 130, 140,
        80, 90, 100, 110, 120, 130, 140, 150,
        90, 100, 110, 120, 130, 140, 150, 158,
    ]


@pytest.mark.parametrize(
    "top", ([1, 2, 3], [*range(15), 256], [*range(15), True])
)
def test_predict_luma_intra8x8_diagonal_down_left_rejects_invalid_top_samples(top):
    with pytest.raises(
        IntraPredictionError,
        match="sixteen 8-bit filtered top samples",
    ):
        predict_luma_intra8x8_diagonal_down_left(top)


def test_predict_luma_intra8x8_diagonal_down_right_uses_all_reference_regions():
    assert predict_luma_intra8x8_diagonal_down_right(
        [20, 40, 60, 80, 100, 120, 140, 160, 180, 200, 220, 240, 250, 230, 210, 190],
        [60, 100, 140, 180, 220, 240, 200, 160],
        30,
    ) == [
        28, 40, 60, 80, 100, 120, 140, 160,
        35, 28, 40, 60, 80, 100, 120, 140,
        63, 35, 28, 40, 60, 80, 100, 120,
        100, 63, 35, 28, 40, 60, 80, 100,
        140, 100, 63, 35, 28, 40, 60, 80,
        180, 140, 100, 63, 35, 28, 40, 60,
        215, 180, 140, 100, 63, 35, 28, 40,
        225, 215, 180, 140, 100, 63, 35, 28,
    ]


@pytest.mark.parametrize(
    ("top", "left", "top_left", "message"),
    (
        ([1] * 15, [2] * 8, 3, "sixteen 8-bit filtered top samples"),
        ([1] * 16, [2] * 7, 3, "eight 8-bit filtered left samples"),
        ([1] * 15 + [256], [2] * 8, 3, "sixteen 8-bit filtered top samples"),
        ([1] * 16, [2] * 7 + [True], 3, "eight 8-bit filtered left samples"),
        ([1] * 16, [2] * 8, 256, "8-bit filtered top-left sample"),
        ([1] * 16, [2] * 8, True, "8-bit filtered top-left sample"),
    ),
)
def test_predict_luma_intra8x8_diagonal_down_right_rejects_invalid_references(
    top, left, top_left, message
):
    with pytest.raises(IntraPredictionError, match=message):
        predict_luma_intra8x8_diagonal_down_right(top, left, top_left)


def test_predict_luma_intra8x8_vertical_right_uses_all_reference_regions():
    assert predict_luma_intra8x8_vertical_right(
        [20, 40, 60, 80, 100, 120, 140, 160, 180, 200, 220, 240, 250, 230, 210, 190],
        [60, 100, 140, 180, 220, 240, 200, 160],
        30,
    ) == [
        28, 40, 60, 80, 100, 120, 140, 160,
        45, 25, 30, 50, 70, 90, 110, 130,
        35, 28, 40, 60, 80, 100, 120, 140,
        80, 45, 25, 30, 50, 70, 90, 110,
        100, 35, 28, 40, 60, 80, 100, 120,
        120, 80, 45, 25, 30, 50, 70, 90,
        140, 100, 35, 28, 40, 60, 80, 100,
        160, 120, 80, 45, 25, 30, 50, 70,
    ]


@pytest.mark.parametrize(
    ("top", "left", "top_left", "message"),
    (
        ([1] * 15, [2] * 8, 3, "sixteen 8-bit filtered top samples"),
        ([1] * 16, [2] * 7, 3, "eight 8-bit filtered left samples"),
        ([1] * 15 + [256], [2] * 8, 3, "sixteen 8-bit filtered top samples"),
        ([1] * 16, [2] * 7 + [True], 3, "eight 8-bit filtered left samples"),
        ([1] * 16, [2] * 8, 256, "8-bit filtered top-left sample"),
        ([1] * 16, [2] * 8, True, "8-bit filtered top-left sample"),
    ),
)
def test_predict_luma_intra8x8_vertical_right_rejects_invalid_references(
    top, left, top_left, message
):
    with pytest.raises(IntraPredictionError, match=message):
        predict_luma_intra8x8_vertical_right(top, left, top_left)


def test_predict_luma_intra8x8_horizontal_down_transposes_vertical_right():
    assert predict_luma_intra8x8_horizontal_down(
        [60, 100, 140, 180, 220, 240, 200, 160],
        [20, 40, 60, 80, 100, 120, 140, 160, 180, 200, 220, 240, 250, 230, 210, 190],
        30,
    ) == [
        28, 45, 35, 80, 100, 120, 140, 160,
        40, 25, 28, 45, 35, 80, 100, 120,
        60, 30, 40, 25, 28, 45, 35, 80,
        80, 50, 60, 30, 40, 25, 28, 45,
        100, 70, 80, 50, 60, 30, 40, 25,
        120, 90, 100, 70, 80, 50, 60, 30,
        140, 110, 120, 90, 100, 70, 80, 50,
        160, 130, 140, 110, 120, 90, 100, 70,
    ]


@pytest.mark.parametrize(
    ("top", "left", "top_left", "message"),
    (
        ([1] * 7, [2] * 16, 3, "eight 8-bit filtered top samples"),
        ([1] * 8, [2] * 15, 3, "sixteen 8-bit filtered left samples"),
        ([1] * 7 + [256], [2] * 16, 3, "eight 8-bit filtered top samples"),
        ([1] * 8, [2] * 15 + [True], 3, "sixteen 8-bit filtered left samples"),
        ([1] * 8, [2] * 16, 256, "8-bit filtered top-left sample"),
        ([1] * 8, [2] * 16, True, "8-bit filtered top-left sample"),
    ),
)
def test_predict_luma_intra8x8_horizontal_down_rejects_invalid_references(
    top, left, top_left, message
):
    with pytest.raises(IntraPredictionError, match=message):
        predict_luma_intra8x8_horizontal_down(top, left, top_left)


def test_predict_luma_intra8x8_vertical_left_alternates_top_reference_phases():
    top = list(range(10, 161, 10))
    assert predict_luma_intra8x8_vertical_left(top) == [
        15, 25, 35, 45, 55, 65, 75, 85,
        20, 30, 40, 50, 60, 70, 80, 90,
        25, 35, 45, 55, 65, 75, 85, 95,
        30, 40, 50, 60, 70, 80, 90, 100,
        35, 45, 55, 65, 75, 85, 95, 105,
        40, 50, 60, 70, 80, 90, 100, 110,
        45, 55, 65, 75, 85, 95, 105, 115,
        50, 60, 70, 80, 90, 100, 110, 120,
    ]


@pytest.mark.parametrize("top", ([1] * 15, [1] * 15 + [256], [1] * 15 + [True]))
def test_predict_luma_intra8x8_vertical_left_rejects_invalid_top_samples(top):
    with pytest.raises(IntraPredictionError, match="sixteen 8-bit filtered top samples"):
        predict_luma_intra8x8_vertical_left(top)


def test_predict_luma_intra8x8_horizontal_up_transposes_vertical_left():
    left = list(range(10, 161, 10))
    assert predict_luma_intra8x8_horizontal_up(left) == [
        15, 20, 25, 30, 35, 40, 45, 50,
        25, 30, 35, 40, 45, 50, 55, 60,
        35, 40, 45, 50, 55, 60, 65, 70,
        45, 50, 55, 60, 65, 70, 75, 80,
        55, 60, 65, 70, 75, 80, 85, 90,
        65, 70, 75, 80, 85, 90, 95, 100,
        75, 80, 85, 90, 95, 100, 105, 110,
        85, 90, 95, 100, 105, 110, 115, 120,
    ]


@pytest.mark.parametrize("left", ([1] * 15, [1] * 15 + [256], [1] * 15 + [True]))
def test_predict_luma_intra8x8_horizontal_up_rejects_invalid_left_samples(left):
    with pytest.raises(IntraPredictionError, match="sixteen 8-bit filtered left samples"):
        predict_luma_intra8x8_horizontal_up(left)


def test_predict_luma_intra8x8_plane_reconstructs_affine_gradient():
    top = [80 + 2 * index for index in range(16)]
    left = [80 + 3 * index for index in range(16)]
    assert predict_luma_intra8x8_plane(top, left) == [
        85, 87, 89, 91, 93, 95, 97, 99,
        88, 90, 92, 94, 96, 98, 100, 102,
        91, 93, 95, 97, 99, 101, 103, 105,
        94, 96, 98, 100, 102, 104, 106, 108,
        97, 99, 101, 103, 105, 107, 109, 111,
        100, 102, 104, 106, 108, 110, 112, 114,
        103, 105, 107, 109, 111, 113, 115, 117,
        106, 108, 110, 112, 114, 116, 118, 120,
    ]


def test_predict_luma_intra8x8_plane_clips_to_8_bit_range():
    high_top = [0] * 16
    high_left = [0] * 16
    high_top[8] = high_left[8] = 255
    assert predict_luma_intra8x8_plane(high_top, high_left)[63] == 255

    low_top = [0] * 16
    low_left = [0] * 16
    low_top[0] = low_left[0] = 255
    assert predict_luma_intra8x8_plane(low_top, low_left)[63] == 0


@pytest.mark.parametrize(
    ("top", "left", "message"),
    (
        ([1] * 15, [2] * 16, "sixteen 8-bit filtered top samples"),
        ([1] * 16, [2] * 15, "sixteen 8-bit filtered left samples"),
        ([1] * 15 + [256], [2] * 16, "sixteen 8-bit filtered top samples"),
        ([1] * 16, [2] * 15 + [True], "sixteen 8-bit filtered left samples"),
    ),
)
def test_predict_luma_intra8x8_plane_rejects_invalid_references(
    top, left, message
):
    with pytest.raises(IntraPredictionError, match=message):
        predict_luma_intra8x8_plane(top, left)


@pytest.mark.parametrize(
    ("top", "left", "expected"),
    (
        ([10, 40, 90, 161], [20, 60, 100, 141], 78),
        ([10, 40, 90, 161], None, 75),
        (None, [20, 60, 100, 141], 80),
        (None, None, 128),
    ),
)
def test_predict_luma_intra4x4_dc_reference_availability(top, left, expected):
    assert predict_luma_intra4x4_dc(top, left) == [expected] * 16


@pytest.mark.parametrize(
    ("top", "left", "message"),
    (
        ([1, 2, 3], None, "four 8-bit top samples"),
        ([0, 1, 2, 256], None, "four 8-bit top samples"),
        (None, [1, 2, 3], "four 8-bit left samples"),
        (None, [0, 1, 2, True], "four 8-bit left samples"),
    ),
)
def test_predict_luma_intra4x4_dc_rejects_invalid_references(top, left, message):
    with pytest.raises(IntraPredictionError, match=message):
        predict_luma_intra4x4_dc(top, left)


def test_predict_luma_intra4x4_diagonal_down_left_interpolates_top_references():
    assert predict_luma_intra4x4_diagonal_down_left(
        [10, 20, 30, 40, 50, 60, 70, 80]
    ) == [
        20, 30, 40, 50,
        30, 40, 50, 60,
        40, 50, 60, 70,
        50, 60, 70, 78,
    ]


@pytest.mark.parametrize(
    "top",
    ([1, 2, 3, 4, 5, 6, 7], [0, 1, 2, 3, 4, 5, 6, 256], [0, 1, 2, 3, 4, 5, 6, True]),
)
def test_predict_luma_intra4x4_diagonal_down_left_rejects_invalid_top(top):
    with pytest.raises(IntraPredictionError, match="eight 8-bit top samples"):
        predict_luma_intra4x4_diagonal_down_left(top)


def test_predict_luma_intra4x4_diagonal_down_right_uses_all_reference_regions():
    assert predict_luma_intra4x4_diagonal_down_right(
        [20, 40, 80, 120, 160, 200, 220, 240],
        [60, 100, 140, 180],
        30,
    ) == [
        28, 45, 80, 120,
        35, 28, 45, 80,
        63, 35, 28, 45,
        100, 63, 35, 28,
    ]


@pytest.mark.parametrize(
    ("top", "left", "top_left", "message"),
    (
        ([1] * 7, [2] * 4, 3, "eight 8-bit top samples"),
        ([1] * 8, [2] * 3, 3, "four 8-bit left samples"),
        ([1] * 8, [2] * 4, 256, "8-bit top-left sample"),
        ([1] * 8, [2] * 4, True, "8-bit top-left sample"),
    ),
)
def test_predict_luma_intra4x4_diagonal_down_right_rejects_invalid_references(
    top, left, top_left, message
):
    with pytest.raises(IntraPredictionError, match=message):
        predict_luma_intra4x4_diagonal_down_right(top, left, top_left)


def test_predict_luma_intra4x4_vertical_right_uses_all_reference_regions():
    assert predict_luma_intra4x4_vertical_right(
        [20, 40, 80, 120, 160, 200, 220, 240],
        [60, 100, 140, 180],
        30,
    ) == [
        28, 45, 80, 120,
        45, 25, 30, 60,
        35, 28, 45, 80,
        80, 45, 25, 30,
    ]


@pytest.mark.parametrize(
    ("top", "left", "top_left", "message"),
    (
        ([1] * 7, [2] * 4, 3, "eight 8-bit top samples"),
        ([1] * 8, [2] * 3, 3, "four 8-bit left samples"),
        ([1] * 8, [2] * 4, 256, "8-bit top-left sample"),
        ([1] * 8, [2] * 4, True, "8-bit top-left sample"),
    ),
)
def test_predict_luma_intra4x4_vertical_right_rejects_invalid_references(
    top, left, top_left, message
):
    with pytest.raises(IntraPredictionError, match=message):
        predict_luma_intra4x4_vertical_right(top, left, top_left)


def test_predict_luma_intra4x4_horizontal_down_transposes_vertical_right():
    assert predict_luma_intra4x4_horizontal_down(
        [20, 40, 80, 120],
        [60, 100, 140, 180],
        30,
    ) == [
        63, 25, 35, 30,
        100, 45, 63, 25,
        140, 80, 100, 45,
        170, 120, 140, 80,
    ]


@pytest.mark.parametrize(
    ("top", "left", "top_left", "message"),
    (
        ([1, 2, 3], [4, 5, 6, 7], 8, "four 8-bit top samples"),
        ([1, 2, 3, 4], [5, 6, 7], 8, "four 8-bit left samples"),
        ([1, 2, 3, 4], [5, 6, 7, 8], 256, "8-bit top-left sample"),
        ([1, 2, 3, 4], [5, 6, 7, 8], True, "8-bit top-left sample"),
    ),
)
def test_predict_luma_intra4x4_horizontal_down_rejects_invalid_references(
    top, left, top_left, message
):
    with pytest.raises(IntraPredictionError, match=message):
        predict_luma_intra4x4_horizontal_down(top, left, top_left)


def test_predict_luma_intra4x4_vertical_left_alternates_top_reference_phases():
    assert predict_luma_intra4x4_vertical_left(
        [10, 20, 30, 40, 50, 60, 70, 80]
    ) == [
        15, 25, 35, 45,
        20, 30, 40, 50,
        25, 35, 45, 55,
        30, 40, 50, 60,
    ]


@pytest.mark.parametrize(
    "top",
    ([1, 2, 3, 4, 5, 6, 7], [0, 1, 2, 3, 4, 5, 6, 256], [0, 1, 2, 3, 4, 5, 6, True]),
)
def test_predict_luma_intra4x4_vertical_left_rejects_invalid_top_samples(top):
    with pytest.raises(IntraPredictionError, match="eight 8-bit top samples"):
        predict_luma_intra4x4_vertical_left(top)


def test_predict_luma_intra4x4_horizontal_up_transposes_vertical_left():
    assert predict_luma_intra4x4_horizontal_up(
        [10, 20, 30, 40, 50, 60, 70, 80]
    ) == [
        15, 20, 25, 30,
        25, 30, 35, 40,
        35, 40, 45, 50,
        45, 50, 55, 60,
    ]


@pytest.mark.parametrize(
    "left",
    ([1, 2, 3, 4, 5, 6, 7], [0, 1, 2, 3, 4, 5, 6, 256], [0, 1, 2, 3, 4, 5, 6, True]),
)
def test_predict_luma_intra4x4_horizontal_up_rejects_invalid_left_samples(left):
    with pytest.raises(IntraPredictionError, match="eight 8-bit left samples"):
        predict_luma_intra4x4_horizontal_up(left)