#include "pygorvid/h264.hpp"
#include "pygorvid/cabac.hpp"
#include "pygorvid/cavlc.hpp"

#include <algorithm>
#include <cstdlib>
#include <limits>
#include <numeric>
#include <optional>
#include <stdexcept>

namespace pygorvid {
namespace {

using MotionVector = std::array<std::int64_t, 2>;
using MacroblockMotionVectors = std::array<MotionVector, 16>;

std::size_t checked_plane_size(std::uint32_t width, std::uint32_t height) {
    if (height != 0 &&
        static_cast<std::size_t>(width) >
            std::numeric_limits<std::size_t>::max() / height) {
        throw std::runtime_error("decoded frame plane size overflows");
    }
    return static_cast<std::size_t>(width) * height;
}

std::int32_t chroma_qp(std::int32_t qpy, std::int64_t chroma_qp_offset);

constexpr std::array<std::uint8_t, 52> deblock_alpha{{
    0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 4, 4, 5, 6, 7, 8, 9,
    10, 12, 13, 15, 17, 20, 22, 25, 28, 32, 36, 40, 45, 50, 56, 63, 71,
    80, 90, 101, 113, 127, 144, 162, 182, 203, 226, 255, 255,
}};

constexpr std::array<std::uint8_t, 52> deblock_beta{{
    0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2, 2, 2, 3, 3, 3, 3,
    4, 4, 4, 6, 6, 7, 7, 8, 8, 9, 9, 10, 10, 11, 11, 12, 12, 13, 13, 14,
    14, 15, 15, 16, 16, 17, 17, 18, 18,
}};

constexpr std::array<std::array<std::uint8_t, 52>, 3> deblock_tc0{{
    {{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
      1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 4, 4, 4, 5, 6, 6,
      7, 8, 9, 10, 11, 13}},
    {{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1,
      1, 1, 1, 1, 1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 4, 4, 5, 5, 6, 7, 8, 8,
      10, 11, 12, 13, 15, 17}},
    {{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 1, 1,
      1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 4, 4, 4, 5, 6, 6, 7, 8, 9, 10, 11,
      13, 14, 16, 18, 20, 23, 25}},
}};

std::uint8_t clip_sample(std::int32_t value) {
    return static_cast<std::uint8_t>(std::clamp(value, 0, 255));
}

void filter_deblocking_edge(
    std::vector<std::uint8_t>& plane, std::uint32_t width,
    std::uint32_t height, std::uint32_t edge_x, std::uint32_t edge_y,
    bool vertical, std::uint32_t segment_samples, std::uint8_t strength,
    std::int32_t qp_p, std::int32_t qp_q, std::int64_t alpha_offset_div2,
    std::int64_t beta_offset_div2, bool chroma) {
    if (strength == 0) {
        return;
    }
    const auto average_qp = (qp_p + qp_q + 1) >> 1;
    const auto index_a = std::clamp<std::int64_t>(
        average_qp + 2 * alpha_offset_div2, 0, 51);
    const auto index_b = std::clamp<std::int64_t>(
        average_qp + 2 * beta_offset_div2, 0, 51);
    const auto alpha = deblock_alpha[static_cast<std::size_t>(index_a)];
    const auto beta = deblock_beta[static_cast<std::size_t>(index_b)];
    if (alpha == 0 || beta == 0) {
        return;
    }
    for (std::uint32_t offset = 0; offset < segment_samples; ++offset) {
        const auto x = edge_x + (vertical ? 0 : offset);
        const auto y = edge_y + (vertical ? offset : 0);
        if ((vertical && (edge_x < 4 || edge_x + 3 >= width || y >= height)) ||
            (!vertical && (edge_y < 4 || edge_y + 3 >= height || x >= width))) {
            continue;
        }
        const auto coordinate = [&](std::int32_t distance) {
            const auto sample_x = static_cast<std::size_t>(
                static_cast<std::int64_t>(x) +
                (vertical ? distance : 0));
            const auto sample_y = static_cast<std::size_t>(
                static_cast<std::int64_t>(y) +
                (vertical ? 0 : distance));
            return sample_y * width + sample_x;
        };
        const auto p0_index = coordinate(-1);
        const auto p1_index = coordinate(-2);
        const auto p2_index = coordinate(-3);
        const auto q0_index = coordinate(0);
        const auto q1_index = coordinate(1);
        const auto q2_index = coordinate(2);
        const auto p0 = plane[p0_index];
        const auto p1 = plane[p1_index];
        const auto p2 = plane[p2_index];
        const auto q0 = plane[q0_index];
        const auto q1 = plane[q1_index];
        const auto q2 = plane[q2_index];
        if (std::abs(static_cast<std::int32_t>(p0) - q0) >= alpha ||
            std::abs(static_cast<std::int32_t>(p1) - p0) >= beta ||
            std::abs(static_cast<std::int32_t>(q1) - q0) >= beta) {
            continue;
        }
        if (strength == 4) {
            if (chroma) {
                plane[p0_index] = clip_sample(
                    (2 * static_cast<std::int32_t>(p1) + p0 + q1 + 2) >> 2);
                plane[q0_index] = clip_sample(
                    (2 * static_cast<std::int32_t>(q1) + q0 + p1 + 2) >> 2);
                continue;
            }
            const auto alpha_passes =
                std::abs(static_cast<std::int32_t>(p0) - q0) <
                (alpha >> 2) + 2;
            const auto p3 = plane[coordinate(-4)];
            const auto q3 = (vertical ? edge_x + 3 < width : edge_y + 3 < height)
                ? plane[coordinate(3)]
                : q2;
            if (std::abs(static_cast<std::int32_t>(p2) - p0) < beta &&
                alpha_passes) {
                plane[p0_index] = clip_sample(
                    (p2 + 2 * static_cast<std::int32_t>(p1) +
                     2 * static_cast<std::int32_t>(p0) +
                     2 * static_cast<std::int32_t>(q0) + q1 + 4) >>
                    3);
                plane[p1_index] = clip_sample(
                    (p2 + p1 + static_cast<std::int32_t>(p0) + q0 + 2) >> 2);
                plane[p2_index] = clip_sample(
                    (2 * static_cast<std::int32_t>(p3) +
                     3 * static_cast<std::int32_t>(p2) + p1 + p0 + q0 + 4) >>
                    3);
            } else {
                plane[p0_index] = clip_sample(
                    (2 * static_cast<std::int32_t>(p1) + p0 + q1 + 2) >> 2);
            }
            if (std::abs(static_cast<std::int32_t>(q2) - q0) < beta &&
                alpha_passes) {
                plane[q0_index] = clip_sample(
                    (p1 + 2 * static_cast<std::int32_t>(p0) +
                     2 * static_cast<std::int32_t>(q0) +
                     2 * static_cast<std::int32_t>(q1) + q2 + 4) >>
                    3);
                plane[q1_index] = clip_sample(
                    (p0 + q0 + static_cast<std::int32_t>(q1) + q2 + 2) >> 2);
                plane[q2_index] = clip_sample(
                    (2 * static_cast<std::int32_t>(q3) +
                     3 * static_cast<std::int32_t>(q2) + q1 + q0 + p0 + 4) >>
                    3);
            } else {
                plane[q0_index] = clip_sample(
                    (2 * static_cast<std::int32_t>(q1) + q0 + p1 + 2) >> 2);
            }
            continue;
        }

        const auto tc0 = deblock_tc0[static_cast<std::size_t>(strength - 1)]
                                    [static_cast<std::size_t>(index_a)];
        const auto smooth_p = !chroma && std::abs(
            static_cast<std::int32_t>(p2) - p0) < beta;
        const auto smooth_q = !chroma && std::abs(
            static_cast<std::int32_t>(q2) - q0) < beta;
        const auto tc = static_cast<std::int32_t>(tc0) +
                        (chroma ? 1 : smooth_p + smooth_q);
        const auto delta = std::clamp(
            (((static_cast<std::int32_t>(q0) - p0) * 4 +
              static_cast<std::int32_t>(p1) - q1 + 4) >>
             3),
            -tc, tc);
        plane[p0_index] = clip_sample(static_cast<std::int32_t>(p0) + delta);
        plane[q0_index] = clip_sample(static_cast<std::int32_t>(q0) - delta);
        if (smooth_p) {
            const auto delta_p1 =
                (static_cast<std::int32_t>(p2) +
                 ((static_cast<std::int32_t>(p0) + q0 + 1) >> 1) -
                 2 * static_cast<std::int32_t>(p1)) >>
                1;
            plane[p1_index] = clip_sample(
                static_cast<std::int32_t>(p1) +
                std::clamp(delta_p1, -static_cast<std::int32_t>(tc0),
                           static_cast<std::int32_t>(tc0)));
        }
        if (smooth_q) {
            const auto delta_q1 =
                (static_cast<std::int32_t>(q2) +
                 ((static_cast<std::int32_t>(p0) + q0 + 1) >> 1) -
                 2 * static_cast<std::int32_t>(q1)) >>
                1;
            plane[q1_index] = clip_sample(
                static_cast<std::int32_t>(q1) +
                std::clamp(delta_q1, -static_cast<std::int32_t>(tc0),
                           static_cast<std::int32_t>(tc0)));
        }
    }
}

void deblock_p_inter_frame(
    Yuv420Frame& frame, std::uint32_t macroblock_width,
    std::uint32_t macroblock_height, const std::vector<std::int32_t>& qpy,
    const std::vector<MacroblockMotionVectors>& motion_vectors,
    const std::vector<MacroblockMotionVectors>& motion_vectors_l1,
    const std::vector<std::array<std::array<std::int32_t, 16>, 2>>&
        reference_indices,
    const std::vector<std::array<std::uint8_t, 16>>& prediction_flags,
    const std::vector<std::uint8_t>& macroblock_intra16,
    const std::vector<std::uint8_t>& luma_total_coefficients,
    const std::vector<std::size_t>& slice_ids,
    const std::vector<std::uint8_t>& disable_idc,
    const std::vector<std::int64_t>& alpha_offsets,
    const std::vector<std::int64_t>& beta_offsets, const PpsInfo& pps,
    const std::vector<const Yuv420Frame*>& reference_frames_l0,
    const std::vector<const Yuv420Frame*>& reference_frames_l1) {
    const auto macroblock_count =
        checked_plane_size(macroblock_width, macroblock_height);
    const auto luma_block_width =
        static_cast<std::size_t>(macroblock_width) * 4;
    if (frame.width != macroblock_width * 16 ||
        frame.height != macroblock_height * 16 ||
        qpy.size() != macroblock_count ||
        motion_vectors.size() != macroblock_count ||
        motion_vectors_l1.size() != macroblock_count ||
        reference_indices.size() != macroblock_count ||
        prediction_flags.size() != macroblock_count ||
        macroblock_intra16.size() != macroblock_count ||
        slice_ids.size() != macroblock_count ||
        disable_idc.size() != macroblock_count ||
        alpha_offsets.size() != macroblock_count ||
        beta_offsets.size() != macroblock_count ||
        luma_total_coefficients.size() !=
            checked_plane_size(static_cast<std::uint32_t>(luma_block_width),
                               macroblock_height * 4)) {
        throw std::runtime_error(
            "P-picture deblocking requires complete macroblock metadata");
    }
    const auto different_by_at_least_four = [](
        std::int64_t first, std::int64_t second) {
        constexpr auto maximum = std::numeric_limits<std::int64_t>::max();
        return (first <= maximum - 4 && second > first + 3) ||
               (second <= maximum - 4 && first > second + 3);
    };
    struct Prediction {
        const Yuv420Frame* reference = nullptr;
        MotionVector motion{};
    };
    const auto block_predictions =
        [&](std::size_t address, std::size_t block_index) {
        std::array<Prediction, 2> result{};
        std::size_t count = 0;
        for (std::size_t list_index = 0; list_index < 2; ++list_index) {
            if ((prediction_flags[address][block_index] &
                 (1U << list_index)) == 0) {
                continue;
            }
            const auto reference_index =
                reference_indices[address][list_index][block_index];
            const auto& references =
                list_index == 0 ? reference_frames_l0 : reference_frames_l1;
            if (reference_index < 0 ||
                static_cast<std::size_t>(reference_index) >=
                    references.size() ||
                references[static_cast<std::size_t>(reference_index)] ==
                    nullptr) {
                throw std::runtime_error(
                    "deblocking prediction references an unavailable picture");
            }
            result[count++] = {
                references[static_cast<std::size_t>(reference_index)],
                list_index == 0
                    ? motion_vectors[address][block_index]
                    : motion_vectors_l1[address][block_index]};
        }
        return std::pair{result, count};
    };
    const auto predictions_differ =
        [&](std::size_t p_address, std::size_t p_block,
            std::size_t q_address, std::size_t q_block) {
        const auto [p, p_count] = block_predictions(p_address, p_block);
        const auto [q, q_count] = block_predictions(q_address, q_block);
        if (p_count != q_count) {
            return true;
        }
        const auto matches = [&](const Prediction& first,
                                 const Prediction& second) {
            return first.reference == second.reference &&
                   !different_by_at_least_four(first.motion[0],
                                               second.motion[0]) &&
                   !different_by_at_least_four(first.motion[1],
                                               second.motion[1]);
        };
        if (p_count == 0) {
            return false;
        }
        if (p_count == 1) {
            return !matches(p[0], q[0]);
        }
        const bool same_order =
            matches(p[0], q[0]) && matches(p[1], q[1]);
        const bool swapped_order =
            matches(p[0], q[1]) && matches(p[1], q[0]);
        return !same_order && !swapped_order;
    };
    const auto chroma_width = frame.width / 2;
    for (std::size_t macroblock_address = 0;
         macroblock_address < macroblock_count; ++macroblock_address) {
        if (disable_idc[macroblock_address] == 1) {
            continue;
        }
        const auto macroblock_x = static_cast<std::uint32_t>(
            macroblock_address % macroblock_width);
        const auto macroblock_y = static_cast<std::uint32_t>(
            macroblock_address / macroblock_width);
        for (const bool vertical : {true, false}) {
            for (const auto edge : {0U, 4U, 8U, 12U}) {
                if (edge == 0 &&
                    (vertical ? macroblock_x == 0 : macroblock_y == 0)) {
                    continue;
                }
                const auto neighbor_address = edge == 0
                    ? macroblock_address -
                          (vertical ? 1 : macroblock_width)
                    : macroblock_address;
                const auto slice_boundary =
                    slice_ids[neighbor_address] !=
                    slice_ids[macroblock_address];
                if (edge == 0 && disable_idc[macroblock_address] == 2 &&
                    slice_boundary) {
                    continue;
                }
                const auto qp_p = qpy[neighbor_address];
                const auto qp_q = qpy[macroblock_address];
                std::array<std::uint8_t, 4> strengths{};
                for (std::uint32_t segment = 0; segment < 4; ++segment) {
                    std::uint32_t p_block = 0;
                    std::uint32_t q_block = 0;
                    if (vertical) {
                        q_block = segment * 4 + edge / 4;
                        p_block = edge == 0
                            ? segment * 4 + 3
                            : q_block - 1;
                    } else {
                        q_block = edge / 4 * 4 + segment;
                        p_block = edge == 0
                            ? 12 + segment
                            : q_block - 4;
                    }
                    const auto p_macroblock_x = edge == 0 && vertical
                        ? macroblock_x - 1
                        : macroblock_x;
                    const auto p_macroblock_y = edge == 0 && !vertical
                        ? macroblock_y - 1
                        : macroblock_y;
                    const auto p_block_x = p_macroblock_x * 4 + p_block % 4;
                    const auto p_block_y = p_macroblock_y * 4 + p_block / 4;
                    const auto q_block_x = macroblock_x * 4 + q_block % 4;
                    const auto q_block_y = macroblock_y * 4 + q_block / 4;
                    const auto p_has_coefficients =
                        luma_total_coefficients[
                            static_cast<std::size_t>(p_block_y) *
                                luma_block_width +
                            p_block_x] != 0;
                    const auto q_has_coefficients =
                        luma_total_coefficients[
                            static_cast<std::size_t>(q_block_y) *
                                luma_block_width +
                            q_block_x] != 0;
                    if (macroblock_intra16[neighbor_address] ||
                        macroblock_intra16[macroblock_address]) {
                        strengths[segment] = edge == 0 ? 4 : 3;
                    } else if (p_has_coefficients || q_has_coefficients) {
                        strengths[segment] = 2;
                    } else {
                        strengths[segment] = predictions_differ(
                            neighbor_address, p_block, macroblock_address,
                            q_block)
                            ? 1
                            : 0;
                    }
                    const auto x = macroblock_x * 16 +
                        (vertical ? edge : segment * 4);
                    const auto y = macroblock_y * 16 +
                        (vertical ? segment * 4 : edge);
                    filter_deblocking_edge(
                        frame.luma, frame.width, frame.height, x, y,
                        vertical, 4, strengths[segment], qp_p, qp_q,
                        alpha_offsets[macroblock_address],
                        beta_offsets[macroblock_address], false);
                }
                if (edge != 0 && edge != 8) {
                    continue;
                }
                for (std::size_t plane_index = 0; plane_index < 2;
                     ++plane_index) {
                    const auto qp_offset = plane_index == 0
                        ? pps.chroma_qp_index_offset
                        : pps.second_chroma_qp_index_offset;
                    auto& plane = plane_index == 0 ? frame.chroma_u
                                                   : frame.chroma_v;
                    const auto chroma_qp_p = chroma_qp(qp_p, qp_offset);
                    const auto chroma_qp_q = chroma_qp(qp_q, qp_offset);
                    for (std::uint32_t segment = 0; segment < 4; ++segment) {
                        const auto x = macroblock_x * 8 +
                            (vertical ? edge / 2 : segment * 2);
                        const auto y = macroblock_y * 8 +
                            (vertical ? segment * 2 : edge / 2);
                        filter_deblocking_edge(
                            plane, chroma_width, frame.height / 2, x, y,
                            vertical, 2, strengths[segment], chroma_qp_p,
                            chroma_qp_q, alpha_offsets[macroblock_address],
                            beta_offsets[macroblock_address], true);
                    }
                }
            }
        }
    }
}

void deblock_idr_intra_frame(
    Yuv420Frame& frame, std::uint32_t macroblock_width,
    std::uint32_t macroblock_height, const std::vector<std::int32_t>& qpy,
    const std::vector<std::size_t>& slice_ids,
    const std::vector<std::uint8_t>& disable_idc,
    const std::vector<std::int64_t>& alpha_offsets,
    const std::vector<std::int64_t>& beta_offsets,
    const PpsInfo& pps) {
    if (std::all_of(disable_idc.begin(), disable_idc.end(),
                    [](std::uint8_t idc) { return idc == 1; })) {
        return;
    }
    if (frame.width != macroblock_width * 16 ||
        frame.height != macroblock_height * 16) {
        throw std::runtime_error(
            "deblocking currently requires uncropped macroblock dimensions");
    }
    const auto chroma_width = frame.width / 2;
    for (std::size_t mb_address = 0; mb_address < qpy.size(); ++mb_address) {
        const auto mb_x = static_cast<std::uint32_t>(mb_address % macroblock_width);
        const auto mb_y = static_cast<std::uint32_t>(mb_address / macroblock_width);
        const auto idc = disable_idc[mb_address];
        const auto can_filter_edge = [&](std::size_t neighbor) {
            if (idc == 1 || disable_idc[neighbor] == 1) {
                return false;
            }
            return idc != 2 || slice_ids[neighbor] == slice_ids[mb_address];
        };
        const auto filter_edge = [&](std::uint32_t x, std::uint32_t y,
                                     bool vertical, std::uint32_t luma_segment,
                                     std::uint8_t strength,
                                     std::int32_t qp_p,
                                     std::int32_t qp_q,
                                     std::size_t macroblock_q) {
            filter_deblocking_edge(
                frame.luma, frame.width, frame.height, x, y, vertical,
                luma_segment, strength, qp_p, qp_q,
                alpha_offsets[macroblock_q], beta_offsets[macroblock_q],
                false);
        };
        if (mb_x > 0) {
            const auto neighbor = mb_address - 1;
            if (can_filter_edge(neighbor)) {
                for (std::uint32_t segment = 0; segment < 4; ++segment) {
                    filter_edge(mb_x * 16, mb_y * 16 + segment * 4, true, 4,
                                4, qpy[neighbor], qpy[mb_address], mb_address);
                }
                for (std::uint32_t segment = 0; segment < 4; ++segment) {
                    for (std::size_t plane_index = 0; plane_index < 2;
                         ++plane_index) {
                        const auto offset = plane_index == 0
                            ? pps.chroma_qp_index_offset
                            : pps.second_chroma_qp_index_offset;
                        auto& plane = plane_index == 0 ? frame.chroma_u
                                                       : frame.chroma_v;
                        filter_deblocking_edge(
                            plane, chroma_width, frame.height / 2,
                            mb_x * 8, mb_y * 8 + segment * 2, true, 2, 4,
                            chroma_qp(qpy[neighbor], offset),
                            chroma_qp(qpy[mb_address], offset),
                            alpha_offsets[mb_address], beta_offsets[mb_address],
                            true);
                    }
                }
            }
        }
        if (idc != 1) {
            for (const auto edge : {4U, 8U, 12U}) {
                for (std::uint32_t segment = 0; segment < 4; ++segment) {
                    filter_edge(mb_x * 16 + edge,
                                mb_y * 16 + segment * 4, true, 4, 3,
                                qpy[mb_address], qpy[mb_address], mb_address);
                }
                if (edge == 8) {
                    filter_deblocking_edge(
                        frame.chroma_u, chroma_width, frame.height / 2,
                        mb_x * 8 + 4, mb_y * 8, true, 8, 3,
                        chroma_qp(qpy[mb_address], pps.chroma_qp_index_offset),
                        chroma_qp(qpy[mb_address], pps.chroma_qp_index_offset),
                        alpha_offsets[mb_address], beta_offsets[mb_address], true);
                    filter_deblocking_edge(
                        frame.chroma_v, chroma_width, frame.height / 2,
                        mb_x * 8 + 4, mb_y * 8, true, 8, 3,
                        chroma_qp(qpy[mb_address],
                                  pps.second_chroma_qp_index_offset),
                        chroma_qp(qpy[mb_address],
                                  pps.second_chroma_qp_index_offset),
                        alpha_offsets[mb_address], beta_offsets[mb_address], true);
                }
            }
        }
        if (mb_y > 0) {
            const auto neighbor = mb_address - macroblock_width;
            if (can_filter_edge(neighbor)) {
                for (std::uint32_t segment = 0; segment < 4; ++segment) {
                    filter_edge(mb_x * 16 + segment * 4, mb_y * 16, false, 4,
                                4, qpy[neighbor], qpy[mb_address], mb_address);
                }
                for (std::uint32_t segment = 0; segment < 4; ++segment) {
                    for (std::size_t plane_index = 0; plane_index < 2;
                         ++plane_index) {
                        const auto offset = plane_index == 0
                            ? pps.chroma_qp_index_offset
                            : pps.second_chroma_qp_index_offset;
                        auto& plane = plane_index == 0 ? frame.chroma_u
                                                       : frame.chroma_v;
                        filter_deblocking_edge(
                            plane, chroma_width, frame.height / 2,
                            mb_x * 8 + segment * 2, mb_y * 8, false, 2, 4,
                            chroma_qp(qpy[neighbor], offset),
                            chroma_qp(qpy[mb_address], offset),
                            alpha_offsets[mb_address], beta_offsets[mb_address],
                            true);
                    }
                }
            }
        }
        if (idc != 1) {
            for (const auto edge : {4U, 8U, 12U}) {
                for (std::uint32_t segment = 0; segment < 4; ++segment) {
                    filter_edge(mb_x * 16 + segment * 4,
                                mb_y * 16 + edge, false, 4, 3,
                                qpy[mb_address], qpy[mb_address], mb_address);
                }
                if (edge == 8) {
                    filter_deblocking_edge(
                        frame.chroma_u, chroma_width, frame.height / 2,
                        mb_x * 8, mb_y * 8 + 4, false, 8, 3,
                        chroma_qp(qpy[mb_address], pps.chroma_qp_index_offset),
                        chroma_qp(qpy[mb_address], pps.chroma_qp_index_offset),
                        alpha_offsets[mb_address], beta_offsets[mb_address], true);
                    filter_deblocking_edge(
                        frame.chroma_v, chroma_width, frame.height / 2,
                        mb_x * 8, mb_y * 8 + 4, false, 8, 3,
                        chroma_qp(qpy[mb_address],
                                  pps.second_chroma_qp_index_offset),
                        chroma_qp(qpy[mb_address],
                                  pps.second_chroma_qp_index_offset),
                        alpha_offsets[mb_address], beta_offsets[mb_address], true);
                }
            }
        }
    }
}

void require_same_picture(const IdrSliceHeader& expected,
                          const IdrSliceHeader& actual) {
    if (actual.picture_parameter_set_id != expected.picture_parameter_set_id ||
        actual.frame_num != expected.frame_num ||
        actual.idr_pic_id != expected.idr_pic_id ||
        actual.no_output_of_prior_pics != expected.no_output_of_prior_pics ||
        actual.long_term_reference != expected.long_term_reference ||
        actual.pic_order_cnt_lsb != expected.pic_order_cnt_lsb ||
        actual.delta_pic_order_bottom != expected.delta_pic_order_bottom ||
        actual.delta_pic_order_cnt0 != expected.delta_pic_order_cnt0 ||
        actual.delta_pic_order_cnt1 != expected.delta_pic_order_cnt1) {
        throw std::runtime_error(
            "IDR slices do not belong to the same coded picture");
    }
}

void write_luma_macroblock(BitReader& reader, Yuv420Frame& frame,
                           const SpsInfo& sps, std::uint32_t macroblock_x,
                           std::uint32_t macroblock_y) {
    for (std::uint32_t sample_y = 0; sample_y < 16; ++sample_y) {
        for (std::uint32_t sample_x = 0; sample_x < 16; ++sample_x) {
            const auto value = static_cast<std::uint8_t>(reader.read_bits(8));
            const auto coded_x = macroblock_x * 16 + sample_x;
            const auto coded_y = macroblock_y * 16 + sample_y;
            if (coded_x < sps.frame_crop_left ||
                coded_y < sps.frame_crop_top ||
                coded_x >= sps.frame_crop_left + frame.width ||
                coded_y >= sps.frame_crop_top + frame.height) {
                continue;
            }
            const auto x = coded_x - sps.frame_crop_left;
            const auto y = coded_y - sps.frame_crop_top;
            frame.luma[static_cast<std::size_t>(y) * frame.width + x] = value;
        }
    }
}

void write_chroma_macroblock(BitReader& reader, std::vector<std::uint8_t>& plane,
                             const Yuv420Frame& frame, const SpsInfo& sps,
                             std::uint32_t macroblock_x,
                             std::uint32_t macroblock_y) {
    const auto plane_width = frame.width / 2;
    const auto crop_left = sps.frame_crop_left / 2;
    const auto crop_top = sps.frame_crop_top / 2;
    for (std::uint32_t sample_y = 0; sample_y < 8; ++sample_y) {
        for (std::uint32_t sample_x = 0; sample_x < 8; ++sample_x) {
            const auto value = static_cast<std::uint8_t>(reader.read_bits(8));
            const auto coded_x = macroblock_x * 8 + sample_x;
            const auto coded_y = macroblock_y * 8 + sample_y;
            if (coded_x < crop_left || coded_y < crop_top ||
                coded_x >= crop_left + plane_width ||
                coded_y >= crop_top + frame.height / 2) {
                continue;
            }
            const auto x = coded_x - crop_left;
            const auto y = coded_y - crop_top;
            plane[static_cast<std::size_t>(y) * plane_width + x] = value;
        }
    }
}

std::int64_t floor_shift(std::int64_t value, unsigned shift) {
    const auto divisor = std::int64_t{1} << shift;
    auto quotient = value / divisor;
    if (value < 0 && value % divisor != 0) {
        --quotient;
    }
    return quotient;
}

std::array<std::int64_t, 16> reconstruct_intra16x16_luma_dc(
    const CavlcResidualBlock& block, std::int32_t qpy) {
    static constexpr std::array<std::size_t, 16> scan{{
        0, 1, 4, 8, 5, 2, 3, 6, 9, 12, 13, 10, 7, 11, 14, 15,
    }};
    static constexpr std::array<std::array<std::int64_t, 3>, 6> scale{{
        {{10, 13, 16}}, {{11, 14, 18}}, {{13, 16, 20}},
        {{14, 18, 23}}, {{16, 20, 25}}, {{18, 23, 29}},
    }};

    std::array<std::int64_t, 16> levels{};
    for (std::size_t index = 0; index < scan.size(); ++index) {
        levels[scan[index]] = block.coefficients[index];
    }
    const auto hadamard = [](const std::array<std::int64_t, 4>& values) {
        return std::array<std::int64_t, 4>{{
            values[0] + values[1] + values[2] + values[3],
            values[0] + values[1] - values[2] - values[3],
            values[0] - values[1] - values[2] + values[3],
            values[0] - values[1] + values[2] - values[3],
        }};
    };

    std::array<std::int64_t, 16> horizontal{};
    for (std::size_t row = 0; row < 4; ++row) {
        const auto transformed = hadamard({{
            levels[row * 4], levels[row * 4 + 1],
            levels[row * 4 + 2], levels[row * 4 + 3],
        }});
        std::copy(transformed.begin(), transformed.end(),
                  horizontal.begin() + static_cast<std::ptrdiff_t>(row * 4));
    }

    std::array<std::int64_t, 16> dc_residuals{};
    const auto factor = scale[static_cast<std::size_t>(qpy % 6)][0] * 16;
    for (std::size_t column = 0; column < 4; ++column) {
        const auto transformed = hadamard({{
            horizontal[column], horizontal[4 + column],
            horizontal[8 + column], horizontal[12 + column],
        }});
        for (std::size_t row = 0; row < 4; ++row) {
            const auto hadamard_value = transformed[row];
            if (hadamard_value < -32768 || hadamard_value > 32767) {
                throw std::runtime_error(
                    "I16x16 luma DC Hadamard coefficient exceeds 8-bit range");
            }
            auto scaled = std::int64_t{0};
            if (qpy >= 36) {
                scaled = (hadamard_value * factor) *
                         (std::int64_t{1} << (qpy / 6 - 6));
            } else {
                const auto shift = static_cast<unsigned>(6 - qpy / 6);
                scaled = floor_shift(
                    hadamard_value * factor +
                        (std::int64_t{1} << (5 - qpy / 6)),
                    shift);
            }
            if (scaled < -32768 || scaled > 32767) {
                throw std::runtime_error(
                    "I16x16 scaled luma DC coefficient exceeds 8-bit range");
            }
            dc_residuals[row * 4 + column] = scaled;
        }
    }
    return dc_residuals;
}

std::array<std::int64_t, 16> inverse_transform_luma4x4(
    const std::array<std::int64_t, 16>& coefficients) {
    const auto transform_line = [](const std::array<std::int64_t, 4>& values) {
        const auto s0 = values[0] + values[2];
        const auto s1 = values[0] - values[2];
        const auto s2 = floor_shift(values[1], 1) - values[3];
        const auto s3 = values[1] + floor_shift(values[3], 1);
        return std::array<std::int64_t, 4>{{
            s0 + s3, s1 + s2, s1 - s2, s0 - s3,
        }};
    };
    std::array<std::int64_t, 16> horizontal{};
    for (std::size_t row = 0; row < 4; ++row) {
        const auto transformed = transform_line({{
            coefficients[row * 4], coefficients[row * 4 + 1],
            coefficients[row * 4 + 2], coefficients[row * 4 + 3],
        }});
        std::copy(transformed.begin(), transformed.end(),
                  horizontal.begin() + static_cast<std::ptrdiff_t>(row * 4));
    }
    std::array<std::int64_t, 16> residual{};
    for (std::size_t column = 0; column < 4; ++column) {
        const auto transformed = transform_line({{
            horizontal[column], horizontal[4 + column],
            horizontal[8 + column], horizontal[12 + column],
        }});
        for (std::size_t row = 0; row < 4; ++row) {
            residual[row * 4 + column] =
                floor_shift(transformed[row] + 32, 6);
        }
    }
    return residual;
}

std::array<std::int64_t, 16> inverse_scale_intra4x4_ac(
    const CavlcResidualBlock& block, std::int32_t qpy,
    std::int64_t dc_coefficient) {
    static constexpr std::array<std::size_t, 16> scan{{
        0, 1, 4, 8, 5, 2, 3, 6, 9, 12, 13, 10, 7, 11, 14, 15,
    }};
    static constexpr std::array<std::array<std::int64_t, 3>, 6> scale{{
        {{10, 13, 16}}, {{11, 14, 18}}, {{13, 16, 20}},
        {{14, 18, 23}}, {{16, 20, 25}}, {{18, 23, 29}},
    }};
    if (block.coefficients.size() != 15 || qpy < 0 || qpy > 51) {
        throw std::runtime_error("invalid I16x16 AC reconstruction input");
    }
    std::array<std::int64_t, 16> coefficients{};
    coefficients[0] = dc_coefficient;
    for (std::size_t scan_index = 1; scan_index < scan.size(); ++scan_index) {
        const auto natural_index = scan[scan_index];
        const auto row = natural_index / 4;
        const auto column = natural_index % 4;
        const auto factor_class = row % 2 + column % 2;
        const auto value =
            static_cast<std::int64_t>(block.coefficients[scan_index - 1]) *
            scale[static_cast<std::size_t>(qpy % 6)][factor_class] * 16;
        coefficients[natural_index] =
            qpy >= 24
                ? value * (std::int64_t{1} << (qpy / 6 - 4))
                : floor_shift(value +
                                  (std::int64_t{1} << (3 - qpy / 6)),
                              static_cast<unsigned>(4 - qpy / 6));
    }
    return coefficients;
}

std::array<std::int64_t, 16> inverse_scale_intra4x4(
    const CavlcResidualBlock& block, std::int32_t qpy) {
    static constexpr std::array<std::size_t, 16> scan{{
        0, 1, 4, 8, 5, 2, 3, 6, 9, 12, 13, 10, 7, 11, 14, 15,
    }};
    static constexpr std::array<std::array<std::int64_t, 3>, 6> scale{{
        {{10, 13, 16}}, {{11, 14, 18}}, {{13, 16, 20}},
        {{14, 18, 23}}, {{16, 20, 25}}, {{18, 23, 29}},
    }};
    if (block.coefficients.size() != 16 || qpy < 0 || qpy > 51) {
        throw std::runtime_error("invalid I4x4 residual reconstruction input");
    }
    std::array<std::int64_t, 16> coefficients{};
    for (std::size_t scan_index = 0; scan_index < scan.size(); ++scan_index) {
        const auto natural_index = scan[scan_index];
        const auto row = natural_index / 4;
        const auto column = natural_index % 4;
        const auto factor_class = row % 2 + column % 2;
        const auto value =
            static_cast<std::int64_t>(block.coefficients[scan_index]) *
            scale[static_cast<std::size_t>(qpy % 6)][factor_class] * 16;
        coefficients[natural_index] =
            qpy >= 24
                ? value * (std::int64_t{1} << (qpy / 6 - 4))
                : floor_shift(value +
                                  (std::int64_t{1} << (3 - qpy / 6)),
                              static_cast<unsigned>(4 - qpy / 6));
    }
    return coefficients;
}

std::int32_t chroma_qp(std::int32_t qpy, std::int64_t chroma_qp_offset) {
    static constexpr std::array<std::int32_t, 52> qp_table{{
        0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17,
        18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 29, 30, 31, 32,
        32, 33, 34, 34, 35, 35, 36, 36, 37, 37, 37, 38, 38, 38, 39, 39,
        39, 39,
    }};
    const auto index = std::clamp<std::int64_t>(
        static_cast<std::int64_t>(qpy) + chroma_qp_offset, 0, 51);
    return qp_table[static_cast<std::size_t>(index)];
}

std::array<std::int64_t, 4> reconstruct_intra16x16_chroma_dc(
    const CavlcResidualBlock& block, std::int32_t qpc) {
    static constexpr std::array<std::int64_t, 6> scale{{10, 11, 13, 14, 16, 18}};
    if (block.coefficients.size() != 4 || qpc < 0 || qpc > 51) {
        throw std::runtime_error("invalid I16x16 chroma DC reconstruction input");
    }
    const auto c0 = static_cast<std::int64_t>(block.coefficients[0]);
    const auto c1 = static_cast<std::int64_t>(block.coefficients[1]);
    const auto c2 = static_cast<std::int64_t>(block.coefficients[2]);
    const auto c3 = static_cast<std::int64_t>(block.coefficients[3]);
    const std::array<std::int64_t, 4> transformed{{
        c0 + c1 + c2 + c3,
        c0 - c1 + c2 - c3,
        c0 + c1 - c2 - c3,
        c0 - c1 - c2 + c3,
    }};
    const auto multiplier = scale[static_cast<std::size_t>(qpc % 6)] * 16;
    std::array<std::int64_t, 4> scaled{};
    for (std::size_t index = 0; index < scaled.size(); ++index) {
        const auto value = transformed[index] * multiplier *
                           (std::int64_t{1} << (qpc / 6));
        scaled[index] = floor_shift(value, 5);
        if (scaled[index] < -32768 || scaled[index] > 32767) {
            throw std::runtime_error(
                "I16x16 chroma DC coefficient exceeds 8-bit range");
        }
    }
    return scaled;
}

std::array<std::uint32_t, 2> luma_block_position(std::uint32_t block_index) {
    return {{
        (block_index / 4 % 2) * 2 + (block_index % 4) % 2,
        (block_index / 8) * 2 + (block_index % 4) / 2,
    }};
}

void add_intra16x16_chroma_residual(
    std::vector<std::uint8_t>& plane, std::uint32_t plane_width,
    std::uint32_t macroblock_x, std::uint32_t macroblock_y,
    const std::array<std::array<std::int64_t, 16>, 4>& residual_blocks) {
    const auto x = macroblock_x * 8U;
    const auto y = macroblock_y * 8U;
    for (std::uint32_t block_index = 0; block_index < 4; ++block_index) {
        const auto block_x = (block_index % 2) * 4;
        const auto block_y = (block_index / 2) * 4;
        for (std::uint32_t row = 0; row < 4; ++row) {
            for (std::uint32_t column = 0; column < 4; ++column) {
                const auto sample_x = x + block_x + column;
                const auto sample_y = y + block_y + row;
                auto& sample = plane[
                    static_cast<std::size_t>(sample_y) * plane_width +
                    sample_x];
                sample = static_cast<std::uint8_t>(
                    std::clamp<std::int64_t>(
                        static_cast<std::int64_t>(sample) +
                            residual_blocks[block_index][row * 4 + column],
                        0, 255));
            }
        }
    }
}

void add_intra16x16_luma_residual(
    Yuv420Frame& frame, std::uint32_t macroblock_x,
    std::uint32_t macroblock_y,
    const std::array<std::array<std::int64_t, 16>, 16>& residual_blocks) {
    const auto x = macroblock_x * 16U;
    const auto y = macroblock_y * 16U;
    for (std::uint32_t block_index = 0; block_index < 16; ++block_index) {
        const auto block_position = luma_block_position(block_index);
        const auto block_x = block_position[0];
        const auto block_y = block_position[1];
        for (std::uint32_t row = 0; row < 4; ++row) {
            for (std::uint32_t column = 0; column < 4; ++column) {
                const auto sample_x = x + block_x * 4 + column;
                const auto sample_y = y + block_y * 4 + row;
                auto& sample = frame.luma[
                    static_cast<std::size_t>(sample_y) * frame.width +
                    sample_x];
                sample = static_cast<std::uint8_t>(
                    std::clamp<std::int64_t>(
                        static_cast<std::int64_t>(sample) +
                            residual_blocks[block_index][row * 4 + column],
                        0, 255));
            }
        }
    }
}

void predict_intra16x16(Yuv420Frame& frame, std::uint32_t macroblock_x,
                        std::uint32_t macroblock_y, std::uint8_t mode) {
    if (frame.width % 16 != 0 || frame.height % 16 != 0) {
        throw std::runtime_error(
            "I16x16 prediction currently requires uncropped macroblock dimensions");
    }
    const auto x = macroblock_x * 16U;
    const auto y = macroblock_y * 16U;
    const bool top_available = macroblock_y != 0;
    const bool left_available = macroblock_x != 0;
    std::array<std::uint8_t, 16> top{};
    std::array<std::uint8_t, 16> left{};
    if (top_available) {
        std::copy_n(frame.luma.begin() +
                        static_cast<std::ptrdiff_t>((y - 1) * frame.width + x),
                    16, top.begin());
    }
    if (left_available) {
        for (std::uint32_t index = 0; index < 16; ++index) {
            left[index] =
                frame.luma[static_cast<std::size_t>(y + index) * frame.width +
                           x - 1];
        }
    }

    std::array<std::uint8_t, 256> prediction{};
    if (mode == 0) {
        if (!top_available) {
            throw std::runtime_error(
                "I16x16 vertical mode has no top reference samples");
        }
        for (std::uint32_t row = 0; row < 16; ++row) {
            std::copy(top.begin(), top.end(),
                      prediction.begin() + static_cast<std::ptrdiff_t>(row * 16));
        }
    } else if (mode == 1) {
        if (!left_available) {
            throw std::runtime_error(
                "I16x16 horizontal mode has no left reference samples");
        }
        for (std::uint32_t row = 0; row < 16; ++row) {
            std::fill_n(prediction.begin() +
                            static_cast<std::ptrdiff_t>(row * 16),
                        16, left[row]);
        }
    } else if (mode == 2) {
        std::uint32_t dc = 128;
        if (top_available && left_available) {
            dc = (std::accumulate(top.begin(), top.end(), 0U) +
                  std::accumulate(left.begin(), left.end(), 0U) + 16) >>
                 5;
        } else if (top_available) {
            dc = (std::accumulate(top.begin(), top.end(), 0U) + 8) >> 4;
        } else if (left_available) {
            dc = (std::accumulate(left.begin(), left.end(), 0U) + 8) >> 4;
        }
        prediction.fill(static_cast<std::uint8_t>(dc));
    } else if (mode == 3) {
        if (!top_available || !left_available) {
            throw std::runtime_error(
                "I16x16 plane mode requires top and left references");
        }
        const auto top_left =
            frame.luma[static_cast<std::size_t>(y - 1) * frame.width + x - 1];
        std::int32_t horizontal_gradient = 0;
        std::int32_t vertical_gradient = 0;
        for (std::int32_t index = 1; index <= 7; ++index) {
            horizontal_gradient +=
                index * (static_cast<std::int32_t>(top[7 + index]) -
                         static_cast<std::int32_t>(top[7 - index]));
            vertical_gradient +=
                index * (static_cast<std::int32_t>(left[7 + index]) -
                         static_cast<std::int32_t>(left[7 - index]));
        }
        horizontal_gradient +=
            8 * (static_cast<std::int32_t>(top[15]) - top_left);
        vertical_gradient +=
            8 * (static_cast<std::int32_t>(left[15]) - top_left);
        const auto a = 16 * (static_cast<std::int32_t>(top[15]) + left[15]);
        const auto b = (5 * horizontal_gradient + 32) >> 6;
        const auto c = (5 * vertical_gradient + 32) >> 6;
        for (std::int32_t row = 0; row < 16; ++row) {
            for (std::int32_t column = 0; column < 16; ++column) {
                const auto value =
                    (a + b * (column - 7) + c * (row - 7) + 16) >> 5;
                prediction[static_cast<std::size_t>(row * 16 + column)] =
                    static_cast<std::uint8_t>(
                        std::clamp(value, 0, 255));
            }
        }
    } else {
        throw std::runtime_error("I16x16 prediction mode exceeds 3");
    }
    for (std::uint32_t row = 0; row < 16; ++row) {
        std::copy_n(prediction.begin() +
                        static_cast<std::ptrdiff_t>(row * 16),
                    16, frame.luma.begin() +
                            static_cast<std::ptrdiff_t>((y + row) * frame.width + x));
    }
}

std::array<std::uint8_t, 16> predict_intra4x4(
    std::uint8_t mode, const std::array<std::uint8_t, 8>& top,
    bool top_available, const std::array<std::uint8_t, 4>& left,
    bool left_available, std::uint8_t top_left,
    bool top_left_available) {
    std::array<std::uint8_t, 16> prediction{};
    const auto average2 = [](std::uint8_t a, std::uint8_t b) {
        return static_cast<std::uint8_t>((a + b + 1) >> 1);
    };
    const auto filtered3 = [](std::uint8_t a, std::uint8_t b,
                              std::uint8_t c) {
        return static_cast<std::uint8_t>((a + 2U * b + c + 2) >> 2);
    };
    const auto top_sample = [&](std::int32_t index) {
        return index < 0 ? top_left : top[static_cast<std::size_t>(index)];
    };
    const auto left_sample = [&](std::int32_t index) {
        return index < 0 ? top_left : left[static_cast<std::size_t>(index)];
    };
    if (mode == 0) {
        if (!top_available) {
            throw std::runtime_error(
                "I4x4 vertical mode has no top reference samples");
        }
        for (std::uint32_t row = 0; row < 4; ++row) {
            std::copy_n(top.begin(), 4,
                        prediction.begin() + static_cast<std::ptrdiff_t>(row * 4));
        }
    } else if (mode == 1) {
        if (!left_available) {
            throw std::runtime_error(
                "I4x4 horizontal mode has no left reference samples");
        }
        for (std::uint32_t row = 0; row < 4; ++row) {
            std::fill_n(prediction.begin() +
                            static_cast<std::ptrdiff_t>(row * 4),
                        4, left[row]);
        }
    } else if (mode == 2) {
        std::uint32_t dc = 128;
        if (top_available && left_available) {
            dc = (std::accumulate(top.begin(), top.begin() + 4, 0U) +
                  std::accumulate(left.begin(), left.end(), 0U) + 4) >>
                 3;
        } else if (top_available) {
            dc = (std::accumulate(top.begin(), top.begin() + 4, 0U) + 2) >> 2;
        } else if (left_available) {
            dc = (std::accumulate(left.begin(), left.end(), 0U) + 2) >> 2;
        }
        prediction.fill(static_cast<std::uint8_t>(dc));
    } else if (mode == 3) {
        if (!top_available) {
            throw std::runtime_error(
                "I4x4 diagonal-down-left mode has no top references");
        }
        for (std::uint32_t row = 0; row < 4; ++row) {
            for (std::uint32_t column = 0; column < 4; ++column) {
                const auto index = column + row;
                const auto a = top[std::min(index, 7U)];
                const auto b = top[std::min(index + 1, 7U)];
                const auto c = top[std::min(index + 2, 7U)];
                prediction[row * 4 + column] =
                    row == 3 && column == 3
                        ? static_cast<std::uint8_t>((top[6] + 3U * top[7] + 2) >> 2)
                        : filtered3(a, b, c);
            }
        }
    } else if (mode == 4) {
        if (!top_available || !left_available || !top_left_available) {
            throw std::runtime_error(
                "I4x4 diagonal-down-right mode lacks reference samples");
        }
        const auto top_sample = [&](std::int32_t index) {
            return index < 0 ? top_left
                             : top[static_cast<std::size_t>(index)];
        };
        const auto left_sample = [&](std::int32_t index) {
            return index < 0 ? top_left
                             : left[static_cast<std::size_t>(index)];
        };
        for (std::int32_t row = 0; row < 4; ++row) {
            for (std::int32_t column = 0; column < 4; ++column) {
                if (column > row) {
                    prediction[static_cast<std::size_t>(row * 4 + column)] =
                        filtered3(top_sample(column - row - 2),
                                  top_sample(column - row - 1),
                                  top_sample(column - row));
                } else if (column < row) {
                    prediction[static_cast<std::size_t>(row * 4 + column)] =
                        filtered3(left_sample(row - column - 2),
                                  left_sample(row - column - 1),
                                  left_sample(row - column));
                } else {
                    prediction[static_cast<std::size_t>(row * 4 + column)] =
                        filtered3(top[0], top_left, left[0]);
                }
            }
        }
    } else if (mode == 5 || mode == 6) {
        if (!top_available || !left_available || !top_left_available) {
            throw std::runtime_error(
                "I4x4 directional mode lacks reference samples");
        }
        for (std::int32_t row = 0; row < 4; ++row) {
            for (std::int32_t column = 0; column < 4; ++column) {
                if (mode == 5) {
                    const auto angle = 2 * column - row;
                    std::uint8_t value = 0;
                    if (angle >= 0 && angle % 2 == 0) {
                        const auto index = column - (row >> 1);
                        value = average2(top_sample(index - 1),
                                         top_sample(index));
                    } else if (angle >= 1) {
                        const auto index = column - (row >> 1);
                        value = filtered3(
                            top_sample(index - 2), top_sample(index - 1),
                            top_sample(index));
                    } else if (angle == -1) {
                        value = filtered3(left[0], top_left, top[0]);
                    } else {
                        value = filtered3(
                            left_sample(row - 1), left_sample(row - 2),
                            left_sample(row - 3));
                    }
                    prediction[static_cast<std::size_t>(row * 4 + column)] =
                        value;
                } else {
                    const auto angle = 2 * row - column;
                    std::uint8_t value = 0;
                    if (angle >= 0 && angle % 2 == 0) {
                        const auto index = row - (column >> 1);
                        value = average2(left_sample(index - 1),
                                         left_sample(index));
                    } else if (angle >= 1) {
                        const auto index = row - (column >> 1);
                        value = filtered3(
                            left_sample(index - 2), left_sample(index - 1),
                            left_sample(index));
                    } else if (angle == -1) {
                        value = filtered3(left[0], top_left, top[0]);
                    } else {
                        value = filtered3(
                            top_sample(column - 1), top_sample(column - 2),
                            top_sample(column - 3));
                    }
                    prediction[static_cast<std::size_t>(row * 4 + column)] =
                        value;
                }
            }
        }
    } else if (mode == 7) {
        if (!top_available) {
            throw std::runtime_error(
                "I4x4 vertical-left mode has no top references");
        }
        for (std::uint32_t row = 0; row < 4; ++row) {
            for (std::uint32_t column = 0; column < 4; ++column) {
                const auto index = column + (row >> 1);
                prediction[row * 4 + column] = row % 2 == 0
                    ? average2(top[index], top[index + 1])
                    : filtered3(top[index], top[index + 1], top[index + 2]);
            }
        }
    } else if (mode == 8) {
        if (!left_available) {
            throw std::runtime_error(
                "I4x4 horizontal-up mode has no left references");
        }
        for (std::uint32_t row = 0; row < 4; ++row) {
            for (std::uint32_t column = 0; column < 4; ++column) {
                const auto index = row + (column >> 1);
                const auto horizontal_phase = column + 2 * row;
                std::uint8_t value = 0;
                if (horizontal_phase > 5) {
                    value = left[3];
                } else if (horizontal_phase % 2 == 0) {
                    value = average2(left[index], left[std::min(index + 1, 3U)]);
                } else {
                    value = filtered3(
                        left[index], left[std::min(index + 1, 3U)],
                        left[std::min(index + 2, 3U)]);
                }
                prediction[row * 4 + column] = value;
            }
        }
    } else {
        throw std::runtime_error("I4x4 prediction mode exceeds 8");
    }
    return prediction;
}

std::array<std::int64_t, 8> inverse_transform_luma8x8_line(
    const std::array<std::int64_t, 8>& coefficients) {
    const auto a0 = coefficients[0] + coefficients[4];
    const auto a2 = coefficients[0] - coefficients[4];
    const auto a4 = (coefficients[2] >> 1) - coefficients[6];
    const auto a6 = coefficients[2] + (coefficients[6] >> 1);
    const auto b0 = a0 + a6;
    const auto b2 = a2 + a4;
    const auto b4 = a2 - a4;
    const auto b6 = a0 - a6;
    const auto a1 = -coefficients[3] + coefficients[5] - coefficients[7] -
                    (coefficients[7] >> 1);
    const auto a3 = coefficients[1] + coefficients[7] - coefficients[3] -
                    (coefficients[3] >> 1);
    const auto a5 = -coefficients[1] + coefficients[7] + coefficients[5] +
                    (coefficients[5] >> 1);
    const auto a7 = coefficients[3] + coefficients[5] + coefficients[1] +
                    (coefficients[1] >> 1);
    const auto b1 = a1 + (a7 >> 2);
    const auto b3 = a3 + (a5 >> 2);
    const auto b5 = (a3 >> 2) - a5;
    const auto b7 = a7 - (a1 >> 2);
    return {{b0 + b7, b2 + b5, b4 + b3, b6 + b1,
             b6 - b1, b4 - b3, b2 - b5, b0 - b7}};
}

std::array<std::int64_t, 64> inverse_transform_luma8x8(
    const std::array<std::int64_t, 64>& coefficients) {
    std::array<std::int64_t, 64> horizontal{};
    for (std::size_t row = 0; row < 8; ++row) {
        std::array<std::int64_t, 8> line{};
        std::copy_n(coefficients.begin() +
                        static_cast<std::ptrdiff_t>(row * 8),
                    8, line.begin());
        const auto transformed = inverse_transform_luma8x8_line(line);
        std::copy(transformed.begin(), transformed.end(),
                  horizontal.begin() + static_cast<std::ptrdiff_t>(row * 8));
    }

    std::array<std::int64_t, 64> residual{};
    for (std::size_t column = 0; column < 8; ++column) {
        std::array<std::int64_t, 8> line{};
        for (std::size_t row = 0; row < 8; ++row) {
            line[row] = horizontal[row * 8 + column];
        }
        const auto transformed = inverse_transform_luma8x8_line(line);
        for (std::size_t row = 0; row < 8; ++row) {
            residual[row * 8 + column] =
                floor_shift(transformed[row] + 32, 6);
        }
    }
    return residual;
}

std::array<std::int64_t, 64> inverse_scale_luma8x8(
    const std::array<std::int32_t, 64>& levels, std::int32_t qpy) {
    static constexpr std::array<std::array<std::int64_t, 6>, 6> factors{{
        {{20, 18, 32, 19, 25, 24}},
        {{22, 19, 35, 21, 28, 26}},
        {{26, 23, 42, 24, 33, 31}},
        {{28, 25, 45, 26, 35, 33}},
        {{32, 28, 51, 30, 40, 38}},
        {{36, 32, 58, 34, 46, 43}},
    }};
    static constexpr std::array<std::size_t, 64> factor_classes{{
        0, 3, 4, 3, 0, 3, 4, 3, 3, 1, 5, 1, 3, 1, 5, 1,
        4, 5, 2, 5, 4, 5, 2, 5, 3, 1, 5, 1, 3, 1, 5, 1,
        0, 3, 4, 3, 0, 3, 4, 3, 3, 1, 5, 1, 3, 1, 5, 1,
        4, 5, 2, 5, 4, 5, 2, 5, 3, 1, 5, 1, 3, 1, 5, 1,
    }};
    if (qpy < 0 || qpy > 51) {
        throw std::runtime_error("8x8 inverse scaling QPY is outside [0,51]");
    }
    std::array<std::int64_t, 64> scaled{};
    for (std::size_t index = 0; index < scaled.size(); ++index) {
        const auto value =
            static_cast<std::int64_t>(levels[index]) *
            factors[static_cast<std::size_t>(qpy % 6)]
                   [factor_classes[index]] *
            16;
        scaled[index] = qpy >= 36
                            ? value * (std::int64_t{1} << (qpy / 6 - 6))
                            : floor_shift(value +
                                              (std::int64_t{1}
                                               << (5 - qpy / 6)),
                                          static_cast<unsigned>(6 - qpy / 6));
    }
    return scaled;
}

std::array<std::uint8_t, 64> predict_intra8x8(
    std::uint8_t mode, std::array<std::uint8_t, 16> top,
    bool top_available, std::array<std::uint8_t, 16> left,
    bool left_available, std::uint8_t top_left,
    bool top_left_available) {
    const auto raw_top = top;
    const auto raw_left = left;
    const auto filtered3 = [](std::uint8_t a, std::uint8_t b,
                              std::uint8_t c) {
        return static_cast<std::uint8_t>((a + 2U * b + c + 2) >> 2);
    };
    if (top_available) {
        top[0] = top_left_available
                     ? filtered3(top_left, raw_top[0], raw_top[1])
                     : static_cast<std::uint8_t>(
                           (3U * raw_top[0] + raw_top[1] + 2) >> 2);
        for (std::size_t index = 1; index < 15; ++index) {
            top[index] = filtered3(raw_top[index - 1], raw_top[index],
                                   raw_top[index + 1]);
        }
        top[15] = static_cast<std::uint8_t>(
            (raw_top[14] + 3U * raw_top[15] + 2) >> 2);
    }
    if (left_available) {
        left[0] = top_left_available
                      ? filtered3(top_left, raw_left[0], raw_left[1])
                      : static_cast<std::uint8_t>(
                            (3U * raw_left[0] + raw_left[1] + 2) >> 2);
        for (std::size_t index = 1; index < 7; ++index) {
            left[index] = filtered3(raw_left[index - 1], raw_left[index],
                                    raw_left[index + 1]);
        }
        left[7] = static_cast<std::uint8_t>(
            (raw_left[6] + 3U * raw_left[7] + 2) >> 2);
        std::fill(left.begin() + 8, left.end(), left[7]);
    }
    if (top_left_available) {
        if (top_available && left_available) {
            top_left = filtered3(raw_top[0], top_left, raw_left[0]);
        } else if (top_available) {
            top_left = static_cast<std::uint8_t>(
                (3U * top_left + raw_top[0] + 2) >> 2);
        } else if (left_available) {
            top_left = static_cast<std::uint8_t>(
                (3U * top_left + raw_left[0] + 2) >> 2);
        }
    }

    const auto top_sample = [&](std::int32_t index) -> std::uint8_t {
        return index < 0 ? top_left : top[static_cast<std::size_t>(index)];
    };
    const auto left_sample = [&](std::int32_t index) -> std::uint8_t {
        return index < 0 ? top_left : left[static_cast<std::size_t>(index)];
    };
    std::array<std::uint8_t, 64> prediction{};
    if (mode == 0) {
        if (!top_available) {
            throw std::runtime_error("I8x8 vertical mode has no top references");
        }
        for (std::size_t row = 0; row < 8; ++row) {
            std::copy_n(top.begin(), 8, prediction.begin() +
                                               static_cast<std::ptrdiff_t>(row * 8));
        }
    } else if (mode == 1) {
        if (!left_available) {
            throw std::runtime_error(
                "I8x8 horizontal mode has no left references");
        }
        for (std::size_t row = 0; row < 8; ++row) {
            std::fill_n(prediction.begin() +
                            static_cast<std::ptrdiff_t>(row * 8),
                        8, left[row]);
        }
    } else if (mode == 2) {
        std::uint32_t dc = 128;
        if (top_available && left_available) {
            dc = (std::accumulate(top.begin(), top.begin() + 8, 0U) +
                  std::accumulate(left.begin(), left.begin() + 8, 0U) + 8) >> 4;
        } else if (top_available) {
            dc = (std::accumulate(top.begin(), top.begin() + 8, 0U) + 4) >> 3;
        } else if (left_available) {
            dc = (std::accumulate(left.begin(), left.begin() + 8, 0U) + 4) >> 3;
        }
        prediction.fill(static_cast<std::uint8_t>(dc));
    } else if (mode == 3) {
        if (!top_available) {
            throw std::runtime_error(
                "I8x8 diagonal-down-left mode has no top references");
        }
        for (std::size_t row = 0; row < 8; ++row) {
            for (std::size_t column = 0; column < 8; ++column) {
                const auto index = row + column;
                prediction[row * 8 + column] = filtered3(
                    top[index], top[index + 1],
                    top[std::min<std::size_t>(index + 2, 15)]);
            }
        }
    } else if (mode == 4) {
        if (!top_available || !left_available || !top_left_available) {
            throw std::runtime_error(
                "I8x8 diagonal-down-right mode lacks references");
        }
        for (std::int32_t row = 0; row < 8; ++row) {
            for (std::int32_t column = 0; column < 8; ++column) {
                const auto position = column - row - 1;
                const auto reference_at = [&](std::int32_t index) {
                    if (index == -1) {
                        return top_left;
                    }
                    return index < -1 ? left[static_cast<std::size_t>(-index - 2)]
                                      : top[static_cast<std::size_t>(index)];
                };
                prediction[static_cast<std::size_t>(row * 8 + column)] =
                    filtered3(reference_at(position - 1),
                              reference_at(position),
                              reference_at(position + 1));
            }
        }
    } else if (mode == 5) {
        if (!top_available || !left_available || !top_left_available) {
            throw std::runtime_error(
                "I8x8 vertical-right mode lacks references");
        }
        for (std::int32_t row = 0; row < 8; ++row) {
            for (std::int32_t column = 0; column < 8; ++column) {
                const auto phase = 2 * column - row;
                std::uint8_t value = 0;
                if (phase >= 0 && phase % 2 == 0) {
                    const auto center = phase / 2;
                    value = static_cast<std::uint8_t>(
                        (top_sample(center - 1) + top_sample(center) + 1) >> 1);
                } else if (phase >= 0) {
                    const auto center = (phase - 1) / 2;
                    value = filtered3(top_sample(center - 1),
                                      top_sample(center),
                                      top_sample(center + 1));
                } else if (phase == -1) {
                    value = filtered3(left[0], top_left, top[0]);
                } else {
                    const auto center = -phase - 2;
                    value = filtered3(
                        center == 0 ? top_left : left[center - 1],
                        left[center], left[center + 1]);
                }
                prediction[static_cast<std::size_t>(row * 8 + column)] = value;
            }
        }
    } else if (mode == 6) {
        if (!top_available || !left_available || !top_left_available) {
            throw std::runtime_error(
                "I8x8 horizontal-down mode lacks references");
        }
        const auto vertical_value = [&](std::int32_t row,
                                        std::int32_t column) {
            const auto phase = 2 * column - row;
            if (phase >= 0 && phase % 2 == 0) {
                const auto center = phase / 2;
                return static_cast<std::uint8_t>(
                    (left_sample(center - 1) + left_sample(center) + 1) >> 1);
            }
            if (phase >= 0) {
                const auto center = (phase - 1) / 2;
                return filtered3(left_sample(center - 1), left_sample(center),
                                 left_sample(center + 1));
            }
            if (phase == -1) {
                return filtered3(top[0], top_left, left[0]);
            }
            const auto center = -phase - 2;
            return filtered3(center == 0 ? top_left : top[center - 1],
                             top[center], top[center + 1]);
        };
        for (std::int32_t row = 0; row < 8; ++row) {
            for (std::int32_t column = 0; column < 8; ++column) {
                prediction[static_cast<std::size_t>(row * 8 + column)] =
                    vertical_value(column, row);
            }
        }
    } else if (mode == 7 || mode == 8) {
        if ((mode == 7 && !top_available) ||
            (mode == 8 && !left_available)) {
            throw std::runtime_error(
                "I8x8 directional mode lacks edge references");
        }
        if (mode == 7) {
            for (std::size_t row = 0; row < 8; ++row) {
                for (std::size_t column = 0; column < 8; ++column) {
                    const auto phase = 2 * column + row;
                    const auto center = phase / 2;
                    prediction[row * 8 + column] = phase % 2 == 0
                        ? static_cast<std::uint8_t>(
                              (top[center] + top[center + 1] + 1) >> 1)
                        : filtered3(top[center], top[center + 1],
                                    top[center + 2]);
                }
            }
        } else {
            for (std::size_t row = 0; row < 8; ++row) {
                for (std::size_t column = 0; column < 8; ++column) {
                    const auto phase = 2 * column + row;
                    const auto center = phase / 2;
                    const auto value = phase % 2 == 0
                        ? static_cast<std::uint8_t>(
                              (left[center] + left[center + 1] + 1) >> 1)
                        : filtered3(left[center], left[center + 1],
                                    left[center + 2]);
                    prediction[row * 8 + column] = value;
                }
            }
            for (std::size_t row = 0; row < 8; ++row) {
                for (std::size_t column = row + 1; column < 8; ++column) {
                    std::swap(prediction[row * 8 + column],
                              prediction[column * 8 + row]);
                }
            }
        }
    } else if (mode == 9) {
        if (!top_available || !left_available) {
            throw std::runtime_error("I8x8 plane mode lacks edge references");
        }
        std::int32_t horizontal_gradient = 0;
        std::int32_t vertical_gradient = 0;
        for (std::int32_t index = 1; index <= 4; ++index) {
            horizontal_gradient +=
                index * (static_cast<std::int32_t>(top[4 + index]) -
                         static_cast<std::int32_t>(top[4 - index]));
            vertical_gradient +=
                index * (static_cast<std::int32_t>(left[4 + index]) -
                         static_cast<std::int32_t>(left[4 - index]));
        }
        const auto a = 16 * (static_cast<std::int32_t>(top[8]) + left[8]);
        const auto b = (17 * horizontal_gradient + 16) >> 5;
        const auto c = (17 * vertical_gradient + 16) >> 5;
        for (std::int32_t row = 0; row < 8; ++row) {
            for (std::int32_t column = 0; column < 8; ++column) {
                const auto value =
                    (a + b * (column - 3) + c * (row - 3) + 16) >> 5;
                prediction[static_cast<std::size_t>(row * 8 + column)] =
                    static_cast<std::uint8_t>(std::clamp(value, 0, 255));
            }
        }
    } else {
        throw std::runtime_error("I8x8 prediction mode exceeds 9");
    }
    return prediction;
}

void predict_chroma8x8(std::vector<std::uint8_t>& plane,
                       std::uint32_t plane_width,
                       std::uint32_t macroblock_x,
                       std::uint32_t macroblock_y, std::uint8_t mode) {
    const auto x = macroblock_x * 8U;
    const auto y = macroblock_y * 8U;
    const bool top_available = macroblock_y != 0;
    const bool left_available = macroblock_x != 0;
    std::array<std::uint8_t, 8> top{};
    std::array<std::uint8_t, 8> left{};
    if (top_available) {
        std::copy_n(plane.begin() +
                        static_cast<std::ptrdiff_t>((y - 1) * plane_width + x),
                    8, top.begin());
    }
    if (left_available) {
        for (std::uint32_t row = 0; row < 8; ++row) {
            left[row] =
                plane[static_cast<std::size_t>(y + row) * plane_width + x - 1];
        }
    }

    std::array<std::uint8_t, 64> prediction{};
    if (mode == 0) {
        const auto average = [](const std::uint8_t* samples) {
            return static_cast<std::uint8_t>(
                (static_cast<std::uint32_t>(samples[0]) + samples[1] +
                 samples[2] + samples[3] + 2) >>
                2);
        };
        for (std::uint32_t block_row = 0; block_row < 2; ++block_row) {
            for (std::uint32_t block_column = 0; block_column < 2;
                 ++block_column) {
                std::uint32_t dc = 128;
                if (block_row == 0 && block_column == 1) {
                    if (top_available) {
                        dc = average(top.data() + 4);
                    } else if (left_available) {
                        dc = average(left.data());
                    }
                } else if (block_row == 1 && block_column == 0) {
                    if (left_available) {
                        dc = average(left.data() + 4);
                    } else if (top_available) {
                        dc = average(top.data());
                    }
                } else if (top_available && left_available) {
                    const auto top_offset = block_column * 4;
                    const auto left_offset = block_row * 4;
                    dc = (static_cast<std::uint32_t>(top[top_offset]) +
                          top[top_offset + 1] + top[top_offset + 2] +
                          top[top_offset + 3] + left[left_offset] +
                          left[left_offset + 1] + left[left_offset + 2] +
                          left[left_offset + 3] + 4) >>
                         3;
                } else if (top_available) {
                    dc = average(top.data() + block_column * 4);
                } else if (left_available) {
                    dc = average(left.data() + block_row * 4);
                }
                for (std::uint32_t row = block_row * 4;
                     row < block_row * 4 + 4; ++row) {
                    std::fill_n(prediction.begin() +
                                    static_cast<std::ptrdiff_t>(row * 8 +
                                                                block_column * 4),
                                4, static_cast<std::uint8_t>(dc));
                }
            }
        }
    } else if (mode == 1) {
        if (!left_available) {
            throw std::runtime_error(
                "chroma horizontal mode has no left reference samples");
        }
        for (std::uint32_t row = 0; row < 8; ++row) {
            std::fill_n(prediction.begin() +
                            static_cast<std::ptrdiff_t>(row * 8),
                        8, left[row]);
        }
    } else if (mode == 2) {
        if (!top_available) {
            throw std::runtime_error(
                "chroma vertical mode has no top reference samples");
        }
        for (std::uint32_t row = 0; row < 8; ++row) {
            std::copy(top.begin(), top.end(),
                      prediction.begin() + static_cast<std::ptrdiff_t>(row * 8));
        }
    } else if (mode == 3) {
        if (!top_available || !left_available) {
            throw std::runtime_error(
                "chroma plane mode requires top and left references");
        }
        const auto top_left =
            plane[static_cast<std::size_t>(y - 1) * plane_width + x - 1];
        std::int32_t horizontal_gradient = 0;
        std::int32_t vertical_gradient = 0;
        for (std::int32_t index = 0; index < 4; ++index) {
            const auto weight = index + 1;
            const auto top_reference =
                index == 3 ? top_left : top[2 - index];
            const auto left_reference =
                index == 3 ? top_left : left[2 - index];
            horizontal_gradient +=
                weight * (static_cast<std::int32_t>(top[4 + index]) -
                          top_reference);
            vertical_gradient +=
                weight * (static_cast<std::int32_t>(left[4 + index]) -
                          left_reference);
        }
        const auto a = 16 * (static_cast<std::int32_t>(top[7]) + left[7]);
        const auto b = (34 * horizontal_gradient + 32) >> 6;
        const auto c = (34 * vertical_gradient + 32) >> 6;
        for (std::int32_t row = 0; row < 8; ++row) {
            for (std::int32_t column = 0; column < 8; ++column) {
                const auto value =
                    (a + b * (column - 3) + c * (row - 3) + 16) >> 5;
                prediction[static_cast<std::size_t>(row * 8 + column)] =
                    static_cast<std::uint8_t>(
                        std::clamp(value, 0, 255));
            }
        }
    } else {
        throw std::runtime_error("chroma intra prediction mode exceeds 3");
    }
    for (std::uint32_t row = 0; row < 8; ++row) {
        std::copy_n(prediction.begin() +
                        static_cast<std::ptrdiff_t>(row * 8),
                    8, plane.begin() +
                           static_cast<std::ptrdiff_t>((y + row) * plane_width + x));
    }
}

} // namespace

namespace {

Yuv420Frame decode_idr_i_frame_impl(
    const std::vector<std::vector<std::uint8_t>>& slice_nals,
    const SpsInfo& sps, const PpsInfo& pps, bool pcm_only) {
    if (slice_nals.empty()) {
        throw std::invalid_argument("IDR picture contains no slice NAL units");
    }
    if (pps.num_slice_groups_minus1 != 0) {
        throw std::runtime_error("I_PCM decoding does not support slice groups");
    }
    if (!sps.frame_mbs_only || sps.chroma_format_idc != 1 ||
        sps.bit_depth_luma != 8 || sps.bit_depth_chroma != 8 ||
        sps.coded_width == 0 || sps.coded_height == 0 ||
        sps.coded_width % 16 != 0 || sps.coded_height % 16 != 0 ||
        sps.width == 0 || sps.height == 0 || sps.width % 2 != 0 ||
        sps.height % 2 != 0 ||
        sps.frame_crop_left % 2 != 0 || sps.frame_crop_top % 2 != 0 ||
        static_cast<std::uint64_t>(sps.frame_crop_left) + sps.width +
                sps.frame_crop_right !=
            sps.coded_width ||
        static_cast<std::uint64_t>(sps.frame_crop_top) + sps.height +
                sps.frame_crop_bottom !=
            sps.coded_height) {
        throw std::runtime_error(
            "I_PCM decoding requires a valid progressive 8-bit 4:2:0 SPS");
    }

    const auto macroblock_width = sps.coded_width / 16;
    const auto macroblock_height = sps.coded_height / 16;
    const auto macroblock_count =
        checked_plane_size(macroblock_width, macroblock_height);
    Yuv420Frame frame;
    frame.width = sps.width;
    frame.height = sps.height;
    frame.luma.resize(checked_plane_size(frame.width, frame.height));
    frame.chroma_u.resize(
        checked_plane_size(frame.width / 2, frame.height / 2));
    frame.chroma_v.resize(frame.chroma_u.size());
    std::vector<bool> decoded_macroblocks(macroblock_count, false);
    std::vector<bool> macroblock_intra16_or_pcm(macroblock_count, false);
    std::vector<bool> macroblock_pcm(macroblock_count, false);
    std::vector<bool> macroblock_transform_8x8(macroblock_count, false);
    std::vector<bool> macroblock_chroma_dc_coded_cb(macroblock_count, false);
    std::vector<bool> macroblock_chroma_dc_coded_cr(macroblock_count, false);
    std::vector<bool> macroblock_luma_dc_coded(macroblock_count, false);
    std::vector<std::uint8_t> macroblock_chroma_mode(macroblock_count, 0);
    std::vector<std::uint8_t> macroblock_luma_cbp(macroblock_count, 0);
    std::vector<std::uint8_t> macroblock_chroma_cbp(macroblock_count, 0);
    const auto luma_block_width = static_cast<std::size_t>(macroblock_width) * 4;
    std::vector<std::uint8_t> luma_total_coefficients(
        checked_plane_size(static_cast<std::uint32_t>(luma_block_width),
                           macroblock_height * 4),
        0);
    std::vector<std::uint8_t> luma_coded_block_flags(
        luma_total_coefficients.size(), 0);
    std::vector<std::uint8_t> intra4x4_modes(luma_total_coefficients.size(), 2);
    std::vector<std::uint8_t> intra4x4_mode_valid(
        luma_total_coefficients.size(), 0);
    std::vector<std::uint8_t> intra4x4_reconstructed(
        luma_total_coefficients.size(), 0);
    const auto chroma_block_width = macroblock_width * 2;
    const auto chroma_block_height = macroblock_height * 2;
    std::array<std::vector<std::uint8_t>, 2> chroma_total_coefficients{{
        std::vector<std::uint8_t>(
            checked_plane_size(chroma_block_width, chroma_block_height), 0),
        std::vector<std::uint8_t>(
            checked_plane_size(chroma_block_width, chroma_block_height), 0),
    }};
    const auto no_slice = std::numeric_limits<std::size_t>::max();
    std::vector<std::size_t> macroblock_slice_ids(macroblock_count, no_slice);
    std::vector<std::int32_t> macroblock_qpy(macroblock_count, 0);
    std::vector<std::uint8_t> macroblock_disable_idc(macroblock_count, 1);
    std::vector<std::int64_t> macroblock_alpha_offsets(macroblock_count, 0);
    std::vector<std::int64_t> macroblock_beta_offsets(macroblock_count, 0);

    bool have_picture_header = false;
    IdrSliceHeader picture_header;
    std::size_t decoded_count = 0;
    std::size_t slice_id = 0;
    for (const auto& nal : slice_nals) {
        const auto header = parse_nal_header(nal);
        if (header.unit_type != 5 || header.reference_idc == 0) {
            throw std::runtime_error(
                "I_PCM frame input must contain reference IDR slice NAL units");
        }
        const auto slice_header = parse_idr_i_slice_header(nal, sps, pps);
        if (!have_picture_header) {
            picture_header = slice_header;
            have_picture_header = true;
        } else {
            require_same_picture(picture_header, slice_header);
        }
        if (slice_header.first_macroblock_in_slice >= macroblock_count) {
            throw std::runtime_error(
                "first_mb_in_slice exceeds picture macroblock count");
        }

        const std::vector<std::uint8_t> ebsp(nal.begin() + 1, nal.end());
        const auto rbsp = ebsp_to_rbsp(ebsp);
        BitReader reader(rbsp);
        reader.restore_bit_offset(slice_header.slice_data_bit_offset);
        auto previous_qpy = static_cast<std::int32_t>(
            26 + pps.pic_init_qp_minus26 + slice_header.slice_qp_delta);
        std::int32_t previous_mb_qp_delta = 0;
        std::optional<CabacArithmeticDecoder> cabac_decoder;
        std::vector<CabacContextModel> cabac_contexts;
        if (pps.entropy_coding_mode) {
            cabac_decoder.emplace(reader);
            cabac_contexts = make_cabac_slice_contexts(
                slice_header.slice_type, 0, previous_qpy);
        }
        auto macroblock_address =
            static_cast<std::size_t>(slice_header.first_macroblock_in_slice);
        std::size_t slice_macroblock_count = 0;
        while (true) {
            if (!cabac_decoder && !reader.more_rbsp_data()) {
                break;
            }
            if (macroblock_address >= macroblock_count) {
                throw std::runtime_error(
                    "slice contains more macroblocks than the picture");
            }
            const auto macroblock_x =
                static_cast<std::uint32_t>(macroblock_address % macroblock_width);
            const auto macroblock_y =
                static_cast<std::uint32_t>(macroblock_address / macroblock_width);
            const bool left_available = macroblock_x != 0;
            const bool top_available = macroblock_y != 0;
            const auto left_address =
                left_available ? macroblock_address - 1 : macroblock_address;
            const auto top_address =
                top_available ? macroblock_address - macroblock_width
                              : macroblock_address;
            const auto neighbor_is_intra16_or_pcm =
                [&](std::size_t neighbor_address, bool available) {
                    return available &&
                           decoded_macroblocks[neighbor_address] &&
                           macroblock_slice_ids[neighbor_address] == slice_id &&
                           macroblock_intra16_or_pcm[neighbor_address];
                };
            const auto macroblock_type = cabac_decoder
                ? cabac_decoder->decode_i_intra_mb_type(
                      slice_header.slice_type, cabac_contexts,
                      left_available,
                      neighbor_is_intra16_or_pcm(left_address, left_available),
                      top_available,
                      neighbor_is_intra16_or_pcm(top_address, top_available))
                : reader.read_ue();
            const auto luma_neighbor_coded =
                [&](std::uint32_t neighbor_x, std::uint32_t neighbor_y,
                    bool neighbor_exists) {
                    if (!neighbor_exists) {
                        return true;
                    }
                    const auto neighbor_macroblock =
                        static_cast<std::size_t>(neighbor_y / 4) *
                            macroblock_width +
                        neighbor_x / 4;
                    if (neighbor_macroblock != macroblock_address &&
                        (!decoded_macroblocks[neighbor_macroblock] ||
                         macroblock_slice_ids[neighbor_macroblock] !=
                             slice_id)) {
                        return true;
                    }
                    if (macroblock_pcm[neighbor_macroblock]) {
                        return true;
                    }
                    const auto neighbor_index =
                        static_cast<std::size_t>(neighbor_y) *
                            luma_block_width +
                        neighbor_x;
                    return luma_coded_block_flags[neighbor_index] != 0;
                };
            const bool pcm_macroblock = macroblock_type == 25;
            const bool intra4x4_macroblock = macroblock_type == 0;
            const bool intra16x16 =
                macroblock_type >= 1 && macroblock_type <= 24;
            if (!pcm_macroblock &&
                (!(intra4x4_macroblock || intra16x16) || pcm_only)) {
                throw std::runtime_error(
                    pcm_only
                        ? "I_PCM decoder supports I_PCM macroblocks only"
                        : "IDR I decoder supports I_PCM and I16x16 macroblocks only");
            }
            if (decoded_macroblocks[macroblock_address]) {
                throw std::runtime_error(
                    "IDR slices contain an overlapping macroblock");
            }
            macroblock_slice_ids[macroblock_address] = slice_id;
            macroblock_intra16_or_pcm[macroblock_address] =
                pcm_macroblock || intra16x16;
            macroblock_pcm[macroblock_address] = pcm_macroblock;
            macroblock_luma_cbp[macroblock_address] =
                pcm_macroblock ? 15 : 0;
            macroblock_chroma_cbp[macroblock_address] =
                pcm_macroblock ? 2 : 0;
            if (pcm_macroblock) {
                macroblock_luma_dc_coded[macroblock_address] = true;
                macroblock_chroma_dc_coded_cb[macroblock_address] = true;
                macroblock_chroma_dc_coded_cr[macroblock_address] = true;
            }
            if (pcm_macroblock) {
                while (reader.bit_offset() % 8 != 0) {
                    if (reader.read_bit()) {
                        throw std::runtime_error(
                            "pcm_alignment_zero_bit is not zero");
                    }
                }
                write_luma_macroblock(reader, frame, sps, macroblock_x,
                                      macroblock_y);
                write_chroma_macroblock(reader, frame.chroma_u, frame, sps,
                                        macroblock_x, macroblock_y);
                write_chroma_macroblock(reader, frame.chroma_v, frame, sps,
                                        macroblock_x, macroblock_y);
                if (cabac_decoder) {
                    cabac_decoder->reset_after_ipcm();
                }
                for (std::uint32_t block_index = 0; block_index < 16;
                     ++block_index) {
                    const auto block_position =
                        luma_block_position(block_index);
                    const auto block_x = macroblock_x * 4 + block_position[0];
                    const auto block_y = macroblock_y * 4 + block_position[1];
                    const auto block_grid_index =
                        static_cast<std::size_t>(block_y) * luma_block_width +
                        block_x;
                    luma_total_coefficients[block_grid_index] = 16;
                    luma_coded_block_flags[block_grid_index] = 1;
                    intra4x4_modes[block_grid_index] = 2;
                    intra4x4_mode_valid[block_grid_index] = true;
                    intra4x4_reconstructed[block_grid_index] = true;
                }
                for (auto& plane_coefficients : chroma_total_coefficients) {
                    for (std::uint32_t block_index = 0; block_index < 4;
                         ++block_index) {
                        const auto block_x =
                            macroblock_x * 2 + block_index % 2;
                        const auto block_y =
                            macroblock_y * 2 + block_index / 2;
                        plane_coefficients[
                            static_cast<std::size_t>(block_y) *
                                chroma_block_width +
                            block_x] = 16;
                    }
                }
            } else if (intra4x4_macroblock) {
                if (sps.frame_crop_left != 0 || sps.frame_crop_top != 0 ||
                    sps.frame_crop_right != 0 || sps.frame_crop_bottom != 0) {
                    throw std::runtime_error(
                        "I4x4 prediction currently requires uncropped pictures");
                }
                bool transform_8x8 = false;
                if (pps.transform_8x8_mode) {
                    const bool left_transform =
                        left_available &&
                        decoded_macroblocks[left_address] &&
                        macroblock_slice_ids[left_address] == slice_id &&
                        macroblock_transform_8x8[left_address];
                    const bool top_transform =
                        top_available &&
                        decoded_macroblocks[top_address] &&
                        macroblock_slice_ids[top_address] == slice_id &&
                        macroblock_transform_8x8[top_address];
                    const auto transform_context =
                        399 + static_cast<std::size_t>(left_transform) +
                        static_cast<std::size_t>(top_transform);
                    transform_8x8 = cabac_decoder
                        ? cabac_decoder->decode_bin(
                              cabac_contexts[transform_context])
                        : reader.read_bit();
                    macroblock_transform_8x8[macroblock_address] =
                        transform_8x8;
                }
                if (transform_8x8 && !cabac_decoder) {
                    throw std::runtime_error(
                        "CAVLC Intra_8x8 residual decoding is not supported");
                }
                std::array<std::uint8_t, 16> modes{};
                std::array<std::uint8_t, 4> modes_8x8{};
                if (transform_8x8) {
                    for (std::uint32_t block_index = 0; block_index < 4;
                         ++block_index) {
                        const auto block_x = macroblock_x * 4 +
                                             (block_index % 2) * 2;
                        const auto block_y = macroblock_y * 4 +
                                             (block_index / 2) * 2;
                        const auto neighbor_mode =
                            [&](std::uint32_t neighbor_x,
                                std::uint32_t neighbor_y,
                                bool neighbor_exists)
                                -> std::optional<std::uint8_t> {
                            if (!neighbor_exists) {
                                return std::nullopt;
                            }
                            const auto neighbor_index =
                                static_cast<std::size_t>(neighbor_y) *
                                    luma_block_width +
                                neighbor_x;
                            const auto neighbor_macroblock =
                                static_cast<std::size_t>(neighbor_y / 4) *
                                    macroblock_width +
                                neighbor_x / 4;
                            const bool available =
                                neighbor_macroblock == macroblock_address
                                    ? intra4x4_mode_valid[neighbor_index] != 0
                                    : decoded_macroblocks[neighbor_macroblock] &&
                                          macroblock_slice_ids[neighbor_macroblock] ==
                                              slice_id;
                            if (!available) {
                                return std::nullopt;
                            }
                            return intra4x4_modes[neighbor_index];
                        };
                        const auto left_mode = neighbor_mode(
                            block_x == 0 ? 0 : block_x - 1, block_y,
                            block_x != 0);
                        const auto top_mode = neighbor_mode(
                            block_x, block_y == 0 ? 0 : block_y - 1,
                            block_y != 0);
                        const auto predicted_mode =
                            left_mode && top_mode
                                ? std::min(*left_mode, *top_mode)
                                : std::uint8_t{2};
                        const auto mode =
                            cabac_decoder->decode_intra4x4_pred_mode(
                                predicted_mode, cabac_contexts);
                        modes_8x8[block_index] = mode;
                        for (std::uint32_t sub_y = 0; sub_y < 2; ++sub_y) {
                            for (std::uint32_t sub_x = 0; sub_x < 2; ++sub_x) {
                                const auto grid_index =
                                    static_cast<std::size_t>(block_y + sub_y) *
                                        luma_block_width +
                                    block_x + sub_x;
                                intra4x4_modes[grid_index] = mode;
                                intra4x4_mode_valid[grid_index] = true;
                            }
                        }
                    }
                } else {
                    for (std::uint32_t block_index = 0; block_index < 16;
                         ++block_index) {
                        const auto position = luma_block_position(block_index);
                        const auto block_x = macroblock_x * 4 + position[0];
                        const auto block_y = macroblock_y * 4 + position[1];
                        const auto block_grid_index =
                            static_cast<std::size_t>(block_y) * luma_block_width +
                            block_x;
                        const auto neighbor_mode =
                            [&](std::uint32_t neighbor_x,
                                std::uint32_t neighbor_y,
                                bool neighbor_exists)
                                -> std::optional<std::uint8_t> {
                            if (!neighbor_exists) {
                                return std::nullopt;
                            }
                            const auto neighbor_index =
                                static_cast<std::size_t>(neighbor_y) *
                                    luma_block_width +
                                neighbor_x;
                            const auto neighbor_macroblock =
                                static_cast<std::size_t>(neighbor_y / 4) *
                                    macroblock_width +
                                neighbor_x / 4;
                            const bool available =
                                neighbor_macroblock == macroblock_address
                                    ? intra4x4_mode_valid[neighbor_index] != 0
                                    : decoded_macroblocks[neighbor_macroblock] &&
                                          macroblock_slice_ids[neighbor_macroblock] ==
                                              slice_id;
                            if (!available) {
                                return std::nullopt;
                            }
                            return intra4x4_modes[neighbor_index];
                        };
                        const auto left_mode = neighbor_mode(
                            block_x == 0 ? 0 : block_x - 1, block_y,
                            block_x != 0);
                        const auto top_mode = neighbor_mode(
                            block_x, block_y == 0 ? 0 : block_y - 1,
                            block_y != 0);
                        const auto predicted_mode =
                            left_mode && top_mode
                                ? std::min(*left_mode, *top_mode)
                                : std::uint8_t{2};
                        std::uint8_t mode = predicted_mode;
                        if (cabac_decoder) {
                            mode = cabac_decoder->decode_intra4x4_pred_mode(
                                predicted_mode, cabac_contexts);
                        } else {
                            const auto previous_mode_flag = reader.read_bit();
                            if (!previous_mode_flag) {
                                const auto remainder = reader.read_bits(3);
                                mode = static_cast<std::uint8_t>(
                                    remainder < predicted_mode ? remainder
                                                               : remainder + 1);
                            }
                        }
                        modes[block_index] = mode;
                        intra4x4_modes[block_grid_index] = mode;
                        intra4x4_mode_valid[block_grid_index] = true;
                    }
                }
                const bool chroma_left_nonzero =
                    left_available && decoded_macroblocks[left_address] &&
                    macroblock_slice_ids[left_address] == slice_id &&
                    macroblock_chroma_mode[left_address] != 0;
                const bool chroma_top_nonzero =
                    top_available && decoded_macroblocks[top_address] &&
                    macroblock_slice_ids[top_address] == slice_id &&
                    macroblock_chroma_mode[top_address] != 0;
                const auto chroma_mode = cabac_decoder
                    ? cabac_decoder->decode_intra_chroma_pred_mode(
                          chroma_left_nonzero, chroma_top_nonzero,
                          cabac_contexts)
                    : reader.read_ue();
                if (chroma_mode > 3) {
                    throw std::runtime_error(
                        "intra_chroma_pred_mode exceeds 3");
                }
                static constexpr std::array<std::uint8_t, 48> intra_cbp{{
                    47, 31, 15, 0, 23, 27, 29, 30, 7, 11, 13, 14,
                    39, 43, 45, 46, 16, 3, 5, 10, 12, 19, 21, 26,
                    28, 35, 37, 42, 44, 1, 2, 4, 8, 17, 18, 20,
                    24, 6, 9, 22, 25, 32, 33, 34, 36, 40, 38, 41,
                }};
                std::uint8_t luma_cbp = 0;
                std::uint8_t chroma_cbp = 0;
                if (cabac_decoder) {
                    const auto left_luma_cbp =
                        left_available && decoded_macroblocks[left_address] &&
                                macroblock_slice_ids[left_address] == slice_id
                            ? macroblock_luma_cbp[left_address]
                            : 15;
                    const auto top_luma_cbp =
                        top_available && decoded_macroblocks[top_address] &&
                                macroblock_slice_ids[top_address] == slice_id
                            ? macroblock_luma_cbp[top_address]
                            : 15;
                    const auto left_chroma_cbp =
                        left_available && decoded_macroblocks[left_address] &&
                                macroblock_slice_ids[left_address] == slice_id
                            ? macroblock_chroma_cbp[left_address]
                            : 0;
                    const auto top_chroma_cbp =
                        top_available && decoded_macroblocks[top_address] &&
                                macroblock_slice_ids[top_address] == slice_id
                            ? macroblock_chroma_cbp[top_address]
                            : 0;
                    luma_cbp = cabac_decoder->decode_luma_coded_block_pattern(
                        left_luma_cbp, top_luma_cbp, cabac_contexts);
                    chroma_cbp =
                        cabac_decoder->decode_chroma_coded_block_pattern(
                            left_chroma_cbp, top_chroma_cbp, cabac_contexts);
                } else {
                    const auto cbp_code = reader.read_ue();
                    if (cbp_code >= intra_cbp.size()) {
                        throw std::runtime_error(
                            "I4x4 coded_block_pattern code exceeds 47");
                    }
                    const auto coded_block_pattern = intra_cbp[cbp_code];
                    luma_cbp = coded_block_pattern & 0x0f;
                    chroma_cbp = (coded_block_pattern >> 4) & 0x03;
                }
                const auto coded_block_pattern =
                    static_cast<std::uint8_t>(luma_cbp | (chroma_cbp << 4));
                macroblock_chroma_mode[macroblock_address] =
                    static_cast<std::uint8_t>(chroma_mode);
                macroblock_luma_cbp[macroblock_address] = luma_cbp;
                macroblock_chroma_cbp[macroblock_address] = chroma_cbp;
                std::array<CavlcResidualBlock, 16> luma_blocks;
                std::array<std::array<std::int32_t, 64>, 4>
                    luma8x8_levels{};
                std::array<CavlcResidualBlock, 2> chroma_dc_blocks{};
                std::array<std::array<CavlcResidualBlock, 4>, 2>
                    chroma_ac_blocks;
                for (auto& block : luma_blocks) {
                    block.coefficients.resize(16, 0);
                }
                for (auto& plane_blocks : chroma_ac_blocks) {
                    for (auto& block : plane_blocks) {
                        block.coefficients.resize(15, 0);
                    }
                }
                if (coded_block_pattern != 0) {
                    const auto macroblock_qp_delta = cabac_decoder
                        ? cabac_decoder->decode_mb_qp_delta(
                              previous_mb_qp_delta, cabac_contexts)
                        : reader.read_se();
                    if (macroblock_qp_delta < -26 ||
                        macroblock_qp_delta > 25) {
                        throw std::runtime_error(
                            "mb_qp_delta is outside the 8-bit H.264 range");
                    }
                    previous_qpy = static_cast<std::int32_t>(
                        (previous_qpy + macroblock_qp_delta + 52) % 52);
                    previous_mb_qp_delta =
                        static_cast<std::int32_t>(macroblock_qp_delta);
                } else {
                    previous_mb_qp_delta = 0;
                }

                const auto luma_neighbor_count =
                    [&](std::uint32_t neighbor_x,
                        std::uint32_t neighbor_y,
                        bool neighbor_exists) -> std::optional<std::uint8_t> {
                    if (!neighbor_exists) {
                        return std::nullopt;
                    }
                    const auto neighbor_macroblock =
                        static_cast<std::size_t>(neighbor_y / 4) *
                            macroblock_width +
                        neighbor_x / 4;
                    if (neighbor_macroblock != macroblock_address &&
                        (!decoded_macroblocks[neighbor_macroblock] ||
                         macroblock_slice_ids[neighbor_macroblock] !=
                             slice_id)) {
                        return std::nullopt;
                    }
                    return luma_total_coefficients[
                        static_cast<std::size_t>(neighbor_y) *
                            luma_block_width +
                        neighbor_x];
                };
                bool any_luma_coefficients = false;
                if (transform_8x8) {
                    static constexpr std::array<std::size_t, 64> scan{{
                        0, 1, 8, 16, 9, 2, 3, 10, 17, 24, 32, 25, 18, 11, 4,
                        5, 12, 19, 26, 33, 40, 48, 41, 34, 27, 20, 13, 6, 7,
                        14, 21, 28, 35, 42, 49, 56, 57, 50, 43, 36, 29, 22,
                        15, 23, 30, 37, 44, 51, 58, 59, 52, 45, 38, 31, 39,
                        46, 53, 60, 61, 54, 47, 55, 62, 63,
                    }};
                    for (std::uint32_t block_index = 0; block_index < 4;
                         ++block_index) {
                        if ((luma_cbp & (1U << block_index)) == 0) {
                            continue;
                        }
                        const auto scan_levels =
                            cabac_decoder->decode_luma8x8_residual_block(
                                cabac_contexts);
                        for (std::size_t scan_index = 0;
                             scan_index < scan.size(); ++scan_index) {
                            luma8x8_levels[block_index][scan[scan_index]] =
                                scan_levels[scan_index];
                        }
                        for (std::uint32_t sub_y = 0; sub_y < 2; ++sub_y) {
                            for (std::uint32_t sub_x = 0; sub_x < 2; ++sub_x) {
                                std::uint8_t total = 0;
                                for (std::uint32_t row = sub_y * 4;
                                     row < sub_y * 4 + 4; ++row) {
                                    for (std::uint32_t column = sub_x * 4;
                                         column < sub_x * 4 + 4; ++column) {
                                        total +=
                                            luma8x8_levels[block_index]
                                                [row * 8 + column] != 0;
                                    }
                                }
                                const auto grid_x =
                                    macroblock_x * 4 +
                                    (block_index % 2) * 2 + sub_x;
                                const auto grid_y =
                                    macroblock_y * 4 +
                                    (block_index / 2) * 2 + sub_y;
                                luma_total_coefficients[
                                    static_cast<std::size_t>(grid_y) *
                                        luma_block_width +
                                    grid_x] = total;
                                luma_coded_block_flags[
                                    static_cast<std::size_t>(grid_y) *
                                        luma_block_width +
                                    grid_x] = 1;
                                any_luma_coefficients |= total != 0;
                            }
                        }
                    }
                } else {
                    for (std::uint32_t block_index = 0; block_index < 16;
                         ++block_index) {
                        const auto position = luma_block_position(block_index);
                        const auto block_x = macroblock_x * 4 + position[0];
                        const auto block_y = macroblock_y * 4 + position[1];
                        const auto block_grid_index =
                            static_cast<std::size_t>(block_y) * luma_block_width +
                            block_x;
                        if ((luma_cbp & (1U << (block_index / 4))) == 0) {
                            luma_total_coefficients[block_grid_index] = 0;
                            luma_coded_block_flags[block_grid_index] = 0;
                            continue;
                        }
                        const auto left_count = luma_neighbor_count(
                            block_x == 0 ? 0 : block_x - 1, block_y,
                            block_x != 0);
                        const auto top_count = luma_neighbor_count(
                            block_x, block_y == 0 ? 0 : block_y - 1,
                            block_y != 0);
                        const auto nc = left_count && top_count
                            ? (static_cast<std::int32_t>(*left_count) +
                               static_cast<std::int32_t>(*top_count) + 1) /
                                  2
                            : left_count
                                ? static_cast<std::int32_t>(*left_count)
                                : top_count
                                    ? static_cast<std::int32_t>(*top_count)
                                    : 0;
                        auto& block = luma_blocks[block_index];
                        if (cabac_decoder) {
                            const auto decoded =
                                cabac_decoder->decode_residual_block(
                                    2,
                                    luma_neighbor_coded(
                                        block_x == 0 ? 0 : block_x - 1,
                                        block_y, block_x != 0),
                                    luma_neighbor_coded(
                                        block_x, block_y == 0 ? 0 : block_y - 1,
                                        block_y != 0),
                                    cabac_contexts);
                            block.coefficients.assign(decoded.levels.begin(),
                                                      decoded.levels.end());
                            block.total_coeff = static_cast<std::uint8_t>(
                                std::count_if(
                                    decoded.levels.begin(),
                                    decoded.levels.end(),
                                    [](std::int32_t level) {
                                        return level != 0;
                                    }));
                            luma_coded_block_flags[block_grid_index] =
                                decoded.coded;
                        } else {
                            block = decode_cavlc_residual_block(reader, nc, 16);
                            luma_coded_block_flags[block_grid_index] =
                                block.total_coeff != 0;
                        }
                        luma_total_coefficients[block_grid_index] =
                            block.total_coeff;
                        any_luma_coefficients |= block.total_coeff != 0;
                    }
                }

                const auto qpc_cb =
                    chroma_qp(previous_qpy, pps.chroma_qp_index_offset);
                const auto qpc_cr = chroma_qp(
                    previous_qpy, pps.second_chroma_qp_index_offset);
                bool any_chroma_coefficients = false;
                if (chroma_cbp != 0) {
                    for (std::size_t plane_index = 0; plane_index < 2;
                         ++plane_index) {
                        auto& block = chroma_dc_blocks[plane_index];
                        if (cabac_decoder) {
                            const auto& dc_flags =
                                plane_index == 0
                                    ? macroblock_chroma_dc_coded_cb
                                    : macroblock_chroma_dc_coded_cr;
                            const bool left_coded =
                                !left_available ||
                                !decoded_macroblocks[left_address] ||
                                macroblock_slice_ids[left_address] != slice_id
                                    ? true
                                    : dc_flags[left_address];
                            const bool top_coded =
                                !top_available ||
                                !decoded_macroblocks[top_address] ||
                                macroblock_slice_ids[top_address] != slice_id
                                    ? true
                                    : dc_flags[top_address];
                            const auto decoded =
                                cabac_decoder->decode_residual_block(
                                    3, left_coded, top_coded, cabac_contexts);
                            block.coefficients.assign(
                                decoded.levels.begin(),
                                decoded.levels.begin() + 4);
                            block.total_coeff = static_cast<std::uint8_t>(
                                std::count_if(
                                    block.coefficients.begin(),
                                    block.coefficients.end(),
                                    [](std::int32_t level) {
                                        return level != 0;
                                    }));
                            if (plane_index == 0) {
                                macroblock_chroma_dc_coded_cb[macroblock_address] =
                                    decoded.coded;
                            } else {
                                macroblock_chroma_dc_coded_cr[macroblock_address] =
                                    decoded.coded;
                            }
                        } else {
                            block = decode_cavlc_residual_block(reader, -1, 4);
                        }
                        any_chroma_coefficients |= block.total_coeff != 0;
                    }
                }
                if (chroma_cbp == 2) {
                    for (std::size_t plane_index = 0; plane_index < 2;
                         ++plane_index) {
                        for (std::uint32_t block_index = 0; block_index < 4;
                             ++block_index) {
                            const auto block_x =
                                macroblock_x * 2 + block_index % 2;
                            const auto block_y =
                                macroblock_y * 2 + block_index / 2;
                            const auto block_grid_index =
                                static_cast<std::size_t>(block_y) *
                                    chroma_block_width +
                                block_x;
                            const auto neighbor_count =
                                [&](std::uint32_t neighbor_x,
                                    std::uint32_t neighbor_y,
                                    bool neighbor_exists)
                                    -> std::optional<std::uint8_t> {
                                if (!neighbor_exists) {
                                    return std::nullopt;
                                }
                                const auto neighbor_macroblock =
                                    static_cast<std::size_t>(neighbor_y / 2) *
                                        macroblock_width +
                                    neighbor_x / 2;
                                if (neighbor_macroblock != macroblock_address &&
                                    (!decoded_macroblocks[neighbor_macroblock] ||
                                     macroblock_slice_ids[neighbor_macroblock] !=
                                         slice_id)) {
                                    return std::nullopt;
                                }
                                return chroma_total_coefficients[plane_index][
                                    static_cast<std::size_t>(neighbor_y) *
                                        chroma_block_width +
                                    neighbor_x];
                            };
                            const auto left_count = neighbor_count(
                                block_x == 0 ? 0 : block_x - 1, block_y,
                                block_x != 0);
                            const auto top_count = neighbor_count(
                                block_x, block_y == 0 ? 0 : block_y - 1,
                                block_y != 0);
                            const auto nc = left_count && top_count
                                ? (static_cast<std::int32_t>(*left_count) +
                                   static_cast<std::int32_t>(*top_count) + 1) /
                                      2
                                : left_count
                                    ? static_cast<std::int32_t>(*left_count)
                                    : top_count
                                        ? static_cast<std::int32_t>(*top_count)
                                        : 0;
                            auto& block =
                                chroma_ac_blocks[plane_index][block_index];
                            if (cabac_decoder) {
                                const auto decoded =
                                    cabac_decoder->decode_residual_block(
                                        4, !left_count || *left_count != 0,
                                        !top_count || *top_count != 0,
                                        cabac_contexts);
                                block.coefficients.assign(
                                    decoded.levels.begin(),
                                    decoded.levels.begin() + 15);
                                block.total_coeff =
                                    static_cast<std::uint8_t>(std::count_if(
                                        block.coefficients.begin(),
                                        block.coefficients.end(),
                                        [](std::int32_t level) {
                                            return level != 0;
                                        }));
                            } else {
                                block = decode_cavlc_residual_block(reader, nc, 15);
                            }
                            chroma_total_coefficients[plane_index]
                                                     [block_grid_index] =
                                block.total_coeff;
                            any_chroma_coefficients |= block.total_coeff != 0;
                        }
                    }
                }
                if (any_luma_coefficients &&
                    ((transform_8x8 &&
                      sps.seq_intra_luma_8x8_scaling_list_present) ||
                     (!transform_8x8 &&
                      sps.seq_intra_luma_4x4_scaling_list_present) ||
                     pps.pic_scaling_matrix_present)) {
                    throw std::runtime_error(
                        "intra luma residuals with custom scaling matrices are not supported");
                }
                if (any_chroma_coefficients &&
                    (sps.seq_intra_chroma_4x4_scaling_list_present ||
                     pps.pic_scaling_matrix_present)) {
                    throw std::runtime_error(
                        "I4x4 chroma residuals with custom scaling matrices are not supported");
                }
                std::array<std::array<std::int64_t, 16>, 16>
                    luma_residual_blocks{};
                std::array<std::array<std::int64_t, 64>, 4>
                    luma8x8_residual_blocks{};
                if (transform_8x8) {
                    for (std::uint32_t block_index = 0; block_index < 4;
                         ++block_index) {
                        const auto scaled = inverse_scale_luma8x8(
                            luma8x8_levels[block_index], previous_qpy);
                        luma8x8_residual_blocks[block_index] =
                            inverse_transform_luma8x8(scaled);
                    }
                } else {
                    for (std::uint32_t block_index = 0; block_index < 16;
                         ++block_index) {
                        luma_residual_blocks[block_index] =
                            inverse_transform_luma4x4(
                                inverse_scale_intra4x4(luma_blocks[block_index],
                                                       previous_qpy));
                    }
                }
                std::array<std::array<std::int64_t, 4>, 2>
                    chroma_dc_residuals{};
                if (chroma_cbp != 0) {
                    chroma_dc_residuals[0] =
                        reconstruct_intra16x16_chroma_dc(chroma_dc_blocks[0],
                                                        qpc_cb);
                    chroma_dc_residuals[1] =
                        reconstruct_intra16x16_chroma_dc(chroma_dc_blocks[1],
                                                        qpc_cr);
                }
                std::array<std::array<std::array<std::int64_t, 16>, 4>, 2>
                    chroma_residual_blocks{};
                for (std::size_t plane_index = 0; plane_index < 2;
                     ++plane_index) {
                    const auto& dc = chroma_dc_residuals[plane_index];
                    for (std::uint32_t block_index = 0; block_index < 4;
                         ++block_index) {
                        const auto qp = plane_index == 0 ? qpc_cb : qpc_cr;
                        chroma_residual_blocks[plane_index][block_index] =
                            inverse_transform_luma4x4(
                                inverse_scale_intra4x4_ac(
                                    chroma_ac_blocks[plane_index][block_index],
                                    qp, dc[block_index]));
                    }
                }

                const auto sample_available =
                    [&](std::uint32_t sample_x,
                        std::uint32_t sample_y) -> bool {
                    if (sample_x >= sps.coded_width ||
                        sample_y >= sps.coded_height) {
                        return false;
                    }
                    const auto block_x = sample_x / 4;
                    const auto block_y = sample_y / 4;
                    const auto block_grid_index =
                        static_cast<std::size_t>(block_y) * luma_block_width +
                        block_x;
                    const auto neighbor_macroblock =
                        static_cast<std::size_t>(block_y / 4) *
                            macroblock_width +
                        block_x / 4;
                    if (neighbor_macroblock == macroblock_address) {
                        return intra4x4_reconstructed[block_grid_index];
                    }
                    return decoded_macroblocks[neighbor_macroblock] &&
                           macroblock_slice_ids[neighbor_macroblock] == slice_id;
                };
                if (transform_8x8) {
                    for (std::uint32_t block_index = 0; block_index < 4;
                         ++block_index) {
                        const auto origin_x =
                            macroblock_x * 16 + (block_index % 2) * 8;
                        const auto origin_y =
                            macroblock_y * 16 + (block_index / 2) * 8;
                        std::array<std::uint8_t, 16> top{};
                        std::array<std::uint8_t, 16> left{};
                        bool top_available = true;
                        bool left_available = true;
                        for (std::uint32_t index = 0; index < 16; ++index) {
                            if (origin_y != 0 &&
                                sample_available(origin_x + index,
                                                 origin_y - 1)) {
                                top[index] = frame.luma[
                                    static_cast<std::size_t>(origin_y - 1) *
                                        frame.width +
                                    origin_x + index];
                            } else if (index < 8) {
                                top_available = false;
                            } else {
                                top[index] = index == 8 ? top[7] : top[index - 1];
                            }
                        }
                        for (std::uint32_t index = 0; index < 8; ++index) {
                            if (origin_x != 0 &&
                                sample_available(origin_x - 1,
                                                 origin_y + index)) {
                                left[index] = frame.luma[
                                    static_cast<std::size_t>(origin_y + index) *
                                        frame.width +
                                    origin_x - 1];
                            } else {
                                left_available = false;
                            }
                        }
                        bool top_left_available = false;
                        std::uint8_t top_left = 128;
                        if (origin_x != 0 && origin_y != 0 &&
                            sample_available(origin_x - 1, origin_y - 1)) {
                            top_left_available = true;
                            top_left = frame.luma[
                                static_cast<std::size_t>(origin_y - 1) *
                                    frame.width +
                                origin_x - 1];
                        }
                        const auto prediction = predict_intra8x8(
                            modes_8x8[block_index], top, top_available, left,
                            left_available, top_left, top_left_available);
                        for (std::uint32_t row = 0; row < 8; ++row) {
                            for (std::uint32_t column = 0; column < 8;
                                 ++column) {
                                const auto residual_index = row * 8 + column;
                                auto& sample = frame.luma[
                                    static_cast<std::size_t>(origin_y + row) *
                                        frame.width +
                                    origin_x + column];
                                sample = static_cast<std::uint8_t>(
                                    std::clamp<std::int64_t>(
                                        static_cast<std::int64_t>(
                                            prediction[residual_index]) +
                                            luma8x8_residual_blocks[block_index]
                                                [residual_index],
                                        0, 255));
                            }
                        }
                        for (std::uint32_t sub_y = 0; sub_y < 2; ++sub_y) {
                            for (std::uint32_t sub_x = 0; sub_x < 2; ++sub_x) {
                                const auto grid_index =
                                    static_cast<std::size_t>(
                                        origin_y / 4 + sub_y) *
                                        luma_block_width +
                                    origin_x / 4 + sub_x;
                                intra4x4_reconstructed[grid_index] = true;
                            }
                        }
                    }
                } else for (std::uint32_t block_index = 0; block_index < 16;
                     ++block_index) {
                    const auto position = luma_block_position(block_index);
                    const auto origin_x = macroblock_x * 16 + position[0] * 4;
                    const auto origin_y = macroblock_y * 16 + position[1] * 4;
                    std::array<std::uint8_t, 8> top{};
                    std::array<std::uint8_t, 4> left{};
                    bool top_available = true;
                    bool left_available = true;
                    for (std::uint32_t index = 0; index < 8; ++index) {
                        if (origin_y == 0 ||
                            !sample_available(origin_x + index, origin_y - 1)) {
                            top_available &= index >= 4;
                            continue;
                        }
                        top[index] = frame.luma[
                            static_cast<std::size_t>(origin_y - 1) * frame.width +
                            origin_x + index];
                    }
                    for (std::uint32_t index = 0; index < 4; ++index) {
                        if (origin_x == 0 ||
                            !sample_available(origin_x - 1, origin_y + index)) {
                            left_available = false;
                            continue;
                        }
                        left[index] = frame.luma[
                            static_cast<std::size_t>(origin_y + index) *
                                frame.width +
                            origin_x - 1];
                    }
                    for (std::uint32_t index = 4; index < 8; ++index) {
                        if (origin_y == 0 ||
                            !sample_available(origin_x + index, origin_y - 1)) {
                            top[index] = top[3];
                        }
                    }
                    if (modes[block_index] == 1 && !left_available) {
                        throw std::runtime_error(
                            "I4x4 horizontal mode has no left reference samples "
                            "(macroblock " +
                            std::to_string(macroblock_address) + ", block " +
                            std::to_string(block_index) + ", position " +
                            std::to_string(position[0]) + "," +
                            std::to_string(position[1]) + ")");
                    }
                    if (modes[block_index] == 7 && !top_available) {
                        throw std::runtime_error(
                            "I4x4 vertical-left mode has no top references "
                            "(macroblock " +
                            std::to_string(macroblock_address) + ", block " +
                            std::to_string(block_index) + ", position " +
                            std::to_string(position[0]) + "," +
                            std::to_string(position[1]) + ")");
                    }
                    bool top_left_available = false;
                    std::uint8_t top_left = 128;
                    if (origin_x > 0 && origin_y > 0 &&
                        sample_available(origin_x - 1, origin_y - 1)) {
                        top_left_available = true;
                        top_left = frame.luma[
                            static_cast<std::size_t>(origin_y - 1) *
                                frame.width +
                            origin_x - 1];
                    }
                    const auto prediction = predict_intra4x4(
                        modes[block_index], top, top_available, left,
                        left_available, top_left, top_left_available);
                    for (std::uint32_t row = 0; row < 4; ++row) {
                        for (std::uint32_t column = 0; column < 4; ++column) {
                            const auto residual_index = row * 4 + column;
                            auto& sample = frame.luma[
                                static_cast<std::size_t>(origin_y + row) *
                                    frame.width +
                                origin_x + column];
                            sample = static_cast<std::uint8_t>(
                                std::clamp<std::int64_t>(
                                    static_cast<std::int64_t>(
                                        prediction[residual_index]) +
                                        luma_residual_blocks[block_index]
                                                            [residual_index],
                                    0, 255));
                        }
                    }
                    const auto block_grid_index =
                        static_cast<std::size_t>(origin_y / 4) *
                            luma_block_width +
                        origin_x / 4;
                    intra4x4_mode_valid[block_grid_index] = true;
                    intra4x4_reconstructed[block_grid_index] = true;
                }
                predict_chroma8x8(frame.chroma_u, frame.width / 2,
                                  macroblock_x, macroblock_y,
                                  static_cast<std::uint8_t>(chroma_mode));
                predict_chroma8x8(frame.chroma_v, frame.width / 2,
                                  macroblock_x, macroblock_y,
                                  static_cast<std::uint8_t>(chroma_mode));
                add_intra16x16_chroma_residual(
                    frame.chroma_u, frame.width / 2, macroblock_x, macroblock_y,
                    chroma_residual_blocks[0]);
                add_intra16x16_chroma_residual(
                    frame.chroma_v, frame.width / 2, macroblock_x, macroblock_y,
                    chroma_residual_blocks[1]);
            } else {
                if (sps.frame_crop_left != 0 || sps.frame_crop_top != 0 ||
                    sps.frame_crop_right != 0 || sps.frame_crop_bottom != 0) {
                    throw std::runtime_error(
                        "I16x16 prediction currently requires uncropped pictures");
                }
                const auto luma_mode =
                    static_cast<std::uint8_t>((macroblock_type - 1) % 4);
                const bool luma_ac_coded = macroblock_type >= 13;
                const auto chroma_cbp =
                    static_cast<std::uint8_t>(((macroblock_type - 1) / 4) % 3);
                const bool chroma_left_nonzero =
                    left_available && decoded_macroblocks[left_address] &&
                    macroblock_slice_ids[left_address] == slice_id &&
                    macroblock_chroma_mode[left_address] != 0;
                const bool chroma_top_nonzero =
                    top_available && decoded_macroblocks[top_address] &&
                    macroblock_slice_ids[top_address] == slice_id &&
                    macroblock_chroma_mode[top_address] != 0;
                const auto chroma_mode = cabac_decoder
                    ? cabac_decoder->decode_intra_chroma_pred_mode(
                          chroma_left_nonzero, chroma_top_nonzero,
                          cabac_contexts)
                    : reader.read_ue();
                if (chroma_mode > 3) {
                    throw std::runtime_error(
                        "intra_chroma_pred_mode exceeds 3");
                }
                const auto luma_cbp =
                    static_cast<std::uint8_t>(luma_ac_coded ? 15 : 0);
                macroblock_luma_cbp[macroblock_address] = luma_cbp;
                macroblock_chroma_cbp[macroblock_address] = chroma_cbp;
                macroblock_chroma_mode[macroblock_address] =
                    static_cast<std::uint8_t>(chroma_mode);
                const auto macroblock_qp_delta = cabac_decoder
                    ? cabac_decoder->decode_mb_qp_delta(
                          previous_mb_qp_delta, cabac_contexts)
                    : reader.read_se();
                if (macroblock_qp_delta < -26 || macroblock_qp_delta > 25) {
                    throw std::runtime_error(
                        "mb_qp_delta is outside the 8-bit H.264 range");
                }
                previous_mb_qp_delta = macroblock_qp_delta;
                previous_qpy = static_cast<std::int32_t>(
                    (previous_qpy + macroblock_qp_delta + 52) % 52);
                CavlcResidualBlock luma_dc;
                if (cabac_decoder) {
                    const auto dc_condition = [&](std::size_t neighbor_address,
                                                  bool available) -> bool {
                        if (!available ||
                            !decoded_macroblocks[neighbor_address] ||
                            macroblock_slice_ids[neighbor_address] != slice_id) {
                            return true;
                        }
                        if (!macroblock_intra16_or_pcm[neighbor_address]) {
                            return false;
                        }
                        return macroblock_luma_dc_coded[neighbor_address];
                    };
                    const auto decoded = cabac_decoder->decode_residual_block(
                        0, dc_condition(left_address, left_available),
                        dc_condition(top_address, top_available),
                        cabac_contexts);
                    luma_dc.coefficients.assign(
                        decoded.levels.begin(), decoded.levels.end());
                    luma_dc.total_coeff = static_cast<std::uint8_t>(
                        std::count_if(
                            decoded.levels.begin(), decoded.levels.end(),
                            [](std::int32_t level) { return level != 0; }));
                    macroblock_luma_dc_coded[macroblock_address] =
                        decoded.coded;
                } else {
                    luma_dc = decode_cavlc_residual_block(reader, 0, 16);
                    macroblock_luma_dc_coded[macroblock_address] =
                        luma_dc.total_coeff != 0;
                }
                if (luma_dc.coefficients.size() != 16) {
                    throw std::runtime_error(
                        "I16x16 luma DC residual block has an invalid size");
                }
                std::array<std::int64_t, 16> luma_dc_residuals{};
                if (luma_dc.total_coeff != 0) {
                    if (sps.seq_intra_luma_4x4_scaling_list_present ||
                        pps.pic_scaling_matrix_present) {
                        throw std::runtime_error(
                            "I16x16 luma DC residuals with custom scaling matrices are not supported");
                    }
                    luma_dc_residuals =
                        reconstruct_intra16x16_luma_dc(luma_dc, previous_qpy);
                }
                const std::array<std::int32_t, 2> qpc{{
                    chroma_qp(previous_qpy, pps.chroma_qp_index_offset),
                    chroma_qp(previous_qpy, pps.second_chroma_qp_index_offset),
                }};
                std::array<std::array<std::int64_t, 4>, 2>
                    chroma_dc_residuals{};
                std::array<bool, 2> chroma_dc_nonzero{};
                std::array<CavlcResidualBlock, 16> luma_ac_blocks;
                for (auto& block : luma_ac_blocks) {
                    block.coefficients.resize(15, 0);
                }
                bool any_luma_ac_coefficients = false;
                if (luma_ac_coded) {
                    for (std::uint32_t block_index = 0; block_index < 16;
                         ++block_index) {
                        const auto block_position =
                            luma_block_position(block_index);
                        const auto block_x = macroblock_x * 4 + block_position[0];
                        const auto block_y = macroblock_y * 4 + block_position[1];
                        const auto block_grid_index =
                            static_cast<std::size_t>(block_y) * luma_block_width +
                            block_x;
                        const auto neighbor_count =
                            [&](std::uint32_t neighbor_x,
                                std::uint32_t neighbor_y,
                                bool neighbor_exists) -> std::optional<std::uint8_t> {
                            if (!neighbor_exists) {
                                return std::nullopt;
                            }
                            const auto neighbor_macroblock =
                                static_cast<std::size_t>(neighbor_y / 4) *
                                    macroblock_width +
                                neighbor_x / 4;
                            if (neighbor_macroblock != macroblock_address &&
                                (!decoded_macroblocks[neighbor_macroblock] ||
                                 macroblock_slice_ids[neighbor_macroblock] !=
                                     slice_id)) {
                                return std::nullopt;
                            }
                            return luma_total_coefficients[
                                static_cast<std::size_t>(neighbor_y) *
                                    luma_block_width +
                                neighbor_x];
                        };
                        const auto left_count = neighbor_count(
                            block_x == 0 ? 0 : block_x - 1, block_y,
                            block_x != 0);
                        const auto top_count = neighbor_count(
                            block_x, block_y == 0 ? 0 : block_y - 1,
                            block_y != 0);
                        const auto nc = left_count && top_count
                            ? (static_cast<std::int32_t>(*left_count) +
                               static_cast<std::int32_t>(*top_count) + 1) /
                                  2
                            : left_count
                                ? static_cast<std::int32_t>(*left_count)
                                : top_count
                                    ? static_cast<std::int32_t>(*top_count)
                                    : 0;
                        auto& block = luma_ac_blocks[block_index];
                        bool luma_block_coded = false;
                        if (cabac_decoder) {
                            const auto ac_condition =
                                [&](std::uint32_t neighbor_x,
                                    std::uint32_t neighbor_y,
                                    bool neighbor_exists) {
                                if (!neighbor_exists) {
                                    return true;
                                }
                                const auto neighbor_macroblock =
                                    static_cast<std::size_t>(neighbor_y / 4) *
                                        macroblock_width +
                                    neighbor_x / 4;
                                if (neighbor_macroblock != macroblock_address &&
                                    (!decoded_macroblocks[neighbor_macroblock] ||
                                     macroblock_slice_ids[neighbor_macroblock] !=
                                         slice_id)) {
                                    return true;
                                }
                                return luma_neighbor_coded(
                                    neighbor_x, neighbor_y, true);
                            };
                            const auto cond_left = ac_condition(
                                block_x == 0 ? 0 : block_x - 1, block_y,
                                block_x != 0);
                            const auto cond_top = ac_condition(
                                block_x, block_y == 0 ? 0 : block_y - 1,
                                block_y != 0);
                            const auto decoded =
                                cabac_decoder->decode_residual_block(
                                    1, cond_left, cond_top, cabac_contexts);
                            luma_block_coded = decoded.coded;
                            block.coefficients.assign(
                                decoded.levels.begin(),
                                decoded.levels.begin() + 15);
                            block.total_coeff = static_cast<std::uint8_t>(
                                std::count_if(
                                    block.coefficients.begin(),
                                    block.coefficients.end(),
                                    [](std::int32_t level) {
                                        return level != 0;
                                    }));
                        } else {
                            block = decode_cavlc_residual_block(reader, nc, 15);
                            luma_block_coded = block.total_coeff != 0;
                        }
                        if (luma_ac_blocks[block_index].coefficients.size() != 15) {
                            throw std::runtime_error(
                                "I16x16 luma AC residual block has an invalid size");
                        }
                        any_luma_ac_coefficients |=
                            luma_ac_blocks[block_index].total_coeff != 0;
                        luma_total_coefficients[block_grid_index] =
                            luma_ac_blocks[block_index].total_coeff;
                        luma_coded_block_flags[block_grid_index] =
                            luma_block_coded;
                    }
                }
                for (std::size_t plane_index = 0; plane_index < 2;
                     ++plane_index) {
                    if (chroma_cbp == 0) {
                        continue;
                    }
                    CavlcResidualBlock chroma_dc;
                    if (cabac_decoder) {
                        const auto& dc_flags =
                            plane_index == 0
                                ? macroblock_chroma_dc_coded_cb
                                : macroblock_chroma_dc_coded_cr;
                        const auto dc_condition =
                            [&](std::size_t neighbor_address,
                                bool available) -> bool {
                            return !available ||
                                   !decoded_macroblocks[neighbor_address] ||
                                   macroblock_slice_ids[neighbor_address] !=
                                       slice_id
                                       ? true
                                       : dc_flags[neighbor_address];
                        };
                        const auto decoded =
                            cabac_decoder->decode_residual_block(
                                3, dc_condition(left_address, left_available),
                                dc_condition(top_address, top_available),
                                cabac_contexts);
                        chroma_dc.coefficients.assign(
                            decoded.levels.begin(),
                            decoded.levels.begin() + 4);
                        chroma_dc.total_coeff = static_cast<std::uint8_t>(
                            std::count_if(
                                chroma_dc.coefficients.begin(),
                                chroma_dc.coefficients.end(),
                                [](std::int32_t level) { return level != 0; }));
                        if (plane_index == 0) {
                            macroblock_chroma_dc_coded_cb[macroblock_address] =
                                decoded.coded;
                        } else {
                            macroblock_chroma_dc_coded_cr[macroblock_address] =
                                decoded.coded;
                        }
                    } else {
                        chroma_dc =
                            decode_cavlc_residual_block(reader, -1, 4);
                    }
                    if (chroma_dc.coefficients.size() != 4) {
                        throw std::runtime_error(
                            "I16x16 chroma DC residual block has an invalid size");
                    }
                    chroma_dc_nonzero[plane_index] =
                        chroma_dc.total_coeff != 0;
                    if (chroma_dc_nonzero[plane_index]) {
                        chroma_dc_residuals[plane_index] =
                            reconstruct_intra16x16_chroma_dc(
                                chroma_dc, qpc[plane_index]);
                    }
                    for (std::uint32_t block_index = 0; block_index < 16;
                         ++block_index) {
                        const auto position = luma_block_position(block_index);
                        const auto block_x = macroblock_x * 4 + position[0];
                        const auto block_y = macroblock_y * 4 + position[1];
                        const auto block_grid_index =
                            static_cast<std::size_t>(block_y) * luma_block_width +
                            block_x;
                        intra4x4_modes[block_grid_index] = 2;
                        intra4x4_mode_valid[block_grid_index] = true;
                        intra4x4_reconstructed[block_grid_index] = true;
                    }
                }
                std::array<std::array<CavlcResidualBlock, 4>, 2>
                    chroma_ac_blocks;
                bool any_chroma_ac_coefficients = false;
                for (std::size_t plane_index = 0; plane_index < 2;
                     ++plane_index) {
                    for (std::uint32_t block_index = 0; block_index < 4;
                         ++block_index) {
                        chroma_ac_blocks[plane_index][block_index]
                            .coefficients.resize(15, 0);
                    }
                    if (chroma_cbp != 2) {
                        continue;
                    }
                    for (std::uint32_t block_index = 0; block_index < 4;
                         ++block_index) {
                        const auto block_x =
                            macroblock_x * 2 + block_index % 2;
                        const auto block_y =
                            macroblock_y * 2 + block_index / 2;
                        const auto block_grid_index =
                            static_cast<std::size_t>(block_y) *
                                chroma_block_width +
                            block_x;
                        const auto neighbor_count =
                            [&](std::uint32_t neighbor_x,
                                std::uint32_t neighbor_y,
                                bool neighbor_exists) -> std::optional<std::uint8_t> {
                            if (!neighbor_exists) {
                                return std::nullopt;
                            }
                            const auto neighbor_macroblock =
                                static_cast<std::size_t>(neighbor_y / 2) *
                                    macroblock_width +
                                neighbor_x / 2;
                            if (neighbor_macroblock != macroblock_address &&
                                (!decoded_macroblocks[neighbor_macroblock] ||
                                 macroblock_slice_ids[neighbor_macroblock] !=
                                     slice_id)) {
                                return std::nullopt;
                            }
                            return chroma_total_coefficients[plane_index][
                                static_cast<std::size_t>(neighbor_y) *
                                    chroma_block_width +
                                neighbor_x];
                        };
                        const auto left_count = neighbor_count(
                            block_x == 0 ? 0 : block_x - 1, block_y,
                            block_x != 0);
                        const auto top_count = neighbor_count(
                            block_x, block_y == 0 ? 0 : block_y - 1,
                            block_y != 0);
                        const auto nc = left_count && top_count
                            ? (static_cast<std::int32_t>(*left_count) +
                               static_cast<std::int32_t>(*top_count) + 1) /
                                  2
                            : left_count
                                ? static_cast<std::int32_t>(*left_count)
                                : top_count
                                    ? static_cast<std::int32_t>(*top_count)
                                    : 0;
                        auto& block =
                            chroma_ac_blocks[plane_index][block_index];
                        if (cabac_decoder) {
                            const auto decoded =
                                cabac_decoder->decode_residual_block(
                                    4, !left_count || *left_count != 0,
                                    !top_count || *top_count != 0,
                                    cabac_contexts);
                            block.coefficients.assign(
                                decoded.levels.begin(),
                                decoded.levels.begin() + 15);
                            block.total_coeff = static_cast<std::uint8_t>(
                                std::count_if(
                                    block.coefficients.begin(),
                                    block.coefficients.end(),
                                    [](std::int32_t level) {
                                        return level != 0;
                                    }));
                        } else {
                            block =
                                decode_cavlc_residual_block(reader, nc, 15);
                        }
                        if (block.coefficients.size() != 15) {
                            throw std::runtime_error(
                                "I16x16 chroma AC residual block has an invalid size");
                        }
                        any_chroma_ac_coefficients |= block.total_coeff != 0;
                        chroma_total_coefficients[plane_index][block_grid_index] =
                            block.total_coeff;
                    }
                }
                if (luma_dc.total_coeff != 0 || any_luma_ac_coefficients) {
                    if (sps.seq_intra_luma_4x4_scaling_list_present ||
                        pps.pic_scaling_matrix_present) {
                        throw std::runtime_error(
                            "I16x16 luma residuals with custom scaling matrices are not supported");
                    }
                }
                if (chroma_dc_nonzero[0] || chroma_dc_nonzero[1] ||
                    any_chroma_ac_coefficients) {
                    if (sps.seq_intra_chroma_4x4_scaling_list_present ||
                        pps.pic_scaling_matrix_present) {
                        throw std::runtime_error(
                            "I16x16 chroma residuals with custom scaling matrices are not supported");
                    }
                }
                std::array<std::array<std::int64_t, 16>, 16>
                    luma_residual_blocks{};
                for (std::uint32_t block_index = 0; block_index < 16;
                     ++block_index) {
                    const auto block_position =
                        luma_block_position(block_index);
                    const auto dc_block_index =
                        block_position[1] * 4 + block_position[0];
                    const auto coefficients = inverse_scale_intra4x4_ac(
                        luma_ac_blocks[block_index], previous_qpy,
                        luma_dc_residuals[dc_block_index]);
                    luma_residual_blocks[block_index] =
                        inverse_transform_luma4x4(coefficients);
                }
                predict_intra16x16(frame, macroblock_x, macroblock_y,
                                   luma_mode);
                add_intra16x16_luma_residual(
                    frame, macroblock_x, macroblock_y, luma_residual_blocks);
                predict_chroma8x8(frame.chroma_u, frame.width / 2,
                                  macroblock_x, macroblock_y,
                                  static_cast<std::uint8_t>(chroma_mode));
                predict_chroma8x8(frame.chroma_v, frame.width / 2,
                                  macroblock_x, macroblock_y,
                                  static_cast<std::uint8_t>(chroma_mode));
                for (std::size_t plane_index = 0; plane_index < 2;
                     ++plane_index) {
                    std::array<std::array<std::int64_t, 16>, 4>
                        chroma_residual_blocks{};
                    for (std::uint32_t block_index = 0; block_index < 4;
                         ++block_index) {
                        const auto coefficients = inverse_scale_intra4x4_ac(
                            chroma_ac_blocks[plane_index][block_index],
                            qpc[plane_index],
                            chroma_dc_residuals[plane_index][block_index]);
                        chroma_residual_blocks[block_index] =
                            inverse_transform_luma4x4(coefficients);
                    }
                    auto& plane = plane_index == 0 ? frame.chroma_u
                                                   : frame.chroma_v;
                    add_intra16x16_chroma_residual(
                        plane, frame.width / 2, macroblock_x, macroblock_y,
                        chroma_residual_blocks);
                }
            }
            macroblock_qpy[macroblock_address] =
                pcm_macroblock ? 0 : previous_qpy;
            macroblock_disable_idc[macroblock_address] =
                slice_header.disable_deblocking_filter_idc;
            macroblock_alpha_offsets[macroblock_address] =
                slice_header.slice_alpha_c0_offset_div2;
            macroblock_beta_offsets[macroblock_address] =
                slice_header.slice_beta_offset_div2;
            decoded_macroblocks[macroblock_address] = true;
            ++decoded_count;
            ++slice_macroblock_count;
            if (cabac_decoder && cabac_decoder->decode_terminate_bin()) {
                break;
            }
            ++macroblock_address;
        }
        if (slice_macroblock_count == 0) {
            throw std::runtime_error("IDR slice contains no macroblocks");
        }
        ++slice_id;
    }
    if (decoded_count != macroblock_count) {
        throw std::runtime_error(
            "IDR slices do not cover every picture macroblock");
    }
    deblock_idr_intra_frame(
        frame, macroblock_width, macroblock_height, macroblock_qpy,
        macroblock_slice_ids, macroblock_disable_idc,
        macroblock_alpha_offsets, macroblock_beta_offsets, pps);
    return frame;
}

std::int64_t floor_divide(std::int64_t value, std::int64_t divisor) {
    auto quotient = value / divisor;
    if (value % divisor < 0) {
        --quotient;
    }
    return quotient;
}

std::uint8_t clip_luma_sample(std::int64_t value) {
    return static_cast<std::uint8_t>(std::clamp<std::int64_t>(value, 0, 255));
}

std::int64_t saturating_add(std::int64_t value, std::int64_t offset) {
    if (offset > 0 &&
        value > std::numeric_limits<std::int64_t>::max() - offset) {
        return std::numeric_limits<std::int64_t>::max();
    }
    if (offset < 0 &&
        value < std::numeric_limits<std::int64_t>::min() - offset) {
        return std::numeric_limits<std::int64_t>::min();
    }
    return value + offset;
}

std::uint8_t sample_luma_fractional(
    const std::vector<std::uint8_t>& plane, std::uint32_t width,
    std::uint32_t height, std::int64_t x_int, std::int64_t y_int,
    std::uint8_t x_fraction, std::uint8_t y_fraction) {
    if (x_fraction > 3 || y_fraction > 3 || width == 0 || height == 0 ||
        plane.size() != checked_plane_size(width, height)) {
        throw std::invalid_argument("invalid fractional luma sample input");
    }
    const auto sample_at = [&](std::int64_t x_offset,
                               std::int64_t y_offset) {
        const auto x = std::clamp<std::int64_t>(
            saturating_add(x_int, x_offset), 0,
            static_cast<std::int64_t>(width) - 1);
        const auto y = std::clamp<std::int64_t>(
            saturating_add(y_int, y_offset), 0,
            static_cast<std::int64_t>(height) - 1);
        return plane[static_cast<std::size_t>(y) * width +
                     static_cast<std::size_t>(x)];
    };
    const auto filter_six_tap = [](const std::array<std::int64_t, 6>& taps) {
        return taps[0] - 5 * taps[1] + 20 * taps[2] + 20 * taps[3] -
               5 * taps[4] + taps[5];
    };
    const auto horizontal_half = [&](std::int64_t x_offset,
                                     std::int64_t y_offset) {
        std::array<std::int64_t, 6> taps{};
        for (std::size_t index = 0; index < taps.size(); ++index) {
            taps[index] = sample_at(
                x_offset + static_cast<std::int64_t>(index) - 2,
                y_offset);
        }
        return clip_luma_sample((filter_six_tap(taps) + 16) >> 5);
    };
    const auto vertical_half = [&](std::int64_t x_offset,
                                   std::int64_t y_offset) {
        std::array<std::int64_t, 6> taps{};
        for (std::size_t index = 0; index < taps.size(); ++index) {
            taps[index] = sample_at(
                x_offset,
                y_offset + static_cast<std::int64_t>(index) - 2);
        }
        return clip_luma_sample((filter_six_tap(taps) + 16) >> 5);
    };
    const auto average = [](std::uint8_t first, std::uint8_t second) {
        return static_cast<std::uint8_t>(
            (static_cast<std::uint16_t>(first) + second + 1) >> 1);
    };
    if (x_fraction == 0 && y_fraction == 0) {
        return sample_at(0, 0);
    }

    const bool needs_j =
        (x_fraction == 1 && y_fraction == 2) ||
        (x_fraction == 2 && y_fraction > 0) ||
        (x_fraction == 3 && y_fraction == 2);
    std::array<std::int64_t, 6> vertical_intermediate{};
    if (needs_j) {
        for (std::size_t column = 0; column < vertical_intermediate.size();
             ++column) {
            std::array<std::int64_t, 6> taps{};
            for (std::size_t row = 0; row < taps.size(); ++row) {
                taps[row] = sample_at(
                    static_cast<std::int64_t>(column) - 2,
                    static_cast<std::int64_t>(row) - 2);
            }
            vertical_intermediate[column] = filter_six_tap(taps);
        }
    }
    const auto center = sample_at(0, 0);
    const auto right = sample_at(1, 0);
    const auto lower = sample_at(0, 1);
    std::optional<std::uint8_t> b;
    std::optional<std::uint8_t> h;
    std::optional<std::uint8_t> j;
    std::optional<std::uint8_t> m;
    std::optional<std::uint8_t> s;
    if (x_fraction > 0 && y_fraction <= 1) {
        b = horizontal_half(0, 0);
    }
    if (y_fraction > 0 && x_fraction <= 1) {
        h = needs_j
            ? clip_luma_sample((vertical_intermediate[2] + 16) >> 5)
            : vertical_half(0, 0);
    }
    if (needs_j) {
        j = clip_luma_sample(
            (filter_six_tap(vertical_intermediate) + 512) >> 10);
    }
    if (x_fraction == 3 && y_fraction > 0) {
        m = needs_j
            ? clip_luma_sample((vertical_intermediate[3] + 16) >> 5)
            : vertical_half(1, 0);
    }
    if (x_fraction > 0 && y_fraction == 3) {
        s = horizontal_half(0, 1);
    }
    switch (x_fraction * 4 + y_fraction) {
    case 1: return average(center, *h);
    case 2: return *h;
    case 3: return average(lower, *h);
    case 4: return average(center, *b);
    case 5: return average(*b, *h);
    case 6: return average(*h, *j);
    case 7: return average(*h, *s);
    case 8: return *b;
    case 9: return average(*b, *j);
    case 10: return *j;
    case 11: return average(*j, *s);
    case 12: return average(right, *b);
    case 13: return average(*b, *m);
    case 14: return average(*j, *m);
    case 15: return average(*m, *s);
    default:
        throw std::logic_error("unreachable luma fractional-sample position");
    }
}

std::uint8_t sample_chroma_fractional(
    const std::vector<std::uint8_t>& plane, std::uint32_t width,
    std::uint32_t height, std::int64_t x_int, std::int64_t y_int,
    std::uint8_t x_fraction, std::uint8_t y_fraction) {
    if (x_fraction > 7 || y_fraction > 7 || width == 0 || height == 0 ||
        plane.size() != checked_plane_size(width, height)) {
        throw std::invalid_argument("invalid fractional chroma sample input");
    }
    const auto sample_at = [&](std::int64_t x_offset,
                               std::int64_t y_offset) {
        const auto x = std::clamp<std::int64_t>(
            saturating_add(x_int, x_offset), 0,
            static_cast<std::int64_t>(width) - 1);
        const auto y = std::clamp<std::int64_t>(
            saturating_add(y_int, y_offset), 0,
            static_cast<std::int64_t>(height) - 1);
        return static_cast<std::int64_t>(
            plane[static_cast<std::size_t>(y) * width +
                  static_cast<std::size_t>(x)]);
    };
    const auto a = sample_at(0, 0);
    const auto b = sample_at(1, 0);
    const auto c = sample_at(0, 1);
    const auto d = sample_at(1, 1);
    const auto x_weight = static_cast<std::int64_t>(x_fraction);
    const auto y_weight = static_cast<std::int64_t>(y_fraction);
    return static_cast<std::uint8_t>(
        ((8 - x_weight) * (8 - y_weight) * a +
         x_weight * (8 - y_weight) * b +
         (8 - x_weight) * y_weight * c + x_weight * y_weight * d + 32) >>
        6);
}

} // namespace

Yuv420Frame decode_idr_i_pcm_frame(
    const std::vector<std::vector<std::uint8_t>>& slice_nals,
    const SpsInfo& sps, const PpsInfo& pps) {
    return decode_idr_i_frame_impl(slice_nals, sps, pps, true);
}

Yuv420Frame decode_idr_i_frame(
    const std::vector<std::vector<std::uint8_t>>& slice_nals,
    const SpsInfo& sps, const PpsInfo& pps) {
    return decode_idr_i_frame_impl(slice_nals, sps, pps, false);
}

Yuv420Frame decode_p_frame_with_lists(
    const std::vector<std::vector<std::uint8_t>>& slice_nals,
    const SpsInfo& sps, const PpsInfo& pps,
    const std::vector<const Yuv420Frame*>& reference_frames_l0,
    const std::vector<const Yuv420Frame*>& reference_frames_l1) {
    if (slice_nals.empty()) {
        throw std::invalid_argument("P picture contains no slice NAL units");
    }
    if (reference_frames_l0.empty() || reference_frames_l0.front() == nullptr) {
        throw std::invalid_argument(
            "inter picture requires a decoded L0 reference frame");
    }
    const auto& reference_frame = *reference_frames_l0.front();
    const auto* reference_frame_l1 =
        reference_frames_l1.empty() ? nullptr : reference_frames_l1.front();
    if (pps.num_slice_groups_minus1 != 0 || !pps.deblocking_filter_control_present) {
        throw std::runtime_error(
            "P-picture decoding requires no slice groups and explicit deblocking control");
    }
    if (!sps.frame_mbs_only || sps.chroma_format_idc != 1 ||
        sps.bit_depth_luma != 8 || sps.bit_depth_chroma != 8 ||
        sps.coded_width == 0 || sps.coded_height == 0 ||
        sps.coded_width % 16 != 0 || sps.coded_height % 16 != 0 ||
        sps.frame_crop_left != 0 || sps.frame_crop_right != 0 ||
        sps.frame_crop_top != 0 || sps.frame_crop_bottom != 0 ||
        sps.width == 0 || sps.height == 0 || sps.width % 2 != 0 ||
        sps.height % 2 != 0 ||
        static_cast<std::uint64_t>(sps.frame_crop_left) + sps.width +
                sps.frame_crop_right !=
            sps.coded_width ||
        static_cast<std::uint64_t>(sps.frame_crop_top) + sps.height +
                sps.frame_crop_bottom !=
            sps.coded_height) {
        throw std::runtime_error(
            "P-picture decoding requires an uncropped progressive 8-bit 4:2:0 SPS");
    }
    if (reference_frame.width != sps.width ||
        reference_frame.height != sps.height ||
        reference_frame.luma.size() !=
            checked_plane_size(sps.width, sps.height) ||
        reference_frame.chroma_u.size() !=
            checked_plane_size(sps.width / 2, sps.height / 2) ||
        reference_frame.chroma_v.size() != reference_frame.chroma_u.size()) {
        throw std::invalid_argument(
            "P-picture reference frame dimensions or planes do not match the SPS");
    }
    for (const auto* reference : reference_frames_l0) {
        if (reference == nullptr || reference->width != sps.width ||
            reference->height != sps.height ||
            reference->luma.size() != checked_plane_size(sps.width, sps.height) ||
            reference->chroma_u.size() !=
                checked_plane_size(sps.width / 2, sps.height / 2) ||
            reference->chroma_v.size() != reference->chroma_u.size()) {
            throw std::invalid_argument(
                "P-picture L0 reference dimensions or planes do not match the SPS");
        }
    }
    for (const auto* reference : reference_frames_l1) {
        if (reference == nullptr || reference->width != sps.width ||
            reference->height != sps.height ||
            reference->luma.size() != checked_plane_size(sps.width, sps.height) ||
            reference->chroma_u.size() !=
                checked_plane_size(sps.width / 2, sps.height / 2) ||
            reference->chroma_v.size() != reference->chroma_u.size()) {
            throw std::invalid_argument(
                "B-picture L1 reference dimensions or planes do not match the SPS");
        }
    }

    const auto macroblock_width = sps.coded_width / 16;
    const auto macroblock_height = sps.coded_height / 16;
    const auto macroblock_count =
        checked_plane_size(macroblock_width, macroblock_height);
    std::vector<std::uint8_t> decoded_macroblocks(macroblock_count, 0);
    std::vector<std::uint8_t> macroblock_skipped(macroblock_count, 0);
    std::vector<std::uint8_t> macroblock_b_direct(macroblock_count, 0);
    std::vector<std::uint8_t> macroblock_luma_cbp(macroblock_count, 0);
    std::vector<std::uint8_t> macroblock_chroma_cbp(macroblock_count, 0);
    std::vector<std::uint8_t> macroblock_transform_8x8(macroblock_count, 0);
    const auto luma_block_width =
        static_cast<std::size_t>(macroblock_width) * 4;
    std::vector<std::uint8_t> luma_total_coefficients(
        checked_plane_size(static_cast<std::uint32_t>(luma_block_width),
                           macroblock_height * 4),
        0);
    std::vector<std::uint8_t> luma_coded_block_flags(
        luma_total_coefficients.size(), 0);
    const auto chroma_block_width = macroblock_width * 2;
    const auto chroma_block_height = macroblock_height * 2;
    std::array<std::vector<std::uint8_t>, 2> chroma_total_coefficients{{
        std::vector<std::uint8_t>(
            checked_plane_size(chroma_block_width, chroma_block_height), 0),
        std::vector<std::uint8_t>(
            checked_plane_size(chroma_block_width, chroma_block_height), 0),
    }};
    std::array<std::vector<std::uint8_t>, 2> macroblock_chroma_dc_coded{{
        std::vector<std::uint8_t>(macroblock_count, 0),
        std::vector<std::uint8_t>(macroblock_count, 0),
    }};
    std::array<std::vector<std::uint8_t>, 2> chroma_coded_block_flags{{
        std::vector<std::uint8_t>(
            checked_plane_size(chroma_block_width, chroma_block_height), 0),
        std::vector<std::uint8_t>(
            checked_plane_size(chroma_block_width, chroma_block_height), 0),
    }};
    std::vector<std::uint8_t> macroblock_chroma_mode(macroblock_count, 0);
    std::vector<std::uint8_t> macroblock_intra16(macroblock_count, 0);
    std::vector<std::uint8_t> macroblock_luma_dc_coded(macroblock_count, 0);
    std::vector<MacroblockMotionVectors> macroblock_motion_vectors(
        macroblock_count);
    std::vector<MacroblockMotionVectors> macroblock_motion_vectors_l1(
        macroblock_count);
    std::vector<std::array<std::array<std::int32_t, 16>, 2>>
        macroblock_reference_indices(macroblock_count);
    std::vector<std::array<std::uint8_t, 16>> macroblock_prediction_flags(
        macroblock_count);
    std::vector<MacroblockMotionVectors> macroblock_motion_vector_differences(
        macroblock_count);
    std::vector<MacroblockMotionVectors> macroblock_motion_vector_differences_l1(
        macroblock_count);
    std::vector<std::array<std::uint8_t, 16>> motion_vector_valid(
        macroblock_count);
    std::vector<std::size_t> macroblock_slice_ids(
        macroblock_count, std::numeric_limits<std::size_t>::max());
    std::vector<std::int32_t> macroblock_qpy(macroblock_count, 0);
    std::vector<std::uint8_t> macroblock_disable_idc(macroblock_count, 1);
    std::vector<std::int64_t> macroblock_alpha_offsets(macroblock_count, 0);
    std::vector<std::int64_t> macroblock_beta_offsets(macroblock_count, 0);
    auto frame = reference_frame;
    const auto derive_luma_nc =
        [&](std::uint32_t block_x, std::uint32_t block_y,
            std::size_t current_macroblock,
            std::size_t current_slice_id) {
        const auto neighbor_count =
            [&](std::uint32_t neighbor_x, std::uint32_t neighbor_y,
                bool exists) -> std::optional<std::uint8_t> {
            if (!exists) {
                return std::nullopt;
            }
            const auto neighbor_macroblock =
                static_cast<std::size_t>(neighbor_y / 4) * macroblock_width +
                neighbor_x / 4;
            if (neighbor_macroblock != current_macroblock &&
                (!decoded_macroblocks[neighbor_macroblock] ||
                 macroblock_slice_ids[neighbor_macroblock] !=
                     current_slice_id)) {
                return std::nullopt;
            }
            return luma_total_coefficients[
                static_cast<std::size_t>(neighbor_y) * luma_block_width +
                neighbor_x];
        };
        const auto left = neighbor_count(
            block_x == 0 ? 0 : block_x - 1, block_y, block_x != 0);
        const auto top = neighbor_count(
            block_x, block_y == 0 ? 0 : block_y - 1, block_y != 0);
        if (left && top) {
            return (static_cast<std::int32_t>(*left) +
                    static_cast<std::int32_t>(*top) + 1) /
                   2;
        }
        return left ? static_cast<std::int32_t>(*left)
                    : top ? static_cast<std::int32_t>(*top) : 0;
    };
    const auto neighbor_motion_vector =
        [&](std::int64_t block_x, std::int64_t block_y,
            std::size_t current_macroblock,
            std::size_t current_slice_id,
            bool list1 = false) -> std::optional<MotionVector> {
        if (block_x < 0 || block_y < 0 ||
            block_x >= static_cast<std::int64_t>(macroblock_width) * 4 ||
            block_y >= static_cast<std::int64_t>(macroblock_height) * 4) {
            return std::nullopt;
        }
        const auto address = static_cast<std::size_t>(block_y / 4) *
                                 macroblock_width +
                             static_cast<std::size_t>(block_x / 4);
        const auto block_index =
            static_cast<std::size_t>(block_y % 4) * 4 +
            static_cast<std::size_t>(block_x % 4);
        if (address == current_macroblock) {
            if (!motion_vector_valid[address][block_index]) {
                return std::nullopt;
            }
        } else if (!decoded_macroblocks[address] ||
                   macroblock_slice_ids[address] != current_slice_id) {
            return std::nullopt;
        }
        return list1 ? macroblock_motion_vectors_l1[address][block_index]
                     : macroblock_motion_vectors[address][block_index];
    };
    const auto neighbor_uses_nonzero_reference =
        [&](std::int64_t block_x, std::int64_t block_y,
            std::size_t current_macroblock, std::size_t current_slice_id,
            std::size_t list_index,
            const std::array<std::array<std::int32_t, 16>, 2>&
                current_reference_indices,
            const std::array<std::uint8_t, 16>& current_prediction_flags) {
        if (block_x < 0 || block_y < 0 ||
            block_x >= static_cast<std::int64_t>(macroblock_width) * 4 ||
            block_y >= static_cast<std::int64_t>(macroblock_height) * 4) {
            return false;
        }
        const auto address = static_cast<std::size_t>(block_y / 4) *
                                 macroblock_width +
                             static_cast<std::size_t>(block_x / 4);
        const auto block_index =
            static_cast<std::size_t>(block_y % 4) * 4 +
            static_cast<std::size_t>(block_x % 4);
        if (address == current_macroblock) {
            if (!motion_vector_valid[address][block_index]) {
                return false;
            }
            return (current_prediction_flags[block_index] &
                    (1U << list_index)) != 0 &&
                   current_reference_indices[list_index][block_index] > 0;
        }
        if (!decoded_macroblocks[address] ||
            macroblock_slice_ids[address] != current_slice_id ||
            macroblock_skipped[address]) {
            return false;
        }
        return (macroblock_prediction_flags[address][block_index] &
                (1U << list_index)) != 0 &&
               macroblock_reference_indices[address][list_index]
                                                 [block_index] > 0;
    };
    const auto neighbor_motion_vector_difference =
        [&](std::int64_t block_x, std::int64_t block_y,
            std::size_t current_macroblock,
            std::size_t current_slice_id,
            const std::array<std::uint8_t, 16>& current_list_motion_valid,
            bool list1 = false) -> std::optional<MotionVector> {
        if (block_x < 0 || block_y < 0 ||
            block_x >= static_cast<std::int64_t>(macroblock_width) * 4 ||
            block_y >= static_cast<std::int64_t>(macroblock_height) * 4) {
            return std::nullopt;
        }
        const auto address = static_cast<std::size_t>(block_y / 4) *
                                 macroblock_width +
                             static_cast<std::size_t>(block_x / 4);
        const auto block_index =
            static_cast<std::size_t>(block_y % 4) * 4 +
            static_cast<std::size_t>(block_x % 4);
        if (address == current_macroblock) {
            if (!current_list_motion_valid[block_index]) {
                return std::nullopt;
            }
        } else if (!decoded_macroblocks[address] ||
                   macroblock_slice_ids[address] != current_slice_id) {
            return std::nullopt;
        }
        return list1
            ? macroblock_motion_vector_differences_l1[address][block_index]
            : macroblock_motion_vector_differences[address][block_index];
    };
    const auto neighboring_motion_vectors =
        [&](std::size_t macroblock_address,
            std::size_t current_slice_id, std::int32_t part_x = 0,
            std::int32_t part_y = 0, std::int32_t part_width = 4,
            bool list1 = false) {
        const auto block_x = static_cast<std::int64_t>(
                                 macroblock_address % macroblock_width) *
                                 4 +
                             part_x;
        const auto block_y = static_cast<std::int64_t>(
                                 macroblock_address / macroblock_width) *
                                 4 +
                             part_y;
        auto candidate_a = neighbor_motion_vector(
            block_x - 1, block_y, macroblock_address, current_slice_id,
            list1);
        auto candidate_b = neighbor_motion_vector(
            block_x, block_y - 1, macroblock_address, current_slice_id,
            list1);
        auto candidate_c = neighbor_motion_vector(
            block_x + part_width, block_y - 1, macroblock_address,
            current_slice_id, list1);
        if (!candidate_c) {
            candidate_c = neighbor_motion_vector(
                block_x - 1, block_y - 1, macroblock_address,
                current_slice_id, list1);
        }
        return std::array<std::optional<MotionVector>, 3>{
            candidate_a, candidate_b, candidate_c};
    };
    const auto median_motion_vector = [](
        std::array<std::optional<MotionVector>, 3> candidates) {
        if (!candidates[1] && !candidates[2] && candidates[0]) {
            candidates[1] = candidates[0];
            candidates[2] = candidates[0];
        }
        const auto available_count = static_cast<unsigned>(
            candidates[0].has_value()) +
                                     static_cast<unsigned>(
                                         candidates[1].has_value()) +
                                     static_cast<unsigned>(
                                         candidates[2].has_value());
        if (available_count == 1) {
            for (const auto& candidate : candidates) {
                if (candidate) {
                    return *candidate;
                }
            }
        }
        MotionVector predictor{};
        for (std::size_t component = 0; component < predictor.size();
             ++component) {
            std::array<std::int64_t, 3> values{};
            for (std::size_t index = 0; index < candidates.size(); ++index) {
                if (candidates[index]) {
                    values[index] = (*candidates[index])[component];
                }
            }
            std::sort(values.begin(), values.end());
            predictor[component] = values[1];
        }
        return predictor;
    };
    const auto derive_motion_vector_predictor =
        [&](std::size_t macroblock_address,
            std::size_t current_slice_id) {
        return median_motion_vector(neighboring_motion_vectors(
            macroblock_address, current_slice_id));
    };
    const auto derive_skip_motion_vector =
        [&](std::size_t macroblock_address,
            std::size_t current_slice_id) {
        const auto candidates = neighboring_motion_vectors(
            macroblock_address, current_slice_id);
        constexpr MotionVector zero_motion{};
        if (!candidates[0] || !candidates[1] ||
            *candidates[0] == zero_motion ||
            *candidates[1] == zero_motion) {
            return zero_motion;
        }
        return derive_motion_vector_predictor(
            macroblock_address, current_slice_id);
    };
    const auto derive_partition_motion_predictor =
        [&](std::size_t macroblock_address, std::size_t current_slice_id,
            std::int32_t part_x, std::int32_t part_y,
            std::int32_t part_width, std::int32_t part_height,
            std::size_t list_index,
            std::int32_t current_reference_index,
            const std::array<std::array<std::int32_t, 16>, 2>&
                current_reference_indices,
            const std::array<std::uint8_t, 16>& current_prediction_flags,
            const std::array<std::uint8_t, 16>&
                current_list_motion_valid,
            const MacroblockMotionVectors& current_motion,
            const MacroblockMotionVectors& current_motion_l1) {
        struct MotionVectorCandidate {
            std::int32_t reference_index;
            MotionVector motion_vector;
        };
        const auto candidate_at =
            [&](std::int64_t block_x,
                std::int64_t block_y)
            -> std::optional<MotionVectorCandidate> {
            if (block_x < 0 || block_y < 0 ||
                block_x >= static_cast<std::int64_t>(macroblock_width) * 4 ||
                block_y >= static_cast<std::int64_t>(macroblock_height) * 4) {
                return std::nullopt;
            }
            const auto address = static_cast<std::size_t>(block_y / 4) *
                                     macroblock_width +
                                 static_cast<std::size_t>(block_x / 4);
            const auto block_index =
                static_cast<std::size_t>(block_y % 4) * 4 +
                static_cast<std::size_t>(block_x % 4);
            const bool current = address == macroblock_address;
            if (current) {
                if (!current_list_motion_valid[block_index]) {
                    return std::nullopt;
                }
            } else if (!decoded_macroblocks[address] ||
                       macroblock_slice_ids[address] != current_slice_id) {
                return std::nullopt;
            }
            const auto flags = current
                ? current_prediction_flags[block_index]
                : macroblock_prediction_flags[address][block_index];
            if ((flags & (1U << list_index)) == 0) {
                return std::nullopt;
            }
            return MotionVectorCandidate{
                current
                    ? current_reference_indices[list_index][block_index]
                    : macroblock_reference_indices[address][list_index]
                                                        [block_index],
                list_index == 0
                    ? (current ? current_motion[block_index]
                               : macroblock_motion_vectors[address][block_index])
                    : (current ? current_motion_l1[block_index]
                               : macroblock_motion_vectors_l1[address]
                                                               [block_index])};
        };
        const auto block_x =
            static_cast<std::int64_t>(macroblock_address % macroblock_width) *
                4 +
            part_x;
        const auto block_y =
            static_cast<std::int64_t>(macroblock_address / macroblock_width) *
                4 +
            part_y;
        auto candidate_a = candidate_at(block_x - 1, block_y);
        auto candidate_b = candidate_at(block_x, block_y - 1);
        auto candidate_c = candidate_at(block_x + part_width, block_y - 1);
        if (!candidate_c) {
            candidate_c = candidate_at(block_x - 1, block_y - 1);
        }
        const std::array<std::optional<MotionVectorCandidate>, 3> candidates{
            candidate_a, candidate_b, candidate_c};
        const auto matches_current_reference =
            [&](const std::optional<MotionVectorCandidate>& candidate) {
            return current_reference_index >= 0 && candidate &&
                   candidate->reference_index == current_reference_index;
        };
        std::optional<std::size_t> preferred;
        if (part_width == 4 && part_height == 2) {
            preferred = part_y == 0 ? 1 : 0;
        } else if (part_width == 2 && part_height == 4) {
            preferred = part_x == 0 ? 0 : 2;
        }
        if (preferred && matches_current_reference(candidates[*preferred])) {
            return candidates[*preferred]->motion_vector;
        }
        if (!candidate_b && !candidate_c) {
            return candidate_a ? candidate_a->motion_vector : MotionVector{};
        }
        std::size_t matching_count = 0;
        MotionVector matching{};
        for (const auto& candidate : candidates) {
            if (matches_current_reference(candidate)) {
                ++matching_count;
                matching = candidate->motion_vector;
            }
        }
        if (matching_count == 1) {
            return matching;
        }
        MotionVector predictor{};
        for (std::size_t component = 0; component < predictor.size();
             ++component) {
            std::array<std::int64_t, 3> values{};
            for (std::size_t index = 0; index < candidates.size(); ++index) {
                if (candidates[index] &&
                    candidates[index]->reference_index >= 0) {
                    values[index] =
                        candidates[index]->motion_vector[component];
                }
            }
            std::sort(values.begin(), values.end());
            predictor[component] = values[1];
        }
        return predictor;
    };
    struct DirectMotionCandidate {
        std::int32_t reference_index;
        MotionVector motion_vector;
    };
    const auto neighbor_direct_motion_candidate =
        [&](std::int64_t block_x, std::int64_t block_y,
            std::size_t current_macroblock, std::size_t current_slice_id,
            std::size_t list_index,
            const std::array<std::array<std::int32_t, 16>, 2>&
                current_reference_indices,
            const std::array<std::uint8_t, 16>& current_prediction_flags,
            const MacroblockMotionVectors& current_motion,
            const MacroblockMotionVectors& current_motion_l1)
        -> std::optional<DirectMotionCandidate> {
        if (block_x < 0 || block_y < 0 ||
            block_x >= static_cast<std::int64_t>(macroblock_width) * 4 ||
            block_y >= static_cast<std::int64_t>(macroblock_height) * 4) {
            return std::nullopt;
        }
        const auto address = static_cast<std::size_t>(block_y / 4) *
                                 macroblock_width +
                             static_cast<std::size_t>(block_x / 4);
        const auto block_index =
            static_cast<std::size_t>(block_y % 4) * 4 +
            static_cast<std::size_t>(block_x % 4);
        if (address == current_macroblock) {
            if (!motion_vector_valid[address][block_index]) {
                return std::nullopt;
            }
            if ((current_prediction_flags[block_index] &
                 (1U << list_index)) == 0) {
                return DirectMotionCandidate{-1, {}};
            }
            return DirectMotionCandidate{
                current_reference_indices[list_index][block_index],
                list_index == 0 ? current_motion[block_index]
                                : current_motion_l1[block_index]};
        }
        if (!decoded_macroblocks[address] ||
            macroblock_slice_ids[address] != current_slice_id) {
            return std::nullopt;
        }
        if ((macroblock_prediction_flags[address][block_index] &
             (1U << list_index)) == 0) {
            return DirectMotionCandidate{-1, {}};
        }
        return DirectMotionCandidate{
            macroblock_reference_indices[address][list_index][block_index],
            list_index == 0
                ? macroblock_motion_vectors[address][block_index]
                : macroblock_motion_vectors_l1[address][block_index]};
    };
    const auto derive_spatial_direct =
        [&](std::size_t macroblock_address, std::size_t current_slice_id,
            std::int32_t partition_x, std::int32_t partition_y,
            std::int32_t partition_width, std::int32_t partition_height,
            std::array<std::array<std::int32_t, 16>, 2>&
                output_reference_indices,
            std::array<std::uint8_t, 16>& output_prediction_flags,
            MacroblockMotionVectors& output_motion,
            MacroblockMotionVectors& output_motion_l1) {
        const std::int32_t inference_step =
            partition_width == 4 && partition_height == 4
                ? 4
                : sps.direct_8x8_inference ? 2 : 1;
        const auto minimum_nonnegative = [](std::int32_t left,
                                            std::int32_t right) {
            if (left >= 0 && right >= 0) {
                return std::min(left, right);
            }
            return std::max(left, right);
        };
        for (std::int32_t group_y = partition_y;
             group_y < partition_y + partition_height;
             group_y += inference_step) {
            for (std::int32_t group_x = partition_x;
                 group_x < partition_x + partition_width;
                 group_x += inference_step) {
                const auto block_x =
                    static_cast<std::int64_t>(macroblock_address %
                                              macroblock_width) *
                        4 +
                    group_x;
                const auto block_y =
                    static_cast<std::int64_t>(macroblock_address /
                                              macroblock_width) *
                        4 +
                    group_y;
                std::array<std::array<std::optional<DirectMotionCandidate>, 4>,
                           2>
                    candidates{};
                for (std::size_t list_index = 0; list_index < 2;
                     ++list_index) {
                    candidates[list_index][0] =
                        neighbor_direct_motion_candidate(
                            block_x - 1, block_y, macroblock_address,
                            current_slice_id, list_index,
                            output_reference_indices,
                            output_prediction_flags, output_motion,
                            output_motion_l1);
                    candidates[list_index][1] =
                        neighbor_direct_motion_candidate(
                            block_x, block_y - 1, macroblock_address,
                            current_slice_id, list_index,
                            output_reference_indices,
                            output_prediction_flags, output_motion,
                            output_motion_l1);
                    candidates[list_index][2] =
                        neighbor_direct_motion_candidate(
                            block_x + inference_step, block_y - 1,
                            macroblock_address, current_slice_id, list_index,
                            output_reference_indices,
                            output_prediction_flags, output_motion,
                            output_motion_l1);
                    if (!candidates[list_index][2]) {
                        candidates[list_index][2] =
                            neighbor_direct_motion_candidate(
                                block_x - 1, block_y - 1,
                                macroblock_address, current_slice_id,
                                list_index, output_reference_indices,
                                output_prediction_flags, output_motion,
                                output_motion_l1);
                    }
                    candidates[list_index][3] =
                        neighbor_direct_motion_candidate(
                            block_x - 1, block_y - 1, macroblock_address,
                            current_slice_id, list_index,
                            output_reference_indices,
                            output_prediction_flags, output_motion,
                            output_motion_l1);
                }
                std::array<std::int32_t, 2> reference_index{};
                for (std::size_t list_index = 0; list_index < 2;
                     ++list_index) {
                    const auto reference_at = [&](std::size_t index) {
                        return candidates[list_index][index]
                                   ? candidates[list_index][index]
                                         ->reference_index
                                   : -1;
                    };
                    reference_index[list_index] = minimum_nonnegative(
                        reference_at(0),
                        minimum_nonnegative(reference_at(1),
                                            reference_at(2)));
                }
                const bool direct_zero =
                    reference_index[0] == -1 && reference_index[1] == -1;
                if (direct_zero) {
                    reference_index = {0, 0};
                }
                bool colocated_zero = false;
                if (!reference_frames_l1.empty() &&
                    !reference_frames_l1.front()->long_term_reference &&
                    macroblock_address <
                        reference_frames_l1.front()
                            ->colocated_motion_field.size()) {
                    const auto colocated_block =
                        static_cast<std::size_t>(group_y) * 4 +
                        static_cast<std::size_t>(group_x);
                    const auto& colocated =
                        reference_frames_l1.front()
                            ->colocated_motion_field[macroblock_address]
                                                    [colocated_block];
                    colocated_zero =
                        colocated.reference_index == 0 &&
                        colocated.motion_vector[0] >= -1 &&
                        colocated.motion_vector[0] <= 1 &&
                        colocated.motion_vector[1] >= -1 &&
                        colocated.motion_vector[1] <= 1;
                }
                std::array<MotionVector, 2> group_motion{};
                for (std::size_t list_index = 0; list_index < 2;
                     ++list_index) {
                    if (reference_index[list_index] < 0) {
                        continue;
                    }
                    const auto& list = list_index == 0
                                           ? reference_frames_l0
                                           : reference_frames_l1;
                    if (static_cast<std::size_t>(
                            reference_index[list_index]) >= list.size()) {
                        throw std::runtime_error(
                            "B_Direct spatial reference index is unavailable");
                    }
                    if (direct_zero ||
                        (reference_index[list_index] == 0 &&
                         colocated_zero)) {
                        continue;
                    }
                    auto match_count = 0;
                    MotionVector matching_motion{};
                    std::array<MotionVector, 3> median_candidates{};
                    for (std::size_t candidate_index = 0;
                         candidate_index < 3; ++candidate_index) {
                        if (!candidates[list_index][candidate_index] ||
                            candidates[list_index][candidate_index]
                                    ->reference_index < 0) {
                            continue;
                        }
                        median_candidates[candidate_index] =
                            candidates[list_index][candidate_index]
                                ->motion_vector;
                        if (candidates[list_index][candidate_index]
                                ->reference_index ==
                            reference_index[list_index]) {
                            ++match_count;
                            matching_motion =
                                candidates[list_index][candidate_index]
                                    ->motion_vector;
                        }
                    }
                    if (match_count == 1) {
                        group_motion[list_index] = matching_motion;
                    } else {
                        for (std::size_t component = 0; component < 2;
                             ++component) {
                            std::array<std::int64_t, 3> values{
                                median_candidates[0][component],
                                median_candidates[1][component],
                                median_candidates[2][component]};
                            std::sort(values.begin(), values.end());
                            group_motion[list_index][component] = values[1];
                        }
                    }
                }
                const auto prediction_mask =
                    static_cast<std::uint8_t>(
                        (reference_index[0] >= 0 ? 1 : 0) |
                        (reference_index[1] >= 0 ? 2 : 0));
                for (std::int32_t block_offset_y = 0;
                     block_offset_y < inference_step; ++block_offset_y) {
                    for (std::int32_t block_offset_x = 0;
                         block_offset_x < inference_step; ++block_offset_x) {
                        const auto block_index = static_cast<std::size_t>(
                            (group_y + block_offset_y) * 4 +
                            group_x + block_offset_x);
                        output_prediction_flags[block_index] =
                            prediction_mask;
                        for (std::size_t list_index = 0; list_index < 2;
                             ++list_index) {
                            output_reference_indices[list_index][block_index] =
                                reference_index[list_index];
                        }
                        output_motion[block_index] = group_motion[0];
                        output_motion_l1[block_index] = group_motion[1];
                        motion_vector_valid[macroblock_address][block_index] =
                            1;
                    }
                }
            }
        }
    };
    const auto reconstruct_macroblock =
        [&](std::size_t macroblock_address,
            const MacroblockMotionVectors& motion_vectors,
            const MacroblockMotionVectors* motion_vectors_l1 = nullptr,
            const std::array<std::uint8_t, 16>* prediction_flags = nullptr,
            const std::array<std::array<std::int32_t, 16>, 2>*
                reference_indices = nullptr) {
        const auto macroblock_x = static_cast<std::uint32_t>(
            macroblock_address % macroblock_width);
        const auto macroblock_y = static_cast<std::uint32_t>(
            macroblock_address / macroblock_width);
        const auto copy_plane_macroblock =
            [&](std::uint8_t plane_index,
                std::vector<std::uint8_t>& destination,
                std::uint32_t plane_width, std::uint32_t plane_height,
                std::uint32_t block_width, std::uint32_t block_height,
                bool chroma,
                std::int64_t motion_unit,
                const auto& sample_fractional) {
            for (std::uint32_t row = 0; row < block_height; ++row) {
                for (std::uint32_t column = 0; column < block_width; ++column) {
                    const auto destination_x = static_cast<std::int64_t>(
                        macroblock_x * block_width + column);
                    const auto destination_y = static_cast<std::int64_t>(
                        macroblock_y * block_height + row);
                    const auto luma_x = chroma ? column * 2 : column;
                    const auto luma_y = chroma ? row * 2 : row;
                    const auto motion_index =
                        static_cast<std::size_t>(luma_y / 4) * 4 +
                        luma_x / 4;
                    const auto sample_with_motion =
                        [&](std::size_t list_index,
                            const MotionVector& motion) {
                        const auto& reference_list =
                            list_index == 0 ? reference_frames_l0
                                            : reference_frames_l1;
                        if (reference_list.empty()) {
                            throw std::runtime_error(
                                "inter prediction requires an unavailable reference list");
                        }
                        const auto reference_index =
                            reference_indices == nullptr
                                ? std::size_t{0}
                                : static_cast<std::size_t>(std::max(
                                      0, (*reference_indices)[list_index]
                                             [motion_index]));
                        if (reference_index >= reference_list.size()) {
                            throw std::runtime_error(
                                "inter prediction reference index exceeds its list");
                        }
                        const auto& reference_frame =
                            *reference_list[reference_index];
                        const auto& reference =
                            plane_index == 0
                                ? reference_frame.luma
                                : plane_index == 1
                                    ? reference_frame.chroma_u
                                    : reference_frame.chroma_v;
                        const auto motion_x_int =
                            floor_divide(motion[0], motion_unit);
                        const auto motion_y_int =
                            floor_divide(motion[1], motion_unit);
                        const auto motion_x_fraction =
                            static_cast<std::uint8_t>(
                                motion[0] - motion_x_int * motion_unit);
                        const auto motion_y_fraction =
                            static_cast<std::uint8_t>(
                                motion[1] - motion_y_int * motion_unit);
                        return sample_fractional(
                            reference, plane_width, plane_height,
                            saturating_add(destination_x, motion_x_int),
                            saturating_add(destination_y, motion_y_int),
                            motion_x_fraction, motion_y_fraction);
                    };
                    const auto flags = prediction_flags == nullptr
                        ? std::uint8_t{1}
                        : (*prediction_flags)[motion_index];
                    const auto sample_l0 =
                        (flags & 1) != 0
                            ? sample_with_motion(
                                  0, motion_vectors[motion_index])
                            : std::uint8_t{0};
                    const auto sample_l1 =
                        (flags & 2) != 0
                            ? sample_with_motion(
                                  1,
                                  motion_vectors_l1 == nullptr
                                      ? MotionVector{}
                                      : (*motion_vectors_l1)[motion_index])
                            : std::uint8_t{0};
                    std::uint8_t prediction = sample_l0;
                    if ((flags & 3) == 2) {
                        prediction = sample_l1;
                    } else if ((flags & 3) == 3) {
                        prediction = static_cast<std::uint8_t>(
                            (static_cast<std::uint32_t>(sample_l0) +
                             sample_l1 + 1) >>
                            1);
                    }
                    destination[static_cast<std::size_t>(destination_y) *
                                    plane_width +
                                static_cast<std::size_t>(destination_x)] =
                        prediction;
                }
            }
        };
        copy_plane_macroblock(
            0, frame.luma, frame.width, frame.height,
            16, 16, false, 4, sample_luma_fractional);
        copy_plane_macroblock(
            1, frame.chroma_u, frame.width / 2,
            frame.height / 2, 8, 8, true, 8,
            sample_chroma_fractional);
        copy_plane_macroblock(
            2, frame.chroma_v, frame.width / 2,
            frame.height / 2, 8, 8, true, 8,
            sample_chroma_fractional);
    };
    PSliceHeader picture_header;
    bool have_picture_header = false;
    std::size_t slice_id = 0;
    for (const auto& nal : slice_nals) {
        const auto nal_header = parse_nal_header(nal);
        if (nal_header.unit_type != 1) {
            throw std::runtime_error(
                "inter-picture input must contain non-IDR slice NAL units");
        }
        const auto slice_header = parse_p_slice_header(nal, sps, pps);
        const bool b_slice = slice_header.slice_type == 1;
        if (b_slice && reference_frame_l1 == nullptr) {
            throw std::runtime_error(
                "B-picture decoding requires both reference lists");
        }
        if (!b_slice && nal_header.reference_idc == 0) {
            throw std::runtime_error(
                "P-picture input must contain reference slice NAL units");
        }
        if (!have_picture_header) {
            picture_header = slice_header;
            have_picture_header = true;
        } else if (slice_header.picture_parameter_set_id !=
                       picture_header.picture_parameter_set_id ||
                   slice_header.frame_num != picture_header.frame_num ||
                   slice_header.pic_order_cnt_lsb !=
                       picture_header.pic_order_cnt_lsb ||
                   slice_header.delta_pic_order_bottom !=
                       picture_header.delta_pic_order_bottom ||
                   slice_header.delta_pic_order_cnt0 !=
                       picture_header.delta_pic_order_cnt0 ||
                   slice_header.delta_pic_order_cnt1 !=
                       picture_header.delta_pic_order_cnt1) {
            throw std::runtime_error(
                "P slices do not belong to the same coded picture");
        }
        if (slice_header.first_macroblock_in_slice >= macroblock_count) {
            throw std::runtime_error(
                "P slice first_mb_in_slice exceeds picture macroblock count");
        }

        const std::vector<std::uint8_t> ebsp(nal.begin() + 1, nal.end());
        const auto rbsp = ebsp_to_rbsp(ebsp);
        BitReader reader(rbsp);
        reader.restore_bit_offset(slice_header.slice_data_bit_offset);
        auto macroblock_address =
            static_cast<std::size_t>(slice_header.first_macroblock_in_slice);
        auto previous_qpy = static_cast<std::int32_t>(
            26 + pps.pic_init_qp_minus26 + slice_header.slice_qp_delta);
        std::int32_t previous_mb_qp_delta = 0;
        std::optional<CabacArithmeticDecoder> cabac_decoder;
        std::vector<CabacContextModel> cabac_contexts;
        if (pps.entropy_coding_mode) {
            cabac_decoder.emplace(reader);
            cabac_contexts = make_cabac_slice_contexts(
                slice_header.slice_type, slice_header.cabac_init_idc,
                previous_qpy);
        }
        const auto decode_skip_macroblock =
            [&](std::size_t address) {
                if (decoded_macroblocks[address] != 0) {
                    throw std::runtime_error(
                        "P slices contain an overlapping macroblock");
                }
                const auto skip_motion =
                    derive_skip_motion_vector(address, slice_id);
                auto macroblock_motion = MacroblockMotionVectors{};
                auto macroblock_motion_l1 = MacroblockMotionVectors{};
                std::array<std::uint8_t, 16> prediction_flags{};
                std::array<std::array<std::int32_t, 16>, 2>
                    reference_indices{};
                if (b_slice) {
                    derive_spatial_direct(
                        address, slice_id, 0, 0, 4, 4, reference_indices,
                        prediction_flags, macroblock_motion,
                        macroblock_motion_l1);
                    reconstruct_macroblock(address, macroblock_motion,
                                           &macroblock_motion_l1,
                                           &prediction_flags,
                                           &reference_indices);
                    macroblock_b_direct[address] = 1;
                } else {
                    macroblock_motion.fill(skip_motion);
                    prediction_flags.fill(1);
                    reconstruct_macroblock(address, macroblock_motion);
                }
                decoded_macroblocks[address] = 1;
                macroblock_skipped[address] = 1;
                macroblock_motion_vectors[address] = macroblock_motion;
                macroblock_motion_vectors_l1[address] =
                    macroblock_motion_l1;
                macroblock_reference_indices[address] = reference_indices;
                macroblock_prediction_flags[address] = prediction_flags;
                motion_vector_valid[address].fill(1);
                macroblock_slice_ids[address] = slice_id;
                macroblock_qpy[address] = previous_qpy;
                macroblock_disable_idc[address] =
                    slice_header.disable_deblocking_filter_idc;
                macroblock_alpha_offsets[address] =
                    slice_header.slice_alpha_c0_offset_div2;
                macroblock_beta_offsets[address] =
                    slice_header.slice_beta_offset_div2;
                previous_mb_qp_delta = 0;
            };
        while (cabac_decoder || reader.more_rbsp_data()) {
            if (cabac_decoder) {
                if (macroblock_address >= macroblock_count) {
                    throw std::runtime_error(
                        "P slice contains more macroblocks than the picture");
                }
                const auto macroblock_x = static_cast<std::uint32_t>(
                    macroblock_address % macroblock_width);
                const auto macroblock_y = static_cast<std::uint32_t>(
                    macroblock_address / macroblock_width);
                const auto left_address =
                    macroblock_x != 0 ? macroblock_address - 1
                                      : macroblock_address;
                const auto top_address =
                    macroblock_y != 0
                        ? macroblock_address - macroblock_width
                        : macroblock_address;
                const bool left_available =
                    macroblock_x != 0 &&
                    decoded_macroblocks[left_address] != 0 &&
                    macroblock_slice_ids[left_address] == slice_id;
                const bool top_available =
                    macroblock_y != 0 &&
                    decoded_macroblocks[top_address] != 0 &&
                    macroblock_slice_ids[top_address] == slice_id;
                const auto left_skipped =
                    left_available && macroblock_skipped[left_address] != 0;
                const auto top_skipped =
                    top_available && macroblock_skipped[top_address] != 0;
                const auto skipped = b_slice
                    ? cabac_decoder->decode_b_skip_flag(
                          left_available, left_skipped, top_available,
                          top_skipped, cabac_contexts)
                    : cabac_decoder->decode_p_skip_flag(
                          0, left_available, left_skipped, top_available,
                          top_skipped,                           cabac_contexts);
                if (skipped) {
                    decode_skip_macroblock(macroblock_address);
                    if (b_slice) {
                        macroblock_b_direct[macroblock_address] = 1;
                    }
                    if (cabac_decoder->decode_terminate_bin()) {
                        break;
                    }
                    ++macroblock_address;
                    continue;
                }
            } else {
                const auto skip_run = reader.read_ue();
                if (skip_run > macroblock_count - macroblock_address) {
                    throw std::runtime_error(
                        "P slice mb_skip_run exceeds picture macroblock count");
                }
                for (std::size_t offset = 0; offset < skip_run; ++offset) {
                    decode_skip_macroblock(macroblock_address++);
                }
                if (!reader.more_rbsp_data()) {
                    break;
                }
            }
            if (macroblock_address >= macroblock_count) {
                throw std::runtime_error(
                    "P slice contains more macroblocks than the picture");
            }
            std::size_t b_mb_type_context_increment = 0;
            if (cabac_decoder && b_slice) {
                const auto context_macroblock_x =
                    macroblock_address % macroblock_width;
                const auto context_macroblock_y =
                    macroblock_address / macroblock_width;
                const bool left_available =
                    context_macroblock_x != 0 &&
                    decoded_macroblocks[macroblock_address - 1] != 0 &&
                    macroblock_slice_ids[macroblock_address - 1] == slice_id;
                const bool top_available =
                    context_macroblock_y != 0 &&
                    decoded_macroblocks[macroblock_address - macroblock_width] !=
                        0 &&
                    macroblock_slice_ids[macroblock_address -
                                         macroblock_width] == slice_id;
                b_mb_type_context_increment =
                    static_cast<std::size_t>(
                        left_available &&
                        macroblock_skipped[macroblock_address - 1] == 0 &&
                        macroblock_b_direct[macroblock_address - 1] == 0) +
                    static_cast<std::size_t>(
                        top_available &&
                        macroblock_skipped[macroblock_address -
                                           macroblock_width] == 0 &&
                        macroblock_b_direct[macroblock_address -
                                            macroblock_width] == 0);
            }
            const auto macroblock_type = cabac_decoder
                ? (b_slice
                       ? cabac_decoder->decode_b_mb_type(
                             b_mb_type_context_increment, cabac_contexts)
                       : cabac_decoder->decode_p_mb_type(0, cabac_contexts))
                : reader.read_ue();
            if (b_slice && macroblock_type > 22) {
                throw std::runtime_error(
                    "B macroblock type is not yet supported "
                    "(macroblock " +
                    std::to_string(macroblock_address) + ", type " +
                    std::to_string(macroblock_type) + ")");
            }
            if (b_slice) {
                macroblock_b_direct[macroblock_address] =
                    macroblock_type == 0;
            }
            if (macroblock_type > 30) {
                throw std::runtime_error(
                    "P decoder currently supports P_L0_16x16, P_L0_16x8, "
                    "P_L0_8x16, P_8x8, P_8x8ref0, Intra16x16, and P_SKIP only "
                    "(macroblock " +
                    std::to_string(macroblock_address) + ", type " +
                    std::to_string(macroblock_type) + ")");
            }
            std::array<std::uint8_t, 4> sub_macroblock_types{};
            if ((!b_slice && (macroblock_type == 3 || macroblock_type == 4)) ||
                (b_slice && macroblock_type == 22)) {
                for (auto& sub_macroblock_type : sub_macroblock_types) {
                    const auto decoded_type = cabac_decoder
                        ? (b_slice
                               ? cabac_decoder->decode_b_sub_mb_type(
                                     cabac_contexts)
                               : cabac_decoder->decode_p_sub_mb_type(
                                     cabac_contexts))
                        : reader.read_ue();
                    if (decoded_type > (b_slice ? 12U : 3U)) {
                        throw std::runtime_error(
                            "inter sub_mb_type exceeds the slice-type limit");
                    }
                    sub_macroblock_type =
                        static_cast<std::uint8_t>(decoded_type);
                }
            }
            auto& decoded = decoded_macroblocks[macroblock_address];
            if (decoded != 0) {
                throw std::runtime_error(
                    "P slices contain an overlapping macroblock");
            }
            const auto macroblock_x = static_cast<std::uint32_t>(
                macroblock_address % macroblock_width);
            const auto macroblock_y = static_cast<std::uint32_t>(
                macroblock_address / macroblock_width);
            if (!b_slice && macroblock_type >= 5) {
                if (macroblock_type > 29) {
                    throw std::runtime_error(
                        "P-slice I_PCM macroblocks are unsupported");
                }
                const auto intra_type =
                    static_cast<std::uint32_t>(macroblock_type - 5);
                if (intra_type == 0) {
                    throw std::runtime_error(
                        "P-slice Intra_NxN macroblocks are unsupported");
                }
                const auto intra16_type = intra_type - 1;
                const auto luma_mode =
                    static_cast<std::uint8_t>(intra16_type % 4);
                const auto chroma_cbp = static_cast<std::uint8_t>(
                    (intra16_type / 4) % 3);
                const auto luma_cbp = static_cast<std::uint8_t>(
                    intra16_type / 12 == 0 ? 0 : 15);
                if (chroma_cbp != 0) {
                    throw std::runtime_error(
                        "P-slice Intra16x16 chroma residuals are unsupported");
                }
                const bool left_available = macroblock_x != 0;
                const bool top_available = macroblock_y != 0;
                const auto left_address =
                    left_available ? macroblock_address - 1
                                   : macroblock_address;
                const auto top_address =
                    top_available ? macroblock_address - macroblock_width
                                  : macroblock_address;
                const bool left_chroma_mode =
                    left_available && decoded_macroblocks[left_address] &&
                    macroblock_slice_ids[left_address] == slice_id &&
                    macroblock_chroma_mode[left_address] != 0;
                const bool top_chroma_mode =
                    top_available && decoded_macroblocks[top_address] &&
                    macroblock_slice_ids[top_address] == slice_id &&
                    macroblock_chroma_mode[top_address] != 0;
                const auto chroma_mode = cabac_decoder
                    ? cabac_decoder->decode_intra_chroma_pred_mode(
                          left_chroma_mode, top_chroma_mode, cabac_contexts)
                    : reader.read_ue();
                if (chroma_mode > 3) {
                    throw std::runtime_error(
                        "P-slice intra_chroma_pred_mode exceeds 3");
                }

                macroblock_luma_cbp[macroblock_address] = luma_cbp;
                macroblock_chroma_cbp[macroblock_address] = chroma_cbp;
                macroblock_chroma_mode[macroblock_address] =
                    static_cast<std::uint8_t>(chroma_mode);
                macroblock_intra16[macroblock_address] = 1;
                macroblock_transform_8x8[macroblock_address] = 0;
                auto qpy = previous_qpy;
                if (luma_cbp != 0) {
                    const auto qp_delta = cabac_decoder
                        ? cabac_decoder->decode_mb_qp_delta(
                              previous_mb_qp_delta, cabac_contexts)
                        : reader.read_se();
                    if (qp_delta < -26 || qp_delta > 25) {
                        throw std::runtime_error(
                            "P-slice Intra16x16 mb_qp_delta is outside [-26,25]");
                    }
                    qpy = static_cast<std::int32_t>(
                        (qpy + qp_delta + 52) % 52);
                    previous_mb_qp_delta = qp_delta;
                } else {
                    previous_mb_qp_delta = 0;
                }
                if (qpy < 0 || qpy > 51) {
                    throw std::runtime_error(
                        "P-slice Intra16x16 QP is outside [0,51]");
                }

                CavlcResidualBlock luma_dc;
                if (cabac_decoder) {
                    const auto dc_condition =
                        [&](std::size_t neighbor_address, bool available) {
                        if (!available ||
                            !decoded_macroblocks[neighbor_address] ||
                            macroblock_slice_ids[neighbor_address] != slice_id) {
                            return true;
                        }
                        return macroblock_intra16[neighbor_address] &&
                               macroblock_luma_dc_coded[neighbor_address];
                    };
                    const auto residual = cabac_decoder->decode_residual_block(
                        0, dc_condition(left_address, left_available),
                        dc_condition(top_address, top_available),
                        cabac_contexts);
                    luma_dc.coefficients.assign(
                        residual.levels.begin(), residual.levels.end());
                    luma_dc.total_coeff = static_cast<std::uint8_t>(
                        std::count_if(
                            residual.levels.begin(), residual.levels.end(),
                            [](std::int32_t level) { return level != 0; }));
                    macroblock_luma_dc_coded[macroblock_address] =
                        residual.coded;
                } else {
                    luma_dc = decode_cavlc_residual_block(reader, 0, 16);
                    macroblock_luma_dc_coded[macroblock_address] =
                        luma_dc.total_coeff != 0;
                }
                std::array<std::int64_t, 16> luma_dc_residuals{};
                if (luma_dc.total_coeff != 0) {
                    if (sps.seq_intra_luma_4x4_scaling_list_present ||
                        pps.pic_scaling_matrix_present) {
                        throw std::runtime_error(
                            "P-slice Intra16x16 custom luma scaling matrices are unsupported");
                    }
                    luma_dc_residuals =
                        reconstruct_intra16x16_luma_dc(luma_dc, qpy);
                }

                std::array<CavlcResidualBlock, 16> luma_ac_blocks;
                for (auto& block : luma_ac_blocks) {
                    block.coefficients.resize(15, 0);
                }
                bool any_luma_ac_coefficients = false;
                for (std::uint32_t block_index = 0; block_index < 16;
                     ++block_index) {
                    const auto position = luma_block_position(block_index);
                    const auto block_x = macroblock_x * 4 + position[0];
                    const auto block_y = macroblock_y * 4 + position[1];
                    const auto grid_index =
                        static_cast<std::size_t>(block_y) * luma_block_width +
                        block_x;
                    if (luma_cbp == 0) {
                        luma_total_coefficients[grid_index] = 0;
                        luma_coded_block_flags[grid_index] = 0;
                        continue;
                    }
                    CavlcResidualBlock block;
                    bool block_coded = false;
                    if (cabac_decoder) {
                        const auto neighbor_coded =
                            [&](std::uint32_t neighbor_x,
                                std::uint32_t neighbor_y, bool exists) {
                            if (!exists) {
                                return true;
                            }
                            const auto neighbor_macroblock =
                                static_cast<std::size_t>(neighbor_y / 4) *
                                    macroblock_width +
                                neighbor_x / 4;
                            if (neighbor_macroblock != macroblock_address &&
                                (!decoded_macroblocks[neighbor_macroblock] ||
                                 macroblock_slice_ids[neighbor_macroblock] !=
                                     slice_id)) {
                                return true;
                            }
                            const auto neighbor_group =
                                ((neighbor_y % 4) / 2) * 2 +
                                (neighbor_x % 4) / 2;
                            const auto neighbor_index =
                                static_cast<std::size_t>(neighbor_y) *
                                    luma_block_width +
                                neighbor_x;
                            return (macroblock_luma_cbp[neighbor_macroblock] &
                                    (1U << neighbor_group)) != 0 &&
                                   luma_coded_block_flags[neighbor_index] != 0;
                        };
                        const auto residual =
                            cabac_decoder->decode_residual_block(
                                1,
                                neighbor_coded(
                                    block_x == 0 ? 0 : block_x - 1,
                                    block_y, block_x != 0),
                                neighbor_coded(
                                    block_x,
                                    block_y == 0 ? 0 : block_y - 1,
                                    block_y != 0),
                                cabac_contexts);
                        block.coefficients.assign(
                            residual.levels.begin(),
                            residual.levels.begin() + 15);
                        block.total_coeff = static_cast<std::uint8_t>(
                            std::count_if(
                                block.coefficients.begin(),
                                block.coefficients.end(),
                                [](std::int32_t level) { return level != 0; }));
                        block_coded = residual.coded;
                    } else {
                        block = decode_cavlc_residual_block(
                            reader,
                            derive_luma_nc(block_x, block_y,
                                           macroblock_address, slice_id),
                            15);
                        block_coded = block.total_coeff != 0;
                    }
                    luma_total_coefficients[grid_index] = block.total_coeff;
                    luma_coded_block_flags[grid_index] = block_coded;
                    any_luma_ac_coefficients |= block.total_coeff != 0;
                    luma_ac_blocks[block_index] = std::move(block);
                }
                if (any_luma_ac_coefficients &&
                    (sps.seq_intra_luma_4x4_scaling_list_present ||
                     pps.pic_scaling_matrix_present)) {
                    throw std::runtime_error(
                        "P-slice Intra16x16 custom luma scaling matrices are unsupported");
                }
                std::array<std::array<std::int64_t, 16>, 16>
                    luma_residual_blocks{};
                for (std::uint32_t block_index = 0; block_index < 16;
                     ++block_index) {
                    const auto position = luma_block_position(block_index);
                    const auto dc_index = position[1] * 4 + position[0];
                    const auto coefficients = inverse_scale_intra4x4_ac(
                        luma_ac_blocks[block_index], qpy,
                        luma_dc_residuals[dc_index]);
                    luma_residual_blocks[block_index] =
                        inverse_transform_luma4x4(coefficients);
                }
                predict_intra16x16(frame, macroblock_x, macroblock_y,
                                   luma_mode);
                add_intra16x16_luma_residual(
                    frame, macroblock_x, macroblock_y, luma_residual_blocks);
                predict_chroma8x8(
                    frame.chroma_u, frame.width / 2, macroblock_x,
                    macroblock_y, static_cast<std::uint8_t>(chroma_mode));
                predict_chroma8x8(
                    frame.chroma_v, frame.width / 2, macroblock_x,
                    macroblock_y, static_cast<std::uint8_t>(chroma_mode));
                decoded = 1;
                macroblock_slice_ids[macroblock_address] = slice_id;
                macroblock_qpy[macroblock_address] = qpy;
                macroblock_disable_idc[macroblock_address] =
                    slice_header.disable_deblocking_filter_idc;
                macroblock_alpha_offsets[macroblock_address] =
                    slice_header.slice_alpha_c0_offset_div2;
                macroblock_beta_offsets[macroblock_address] =
                    slice_header.slice_beta_offset_div2;
                previous_qpy = qpy;
                if (cabac_decoder &&
                    cabac_decoder->decode_terminate_bin()) {
                    break;
                }
                ++macroblock_address;
                continue;
            }
            struct MotionPartition {
                std::int32_t x;
                std::int32_t y;
                std::int32_t width;
                std::int32_t height;
            };
            std::array<MotionPartition, 16> partitions{};
            std::array<std::array<std::uint8_t, 2>, 16>
                partition_list_flags{};
            std::array<bool, 16> partition_direct{};
            std::size_t partition_count = 0;
            if (b_slice && macroblock_type <= 3) {
                partitions[0] = {0, 0, 4, 4};
                partition_count = 1;
                partition_list_flags[0] = {{
                    static_cast<std::uint8_t>(macroblock_type == 0 ||
                                              macroblock_type == 1 ||
                                              macroblock_type == 3),
                    static_cast<std::uint8_t>(macroblock_type == 0 ||
                                              macroblock_type == 2 ||
                                              macroblock_type == 3),
                }};
                partition_direct[0] = macroblock_type == 0;
            } else if (b_slice && macroblock_type <= 21) {
                static constexpr std::array<
                    std::array<std::array<std::uint8_t, 2>, 2>, 9>
                    prediction_modes{{
                        {{{{1, 0}}, {{1, 0}}}},
                        {{{{0, 1}}, {{0, 1}}}},
                        {{{{1, 0}}, {{0, 1}}}},
                        {{{{0, 1}}, {{1, 0}}}},
                        {{{{1, 0}}, {{1, 1}}}},
                        {{{{0, 1}}, {{1, 1}}}},
                        {{{{1, 1}}, {{1, 0}}}},
                        {{{{1, 1}}, {{0, 1}}}},
                        {{{{1, 1}}, {{1, 1}}}},
                    }};
                const auto mode_pair = prediction_modes[
                    (macroblock_type - 4) / 2];
                const bool vertical_split = macroblock_type % 2 == 0;
                for (std::size_t part_index = 0; part_index < 2;
                     ++part_index) {
                    partitions[part_index] = {
                        vertical_split ? 0
                                       : static_cast<std::int32_t>(
                                             part_index * 2),
                        vertical_split
                            ? static_cast<std::int32_t>(part_index * 2)
                            : 0,
                        vertical_split ? 4 : 2,
                        vertical_split ? 2 : 4};
                    partition_list_flags[part_index] =
                        mode_pair[part_index];
                }
                partition_count = 2;
            } else if (b_slice && macroblock_type == 22) {
                static constexpr std::array<std::array<std::uint8_t, 2>, 13>
                    sub_macroblock_prediction_modes{{
                        {{1, 1}}, {{1, 0}}, {{0, 1}}, {{1, 1}},
                        {{1, 0}}, {{1, 0}}, {{0, 1}}, {{0, 1}},
                        {{1, 1}}, {{1, 1}}, {{1, 0}}, {{0, 1}},
                        {{1, 1}},
                    }};
                for (std::size_t sub_index = 0; sub_index < 4; ++sub_index) {
                    const auto sub_x =
                        static_cast<std::int32_t>(sub_index % 2) * 2;
                    const auto sub_y =
                        static_cast<std::int32_t>(sub_index / 2) * 2;
                    const auto sub_type = sub_macroblock_types[sub_index];
                    const auto sub_lists =
                        sub_macroblock_prediction_modes[sub_type];
                    const auto append_partition =
                        [&](std::int32_t x, std::int32_t y,
                            std::int32_t width, std::int32_t height,
                            bool direct) {
                        partitions[partition_count] = {x, y, width, height};
                        partition_list_flags[partition_count] = sub_lists;
                        partition_direct[partition_count] = direct;
                        ++partition_count;
                    };
                    if (sub_type == 0) {
                        append_partition(sub_x, sub_y, 2, 2, true);
                    } else if (sub_type <= 3) {
                        append_partition(sub_x, sub_y, 2, 2, false);
                    } else if (sub_type == 4 || sub_type == 6 ||
                               sub_type == 8) {
                        append_partition(sub_x, sub_y, 2, 1, false);
                        append_partition(sub_x, sub_y + 1, 2, 1, false);
                    } else if (sub_type == 5 || sub_type == 7 ||
                               sub_type == 9) {
                        append_partition(sub_x, sub_y, 1, 2, false);
                        append_partition(sub_x + 1, sub_y, 1, 2, false);
                    } else {
                        for (std::int32_t offset_y = 0; offset_y < 2;
                             ++offset_y) {
                            for (std::int32_t offset_x = 0; offset_x < 2;
                                 ++offset_x) {
                                append_partition(
                                    sub_x + offset_x, sub_y + offset_y,
                                    1, 1, false);
                            }
                        }
                    }
                }
            } else if (macroblock_type == 1) {
                partitions = {{{0, 0, 4, 2}, {0, 2, 4, 2}}};
                partition_count = 2;
            } else if (macroblock_type == 2) {
                partitions = {{{0, 0, 2, 4}, {2, 0, 2, 4}}};
                partition_count = 2;
            } else if (macroblock_type == 3 || macroblock_type == 4) {
                for (std::size_t sub_index = 0; sub_index < 4; ++sub_index) {
                    const auto sub_x =
                        static_cast<std::int32_t>(sub_index % 2) * 2;
                    const auto sub_y =
                        static_cast<std::int32_t>(sub_index / 2) * 2;
                    const auto sub_type = sub_macroblock_types[sub_index];
                    if (sub_type == 0) {
                        partitions[partition_count++] =
                            {sub_x, sub_y, 2, 2};
                    } else if (sub_type == 1) {
                        partitions[partition_count++] =
                            {sub_x, sub_y, 2, 1};
                        partitions[partition_count++] =
                            {sub_x, sub_y + 1, 2, 1};
                    } else if (sub_type == 2) {
                        partitions[partition_count++] =
                            {sub_x, sub_y, 1, 2};
                        partitions[partition_count++] =
                            {sub_x + 1, sub_y, 1, 2};
                    } else {
                        for (std::int32_t offset_y = 0; offset_y < 2;
                             ++offset_y) {
                            for (std::int32_t offset_x = 0; offset_x < 2;
                                 ++offset_x) {
                                partitions[partition_count++] = {
                                    sub_x + offset_x, sub_y + offset_y, 1, 1};
                            }
                        }
                    }
                }
            } else {
                partitions[0] = {0, 0, 4, 4};
                partition_count = 1;
            }
            auto motion = MacroblockMotionVectors{};
            auto motion_l1 = MacroblockMotionVectors{};
            std::array<std::uint8_t, 16> prediction_flags{};
            std::array<std::array<std::int32_t, 16>, 2>
                reference_indices{};
            const bool sub_macroblock_partitioning =
                (!b_slice && (macroblock_type == 3 || macroblock_type == 4)) ||
                (b_slice && macroblock_type == 22);
            std::array<std::array<std::int32_t, 4>, 2>
                sub_macroblock_reference_indices{};
            std::array<std::array<bool, 4>, 2>
                sub_macroblock_reference_index_decoded{};
            for (std::size_t list_index = 0; list_index < 2; ++list_index) {
                for (std::size_t partition_index = 0;
                     partition_index < partition_count; ++partition_index) {
                    const auto& partition = partitions[partition_index];
                    const auto list_used =
                        b_slice
                            ? partition_list_flags[partition_index]
                            : std::array<std::uint8_t, 2>{{1, 0}};
                    if (!list_used[list_index]) {
                        continue;
                    }
                    const auto sub_macroblock_index =
                        static_cast<std::size_t>(partition.y / 2) * 2 +
                        static_cast<std::size_t>(partition.x / 2);
                    const auto maximum_reference_index =
                        list_index == 0
                            ? slice_header.num_ref_idx_l0_active_minus1
                            : slice_header.num_ref_idx_l1_active_minus1;
                    std::int32_t reference_index = 0;
                    if (sub_macroblock_partitioning &&
                        sub_macroblock_reference_index_decoded[list_index]
                            [sub_macroblock_index]) {
                        reference_index =
                            sub_macroblock_reference_indices[list_index]
                                [sub_macroblock_index];
                    } else if (maximum_reference_index != 0 &&
                               !partition_direct[partition_index] &&
                               (b_slice || macroblock_type != 4)) {
                        const auto block_x =
                            static_cast<std::int64_t>(macroblock_x) * 4 +
                            partition.x;
                        const auto block_y =
                            static_cast<std::int64_t>(macroblock_y) * 4 +
                            partition.y;
                        std::uint32_t decoded_reference_index = 0;
                        if (cabac_decoder) {
                            const auto left_context =
                                neighbor_uses_nonzero_reference(
                                    block_x - 1, block_y, macroblock_address,
                                    slice_id, list_index, reference_indices,
                                    prediction_flags);
                            const auto top_context =
                                neighbor_uses_nonzero_reference(
                                    block_x, block_y - 1, macroblock_address,
                                    slice_id, list_index, reference_indices,
                                    prediction_flags);
                            const auto context_increment =
                                static_cast<std::size_t>(left_context) |
                                (static_cast<std::size_t>(top_context) << 1);
                            try {
                                decoded_reference_index =
                                    cabac_decoder->decode_reference_index(
                                        maximum_reference_index,
                                        context_increment, cabac_contexts);
                            } catch (const std::exception& error) {
                                throw std::runtime_error(
                                    "CABAC ref_idx failed at macroblock " +
                                    std::to_string(macroblock_address) +
                                    ", macroblock type " +
                                    std::to_string(macroblock_type) +
                                    ", partition " +
                                    std::to_string(partition_index) +
                                    ", list " +
                                    std::to_string(list_index) +
                                    ", active maximum " +
                                    std::to_string(maximum_reference_index) +
                                    ", context " +
                                    std::to_string(context_increment) + ": " +
                                    error.what());
                            }
                        } else if (maximum_reference_index == 1) {
                            decoded_reference_index =
                                static_cast<std::uint32_t>(
                                    !reader.read_bit());
                        } else {
                            decoded_reference_index = reader.read_ue();
                        }
                        reference_index = static_cast<std::int32_t>(
                            decoded_reference_index);
                    }
                    if (sub_macroblock_partitioning &&
                        !partition_direct[partition_index]) {
                        sub_macroblock_reference_indices[list_index]
                            [sub_macroblock_index] = reference_index;
                        sub_macroblock_reference_index_decoded[list_index]
                            [sub_macroblock_index] = true;
                    }
                    for (std::int32_t block_y = partition.y;
                         block_y < partition.y + partition.height; ++block_y) {
                        for (std::int32_t block_x = partition.x;
                             block_x < partition.x + partition.width;
                             ++block_x) {
                            const auto block_index =
                                static_cast<std::size_t>(block_y * 4 + block_x);
                            prediction_flags[block_index] |=
                                static_cast<std::uint8_t>(1U << list_index);
                            reference_indices[list_index][block_index] =
                                reference_index;
                            motion_vector_valid[macroblock_address]
                                               [block_index] = 1;
                        }
                    }
                }
            }
            motion_vector_valid[macroblock_address].fill(0);
            for (std::size_t partition_index = 0;
                 partition_index < partition_count; ++partition_index) {
                if (!partition_direct[partition_index]) {
                    continue;
                }
                const auto& partition = partitions[partition_index];
                derive_spatial_direct(
                    macroblock_address, slice_id, partition.x, partition.y,
                    partition.width, partition.height, reference_indices,
                    prediction_flags, motion, motion_l1);
            }
            std::array<std::array<MotionVector, 2>, 16> partition_motions{};
            std::array<std::array<MotionVector, 2>, 16> motion_differences{};
            std::array<std::array<std::uint8_t, 16>, 2>
                current_list_motion_valid{};
            for (std::size_t block_index = 0; block_index < 16;
                 ++block_index) {
                if (!motion_vector_valid[macroblock_address][block_index]) {
                    continue;
                }
                for (std::size_t list_index = 0; list_index < 2;
                     ++list_index) {
                    current_list_motion_valid[list_index][block_index] =
                        static_cast<std::uint8_t>(
                            (prediction_flags[block_index] &
                             (1U << list_index)) != 0);
                }
            }
            const auto magnitude = [](std::int64_t value) {
                return value < 0
                    ? static_cast<std::uint64_t>(-(value + 1)) + 1
                    : static_cast<std::uint64_t>(value);
            };
            for (std::size_t list_index = 0; list_index < 2;
                 ++list_index) {
                for (std::size_t partition_index = 0;
                     partition_index < partition_count; ++partition_index) {
                    if (partition_direct[partition_index]) {
                        continue;
                    }
                    const auto& partition = partitions[partition_index];
                    const std::array<std::uint8_t, 2> list_used =
                        b_slice
                            ? partition_list_flags[partition_index]
                            : std::array<std::uint8_t, 2>{{1, 0}};
                    if (!list_used[list_index]) {
                        continue;
                    }
                    const auto partition_block_x =
                        static_cast<std::int64_t>(macroblock_x) * 4 +
                        partition.x;
                    const auto partition_block_y =
                        static_cast<std::int64_t>(macroblock_y) * 4 +
                        partition.y;
                    auto& partition_motion =
                        partition_motions[partition_index][list_index];
                    auto& motion_difference =
                        motion_differences[partition_index][list_index];
                    const auto predictor =
                        derive_partition_motion_predictor(
                            macroblock_address, slice_id, partition.x,
                            partition.y, partition.width, partition.height,
                            list_index,
                            reference_indices[list_index]
                                             [static_cast<std::size_t>(
                                                 partition.y * 4 +
                                                 partition.x)],
                            reference_indices, prediction_flags,
                            current_list_motion_valid[list_index], motion,
                            motion_l1);
                    partition_motion = predictor;
                    const auto left_mvd =
                        neighbor_motion_vector_difference(
                            partition_block_x - 1, partition_block_y,
                            macroblock_address, slice_id,
                            current_list_motion_valid[list_index],
                            list_index == 1);
                    const auto top_mvd =
                        neighbor_motion_vector_difference(
                            partition_block_x, partition_block_y - 1,
                            macroblock_address, slice_id,
                            current_list_motion_valid[list_index],
                            list_index == 1);
                    for (std::size_t component = 0;
                         component < partition_motion.size(); ++component) {
                        std::int64_t difference = 0;
                        if (cabac_decoder) {
                            const auto neighbor_magnitude =
                                (left_mvd
                                     ? magnitude((*left_mvd)[component])
                                     : 0) +
                                (top_mvd
                                     ? magnitude((*top_mvd)[component])
                                     : 0);
                            difference =
                                cabac_decoder
                                    ->decode_motion_vector_difference(
                                        static_cast<std::uint8_t>(component),
                                        neighbor_magnitude, cabac_contexts);
                        } else {
                            difference = reader.read_se();
                        }
                        motion_difference[component] = difference;
                        if ((difference > 0 &&
                             partition_motion[component] >
                                 std::numeric_limits<std::int64_t>::max() -
                                     difference) ||
                            (difference < 0 &&
                             partition_motion[component] <
                                 std::numeric_limits<std::int64_t>::min() -
                                     difference)) {
                            throw std::runtime_error(
                                "inter motion vector exceeds the supported integer range");
                        }
                        partition_motion[component] += difference;
                    }
                    for (std::int32_t block_y = partition.y;
                         block_y < partition.y + partition.height; ++block_y) {
                        for (std::int32_t block_x = partition.x;
                             block_x < partition.x + partition.width;
                             ++block_x) {
                            const auto block_index =
                                static_cast<std::size_t>(block_y * 4 +
                                                         block_x);
                            if (list_index == 0) {
                                motion[block_index] = partition_motion;
                                macroblock_motion_vector_differences
                                    [macroblock_address][block_index] =
                                        motion_difference;
                                prediction_flags[block_index] |= 1;
                            } else {
                                motion_l1[block_index] = partition_motion;
                                macroblock_motion_vector_differences_l1
                                    [macroblock_address][block_index] =
                                        motion_difference;
                                prediction_flags[block_index] |= 2;
                            }
                            current_list_motion_valid[list_index][block_index] =
                                1;
                        }
                    }
                }
            }
            for (std::size_t partition_index = 0;
                 partition_index < partition_count; ++partition_index) {
                if (partition_direct[partition_index]) {
                    continue;
                }
                const auto& partition = partitions[partition_index];
                for (std::int32_t block_y = partition.y;
                     block_y < partition.y + partition.height; ++block_y) {
                    for (std::int32_t block_x = partition.x;
                         block_x < partition.x + partition.width; ++block_x) {
                        const auto block_index =
                            static_cast<std::size_t>(block_y * 4 + block_x);
                        motion_vector_valid[macroblock_address][block_index] =
                            1;
                    }
                }
            }
            macroblock_reference_indices[macroblock_address] =
                reference_indices;
            macroblock_motion_vectors[macroblock_address] = motion;
            macroblock_motion_vectors_l1[macroblock_address] = motion_l1;
            macroblock_prediction_flags[macroblock_address] =
                prediction_flags;
            static constexpr std::array<std::uint8_t, 48> inter_cbp{{
                0, 16, 1, 2, 4, 8, 32, 3, 5, 10, 12, 15, 47, 7, 11, 13,
                14, 6, 9, 31, 35, 37, 42, 44, 33, 34, 36, 40, 39, 43, 45,
                46, 17, 18, 20, 24, 19, 21, 26, 28, 23, 27, 29, 30, 22,
                25, 38, 41,
            }};
            std::uint8_t luma_cbp = 0;
            std::uint8_t chroma_cbp = 0;
            if (cabac_decoder) {
                const auto left_address =
                    macroblock_x != 0 ? macroblock_address - 1
                                      : macroblock_address;
                const auto top_address =
                    macroblock_y != 0
                        ? macroblock_address - macroblock_width
                        : macroblock_address;
                const auto left_luma_cbp =
                    macroblock_x != 0 &&
                            decoded_macroblocks[left_address] != 0 &&
                            macroblock_slice_ids[left_address] == slice_id
                        ? macroblock_luma_cbp[left_address]
                        : 15;
                const auto top_luma_cbp =
                    macroblock_y != 0 &&
                            decoded_macroblocks[top_address] != 0 &&
                            macroblock_slice_ids[top_address] == slice_id
                        ? macroblock_luma_cbp[top_address]
                        : 15;
                const auto left_chroma_cbp =
                    macroblock_x != 0 &&
                            decoded_macroblocks[left_address] != 0 &&
                            macroblock_slice_ids[left_address] == slice_id
                        ? macroblock_chroma_cbp[left_address]
                        : 0;
                const auto top_chroma_cbp =
                    macroblock_y != 0 &&
                            decoded_macroblocks[top_address] != 0 &&
                            macroblock_slice_ids[top_address] == slice_id
                        ? macroblock_chroma_cbp[top_address]
                        : 0;
                luma_cbp = cabac_decoder->decode_luma_coded_block_pattern(
                    left_luma_cbp, top_luma_cbp, cabac_contexts);
                chroma_cbp =
                    cabac_decoder->decode_chroma_coded_block_pattern(
                        left_chroma_cbp, top_chroma_cbp, cabac_contexts);
            } else {
                const auto cbp_code = reader.read_ue();
                if (cbp_code >= inter_cbp.size()) {
                    throw std::runtime_error(
                        "P coded_block_pattern code exceeds 47");
                }
                const auto coded_block_pattern = inter_cbp[cbp_code];
                luma_cbp = coded_block_pattern & 15U;
                chroma_cbp = (coded_block_pattern >> 4) & 3U;
            }
            const auto coded_block_pattern = static_cast<std::uint8_t>(
                luma_cbp | (chroma_cbp << 4));
            macroblock_luma_cbp[macroblock_address] = luma_cbp;
            macroblock_chroma_cbp[macroblock_address] = chroma_cbp;
            bool no_sub_macroblock_partition_smaller_than_8x8 = true;
            if ((!b_slice && (macroblock_type == 3 || macroblock_type == 4)) ||
                (b_slice && macroblock_type == 22)) {
                for (const auto sub_type : sub_macroblock_types) {
                    if ((b_slice && sub_type == 0 &&
                         !sps.direct_8x8_inference) ||
                        (b_slice && sub_type >= 4) ||
                        (!b_slice && sub_type != 0)) {
                        no_sub_macroblock_partition_smaller_than_8x8 = false;
                        break;
                    }
                }
            }
            const bool transform_allowed =
                no_sub_macroblock_partition_smaller_than_8x8 &&
                !(b_slice && macroblock_type == 0 &&
                  !sps.direct_8x8_inference);
            bool transform_8x8 = false;
            if (luma_cbp != 0 && pps.transform_8x8_mode &&
                transform_allowed) {
                const bool left_transform =
                    macroblock_x != 0 &&
                    decoded_macroblocks[macroblock_address - 1] &&
                    macroblock_slice_ids[macroblock_address - 1] == slice_id &&
                    macroblock_transform_8x8[macroblock_address - 1];
                const bool top_transform =
                    macroblock_y != 0 &&
                    decoded_macroblocks[macroblock_address -
                                        macroblock_width] &&
                    macroblock_slice_ids[macroblock_address -
                                         macroblock_width] == slice_id &&
                    macroblock_transform_8x8[macroblock_address -
                                             macroblock_width];
                transform_8x8 = cabac_decoder
                    ? cabac_decoder->decode_bin(
                          cabac_contexts[399 +
                              static_cast<std::size_t>(left_transform) +
                              static_cast<std::size_t>(top_transform)])
                    : reader.read_bit();
                if (transform_8x8 && !cabac_decoder) {
                    throw std::runtime_error(
                        "P CAVLC 8x8 residual decoding is unsupported");
                }
            }
            macroblock_transform_8x8[macroblock_address] = transform_8x8;
            auto qpy = previous_qpy;
            if (coded_block_pattern != 0) {
                const std::int64_t qp_delta = cabac_decoder
                    ? cabac_decoder->decode_mb_qp_delta(
                          previous_mb_qp_delta, cabac_contexts)
                    : reader.read_se();
                if (qp_delta < -26 || qp_delta > 25) {
                    throw std::runtime_error("P mb_qp_delta is outside [-26,25]");
                }
                qpy = static_cast<std::int32_t>(
                    (qpy + qp_delta + 52) % 52);
                previous_mb_qp_delta = qp_delta;
            } else {
                previous_mb_qp_delta = 0;
            }
            if (qpy < 0 || qpy > 51) {
                throw std::runtime_error("P macroblock QP is outside [0,51]");
            }

            std::array<CavlcResidualBlock, 16> luma_blocks;
            for (auto& block : luma_blocks) {
                block.coefficients.resize(16, 0);
            }
            std::array<std::array<std::int32_t, 64>, 4> luma8x8_levels{};
            bool any_luma_coefficients = false;
            if (transform_8x8) {
                static constexpr std::array<std::size_t, 64> scan{{
                    0, 1, 8, 16, 9, 2, 3, 10, 17, 24, 32, 25, 18, 11, 4,
                    5, 12, 19, 26, 33, 40, 48, 41, 34, 27, 20, 13, 6, 7,
                    14, 21, 28, 35, 42, 49, 56, 57, 50, 43, 36, 29, 22,
                    15, 23, 30, 37, 44, 51, 58, 59, 52, 45, 38, 31, 39,
                    46, 53, 60, 61, 54, 47, 55, 62, 63,
                }};
                for (std::uint32_t block_index = 0; block_index < 4;
                     ++block_index) {
                    if ((luma_cbp & (1U << block_index)) == 0) {
                        continue;
                    }
                    const auto scan_levels =
                        cabac_decoder->decode_luma8x8_residual_block(
                            cabac_contexts);
                    for (std::size_t scan_index = 0;
                         scan_index < scan.size(); ++scan_index) {
                        luma8x8_levels[block_index][scan[scan_index]] =
                            scan_levels[scan_index];
                    }
                    for (std::uint32_t sub_y = 0; sub_y < 2; ++sub_y) {
                        for (std::uint32_t sub_x = 0; sub_x < 2; ++sub_x) {
                            std::uint8_t total = 0;
                            for (std::uint32_t row = sub_y * 4;
                                 row < sub_y * 4 + 4; ++row) {
                                for (std::uint32_t column = sub_x * 4;
                                     column < sub_x * 4 + 4; ++column) {
                                    total += luma8x8_levels[block_index]
                                        [row * 8 + column] != 0;
                                }
                            }
                            const auto grid_x =
                                macroblock_x * 4 + (block_index % 2) * 2 +
                                sub_x;
                            const auto grid_y =
                                macroblock_y * 4 + (block_index / 2) * 2 +
                                sub_y;
                            const auto grid_index =
                                static_cast<std::size_t>(grid_y) *
                                    luma_block_width +
                                grid_x;
                            luma_total_coefficients[grid_index] = total;
                            luma_coded_block_flags[grid_index] = 1;
                            any_luma_coefficients |= total != 0;
                        }
                    }
                }
            }
            for (std::uint32_t block_index = 0;
                 !transform_8x8 && block_index < 16; ++block_index) {
                const auto position = luma_block_position(block_index);
                const auto block_x = macroblock_x * 4 + position[0];
                const auto block_y = macroblock_y * 4 + position[1];
                const auto block_grid_index =
                    static_cast<std::size_t>(block_y) * luma_block_width +
                    block_x;
                if ((luma_cbp & (1U << (block_index / 4))) == 0) {
                    luma_total_coefficients[block_grid_index] = 0;
                    luma_coded_block_flags[block_grid_index] = 0;
                    continue;
                }
                CavlcResidualBlock block;
                bool block_coded = false;
                if (cabac_decoder) {
                    const auto neighbor_coded =
                        [&](std::uint32_t neighbor_x,
                            std::uint32_t neighbor_y, bool exists) {
                        if (!exists) {
                            return false;
                        }
                        const auto neighbor_macroblock =
                            static_cast<std::size_t>(neighbor_y / 4) *
                                macroblock_width +
                            neighbor_x / 4;
                        if (neighbor_macroblock != macroblock_address &&
                            (!decoded_macroblocks[neighbor_macroblock] ||
                             macroblock_slice_ids[neighbor_macroblock] !=
                                 slice_id)) {
                            return false;
                        }
                        const auto neighbor_group =
                            ((neighbor_y % 4) / 2) * 2 +
                            (neighbor_x % 4) / 2;
                        const auto neighbor_index =
                            static_cast<std::size_t>(neighbor_y) *
                                luma_block_width +
                            neighbor_x;
                        return (macroblock_luma_cbp[neighbor_macroblock] &
                                (1U << neighbor_group)) != 0 &&
                               luma_coded_block_flags[neighbor_index] != 0;
                    };
                    const auto residual =
                        cabac_decoder->decode_residual_block(
                            2,
                            neighbor_coded(block_x == 0 ? 0 : block_x - 1,
                                           block_y, block_x != 0),
                            neighbor_coded(block_x,
                                           block_y == 0 ? 0 : block_y - 1,
                                           block_y != 0),
                            cabac_contexts);
                    block.coefficients.assign(
                        residual.levels.begin(), residual.levels.end());
                    block.total_coeff = static_cast<std::uint8_t>(
                        std::count_if(
                            residual.levels.begin(), residual.levels.end(),
                            [](std::int32_t level) { return level != 0; }));
                    block_coded = residual.coded;
                } else {
                    block = decode_cavlc_residual_block(
                        reader,
                        derive_luma_nc(block_x, block_y, macroblock_address,
                                       slice_id),
                        16);
                    block_coded = block.total_coeff != 0;
                }
                luma_total_coefficients[block_grid_index] = block.total_coeff;
                luma_coded_block_flags[block_grid_index] = block_coded;
                any_luma_coefficients |= block.total_coeff != 0;
                luma_blocks[block_index] = std::move(block);
            }

            const auto qpc_cb =
                chroma_qp(qpy, pps.chroma_qp_index_offset);
            const auto qpc_cr =
                chroma_qp(qpy, pps.second_chroma_qp_index_offset);
            std::array<CavlcResidualBlock, 2> chroma_dc_blocks;
            std::array<std::array<CavlcResidualBlock, 4>, 2>
                chroma_ac_blocks;
            for (auto& plane_blocks : chroma_ac_blocks) {
                for (auto& block : plane_blocks) {
                    block.coefficients.resize(15, 0);
                }
            }
            bool any_chroma_coefficients = false;
            if (chroma_cbp != 0) {
                for (std::size_t plane_index = 0; plane_index < 2;
                     ++plane_index) {
                    auto& block = chroma_dc_blocks[plane_index];
                    if (cabac_decoder) {
                        const auto left_address =
                            macroblock_x != 0 ? macroblock_address - 1
                                              : macroblock_address;
                        const auto top_address =
                            macroblock_y != 0
                                ? macroblock_address - macroblock_width
                                : macroblock_address;
                        const auto dc_condition =
                            [&](std::size_t neighbor_address,
                                bool available) {
                            return available &&
                                   decoded_macroblocks[neighbor_address] &&
                                   macroblock_slice_ids[neighbor_address] ==
                                       slice_id &&
                                   macroblock_chroma_dc_coded[plane_index]
                                                             [neighbor_address] !=
                                       0;
                        };
                        const auto residual =
                            cabac_decoder->decode_residual_block(
                                3, dc_condition(left_address,
                                                macroblock_x != 0),
                                dc_condition(top_address,
                                             macroblock_y != 0),
                                cabac_contexts);
                        block.coefficients.assign(
                            residual.levels.begin(),
                            residual.levels.begin() + 4);
                        block.total_coeff = static_cast<std::uint8_t>(
                            std::count_if(
                                block.coefficients.begin(),
                                block.coefficients.end(),
                                [](std::int32_t level) { return level != 0; }));
                        macroblock_chroma_dc_coded[plane_index]
                                                  [macroblock_address] =
                            residual.coded;
                    } else {
                        block = decode_cavlc_residual_block(reader, -1, 4);
                    }
                    any_chroma_coefficients |= block.total_coeff != 0;
                }
            }
            if (chroma_cbp == 2) {
                for (std::size_t plane_index = 0; plane_index < 2;
                     ++plane_index) {
                    for (std::uint32_t block_index = 0; block_index < 4;
                         ++block_index) {
                        const auto block_x =
                            macroblock_x * 2 + block_index % 2;
                        const auto block_y =
                            macroblock_y * 2 + block_index / 2;
                        const auto block_grid_index =
                            static_cast<std::size_t>(block_y) *
                                chroma_block_width +
                            block_x;
                        const auto neighbor_count =
                            [&](std::uint32_t neighbor_x,
                                std::uint32_t neighbor_y,
                                bool exists) -> std::optional<std::uint8_t> {
                            if (!exists) {
                                return std::nullopt;
                            }
                            const auto neighbor_macroblock =
                                static_cast<std::size_t>(neighbor_y / 2) *
                                    macroblock_width +
                                neighbor_x / 2;
                            if (neighbor_macroblock != macroblock_address &&
                                (!decoded_macroblocks[neighbor_macroblock] ||
                                 macroblock_slice_ids[neighbor_macroblock] !=
                                     slice_id)) {
                                return std::nullopt;
                            }
                            return chroma_total_coefficients[plane_index][
                                static_cast<std::size_t>(neighbor_y) *
                                    chroma_block_width +
                                neighbor_x];
                        };
                        const auto left = neighbor_count(
                            block_x == 0 ? 0 : block_x - 1, block_y,
                            block_x != 0);
                        const auto top = neighbor_count(
                            block_x, block_y == 0 ? 0 : block_y - 1,
                            block_y != 0);
                        const auto nc = left && top
                            ? (static_cast<std::int32_t>(*left) +
                               static_cast<std::int32_t>(*top) + 1) /
                                  2
                            : left
                                ? static_cast<std::int32_t>(*left)
                                : top
                                    ? static_cast<std::int32_t>(*top)
                                    : 0;
                        auto& block =
                            chroma_ac_blocks[plane_index][block_index];
                        if (cabac_decoder) {
                            const auto neighbor_coded =
                                [&](std::uint32_t neighbor_x,
                                    std::uint32_t neighbor_y,
                                    bool exists) -> std::optional<bool> {
                                if (!exists) {
                                    return std::nullopt;
                                }
                                const auto neighbor_macroblock =
                                    static_cast<std::size_t>(neighbor_y / 2) *
                                        macroblock_width +
                                    neighbor_x / 2;
                                if (neighbor_macroblock != macroblock_address &&
                                    (!decoded_macroblocks[neighbor_macroblock] ||
                                     macroblock_slice_ids[neighbor_macroblock] !=
                                         slice_id)) {
                                    return std::nullopt;
                                }
                                const auto neighbor_index =
                                    static_cast<std::size_t>(neighbor_y) *
                                        chroma_block_width +
                                    neighbor_x;
                                return chroma_coded_block_flags[plane_index]
                                                               [neighbor_index] !=
                                       0;
                            };
                            const auto left_coded = neighbor_coded(
                                block_x == 0 ? 0 : block_x - 1, block_y,
                                block_x != 0);
                            const auto top_coded = neighbor_coded(
                                block_x, block_y == 0 ? 0 : block_y - 1,
                                block_y != 0);
                            const auto residual =
                                cabac_decoder->decode_residual_block(
                                    4, left_coded.value_or(false),
                                    top_coded.value_or(false), cabac_contexts);
                            block.coefficients.assign(
                                residual.levels.begin(),
                                residual.levels.begin() + 15);
                            block.total_coeff = static_cast<std::uint8_t>(
                                std::count_if(
                                    block.coefficients.begin(),
                                    block.coefficients.end(),
                                    [](std::int32_t level) {
                                        return level != 0;
                                    }));
                            chroma_coded_block_flags[plane_index]
                                [block_grid_index] = residual.coded;
                        } else {
                            block = decode_cavlc_residual_block(reader, nc, 15);
                            chroma_coded_block_flags[plane_index]
                                [block_grid_index] =
                                block.total_coeff != 0;
                        }
                        chroma_total_coefficients[plane_index][block_grid_index] =
                            block.total_coeff;
                        any_chroma_coefficients |= block.total_coeff != 0;
                    }
                }
            } else {
                for (std::size_t plane_index = 0; plane_index < 2;
                     ++plane_index) {
                    for (std::uint32_t block_index = 0; block_index < 4;
                         ++block_index) {
                        const auto block_x =
                            macroblock_x * 2 + block_index % 2;
                        const auto block_y =
                            macroblock_y * 2 + block_index / 2;
                        chroma_total_coefficients[plane_index]
                            [static_cast<std::size_t>(block_y) *
                                 chroma_block_width +
                             block_x] = 0;
                        chroma_coded_block_flags[plane_index]
                            [static_cast<std::size_t>(block_y) *
                                 chroma_block_width +
                             block_x] = 0;
                        chroma_ac_blocks[plane_index][block_index]
                            .coefficients.resize(15, 0);
                    }
                }
            }
            if (any_luma_coefficients &&
                ((transform_8x8 &&
                  sps.seq_intra_luma_8x8_scaling_list_present) ||
                 (!transform_8x8 &&
                  sps.seq_intra_luma_4x4_scaling_list_present) ||
                 pps.pic_scaling_matrix_present)) {
                throw std::runtime_error(
                    "P luma residuals with custom scaling matrices are unsupported");
            }
            if (any_chroma_coefficients &&
                (sps.seq_intra_chroma_4x4_scaling_list_present ||
                 pps.pic_scaling_matrix_present)) {
                throw std::runtime_error(
                    "P chroma residuals with custom scaling matrices are unsupported");
            }
            std::array<std::array<std::int64_t, 16>, 16>
                luma_residual_blocks{};
            std::array<std::array<std::int64_t, 64>, 4>
                luma8x8_residual_blocks{};
            if (transform_8x8) {
                for (std::uint32_t block_index = 0; block_index < 4;
                     ++block_index) {
                    luma8x8_residual_blocks[block_index] =
                        inverse_transform_luma8x8(inverse_scale_luma8x8(
                            luma8x8_levels[block_index], qpy));
                }
            } else {
                for (std::uint32_t block_index = 0; block_index < 16;
                     ++block_index) {
                    luma_residual_blocks[block_index] =
                        inverse_transform_luma4x4(inverse_scale_intra4x4(
                            luma_blocks[block_index], qpy));
                }
            }
            std::array<std::array<std::int64_t, 4>, 2>
                chroma_dc_residuals{};
            if (chroma_cbp != 0) {
                chroma_dc_residuals[0] = reconstruct_intra16x16_chroma_dc(
                    chroma_dc_blocks[0], qpc_cb);
                chroma_dc_residuals[1] = reconstruct_intra16x16_chroma_dc(
                    chroma_dc_blocks[1], qpc_cr);
            }
            std::array<std::array<std::array<std::int64_t, 16>, 4>, 2>
                chroma_residual_blocks{};
            for (std::size_t plane_index = 0; plane_index < 2;
                 ++plane_index) {
                const auto qpc = plane_index == 0 ? qpc_cb : qpc_cr;
                for (std::uint32_t block_index = 0; block_index < 4;
                     ++block_index) {
                    const auto coefficients = inverse_scale_intra4x4_ac(
                        chroma_ac_blocks[plane_index][block_index], qpc,
                        chroma_dc_residuals[plane_index][block_index]);
                    chroma_residual_blocks[plane_index][block_index] =
                        inverse_transform_luma4x4(coefficients);
                }
            }
            reconstruct_macroblock(
                macroblock_address, motion,
                b_slice ? &motion_l1 : nullptr,
                b_slice ? &prediction_flags : nullptr,
                &macroblock_reference_indices[macroblock_address]);
            if (transform_8x8) {
                for (std::uint32_t block_index = 0; block_index < 4;
                     ++block_index) {
                    const auto origin_x =
                        macroblock_x * 16 + (block_index % 2) * 8;
                    const auto origin_y =
                        macroblock_y * 16 + (block_index / 2) * 8;
                    for (std::uint32_t row = 0; row < 8; ++row) {
                        for (std::uint32_t column = 0; column < 8; ++column) {
                            auto& sample = frame.luma[
                                static_cast<std::size_t>(origin_y + row) *
                                    frame.width +
                                origin_x + column];
                            sample = static_cast<std::uint8_t>(
                                std::clamp<std::int64_t>(
                                    static_cast<std::int64_t>(sample) +
                                        luma8x8_residual_blocks[block_index]
                                            [row * 8 + column],
                                    0, 255));
                        }
                    }
                }
            } else {
                for (std::uint32_t block_index = 0; block_index < 16;
                     ++block_index) {
                    const auto position = luma_block_position(block_index);
                    const auto origin_x =
                        macroblock_x * 16 + position[0] * 4;
                    const auto origin_y =
                        macroblock_y * 16 + position[1] * 4;
                    for (std::uint32_t row = 0; row < 4; ++row) {
                        for (std::uint32_t column = 0; column < 4; ++column) {
                            auto& sample = frame.luma[
                                static_cast<std::size_t>(origin_y + row) *
                                    frame.width +
                                origin_x + column];
                            sample = static_cast<std::uint8_t>(
                                std::clamp<std::int64_t>(
                                    static_cast<std::int64_t>(sample) +
                                        luma_residual_blocks[block_index]
                                            [row * 4 + column],
                                    0, 255));
                        }
                    }
                }
            }
            add_intra16x16_chroma_residual(
                frame.chroma_u, frame.width / 2, macroblock_x, macroblock_y,
                chroma_residual_blocks[0]);
            add_intra16x16_chroma_residual(
                frame.chroma_v, frame.width / 2, macroblock_x, macroblock_y,
                chroma_residual_blocks[1]);
            decoded = 1;
            macroblock_slice_ids[macroblock_address] = slice_id;
            macroblock_qpy[macroblock_address] = qpy;
            macroblock_disable_idc[macroblock_address] =
                slice_header.disable_deblocking_filter_idc;
            macroblock_alpha_offsets[macroblock_address] =
                slice_header.slice_alpha_c0_offset_div2;
            macroblock_beta_offsets[macroblock_address] =
                slice_header.slice_beta_offset_div2;
            previous_qpy = qpy;
            if (cabac_decoder && cabac_decoder->decode_terminate_bin()) {
                break;
            }
            ++macroblock_address;
        }
        ++slice_id;
    }
    const auto first_missing_macroblock =
        std::find(decoded_macroblocks.begin(), decoded_macroblocks.end(), 0);
    if (first_missing_macroblock != decoded_macroblocks.end()) {
        const auto missing_address = static_cast<std::size_t>(
            std::distance(decoded_macroblocks.begin(),
                          first_missing_macroblock));
        throw std::runtime_error(
            std::string(picture_header.slice_type == 1 ? "B" : "P") +
            " slices do not cover every picture macroblock (first missing " +
            std::to_string(missing_address) + " of " +
            std::to_string(macroblock_count) + ")");
    }
    frame.colocated_motion_field.resize(macroblock_count);
    for (std::size_t address = 0; address < macroblock_count; ++address) {
        for (std::size_t block_index = 0; block_index < 16; ++block_index) {
            const auto flags =
                macroblock_prediction_flags[address][block_index];
            if ((flags & 1) != 0) {
                frame.colocated_motion_field[address][block_index] = {
                    macroblock_reference_indices[address][0][block_index],
                    macroblock_motion_vectors[address][block_index]};
            } else if ((flags & 2) != 0) {
                frame.colocated_motion_field[address][block_index] = {
                    macroblock_reference_indices[address][1][block_index],
                    macroblock_motion_vectors_l1[address][block_index]};
            }
        }
    }
    deblock_p_inter_frame(
        frame, macroblock_width, macroblock_height, macroblock_qpy,
        macroblock_motion_vectors, macroblock_motion_vectors_l1,
        macroblock_reference_indices, macroblock_prediction_flags,
        macroblock_intra16, luma_total_coefficients,
        macroblock_slice_ids, macroblock_disable_idc,
        macroblock_alpha_offsets, macroblock_beta_offsets, pps,
        reference_frames_l0, reference_frames_l1);
    return frame;
}

Yuv420Frame decode_p_frame(
    const std::vector<std::vector<std::uint8_t>>& slice_nals,
    const SpsInfo& sps, const PpsInfo& pps,
    const Yuv420Frame& reference_frame,
    const Yuv420Frame* reference_frame_l1) {
    std::vector<const Yuv420Frame*> reference_frames_l0{&reference_frame};
    std::vector<const Yuv420Frame*> reference_frames_l1;
    if (reference_frame_l1 != nullptr) {
        reference_frames_l1.push_back(reference_frame_l1);
    }
    return decode_p_frame_with_lists(slice_nals, sps, pps,
                                     reference_frames_l0,
                                     reference_frames_l1);
}

Yuv420Frame decode_p_skip_frame(
    const std::vector<std::vector<std::uint8_t>>& slice_nals,
    const SpsInfo& sps, const PpsInfo& pps,
    const Yuv420Frame& reference_frame) {
    return decode_p_frame(slice_nals, sps, pps, reference_frame);
}

Yuv420Frame decode_idr_i_pcm_sample(const std::vector<std::uint8_t>& sample,
                                    std::size_t nal_length_size,
                                    const SpsInfo& sps, const PpsInfo& pps) {
    const auto nals = split_h264_sample_nals(sample, nal_length_size);
    std::vector<std::vector<std::uint8_t>> slices;
    for (const auto& nal : nals) {
        const auto header = parse_nal_header(nal);
        if (header.unit_type == 5) {
            slices.push_back(nal);
        } else if (header.unit_type >= 1 && header.unit_type <= 5) {
            throw std::runtime_error(
                "compressed sample contains unsupported non-IDR VCL NAL units");
        }
    }
    if (slices.empty()) {
        throw std::runtime_error(
            "compressed sample contains no IDR slice NAL units");
    }
    return decode_idr_i_pcm_frame(slices, sps, pps);
}

Yuv420Frame decode_idr_i_sample(const std::vector<std::uint8_t>& sample,
                                std::size_t nal_length_size,
                                const SpsInfo& sps, const PpsInfo& pps) {
    const auto nals = split_h264_sample_nals(sample, nal_length_size);
    std::vector<std::vector<std::uint8_t>> slices;
    for (const auto& nal : nals) {
        const auto header = parse_nal_header(nal);
        if (header.unit_type == 5) {
            slices.push_back(nal);
        } else if (header.unit_type >= 1 && header.unit_type <= 5) {
            throw std::runtime_error(
                "compressed sample contains unsupported non-IDR VCL NAL units");
        }
    }
    if (slices.empty()) {
        throw std::runtime_error(
            "compressed sample contains no IDR slice NAL units");
    }
    return decode_idr_i_frame(slices, sps, pps);
}

Yuv420Frame decode_h264_reference_frame(
    const std::vector<std::vector<std::uint8_t>>& slice_nals,
    const SpsInfo& sps, const PpsInfo& pps,
    const std::vector<const Yuv420Frame*>& reference_frames_l0,
    const std::vector<const Yuv420Frame*>& reference_frames_l1) {
    if (slice_nals.empty()) {
        throw std::invalid_argument("H.264 picture contains no slice NAL units");
    }
    const auto unit_type = parse_nal_header(slice_nals.front()).unit_type;
    if (unit_type == 5) {
        return decode_idr_i_frame(slice_nals, sps, pps);
    }
    if (unit_type == 1) {
        if (reference_frames_l0.empty() ||
            reference_frames_l0.front() == nullptr) {
            throw std::invalid_argument(
                "non-IDR inter picture requires a decoded reference frame");
        }
        const auto header =
            parse_p_slice_header(slice_nals.front(), sps, pps);
        if (header.slice_type == 1 && reference_frames_l1.empty()) {
            throw std::invalid_argument(
                "B picture requires both decoded reference lists");
        }
        return decode_p_frame_with_lists(slice_nals, sps, pps,
                                         reference_frames_l0,
                                         reference_frames_l1);
    }
    throw std::runtime_error(
        "H.264 frame decoding supports IDR I and restricted P/B pictures only");
}

Yuv420Frame decode_h264_reference_frame(
    const std::vector<std::vector<std::uint8_t>>& slice_nals,
    const SpsInfo& sps, const PpsInfo& pps,
    const Yuv420Frame* reference_frame,
    const Yuv420Frame* reference_frame_l1) {
    std::vector<const Yuv420Frame*> reference_frames_l0;
    std::vector<const Yuv420Frame*> reference_frames_l1;
    if (reference_frame != nullptr) {
        reference_frames_l0.push_back(reference_frame);
    }
    if (reference_frame_l1 != nullptr) {
        reference_frames_l1.push_back(reference_frame_l1);
    }
    return decode_h264_reference_frame(slice_nals, sps, pps,
                                       reference_frames_l0,
                                       reference_frames_l1);
}

} // namespace pygorvid
