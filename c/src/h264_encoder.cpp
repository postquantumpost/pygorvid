#include "pygorvid/h264_encoder.hpp"
#include "pygorvid/cabac.hpp"

#include <algorithm>
#include <array>
#include <cstddef>
#include <cstdint>
#include <stdexcept>

namespace pygorvid {
namespace {

class BitWriter {
public:
    std::vector<std::uint8_t> data;
    std::size_t bit_count = 0;

    void bits(std::uint32_t value, int count) {
        for (int shift = count - 1; shift >= 0; --shift) {
            if (bit_count % 8 == 0) {
                data.push_back(0);
            }
            data.back() |= static_cast<std::uint8_t>(
                ((value >> shift) & 1) << (7 - bit_count % 8));
            ++bit_count;
        }
    }

    void ue(std::uint32_t value) {
        int count = 0;
        for (auto remaining = value + 1; remaining; remaining >>= 1) {
            ++count;
        }
        bits(0, count - 1);
        bits(value + 1, count);
    }

    void se(std::int32_t value) {
        ue(value > 0 ? static_cast<std::uint32_t>(2 * value - 1)
                     : static_cast<std::uint32_t>(-2 * value));
    }

    void align() {
        while (bit_count % 8) {
            bits(0, 1);
        }
    }

    std::vector<std::uint8_t> nal(std::uint8_t header) {
        bits(1, 1);
        align();
        std::vector<std::uint8_t> result{header};
        int zeros = 0;
        for (const auto value : data) {
            if (zeros == 2 && value <= 3) {
                result.push_back(3);
                zeros = 0;
            }
            result.push_back(value);
            zeros = value == 0 ? zeros + 1 : 0;
        }
        return result;
    }
};

constexpr std::array<std::array<std::uint8_t, 64>, 4> range_lps{{
    {{128, 128, 128, 123, 116, 111, 105, 100, 95, 90, 85, 81, 77, 73, 69, 66,
      62, 59, 56, 53, 51, 48, 46, 43, 41, 39, 37, 35, 33, 32, 30, 29, 27, 26,
      24, 23, 22, 21, 20, 19, 18, 17, 16, 15, 14, 14, 13, 12, 12, 11, 11, 10,
      10, 9, 9, 8, 8, 7, 7, 7, 6, 6, 6, 2}},
    {{176, 167, 158, 150, 142, 135, 128, 122, 116, 110, 104, 99, 94, 89, 85,
      80, 76, 72, 69, 65, 62, 59, 56, 53, 50, 48, 45, 43, 41, 39, 37, 35, 33,
      31, 30, 28, 27, 26, 24, 23, 22, 21, 20, 19, 18, 17, 16, 15, 14, 14, 13,
      12, 12, 11, 11, 10, 9, 9, 9, 8, 8, 7, 7, 2}},
    {{208, 197, 187, 178, 169, 160, 152, 144, 137, 130, 123, 117, 111, 105,
      100, 95, 90, 86, 81, 77, 73, 69, 66, 63, 59, 56, 54, 51, 48, 46, 43, 41,
      39, 37, 35, 33, 32, 30, 29, 27, 26, 25, 23, 22, 21, 20, 19, 18, 17, 16,
      15, 15, 14, 13, 12, 12, 11, 11, 10, 10, 9, 9, 8, 2}},
    {{240, 227, 216, 205, 195, 185, 175, 166, 158, 150, 142, 135, 128, 122,
      116, 110, 104, 99, 94, 89, 85, 80, 76, 72, 69, 65, 62, 59, 56, 53, 50,
      48, 45, 43, 41, 39, 37, 35, 33, 31, 30, 28, 27, 25, 24, 23, 22, 21, 20,
      19, 18, 17, 16, 15, 14, 14, 13, 12, 12, 11, 11, 10, 9, 2}}
}};

class CabacWriter {
public:
    explicit CabacWriter(std::vector<CabacContextModel> contexts)
        : contexts_(std::move(contexts)) {}

    void decision(bool value, std::size_t context_index) {
        auto& model = contexts_.at(context_index);
        const auto lps =
            range_lps[(range_ >> 6) & 3][model.state_index()];
        if (value == model.mps()) {
            range_ -= lps;
            model.update(value);
            renormalize();
            return;
        }
        low_ += range_ - lps;
        range_ = lps;
        model.update(value);
        renormalize();
    }

    void luma_cbp(int left, int top, int desired) {
        auto context = !static_cast<bool>(left & 2) +
                        2 * !static_cast<bool>(top & 4);
        decision(desired & 1, 73 + context);
        auto current = desired & 1;
        context = !static_cast<bool>(current & 1) +
                  2 * !static_cast<bool>(top & 8);
        decision((desired >> 1) & 1, 73 + context);
        current |= desired & 2;
        context = !static_cast<bool>(left & 8) +
                  2 * !static_cast<bool>(current & 1);
        decision((desired >> 2) & 1, 73 + context);
        current |= desired & 4;
        context = !static_cast<bool>(current & 4) +
                  2 * !static_cast<bool>(current & 2);
        decision((desired >> 3) & 1, 73 + context);
    }

    void zero_luma_cbp(int left, int top) {
        luma_cbp(left, top, 0);
    }

    void bypass(bool value) {
        low_ <<= 1;
        if (value) {
            low_ += range_;
        }
        if (low_ >= 1024) {
            low_ -= 1024;
            put(1);
        } else if (low_ < 512) {
            put(0);
        } else {
            low_ -= 512;
            ++bits_to_follow_;
        }
    }

    void level(int magnitude, int first_context, int greater_context) {
        if (magnitude == 1) {
            decision(false, first_context);
        } else {
            decision(true, first_context);
            for (int count = 0; count < std::min(magnitude - 2, 13); ++count) {
                decision(true, greater_context + 5);
            }
            if (magnitude <= 14) {
                decision(false, greater_context + 5);
            }
        }
        bypass(false);
    }

    void end() {
        range_ -= 2;
        low_ += range_;
        range_ = 2;
        renormalize();
        put((low_ >> 9) & 1);
        bits_.push_back((low_ >> 8) & 1);
        bits_.push_back(1);
    }

    void terminate(bool value) {
        range_ -= 2;
        if (!value) {
            renormalize();
            return;
        }
        low_ += range_;
        range_ = 2;
        renormalize();
        put((low_ >> 9) & 1);
        bits_.push_back((low_ >> 8) & 1);
        bits_.push_back(1);
    }

    const std::vector<CabacContextModel>& contexts() const {
        return contexts_;
    }

    void clear_trailing_bits(std::size_t bit_offset) {
        for (std::size_t index = bit_offset; index < bits_.size(); ++index) {
            bits_[index] = 0;
        }
    }

    std::vector<std::uint8_t> data(int padding = 4) const {
        std::vector<std::uint8_t> result((bits_.size() + 7) / 8);
        for (std::size_t index = 0; index < bits_.size(); ++index) {
            if (bits_[index]) {
                result[index / 8] |= 0x80 >> (index % 8);
            }
        }
        result.insert(result.end(), padding, 0);
        return result;
    }

private:
    void put(std::uint8_t bit) {
        if (first_bit_) {
            first_bit_ = false;
        } else {
            bits_.push_back(bit);
        }
        while (bits_to_follow_) {
            bits_.push_back(bit ^ 1);
            --bits_to_follow_;
        }
    }

    void renormalize() {
        while (range_ < 256) {
            if (low_ < 256) {
                put(0);
            } else if (low_ >= 512) {
                put(1);
                low_ -= 512;
            } else {
                ++bits_to_follow_;
                low_ -= 256;
            }
            low_ <<= 1;
            range_ <<= 1;
        }
    }

    std::vector<CabacContextModel> contexts_;
    std::uint32_t low_ = 0;
    std::uint32_t range_ = 510;
    std::uint32_t bits_to_follow_ = 0;
    std::vector<std::uint8_t> bits_;
    bool first_bit_ = true;
};

std::vector<std::uint8_t> encode_picture_parameter_set(
    std::uint32_t pps_id, bool cabac, std::uint32_t weighted_bipred_idc,
    std::uint32_t sps_id = 0, bool transform_8x8_mode = false,
    std::int32_t pic_init_qp_minus26 = 0) {
    BitWriter writer;
    writer.ue(pps_id);
    writer.ue(sps_id);
    writer.bits(cabac, 1);
    writer.bits(0, 1);
    for (int index = 0; index < 3; ++index) {
        writer.ue(0);
    }
    writer.bits(1, 1);
    writer.bits(weighted_bipred_idc, 2);
    writer.se(pic_init_qp_minus26);
    writer.se(0);
    writer.se(0);
    writer.bits(0b100, 3);
    if (transform_8x8_mode) {
        writer.bits(1, 1);
        writer.bits(0, 1);
        writer.se(0);
    }
    return writer.nal(0x68);
}

std::vector<std::uint8_t> sequence_parameter_set(
    std::uint32_t sps_id, std::uint32_t poc_type,
    bool transform_bypass, bool scaling_matrices,
    bool gaps_allowed = false, bool vui = false) {
    BitWriter writer;
    writer.bits(100, 8);
    writer.bits(0, 8);
    writer.bits(20, 8);
    writer.ue(sps_id);
    writer.ue(1);
    writer.ue(0);
    writer.ue(0);
    writer.bits(transform_bypass, 1);
    writer.bits(scaling_matrices, 1);
    if (scaling_matrices) {
        for (int index = 0; index < 8; ++index) {
            const bool present = index == 0 || index == 6;
            writer.bits(present, 1);
            if (present) {
                writer.se(index < 6 ? 4 : 12);
                for (int item = 1; item < (index < 6 ? 16 : 64); ++item) {
                    writer.se(0);
                }
            }
        }
    }
    writer.ue(0);
    writer.ue(poc_type);
    if (poc_type == 0) {
        writer.ue(0);
    } else if (poc_type == 1) {
        writer.bits(0, 1);
        writer.se(0);
        writer.se(0);
        writer.ue(1);
        writer.se(2);
    }
    writer.ue(2);
    writer.bits(gaps_allowed, 1);
    writer.ue(3);
    writer.ue(3);
    writer.bits(0b110, 3);
    writer.bits(vui, 1);
    if (vui) {
        writer.bits(1, 1);
        writer.bits(1, 8);
        writer.bits(0, 1);
        writer.bits(1, 1);
        writer.bits(5, 3);
        writer.bits(0, 1);
        writer.bits(1, 1);
        writer.bits(1, 8);
        writer.bits(1, 8);
        writer.bits(1, 8);
        writer.bits(0, 1);
        writer.bits(1, 1);
        writer.bits(1, 32);
        writer.bits(120, 32);
        writer.bits(1, 1);
        writer.bits(0, 4);
    }
    return writer.nal(0x67);
}

void write_prediction_weight_table(
    BitWriter& writer, int reference_count, bool explicit_weights) {
    writer.ue(0);
    writer.ue(0);
    for (int index = 0; index < reference_count; ++index) {
        if (explicit_weights) {
            writer.bits(1, 1);
            writer.se(2);
            writer.se(-16);
            writer.bits(1, 1);
            writer.se(2);
            writer.se(8);
            writer.se(2);
            writer.se(-8);
        } else {
            writer.bits(0, 1);
            writer.bits(0, 1);
        }
    }
}

void write_p_macroblock(
    BitWriter& writer, int kind,
    const std::vector<std::array<std::int32_t, 2>>& vectors,
    const std::vector<int>& references = {},
    bool multiple_references = false,
    const std::vector<int>& sub_types = {}) {
    writer.ue(kind);
    if (kind == 3 || kind == 4) {
        for (const int sub_type : sub_types) {
            writer.ue(sub_type);
        }
    }
    for (std::size_t partition = 0; partition < vectors.size(); ++partition) {
        if (multiple_references && kind != 4) {
            writer.bits(references[partition] == 0, 1);
        }
        writer.se(vectors[partition][0]);
        writer.se(vectors[partition][1]);
    }
    writer.ue(0);
}

std::vector<std::uint8_t> sized_nal(BitWriter& writer, std::uint8_t header) {
    const auto nal = writer.nal(header);
    const auto size = static_cast<std::uint32_t>(nal.size());
    std::vector<std::uint8_t> sample;
    for (int shift = 24; shift >= 0; shift -= 8) {
        sample.push_back(static_cast<std::uint8_t>(size >> shift));
    }
    sample.insert(sample.end(), nal.begin(), nal.end());
    return sample;
}

std::vector<std::uint8_t> encode_p_frame(std::uint16_t frame_index) {
    BitWriter writer;
    writer.ue(0);
    writer.ue(0);
    writer.ue(0);
    writer.bits((frame_index - 1) & 15, 4);
    const bool multiple_references = frame_index == 16 || frame_index == 17;
    writer.bits(multiple_references, 1);
    const int reference_count = multiple_references ? 2 : 1;
    if (multiple_references) {
        writer.ue(1);
    }
    const bool reordered = frame_index == 17;
    writer.bits(reordered, 1);
    if (reordered) {
        writer.ue(0);
        writer.ue(1);
        writer.ue(3);
    }
    write_prediction_weight_table(writer, reference_count, frame_index == 18);
    writer.bits(reordered, 1);
    if (reordered) {
        writer.ue(1);
        writer.ue(0);
        writer.ue(0);
    }
    writer.ue(0);
    writer.ue(1);
    if (frame_index == 10) {
        for (int macroblock = 0; macroblock < 16; ++macroblock) {
            writer.ue(0);
            write_p_macroblock(writer, 0, {{{0, 0}}});
        }
    } else if (frame_index == 11) {
        writer.ue(0);
        write_p_macroblock(writer, 0, {{{0, 0}}});
        writer.ue(14);
        write_p_macroblock(writer, 0, {{{0, 0}}});
    } else if (frame_index == 12) {
        const std::array<std::array<std::int32_t, 2>, 5> motion{{
            {{0, 0}}, {{4, 0}}, {{-4, 0}}, {{0, 4}}, {{0, -4}},
        }};
        for (int macroblock = 0; macroblock < 16; ++macroblock) {
            writer.ue(0);
            write_p_macroblock(writer, 0, {motion[macroblock % motion.size()]});
        }
    } else if (frame_index == 13) {
        for (int macroblock = 0; macroblock < 16; ++macroblock) {
            writer.ue(0);
            write_p_macroblock(writer, 1 + macroblock % 2,
                               {{{4, 0}}, {{-4, 4}}});
        }
    } else if (frame_index == 14) {
        const std::vector<int> sub_types{0, 1, 2, 3};
        const std::array<int, 4> counts{{1, 2, 2, 4}};
        for (int macroblock = 0; macroblock < 16; ++macroblock) {
            writer.ue(0);
            std::vector<std::array<std::int32_t, 2>> vectors;
            for (std::size_t sub = 0; sub < sub_types.size(); ++sub) {
                for (int part = 0; part < counts[sub_types[sub]]; ++part) {
                    vectors.push_back({{(static_cast<int>(sub) - 1) * 4,
                                        (part - 1) * 4}});
                }
            }
            write_p_macroblock(writer, 3 + macroblock % 2, vectors,
                               {}, false, sub_types);
        }
    } else if (frame_index == 15) {
        const std::array<std::array<std::int32_t, 2>, 4> motion{{
            {{1, 0}}, {{-1, 2}}, {{2, -1}}, {{-2, 1}},
        }};
        for (int macroblock = 0; macroblock < 16; ++macroblock) {
            writer.ue(0);
            write_p_macroblock(writer, 0, {motion[macroblock % 4]});
        }
    } else if (frame_index == 16 || frame_index == 17) {
        for (int macroblock = 0; macroblock < 16; ++macroblock) {
            writer.ue(0);
            write_p_macroblock(writer, 0, {{{0, 0}}}, {macroblock % 2}, true);
        }
    } else if (frame_index == 19) {
        for (int macroblock = 0; macroblock < 16; ++macroblock) {
            writer.ue(0);
            write_p_macroblock(writer, 0, {{{4, 0}}});
        }
    } else {
        for (int macroblock = 0; macroblock < 16; ++macroblock) {
            writer.ue(0);
            write_p_macroblock(writer, 0, {{{0, 0}}});
        }
    }
    return sized_nal(writer, 0x21);
}

std::vector<std::uint8_t> encode_b_frame() {
    BitWriter writer;
    writer.ue(0);
    writer.ue(1);
    writer.ue(0);
    writer.bits(2, 4);
    writer.bits(1, 1);
    writer.bits(0, 1);
    writer.bits(0, 1);
    writer.bits(0, 1);
    writer.ue(0);
    writer.ue(1);
    for (int macroblock = 0; macroblock < 16; ++macroblock) {
        writer.ue(0);
        writer.ue(1 + macroblock % 3);
        for (int list = 0; list < (macroblock % 3 == 2 ? 2 : 1); ++list) {
            writer.se(0);
            writer.se(0);
        }
        writer.ue(0);
    }
    return sized_nal(writer, 0x01);
}

void write_b_macroblock(BitWriter& writer, int kind,
                        const std::vector<int>& sub_types = {}) {
    writer.ue(kind);
    if (kind == 22) {
        for (const auto sub_type : sub_types) {
            writer.ue(sub_type);
        }
        writer.ue(0);
        return;
    }
    if (kind == 0) {
        writer.ue(0);
        return;
    }
    if (kind >= 1 && kind <= 3) {
        for (int list_index = 0; list_index < 2; ++list_index) {
            if ((kind == 1 && list_index == 1) ||
                (kind == 2 && list_index == 0)) {
                continue;
            }
            writer.se(0);
            writer.se(0);
        }
        writer.ue(0);
        return;
    }
    if (kind >= 4 && kind <= 21) {
        constexpr std::array<std::array<int, 2>, 18> flags_by_kind{{
            {{1, 0}}, {{1, 0}}, {{0, 1}}, {{0, 1}}, {{1, 0}},
            {{1, 0}}, {{0, 1}}, {{0, 1}}, {{1, 1}}, {{1, 1}},
            {{0, 1}}, {{0, 1}}, {{1, 1}}, {{1, 1}}, {{0, 1}},
            {{0, 1}}, {{1, 1}}, {{1, 1}},
        }};
        for (int partition = 0; partition < 2; ++partition) {
            for (const auto active : flags_by_kind[kind - 4]) {
                if (active) {
                    writer.se(0);
                    writer.se(0);
                }
            }
        }
        writer.ue(0);
    }
}

void begin_b_slice(BitWriter& writer, int frame_num, bool spatial_direct,
                   int pps_id = 0, bool explicit_weights = false) {
    writer.ue(0);
    writer.ue(1);
    writer.ue(pps_id);
    writer.bits(frame_num, 4);
    writer.bits(spatial_direct, 1);
    writer.bits(0, 1);
    writer.bits(0, 1);
    writer.bits(0, 1);
    if (explicit_weights) {
        writer.ue(0);
        writer.ue(0);
        for (int list = 0; list < 2; ++list) {
            writer.bits(1, 1);
            writer.se(2);
            writer.se(-8);
            writer.bits(1, 1);
            writer.se(2);
            writer.se(4);
            writer.se(2);
            writer.se(-4);
        }
    }
    writer.bits(0, 1);
    writer.ue(0);
    writer.ue(1);
}

std::vector<std::uint8_t> encode_b_feature_frame(int frame_index) {
    BitWriter writer;
    const bool spatial_direct = frame_index != 22;
    begin_b_slice(writer, (frame_index - 16) & 15, spatial_direct);
    for (int macroblock = 0; macroblock < 16; ++macroblock) {
        writer.ue(0);
        if (frame_index == 23) {
            write_b_macroblock(writer, 1 + (macroblock % 11) % 3);
        } else if (frame_index == 21 && macroblock % 2) {
            write_b_macroblock(writer, 22, {0, 0, 0, 0});
        } else {
            write_b_macroblock(writer, 0);
        }
    }
    return sized_nal(writer, 0x21);
}

std::vector<std::uint8_t> encode_b_weighted_frame(bool explicit_weights) {
    const int pps_id = explicit_weights ? 1 : 2;
    BitWriter writer;
    begin_b_slice(writer, explicit_weights ? 9 : 10, true, pps_id,
                  explicit_weights);
    for (int macroblock = 0; macroblock < 16; ++macroblock) {
        writer.ue(0);
        write_b_macroblock(writer, 3);
    }
    return sized_nal(writer, 0x21);
}

std::vector<std::uint8_t> encode_inter_residual_frame() {
    BitWriter writer;
    writer.ue(0);
    writer.ue(0);
    writer.ue(0);
    writer.bits(11, 4);
    writer.bits(0, 1);
    writer.bits(0, 1);
    write_prediction_weight_table(writer, 1, false);
    writer.bits(0, 1);
    writer.ue(0);
    writer.ue(1);
    for (int macroblock = 0; macroblock < 16; ++macroblock) {
        writer.ue(0);
        writer.ue(0);
        writer.se(0);
        writer.se(0);
        if (macroblock == 0 || macroblock == 15) {
            writer.ue(2);
            writer.se(1);
            for (int block = 0; block < 4; ++block) {
                if (block == 0 || block == 3) {
                    writer.bits(0b01, 2);
                    writer.bits(0, 1);
                    writer.bits(1, 1);
                } else {
                    writer.bits(1, 1);
                }
            }
        } else {
            writer.ue(0);
        }
    }
    return sized_nal(writer, 0x21);
}

std::vector<std::uint8_t> cabac_nal(
    std::uint8_t header, const std::vector<std::uint8_t>& rbsp) {
    std::vector<std::uint8_t> result{header};
    int zeros = 0;
    for (const auto value : rbsp) {
        if (zeros == 2 && value <= 3) {
            result.push_back(3);
            zeros = 0;
        }
        result.push_back(value);
        zeros = value == 0 ? zeros + 1 : 0;
    }
    return result;
}

std::vector<std::uint8_t> sample_nal(std::vector<std::uint8_t> nal) {
    std::vector<std::uint8_t> sample;
    const auto size = static_cast<std::uint32_t>(nal.size());
    for (int shift = 24; shift >= 0; shift -= 8) {
        sample.push_back(static_cast<std::uint8_t>(size >> shift));
    }
    sample.insert(sample.end(), nal.begin(), nal.end());
    return sample;
}

std::vector<std::uint8_t> finish_cabac_frame(
    BitWriter& header, CabacWriter& cabac) {
    cabac.end();
    auto rbsp = header.data;
    const auto cabac_data = cabac.data();
    rbsp.insert(rbsp.end(), cabac_data.begin(), cabac_data.end());
    return sample_nal(cabac_nal(0x21, rbsp));
}

std::vector<std::uint8_t> encode_cabac_i_frame() {
    BitWriter header;
    header.ue(0);
    header.ue(2);
    header.ue(3);
    header.bits(12, 4);
    header.bits(0, 1);
    header.ue(0);
    header.ue(1);
    while (header.bit_count % 8) {
        header.bits(1, 1);
    }
    CabacWriter cabac(make_cabac_slice_contexts(2, 0, 26));
    for (int macroblock = 0; macroblock < 16; ++macroblock) {
        cabac.decision(false, 3);
        for (int block = 0; block < 16; ++block) {
            cabac.decision(true, 68);
        }
        cabac.decision(false, 64);
        cabac.zero_luma_cbp(
            macroblock % 4 == 0 ? 15 : 0, macroblock < 4 ? 15 : 0);
        cabac.decision(false, 77);
        if (macroblock != 15) {
            cabac.terminate(false);
        }
    }
    return finish_cabac_frame(header, cabac);
}

std::vector<std::uint8_t> encode_cabac_p_frame() {
    BitWriter header;
    header.ue(0);
    header.ue(0);
    header.ue(3);
    header.bits(13, 4);
    header.bits(0, 1);
    header.bits(0, 1);
    header.ue(0);
    header.ue(0);
    header.bits(0, 1);
    header.bits(0, 1);
    header.bits(0, 1);
    header.ue(0);
    header.ue(0);
    header.ue(1);
    while (header.bit_count % 8) {
        header.bits(1, 1);
    }
    CabacWriter cabac(make_cabac_slice_contexts(0, 0, 26));
    for (int macroblock = 0; macroblock < 16; ++macroblock) {
        const bool skipped = macroblock % 3 == 1;
        const bool left_skip =
            macroblock % 4 != 0 && (macroblock - 1) % 3 == 1;
        const bool top_skip =
            macroblock >= 4 && (macroblock - 4) % 3 == 1;
        const int increment =
            static_cast<int>(macroblock % 4 != 0 && !left_skip) +
            static_cast<int>(macroblock >= 4 && !top_skip);
        cabac.decision(skipped, 11 + increment);
        if (skipped) {
            if (macroblock != 15) {
                cabac.terminate(false);
            }
            continue;
        }
        cabac.decision(false, 14);
        cabac.decision(false, 15);
        cabac.decision(false, 16);
        cabac.decision(false, 40);
        cabac.decision(false, 47);
        cabac.zero_luma_cbp(
            macroblock % 4 == 0 ? 15 : 0, macroblock < 4 ? 15 : 0);
        cabac.decision(false, 77);
        if (macroblock != 15) {
            cabac.terminate(false);
        }
    }
    return finish_cabac_frame(header, cabac);
}

std::vector<std::uint8_t> encode_cabac_b_frame() {
    BitWriter header;
    header.ue(0);
    header.ue(1);
    header.ue(3);
    header.bits(14, 4);
    header.bits(1, 1);
    header.bits(0, 1);
    header.bits(0, 1);
    header.bits(0, 1);
    header.bits(0, 1);
    header.ue(0);
    header.ue(0);
    header.ue(1);
    while (header.bit_count % 8) {
        header.bits(1, 1);
    }
    CabacWriter cabac(make_cabac_slice_contexts(1, 0, 26));
    for (int macroblock = 0; macroblock < 16; ++macroblock) {
        cabac.decision(
            false, 24 + static_cast<int>(macroblock % 4 != 0) +
                       static_cast<int>(macroblock >= 4));
        cabac.decision(false, 27);
        cabac.zero_luma_cbp(
            macroblock % 4 == 0 ? 15 : 0, macroblock < 4 ? 15 : 0);
        cabac.decision(false, 77);
        if (macroblock != 15) {
            cabac.terminate(false);
        }
    }
    return finish_cabac_frame(header, cabac);
}

std::vector<std::uint8_t> encode_cabac_residual_frame() {
    BitWriter header;
    header.ue(0);
    header.ue(2);
    header.ue(3);
    header.bits(15, 4);
    header.bits(0, 1);
    header.ue(0);
    header.ue(1);
    while (header.bit_count % 8) {
        header.bits(1, 1);
    }
    CabacWriter cabac(make_cabac_slice_contexts(2, 0, 26));
    for (int macroblock = 0; macroblock < 16; ++macroblock) {
        cabac.decision(false, 3);
        for (int block = 0; block < 16; ++block) {
            cabac.decision(true, 68);
        }
        cabac.decision(false, 64);
        cabac.luma_cbp(
            macroblock % 4 == 0 ? 15 : 0, macroblock < 4 ? 15 : 0,
            macroblock == 15 ? 1 : 0);
        cabac.decision(false, 77);
        if (macroblock == 15) {
            cabac.decision(false, 60);
            cabac.decision(true, 93);
            cabac.decision(true, 134);
            cabac.decision(true, 195);
            cabac.decision(false, 248);
            cabac.bypass(false);
            cabac.decision(false, 94);
            cabac.decision(false, 95);
            cabac.decision(false, 93);
        }
        if (macroblock != 15) {
            cabac.terminate(false);
        }
    }
    return finish_cabac_frame(header, cabac);
}

std::vector<std::uint8_t> encode_cabac_i_slice(
    std::uint32_t first_macroblock, std::uint32_t macroblock_count,
    std::uint32_t pps_id, std::int32_t qp_delta,
    std::uint32_t disable_deblocking, std::int32_t alpha_offset = 0,
    std::int32_t beta_offset = 0, bool transform_8x8 = false,
    bool explicit_prediction_modes = false, bool idr = false) {
    BitWriter header;
    header.ue(first_macroblock);
    header.ue(2);
    header.ue(pps_id);
    header.bits(0, 4);
    if (idr) {
        header.ue(0);
        header.bits(0, 2);
    } else {
        header.bits(0, 1);
    }
    header.se(qp_delta);
    header.ue(disable_deblocking);
    if (disable_deblocking != 1) {
        header.se(alpha_offset);
        header.se(beta_offset);
    }
    while (header.bit_count % 8) {
        header.bits(1, 1);
    }

    CabacWriter cabac(make_cabac_slice_contexts(2, 0, 26 + qp_delta));
    for (std::uint32_t macroblock = 0; macroblock < macroblock_count; ++macroblock) {
        cabac.decision(false, 3);
        const bool use_8x8 = transform_8x8;
        if (transform_8x8) {
            cabac.decision(true, 399 +
                static_cast<std::size_t>(macroblock % 4 != 0) +
                static_cast<std::size_t>(macroblock >= 4));
        }
        const int block_count = use_8x8 ? 4 : 16;
        for (int block = 0; block < block_count; ++block) {
            const std::uint8_t desired = explicit_prediction_modes
                ? (macroblock >= 4 ? block
                    : macroblock != 0 ? 1 + block % 2 : 2)
                : 2;
            if (desired != 2) {
                cabac.decision(false, 68);
                const int remaining = desired - (desired > 2 ? 1 : 0);
                for (int bit = 0; bit < 3; ++bit) {
                    cabac.decision((remaining >> bit) & 1, 69);
                }
            } else {
                cabac.decision(true, 68);
            }
        }
        cabac.decision(false, 64);
        const int left = macroblock % 4 == 0 ? 15 : 0;
        const int top = macroblock < 4 ? 15 : 0;
        const bool has_residual = false;
        cabac.luma_cbp(left, top, has_residual ? 1 : 0);
        cabac.decision(false, 77);
        if (has_residual) {
            cabac.decision(false, 60);
            cabac.decision(true, 93);
            cabac.decision(true, 134);
            cabac.decision(true, 195);
            cabac.decision(false, 248);
            cabac.bypass(false);
            cabac.decision(false, 94);
            cabac.decision(false, 95);
            cabac.decision(false, 93);
        }

        if (macroblock + 1 != macroblock_count) {
            cabac.terminate(false);
        }
    }
    cabac.end();
    auto rbsp = header.data;
    const auto data = cabac.data();
    rbsp.insert(rbsp.end(), data.begin(), data.end());
    return cabac_nal(idr ? 0x65 : 0x21, rbsp);
}

std::vector<std::uint8_t> encode_cavlc_i_slice(
    std::uint32_t first_macroblock, std::uint32_t macroblock_count,
    std::int32_t qp_delta, std::uint32_t disable_deblocking,
    std::int32_t alpha_offset, std::int32_t beta_offset) {
    BitWriter writer;
    writer.ue(first_macroblock);
    writer.ue(2);
    writer.ue(9);
    writer.bits(0, 4);
    writer.bits(0, 1);
    writer.se(qp_delta);
    writer.ue(disable_deblocking);
    if (disable_deblocking != 1) {
        writer.se(alpha_offset);
        writer.se(beta_offset);
    }
    for (std::uint32_t index = 0; index < macroblock_count; ++index) {
        writer.ue(0);
        writer.bits(0xffff, 16);
        writer.ue(0);
        writer.ue(3);
    }
    return writer.nal(0x21);
}

std::vector<std::uint8_t> sample_nals(
    const std::vector<std::vector<std::uint8_t>>& nals) {
    std::vector<std::uint8_t> sample;
    for (const auto& nal : nals) {
        const auto value = static_cast<std::uint32_t>(nal.size());
        for (int shift = 24; shift >= 0; shift -= 8) {
            sample.push_back(static_cast<std::uint8_t>(value >> shift));
        }
        sample.insert(sample.end(), nal.begin(), nal.end());
    }
    return sample;
}

std::vector<std::uint8_t> marker_nal(std::uint8_t header,
                                      std::vector<std::uint8_t> payload = {}) {
    payload.push_back(0x80);
    return cabac_nal(header, payload);
}

} // namespace

std::vector<std::uint8_t> H264Encoder::sequence_parameter_set() const {
    return ::pygorvid::sequence_parameter_set(
        0, 2, false, false, false, false);
}

std::vector<std::uint8_t> H264Encoder::picture_parameter_set() const {
    return encode_picture_parameter_set(0, false, 0);
}

std::vector<std::uint8_t> H264Encoder::encode_grey_frame(std::uint16_t idr_pic_id) const {
    return encode_pcm_frame(idr_pic_id, false);
}

std::vector<std::uint8_t> H264Encoder::encode_generated_frame(std::uint16_t frame_index) const {
    if (frame_index == 3) {
        BitWriter writer;
        for (const auto value : {0u, 2u, 0u}) {
            writer.ue(value);
        }
        writer.bits(2, 4);
        writer.bits(0, 1);
        writer.ue(0);
        writer.ue(1);
        for (int macroblock = 0; macroblock < 16; ++macroblock) {
            writer.ue(3);
            writer.ue(0);
            writer.ue(0);
            writer.bits(1, 1);
        }
        const auto nal = writer.nal(0x21);
        const auto size = static_cast<std::uint32_t>(nal.size());
        std::vector<std::uint8_t> sample;
        for (int shift = 24; shift >= 0; shift -= 8) {
            sample.push_back(static_cast<std::uint8_t>(size >> shift));
        }
        sample.insert(sample.end(), nal.begin(), nal.end());
        return sample;
    }
    if (frame_index == 4) {
        BitWriter writer;
        for (const auto value : {0u, 2u, 0u}) {
            writer.ue(value);
        }
        writer.bits(3, 4);
        writer.bits(0, 1);
        writer.ue(0);
        writer.ue(1);
        const std::array<std::uint32_t, 16> luma_modes{
            2, 2, 2, 2, 0, 3, 2, 1, 0, 0, 1, 2, 0, 3, 2, 1};
        const std::array<std::uint32_t, 16> chroma_modes{
            0, 1, 1, 1, 2, 3, 0, 1, 2, 2, 3, 0, 2, 3, 2, 1};
        for (std::size_t index = 0; index < luma_modes.size(); ++index) {
            writer.ue(1 + luma_modes[index] + (index == 0 ? 8 : 0));
            writer.ue(chroma_modes[index]);
            writer.ue(0);
            if (index == 0) {
                writer.bits(0b01000011, 8);
                writer.bits(0b10000, 5);
                writer.bits(0b10000, 5);
                writer.bits(0xff, 8);
            } else {
                writer.bits(1, 1);
            }
        }
        const auto nal = writer.nal(0x21);
        const auto size = static_cast<std::uint32_t>(nal.size());
        std::vector<std::uint8_t> sample;
        for (int shift = 24; shift >= 0; shift -= 8) {
            sample.push_back(static_cast<std::uint8_t>(size >> shift));
        }
        sample.insert(sample.end(), nal.begin(), nal.end());
        return sample;
    }
    if (frame_index == 5) {
        BitWriter writer;
        for (const auto value : {0u, 2u, 0u}) {
            writer.ue(value);
        }
        writer.bits(4, 4);
        writer.bits(0, 1);
        writer.ue(0);
        writer.ue(1);
        for (int macroblock = 0; macroblock < 16; ++macroblock) {
            writer.ue(0);
            for (int block = 0; block < 16; ++block) {
                writer.bits(1, 1);
            }
            writer.ue(0);
            writer.ue(3);
        }
        const auto nal = writer.nal(0x21);
        const auto size = static_cast<std::uint32_t>(nal.size());
        std::vector<std::uint8_t> sample;
        for (int shift = 24; shift >= 0; shift -= 8) {
            sample.push_back(static_cast<std::uint8_t>(size >> shift));
        }
        sample.insert(sample.end(), nal.begin(), nal.end());
        return sample;
    }
    if (frame_index >= 6 && frame_index <= 9) {
        BitWriter writer;
        for (const auto value : {0u, 2u, 0u}) {
            writer.ue(value);
        }
        writer.bits(frame_index == 6 ? 5 : frame_index - 1, 4);
        writer.bits(0, 1);
        writer.ue(0);
        writer.ue(1);

        writer.ue(25);
        writer.align();
        for (int row = 0; row < 16; ++row) {
            for (int column = 0; column < 16; ++column) {
                writer.data.push_back(
                    static_cast<std::uint8_t>(32 + 3 * column + 5 * row));
            }
        }
        for (const int base : {80, 176}) {
            for (int row = 0; row < 8; ++row) {
                for (int column = 0; column < 8; ++column) {
                    writer.data.push_back(
                        static_cast<std::uint8_t>(base + column + 2 * row));
                }
            }
        }
        writer.bit_count += 384 * 8;

        const std::array<int, 16> modes{
            2, 2, 2, 4, 8, 1, 5, 6, 7, 0, 3, 4, 5, 7, 6, 8};
        const std::array<int, 16> scan{
            0, 1, 4, 5, 2, 3, 6, 7, 8, 9, 12, 13, 10, 11, 14, 15};
        const auto scan_index = [&scan](int raster) {
            const auto found = std::find(scan.begin(), scan.end(), raster);
            if (found == scan.end()) {
                throw std::logic_error("invalid Intra_4x4 raster index");
            }
            return static_cast<std::size_t>(found - scan.begin());
        };
        std::array<std::array<int, 16>, 16> macroblock_modes{};
        macroblock_modes[0].fill(2);
        for (int macroblock = 1; macroblock < 16; ++macroblock) {
            writer.ue(0);
            const int mb_x = macroblock % 4;
            const int mb_y = macroblock / 4;
            for (std::size_t block = 0; block < modes.size(); ++block) {
                const int raster = scan[block];
                const int column = raster % 4;
                const int row = raster / 4;
                int left_mode = -1;
                int top_mode = -1;
                if (column) {
                    left_mode = modes[scan_index(raster - 1)];
                } else if (mb_x) {
                    left_mode = macroblock_modes[macroblock - 1][scan_index(row * 4 + 3)];
                }
                if (row) {
                    top_mode = modes[scan_index(raster - 4)];
                } else if (mb_y) {
                    top_mode = macroblock_modes[macroblock - 4][scan_index(12 + column)];
                }
                const int predicted = left_mode >= 0 && top_mode >= 0
                    ? std::min(left_mode, top_mode) : 2;
                const int desired = modes[block];
                if (desired == predicted) {
                    writer.bits(1, 1);
                } else {
                    writer.bits(0, 1);
                    writer.bits(desired - (desired >= predicted ? 1 : 0), 3);
                }
            }
            writer.ue(0);
            if (frame_index >= 7 && macroblock == 15) {
                writer.ue(frame_index >= 8 ? 42 : 29);
                writer.se(1);
                if (frame_index == 9) {
                    // nC = 0: sixteen alternating +2/-2 coefficients.
                    writer.bits(0b0000000000000100, 16);
                    for (int index = 0; index < 16; ++index) {
                        writer.bits(index % 2 == 0 ? 2 : 3, 2);
                    }
                    // nC >= 8: three trailing ones and two total zeros.
                    writer.bits(0b001011, 6);
                    writer.bits(0b010, 3);
                    writer.bits(0b110, 3);
                    writer.bits(0b010, 3);
                    // nC >= 8 followed by nC = 2 select different empty-block tables.
                    writer.bits(0b000011, 6);
                    writer.bits(0b11, 2);
                } else {
                    // Positive trailing-one token, zero runs, then three empty blocks.
                    writer.bits(0b01, 2);
                    writer.bits(0, 1);
                    writer.bits(1, 1);
                    writer.bits(1, 1);
                    writer.bits(1, 1);
                    writer.bits(1, 1);
                }
                if (frame_index >= 8) {
                    // Cb DC +1 and Cr DC -1, each at the sole 2x2 DC coefficient.
                    writer.bits(0b101, 3);
                    writer.bits(0b111, 3);
                    // One Cb and one Cr AC coefficient; remaining AC blocks are empty.
                    for (const std::uint32_t sign : {0u, 1u}) {
                        writer.bits(0b01, 2);
                        writer.bits(sign, 1);
                        writer.bits(1, 1);
                        writer.bits(0b111, 3);
                    }
                }
            } else {
                writer.ue(3);
            }
            macroblock_modes[macroblock] = modes;
        }
        const auto nal = writer.nal(0x21);
        const auto size = static_cast<std::uint32_t>(nal.size());
        std::vector<std::uint8_t> sample;
        for (int shift = 24; shift >= 0; shift -= 8) {
            sample.push_back(static_cast<std::uint8_t>(size >> shift));
        }
        sample.insert(sample.end(), nal.begin(), nal.end());
        return sample;
    }
    if (frame_index >= 10 && frame_index <= 19) {
        return encode_p_frame(frame_index);
    }
    if (frame_index == 20) {
        return encode_b_frame();
    }
    if (frame_index >= 21 && frame_index <= 23) {
        return encode_b_feature_frame(frame_index);
    }
    if (frame_index == 24) {
        return encode_b_weighted_frame(true);
    }
    if (frame_index == 25) {
        return encode_b_weighted_frame(false);
    }
    if (frame_index == 26) {
        return encode_inter_residual_frame();
    }
    if (frame_index == 27) {
        return encode_cabac_i_frame();
    }
    if (frame_index == 28) {
        return encode_cabac_p_frame();
    }
    if (frame_index == 29) {
        return encode_cabac_b_frame();
    }
    if (frame_index == 30) {
        return encode_cabac_residual_frame();
    }
    if (frame_index == 31) {
        return sample_nals({encode_cabac_i_slice(
            0, 16, 4, 0, 0, 2, -3)});
    }
    if (frame_index == 32) {
        return sample_nals({
            encode_cavlc_i_slice(0, 8, 0, 0, 2, -2),
            encode_cavlc_i_slice(8, 8, 0, 1, 0, 0),
        });
    }
    if (frame_index == 33) {
        return sample_nals({
            encode_cavlc_i_slice(0, 8, 1, 0, 1, -1),
            encode_cavlc_i_slice(8, 8, -1, 0, 1, -1),
        });
    }
    if (frame_index == 34) {
        BitWriter writer;
        for (const auto value : {0u, 2u, 6u}) writer.ue(value);
        writer.bits(0, 4);
        writer.ue(0);
        writer.se(0);
        writer.bits(0, 2);
        writer.se(0);
        writer.ue(0);
        writer.se(0);
        writer.se(0);
        for (int macroblock = 0; macroblock < 16; ++macroblock) {
            writer.ue(25);
            writer.align();
            writer.data.insert(writer.data.end(), 384, 128);
            writer.bit_count += 384 * 8;
        }
        return sample_nals({writer.nal(0x65)});
    }
    if (frame_index == 35) {
        return sample_nals({encode_cabac_i_slice(
            0, 16, 5, 0, 0, 0, 0, true)});
    }
    if (frame_index == 36) {
        return sample_nals({encode_cabac_i_slice(
            0, 16, 5, 0, 0, 0, 0, true, true)});
    }
    if (frame_index == 37) {
        return sample_nals({encode_cabac_i_slice(
            0, 16, 7, 0, 0, 0, 0, true, false, true)});
    }
    if (frame_index == 38) {
        return sample_nals({encode_cabac_i_slice(
            0, 16, 8, 0, 0, 0, 0, true, false, true)});
    }
    if (frame_index == 39) {
        return sample_nals({
            marker_nal(0x09, {0x10}),
            marker_nal(0x06, {5, 0x12, 'P', 'Y', 'G', 'O', 'R', 'V', 'I',
                              'D', '-', 'F', 'R', 'A', 'M', 'E', '-', '3',
                              '9', '!'}),
            encode_cabac_i_slice(0, 16, 8, 0, 0, 0, 0, true, false, true),
        });
    }
    if (frame_index == 59) {
        auto coded = encode_pcm_frame(frame_index, false);
        const auto stream_end = sample_nals({
            marker_nal(0x0a),
            marker_nal(0x0b),
        });
        coded.insert(coded.end(), stream_end.begin(), stream_end.end());
        return coded;
    }
    return encode_pcm_frame(frame_index, frame_index == 1);
}

std::vector<std::uint8_t> H264Encoder::encode_pcm_frame(
    std::uint16_t idr_pic_id, bool emulation_prevention_test) const {
    BitWriter writer;
    for (const auto value : {0u, 2u, 0u}) {
        writer.ue(value);
    }
    const bool non_idr_reference = idr_pic_id == 2;
    if (non_idr_reference) {
        writer.bits(1, 4);
        writer.bits(0, 1);
    } else {
        writer.bits(0, 4);
        writer.ue(idr_pic_id);
        writer.bits(0, 2);
    }
    writer.ue(0);
    writer.ue(1);
    for (int macroblock = 0; macroblock < 16; ++macroblock) {
        writer.ue(25);
        writer.align();
        if (emulation_prevention_test && macroblock == 0) {
            writer.data.insert(writer.data.end(), {0, 0, 1});
            writer.data.insert(writer.data.end(), 253, 128);
        } else {
            writer.data.insert(writer.data.end(), 256, 128);
        }
        writer.data.insert(writer.data.end(), 128, 128);
        writer.bit_count += 384 * 8;
    }
    const auto nal = writer.nal(non_idr_reference ? 0x21 : 0x65);
    const auto size = static_cast<std::uint32_t>(nal.size());
    std::vector<std::uint8_t> sample;
    for (int shift = 24; shift >= 0; shift -= 8) {
        sample.push_back(static_cast<std::uint8_t>(size >> shift));
    }
    sample.insert(sample.end(), nal.begin(), nal.end());
    return sample;
}

} // namespace pygorvid
