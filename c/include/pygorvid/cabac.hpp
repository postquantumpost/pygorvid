#ifndef PYGORVID_CABAC_HPP
#define PYGORVID_CABAC_HPP

#include "pygorvid/pygorvid.hpp"

#include <array>
#include <cstdint>
#include <vector>

namespace pygorvid {

class CabacContextModel {
public:
    CabacContextModel(std::int32_t m, std::int32_t n, std::int32_t slice_qpy);

    std::uint8_t state_index() const noexcept;
    bool mps() const noexcept;
    void update(bool bin_value) noexcept;

private:
    friend class CabacArithmeticDecoder;

    std::uint8_t state_index_;
    bool value_mps_;
};

std::vector<CabacContextModel> make_cabac_slice_contexts(
    std::uint8_t slice_type, std::uint8_t cabac_init_idc,
    std::int32_t slice_qpy);

struct CabacResidualBlock {
    std::array<std::int32_t, 16> levels{};
    bool coded = false;
};

class CabacArithmeticDecoder {
public:
    explicit CabacArithmeticDecoder(BitReader& bits);

    std::uint32_t code_range() const noexcept;
    std::uint32_t code_offset() const noexcept;
    bool terminated() const noexcept;

    bool decode_bin(CabacContextModel& model);
    bool decode_bypass_bin();
    bool decode_terminate_bin();
    void reset_after_ipcm();
    bool decode_p_skip_flag(
        std::uint8_t slice_type, bool left_available, bool left_skipped,
        bool top_available, bool top_skipped,
        std::vector<CabacContextModel>& contexts);
    bool decode_b_skip_flag(
        bool left_available, bool left_skipped, bool top_available,
        bool top_skipped, std::vector<CabacContextModel>& contexts);
    std::uint8_t decode_p_mb_type(
        std::uint8_t slice_type,
        std::vector<CabacContextModel>& contexts);
    std::uint8_t decode_b_mb_type(
        std::size_t context_increment,
        std::vector<CabacContextModel>& contexts);
    std::uint8_t decode_p_sub_mb_type(
        std::vector<CabacContextModel>& contexts);
    std::uint8_t decode_b_sub_mb_type(
        std::vector<CabacContextModel>& contexts);
    std::uint8_t decode_reference_index(
        std::uint32_t maximum_reference_index,
        std::size_t context_increment,
        std::vector<CabacContextModel>& contexts);
    std::int32_t decode_motion_vector_difference(
        std::uint8_t component, std::uint64_t neighbor_magnitude,
        std::vector<CabacContextModel>& contexts);
    std::uint8_t decode_i_intra_mb_type(
        std::uint8_t slice_type,
        std::vector<CabacContextModel>& contexts,
        bool left_available, bool left_intra16_or_pcm,
        bool top_available, bool top_intra16_or_pcm);
    std::uint8_t decode_intra_chroma_pred_mode(
        bool left_has_nonzero_mode, bool top_has_nonzero_mode,
        std::vector<CabacContextModel>& contexts);
    std::uint8_t decode_intra4x4_pred_mode(
        std::uint8_t predicted_mode,
        std::vector<CabacContextModel>& contexts);
    std::uint8_t decode_luma_coded_block_pattern(
        std::uint8_t left_cbp, std::uint8_t top_cbp,
        std::vector<CabacContextModel>& contexts);
    std::uint8_t decode_chroma_coded_block_pattern(
        std::uint8_t left_cbp, std::uint8_t top_cbp,
        std::vector<CabacContextModel>& contexts);
    std::int32_t decode_mb_qp_delta(
        std::int32_t previous_delta,
        std::vector<CabacContextModel>& contexts);
    CabacResidualBlock decode_residual_block(
        std::uint8_t ctx_block_cat, bool cond_term_flag_a,
        bool cond_term_flag_b,
        std::vector<CabacContextModel>& contexts);
    std::array<std::int32_t, 64> decode_luma8x8_residual_block(
        std::vector<CabacContextModel>& contexts);

private:
    void validate_bin_state() const;
    void renormalize(std::uint32_t& code_range,
                     std::uint32_t& code_offset);

    BitReader& bits_;
    std::uint32_t code_range_ = 510;
    std::uint32_t code_offset_ = 0;
    bool terminated_ = false;
};

} // namespace pygorvid

#endif
