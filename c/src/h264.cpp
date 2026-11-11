#include "pygorvid/h264.hpp"

#include <algorithm>
#include <array>
#include <limits>
#include <sstream>
#include <stdexcept>

namespace pygorvid {
namespace {

[[noreturn]] void invalid(const std::string& message) {
    throw std::runtime_error(message);
}

std::runtime_error context(const std::string& codec, const std::string& field,
                           const std::exception& error) {
    return std::runtime_error(codec + " " + field + ": " + error.what());
}

bool profile_has_chroma_depth_syntax(std::uint8_t profile_idc) {
    switch (profile_idc) {
    case 44: case 83: case 86: case 100: case 110: case 118:
    case 122: case 128: case 134: case 135: case 138: case 139:
    case 144: case 244:
        return true;
    default:
        return false;
    }
}

bool profile_is_supported(std::uint8_t profile_idc) {
    switch (profile_idc) {
    case 66: case 77: case 88: case 100: case 110: case 122: case 244:
        return true;
    default:
        return false;
    }
}

bool read_flag(BitReader& reader, const std::string& codec, const std::string& field) {
    try {
        return reader.read_bit();
    } catch (const std::exception& error) {
        throw context(codec, field, error);
    }
}

std::uint32_t read_ue(BitReader& reader, const std::string& codec,
                      const std::string& field) {
    try {
        return reader.read_ue();
    } catch (const std::exception& error) {
        throw context(codec, field, error);
    }
}

std::int64_t read_se(BitReader& reader, const std::string& codec,
                     const std::string& field) {
    try {
        return reader.read_se();
    } catch (const std::exception& error) {
        throw context(codec, field, error);
    }
}

std::uint8_t read_byte(BitReader& reader, const std::string& field) {
    try {
        return static_cast<std::uint8_t>(reader.read_bits(8));
    } catch (const std::exception& error) {
        throw context("SPS", field, error);
    }
}

std::array<bool, 3> skip_scaling_lists(BitReader& reader,
                                       std::uint32_t chroma_format_idc) {
    const bool present = read_flag(reader, "SPS", "seq_scaling_matrix_present_flag");
    if (!present) {
        return {};
    }
    bool luma_scaling_list_present = false;
    bool chroma_scaling_list_present = false;
    bool luma_8x8_scaling_list_present = false;
    const auto list_count = chroma_format_idc == 3 ? 12U : 8U;
    for (std::uint32_t list_index = 0; list_index < list_count; ++list_index) {
        if (!read_flag(reader, "SPS", "seq_scaling_list_present_flag")) {
            continue;
        }
        luma_scaling_list_present |= list_index == 0;
        chroma_scaling_list_present |= list_index == 1 || list_index == 2;
        luma_8x8_scaling_list_present |= list_index == 6;
        std::int64_t last_scale = 8;
        std::int64_t next_scale = 8;
        const auto list_size = list_index < 6 ? 16U : 64U;
        for (std::uint32_t i = 0; i < list_size; ++i) {
            if (next_scale != 0) {
                const auto delta_scale = read_se(reader, "SPS", "scaling_list.delta_scale");
                if (delta_scale < -128 || delta_scale > 127) {
                    invalid("SPS scaling_list.delta_scale is outside [-128,127]");
                }
                next_scale = (last_scale + delta_scale + 256) % 256;
            }
            if (next_scale != 0) {
                last_scale = next_scale;
            }
        }
    }
    return {{luma_scaling_list_present, chroma_scaling_list_present,
             luma_8x8_scaling_list_present}};
}

void set_dimensions(SpsInfo& info, std::uint32_t width_mbs_minus_1,
                    std::uint32_t height_map_units_minus_1,
                    const std::array<std::uint32_t, 4>& crop) {
    const auto frame_factor = info.frame_mbs_only ? std::uint64_t{1} : std::uint64_t{2};
    const auto coded_width = (static_cast<std::uint64_t>(width_mbs_minus_1) + 1) * 16;
    const auto coded_height =
        (static_cast<std::uint64_t>(height_map_units_minus_1) + 1) * frame_factor * 16;
    const auto chroma_array_type = info.separate_colour_plane ? 0 : info.chroma_format_idc;
    std::uint64_t sub_width = 1;
    std::uint64_t sub_height = 1;
    if (chroma_array_type == 1 || chroma_array_type == 2) {
        sub_width = 2;
    }
    if (chroma_array_type == 1) {
        sub_height = 2;
    }
    const auto crop_unit_x = chroma_array_type == 0 ? 1 : sub_width;
    const auto crop_unit_y = chroma_array_type == 0 ? frame_factor : sub_height * frame_factor;
    const std::array<std::uint64_t, 4> crop_pixels{
        static_cast<std::uint64_t>(crop[0]) * crop_unit_x,
        static_cast<std::uint64_t>(crop[1]) * crop_unit_x,
        static_cast<std::uint64_t>(crop[2]) * crop_unit_y,
        static_cast<std::uint64_t>(crop[3]) * crop_unit_y};
    if (crop_pixels[0] + crop_pixels[1] >= coded_width ||
        crop_pixels[2] + crop_pixels[3] >= coded_height) {
        invalid("SPS frame cropping removes the entire picture");
    }
    if (coded_width > std::numeric_limits<std::uint32_t>::max() ||
        coded_height > std::numeric_limits<std::uint32_t>::max()) {
        invalid("SPS coded dimensions exceed uint32");
    }
    info.coded_width = static_cast<std::uint32_t>(coded_width);
    info.coded_height = static_cast<std::uint32_t>(coded_height);
    info.width = static_cast<std::uint32_t>(coded_width - crop_pixels[0] - crop_pixels[1]);
    info.height = static_cast<std::uint32_t>(coded_height - crop_pixels[2] - crop_pixels[3]);
    info.frame_crop_left = static_cast<std::uint32_t>(crop_pixels[0]);
    info.frame_crop_right = static_cast<std::uint32_t>(crop_pixels[1]);
    info.frame_crop_top = static_cast<std::uint32_t>(crop_pixels[2]);
    info.frame_crop_bottom = static_cast<std::uint32_t>(crop_pixels[3]);
}

void validate_supported_sps(const SpsInfo& info) {
    if (!profile_is_supported(info.profile_idc)) {
        invalid("unsupported SPS profile_idc " + std::to_string(info.profile_idc) +
                ": only progressive 8-bit 4:2:0 profiles are supported");
    }
    if (info.chroma_format_idc != 1) {
        invalid("unsupported SPS chroma_format_idc " +
                std::to_string(info.chroma_format_idc) + ": only 4:2:0 is supported");
    }
    if (info.separate_colour_plane) {
        invalid("unsupported SPS separate_colour_plane_flag: only interleaved 4:2:0 is supported");
    }
    if (info.bit_depth_luma != 8 || info.bit_depth_chroma != 8) {
        invalid("unsupported SPS bit depth " + std::to_string(info.bit_depth_luma) + "/" +
                std::to_string(info.bit_depth_chroma) + ": only 8-bit 4:2:0 is supported");
    }
    if (!info.frame_mbs_only || info.mb_adaptive_frame_field) {
        invalid("unsupported interlaced or adaptive-frame-field SPS: only progressive 8-bit 4:2:0 is supported");
    }
}

} // namespace

NalHeader parse_nal_header(const std::vector<std::uint8_t>& nal) {
    if (nal.empty()) {
        throw std::invalid_argument("NAL unit is empty");
    }
    if ((nal.front() & 0x80) != 0) {
        invalid("NAL forbidden_zero_bit is set");
    }
    return {static_cast<std::uint8_t>((nal.front() >> 5) & 0x03),
            static_cast<std::uint8_t>(nal.front() & 0x1f)};
}

std::vector<std::uint8_t> ebsp_to_rbsp(const std::vector<std::uint8_t>& ebsp) {
    std::vector<std::uint8_t> rbsp;
    rbsp.reserve(ebsp.size());
    unsigned zero_count = 0;
    for (std::size_t index = 0; index < ebsp.size(); ++index) {
        const auto value = ebsp[index];
        if (zero_count == 2) {
            if (value == 0x03) {
                if (index + 1 == ebsp.size() || ebsp[index + 1] > 0x03) {
                    invalid("malformed emulation-prevention sequence");
                }
                zero_count = 0;
                continue;
            }
            if (value <= 0x02) {
                invalid("malformed emulation-prevention sequence");
            }
        }
        rbsp.push_back(value);
        zero_count = value == 0 ? zero_count + 1 : 0;
    }
    return rbsp;
}

std::vector<std::vector<std::uint8_t>> split_length_prefixed_nals(
    const std::vector<std::uint8_t>& data, std::size_t length_size) {
    if (length_size != 1 && length_size != 2 && length_size != 4) {
        throw std::invalid_argument("NAL length size must be 1, 2, or 4");
    }
    std::vector<std::vector<std::uint8_t>> nals;
    std::size_t offset = 0;
    while (offset < data.size()) {
        if (data.size() - offset < length_size) {
            throw std::runtime_error("truncated NAL length prefix");
        }
        std::size_t nal_size = 0;
        for (std::size_t i = 0; i < length_size; ++i) {
            if (nal_size > (std::numeric_limits<std::size_t>::max() - data[offset + i]) / 256) {
                invalid("NAL unit length exceeds supported range");
            }
            nal_size = nal_size * 256 + data[offset + i];
        }
        offset += length_size;
        if (nal_size == 0) {
            invalid("NAL unit has zero length");
        }
        if (nal_size > data.size() - offset) {
            throw std::runtime_error("NAL unit is truncated");
        }
        nals.emplace_back(data.begin() + static_cast<std::ptrdiff_t>(offset),
                          data.begin() + static_cast<std::ptrdiff_t>(offset + nal_size));
        offset += nal_size;
    }
    return nals;
}

std::vector<std::vector<std::uint8_t>> split_h264_sample_nals(
    const std::vector<std::uint8_t>& data, std::size_t length_size) {
    auto nals = split_length_prefixed_nals(data, length_size);
    if (nals.empty()) {
        return nals;
    }
    const auto& nal = nals.back();
    if ((nal[0] & 0x1f) != 1 && (nal[0] & 0x1f) != 5) {
        return nals;
    }
    const auto size = nal.size();
    if (size < 4 || (nal[size - 1] != 0x0a && nal[size - 1] != 0x0b) ||
        nal[size - 4] != 0 || nal[size - 3] != 0 || nal[size - 2] != 1) {
        return nals;
    }
    auto end = size - 4;
    if (end > 0 && nal[end - 1] == 0) {
        --end;
    }
    if (end <= 1) {
        return nals;
    }
    std::vector<std::uint8_t> slice(nal.begin(),
                                    nal.begin() + static_cast<std::ptrdiff_t>(end));
    const auto marker = nal.back();
    nals.back() = std::move(slice);
    nals.push_back({marker});
    return nals;
}

SpsInfo parse_sps(const std::vector<std::uint8_t>& nal) {
    const auto header = parse_nal_header(nal);
    if (header.unit_type != 7) {
        invalid("NAL unit type " + std::to_string(header.unit_type) + " is not an SPS");
    }
    const std::vector<std::uint8_t> ebsp(nal.begin() + 1, nal.end());
    const auto rbsp = ebsp_to_rbsp(ebsp);
    BitReader reader(rbsp);
    SpsInfo info;
    info.profile_idc = read_byte(reader, "profile_idc");
    info.constraint_flags = read_byte(reader, "constraint flags");
    if ((info.constraint_flags & 0x03) != 0) {
        invalid("SPS reserved constraint bits are nonzero");
    }
    info.level_idc = read_byte(reader, "level_idc");
    info.sps_id = read_ue(reader, "SPS", "seq_parameter_set_id");
    if (info.sps_id > 31) {
        invalid("SPS seq_parameter_set_id " + std::to_string(info.sps_id) + " exceeds 31");
    }

    if (profile_has_chroma_depth_syntax(info.profile_idc)) {
        const auto chroma_format_idc = read_ue(reader, "SPS", "chroma_format_idc");
        if (chroma_format_idc > 3) {
            invalid("SPS chroma_format_idc " + std::to_string(chroma_format_idc) + " exceeds 3");
        }
        info.chroma_format_idc = static_cast<std::uint8_t>(chroma_format_idc);
        if (chroma_format_idc == 3) {
            info.separate_colour_plane =
                read_flag(reader, "SPS", "separate_colour_plane_flag");
        }
        const auto luma_depth = read_ue(reader, "SPS", "bit_depth_luma_minus8");
        const auto chroma_depth = read_ue(reader, "SPS", "bit_depth_chroma_minus8");
        if (luma_depth > 6 || chroma_depth > 6) {
            invalid("SPS bit depth minus 8 exceeds 6");
        }
        info.bit_depth_luma = static_cast<std::uint8_t>(8 + luma_depth);
        info.bit_depth_chroma = static_cast<std::uint8_t>(8 + chroma_depth);
        (void)read_flag(reader, "SPS", "qpprime_y_zero_transform_bypass_flag");
        const auto scaling_lists = skip_scaling_lists(reader, chroma_format_idc);
        info.seq_intra_luma_4x4_scaling_list_present = scaling_lists[0];
        info.seq_intra_chroma_4x4_scaling_list_present = scaling_lists[1];
        info.seq_intra_luma_8x8_scaling_list_present = scaling_lists[2];
    }

    info.log2_max_frame_num_minus4 = read_ue(reader, "SPS", "log2_max_frame_num_minus4");
    if (info.log2_max_frame_num_minus4 > 12) {
        invalid("SPS log2_max_frame_num_minus4 exceeds 12");
    }
    const auto poc_type = read_ue(reader, "SPS", "pic_order_cnt_type");
    if (poc_type > 2) {
        invalid("SPS pic_order_cnt_type " + std::to_string(poc_type) + " exceeds 2");
    }
    info.pic_order_cnt_type = static_cast<std::uint8_t>(poc_type);
    if (poc_type == 0) {
        const auto value = read_ue(reader, "SPS", "log2_max_pic_order_cnt_lsb_minus4");
        if (value > 12) {
            invalid("SPS log2_max_pic_order_cnt_lsb_minus4 exceeds 12");
        }
        info.log2_max_pic_order_cnt_lsb_minus4 = value;
    } else if (poc_type == 1) {
        info.delta_pic_order_always_zero =
            read_flag(reader, "SPS", "delta_pic_order_always_zero_flag");
        info.offset_for_non_ref_pic = read_se(reader, "SPS", "offset_for_non_ref_pic");
        info.offset_for_top_to_bottom_field =
            read_se(reader, "SPS", "offset_for_top_to_bottom_field");
        const auto cycle_count =
            read_ue(reader, "SPS", "num_ref_frames_in_pic_order_cnt_cycle");
        if (cycle_count > 255) {
            invalid("SPS POC cycle count exceeds 255");
        }
        info.offset_for_ref_frame.reserve(cycle_count);
        for (std::uint32_t index = 0; index < cycle_count; ++index) {
            info.offset_for_ref_frame.push_back(
                read_se(reader, "SPS", "offset_for_ref_frame[" + std::to_string(index) + "]"));
        }
    }
    info.max_num_ref_frames = read_ue(reader, "SPS", "max_num_ref_frames");
    info.gaps_in_frame_num_value_allowed =
        read_flag(reader, "SPS", "gaps_in_frame_num_value_allowed_flag");
    const auto width_mbs_minus_1 = read_ue(reader, "SPS", "pic_width_in_mbs_minus1");
    const auto height_map_units_minus_1 =
        read_ue(reader, "SPS", "pic_height_in_map_units_minus1");
    info.frame_mbs_only = read_flag(reader, "SPS", "frame_mbs_only_flag");
    if (!info.frame_mbs_only) {
        info.mb_adaptive_frame_field =
            read_flag(reader, "SPS", "mb_adaptive_frame_field_flag");
    }
    info.direct_8x8_inference = read_flag(reader, "SPS", "direct_8x8_inference_flag");
    std::array<std::uint32_t, 4> crop{};
    if (read_flag(reader, "SPS", "frame_cropping_flag")) {
        crop = {read_ue(reader, "SPS", "frame_crop_left_offset"),
                read_ue(reader, "SPS", "frame_crop_right_offset"),
                read_ue(reader, "SPS", "frame_crop_top_offset"),
                read_ue(reader, "SPS", "frame_crop_bottom_offset")};
    }
    (void)read_flag(reader, "SPS", "vui_parameters_present_flag");
    set_dimensions(info, width_mbs_minus_1, height_map_units_minus_1, crop);
    validate_supported_sps(info);
    return info;
}

PpsInfo parse_pps(const std::vector<std::uint8_t>& nal) {
    const auto header = parse_nal_header(nal);
    if (header.unit_type != 8) {
        invalid("NAL unit type " + std::to_string(header.unit_type) + " is not a PPS");
    }
    const std::vector<std::uint8_t> ebsp(nal.begin() + 1, nal.end());
    const auto rbsp = ebsp_to_rbsp(ebsp);
    BitReader reader(rbsp);
    PpsInfo info;
    info.picture_parameter_set_id = read_ue(reader, "PPS", "pic_parameter_set_id");
    if (info.picture_parameter_set_id > 255) {
        invalid("PPS pic_parameter_set_id " +
                std::to_string(info.picture_parameter_set_id) + " exceeds 255");
    }
    info.sequence_parameter_set_id = read_ue(reader, "PPS", "seq_parameter_set_id");
    if (info.sequence_parameter_set_id > 31) {
        invalid("PPS seq_parameter_set_id " +
                std::to_string(info.sequence_parameter_set_id) + " exceeds 31");
    }
    info.entropy_coding_mode = read_flag(reader, "PPS", "entropy_coding_mode_flag");
    info.bottom_field_pic_order_in_frame_present =
        read_flag(reader, "PPS", "bottom_field_pic_order_in_frame_present_flag");
    info.num_slice_groups_minus1 = read_ue(reader, "PPS", "num_slice_groups_minus1");
    if (info.num_slice_groups_minus1 != 0) {
        invalid("unsupported PPS slice groups: num_slice_groups_minus1=" +
                std::to_string(info.num_slice_groups_minus1));
    }
    info.num_ref_idx_l0_default_active_minus1 =
        read_ue(reader, "PPS", "num_ref_idx_l0_default_active_minus1");
    info.num_ref_idx_l1_default_active_minus1 =
        read_ue(reader, "PPS", "num_ref_idx_l1_default_active_minus1");
    if (info.num_ref_idx_l0_default_active_minus1 > 31 ||
        info.num_ref_idx_l1_default_active_minus1 > 31) {
        invalid("PPS default reference index exceeds 31");
    }
    info.weighted_pred = read_flag(reader, "PPS", "weighted_pred_flag");
    info.weighted_bipred_idc =
        static_cast<std::uint8_t>(reader.read_bits(2));
    if (info.weighted_bipred_idc > 2) {
        invalid("PPS weighted_bipred_idc " +
                std::to_string(info.weighted_bipred_idc) + " is reserved");
    }
    info.pic_init_qp_minus26 = read_se(reader, "PPS", "pic_init_qp_minus26");
    info.pic_init_qs_minus26 = read_se(reader, "PPS", "pic_init_qs_minus26");
    info.chroma_qp_index_offset = read_se(reader, "PPS", "chroma_qp_index_offset");
    if (info.pic_init_qp_minus26 < -26 || info.pic_init_qp_minus26 > 25 ||
        info.pic_init_qs_minus26 < -26 || info.pic_init_qs_minus26 > 25) {
        invalid("PPS initial QP/QS offset is outside [-26,25]");
    }
    if (info.chroma_qp_index_offset < -12 || info.chroma_qp_index_offset > 12) {
        invalid("PPS chroma QP index offset is outside [-12,12]");
    }
    info.deblocking_filter_control_present =
        read_flag(reader, "PPS", "deblocking_filter_control_present_flag");
    info.constrained_intra_pred = read_flag(reader, "PPS", "constrained_intra_pred_flag");
    info.redundant_pic_cnt_present =
        read_flag(reader, "PPS", "redundant_pic_cnt_present_flag");
    info.has_extension = reader.more_rbsp_data();
    info.second_chroma_qp_index_offset = info.chroma_qp_index_offset;
    if (info.has_extension) {
        info.transform_8x8_mode = read_flag(reader, "PPS", "transform_8x8_mode_flag");
        info.pic_scaling_matrix_present =
            read_flag(reader, "PPS", "pic_scaling_matrix_present_flag");
        if (info.pic_scaling_matrix_present) {
            invalid("unsupported PPS pic_scaling_matrix_present_flag");
        }
        info.second_chroma_qp_index_offset =
            read_se(reader, "PPS", "second_chroma_qp_index_offset");
        if (info.second_chroma_qp_index_offset < -12 ||
            info.second_chroma_qp_index_offset > 12) {
            invalid("PPS second chroma QP index offset is outside [-12,12]");
        }
    }
    return info;
}

IdrSliceHeader parse_idr_i_slice_header(const std::vector<std::uint8_t>& nal,
                                        const SpsInfo& sps,
                                        const PpsInfo& pps) {
    const auto header = parse_nal_header(nal);
    if (header.unit_type != 5) {
        invalid("NAL unit is not an IDR slice");
    }
    if (pps.sequence_parameter_set_id != sps.sps_id) {
        invalid("PPS references SPS " + std::to_string(pps.sequence_parameter_set_id) +
                ", supplied SPS is " + std::to_string(sps.sps_id));
    }
    const std::vector<std::uint8_t> ebsp(nal.begin() + 1, nal.end());
    const auto rbsp = ebsp_to_rbsp(ebsp);
    BitReader reader(rbsp);
    IdrSliceHeader result;
    result.first_macroblock_in_slice = read_ue(reader, "slice", "first_mb_in_slice");
    const auto slice_type = read_ue(reader, "slice", "slice_type");
    if (slice_type > 9) {
        invalid("slice_type " + std::to_string(slice_type) + " exceeds 9");
    }
    if (slice_type % 5 != 2) {
        invalid("C++ IDR decoder currently supports I slices only");
    }
    result.slice_type = static_cast<std::uint8_t>(slice_type);
    result.picture_parameter_set_id =
        read_ue(reader, "slice", "pic_parameter_set_id");
    if (result.picture_parameter_set_id != pps.picture_parameter_set_id) {
        invalid("slice references PPS " +
                std::to_string(result.picture_parameter_set_id) +
                ", supplied PPS is " + std::to_string(pps.picture_parameter_set_id));
    }
    const auto frame_num_bits = sps.log2_max_frame_num_minus4 + 4;
    try {
        result.frame_num = reader.read_bits(static_cast<std::uint8_t>(frame_num_bits));
    } catch (const std::exception& error) {
        throw context("slice", "frame_num", error);
    }
    result.idr_pic_id = read_ue(reader, "slice", "idr_pic_id");
    if (result.idr_pic_id > 65535) {
        invalid("idr_pic_id " + std::to_string(result.idr_pic_id) + " exceeds 65535");
    }
    if (sps.pic_order_cnt_type == 0) {
        if (!sps.log2_max_pic_order_cnt_lsb_minus4) {
            invalid("SPS POC type 0 lacks log2_max_pic_order_cnt_lsb_minus4");
        }
        try {
            result.pic_order_cnt_lsb = reader.read_bits(
                static_cast<std::uint8_t>(*sps.log2_max_pic_order_cnt_lsb_minus4 + 4));
        } catch (const std::exception& error) {
            throw context("slice", "pic_order_cnt_lsb", error);
        }
        if (pps.bottom_field_pic_order_in_frame_present) {
            result.delta_pic_order_bottom =
                read_se(reader, "slice", "delta_pic_order_bottom");
        }
    } else if (sps.pic_order_cnt_type == 1 && !sps.delta_pic_order_always_zero) {
        result.delta_pic_order_cnt0 =
            read_se(reader, "slice", "delta_pic_order_cnt[0]");
        if (pps.bottom_field_pic_order_in_frame_present) {
            result.delta_pic_order_cnt1 =
                read_se(reader, "slice", "delta_pic_order_cnt[1]");
        }
    } else if (sps.pic_order_cnt_type > 2) {
        invalid("unsupported SPS pic_order_cnt_type " +
                std::to_string(sps.pic_order_cnt_type));
    }
    if (pps.redundant_pic_cnt_present) {
        result.redundant_pic_cnt = read_ue(reader, "slice", "redundant_pic_cnt");
        if (result.redundant_pic_cnt > 127) {
            invalid("redundant_pic_cnt exceeds 127");
        }
    }
    if (header.reference_idc != 0) {
        result.no_output_of_prior_pics =
            read_flag(reader, "slice", "no_output_of_prior_pics_flag");
        result.long_term_reference =
            read_flag(reader, "slice", "long_term_reference_flag");
    }
    result.slice_qp_delta = read_se(reader, "slice", "slice_qp_delta");
    const auto slice_qpy = 26 + pps.pic_init_qp_minus26 + result.slice_qp_delta;
    const auto qp_bd_offset_y = 6 * (static_cast<std::int64_t>(sps.bit_depth_luma) - 8);
    if (slice_qpy < -qp_bd_offset_y || slice_qpy > 51) {
        invalid("slice QP " + std::to_string(slice_qpy) + " is out of range");
    }
    if (pps.deblocking_filter_control_present) {
        const auto disable_idc = read_ue(reader, "slice", "disable_deblocking_filter_idc");
        if (disable_idc > 2) {
            invalid("disable_deblocking_filter_idc " + std::to_string(disable_idc) +
                    " exceeds 2");
        }
        result.disable_deblocking_filter_idc = static_cast<std::uint8_t>(disable_idc);
        if (disable_idc != 1) {
            result.slice_alpha_c0_offset_div2 =
                read_se(reader, "slice", "slice_alpha_c0_offset_div2");
            result.slice_beta_offset_div2 =
                read_se(reader, "slice", "slice_beta_offset_div2");
            if (result.slice_alpha_c0_offset_div2 < -6 ||
                result.slice_alpha_c0_offset_div2 > 6 ||
                result.slice_beta_offset_div2 < -6 ||
                result.slice_beta_offset_div2 > 6) {
                invalid("slice deblocking offset exceeds [-6,6]");
            }
        }
    }
    if (pps.entropy_coding_mode) {
        while (reader.bit_offset() % 8 != 0) {
            if (!read_flag(reader, "slice", "cabac_alignment_one_bit")) {
                invalid("cabac_alignment_one_bit is not 1 at bit " +
                        std::to_string(reader.bit_offset() - 1));
            }
        }
    }
    result.slice_data_bit_offset = reader.bit_offset();
    return result;
}

PSliceHeader parse_p_slice_header(const std::vector<std::uint8_t>& nal,
                                  const SpsInfo& sps, const PpsInfo& pps) {
    const auto header = parse_nal_header(nal);
    if (header.unit_type != 1) {
        invalid("NAL unit is not a non-IDR slice");
    }
    if (pps.sequence_parameter_set_id != sps.sps_id) {
        invalid("PPS references a different SPS than the supplied SPS");
    }
    if (!sps.frame_mbs_only) {
        invalid("P-slice parsing currently requires progressive frame pictures");
    }
    if (pps.num_slice_groups_minus1 != 0) {
        invalid("inter-slice parsing does not support slice groups");
    }

    const std::vector<std::uint8_t> ebsp(nal.begin() + 1, nal.end());
    const auto rbsp = ebsp_to_rbsp(ebsp);
    BitReader reader(rbsp);
    PSliceHeader result;
    result.first_macroblock_in_slice =
        read_ue(reader, "P slice", "first_mb_in_slice");
    const auto slice_type = read_ue(reader, "inter slice", "slice_type");
    if (slice_type > 9) {
        invalid("inter slice_type is outside [0,9]");
    }
    const auto normalized_slice_type =
        static_cast<std::uint8_t>(slice_type % 5);
    if (normalized_slice_type != 0 && normalized_slice_type != 1) {
        invalid("inter slice parsing supports P and B slices only");
    }
    if (normalized_slice_type == 0 && pps.weighted_pred) {
        invalid("P-slice parsing does not support weighted prediction");
    }
    result.slice_type = normalized_slice_type;
    result.picture_parameter_set_id =
        read_ue(reader, "P slice", "pic_parameter_set_id");
    if (result.picture_parameter_set_id != pps.picture_parameter_set_id) {
        invalid("P slice references a different PPS than the supplied PPS");
    }
    const auto frame_num_bits = sps.log2_max_frame_num_minus4 + 4;
    try {
        result.frame_num = reader.read_bits(
            static_cast<std::uint8_t>(frame_num_bits));
    } catch (const std::exception& error) {
        throw context("P slice", "frame_num", error);
    }
    if (sps.pic_order_cnt_type == 0) {
        if (!sps.log2_max_pic_order_cnt_lsb_minus4) {
            invalid("SPS POC type 0 lacks log2_max_pic_order_cnt_lsb_minus4");
        }
        try {
            result.pic_order_cnt_lsb = reader.read_bits(
                static_cast<std::uint8_t>(
                    *sps.log2_max_pic_order_cnt_lsb_minus4 + 4));
        } catch (const std::exception& error) {
            throw context("P slice", "pic_order_cnt_lsb", error);
        }
        if (pps.bottom_field_pic_order_in_frame_present) {
            result.delta_pic_order_bottom =
                read_se(reader, "P slice", "delta_pic_order_bottom");
        }
    } else if (sps.pic_order_cnt_type == 1 &&
               !sps.delta_pic_order_always_zero) {
        result.delta_pic_order_cnt0 =
            read_se(reader, "P slice", "delta_pic_order_cnt[0]");
        if (pps.bottom_field_pic_order_in_frame_present) {
            result.delta_pic_order_cnt1 =
                read_se(reader, "P slice", "delta_pic_order_cnt[1]");
        }
    } else if (sps.pic_order_cnt_type > 2) {
        invalid("unsupported SPS pic_order_cnt_type");
    }
    if (pps.redundant_pic_cnt_present) {
        result.redundant_pic_cnt =
            read_ue(reader, "P slice", "redundant_pic_cnt");
        if (result.redundant_pic_cnt > 127) {
            invalid("P slice redundant_pic_cnt exceeds 127");
        }
    }

    if (normalized_slice_type == 1) {
        result.direct_spatial_mv_pred = read_flag(
            reader, "B slice", "direct_spatial_mv_pred_flag");
    }
    const bool reference_override =
        read_flag(reader, "inter slice", "num_ref_idx_active_override_flag");
    if (reference_override) {
        result.num_ref_idx_l0_active_minus1 =
            read_ue(reader, "inter slice", "num_ref_idx_l0_active_minus1");
        if (normalized_slice_type == 1) {
            result.num_ref_idx_l1_active_minus1 =
                read_ue(reader, "B slice", "num_ref_idx_l1_active_minus1");
        }
    } else {
        result.num_ref_idx_l0_active_minus1 =
            pps.num_ref_idx_l0_default_active_minus1;
        result.num_ref_idx_l1_active_minus1 =
            pps.num_ref_idx_l1_default_active_minus1;
    }
    if (result.num_ref_idx_l0_active_minus1 > 31 ||
        result.num_ref_idx_l1_active_minus1 > 31) {
        invalid("inter slice active reference count exceeds 32");
    }
    const auto parse_ref_pic_list_modifications =
        [&](const std::string& field,
            std::vector<ReferenceListModification>& modifications) {
        if (!read_flag(reader, "inter slice",
                       "ref_pic_list_modification_flag")) {
            return;
        }
        for (std::size_t command_index = 0; command_index < 64;
             ++command_index) {
            const auto idc =
                read_ue(reader, "inter slice", field + ".modification_of_pic_nums_idc");
            if (idc == 3) {
                return;
            }
            if (idc > 2) {
                invalid(field + ".modification_of_pic_nums_idc exceeds 3");
            }
            ReferenceListModification modification;
            modification.modification_of_pic_nums_idc =
                static_cast<std::uint8_t>(idc);
            if (idc == 0 || idc == 1) {
                modification.abs_diff_pic_num_minus1 =
                    read_ue(reader, "inter slice",
                            field + ".abs_diff_pic_num_minus1");
            } else {
                modification.long_term_pic_num =
                    read_ue(reader, "inter slice",
                            field + ".long_term_pic_num");
            }
            modifications.push_back(modification);
        }
        invalid(field + " contains too many reference-list modifications");
    };
    parse_ref_pic_list_modifications(
        "ref_pic_list_modification_l0",
        result.ref_pic_list_modifications_l0);
    if (normalized_slice_type == 1) {
        parse_ref_pic_list_modifications(
            "ref_pic_list_modification_l1",
            result.ref_pic_list_modifications_l1);
    }
    if (normalized_slice_type == 1 && pps.weighted_bipred_idc != 0) {
        invalid("B decoding does not support weighted biprediction");
    }
    if (header.reference_idc != 0 &&
        read_flag(reader, "P slice",
                  "adaptive_ref_pic_marking_mode_flag")) {
        result.adaptive_ref_pic_marking_mode = true;
        bool terminated = false;
        for (std::size_t operation_index = 0; operation_index < 32;
             ++operation_index) {
            const auto operation =
                read_ue(reader, "P slice",
                        "memory_management_control_operation");
            if (operation == 0) {
                terminated = true;
                break;
            }
            std::size_t operand_count = 0;
            switch (operation) {
            case 1:
            case 2:
            case 4:
            case 6:
                operand_count = 1;
                break;
            case 3:
                operand_count = 2;
                break;
            case 5:
                break;
            default:
                invalid("memory_management_control_operation is invalid");
            }
            MemoryManagementOperation parsed_operation;
            parsed_operation.operation = operation;
            parsed_operation.operands.reserve(operand_count);
            for (std::size_t operand_index = 0;
                 operand_index < operand_count; ++operand_index) {
                parsed_operation.operands.push_back(read_ue(
                    reader, "P slice",
                    "memory_management_operation_operand"));
            }
            result.memory_management_operations.push_back(
                std::move(parsed_operation));
        }
        if (!terminated) {
            invalid("decoded-reference marking exceeds 32 operations");
        }
    }
    if (pps.entropy_coding_mode) {
        const auto cabac_init_idc =
            read_ue(reader, "P slice", "cabac_init_idc");
        if (cabac_init_idc > 2) {
            invalid("P slice cabac_init_idc exceeds 2");
        }
        result.cabac_init_idc =
            static_cast<std::uint8_t>(cabac_init_idc);
    }
    result.slice_qp_delta = read_se(reader, "P slice", "slice_qp_delta");
    const auto slice_qpy =
        26 + pps.pic_init_qp_minus26 + result.slice_qp_delta;
    if (slice_qpy < 0 || slice_qpy > 51) {
        invalid("P slice QP is outside [0,51]");
    }
    if (pps.deblocking_filter_control_present) {
        const auto disable_idc =
            read_ue(reader, "P slice", "disable_deblocking_filter_idc");
        if (disable_idc > 2) {
            invalid("P slice disable_deblocking_filter_idc exceeds 2");
        }
        result.disable_deblocking_filter_idc =
            static_cast<std::uint8_t>(disable_idc);
        if (disable_idc != 1) {
            result.slice_alpha_c0_offset_div2 =
                read_se(reader, "P slice", "slice_alpha_c0_offset_div2");
            result.slice_beta_offset_div2 =
                read_se(reader, "P slice", "slice_beta_offset_div2");
            if (result.slice_alpha_c0_offset_div2 < -6 ||
                result.slice_alpha_c0_offset_div2 > 6 ||
                result.slice_beta_offset_div2 < -6 ||
                result.slice_beta_offset_div2 > 6) {
                invalid("P slice deblocking offset exceeds [-6,6]");
            }
        }
    }
    if (pps.entropy_coding_mode) {
        while (reader.bit_offset() % 8 != 0) {
            if (!read_flag(reader, "P slice", "cabac_alignment_one_bit")) {
                invalid("cabac_alignment_one_bit is not 1 at bit " +
                        std::to_string(reader.bit_offset() - 1));
            }
        }
    }
    result.slice_data_bit_offset = reader.bit_offset();
    return result;
}

} // namespace pygorvid
