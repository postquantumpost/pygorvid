#include "pygorvid/cabac.hpp"

#include "cabac_context_init.hpp"

#include <algorithm>
#include <array>
#include <limits>
#include <stdexcept>

namespace pygorvid {
namespace {

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

constexpr std::array<std::uint8_t, 64> transition_mps{{
    1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21,
    22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40,
    41, 42, 43, 44, 45, 46, 47, 48, 49, 50, 51, 52, 53, 54, 55, 56, 57, 58, 59,
    60, 61, 62, 62
}};

constexpr std::array<std::uint8_t, 64> transition_lps{{
    0, 0, 1, 2, 2, 4, 4, 5, 6, 7, 8, 9, 9, 11, 11, 12, 13, 13, 15, 15, 16, 16,
    18, 18, 19, 19, 21, 21, 22, 22, 23, 24, 24, 25, 26, 26, 27, 27, 28, 29, 29,
    30, 30, 30, 31, 32, 32, 33, 33, 33, 34, 34, 35, 35, 35, 36, 36, 36, 37, 37,
    37, 38, 38, 63
}};

constexpr std::array<std::size_t, 5> residual_significant_offset{{
    0, 15, 29, 44, 47
}};
constexpr std::array<std::size_t, 5> residual_level_offset{{
    0, 10, 20, 30, 39
}};
constexpr std::array<std::size_t, 5> residual_max_coefficients{{
    16, 15, 16, 4, 15
}};

constexpr std::array<std::size_t, 63> luma8x8_significant_increment{{
    0, 1, 2, 3, 4, 5, 5, 4, 4, 3, 3, 4, 4, 4, 5, 5,
    4, 4, 4, 4, 3, 3, 6, 7, 7, 7, 8, 9, 10, 9, 8, 7,
    7, 6, 11, 12, 13, 11, 6, 7, 8, 9, 14, 10, 9, 8, 6,
    11, 12, 13, 11, 6, 9, 14, 10, 9, 11, 12, 13, 11, 14, 10, 12,
}};

constexpr std::array<std::size_t, 63> luma8x8_last_increment{{
    0, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
    2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2,
    3, 3, 3, 3, 3, 3, 3, 3, 4, 4, 4, 4, 4, 4, 4, 4,
    5, 5, 5, 5, 6, 6, 6, 6, 7, 7, 7, 7, 8, 8, 8,
}};

void validate_context_parameters(std::int32_t m, std::int32_t n,
                                 std::int32_t slice_qpy) {
    if (m < -128 || m > 127 || n < -128 || n > 127 ||
        slice_qpy < 0 || slice_qpy > 51) {
        throw std::invalid_argument(
            "CABAC context initialization value is outside its valid range");
    }
}

} // namespace

CabacContextModel::CabacContextModel(std::int32_t m, std::int32_t n,
                                     std::int32_t slice_qpy)
    : state_index_(0), value_mps_(false) {
    validate_context_parameters(m, n, slice_qpy);
    const auto pre_context_state =
        std::clamp(((m * slice_qpy) >> 4) + n, 1, 126);
    if (pre_context_state <= 63) {
        state_index_ = static_cast<std::uint8_t>(63 - pre_context_state);
    } else {
        state_index_ = static_cast<std::uint8_t>(pre_context_state - 64);
        value_mps_ = true;
    }
}

std::uint8_t CabacContextModel::state_index() const noexcept {
    return state_index_;
}

bool CabacContextModel::mps() const noexcept {
    return value_mps_;
}

void CabacContextModel::update(bool bin_value) noexcept {
    if (bin_value == value_mps_) {
        if (state_index_ < 62) {
            state_index_ = transition_mps[state_index_];
        }
        return;
    }
    if (state_index_ == 0) {
        value_mps_ = !value_mps_;
    }
    state_index_ = transition_lps[state_index_];
}

std::vector<CabacContextModel> make_cabac_slice_contexts(
    std::uint8_t slice_type, std::uint8_t cabac_init_idc,
    std::int32_t slice_qpy) {
    if (slice_type > 9) {
        throw std::invalid_argument("CABAC syntax is unsupported for this slice type");
    }
    const auto normalized_slice_type = slice_type % 5;
    const auto intra_slice =
        normalized_slice_type == 2 || normalized_slice_type == 4;
    if (!intra_slice && cabac_init_idc > 2) {
        throw std::invalid_argument("CABAC init idc is outside [0,2]");
    }
    const auto column = intra_slice
        ? std::uint8_t{0}
        : static_cast<std::uint8_t>(cabac_init_idc + 1);
    std::vector<CabacContextModel> contexts;
    contexts.reserve(460);
    for (std::size_t context_index = 0; context_index < 460;
         ++context_index) {
        contexts.emplace_back(
            detail::cabac_context_init_table[column][context_index][0],
            detail::cabac_context_init_table[column][context_index][1],
            slice_qpy);
    }
    return contexts;
}

CabacArithmeticDecoder::CabacArithmeticDecoder(BitReader& bits) : bits_(bits) {
    const auto start_bit_offset = bits_.bit_offset();
    try {
        code_offset_ = bits_.read_bits(9);
        if (code_offset_ >= code_range_) {
            throw std::invalid_argument(
                "CABAC initial code offset must be less than 510");
        }
    } catch (...) {
        bits_.restore_bit_offset(start_bit_offset);
        throw;
    }
}

std::uint32_t CabacArithmeticDecoder::code_range() const noexcept {
    return code_range_;
}

std::uint32_t CabacArithmeticDecoder::code_offset() const noexcept {
    return code_offset_;
}

bool CabacArithmeticDecoder::terminated() const noexcept {
    return terminated_;
}

bool CabacArithmeticDecoder::decode_bin(CabacContextModel& model) {
    validate_bin_state();
    if (model.state_index_ >= 64) {
        throw std::invalid_argument("CABAC context state index is outside [0,63]");
    }

    const auto start_bit_offset = bits_.bit_offset();
    const auto range_lps_value = range_lps[(code_range_ >> 6) & 3]
                                          [model.state_index_];
    auto next_range = code_range_ - range_lps_value;
    auto next_offset = code_offset_;
    const auto decoded = next_offset >= next_range ? !model.value_mps_
                                                    : model.value_mps_;
    if (decoded != model.value_mps_) {
        next_offset -= next_range;
        next_range = range_lps_value;
    }

    auto next_model = model;
    try {
        renormalize(next_range, next_offset);
    } catch (...) {
        bits_.restore_bit_offset(start_bit_offset);
        throw;
    }
    next_model.update(decoded);
    code_range_ = next_range;
    code_offset_ = next_offset;
    model = next_model;
    return decoded;
}

bool CabacArithmeticDecoder::decode_bypass_bin() {
    validate_bin_state();
    const auto start_bit_offset = bits_.bit_offset();
    try {
        const auto next_offset = (code_offset_ << 1) |
                                 static_cast<std::uint32_t>(bits_.read_bit());
        const auto decoded = next_offset >= code_range_;
        code_offset_ = decoded ? next_offset - code_range_ : next_offset;
        return decoded;
    } catch (...) {
        bits_.restore_bit_offset(start_bit_offset);
        throw;
    }
}

bool CabacArithmeticDecoder::decode_terminate_bin() {
    validate_bin_state();
    auto next_range = code_range_ - 2;
    auto next_offset = code_offset_;
    if (next_offset >= next_range) {
        code_range_ = next_range;
        terminated_ = true;
        return true;
    }

    const auto start_bit_offset = bits_.bit_offset();
    try {
        renormalize(next_range, next_offset);
    } catch (...) {
        bits_.restore_bit_offset(start_bit_offset);
        throw;
    }
    code_range_ = next_range;
    code_offset_ = next_offset;
    return false;
}

void CabacArithmeticDecoder::reset_after_ipcm() {
    const auto start_bit_offset = bits_.bit_offset();
    try {
        const auto next_offset = bits_.read_bits(9);
        if (next_offset >= 510) {
            throw std::invalid_argument(
                "CABAC I_PCM restart offset must be less than 510");
        }
        code_range_ = 510;
        code_offset_ = next_offset;
        terminated_ = false;
    } catch (...) {
        bits_.restore_bit_offset(start_bit_offset);
        throw;
    }
}

bool CabacArithmeticDecoder::decode_p_skip_flag(
    std::uint8_t slice_type, bool left_available, bool left_skipped,
    bool top_available, bool top_skipped,
    std::vector<CabacContextModel>& contexts) {
    if (slice_type > 9 || slice_type % 5 != 0) {
        throw std::invalid_argument(
            "mb_skip_flag is unsupported for this slice type");
    }
    if (contexts.size() < 14) {
        throw std::invalid_argument(
            "P mb_skip_flag requires contexts 11 through 13");
    }
    const auto context_increment =
        static_cast<std::size_t>(left_available && !left_skipped) +
        static_cast<std::size_t>(top_available && !top_skipped);
    return decode_bin(contexts[11 + context_increment]);
}

bool CabacArithmeticDecoder::decode_b_skip_flag(
    bool left_available, bool left_skipped, bool top_available,
    bool top_skipped, std::vector<CabacContextModel>& contexts) {
    if (contexts.size() < 27) {
        throw std::invalid_argument(
            "B mb_skip_flag requires contexts 24 through 26");
    }
    const auto context_increment =
        static_cast<std::size_t>(left_available && !left_skipped) +
        static_cast<std::size_t>(top_available && !top_skipped);
    return decode_bin(contexts[24 + context_increment]);
}

std::uint8_t CabacArithmeticDecoder::decode_p_mb_type(
    std::uint8_t slice_type,
    std::vector<CabacContextModel>& contexts) {
    if (slice_type > 9 || slice_type % 5 != 0) {
        throw std::invalid_argument("P mb_type is unsupported for this slice type");
    }
    if (contexts.size() < 21) {
        throw std::invalid_argument(
            "P mb_type requires contexts 14 through 20");
    }

    const auto original_range = code_range_;
    const auto original_offset = code_offset_;
    const auto original_terminated = terminated_;
    const auto original_bit_offset = bits_.bit_offset();
    auto next_contexts = contexts;
    try {
        std::uint8_t macroblock_type = 0;
        if (decode_bin(next_contexts[14])) {
            std::uint8_t intra_type = 0;
            if (decode_bin(next_contexts[17])) {
                if (decode_terminate_bin()) {
                    intra_type = 25;
                } else {
                    intra_type = static_cast<std::uint8_t>(
                        1 + 12 * decode_bin(next_contexts[18]));
                    if (decode_bin(next_contexts[19])) {
                        intra_type += static_cast<std::uint8_t>(
                            4 + 4 * decode_bin(next_contexts[19]));
                    }
                    intra_type += static_cast<std::uint8_t>(
                        2 * decode_bin(next_contexts[20]));
                    intra_type += static_cast<std::uint8_t>(
                        decode_bin(next_contexts[20]));
                }
            }
            macroblock_type = static_cast<std::uint8_t>(5 + intra_type);
        } else {
            const auto second = decode_bin(next_contexts[15]);
            const auto third =
                decode_bin(next_contexts[second ? 17 : 16]);
            if (second && third) {
                macroblock_type = 1;
            } else if (second) {
                macroblock_type = 2;
            } else if (third) {
                macroblock_type = 3;
            }
        }
        contexts = std::move(next_contexts);
        return macroblock_type;
    } catch (...) {
        code_range_ = original_range;
        code_offset_ = original_offset;
        terminated_ = original_terminated;
        bits_.restore_bit_offset(original_bit_offset);
        throw;
    }
}

std::uint8_t CabacArithmeticDecoder::decode_b_mb_type(
    std::size_t context_increment,
    std::vector<CabacContextModel>& contexts) {
    if (context_increment > 3 || contexts.size() < 35) {
        throw std::invalid_argument(
            "B mb_type requires context increment 0..3 and contexts 27 through 34");
    }

    const auto original_range = code_range_;
    const auto original_offset = code_offset_;
    const auto original_terminated = terminated_;
    const auto original_bit_offset = bits_.bit_offset();
    auto next_contexts = contexts;
    try {
        std::uint8_t macroblock_type = 0;
        if (decode_bin(next_contexts[27 + context_increment])) {
            if (!decode_bin(next_contexts[30])) {
                macroblock_type = static_cast<std::uint8_t>(
                    1 + decode_bin(next_contexts[32]));
            } else {
                const auto third = decode_bin(next_contexts[31]);
                const auto rest = static_cast<std::uint8_t>(
                    (decode_bin(next_contexts[32]) << 2) |
                    (decode_bin(next_contexts[32]) << 1) |
                    decode_bin(next_contexts[32]));
                if (!third) {
                    macroblock_type = static_cast<std::uint8_t>(3 + rest);
                } else if (rest == 5) {
                    std::uint8_t intra_type = 0;
                    if (!decode_bin(next_contexts[32])) {
                        intra_type = 0;
                    } else if (decode_terminate_bin()) {
                        intra_type = 25;
                    } else {
                        intra_type = static_cast<std::uint8_t>(
                            1 + 12 * decode_bin(next_contexts[33]));
                        if (decode_bin(next_contexts[34])) {
                            intra_type += static_cast<std::uint8_t>(
                                4 + 4 * decode_bin(next_contexts[34]));
                        }
                        intra_type += static_cast<std::uint8_t>(
                            2 * decode_bin(next_contexts[34]));
                        intra_type += static_cast<std::uint8_t>(
                            decode_bin(next_contexts[34]));
                    }
                    macroblock_type =
                        static_cast<std::uint8_t>(23 + intra_type);
                } else if (rest == 6) {
                    macroblock_type = 11;
                } else if (rest == 7) {
                    macroblock_type = 22;
                } else {
                    macroblock_type = static_cast<std::uint8_t>(
                        12 + (rest << 1) +
                        decode_bin(next_contexts[32]));
                }
            }
        }
        contexts = std::move(next_contexts);
        return macroblock_type;
    } catch (...) {
        code_range_ = original_range;
        code_offset_ = original_offset;
        terminated_ = original_terminated;
        bits_.restore_bit_offset(original_bit_offset);
        throw;
    }
}

std::uint8_t CabacArithmeticDecoder::decode_p_sub_mb_type(
    std::vector<CabacContextModel>& contexts) {
    if (contexts.size() < 24) {
        throw std::invalid_argument(
            "P sub_mb_type requires contexts 21 through 23");
    }

    const auto original_range = code_range_;
    const auto original_offset = code_offset_;
    const auto original_terminated = terminated_;
    const auto original_bit_offset = bits_.bit_offset();
    auto next_contexts = contexts;
    try {
        std::uint8_t sub_macroblock_type = 0;
        if (!decode_bin(next_contexts[21])) {
            if (!decode_bin(next_contexts[22])) {
                sub_macroblock_type = 1;
            } else {
                sub_macroblock_type = static_cast<std::uint8_t>(
                    3 - decode_bin(next_contexts[23]));
            }
        }
        contexts = std::move(next_contexts);
        return sub_macroblock_type;
    } catch (...) {
        code_range_ = original_range;
        code_offset_ = original_offset;
        terminated_ = original_terminated;
        bits_.restore_bit_offset(original_bit_offset);
        throw;
    }
}

std::uint8_t CabacArithmeticDecoder::decode_b_sub_mb_type(
    std::vector<CabacContextModel>& contexts) {
    if (contexts.size() < 40) {
        throw std::invalid_argument(
            "B sub_mb_type requires contexts 36 through 39");
    }

    const auto original_range = code_range_;
    const auto original_offset = code_offset_;
    const auto original_terminated = terminated_;
    const auto original_bit_offset = bits_.bit_offset();
    auto next_contexts = contexts;
    try {
        std::uint8_t sub_macroblock_type = 0;
        if (decode_bin(next_contexts[36])) {
            if (!decode_bin(next_contexts[37])) {
                sub_macroblock_type = static_cast<std::uint8_t>(
                    1 + decode_bin(next_contexts[39]));
            } else if (!decode_bin(next_contexts[38])) {
                sub_macroblock_type = static_cast<std::uint8_t>(
                    3 + (decode_bin(next_contexts[39]) << 1) +
                    decode_bin(next_contexts[39]));
            } else if (decode_bin(next_contexts[39])) {
                sub_macroblock_type = static_cast<std::uint8_t>(
                    11 + decode_bin(next_contexts[39]));
            } else {
                sub_macroblock_type = static_cast<std::uint8_t>(
                    7 + (decode_bin(next_contexts[39]) << 1) +
                    decode_bin(next_contexts[39]));
            }
        }
        contexts = std::move(next_contexts);
        return sub_macroblock_type;
    } catch (...) {
        code_range_ = original_range;
        code_offset_ = original_offset;
        terminated_ = original_terminated;
        bits_.restore_bit_offset(original_bit_offset);
        throw;
    }
}

std::uint8_t CabacArithmeticDecoder::decode_reference_index(
    std::uint32_t maximum_reference_index, std::size_t context_increment,
    std::vector<CabacContextModel>& contexts) {
    if (maximum_reference_index > 31 || context_increment > 3 ||
        contexts.size() < 60) {
        throw std::invalid_argument(
            "ref_idx requires a maximum in [0,31], context increment in [0,3], "
            "and contexts 54 through 59");
    }
    if (maximum_reference_index == 0) {
        return 0;
    }

    const auto original_range = code_range_;
    const auto original_offset = code_offset_;
    const auto original_terminated = terminated_;
    const auto original_bit_offset = bits_.bit_offset();
    auto next_contexts = contexts;
    try {
        std::uint8_t reference_index = 0;
        if (decode_bin(next_contexts[54 + context_increment])) {
            reference_index = 1;
            while (decode_bin(next_contexts[reference_index == 1 ? 58 : 59])) {
                if (reference_index >= maximum_reference_index) {
                    throw std::runtime_error(
                        "CABAC ref_idx exceeds the active reference maximum");
                }
                ++reference_index;
            }
        }
        contexts = std::move(next_contexts);
        return reference_index;
    } catch (...) {
        code_range_ = original_range;
        code_offset_ = original_offset;
        terminated_ = original_terminated;
        bits_.restore_bit_offset(original_bit_offset);
        throw;
    }
}

std::int32_t CabacArithmeticDecoder::decode_motion_vector_difference(
    std::uint8_t component, std::uint64_t neighbor_magnitude,
    std::vector<CabacContextModel>& contexts) {
    if (component > 1 || contexts.size() < 54) {
        throw std::invalid_argument(
            "MVD requires component 0 or 1 and contexts 40 through 53");
    }
    if (neighbor_magnitude > 32) {
        neighbor_magnitude = 33;
    }
    const auto context_base = component == 0 ? 40U : 47U;
    auto context_index = static_cast<std::size_t>(
        (neighbor_magnitude >= 3 ? 1 : 0) +
        (neighbor_magnitude >= 33 ? 1 : 0));
    const auto original_range = code_range_;
    const auto original_offset = code_offset_;
    const auto original_terminated = terminated_;
    const auto original_bit_offset = bits_.bit_offset();
    auto next_contexts = contexts;
    try {
        std::uint64_t magnitude = 0;
        if (decode_bin(next_contexts[context_base + context_index])) {
            magnitude = 1;
            context_index = 3;
            while (magnitude < 9) {
                if (!decode_bin(
                        next_contexts[context_base + context_index])) {
                    break;
                }
                if (magnitude < 4) {
                    ++context_index;
                }
                ++magnitude;
            }
            if (magnitude >= 9) {
                std::uint32_t suffix_length = 3;
                while (decode_bypass_bin()) {
                    if (suffix_length > 30) {
                        throw std::runtime_error(
                            "CABAC MVD exceeds the signed output range");
                    }
                    const auto increment =
                        std::uint64_t{1} << suffix_length;
                    if (magnitude >
                        static_cast<std::uint64_t>(
                            std::numeric_limits<std::int32_t>::max()) -
                            increment) {
                        throw std::runtime_error(
                            "CABAC MVD exceeds the signed output range");
                    }
                    magnitude += increment;
                    ++suffix_length;
                }
                if (suffix_length > 30) {
                    throw std::runtime_error(
                        "CABAC MVD exceeds the signed output range");
                }
                for (auto bit_index = suffix_length; bit_index > 0;
                     --bit_index) {
                    if (decode_bypass_bin()) {
                        const auto increment =
                            std::uint64_t{1} << (bit_index - 1);
                        if (magnitude >
                            static_cast<std::uint64_t>(
                                std::numeric_limits<std::int32_t>::max()) -
                                increment) {
                            throw std::runtime_error(
                                "CABAC MVD exceeds the signed output range");
                        }
                        magnitude += increment;
                    }
                }
            }
            const auto negative = decode_bypass_bin();
            const auto signed_magnitude =
                static_cast<std::int32_t>(magnitude);
            contexts = std::move(next_contexts);
            return negative ? -signed_magnitude : signed_magnitude;
        }
        contexts = std::move(next_contexts);
        return 0;
    } catch (...) {
        code_range_ = original_range;
        code_offset_ = original_offset;
        terminated_ = original_terminated;
        bits_.restore_bit_offset(original_bit_offset);
        throw;
    }
}

std::uint8_t CabacArithmeticDecoder::decode_i_intra_mb_type(
    std::uint8_t slice_type, std::vector<CabacContextModel>& contexts,
    bool left_available, bool left_intra16_or_pcm,
    bool top_available, bool top_intra16_or_pcm) {
    if (slice_type > 9 || slice_type % 5 != 2) {
        throw std::invalid_argument(
            "I-slice mb_type is unsupported for this slice type");
    }
    if (contexts.size() < 11) {
        throw std::invalid_argument(
            "I-slice mb_type requires contexts 3 through 10");
    }

    const auto original_range = code_range_;
    const auto original_offset = code_offset_;
    const auto original_terminated = terminated_;
    const auto original_bit_offset = bits_.bit_offset();
    auto next_contexts = contexts;
    try {
        const auto first_context =
            static_cast<std::size_t>(left_available && left_intra16_or_pcm) +
            static_cast<std::size_t>(top_available && top_intra16_or_pcm);
        const auto first = decode_bin(next_contexts[first_context + 3]);
        std::uint8_t macroblock_type = 0;
        if (first) {
            if (decode_terminate_bin()) {
                macroblock_type = 25;
            } else {
                macroblock_type = static_cast<std::uint8_t>(
                    1 + 12 * decode_bin(next_contexts[6]));
                if (decode_bin(next_contexts[7])) {
                    macroblock_type += static_cast<std::uint8_t>(
                        4 + 4 * decode_bin(next_contexts[8]));
                }
                macroblock_type += static_cast<std::uint8_t>(
                    2 * decode_bin(next_contexts[9]));
                macroblock_type += static_cast<std::uint8_t>(
                    decode_bin(next_contexts[10]));
            }
        }
        contexts = std::move(next_contexts);
        return macroblock_type;
    } catch (...) {
        code_range_ = original_range;
        code_offset_ = original_offset;
        terminated_ = original_terminated;
        bits_.restore_bit_offset(original_bit_offset);
        throw;
    }
}

std::uint8_t CabacArithmeticDecoder::decode_intra_chroma_pred_mode(
    bool left_has_nonzero_mode, bool top_has_nonzero_mode,
    std::vector<CabacContextModel>& contexts) {
    if (contexts.size() < 68) {
        throw std::invalid_argument(
            "intra_chroma_pred_mode requires contexts 64 through 67");
    }
    const auto original_range = code_range_;
    const auto original_offset = code_offset_;
    const auto original_terminated = terminated_;
    const auto original_bit_offset = bits_.bit_offset();
    auto next_contexts = contexts;
    try {
        const auto first_context =
            64 + static_cast<std::size_t>(left_has_nonzero_mode) +
            static_cast<std::size_t>(top_has_nonzero_mode);
        std::uint8_t mode = 0;
        if (decode_bin(next_contexts[first_context])) {
            if (!decode_bin(next_contexts[67])) {
                mode = 1;
            } else if (decode_bin(next_contexts[67])) {
                mode = 3;
            } else {
                mode = 2;
            }
        }
        contexts = std::move(next_contexts);
        return mode;
    } catch (...) {
        code_range_ = original_range;
        code_offset_ = original_offset;
        terminated_ = original_terminated;
        bits_.restore_bit_offset(original_bit_offset);
        throw;
    }
}

std::uint8_t CabacArithmeticDecoder::decode_intra4x4_pred_mode(
    std::uint8_t predicted_mode,
    std::vector<CabacContextModel>& contexts) {
    if (predicted_mode > 8 || contexts.size() < 70) {
        throw std::invalid_argument(
            "intra4x4 prediction mode requires a mode in [0,8] and contexts 68-69");
    }
    const auto original_range = code_range_;
    const auto original_offset = code_offset_;
    const auto original_terminated = terminated_;
    const auto original_bit_offset = bits_.bit_offset();
    auto next_contexts = contexts;
    try {
        if (decode_bin(next_contexts[68])) {
            contexts = std::move(next_contexts);
            return predicted_mode;
        }
        std::uint8_t remainder = 0;
        for (std::uint8_t bit_index = 0; bit_index < 3; ++bit_index) {
            remainder |= static_cast<std::uint8_t>(
                decode_bin(next_contexts[69]) << bit_index);
        }
        const auto mode = static_cast<std::uint8_t>(
            remainder + static_cast<std::uint8_t>(remainder >= predicted_mode));
        contexts = std::move(next_contexts);
        return mode;
    } catch (...) {
        code_range_ = original_range;
        code_offset_ = original_offset;
        terminated_ = original_terminated;
        bits_.restore_bit_offset(original_bit_offset);
        throw;
    }
}

std::uint8_t CabacArithmeticDecoder::decode_luma_coded_block_pattern(
    std::uint8_t left_cbp, std::uint8_t top_cbp,
    std::vector<CabacContextModel>& contexts) {
    if (left_cbp > 15 || top_cbp > 15 || contexts.size() < 77) {
        throw std::invalid_argument(
            "luma coded_block_pattern requires values in [0,15] and contexts 73-76");
    }
    const auto original_range = code_range_;
    const auto original_offset = code_offset_;
    const auto original_terminated = terminated_;
    const auto original_bit_offset = bits_.bit_offset();
    auto next_contexts = contexts;
    try {
        std::uint8_t pattern = 0;
        auto context_index =
            static_cast<std::size_t>((left_cbp & 0x02) == 0) +
            2 * static_cast<std::size_t>((top_cbp & 0x04) == 0);
        pattern |= static_cast<std::uint8_t>(
            decode_bin(next_contexts[73 + context_index]));

        context_index = static_cast<std::size_t>((pattern & 0x01) == 0) +
                        2 * static_cast<std::size_t>((top_cbp & 0x08) == 0);
        pattern |= static_cast<std::uint8_t>(
                       decode_bin(next_contexts[73 + context_index]))
                   << 1;

        context_index = static_cast<std::size_t>((left_cbp & 0x08) == 0) +
                        2 * static_cast<std::size_t>((pattern & 0x01) == 0);
        pattern |= static_cast<std::uint8_t>(
                       decode_bin(next_contexts[73 + context_index]))
                   << 2;

        context_index = static_cast<std::size_t>((pattern & 0x04) == 0) +
                        2 * static_cast<std::size_t>((pattern & 0x02) == 0);
        pattern |= static_cast<std::uint8_t>(
                       decode_bin(next_contexts[73 + context_index]))
                   << 3;
        contexts = std::move(next_contexts);
        return pattern;
    } catch (...) {
        code_range_ = original_range;
        code_offset_ = original_offset;
        terminated_ = original_terminated;
        bits_.restore_bit_offset(original_bit_offset);
        throw;
    }
}

std::uint8_t CabacArithmeticDecoder::decode_chroma_coded_block_pattern(
    std::uint8_t left_cbp, std::uint8_t top_cbp,
    std::vector<CabacContextModel>& contexts) {
    if (left_cbp > 2 || top_cbp > 2 || contexts.size() < 85) {
        throw std::invalid_argument(
            "chroma coded_block_pattern requires values in [0,2] and contexts 77-84");
    }
    const auto original_range = code_range_;
    const auto original_offset = code_offset_;
    const auto original_terminated = terminated_;
    const auto original_bit_offset = bits_.bit_offset();
    auto next_contexts = contexts;
    try {
        auto context_index = static_cast<std::size_t>(left_cbp > 0) +
                             2 * static_cast<std::size_t>(top_cbp > 0);
        std::uint8_t pattern = 0;
        if (decode_bin(next_contexts[77 + context_index])) {
            context_index = 4 + static_cast<std::size_t>(left_cbp == 2) +
                            2 * static_cast<std::size_t>(top_cbp == 2);
            pattern = static_cast<std::uint8_t>(
                1 + decode_bin(next_contexts[77 + context_index]));
        }
        contexts = std::move(next_contexts);
        return pattern;
    } catch (...) {
        code_range_ = original_range;
        code_offset_ = original_offset;
        terminated_ = original_terminated;
        bits_.restore_bit_offset(original_bit_offset);
        throw;
    }
}

std::int32_t CabacArithmeticDecoder::decode_mb_qp_delta(
    std::int32_t previous_delta,
    std::vector<CabacContextModel>& contexts) {
    if (contexts.size() < 64) {
        throw std::invalid_argument("mb_qp_delta requires contexts 60 through 63");
    }
    const auto original_range = code_range_;
    const auto original_offset = code_offset_;
    const auto original_terminated = terminated_;
    const auto original_bit_offset = bits_.bit_offset();
    auto next_contexts = contexts;
    try {
        auto context_index =
            static_cast<std::size_t>(previous_delta != 0);
        const auto first = decode_bin(next_contexts[60 + context_index]);
        std::int32_t delta = 0;
        if (first) {
            std::uint32_t value = 1;
            context_index = 2;
            while (decode_bin(next_contexts[60 + context_index])) {
                if (value >= 102) {
                    throw std::runtime_error(
                        "CABAC mb_qp_delta exceeds the supported QP range");
                }
                ++value;
                context_index = 3;
            }
            const auto magnitude = static_cast<std::int32_t>((value + 1) >> 1);
            delta = (value & 1) == 0 ? -magnitude : magnitude;
        }
        contexts = std::move(next_contexts);
        return delta;
    } catch (...) {
        code_range_ = original_range;
        code_offset_ = original_offset;
        terminated_ = original_terminated;
        bits_.restore_bit_offset(original_bit_offset);
        throw;
    }
}

CabacResidualBlock CabacArithmeticDecoder::decode_residual_block(
    std::uint8_t ctx_block_cat, bool cond_term_flag_a,
    bool cond_term_flag_b, std::vector<CabacContextModel>& contexts) {
    if (ctx_block_cat > 4 || contexts.size() != 460) {
        throw std::invalid_argument(
            "residual block requires ctxBlockCat [0,4] and 460 contexts");
    }
    const auto category = static_cast<std::size_t>(ctx_block_cat);
    const auto coded_base = 85 + category * 4;
    const auto significant_base = 105 + residual_significant_offset[category];
    const auto last_base = 166 + residual_significant_offset[category];
    const auto level_base = 227 + residual_level_offset[category];
    const auto max_coefficients = residual_max_coefficients[category];
    const auto original_range = code_range_;
    const auto original_offset = code_offset_;
    const auto original_terminated = terminated_;
    const auto original_bit_offset = bits_.bit_offset();
    auto next_contexts = contexts;
    CabacResidualBlock result;
    try {
        const auto coded_context =
            coded_base + static_cast<std::size_t>(cond_term_flag_a) +
            2 * static_cast<std::size_t>(cond_term_flag_b);
        result.coded = decode_bin(next_contexts[coded_context]);
        if (result.coded) {
            std::array<bool, 16> significant{};
            bool last_found = false;
            for (std::size_t index = 0; index + 1 < max_coefficients; ++index) {
                const auto increment =
                    ctx_block_cat == 3 ? std::min<std::size_t>(index, 2) : index;
                if (!decode_bin(next_contexts[significant_base + increment])) {
                    continue;
                }
                significant[index] = true;
                if (decode_bin(next_contexts[last_base + increment])) {
                    last_found = true;
                    break;
                }
            }
            if (!last_found) {
                significant[max_coefficients - 1] = true;
            }

            std::size_t equal_one = 0;
            std::size_t greater_one = 0;
            const auto greater_cap = ctx_block_cat == 3 ? 3U : 4U;
            for (std::size_t index = max_coefficients; index-- > 0;) {
                if (!significant[index]) {
                    continue;
                }
                const auto first_increment =
                    greater_one == 0 ? std::min<std::size_t>(1 + equal_one, 4) : 0;
                const auto greater_increment =
                    5 + std::min<std::size_t>(greater_one, greater_cap);
                std::uint64_t absolute_level = 1;
                if (decode_bin(next_contexts[level_base + first_increment])) {
                    absolute_level = 2;
                    while (absolute_level < 15 &&
                           decode_bin(next_contexts[level_base + greater_increment])) {
                        ++absolute_level;
                    }
                    if (absolute_level == 15) {
                        std::uint32_t prefix_length = 0;
                        while (decode_bypass_bin()) {
                            if (++prefix_length >= 23) {
                                throw std::runtime_error(
                                    "CABAC coefficient level prefix exceeds 22");
                            }
                        }
                        std::uint64_t suffix = 0;
                        for (std::uint32_t bit_index = 0;
                             bit_index < prefix_length; ++bit_index) {
                            suffix = (suffix << 1) |
                                     static_cast<std::uint64_t>(decode_bypass_bin());
                        }
                        absolute_level =
                            14 + (std::uint64_t{1} << prefix_length) + suffix;
                    }
                }
                if (absolute_level > static_cast<std::uint64_t>(
                                         std::numeric_limits<std::int32_t>::max())) {
                    throw std::runtime_error(
                        "CABAC coefficient magnitude exceeds signed range");
                }
                const auto negative = decode_bypass_bin();
                const auto magnitude = static_cast<std::int32_t>(absolute_level);
                result.levels[index] = negative ? -magnitude : magnitude;
                if (absolute_level == 1) {
                    ++equal_one;
                } else {
                    ++greater_one;
                }
            }
        }
        contexts = std::move(next_contexts);
        return result;
    } catch (...) {
        code_range_ = original_range;
        code_offset_ = original_offset;
        terminated_ = original_terminated;
        bits_.restore_bit_offset(original_bit_offset);
        throw;
    }
}

void CabacArithmeticDecoder::validate_bin_state() const {
    if (terminated_) {
        throw std::runtime_error("CABAC decoder is already terminated");
    }
    if (code_range_ < 256 || code_range_ > 510) {
        throw std::runtime_error("CABAC code range is outside [256,510]");
    }
    if (code_offset_ >= code_range_) {
        throw std::runtime_error("CABAC code offset is outside the arithmetic range");
    }
}

void CabacArithmeticDecoder::renormalize(std::uint32_t& code_range,
                                         std::uint32_t& code_offset) {
    while (code_range < 256) {
        code_range <<= 1;
        code_offset = (code_offset << 1) |
                      static_cast<std::uint32_t>(bits_.read_bit());
        if (code_offset >= code_range) {
            throw std::runtime_error(
                "CABAC code offset is outside the arithmetic range");
        }
    }
}

std::array<std::int32_t, 64>
CabacArithmeticDecoder::decode_luma8x8_residual_block(
    std::vector<CabacContextModel>& contexts) {
    if (contexts.size() != 460) {
        throw std::invalid_argument(
            "8x8 residual block requires 460 slice contexts");
    }

    const auto original_range = code_range_;
    const auto original_offset = code_offset_;
    const auto original_terminated = terminated_;
    const auto original_bit_offset = bits_.bit_offset();
    auto next_contexts = contexts;
    std::array<std::int32_t, 64> levels{};
    try {
        std::array<bool, 64> significant{};
        bool last_found = false;
        for (std::size_t index = 0; index < 63; ++index) {
            const auto significant_context =
                402 + luma8x8_significant_increment[index];
            if (!decode_bin(next_contexts[significant_context])) {
                continue;
            }
            significant[index] = true;
            const auto last_context =
                417 + luma8x8_last_increment[index];
            if (decode_bin(next_contexts[last_context])) {
                last_found = true;
                break;
            }
        }
        if (!last_found) {
            significant[63] = true;
        }

        std::size_t equal_one = 0;
        std::size_t greater_one = 0;
        for (std::size_t index = 64; index-- > 0;) {
            if (!significant[index]) {
                continue;
            }
            const auto first_increment =
                greater_one == 0 ? std::min<std::size_t>(1 + equal_one, 4) : 0;
            const auto greater_increment =
                5 + std::min<std::size_t>(greater_one, 4);
            std::uint64_t absolute_level = 1;
            if (decode_bin(next_contexts[426 + first_increment])) {
                absolute_level = 2;
                while (absolute_level < 15 &&
                       decode_bin(next_contexts[426 + greater_increment])) {
                    ++absolute_level;
                }
                if (absolute_level == 15) {
                    std::uint32_t prefix_length = 0;
                    while (decode_bypass_bin()) {
                        if (++prefix_length >= 23) {
                            throw std::runtime_error(
                                "CABAC coefficient level prefix exceeds 22");
                        }
                    }
                    std::uint64_t suffix = 0;
                    for (std::uint32_t bit_index = 0;
                         bit_index < prefix_length; ++bit_index) {
                        suffix = (suffix << 1) |
                                 static_cast<std::uint64_t>(decode_bypass_bin());
                    }
                    absolute_level =
                        14 + (std::uint64_t{1} << prefix_length) + suffix;
                }
            }
            if (absolute_level > static_cast<std::uint64_t>(
                                     std::numeric_limits<std::int32_t>::max())) {
                throw std::runtime_error(
                    "CABAC coefficient magnitude exceeds signed range");
            }
            const auto negative = decode_bypass_bin();
            const auto magnitude = static_cast<std::int32_t>(absolute_level);
            levels[index] = negative ? -magnitude : magnitude;
            if (absolute_level == 1) {
                ++equal_one;
            } else {
                ++greater_one;
            }
        }
        contexts = std::move(next_contexts);
        return levels;
    } catch (...) {
        code_range_ = original_range;
        code_offset_ = original_offset;
        terminated_ = original_terminated;
        bits_.restore_bit_offset(original_bit_offset);
        throw;
    }
}

} // namespace pygorvid
