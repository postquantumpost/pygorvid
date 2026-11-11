#include "pygorvid/pygorvid.hpp"
#include "pygorvid/h264.hpp"
#include "pygorvid/cabac.hpp"
#include "pygorvid/cavlc.hpp"
#include "pygorvid/jpeg.hpp"
#include "pygorvid/png.hpp"
#include "pygorvid/video_samples.hpp"

#include <algorithm>
#include <array>
#include <cassert>
#include <cstdint>
#include <filesystem>
#include <fstream>
#include <limits>
#include <string>
#include <stdexcept>
#include <utility>
#include <vector>

namespace {

void test_cabac_arithmetic_engine() {
    const auto intra_contexts = pygorvid::make_cabac_slice_contexts(2, 3, 26);
    assert(intra_contexts.size() == 460);
    assert(intra_contexts[3].state_index() == 46);
    assert(!intra_contexts[3].mps());
    assert(intra_contexts[60].state_index() == 22);
    assert(!intra_contexts[60].mps());

    const auto inter_contexts = pygorvid::make_cabac_slice_contexts(0, 0, 30);
    assert(inter_contexts[14].state_index() == 53);
    assert(!inter_contexts[14].mps());

    {
        const std::vector<std::uint8_t> data(8, 0);
        pygorvid::BitReader bits(data);
        pygorvid::CabacArithmeticDecoder decoder(bits);
        std::vector<pygorvid::CabacContextModel> contexts;
        contexts.reserve(460);
        for (std::size_t index = 0; index < 460; ++index) {
            contexts.emplace_back(0, 64, 26);
        }
        contexts[12] = pygorvid::CabacContextModel(0, 0, 26);
        contexts[14] = pygorvid::CabacContextModel(0, 0, 26);
        contexts[15] = pygorvid::CabacContextModel(0, 0, 26);
        contexts[16] = pygorvid::CabacContextModel(0, 0, 26);
        assert(decoder.decode_p_skip_flag(0, false, false, false, false,
                                          contexts));
        assert(!decoder.decode_p_skip_flag(0, true, false, false, false,
                                           contexts));
        assert(decoder.decode_p_mb_type(0, contexts) == 0);
    }

    {
        const std::vector<std::uint8_t> data{0, 0};
        pygorvid::BitReader bits(data);
        pygorvid::CabacArithmeticDecoder decoder(bits);
        auto contexts = pygorvid::make_cabac_slice_contexts(2, 0, 26);
        assert(decoder.decode_i_intra_mb_type(2, contexts, false, false,
                                              false, false) == 0);
    }
    {
        const std::vector<std::uint8_t> data{0, 0};
        pygorvid::BitReader bits(data);
        pygorvid::CabacArithmeticDecoder decoder(bits);
        std::vector<pygorvid::CabacContextModel> contexts;
        contexts.reserve(460);
        for (std::size_t index = 0; index < 460; ++index) {
            contexts.emplace_back(0, 63, 26);
        }
        contexts[3] = pygorvid::CabacContextModel(0, 64, 26);
        assert(decoder.decode_i_intra_mb_type(2, contexts, false, false,
                                              false, false) == 1);
    }
    {
        const std::vector<std::uint8_t> data{0xfe, 0xfc};
        pygorvid::BitReader bits(data);
        pygorvid::CabacArithmeticDecoder decoder(bits);
        auto contexts = pygorvid::make_cabac_slice_contexts(2, 0, 26);
        assert(decoder.decode_i_intra_mb_type(2, contexts, false, false,
                                              false, false) == 25);
        assert(decoder.terminated());
    }
    {
        const std::vector<std::uint8_t> data(16, 0);
        pygorvid::BitReader bits(data);
        pygorvid::CabacArithmeticDecoder decoder(bits);
        std::vector<pygorvid::CabacContextModel> contexts;
        contexts.reserve(460);
        for (std::size_t index = 0; index < 460; ++index) {
            contexts.emplace_back(0, 63, 26);
        }
        assert(decoder.decode_intra_chroma_pred_mode(false, false,
                                                      contexts) == 0);
        assert(decoder.decode_intra4x4_pred_mode(3, contexts) == 0);
        assert(decoder.decode_luma_coded_block_pattern(0, 0, contexts) == 0);
        assert(decoder.decode_chroma_coded_block_pattern(0, 0, contexts) == 0);
        assert(decoder.decode_mb_qp_delta(0, contexts) == 0);
        const auto residual = decoder.decode_residual_block(
            2, false, false, contexts);
        assert(!residual.coded);
        assert(std::all_of(residual.levels.begin(), residual.levels.end(),
                           [](std::int32_t level) { return level == 0; }));

        const auto residual8x8 =
            decoder.decode_luma8x8_residual_block(contexts);
        assert(std::count_if(
                   residual8x8.begin(), residual8x8.end(),
                   [](std::int32_t level) { return level != 0; }) == 1);
        assert(residual8x8[63] == 1);
    }

    const std::vector<std::uint8_t> initial_data{0b01010101, 0b10000000};
    pygorvid::BitReader initial_bits(initial_data);
    pygorvid::CabacArithmeticDecoder initial_decoder(initial_bits);
    assert(initial_decoder.code_range() == 510);
    assert(initial_decoder.code_offset() == 171);
    assert(initial_bits.bit_offset() == 9);

    pygorvid::CabacContextModel mps_model(0, 63, 26);
    assert(mps_model.state_index() == 0);
    assert(!mps_model.mps());
    const std::vector<std::uint8_t> regular_data{0b01010101, 0b10000000};
    pygorvid::BitReader regular_bits(regular_data);
    pygorvid::CabacArithmeticDecoder regular_decoder(regular_bits);
    assert(!regular_decoder.decode_bin(mps_model));
    assert(regular_decoder.code_range() == 270);
    assert(regular_decoder.code_offset() == 171);
    assert(mps_model.state_index() == 1);
    assert(!mps_model.mps());

    const std::vector<std::uint8_t> bypass_data{0b01010101, 0b10000000};
    pygorvid::BitReader bypass_bits(bypass_data);
    pygorvid::CabacArithmeticDecoder bypass_decoder(bypass_bits);
    assert(!bypass_decoder.decode_bypass_bin());
    assert(bypass_decoder.code_offset() == 342);
    assert(bypass_decoder.code_range() == 510);
    assert(bypass_decoder.decode_bypass_bin());
    assert(bypass_decoder.code_offset() == 174);

    pygorvid::CabacContextModel lps_model(0, 63, 26);
    const std::vector<std::uint8_t> lps_data{0b11111110, 0b10000000};
    pygorvid::BitReader lps_bits(lps_data);
    pygorvid::CabacArithmeticDecoder lps_decoder(lps_bits);
    assert(lps_decoder.decode_bin(lps_model));
    assert(lps_model.state_index() == 0);
    assert(lps_model.mps());

    bool threw = false;
    const std::vector<std::uint8_t> unterminated_data{0, 0};
    pygorvid::BitReader unterminated_bits(unterminated_data);
    pygorvid::CabacArithmeticDecoder unterminated_decoder(unterminated_bits);
    assert(!unterminated_decoder.decode_terminate_bin());
    assert(unterminated_decoder.code_range() == 508);
    assert(!unterminated_decoder.terminated());

    const std::vector<std::uint8_t> terminated_data{0b11111110, 0b10000000};
    pygorvid::BitReader terminated_bits(terminated_data);
    pygorvid::CabacArithmeticDecoder terminated_decoder(terminated_bits);
    assert(terminated_decoder.decode_terminate_bin());
    assert(terminated_decoder.terminated());
    threw = false;
    try {
        (void)terminated_decoder.decode_bypass_bin();
    } catch (const std::runtime_error&) {
        threw = true;
    }
    assert(threw);

    pygorvid::CabacContextModel transactional_model(0, 0, 26);
    const std::vector<std::uint8_t> transactional_data{
        0b11111110, 0b10000000};
    pygorvid::BitReader transactional_bits(transactional_data);
    pygorvid::CabacArithmeticDecoder transactional_decoder(transactional_bits);
    (void)transactional_bits.read_bits(7);
    const auto original_range = transactional_decoder.code_range();
    const auto original_offset = transactional_decoder.code_offset();
    const auto original_state = transactional_model.state_index();
    const auto original_mps = transactional_model.mps();
    const auto original_bit_offset = transactional_bits.bit_offset();
    threw = false;
    try {
        (void)transactional_decoder.decode_bin(transactional_model);
    } catch (const std::runtime_error&) {
        threw = true;
    }
    assert(threw);
    assert(transactional_decoder.code_range() == original_range);
    assert(transactional_decoder.code_offset() == original_offset);
    assert(transactional_model.state_index() == original_state);
    assert(transactional_model.mps() == original_mps);
    assert(transactional_bits.bit_offset() == original_bit_offset);

    threw = false;
    const std::vector<std::uint8_t> truncated_data{0};
    pygorvid::BitReader truncated_bits(truncated_data);
    try {
        pygorvid::CabacArithmeticDecoder truncated_decoder(truncated_bits);
        (void)truncated_decoder;
    } catch (const std::runtime_error&) {
        threw = true;
    }
    assert(threw);
    assert(truncated_bits.bit_offset() == 0);
}

void append_u32(std::vector<std::uint8_t>& output, std::uint32_t value) {
    output.push_back(static_cast<std::uint8_t>(value >> 24));
    output.push_back(static_cast<std::uint8_t>(value >> 16));
    output.push_back(static_cast<std::uint8_t>(value >> 8));
    output.push_back(static_cast<std::uint8_t>(value));
}

void append_u16(std::vector<std::uint8_t>& output, std::uint16_t value) {
    output.push_back(static_cast<std::uint8_t>(value >> 8));
    output.push_back(static_cast<std::uint8_t>(value));
}

std::vector<std::uint8_t> make_box(
    const std::string& type, const std::vector<std::uint8_t>& payload) {
    std::vector<std::uint8_t> box;
    append_u32(box, static_cast<std::uint32_t>(payload.size() + 8));
    box.insert(box.end(), type.begin(), type.end());
    box.insert(box.end(), payload.begin(), payload.end());
    return box;
}

void append(std::vector<std::uint8_t>& output,
            const std::vector<std::uint8_t>& value) {
    output.insert(output.end(), value.begin(), value.end());
}

class BitWriter {
public:
    void bit(bool value) { bits_.push_back(value ? 1 : 0); }

    void bits(std::uint32_t value, unsigned count) {
        for (unsigned index = count; index > 0; --index) {
            bit(((value >> (index - 1)) & 1U) != 0);
        }
    }

    void ue(std::uint32_t value) {
        const auto code = static_cast<std::uint64_t>(value) + 1;
        unsigned width = 0;
        for (auto remaining = code; remaining != 0; remaining >>= 1) {
            ++width;
        }
        for (unsigned index = 1; index < width; ++index) {
            bit(false);
        }
        for (unsigned index = width; index > 0; --index) {
            bit(((code >> (index - 1)) & 1U) != 0);
        }
    }

    void se(std::int64_t value) {
        const auto code = value <= 0
            ? static_cast<std::uint64_t>(-value) * 2
            : static_cast<std::uint64_t>(value) * 2 - 1;
        assert(code <= std::numeric_limits<std::uint32_t>::max());
        ue(static_cast<std::uint32_t>(code));
    }

    void align_with_ones() {
        while (bits_.size() % 8 != 0) {
            bit(true);
        }
    }

    void align_with_zeros() {
        while (bits_.size() % 8 != 0) {
            bit(false);
        }
    }

    std::vector<std::uint8_t> finish() {
        bit(true);
        while (bits_.size() % 8 != 0) {
            bit(false);
        }
        std::vector<std::uint8_t> bytes(bits_.size() / 8);
        for (std::size_t index = 0; index < bits_.size(); ++index) {
            bytes[index / 8] |= static_cast<std::uint8_t>(
                bits_[index] << (7 - index % 8));
        }
        return bytes;
    }

private:
    std::vector<std::uint8_t> bits_;
};

std::vector<std::uint8_t> make_sps(std::uint8_t profile_idc,
                                   std::uint32_t width_mbs_minus_1,
                                   std::uint32_t height_map_units_minus_1,
                                   std::array<std::uint32_t, 4> crop = {}) {
    BitWriter syntax;
    syntax.ue(0);
    if (profile_idc == 100 || profile_idc == 110 || profile_idc == 122 ||
        profile_idc == 244) {
        syntax.ue(1);
        syntax.ue(0);
        syntax.ue(0);
        syntax.bit(false);
        syntax.bit(false);
    }
    syntax.ue(0);
    syntax.ue(0);
    syntax.ue(0);
    syntax.ue(0);
    syntax.bit(false);
    syntax.ue(width_mbs_minus_1);
    syntax.ue(height_map_units_minus_1);
    syntax.bit(true);
    syntax.bit(true);
    const auto cropping = crop[0] != 0 || crop[1] != 0 || crop[2] != 0 || crop[3] != 0;
    syntax.bit(cropping);
    if (cropping) {
        for (const auto offset : crop) {
            syntax.ue(offset);
        }
    }
    syntax.bit(false);
    std::vector<std::uint8_t> nal{0x67, profile_idc, 0, 40};
    append(nal, syntax.finish());
    return nal;
}

std::vector<std::uint8_t> make_pps(bool entropy, bool bottom_field_poc,
                                   bool weighted_pred = false,
                                   std::uint8_t weighted_bipred_idc = 0,
                                   bool extension = false) {
    BitWriter syntax;
    syntax.ue(0);
    syntax.ue(0);
    syntax.bit(entropy);
    syntax.bit(bottom_field_poc);
    syntax.ue(0);
    syntax.ue(0);
    syntax.ue(0);
    syntax.bit(weighted_pred);
    syntax.bits(weighted_bipred_idc, 2);
    syntax.se(0);
    syntax.se(0);
    syntax.se(0);
    syntax.bit(true);
    syntax.bit(false);
    syntax.bit(false);
    if (extension) {
        syntax.bit(true);
        syntax.bit(false);
        syntax.se(0);
    }
    std::vector<std::uint8_t> nal{0x68};
    append(nal, syntax.finish());
    return nal;
}

std::vector<std::uint8_t> rbsp_to_ebsp(
    const std::vector<std::uint8_t>& rbsp);
std::vector<std::uint8_t> make_i_pcm_slice(
    std::uint32_t first_macroblock,
    const std::array<std::uint8_t, 256>& luma,
    const std::array<std::uint8_t, 64>& chroma_u,
    const std::array<std::uint8_t, 64>& chroma_v,
    std::uint32_t disable_deblocking = 1);

std::vector<std::uint8_t> make_idr_slice(bool entropy, std::uint32_t frame_num,
                                         std::uint32_t idr_pic_id,
                                         std::uint32_t poc_lsb,
                                         std::int64_t qp_delta,
                                         std::uint32_t disable_deblocking,
                                         std::int64_t alpha_offset = 0,
                                         std::int64_t beta_offset = 0,
                                         bool has_delta_pic_order_bottom = false,
                                         std::int64_t delta_pic_order_bottom = 0) {
    BitWriter syntax;
    syntax.ue(0);
    syntax.ue(2);
    syntax.ue(0);
    syntax.bits(frame_num, 4);
    syntax.ue(idr_pic_id);
    syntax.bits(poc_lsb, 4);
    if (has_delta_pic_order_bottom) {
        syntax.se(delta_pic_order_bottom);
    }
    syntax.bit(true);
    syntax.bit(false);
    syntax.se(qp_delta);
    syntax.ue(disable_deblocking);
    if (disable_deblocking != 1) {
        syntax.se(alpha_offset);
        syntax.se(beta_offset);
    }
    if (entropy) {
        syntax.align_with_ones();
    }
    std::vector<std::uint8_t> nal{0x65};
    append(nal, syntax.finish());
    return nal;
}

std::vector<std::uint8_t> make_p_skip_slice(
    std::uint32_t first_macroblock, std::uint32_t skip_run,
    std::uint32_t frame_num, std::uint32_t poc_lsb) {
    BitWriter syntax;
    syntax.ue(first_macroblock);
    syntax.ue(0);
    syntax.ue(0);
    syntax.bits(frame_num, 4);
    syntax.bits(poc_lsb, 4);
    syntax.bit(false);
    syntax.bit(false);
    syntax.bit(false);
    syntax.se(0);
    syntax.ue(1);
    syntax.ue(skip_run);
    std::vector<std::uint8_t> nal{0x41};
    append(nal, rbsp_to_ebsp(syntax.finish()));
    return nal;
}

std::vector<std::uint8_t> make_p_partition_slice(
    std::uint32_t macroblock_type, std::int64_t second_motion_difference_x) {
    assert(macroblock_type == 1 || macroblock_type == 2);
    BitWriter syntax;
    syntax.ue(0);
    syntax.ue(0);
    syntax.ue(0);
    syntax.bits(1, 4);
    syntax.bits(1, 4);
    syntax.bit(false);
    syntax.bit(false);
    syntax.bit(false);
    syntax.se(0);
    syntax.ue(1);
    syntax.ue(0);
    syntax.ue(macroblock_type);
    syntax.se(0);
    syntax.se(0);
    syntax.se(second_motion_difference_x);
    syntax.se(0);
    syntax.ue(0);
    syntax.ue(3);
    std::vector<std::uint8_t> nal{0x41};
    append(nal, rbsp_to_ebsp(syntax.finish()));
    return nal;
}

std::vector<std::uint8_t> make_p_l0_16x16_slice(
    std::int64_t motion_difference_x = 0,
    std::uint32_t coded_block_pattern_code = 0,
    std::int64_t macroblock_qp_delta = 0,
    const std::array<std::string, 4>& luma_residual_bits = {
        "1", "1", "1", "1"},
    const std::array<std::string, 2>& chroma_dc_bits = {"01", "01"},
    const std::array<std::array<std::string, 4>, 2>* chroma_ac_bits =
        nullptr,
    std::int64_t motion_difference_y = 0) {
    BitWriter syntax;
    syntax.ue(0);
    syntax.ue(0);
    syntax.ue(0);
    syntax.bits(1, 4);
    syntax.bits(1, 4);
    syntax.bit(false);
    syntax.bit(false);
    syntax.bit(false);
    syntax.se(0);
    syntax.ue(1);
    syntax.ue(0);
    syntax.ue(0);
    syntax.se(motion_difference_x);
    syntax.se(motion_difference_y);
    syntax.ue(coded_block_pattern_code);
    if (coded_block_pattern_code != 0) {
        static constexpr std::array<std::uint8_t, 48> inter_cbp{{
            0, 16, 1, 2, 4, 8, 32, 3, 5, 10, 12, 15, 47, 7, 11, 13,
            14, 6, 9, 31, 35, 37, 42, 44, 33, 34, 36, 40, 39, 43, 45,
            46, 17, 18, 20, 24, 19, 21, 26, 28, 23, 27, 29, 30, 22,
            25, 38, 41,
        }};
        assert(coded_block_pattern_code < inter_cbp.size());
        const auto pattern = inter_cbp[coded_block_pattern_code];
        const auto luma_cbp = pattern & 15U;
        const auto chroma_cbp = (pattern >> 4) & 3U;
        syntax.se(macroblock_qp_delta);
        for (std::uint32_t block_index = 0; block_index < 16;
             ++block_index) {
            if ((luma_cbp & (1U << (block_index / 4))) != 0) {
                const auto& bits = luma_residual_bits[block_index % 4];
                for (const auto bit : bits) {
                    syntax.bit(bit == '1');
                }
            }
        }
        if (chroma_cbp != 0) {
            for (unsigned plane_index = 0; plane_index < 2; ++plane_index) {
                for (const auto bit : chroma_dc_bits[plane_index]) {
                    syntax.bit(bit == '1');
                }
            }
        }
        if (chroma_cbp == 2) {
            for (std::size_t plane_index = 0; plane_index < 2;
                 ++plane_index) {
                for (std::size_t block_index = 0; block_index < 4;
                     ++block_index) {
                    const auto bits = chroma_ac_bits == nullptr
                        ? std::string("1")
                        : (*chroma_ac_bits)[plane_index][block_index];
                    for (const auto bit : bits) {
                        syntax.bit(bit == '1');
                    }
                }
            }
        }
    }
    std::vector<std::uint8_t> nal{0x41};
    append(nal, rbsp_to_ebsp(syntax.finish()));
    return nal;
}

std::vector<std::uint8_t> make_p_l0_then_skip_slice(
    const std::array<std::int64_t, 3>& motion_differences_x) {
    BitWriter syntax;
    syntax.ue(0);
    syntax.ue(0);
    syntax.ue(0);
    syntax.bits(1, 4);
    syntax.bits(1, 4);
    syntax.bit(false);
    syntax.bit(false);
    syntax.bit(false);
    syntax.se(0);
    syntax.ue(1);
    for (const auto motion_difference_x : motion_differences_x) {
        syntax.ue(0);
        syntax.ue(0);
        syntax.se(motion_difference_x);
        syntax.se(0);
        syntax.ue(0);
    }
    syntax.ue(1);
    std::vector<std::uint8_t> nal{0x41};
    append(nal, rbsp_to_ebsp(syntax.finish()));
    return nal;
}

void test_h264_nal_utilities() {
    const auto parsed_header = pygorvid::parse_nal_header({0x65});
    assert(parsed_header.reference_idc == 3 && parsed_header.unit_type == 5);
    bool empty_rejected = false;
    try {
        (void)pygorvid::parse_nal_header({});
    } catch (const std::invalid_argument&) {
        empty_rejected = true;
    }
    assert(empty_rejected);

    assert((pygorvid::ebsp_to_rbsp({0, 0, 3, 0}) ==
            std::vector<std::uint8_t>{0, 0, 0}));
    bool bad_escape_rejected = false;
    try {
        (void)pygorvid::ebsp_to_rbsp({0, 0, 3});
    } catch (const std::runtime_error&) {
        bad_escape_rejected = true;
    }
    assert(bad_escape_rejected);

    for (const auto length_size : {1U, 2U, 4U}) {
        std::vector<std::uint8_t> sample;
        for (auto index = length_size; index > 0; --index) {
            sample.push_back(static_cast<std::uint8_t>(
                2U >> ((index - 1) * 8)));
        }
        sample.insert(sample.end(), {0x65, 0xaa});
        const auto nals = pygorvid::split_length_prefixed_nals(sample, length_size);
        assert(nals.size() == 1);
        assert((nals[0] == std::vector<std::uint8_t>{0x65, 0xaa}));
    }

    const std::vector<std::uint8_t> malformed_length{0, 0, 0, 2, 0x65};
    bool truncated_rejected = false;
    try {
        (void)pygorvid::split_length_prefixed_nals(malformed_length, 4);
    } catch (const std::runtime_error&) {
        truncated_rejected = true;
    }
    assert(truncated_rejected);

    const std::vector<std::uint8_t> slice_with_marker{
        0, 0, 0, 6, 0x65, 0x80, 0, 0, 1, 0x0a};
    const auto recovered = pygorvid::split_h264_sample_nals(slice_with_marker, 4);
    assert(recovered.size() == 2);
    assert((recovered[0] == std::vector<std::uint8_t>{0x65, 0x80}));
    assert((recovered[1] == std::vector<std::uint8_t>{0x0a}));
}

void test_h264_parameter_sets() {
    const auto sps = pygorvid::parse_sps(make_sps(100, 119, 67, {0, 0, 0, 4}));
    assert(sps.profile_idc == 100);
    assert(sps.chroma_format_idc == 1);
    assert(sps.bit_depth_luma == 8);
    assert(sps.bit_depth_chroma == 8);
    assert(sps.frame_mbs_only);
    assert(sps.direct_8x8_inference);
    assert(sps.coded_width == 1920 && sps.coded_height == 1088);
    assert(sps.width == 1920 && sps.height == 1080);
    assert(sps.frame_crop_bottom == 8);

    const auto baseline = pygorvid::parse_sps(make_sps(66, 0, 0));
    assert(baseline.chroma_format_idc == 1);
    assert(!baseline.seq_intra_luma_4x4_scaling_list_present);
    assert(baseline.width == 16 && baseline.height == 16);

    auto invalid_profile = make_sps(100, 0, 0);
    invalid_profile[1] = 118;
    bool profile_rejected = false;
    try {
        (void)pygorvid::parse_sps(invalid_profile);
    } catch (const std::runtime_error&) {
        profile_rejected = true;
    }
    assert(profile_rejected);

    const auto pps_nal = make_pps(true, false, true, 1, true);
    const auto pps = pygorvid::parse_pps(pps_nal);
    assert(pps.entropy_coding_mode);
    assert(!pps.bottom_field_pic_order_in_frame_present);
    assert(pps.weighted_pred && pps.weighted_bipred_idc == 1);
    assert(pps.has_extension && pps.transform_8x8_mode);
    assert(!pps.pic_scaling_matrix_present);
    assert(pps.second_chroma_qp_index_offset == 0);

    auto invalid_pps = pps_nal;
    invalid_pps[0] = 0x67;
    bool wrong_type_rejected = false;
    try {
        (void)pygorvid::parse_pps(invalid_pps);
    } catch (const std::runtime_error&) {
        wrong_type_rejected = true;
    }
    assert(wrong_type_rejected);

    std::ifstream fixture(std::filesystem::path(PYGORVID_SOURCE_DIR) /
                          "testdata/h264/rannirune-eos.hex");
    assert(fixture.good());
    std::vector<std::vector<std::uint8_t>> nals;
    std::string hex;
    while (fixture >> hex) {
        std::vector<std::uint8_t> nal;
        for (std::size_t offset = 0; offset < hex.size(); offset += 2) {
            nal.push_back(static_cast<std::uint8_t>(
                std::stoul(hex.substr(offset, 2), nullptr, 16)));
        }
        nals.push_back(std::move(nal));
    }
    assert(nals.size() == 3);
    const auto fixture_sps = pygorvid::parse_sps(nals[0]);
    const auto fixture_pps = pygorvid::parse_pps(nals[1]);
    assert(fixture_sps.profile_idc == 100);
    assert(fixture_sps.width > 0 && fixture_sps.height > 0);
    assert(fixture_pps.entropy_coding_mode);
    assert(fixture_pps.sequence_parameter_set_id == fixture_sps.sps_id);
}

void test_h264_idr_slice_header() {
    const auto sps = pygorvid::parse_sps(make_sps(66, 0, 0));
    const auto pps_nal = make_pps(false, false);
    const auto pps = pygorvid::parse_pps(pps_nal);
    const auto slice_nal = make_idr_slice(false, 5, 2, 9, -3, 0, -2, 4);
    const auto header = pygorvid::parse_idr_i_slice_header(slice_nal, sps, pps);
    assert(header.first_macroblock_in_slice == 0);
    assert(header.picture_parameter_set_id == 0);
    assert(header.frame_num == 5);
    assert(header.idr_pic_id == 2);
    assert(header.pic_order_cnt_lsb == 9);
    assert(header.no_output_of_prior_pics);
    assert(!header.long_term_reference);
    assert(header.slice_qp_delta == -3);
    assert(header.disable_deblocking_filter_idc == 0);
    assert(header.slice_alpha_c0_offset_div2 == -2);
    assert(header.slice_beta_offset_div2 == 4);
    assert(header.slice_data_bit_offset > 0);

    const auto bottom_field_pps = pygorvid::parse_pps(make_pps(false, true));
    const auto bottom_field_slice =
        make_idr_slice(false, 0, 0, 0, 0, 1, 0, 0, true, 3);
    const auto bottom_field_header = pygorvid::parse_idr_i_slice_header(
        bottom_field_slice, sps, bottom_field_pps);
    assert(bottom_field_header.delta_pic_order_bottom == 3);

    const auto cabac_pps = pygorvid::parse_pps(make_pps(true, false));
    const auto cabac_slice = make_idr_slice(true, 1, 0, 2, 0, 1);
    const auto cabac_header =
        pygorvid::parse_idr_i_slice_header(cabac_slice, sps, cabac_pps);
    assert(cabac_header.slice_data_bit_offset % 8 == 0);
    assert(cabac_header.disable_deblocking_filter_idc == 1);

    const auto non_idr = std::vector<std::uint8_t>{0x41, 0x80};
    bool non_idr_rejected = false;
    try {
        (void)pygorvid::parse_idr_i_slice_header(non_idr, sps, pps);
    } catch (const std::runtime_error&) {
        non_idr_rejected = true;
    }
    assert(non_idr_rejected);

    BitWriter p_slice_syntax;
    p_slice_syntax.ue(0);
    p_slice_syntax.ue(0);
    p_slice_syntax.ue(0);
    std::vector<std::uint8_t> p_slice_nal{0x65};
    append(p_slice_nal, p_slice_syntax.finish());
    bool inter_slice_rejected = false;
    try {
        (void)pygorvid::parse_idr_i_slice_header(p_slice_nal, sps, pps);
    } catch (const std::runtime_error&) {
        inter_slice_rejected = true;
    }
    assert(inter_slice_rejected);
}

void test_h264_p_skip_picture() {
    const auto sps = pygorvid::parse_sps(make_sps(66, 1, 0));
    const auto pps = pygorvid::parse_pps(make_pps(false, false));
    const auto p_slice = make_p_skip_slice(0, 2, 1, 1);
    const auto header = pygorvid::parse_p_slice_header(p_slice, sps, pps);
    assert(header.first_macroblock_in_slice == 0);
    assert(header.picture_parameter_set_id == 0);
    assert(header.frame_num == 1);
    assert(header.pic_order_cnt_lsb == 1);
    assert(header.disable_deblocking_filter_idc == 1);
    assert(header.slice_data_bit_offset > 0);

    std::array<std::uint8_t, 256> first_luma{};
    std::array<std::uint8_t, 256> second_luma{};
    std::array<std::uint8_t, 64> chroma_u{};
    std::array<std::uint8_t, 64> chroma_v{};
    first_luma.fill(51);
    second_luma.fill(147);
    chroma_u.fill(89);
    chroma_v.fill(201);
    const auto reference = pygorvid::decode_idr_i_pcm_frame(
        {make_i_pcm_slice(0, first_luma, chroma_u, chroma_v),
         make_i_pcm_slice(1, second_luma, chroma_u, chroma_v)},
        sps, pps);
    const auto decoded = pygorvid::decode_h264_reference_frame(
        {p_slice}, sps, pps, &reference);
    assert(decoded.width == reference.width);
    assert(decoded.height == reference.height);
    assert(decoded.luma == reference.luma);
    assert(decoded.chroma_u == reference.chroma_u);
    assert(decoded.chroma_v == reference.chroma_v);

    bool missing_reference_rejected = false;
    try {
        (void)pygorvid::decode_h264_reference_frame({p_slice}, sps, pps);
    } catch (const std::invalid_argument&) {
        missing_reference_rejected = true;
    }
    assert(missing_reference_rejected);

    bool zero_skip_run_rejected = false;
    try {
        (void)pygorvid::decode_p_skip_frame(
            {make_p_skip_slice(0, 0, 1, 1)}, sps, pps, reference);
    } catch (const std::runtime_error&) {
        zero_skip_run_rejected = true;
    }
    assert(zero_skip_run_rejected);

    bool incomplete_picture_rejected = false;
    try {
        (void)pygorvid::decode_p_skip_frame(
            {make_p_skip_slice(0, 1, 1, 1)}, sps, pps, reference);
    } catch (const std::runtime_error&) {
        incomplete_picture_rejected = true;
    }
    assert(incomplete_picture_rejected);

    const auto single_macroblock_sps =
        pygorvid::parse_sps(make_sps(66, 0, 0));
    std::array<std::uint8_t, 256> single_luma{};
    std::array<std::uint8_t, 64> single_u{};
    std::array<std::uint8_t, 64> single_v{};
    for (std::size_t index = 0; index < single_luma.size(); ++index) {
        single_luma[index] = static_cast<std::uint8_t>(index);
    }
    for (std::size_t index = 0; index < single_u.size(); ++index) {
        single_u[index] = static_cast<std::uint8_t>(
            (index / 8) * 4 + (index % 8) * 32);
        single_v[index] = static_cast<std::uint8_t>(255 - single_u[index]);
    }
    const auto single_reference = pygorvid::decode_idr_i_pcm_frame(
        {make_i_pcm_slice(0, single_luma, single_u, single_v)},
        single_macroblock_sps, pps);
    const auto coded_p_picture = pygorvid::decode_p_frame(
        {make_p_l0_16x16_slice()}, single_macroblock_sps, pps,
        single_reference);
    assert(coded_p_picture.luma == single_reference.luma);
    assert(coded_p_picture.chroma_u == single_reference.chroma_u);
    assert(coded_p_picture.chroma_v == single_reference.chroma_v);

    const auto shifted_p_picture = pygorvid::decode_p_frame(
        {make_p_l0_16x16_slice(8)}, single_macroblock_sps, pps,
        single_reference);
    for (std::uint32_t row = 0; row < 16; ++row) {
        for (std::uint32_t column = 0; column < 16; ++column) {
            const auto source_column = std::min(column + 2, 15U);
            assert(shifted_p_picture.luma[row * 16 + column] ==
                   single_reference.luma[row * 16 + source_column]);
        }
    }
    for (std::uint32_t row = 0; row < 8; ++row) {
        for (std::uint32_t column = 0; column < 8; ++column) {
            const auto source_column = std::min(column + 1, 7U);
            assert(shifted_p_picture.chroma_u[row * 8 + column] ==
                   single_reference.chroma_u[row * 8 + source_column]);
            assert(shifted_p_picture.chroma_v[row * 8 + column] ==
                   single_reference.chroma_v[row * 8 + source_column]);
        }
    }

    const auto p_luma_residual = pygorvid::decode_p_frame(
        {make_p_l0_16x16_slice(
            0, 2, 0, {"0101", "1", "1", "1"})},
        single_macroblock_sps, pps, single_reference);
    for (std::uint32_t row = 0; row < 16; ++row) {
        for (std::uint32_t column = 0; column < 16; ++column) {
            const auto expected_delta =
                row < 4 && column < 4 ? 3 : 0;
            assert(p_luma_residual.luma[row * 16 + column] ==
                   single_reference.luma[row * 16 + column] +
                       expected_delta);
        }
    }
    const auto p_luma_residual_qp = pygorvid::decode_p_frame(
        {make_p_l0_16x16_slice(
            0, 2, 6, {"0101", "1", "1", "1"})},
        single_macroblock_sps, pps, single_reference);
    assert(p_luma_residual_qp.luma[0] >
           p_luma_residual.luma[0]);

    const auto negative_shift_p_picture = pygorvid::decode_p_frame(
        {make_p_l0_16x16_slice(-8)}, single_macroblock_sps, pps,
        single_reference);
    for (std::uint32_t row = 0; row < 16; ++row) {
        for (std::uint32_t column = 0; column < 16; ++column) {
            const auto source_column = column < 2 ? 0U : column - 2;
            assert(negative_shift_p_picture.luma[row * 16 + column] ==
                   single_reference.luma[row * 16 + source_column]);
        }
    }

    const auto quarter_sample_p_picture = pygorvid::decode_p_frame(
        {make_p_l0_16x16_slice(1)}, single_macroblock_sps, pps,
        single_reference);
    assert(quarter_sample_p_picture.luma[8 * 16 + 8] == 137);
    assert(quarter_sample_p_picture.chroma_u[2] == 68);
    assert(quarter_sample_p_picture.chroma_v[2] == 187);

    const auto negative_quarter_sample_p_picture =
        pygorvid::decode_p_frame(
            {make_p_l0_16x16_slice(-1)}, single_macroblock_sps, pps,
            single_reference);
    assert(negative_quarter_sample_p_picture.luma[8 * 16 + 8] == 136);
    assert(negative_quarter_sample_p_picture.chroma_u[2] == 60);
    assert(negative_quarter_sample_p_picture.chroma_v[2] == 195);

    const auto diagonal_quarter_sample_p_picture =
        pygorvid::decode_p_frame(
            {make_p_l0_16x16_slice(1, 0, 0, {"1", "1", "1", "1"},
                                   {"01", "01"}, nullptr, 1)},
            single_macroblock_sps, pps, single_reference);
    assert(diagonal_quarter_sample_p_picture.luma[8 * 16 + 8] == 141);
    assert(diagonal_quarter_sample_p_picture.chroma_u[2 * 8 + 2] == 77);

    const auto diagonal_half_sample_p_picture =
        pygorvid::decode_p_frame(
            {make_p_l0_16x16_slice(2, 0, 0, {"1", "1", "1", "1"},
                                   {"01", "01"}, nullptr, 2)},
            single_macroblock_sps, pps, single_reference);
    assert(diagonal_half_sample_p_picture.luma[8 * 16 + 8] == 145);

    const auto vertical_quarter_sample_p_picture =
        pygorvid::decode_p_frame(
            {make_p_l0_16x16_slice(0, 0, 0, {"1", "1", "1", "1"},
                                   {"01", "01"}, nullptr, 1)},
            single_macroblock_sps, pps, single_reference);
    assert(vertical_quarter_sample_p_picture.luma[8 * 16 + 8] == 140);
    assert(vertical_quarter_sample_p_picture.chroma_u[2 * 8 + 2] == 73);

    const auto fractional_boundary_p_picture = pygorvid::decode_p_frame(
        {make_p_l0_16x16_slice(-1)}, single_macroblock_sps, pps,
        single_reference);
    assert(fractional_boundary_p_picture.luma[0] == 0);
    assert(fractional_boundary_p_picture.chroma_u[0] == 0);
    assert(fractional_boundary_p_picture.chroma_v[0] == 255);

    std::array<std::uint8_t, 256> macroblock_luma{};
    std::array<std::uint8_t, 64> macroblock_u{};
    std::array<std::uint8_t, 64> macroblock_v{};
    for (std::size_t index = 0; index < macroblock_luma.size(); ++index) {
        macroblock_luma[index] = static_cast<std::uint8_t>(index);
    }
    macroblock_u.fill(80);
    macroblock_v.fill(180);
    const auto four_macroblock_sps =
        pygorvid::parse_sps(make_sps(66, 1, 1));
    const auto four_macroblock_reference =
        pygorvid::decode_idr_i_pcm_frame(
            {make_i_pcm_slice(0, macroblock_luma, macroblock_u, macroblock_v),
             make_i_pcm_slice(1, macroblock_luma, macroblock_u, macroblock_v),
             make_i_pcm_slice(2, macroblock_luma, macroblock_u, macroblock_v),
             make_i_pcm_slice(3, macroblock_luma, macroblock_u, macroblock_v)},
            four_macroblock_sps, pps);
    const auto p_l0_16x8 = pygorvid::decode_p_frame(
        {make_p_partition_slice(1, 4)}, four_macroblock_sps, pps,
        four_macroblock_reference);
    assert(p_l0_16x8.luma[4 * 32 + 8] ==
           four_macroblock_reference.luma[4 * 32 + 8]);
    assert(p_l0_16x8.luma[12 * 32 + 8] ==
           four_macroblock_reference.luma[12 * 32 + 9]);
    const auto p_l0_8x16 = pygorvid::decode_p_frame(
        {make_p_partition_slice(2, 4)}, four_macroblock_sps, pps,
        four_macroblock_reference);
    assert(p_l0_8x16.luma[8 * 32 + 4] ==
           four_macroblock_reference.luma[8 * 32 + 4]);
    assert(p_l0_8x16.luma[8 * 32 + 12] ==
           four_macroblock_reference.luma[8 * 32 + 13]);
    const auto inter_with_skip = pygorvid::decode_p_frame(
        {make_p_l0_then_skip_slice({8, 0, 0})}, four_macroblock_sps, pps,
        four_macroblock_reference);
    for (std::uint32_t row = 0; row < 32; ++row) {
        for (std::uint32_t column = 0; column < 32; ++column) {
            const auto source_column = std::min(column + 2, 31U);
            assert(inter_with_skip.luma[row * 32 + column] ==
                   four_macroblock_reference
                       .luma[row * 32 + source_column]);
        }
    }

    const auto p_chroma_dc = pygorvid::decode_p_frame(
        {make_p_l0_16x16_slice(0, 1)}, single_macroblock_sps, pps,
        single_reference);
    assert(p_chroma_dc.luma == single_reference.luma);
    assert(p_chroma_dc.chroma_u == single_reference.chroma_u);
    assert(p_chroma_dc.chroma_v == single_reference.chroma_v);

    std::array<std::array<std::string, 4>, 2> p_chroma_ac_bits{};
    for (auto& plane_bits : p_chroma_ac_bits) {
        plane_bits.fill("1");
    }
    p_chroma_ac_bits[0][0] = "0101";
    const auto p_chroma_ac = pygorvid::decode_p_frame(
        {make_p_l0_16x16_slice(
            0, 6, 0, {"1", "1", "1", "1"}, {"01", "01"},
            &p_chroma_ac_bits)},
        single_macroblock_sps, pps, single_reference);
    assert(p_chroma_ac.luma == single_reference.luma);
    assert(p_chroma_ac.chroma_u != single_reference.chroma_u);
    assert(p_chroma_ac.chroma_v == single_reference.chroma_v);
}

void test_png_encoding() {
    pygorvid::Yuv420Frame frame;
    frame.width = 2;
    frame.height = 2;
    frame.luma = {16, 235, 81, 145};
    frame.chroma_u = {128};
    frame.chroma_v = {128};
    const auto png = pygorvid::encode_png(frame);
    assert((std::vector<std::uint8_t>(png.begin(), png.begin() + 8) ==
            std::vector<std::uint8_t>{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a,
                                      0x0a}));
    assert(png[8] == 0 && png[9] == 0 && png[10] == 0 && png[11] == 13);
    assert(std::string(png.begin() + 12, png.begin() + 16) == "IHDR");
    assert(png[16] == 0 && png[17] == 0 && png[18] == 0 && png[19] == 2);
    assert(png[20] == 0 && png[21] == 0 && png[22] == 0 && png[23] == 2);
    const auto idat_length =
        (static_cast<std::uint32_t>(png[33]) << 24) |
        (static_cast<std::uint32_t>(png[34]) << 16) |
        (static_cast<std::uint32_t>(png[35]) << 8) |
        static_cast<std::uint32_t>(png[36]);
    assert(std::string(png.begin() + 37, png.begin() + 41) == "IDAT");
    assert(idat_length == 25);
    const auto raw_begin = static_cast<std::size_t>(48);
    assert(png[43] == 1);
    assert(png[raw_begin] == 0);
    assert(png[raw_begin + 1] == 0 && png[raw_begin + 2] == 0 &&
           png[raw_begin + 3] == 0);
    assert(png[raw_begin + 4] == 255 && png[raw_begin + 5] == 255 &&
           png[raw_begin + 6] == 255);
    assert(png[raw_begin + 7] == 0);
    assert(png[raw_begin + 8] == 76 && png[raw_begin + 9] == 76 &&
           png[raw_begin + 10] == 76);
    assert(png[raw_begin + 11] == 150 && png[raw_begin + 12] == 150 &&
           png[raw_begin + 13] == 150);
    pygorvid::Yuv420Frame color_frame;
    color_frame.width = 2;
    color_frame.height = 2;
    color_frame.luma = {16, 16, 16, 16};
    color_frame.chroma_u = {0};
    color_frame.chroma_v = {2};
    const auto color_png = pygorvid::encode_png(color_frame);
    assert(color_png[raw_begin + 1] == 0);
    assert(color_png[raw_begin + 2] == 153);
    assert(color_png[raw_begin + 3] == 0);
    assert(std::string(png.end() - 8, png.end() - 4) == "IEND");
    const auto read_be32 = [&png](std::size_t offset) {
        return (static_cast<std::uint32_t>(png[offset]) << 24) |
               (static_cast<std::uint32_t>(png[offset + 1]) << 16) |
               (static_cast<std::uint32_t>(png[offset + 2]) << 8) |
               static_cast<std::uint32_t>(png[offset + 3]);
    };
    const auto compute_crc = [&png](std::size_t begin, std::size_t end) {
        std::uint32_t crc = 0xffffffffU;
        for (auto index = begin; index < end; ++index) {
            crc ^= png[index];
            for (unsigned bit = 0; bit < 8; ++bit) {
                crc = (crc >> 1) ^
                      ((crc & 1U) != 0 ? 0xedb88320U : 0U);
            }
        }
        return crc ^ 0xffffffffU;
    };
    for (std::size_t chunk_offset = 8; chunk_offset < png.size();) {
        const auto chunk_size = read_be32(chunk_offset);
        const auto type_offset = chunk_offset + 4;
        const auto crc_offset = type_offset + 4 + chunk_size;
        assert(read_be32(crc_offset) == compute_crc(type_offset, crc_offset));
        chunk_offset = crc_offset + 4;
    }
    assert(png[44] == 14 && png[45] == 0);
    assert(png[46] == 0xf1 && png[47] == 0xff);
    std::uint32_t adler_a = 1;
    std::uint32_t adler_b = 0;
    for (std::size_t index = raw_begin; index < raw_begin + 14; ++index) {
        adler_a = (adler_a + png[index]) % 65521;
        adler_b = (adler_b + adler_a) % 65521;
    }
    assert(read_be32(raw_begin + 14) == (adler_b << 16 | adler_a));

    frame.chroma_v.clear();
    bool invalid_planes_rejected = false;
    try {
        (void)pygorvid::encode_png(frame);
    } catch (const std::invalid_argument&) {
        invalid_planes_rejected = true;
    }
    assert(invalid_planes_rejected);
}

void test_jpeg_encoding() {
    std::ifstream fixture(std::filesystem::path(PYGORVID_SOURCE_DIR) /
                          "testdata/jpeg-parity-18x10.hex");
    assert(fixture);
    pygorvid::Yuv420Frame parity_frame;
    std::string luma_hex, u_hex, v_hex, jpeg_hex, part;
    fixture >> parity_frame.width >> parity_frame.height >> luma_hex >> u_hex >> v_hex;
    assert(fixture);
    while (fixture >> part) {
        jpeg_hex += part;
    }
    const auto decode_hex = [](const std::string& hex) {
        assert(hex.size() % 2 == 0);
        std::vector<std::uint8_t> bytes;
        for (std::size_t offset = 0; offset < hex.size(); offset += 2) {
            bytes.push_back(static_cast<std::uint8_t>(
                std::stoul(hex.substr(offset, 2), nullptr, 16)));
        }
        return bytes;
    };
    parity_frame.luma = decode_hex(luma_hex);
    parity_frame.chroma_u = decode_hex(u_hex);
    parity_frame.chroma_v = decode_hex(v_hex);
    assert(pygorvid::encode_jpeg(parity_frame) == decode_hex(jpeg_hex));

    pygorvid::Yuv420Frame frame;
    frame.width = 10;
    frame.height = 2;
    frame.luma.assign(20, 100);
    frame.chroma_u.assign(5, 80);
    frame.chroma_v.assign(5, 180);
    const auto jpeg = pygorvid::encode_jpeg(frame);
    assert(jpeg.size() > 4);
    assert(jpeg[0] == 0xff && jpeg[1] == 0xd8);
    assert(jpeg[jpeg.size() - 2] == 0xff && jpeg.back() == 0xd9);

    frame.chroma_v.clear();
    bool rejected = false;
    try {
        (void)pygorvid::encode_jpeg(frame);
    } catch (const std::invalid_argument&) {
        rejected = true;
    }
    assert(rejected);
    frame.width = 65536;
    rejected = false;
    try {
        (void)pygorvid::encode_jpeg(frame);
    } catch (const std::invalid_argument&) {
        rejected = true;
    }
    assert(rejected);
}

std::vector<std::uint8_t> bits_from_string(const std::string& bits) {
    std::vector<std::uint8_t> data((bits.size() + 7) / 8, 0);
    for (std::size_t index = 0; index < bits.size(); ++index) {
        if (bits[index] == '1') {
            data[index / 8] |= static_cast<std::uint8_t>(1U << (7 - index % 8));
        }
    }
    return data;
}

std::vector<std::uint8_t> rbsp_to_ebsp(
    const std::vector<std::uint8_t>& rbsp) {
    std::vector<std::uint8_t> ebsp;
    ebsp.reserve(rbsp.size());
    unsigned zero_count = 0;
    for (const auto value : rbsp) {
        if (zero_count == 2 && value <= 3) {
            ebsp.push_back(3);
            zero_count = 0;
        }
        ebsp.push_back(value);
        zero_count = value == 0 ? zero_count + 1 : 0;
    }
    return ebsp;
}

std::vector<std::uint8_t> make_i_pcm_slice(
    std::uint32_t first_macroblock,
    const std::array<std::uint8_t, 256>& luma,
    const std::array<std::uint8_t, 64>& chroma_u,
    const std::array<std::uint8_t, 64>& chroma_v,
    std::uint32_t disable_deblocking) {
    BitWriter syntax;
    syntax.ue(first_macroblock);
    syntax.ue(2);
    syntax.ue(0);
    syntax.bits(0, 4);
    syntax.ue(0);
    syntax.bits(0, 4);
    syntax.bit(true);
    syntax.bit(false);
    syntax.se(0);
    syntax.ue(disable_deblocking);
    if (disable_deblocking != 1) {
        syntax.se(0);
        syntax.se(0);
    }
    syntax.ue(25);
    syntax.align_with_zeros();
    for (const auto sample : luma) {
        syntax.bits(sample, 8);
    }
    for (const auto sample : chroma_u) {
        syntax.bits(sample, 8);
    }
    for (const auto sample : chroma_v) {
        syntax.bits(sample, 8);
    }
    std::vector<std::uint8_t> nal{0x65};
    append(nal, rbsp_to_ebsp(syntax.finish()));
    return nal;
}

std::vector<std::uint8_t> make_i4x4_slice(
    std::uint32_t coded_block_pattern_code,
    const std::array<std::string, 16>& luma_residual_bits,
    const std::array<std::string, 2>& chroma_dc_bits = {"01", "01"},
    const std::array<std::array<std::string, 4>, 2>* chroma_ac_bits = nullptr,
    std::int64_t macroblock_qp_delta = 0) {
    static constexpr std::array<std::uint8_t, 48> intra_cbp{{
        47, 31, 15, 0, 23, 27, 29, 30, 7, 11, 13, 14,
        39, 43, 45, 46, 16, 3, 5, 10, 12, 19, 21, 26,
        28, 35, 37, 42, 44, 1, 2, 4, 8, 17, 18, 20,
        24, 6, 9, 22, 25, 32, 33, 34, 36, 40, 38, 41,
    }};
    assert(coded_block_pattern_code < intra_cbp.size());

    BitWriter syntax;
    syntax.ue(0);
    syntax.ue(2);
    syntax.ue(0);
    syntax.bits(0, 4);
    syntax.ue(0);
    syntax.bits(0, 4);
    syntax.bit(true);
    syntax.bit(false);
    syntax.se(0);
    syntax.ue(1);
    syntax.ue(0);
    for (std::size_t block_index = 0; block_index < 16; ++block_index) {
        syntax.bit(true);
    }
    syntax.ue(0);
    syntax.ue(coded_block_pattern_code);

    const auto coded_block_pattern = intra_cbp[coded_block_pattern_code];
    if (coded_block_pattern != 0) {
        syntax.se(macroblock_qp_delta);
    }
    const auto luma_cbp = coded_block_pattern & 0x0f;
    const auto chroma_cbp = (coded_block_pattern >> 4) & 0x03;
    for (std::size_t block_index = 0; block_index < 16; ++block_index) {
        if ((luma_cbp & (1U << (block_index / 4))) == 0) {
            continue;
        }
        for (const auto bit : luma_residual_bits[block_index]) {
            syntax.bit(bit == '1');
        }
    }
    if (chroma_cbp != 0) {
        for (const auto& bits : chroma_dc_bits) {
            for (const auto bit : bits) {
                syntax.bit(bit == '1');
            }
        }
    }
    if (chroma_cbp == 2 && chroma_ac_bits != nullptr) {
        for (const auto& plane : *chroma_ac_bits) {
            for (const auto& bits : plane) {
                for (const auto bit : bits) {
                    syntax.bit(bit == '1');
                }
            }
        }
    } else if (chroma_cbp == 2) {
        for (std::size_t plane_index = 0; plane_index < 2; ++plane_index) {
            for (std::size_t block_index = 0; block_index < 4; ++block_index) {
                syntax.bit(true);
            }
        }
    }
    std::vector<std::uint8_t> nal{0x65};
    append(nal, rbsp_to_ebsp(syntax.finish()));
    return nal;
}

std::vector<std::uint8_t> make_mixed_i_slice(
    const std::array<std::uint8_t, 256>& luma,
    const std::array<std::uint8_t, 64>& chroma_u,
    const std::array<std::uint8_t, 64>& chroma_v,
    const std::array<std::string, 3>& luma_dc_bits = {"1", "1", "1"},
    const std::array<std::int64_t, 3>& macroblock_qp_deltas = {0, 0, 0},
    const std::array<std::uint32_t, 3>& macroblock_types = {2, 1, 4},
    const std::array<std::string, 16>* luma_ac_bits = nullptr,
    const std::array<std::array<std::string, 2>, 3>* chroma_dc_bits = nullptr,
    const std::array<std::array<std::string, 8>, 3>* chroma_ac_bits = nullptr,
    std::uint32_t disable_deblocking = 1) {
    BitWriter syntax;
    syntax.ue(0);
    syntax.ue(2);
    syntax.ue(0);
    syntax.bits(0, 4);
    syntax.ue(0);
    syntax.bits(0, 4);
    syntax.bit(true);
    syntax.bit(false);
    syntax.se(0);
    syntax.ue(disable_deblocking);
    if (disable_deblocking != 1) {
        syntax.se(0);
        syntax.se(0);
    }
    syntax.ue(25);
    syntax.align_with_zeros();
    for (const auto sample : luma) {
        syntax.bits(sample, 8);
    }
    for (const auto sample : chroma_u) {
        syntax.bits(sample, 8);
    }
    for (const auto sample : chroma_v) {
        syntax.bits(sample, 8);
    }
    const std::array<std::uint32_t, 3> chroma_modes{1, 2, 3};
    for (std::size_t index = 0; index < macroblock_types.size(); ++index) {
        syntax.ue(macroblock_types[index]);
        syntax.ue(chroma_modes[index]);
        syntax.se(macroblock_qp_deltas[index]);
        for (const auto bit : luma_dc_bits[index]) {
            syntax.bit(bit == '1');
        }
        const auto chroma_cbp =
            ((macroblock_types[index] - 1) / 4) % 3;
        if (macroblock_types[index] >= 13 &&
            macroblock_types[index] <= 24) {
            for (std::size_t block_index = 0; block_index < 16;
                 ++block_index) {
                const auto block_bits =
                    luma_ac_bits == nullptr ? std::string("1")
                                            : (*luma_ac_bits)[block_index];
                for (const auto bit : block_bits) {
                    syntax.bit(bit == '1');
                }
            }
        }
        if (chroma_cbp != 0) {
            for (std::size_t plane_index = 0; plane_index < 2;
                 ++plane_index) {
                const auto block_bits =
                    chroma_dc_bits == nullptr
                        ? std::string("01")
                        : (*chroma_dc_bits)[index][plane_index];
                for (const auto bit : block_bits) {
                    syntax.bit(bit == '1');
                }
            }
        }
        if (chroma_cbp == 2) {
            for (std::size_t block_index = 0; block_index < 8;
                 ++block_index) {
                const auto block_bits =
                    chroma_ac_bits == nullptr
                        ? std::string("1")
                        : (*chroma_ac_bits)[index][block_index];
                for (const auto bit : block_bits) {
                    syntax.bit(bit == '1');
                }
            }
        }
    }
    std::vector<std::uint8_t> nal{0x65};
    append(nal, rbsp_to_ebsp(syntax.finish()));
    return nal;
}

std::vector<std::uint8_t> make_i4x4_prediction_slice(
    const std::array<std::uint8_t, 256>& pcm_luma,
    const std::array<std::uint8_t, 64>& pcm_chroma_u,
    const std::array<std::uint8_t, 64>& pcm_chroma_v,
    const std::array<std::array<std::uint8_t, 16>, 3>& requested_modes) {
    BitWriter syntax;
    syntax.ue(0);
    syntax.ue(2);
    syntax.ue(0);
    syntax.bits(0, 4);
    syntax.ue(0);
    syntax.bits(0, 4);
    syntax.bit(true);
    syntax.bit(false);
    syntax.se(0);
    syntax.ue(1);
    syntax.ue(25);
    syntax.align_with_zeros();
    for (const auto sample : pcm_luma) {
        syntax.bits(sample, 8);
    }
    for (const auto sample : pcm_chroma_u) {
        syntax.bits(sample, 8);
    }
    for (const auto sample : pcm_chroma_v) {
        syntax.bits(sample, 8);
    }

    constexpr std::size_t macroblock_width = 2;
    constexpr std::size_t block_width = macroblock_width * 4;
    static constexpr std::array<std::array<std::uint32_t, 2>, 16>
        block_positions{{
            {{0, 0}}, {{1, 0}}, {{0, 1}}, {{1, 1}},
            {{2, 0}}, {{3, 0}}, {{2, 1}}, {{3, 1}},
            {{0, 2}}, {{1, 2}}, {{0, 3}}, {{1, 3}},
            {{2, 2}}, {{3, 2}}, {{2, 3}}, {{3, 3}},
        }};
    std::array<std::uint8_t, block_width * 8> decoded_modes{};
    std::array<bool, block_width * 8> mode_available{};
    for (std::size_t block = 0; block < 16; ++block) {
        const auto position = block_positions[block];
        const auto index = static_cast<std::size_t>(position[1]) * block_width +
                           position[0];
        decoded_modes[index] = 2;
        mode_available[index] = true;
    }
    for (std::size_t macroblock = 1; macroblock < 4; ++macroblock) {
        syntax.ue(0);
        const auto macroblock_x = macroblock % macroblock_width;
        const auto macroblock_y = macroblock / macroblock_width;
        for (std::size_t block = 0; block < 16; ++block) {
            const auto position = block_positions[block];
            const auto block_x = macroblock_x * 4 + position[0];
            const auto block_y = macroblock_y * 4 + position[1];
            const auto block_index = block_y * block_width + block_x;
            const auto left_mode = block_x > 0 &&
                    mode_available[block_y * block_width + block_x - 1]
                ? decoded_modes[block_y * block_width + block_x - 1]
                : 2;
            const auto top_mode = block_y > 0 &&
                    mode_available[(block_y - 1) * block_width + block_x]
                ? decoded_modes[(block_y - 1) * block_width + block_x]
                : 2;
            const auto left_available =
                block_x > 0 &&
                mode_available[block_y * block_width + block_x - 1];
            const auto top_available =
                block_y > 0 &&
                mode_available[(block_y - 1) * block_width + block_x];
            const auto predicted_mode =
                left_available && top_available
                    ? std::min(left_mode, top_mode)
                    : 2;
            const auto mode = requested_modes[macroblock - 1][block];
            if (mode == predicted_mode) {
                syntax.bit(true);
            } else {
                syntax.bit(false);
                syntax.bits(mode < predicted_mode ? mode : mode - 1, 3);
            }
            decoded_modes[block_index] = mode;
            mode_available[block_index] = true;
        }
        syntax.ue(0);
        syntax.ue(3);
    }
    std::vector<std::uint8_t> nal{0x65};
    append(nal, rbsp_to_ebsp(syntax.finish()));
    return nal;
}

void test_h264_i4x4_prediction_modes() {
    const auto sps = pygorvid::parse_sps(make_sps(66, 1, 1));
    const auto pps = pygorvid::parse_pps(make_pps(false, false));
    std::array<std::uint8_t, 256> pcm_luma{};
    std::array<std::uint8_t, 64> pcm_chroma_u{};
    std::array<std::uint8_t, 64> pcm_chroma_v{};
    for (std::size_t index = 0; index < pcm_luma.size(); ++index) {
        pcm_luma[index] = static_cast<std::uint8_t>(20 + (index * 7) % 180);
    }
    pcm_chroma_u.fill(80);
    pcm_chroma_v.fill(180);

    for (std::uint8_t mode = 0; mode <= 8; ++mode) {
        std::array<std::array<std::uint8_t, 16>, 3> modes{};
        for (auto& macroblock_modes : modes) {
            macroblock_modes.fill(2);
        }
        modes[2][3] = mode;
        const auto slice = make_i4x4_prediction_slice(
            pcm_luma, pcm_chroma_u, pcm_chroma_v, modes);
        const auto frame = pygorvid::decode_idr_i_frame({slice}, sps, pps);
        const auto sample_x = 20U;
        const auto sample_y = 20U;
        const auto top = [&](std::size_t index) {
            return frame.luma[static_cast<std::size_t>(sample_y - 1) *
                                  frame.width +
                              sample_x + index];
        };
        const auto left = [&](std::size_t index) {
            return frame.luma[static_cast<std::size_t>(sample_y + index) *
                                  frame.width +
                              sample_x - 1];
        };
        const auto corner = frame.luma[
            static_cast<std::size_t>(sample_y - 1) * frame.width + sample_x - 1];
        std::uint8_t expected = 0;
        if (mode == 0) {
            expected = top(0);
        } else if (mode == 1) {
            expected = left(0);
        } else if (mode == 2) {
            std::uint32_t sum = 4;
            for (std::size_t index = 0; index < 4; ++index) {
                sum += top(index) + left(index);
            }
            expected = static_cast<std::uint8_t>(sum >> 3);
        } else if (mode == 3) {
            expected = static_cast<std::uint8_t>(
                (top(0) + 2U * top(1) + top(2) + 2) >> 2);
        } else if (mode == 4) {
            expected = static_cast<std::uint8_t>(
                (top(0) + 2U * corner + left(0) + 2) >> 2);
        } else if (mode == 5) {
            expected = static_cast<std::uint8_t>((corner + top(0) + 1) >> 1);
        } else if (mode == 6) {
            expected = static_cast<std::uint8_t>((corner + left(0) + 1) >> 1);
        } else if (mode == 7) {
            expected = static_cast<std::uint8_t>((top(0) + top(1) + 1) >> 1);
        } else {
            expected = static_cast<std::uint8_t>((left(0) + left(1) + 1) >> 1);
        }
        assert(frame.luma[static_cast<std::size_t>(sample_y) * frame.width +
                          sample_x] == expected);
        if (mode == 8) {
            const auto horizontal_up_row_0_column_1 =
                static_cast<std::uint8_t>(
                    (left(0) + 2U * left(1) + left(2) + 2) >> 2);
            const auto horizontal_up_row_1_column_0 =
                static_cast<std::uint8_t>((left(1) + left(2) + 1) >> 1);
            assert(frame.luma[static_cast<std::size_t>(sample_y) *
                                  frame.width +
                              sample_x + 1] ==
                   horizontal_up_row_0_column_1);
            assert(frame.luma[static_cast<std::size_t>(sample_y + 1) *
                                  frame.width +
                              sample_x] ==
                   horizontal_up_row_1_column_0);
        }
    }
}

void test_h264_i_pcm_frame_reconstruction() {
    const auto sps = pygorvid::parse_sps(make_sps(66, 1, 0));
    const auto pps = pygorvid::parse_pps(make_pps(false, false));

    std::array<std::uint8_t, 256> first_luma{};
    std::array<std::uint8_t, 256> second_luma{};
    std::array<std::uint8_t, 64> first_u{};
    std::array<std::uint8_t, 64> first_v{};
    std::array<std::uint8_t, 64> second_u{};
    std::array<std::uint8_t, 64> second_v{};
    for (std::size_t index = 0; index < first_luma.size(); ++index) {
        first_luma[index] = static_cast<std::uint8_t>(20 + index % 160);
        second_luma[index] = static_cast<std::uint8_t>(90 + index % 100);
    }
    first_luma[20] = 0;
    first_u.fill(70);
    first_v.fill(180);
    first_u[0] = 0;
    second_u.fill(80);
    second_v.fill(190);

    const auto first_slice =
        make_i_pcm_slice(0, first_luma, first_u, first_v);
    const auto second_slice =
        make_i_pcm_slice(1, second_luma, second_u, second_v);
    const auto frame = pygorvid::decode_idr_i_pcm_frame(
        {first_slice, second_slice}, sps, pps);
    assert(frame.width == 32 && frame.height == 16);
    assert(frame.luma.size() == 32 * 16);
    assert(frame.chroma_u.size() == 16 * 8);
    assert(frame.chroma_v.size() == 16 * 8);
    assert(frame.luma[0] == first_luma[0]);
    assert(frame.luma[15] == first_luma[15]);
    assert(frame.luma[36] == 0);
    assert(frame.luma[16] == second_luma[0]);
    assert(frame.luma[32] == first_luma[16]);
    assert(frame.chroma_u[0] == 0);
    assert(frame.chroma_v[0] == 180);
    assert(frame.chroma_u[8] == 80);
    assert(frame.chroma_v[8] == 190);

    const auto filtered_frame = pygorvid::decode_idr_i_pcm_frame(
        {make_i_pcm_slice(0, first_luma, first_u, first_v, 0),
         make_i_pcm_slice(1, second_luma, second_u, second_v, 0)},
        sps, pps);
    assert(filtered_frame.luma == frame.luma);
    assert(filtered_frame.chroma_u == frame.chroma_u);
    assert(filtered_frame.chroma_v == frame.chroma_v);

    std::vector<std::uint8_t> sample;
    for (const auto* slice : {&first_slice, &second_slice}) {
        append_u32(sample, static_cast<std::uint32_t>(slice->size()));
        append(sample, *slice);
    }
    const auto sample_frame =
        pygorvid::decode_idr_i_pcm_sample(sample, 4, sps, pps);
    assert(sample_frame.luma == frame.luma);
    assert(sample_frame.chroma_u == frame.chroma_u);
    assert(sample_frame.chroma_v == frame.chroma_v);

    const auto cropped_sps =
        pygorvid::parse_sps(make_sps(66, 1, 0, {1, 1, 1, 1}));
    const auto cropped_frame = pygorvid::decode_idr_i_pcm_frame(
        {first_slice, second_slice}, cropped_sps, pps);
    assert(cropped_frame.width == 28 && cropped_frame.height == 12);
    assert(cropped_frame.luma[0] == first_luma[34]);
    assert(cropped_frame.chroma_u[0] == first_u[9]);
    assert(cropped_frame.chroma_v[0] == first_v[9]);

    bool incomplete_picture_rejected = false;
    try {
        (void)pygorvid::decode_idr_i_pcm_frame({first_slice}, sps, pps);
    } catch (const std::runtime_error&) {
        incomplete_picture_rejected = true;
    }
    assert(incomplete_picture_rejected);

    auto cabac_pps = pygorvid::parse_pps(make_pps(true, false));
    bool cabac_rejected = false;
    try {
        (void)pygorvid::decode_idr_i_pcm_frame(
            {first_slice, second_slice}, sps, cabac_pps);
    } catch (const std::runtime_error&) {
        cabac_rejected = true;
    }
    assert(cabac_rejected);
}

void test_h264_i4x4_residual_reconstruction() {
    const auto sps = pygorvid::parse_sps(make_sps(66, 0, 0));
    const auto pps = pygorvid::parse_pps(make_pps(false, false));
    std::array<std::string, 16> zero_luma_bits;
    zero_luma_bits.fill("1");
    const auto baseline = pygorvid::decode_idr_i_frame(
        {make_i4x4_slice(3, zero_luma_bits)}, sps, pps);

    auto luma_bits = zero_luma_bits;
    luma_bits[0] = "0101";
    const auto luma_frame = pygorvid::decode_idr_i_frame(
        {make_i4x4_slice(29, luma_bits)}, sps, pps);
    assert(luma_frame.luma[0] == 131);
    assert(luma_frame.luma[4] == 131);
    assert(luma_frame.luma[4 * 16] == 131);
    assert(luma_frame.luma[4 * 16 + 4] == 131);
    assert(luma_frame.chroma_u == baseline.chroma_u);
    assert(luma_frame.chroma_v == baseline.chroma_v);

    auto luma_ac_bits = zero_luma_bits;
    luma_ac_bits[0] = "010011";
    const auto luma_ac_frame = pygorvid::decode_idr_i_frame(
        {make_i4x4_slice(29, luma_ac_bits)}, sps, pps);
    assert(luma_ac_frame.luma[0] == 132);
    assert(luma_ac_frame.luma[1] == 130);
    assert(luma_ac_frame.luma[2] == 126);
    assert(luma_ac_frame.luma[3] == 124);

    const auto raised_qp_frame = pygorvid::decode_idr_i_frame(
        {make_i4x4_slice(29, luma_bits, {"01", "01"}, nullptr, 6)},
        sps, pps);
    assert(raised_qp_frame.luma[0] == 135);

    std::array<std::array<std::string, 4>, 2> chroma_ac_bits{};
    for (auto& plane : chroma_ac_bits) {
        plane.fill("1");
    }
    chroma_ac_bits[0][0] = "0101";
    const auto chroma_frame = pygorvid::decode_idr_i_frame(
        {make_i4x4_slice(42, zero_luma_bits, {"01", "01"},
                         &chroma_ac_bits)},
        sps, pps);
    assert(chroma_frame.luma == baseline.luma);
    assert(chroma_frame.chroma_u[0] == 132);
    assert(chroma_frame.chroma_u[1] == 130);
    assert(chroma_frame.chroma_u[2] == 126);
    assert(chroma_frame.chroma_u[3] == 124);
    assert(chroma_frame.chroma_v == baseline.chroma_v);

    const auto chroma_dc_frame = pygorvid::decode_idr_i_frame(
        {make_i4x4_slice(16, zero_luma_bits,
                         {"00011111", "01"}, nullptr, 16)},
        sps, pps);
    assert(chroma_dc_frame.chroma_u[0] == 129);
    assert(chroma_dc_frame.chroma_v == baseline.chroma_v);
}

void test_h264_i16x16_residual_reconstruction() {
    const auto sps = pygorvid::parse_sps(make_sps(66, 1, 1));
    const auto pps = pygorvid::parse_pps(make_pps(false, false));
    std::array<std::uint8_t, 256> luma{};
    std::array<std::uint8_t, 64> chroma_u{};
    std::array<std::uint8_t, 64> chroma_v{};
    for (std::size_t index = 0; index < luma.size(); ++index) {
        luma[index] = static_cast<std::uint8_t>(32 + index / 16);
    }
    for (std::size_t index = 0; index < chroma_u.size(); ++index) {
        chroma_u[index] = static_cast<std::uint8_t>(64 + index / 8);
        chroma_v[index] = static_cast<std::uint8_t>(192 - index / 8);
    }
    const auto slice = make_mixed_i_slice(luma, chroma_u, chroma_v);
    const auto frame = pygorvid::decode_idr_i_frame({slice}, sps, pps);
    assert(frame.width == 32 && frame.height == 32);
    assert(frame.luma[0] == luma[0]);
    assert(frame.luma[16] == luma[15]);
    assert(frame.luma[16 * 32] == luma[16 * 15]);
    assert(frame.luma[16 * 32 + 16] == luma[16 * 15]);
    assert(frame.chroma_u[0] == chroma_u[0]);
    assert(frame.chroma_v[0] == chroma_v[0]);
    assert(frame.chroma_u[8] == chroma_u[7]);
    assert(frame.chroma_v[8] == chroma_v[7]);
    assert(frame.chroma_u[8 * 16] == chroma_u[56]);
    assert(frame.chroma_v[8 * 16] == chroma_v[56]);
    assert(frame.luma[16 * 32 + 16] == 47);
    assert(frame.chroma_u[8 * 16 + 8] == 71);
    assert(frame.chroma_v[8 * 16 + 8] == 185);

    const auto filtered_slice = make_mixed_i_slice(
        luma, chroma_u, chroma_v, {"1", "1", "1"}, {0, 0, 0}, {2, 1, 4},
        nullptr, nullptr, nullptr, 0);
    const auto filtered_frame =
        pygorvid::decode_idr_i_frame({filtered_slice}, sps, pps);
    assert(filtered_frame.luma != frame.luma ||
           filtered_frame.chroma_u != frame.chroma_u ||
           filtered_frame.chroma_v != frame.chroma_v);

    std::vector<std::uint8_t> sample;
    append_u32(sample, static_cast<std::uint32_t>(slice.size()));
    append(sample, slice);
    const auto sample_frame =
        pygorvid::decode_idr_i_sample(sample, 4, sps, pps);
    assert(sample_frame.luma == frame.luma);
    assert(sample_frame.chroma_u == frame.chroma_u);
    assert(sample_frame.chroma_v == frame.chroma_v);

    const std::array<std::string, 3> positive_dc_bits{"1", "1", "0101"};
    const std::array<std::int64_t, 3> positive_dc_qp_deltas{0, 0, 10};
    const auto positive_dc_slice = make_mixed_i_slice(
        luma, chroma_u, chroma_v, positive_dc_bits, positive_dc_qp_deltas);
    const auto positive_dc_frame =
        pygorvid::decode_idr_i_frame({positive_dc_slice}, sps, pps);
    for (std::uint32_t row = 16; row < 32; ++row) {
        for (std::uint32_t column = 16; column < 32; ++column) {
            const auto index = static_cast<std::size_t>(row) * 32 + column;
            assert(positive_dc_frame.luma[index] == frame.luma[index] + 3);
        }
    }

    const std::array<std::string, 3> negative_dc_bits{"1", "1", "0111"};
    const auto negative_dc_slice =
        make_mixed_i_slice(luma, chroma_u, chroma_v, negative_dc_bits);
    const auto negative_dc_frame =
        pygorvid::decode_idr_i_frame({negative_dc_slice}, sps, pps);
    for (std::uint32_t row = 16; row < 32; ++row) {
        for (std::uint32_t column = 16; column < 32; ++column) {
            const auto index = static_cast<std::size_t>(row) * 32 + column;
            assert(negative_dc_frame.luma[index] == frame.luma[index] - 1);
        }
    }

    std::array<std::string, 16> luma_ac_bits;
    luma_ac_bits.fill("1");
    luma_ac_bits[0] = "0101";
    const std::array<std::uint32_t, 3> luma_ac_types{2, 1, 14};
    std::array<std::string, 16> luma_ac_baseline_bits;
    luma_ac_baseline_bits.fill("1");
    luma_ac_baseline_bits[15] = "0101";
    const auto luma_ac_baseline_slice = make_mixed_i_slice(
        luma, chroma_u, chroma_v, {"1", "1", "1"}, {0, 0, 0},
        luma_ac_types, &luma_ac_baseline_bits);
    const auto luma_ac_baseline =
        pygorvid::decode_idr_i_frame({luma_ac_baseline_slice}, sps, pps);
    const auto luma_ac_slice = make_mixed_i_slice(
        luma, chroma_u, chroma_v, {"1", "1", "1"}, {0, 0, 0},
        luma_ac_types, &luma_ac_bits);
    const auto luma_ac_frame =
        pygorvid::decode_idr_i_frame({luma_ac_slice}, sps, pps);
    assert(luma_ac_frame.luma[16 * 32 + 16] ==
           luma_ac_baseline.luma[16 * 32 + 16] + 4);
    assert(luma_ac_frame.luma[16 * 32 + 17] ==
           luma_ac_baseline.luma[16 * 32 + 17] + 2);
    assert(luma_ac_frame.luma[16 * 32 + 18] ==
           luma_ac_baseline.luma[16 * 32 + 18] - 2);
    assert(luma_ac_frame.luma[16 * 32 + 19] ==
           luma_ac_baseline.luma[16 * 32 + 19] - 4);

    const auto combined_residual_slice = make_mixed_i_slice(
        luma, chroma_u, chroma_v, {"1", "1", "0101"}, {0, 0, 0},
        luma_ac_types, &luma_ac_bits);
    const auto combined_residual_frame =
        pygorvid::decode_idr_i_frame({combined_residual_slice}, sps, pps);
    for (std::uint32_t column = 16; column < 20; ++column) {
        assert(combined_residual_frame.luma[16 * 32 + column] ==
               luma_ac_frame.luma[16 * 32 + column] + 1);
    }

    std::array<std::string, 16> contextual_ac_bits;
    contextual_ac_bits.fill("1");
    contextual_ac_bits[0] = "00100111";
    contextual_ac_bits[1] = "00100111";
    contextual_ac_bits[2] = "00100111";
    contextual_ac_bits[3] = "01100111";
    std::array<std::string, 16> contextual_baseline_bits;
    contextual_baseline_bits.fill("1");
    contextual_baseline_bits[15] = "0101";
    const auto contextual_baseline_slice = make_mixed_i_slice(
        luma, chroma_u, chroma_v, {"1", "1", "1"}, {0, 0, 0},
        luma_ac_types, &contextual_baseline_bits);
    const auto contextual_baseline =
        pygorvid::decode_idr_i_frame({contextual_baseline_slice}, sps, pps);
    const auto contextual_ac_slice = make_mixed_i_slice(
        luma, chroma_u, chroma_v, {"1", "1", "1"}, {0, 0, 0},
        luma_ac_types, &contextual_ac_bits);
    const auto contextual_ac_frame =
        pygorvid::decode_idr_i_frame({contextual_ac_slice}, sps, pps);
    assert(contextual_ac_frame.luma[16 * 32 + 16] ==
           contextual_baseline.luma[16 * 32 + 16] + 8);

    auto scaled_sps = sps;
    scaled_sps.seq_intra_luma_4x4_scaling_list_present = true;
    bool custom_scaling_matrix_rejected = false;
    try {
        (void)pygorvid::decode_idr_i_frame({positive_dc_slice}, scaled_sps, pps);
    } catch (const std::runtime_error&) {
        custom_scaling_matrix_rejected = true;
    }
    assert(custom_scaling_matrix_rejected);

    std::array<std::string, 16> negative_ac_bits;
    negative_ac_bits.fill("1");
    negative_ac_bits[0] = "0111";
    const auto negative_ac_slice = make_mixed_i_slice(
        luma, chroma_u, chroma_v, {"1", "1", "1"}, {0, 0, 0},
        luma_ac_types, &negative_ac_bits);
    const auto negative_ac_frame =
        pygorvid::decode_idr_i_frame({negative_ac_slice}, sps, pps);
    assert(negative_ac_frame.luma[16 * 32 + 16] ==
           luma_ac_baseline.luma[16 * 32 + 16] - 4);
    assert(negative_ac_frame.luma[16 * 32 + 17] ==
           luma_ac_baseline.luma[16 * 32 + 17] - 2);
    assert(negative_ac_frame.luma[16 * 32 + 18] ==
           luma_ac_baseline.luma[16 * 32 + 18] + 2);
    assert(negative_ac_frame.luma[16 * 32 + 19] ==
           luma_ac_baseline.luma[16 * 32 + 19] + 4);

    const std::array<std::uint32_t, 3> unsupported_types{2, 1, 26};
    const auto unsupported_slice = make_mixed_i_slice(
        luma, chroma_u, chroma_v, {"1", "1", "1"}, {0, 0, 0},
        unsupported_types);
    bool unsupported_macroblock_rejected = false;
    try {
        (void)pygorvid::decode_idr_i_frame(
            {unsupported_slice}, sps, pps);
    } catch (const std::runtime_error&) {
        unsupported_macroblock_rejected = true;
    }
    assert(unsupported_macroblock_rejected);

    std::array<std::array<std::string, 2>, 3> chroma_dc_bits;
    for (auto& macroblock_bits : chroma_dc_bits) {
        macroblock_bits = {{"1", "1"}};
    }
    chroma_dc_bits[2] = {{"00011111", "00011111"}};
    const std::array<std::uint32_t, 3> chroma_dc_types{2, 1, 5};
    const auto chroma_dc_slice = make_mixed_i_slice(
        luma, chroma_u, chroma_v, {"1", "1", "1"}, {0, 0, 16},
        chroma_dc_types, nullptr, &chroma_dc_bits);
    const auto chroma_dc_frame =
        pygorvid::decode_idr_i_frame({chroma_dc_slice}, sps, pps);
    for (std::uint32_t row = 12; row < 16; ++row) {
        for (std::uint32_t column = 12; column < 16; ++column) {
            const auto index = static_cast<std::size_t>(row) * 16 + column;
            assert(chroma_dc_frame.chroma_u[index] ==
                   frame.chroma_u[index] + 1);
            assert(chroma_dc_frame.chroma_v[index] ==
                   frame.chroma_v[index] + 1);
        }
    }
    chroma_dc_bits[2] = {{"000111101", "01"}};
    const auto chroma_dc_frequency_slice = make_mixed_i_slice(
        luma, chroma_u, chroma_v, {"1", "1", "1"}, {0, 0, 16},
        chroma_dc_types, nullptr, &chroma_dc_bits);
    const auto chroma_dc_frequency_frame =
        pygorvid::decode_idr_i_frame({chroma_dc_frequency_slice}, sps, pps);
    assert(chroma_dc_frequency_frame.chroma_u[8 * 16 + 8] ==
           frame.chroma_u[8 * 16 + 8] + 1);
    assert(chroma_dc_frequency_frame.chroma_u[8 * 16 + 12] ==
           frame.chroma_u[8 * 16 + 12] - 1);
    assert(chroma_dc_frequency_frame.chroma_u[12 * 16 + 8] ==
           frame.chroma_u[12 * 16 + 8] + 1);
    assert(chroma_dc_frequency_frame.chroma_u[12 * 16 + 12] ==
           frame.chroma_u[12 * 16 + 12] - 1);

    std::array<std::array<std::string, 8>, 3> chroma_ac_bits;
    for (auto& macroblock_bits : chroma_ac_bits) {
        macroblock_bits.fill("1");
    }
    chroma_ac_bits[2][0] = "0101";
    std::array<std::string, 16> combined_luma_ac_bits;
    combined_luma_ac_bits.fill("1");
    combined_luma_ac_bits[0] = "0101";
    const std::array<std::uint32_t, 3> chroma_ac_types{2, 1, 21};
    const auto chroma_ac_slice = make_mixed_i_slice(
        luma, chroma_u, chroma_v, {"1", "1", "1"}, {0, 0, 0},
        chroma_ac_types, &combined_luma_ac_bits, nullptr, &chroma_ac_bits);
    const auto chroma_ac_frame =
        pygorvid::decode_idr_i_frame({chroma_ac_slice}, sps, pps);
    assert(chroma_ac_frame.chroma_u[8 * 16 + 8] ==
           frame.chroma_u[8 * 16 + 8] + 4);
    assert(chroma_ac_frame.chroma_u[8 * 16 + 9] ==
           frame.chroma_u[8 * 16 + 9] + 2);
    assert(chroma_ac_frame.chroma_u[8 * 16 + 10] ==
           frame.chroma_u[8 * 16 + 10] - 2);
    assert(chroma_ac_frame.chroma_u[8 * 16 + 11] ==
           frame.chroma_u[8 * 16 + 11] - 4);

    auto scaled_chroma_sps = sps;
    scaled_chroma_sps.seq_intra_chroma_4x4_scaling_list_present = true;
    bool custom_chroma_scaling_rejected = false;
    try {
        (void)pygorvid::decode_idr_i_frame(
            {chroma_dc_slice}, scaled_chroma_sps, pps);
    } catch (const std::runtime_error&) {
        custom_chroma_scaling_rejected = true;
    }
    assert(custom_chroma_scaling_rejected);

    bool pcm_only_rejected_intra = false;
    try {
        (void)pygorvid::decode_idr_i_pcm_frame({slice}, sps, pps);
    } catch (const std::runtime_error&) {
        pcm_only_rejected_intra = true;
    }
    assert(pcm_only_rejected_intra);
}

void test_cavlc_residual_blocks() {
    struct Vector {
        const char* bits;
        std::int32_t nc;
        std::size_t max_coeff;
        std::size_t first_coefficient;
        std::int32_t first_value;
        std::size_t second_coefficient;
        std::int32_t second_value;
    };
    const std::array<Vector, 6> vectors{{
        {"1", 0, 16, 0, 0, 16, 0},
        {"0101", 0, 16, 0, 1, 16, 0},
        {"011011", 0, 16, 1, -1, 0, 0},
        {"00101111", 0, 16, 0, -1, 1, 1},
        {"101", -1, 4, 0, 1, 4, 0},
        {"000011", 8, 16, 0, 0, 16, 0},
    }};
    for (const auto& vector : vectors) {
        const auto data = bits_from_string(vector.bits);
        pygorvid::BitReader reader(data);
        const auto block = pygorvid::decode_cavlc_residual_block(
            reader, vector.nc, vector.max_coeff);
        assert(block.coefficients.size() == vector.max_coeff);
        assert(block.coefficients[vector.first_coefficient] == vector.first_value);
        if (vector.second_coefficient < vector.max_coeff &&
            block.coefficients[vector.second_coefficient] != vector.second_value) {
            throw std::runtime_error(std::string("CAVLC vector failed: ") + vector.bits);
        }
        assert(reader.bit_offset() == std::string(vector.bits).size());
    }

    const auto contextual_data =
        bits_from_string("00100111011001110110011101100111");
    pygorvid::BitReader contextual_reader(contextual_data);
    for (const auto context : {0, 2, 2, 2}) {
        const auto block = pygorvid::decode_cavlc_residual_block(
            contextual_reader, context, 15);
        assert(block.total_coeff == 2);
    }
    assert(contextual_reader.bit_offset() == 32);

    const auto truncated = bits_from_string("0");
    pygorvid::BitReader reader(truncated);
    bool malformed_rejected = false;
    try {
        (void)pygorvid::decode_cavlc_residual_block(reader, 0, 16);
    } catch (const std::runtime_error&) {
        malformed_rejected = true;
    }
    assert(malformed_rejected);
    assert(reader.bit_offset() == 0);
}

std::vector<std::uint8_t> make_test_mp4() {
    std::vector<std::uint8_t> ftyp_payload{'i', 's', 'o', 'm', 0, 0, 0, 0};
    append(ftyp_payload, std::vector<std::uint8_t>{'i', 's', 'o', 'm'});
    auto ftyp = make_box("ftyp", ftyp_payload);

    std::vector<std::uint8_t> tkhd(84, 0);
    tkhd[76] = 0;
    tkhd[77] = 2;
    tkhd[78] = 0;
    tkhd[79] = 0;
    tkhd[80] = 0;
    tkhd[81] = 1;
    tkhd[82] = 0;
    tkhd[83] = 0;

    std::vector<std::uint8_t> mdhd(20, 0);
    mdhd[12] = 0;
    mdhd[13] = 0;
    mdhd[14] = 3;
    mdhd[15] = 232;

    std::vector<std::uint8_t> hdlr(12, 0);
    hdlr[8] = 'v';
    hdlr[9] = 'i';
    hdlr[10] = 'd';
    hdlr[11] = 'e';

    std::vector<std::uint8_t> stts(16, 0);
    stts[7] = 1;
    stts[11] = 3;
    stts[15] = 40;
    auto stbl = make_box("stbl", make_box("stts", stts));
    auto minf = make_box("minf", stbl);

    std::vector<std::uint8_t> mdia_payload;
    append(mdia_payload, make_box("mdhd", mdhd));
    append(mdia_payload, make_box("hdlr", hdlr));
    append(mdia_payload, minf);

    std::vector<std::uint8_t> trak_payload;
    append(trak_payload, make_box("tkhd", tkhd));
    append(trak_payload, make_box("mdia", mdia_payload));
    auto moov = make_box("moov", make_box("trak", trak_payload));

    append(ftyp, moov);
    return ftyp;
}

std::vector<std::uint8_t> make_sample_test_mp4() {
    std::vector<std::uint8_t> ftyp_payload{'i', 's', 'o', 'm', 0, 0, 0, 0};
    append(ftyp_payload, std::vector<std::uint8_t>{'i', 's', 'o', 'm'});
    const auto ftyp = make_box("ftyp", ftyp_payload);

    const std::vector<std::vector<std::uint8_t>> samples{
        {0, 0, 0, 2, 0x65, 0x80},
        {0, 0, 0, 3, 0x41, 0x80, 0x00},
        {0, 0, 0, 2, 0x65, 0x80}};
    std::vector<std::uint8_t> media_data;
    for (const auto& sample : samples) {
        append(media_data, sample);
    }
    const auto mdat = make_box("mdat", media_data);
    const auto first_sample_offset =
        static_cast<std::uint32_t>(ftyp.size() + 8);

    const std::vector<std::uint8_t> avcc_payload{
        1, 66, 0, 30, 0xff, 0xe1, 0, 2, 0x67, 0x42, 1, 0, 2, 0x68, 0xce};
    std::vector<std::uint8_t> avc1_payload(78, 0);
    append(avc1_payload, make_box("avcC", avcc_payload));
    auto avc1 = make_box("avc1", avc1_payload);
    std::vector<std::uint8_t> stsd_payload(4, 0);
    append_u32(stsd_payload, 1);
    append(stsd_payload, avc1);

    std::vector<std::uint8_t> stsz_payload(4, 0);
    append_u32(stsz_payload, 0);
    append_u32(stsz_payload, static_cast<std::uint32_t>(samples.size()));
    for (const auto& sample : samples) {
        append_u32(stsz_payload, static_cast<std::uint32_t>(sample.size()));
    }

    std::vector<std::uint8_t> stsc_payload(4, 0);
    append_u32(stsc_payload, 1);
    append_u32(stsc_payload, 1);
    append_u32(stsc_payload, 1);
    append_u32(stsc_payload, 1);

    std::vector<std::uint8_t> stts_payload(4, 0);
    append_u32(stts_payload, 1);
    append_u32(stts_payload, static_cast<std::uint32_t>(samples.size()));
    append_u32(stts_payload, 40);

    std::vector<std::uint8_t> ctts_payload{1, 0, 0, 0};
    append_u32(ctts_payload, 3);
    append_u32(ctts_payload, 1);
    append_u32(ctts_payload, 0);
    append_u32(ctts_payload, 1);
    append_u32(ctts_payload, 10);
    append_u32(ctts_payload, 1);
    append_u32(ctts_payload, 0xffffffd8U);

    std::vector<std::uint8_t> stss_payload(4, 0);
    append_u32(stss_payload, 2);
    append_u32(stss_payload, 1);
    append_u32(stss_payload, 3);

    std::vector<std::uint8_t> stco_payload(4, 0);
    append_u32(stco_payload, static_cast<std::uint32_t>(samples.size()));
    auto sample_offset = first_sample_offset;
    for (const auto& sample : samples) {
        append_u32(stco_payload, sample_offset);
        sample_offset += static_cast<std::uint32_t>(sample.size());
    }

    std::vector<std::uint8_t> stbl_payload;
    append(stbl_payload, make_box("stsd", stsd_payload));
    append(stbl_payload, make_box("stsz", stsz_payload));
    append(stbl_payload, make_box("stsc", stsc_payload));
    append(stbl_payload, make_box("stts", stts_payload));
    append(stbl_payload, make_box("ctts", ctts_payload));
    append(stbl_payload, make_box("stss", stss_payload));
    append(stbl_payload, make_box("stco", stco_payload));
    const auto stbl = make_box("stbl", stbl_payload);
    const auto minf = make_box("minf", stbl);

    std::vector<std::uint8_t> hdlr_payload(8, 0);
    append(hdlr_payload, std::vector<std::uint8_t>{'v', 'i', 'd', 'e'});
    std::vector<std::uint8_t> mdhd_payload(20, 0);
    mdhd_payload[12] = 0;
    mdhd_payload[13] = 0;
    mdhd_payload[14] = 3;
    mdhd_payload[15] = 232;
    std::vector<std::uint8_t> mdia_payload;
    append(mdia_payload, make_box("mdhd", mdhd_payload));
    append(mdia_payload, make_box("hdlr", hdlr_payload));
    append(mdia_payload, minf);
    const auto mdia = make_box("mdia", mdia_payload);
    const auto trak = make_box("trak", mdia);
    const auto moov = make_box("moov", trak);

    std::vector<std::uint8_t> file_data;
    append(file_data, ftyp);
    append(file_data, mdat);
    append(file_data, moov);
    return file_data;
}

void test_video_sample_reader() {
    const auto test_directory =
        std::filesystem::path("tmp/pygorvid-cpp-sample-tests");
    std::filesystem::create_directories(test_directory);
    const auto test_file = test_directory / "samples.mp4";
    {
        std::ofstream output(test_file, std::ios::binary);
        const auto data = make_sample_test_mp4();
        output.write(reinterpret_cast<const char*>(data.data()),
                     static_cast<std::streamsize>(data.size()));
        assert(output.good());
    }

    pygorvid::VideoSampleReader reader;
    assert(reader.open(test_file.string()));
    assert(reader.error_info().empty());
    assert(reader.is_open());
    assert(reader.sample_count() == 3);
    assert(reader.presentation_sample_index(0) == 0);
    assert(reader.presentation_sample_index(1) == 2);
    assert(reader.presentation_sample_index(2) == 1);
    assert(reader.timescale() == 1000);
    const auto& configuration = reader.configuration();
    assert(configuration.profile_idc == 66);
    assert(configuration.level_idc == 30);
    assert(configuration.nal_length_size == 4);
    assert((configuration.sequence_parameter_sets ==
            std::vector<std::vector<std::uint8_t>>{{0x67, 0x42}}));
    assert((configuration.picture_parameter_sets ==
            std::vector<std::vector<std::uint8_t>>{{0x68, 0xce}}));

    const auto first = reader.sample_at(0);
    assert(first.index == 0);
    assert(first.dts_ticks == 0);
    assert(first.pts_ticks == 0);
    assert(first.duration_ticks == 40);
    assert(first.is_sync);
    assert(first.data ==
           std::vector<std::uint8_t>({0, 0, 0, 2, 0x65, 0x80}));

    const auto second = reader.sample_at(1);
    assert(second.dts_ticks == 40);
    assert(second.pts_ticks == 50);
    assert(!second.is_sync);
    assert(second.data.size() == 7);

    const auto third = reader.sample_at(2);
    assert(third.dts_ticks == 80);
    assert(third.pts_ticks == 40);
    assert(third.is_sync);

    pygorvid::CompressedVideoSample sequential;
    assert(reader.read_next(sequential));
    assert(sequential.index == 0);
    assert(reader.read_next(sequential));
    assert(sequential.index == 1);
    assert(reader.read_next(sequential));
    assert(sequential.index == 2);
    assert(!reader.read_next(sequential));

    bool out_of_range_rejected = false;
    try {
        (void)reader.sample_at(3);
    } catch (const std::out_of_range&) {
        out_of_range_rejected = true;
    }
    assert(out_of_range_rejected);
    bool presentation_index_out_of_range_rejected = false;
    try {
        (void)reader.presentation_sample_index(3);
    } catch (const std::out_of_range&) {
        presentation_index_out_of_range_rejected = true;
    }
    assert(presentation_index_out_of_range_rejected);
    reader.close();
    assert(!reader.is_open());

    std::filesystem::remove(test_file);
    std::filesystem::remove(test_directory);
}

} // namespace

int main() {
    test_cabac_arithmetic_engine();
    test_h264_nal_utilities();
    test_h264_parameter_sets();
    test_h264_idr_slice_header();
    test_h264_p_skip_picture();
    test_png_encoding();
    test_jpeg_encoding();
    test_h264_i_pcm_frame_reconstruction();
    test_h264_i4x4_prediction_modes();
    test_h264_i4x4_residual_reconstruction();
    test_h264_i16x16_residual_reconstruction();
    test_cavlc_residual_blocks();
    test_video_sample_reader();
    {
        const std::vector<std::uint8_t> data{0b10110010, 0b01100000};
        pygorvid::BitReader reader(data);
        assert(reader.read_bits(3) == 5);
        assert(reader.read_bit());
        assert(reader.bit_offset() == 4);
        reader.align_to_byte();
        assert(reader.bit_offset() == 8);
        assert(reader.read_bits(4) == 6);
    }
    {
        const std::vector<std::uint8_t> data{0b00101010};
        pygorvid::BitReader reader(data);
        assert(reader.read_ue() == 4);
        assert(reader.read_se() == 1);
    }
    {
        const std::vector<std::uint8_t> mp4{
            0, 0, 0, 16, 'f', 't', 'y', 'p', 'm', 'p', '4', '2', 0, 0, 0, 0};
        assert(pygorvid::detect_format(mp4) == "mp4");
        auto mov = mp4;
        mov[8] = 'q';
        mov[9] = 't';
        mov[10] = ' ';
        mov[11] = ' ';
        assert(pygorvid::detect_format(mov) == "mov");
        assert(pygorvid::detect_format({}) == "unknown");
    }
    {
        const std::vector<std::uint8_t> data{0};
        pygorvid::BitReader reader(data);
        bool failed = false;
        try {
            (void)reader.read_ue();
        } catch (const std::runtime_error&) {
            failed = true;
        }
        assert(failed);
        assert(reader.bit_offset() == 0);
    }
    {
        const auto test_directory = std::filesystem::path("tmp/pygorvid-cpp-tests");
        std::filesystem::create_directories(test_directory);
        const auto test_file = test_directory / "metadata.mp4";
        {
            std::ofstream output(test_file, std::ios::binary);
            const auto payload = make_test_mp4();
            output.write(reinterpret_cast<const char*>(payload.data()),
                         static_cast<std::streamsize>(payload.size()));
            assert(output.good());
        }
        pygorvid::Mp4File file;
        assert(file.open(test_file.string()));
        assert(file.is_open());
        const auto info = file.get_basic_info();
        assert(file.error_info().empty());
        assert(info.hasvideo);
        assert(info.videostreams.size() == 1);
        assert(info.videostreams[0].width == 2);
        assert(info.videostreams[0].height == 1);
        assert(info.videostreams[0].frame_count == 3);
        assert(info.videostreams[0].duration_seconds == 0.12);
        assert(info.videostreams[0].framerate == 25.0);
        file.close();
        assert(!file.is_open());
        std::filesystem::remove(test_file);
        std::filesystem::remove(test_directory);
    }
}
