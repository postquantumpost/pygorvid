#ifndef PYGORVID_H264_HPP
#define PYGORVID_H264_HPP

#include "pygorvid/pygorvid.hpp"

#include <array>
#include <optional>

namespace pygorvid {

struct NalHeader {
    std::uint8_t reference_idc = 0;
    std::uint8_t unit_type = 0;
};

struct SpsInfo {
    std::uint8_t profile_idc = 0;
    std::uint8_t constraint_flags = 0;
    std::uint8_t level_idc = 0;
    std::uint32_t sps_id = 0;
    std::uint8_t chroma_format_idc = 1;
    bool separate_colour_plane = false;
    bool seq_intra_luma_4x4_scaling_list_present = false;
    bool seq_intra_chroma_4x4_scaling_list_present = false;
    bool seq_intra_luma_8x8_scaling_list_present = false;
    std::uint8_t bit_depth_luma = 8;
    std::uint8_t bit_depth_chroma = 8;
    std::uint32_t coded_width = 0;
    std::uint32_t coded_height = 0;
    std::uint32_t width = 0;
    std::uint32_t height = 0;
    std::uint32_t frame_crop_left = 0;
    std::uint32_t frame_crop_right = 0;
    std::uint32_t frame_crop_top = 0;
    std::uint32_t frame_crop_bottom = 0;
    std::uint32_t log2_max_frame_num_minus4 = 0;
    std::uint8_t pic_order_cnt_type = 0;
    std::optional<std::uint32_t> log2_max_pic_order_cnt_lsb_minus4;
    bool delta_pic_order_always_zero = false;
    std::optional<std::int64_t> offset_for_non_ref_pic;
    std::optional<std::int64_t> offset_for_top_to_bottom_field;
    std::vector<std::int64_t> offset_for_ref_frame;
    std::uint32_t max_num_ref_frames = 0;
    bool gaps_in_frame_num_value_allowed = false;
    bool frame_mbs_only = true;
    bool mb_adaptive_frame_field = false;
    bool direct_8x8_inference = true;
};

struct PpsInfo {
    std::uint32_t picture_parameter_set_id = 0;
    std::uint32_t sequence_parameter_set_id = 0;
    bool entropy_coding_mode = false;
    bool bottom_field_pic_order_in_frame_present = false;
    std::uint32_t num_slice_groups_minus1 = 0;
    std::uint32_t num_ref_idx_l0_default_active_minus1 = 0;
    std::uint32_t num_ref_idx_l1_default_active_minus1 = 0;
    bool weighted_pred = false;
    std::uint8_t weighted_bipred_idc = 0;
    std::int64_t pic_init_qp_minus26 = 0;
    std::int64_t pic_init_qs_minus26 = 0;
    std::int64_t chroma_qp_index_offset = 0;
    bool deblocking_filter_control_present = false;
    bool constrained_intra_pred = false;
    bool redundant_pic_cnt_present = false;
    bool has_extension = false;
    bool transform_8x8_mode = false;
    bool pic_scaling_matrix_present = false;
    std::int64_t second_chroma_qp_index_offset = 0;
};

struct IdrSliceHeader {
    std::uint32_t first_macroblock_in_slice = 0;
    std::uint8_t slice_type = 2;
    std::uint32_t picture_parameter_set_id = 0;
    std::uint32_t frame_num = 0;
    std::uint32_t idr_pic_id = 0;
    std::uint32_t pic_order_cnt_lsb = 0;
    std::int64_t delta_pic_order_bottom = 0;
    std::int64_t delta_pic_order_cnt0 = 0;
    std::int64_t delta_pic_order_cnt1 = 0;
    std::uint32_t redundant_pic_cnt = 0;
    bool no_output_of_prior_pics = false;
    bool long_term_reference = false;
    std::int64_t slice_qp_delta = 0;
    std::uint8_t disable_deblocking_filter_idc = 0;
    std::int64_t slice_alpha_c0_offset_div2 = 0;
    std::int64_t slice_beta_offset_div2 = 0;
    std::size_t slice_data_bit_offset = 0;
};

struct ReferenceListModification {
    std::uint8_t modification_of_pic_nums_idc = 3;
    std::uint32_t abs_diff_pic_num_minus1 = 0;
    std::uint32_t long_term_pic_num = 0;
};

struct PSliceHeader {
    std::uint32_t first_macroblock_in_slice = 0;
    std::uint8_t slice_type = 0;
    std::uint32_t picture_parameter_set_id = 0;
    std::uint32_t frame_num = 0;
    std::uint32_t pic_order_cnt_lsb = 0;
    std::int64_t delta_pic_order_bottom = 0;
    std::int64_t delta_pic_order_cnt0 = 0;
    std::int64_t delta_pic_order_cnt1 = 0;
    std::uint32_t redundant_pic_cnt = 0;
    bool direct_spatial_mv_pred = false;
    std::uint32_t num_ref_idx_l0_active_minus1 = 0;
    std::uint32_t num_ref_idx_l1_active_minus1 = 0;
    std::vector<ReferenceListModification> ref_pic_list_modifications_l0;
    std::vector<ReferenceListModification> ref_pic_list_modifications_l1;
    std::uint8_t cabac_init_idc = 0;
    std::int64_t slice_qp_delta = 0;
    std::uint8_t disable_deblocking_filter_idc = 0;
    std::int64_t slice_alpha_c0_offset_div2 = 0;
    std::int64_t slice_beta_offset_div2 = 0;
    std::size_t slice_data_bit_offset = 0;
};

struct Yuv420Frame {
    std::uint32_t width = 0;
    std::uint32_t height = 0;
    std::vector<std::uint8_t> luma;
    std::vector<std::uint8_t> chroma_u;
    std::vector<std::uint8_t> chroma_v;
};

NalHeader parse_nal_header(const std::vector<std::uint8_t>& nal);
std::vector<std::uint8_t> ebsp_to_rbsp(const std::vector<std::uint8_t>& ebsp);
std::vector<std::vector<std::uint8_t>> split_length_prefixed_nals(
    const std::vector<std::uint8_t>& data, std::size_t length_size);
std::vector<std::vector<std::uint8_t>> split_h264_sample_nals(
    const std::vector<std::uint8_t>& data, std::size_t length_size);
SpsInfo parse_sps(const std::vector<std::uint8_t>& nal);
PpsInfo parse_pps(const std::vector<std::uint8_t>& nal);
IdrSliceHeader parse_idr_i_slice_header(const std::vector<std::uint8_t>& nal,
                                        const SpsInfo& sps,
                                        const PpsInfo& pps);
PSliceHeader parse_p_slice_header(const std::vector<std::uint8_t>& nal,
                                  const SpsInfo& sps, const PpsInfo& pps);
Yuv420Frame decode_idr_i_pcm_frame(
    const std::vector<std::vector<std::uint8_t>>& slice_nals,
    const SpsInfo& sps, const PpsInfo& pps);
Yuv420Frame decode_idr_i_pcm_sample(const std::vector<std::uint8_t>& sample,
                                    std::size_t nal_length_size,
                                    const SpsInfo& sps, const PpsInfo& pps);
Yuv420Frame decode_idr_i_frame(
    const std::vector<std::vector<std::uint8_t>>& slice_nals,
    const SpsInfo& sps, const PpsInfo& pps);
Yuv420Frame decode_idr_i_sample(const std::vector<std::uint8_t>& sample,
                                std::size_t nal_length_size,
                                const SpsInfo& sps, const PpsInfo& pps);
Yuv420Frame decode_p_skip_frame(
    const std::vector<std::vector<std::uint8_t>>& slice_nals,
    const SpsInfo& sps, const PpsInfo& pps,
    const Yuv420Frame& reference_frame);
Yuv420Frame decode_p_frame(
    const std::vector<std::vector<std::uint8_t>>& slice_nals,
    const SpsInfo& sps, const PpsInfo& pps,
    const Yuv420Frame& reference_frame,
    const Yuv420Frame* reference_frame_l1 = nullptr);
Yuv420Frame decode_h264_reference_frame(
    const std::vector<std::vector<std::uint8_t>>& slice_nals,
    const SpsInfo& sps, const PpsInfo& pps,
    const Yuv420Frame* reference_frame = nullptr,
    const Yuv420Frame* reference_frame_l1 = nullptr);
Yuv420Frame decode_h264_reference_frame(
    const std::vector<std::vector<std::uint8_t>>& slice_nals,
    const SpsInfo& sps, const PpsInfo& pps,
    const std::vector<const Yuv420Frame*>& reference_frames_l0,
    const std::vector<const Yuv420Frame*>& reference_frames_l1);

} // namespace pygorvid

#endif
