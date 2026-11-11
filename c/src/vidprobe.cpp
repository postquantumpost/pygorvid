#include "pygorvid/h264.hpp"
#include "pygorvid/jpeg.hpp"
#include "pygorvid/pygorvid.hpp"
#include "pygorvid/png.hpp"
#include "pygorvid/video_samples.hpp"

#include <algorithm>
#include <cctype>
#include <filesystem>
#include <fstream>
#include <iostream>
#include <limits>
#include <optional>
#include <string>
#include <vector>

namespace {

struct Options {
    std::vector<std::string> files;
    std::optional<std::size_t> extract_frame;
    std::optional<std::string> output_path;
    std::string output_prefix;
};

void usage() {
    std::cerr << "usage: vidprobe [--extractframe INDEX --output IMAGE] "
                 "[--prefix TEXT] [--input FILE] FILE...\n";
}

std::size_t parse_frame_index(const std::string& value) {
    if (value.empty() || value.front() == '-') {
        throw std::invalid_argument("--extractframe requires a nonnegative index");
    }
    std::size_t parsed_characters = 0;
    const auto parsed = std::stoull(value, &parsed_characters);
    if (parsed_characters != value.size() ||
        parsed > std::numeric_limits<std::size_t>::max()) {
        throw std::invalid_argument("--extractframe index is invalid or too large");
    }
    return static_cast<std::size_t>(parsed);
}

std::string prefixed_output_path(const std::string& output_path,
                                 const std::string& prefix) {
    if (prefix.empty()) {
        return output_path;
    }
    const std::filesystem::path path(output_path);
    return (path.parent_path() /
            (prefix + path.filename().string()))
        .string();
}

std::uint32_t slice_pps_id(const std::vector<std::uint8_t>& nal) {
    if (nal.size() < 2) {
        throw std::runtime_error("H.264 slice NAL is truncated");
    }
    const std::vector<std::uint8_t> ebsp(nal.begin() + 1, nal.end());
    const auto rbsp = pygorvid::ebsp_to_rbsp(ebsp);
    pygorvid::BitReader reader(rbsp);
    (void)reader.read_ue();
    (void)reader.read_ue();
    return reader.read_ue();
}

void save_image(const std::string& path, const pygorvid::Yuv420Frame& frame) {
    auto extension = std::filesystem::path(path).extension().string();
    std::transform(extension.begin(), extension.end(), extension.begin(),
                   [](unsigned char ch) { return static_cast<char>(std::tolower(ch)); });
    const auto image = extension == ".jpg" ? pygorvid::encode_jpeg(frame)
                                           : pygorvid::encode_png(frame);
    if (image.size() >
        static_cast<std::size_t>(
            std::numeric_limits<std::streamsize>::max())) {
        throw std::runtime_error("image output exceeds the supported write size");
    }
    std::ofstream output(path, std::ios::binary | std::ios::trunc);
    if (!output) {
        throw std::runtime_error("cannot open image output file");
    }
    output.write(reinterpret_cast<const char*>(image.data()),
                 static_cast<std::streamsize>(image.size()));
    output.close();
    if (!output) {
        throw std::runtime_error("cannot write complete image output file");
    }
}

void extract_frame(const std::string& input_path, std::size_t frame_index,
                   const std::string& output_path) {
    std::error_code path_error;
    const auto normalized_input =
        std::filesystem::absolute(input_path, path_error).lexically_normal();
    if (path_error) {
        throw std::runtime_error("cannot resolve input file path");
    }
    const auto normalized_output =
        std::filesystem::absolute(output_path, path_error).lexically_normal();
    if (path_error || normalized_input == normalized_output) {
        throw std::invalid_argument(
            "image output path must not overwrite the input video");
    }
    const bool output_exists = std::filesystem::exists(output_path, path_error);
    if (path_error) {
        throw std::runtime_error("cannot inspect image output path");
    }
    if (output_exists &&
        std::filesystem::equivalent(input_path, output_path, path_error)) {
        throw std::invalid_argument(
            "image output path must not overwrite the input video");
    }
    if (path_error) {
        throw std::runtime_error("cannot compare input and output paths");
    }

    pygorvid::VideoSampleReader sample_reader;
    if (!sample_reader.open(input_path)) {
        throw std::runtime_error(sample_reader.error_info());
    }
    if (frame_index >= sample_reader.sample_count()) {
        throw std::out_of_range("requested frame index exceeds the sample count");
    }
    const auto target_sample_index =
        sample_reader.presentation_sample_index(frame_index);
    const auto& configuration = sample_reader.configuration();
    std::vector<pygorvid::SpsInfo> sequence_parameter_sets;
    std::vector<pygorvid::PpsInfo> picture_parameter_sets;
    sequence_parameter_sets.reserve(configuration.sequence_parameter_sets.size());
    picture_parameter_sets.reserve(configuration.picture_parameter_sets.size());
    for (const auto& nal : configuration.sequence_parameter_sets) {
        sequence_parameter_sets.push_back(pygorvid::parse_sps(nal));
    }
    for (const auto& nal : configuration.picture_parameter_sets) {
        picture_parameter_sets.push_back(pygorvid::parse_pps(nal));
    }

    std::vector<pygorvid::Yuv420Frame> reference_frames;
    std::vector<std::uint32_t> reference_frame_nums;
    std::vector<std::optional<std::uint32_t>>
        reference_long_term_frame_idxs;
    std::vector<std::int64_t> reference_picture_order;
    std::optional<std::uint32_t> maximum_long_term_frame_idx;
    std::optional<pygorvid::Yuv420Frame> decoded_frame;
    for (std::size_t sample_index = 0; sample_index <= target_sample_index;
         ++sample_index) {
        const auto sample = sample_reader.sample_at(sample_index);
        const auto nals = pygorvid::split_h264_sample_nals(
            sample.data, configuration.nal_length_size);
        std::vector<std::vector<std::uint8_t>> slices;
        const pygorvid::SpsInfo* selected_sps = nullptr;
        const pygorvid::PpsInfo* selected_pps = nullptr;
        for (const auto& nal : nals) {
            const auto header = pygorvid::parse_nal_header(nal);
            if (header.unit_type != 1 && header.unit_type != 5) {
                if (header.unit_type >= 2 && header.unit_type <= 4) {
                    throw std::runtime_error(
                        "data-partitioned H.264 slices are unsupported");
                }
                continue;
            }
            const auto pps_id = slice_pps_id(nal);
            const auto pps = std::find_if(
                picture_parameter_sets.begin(), picture_parameter_sets.end(),
                [pps_id](const auto& entry) {
                    return entry.picture_parameter_set_id == pps_id;
                });
            if (pps == picture_parameter_sets.end()) {
                throw std::runtime_error(
                    "slice references a PPS missing from avcC");
            }
            const auto sps = std::find_if(
                sequence_parameter_sets.begin(),
                sequence_parameter_sets.end(),
                [&pps](const auto& entry) {
                    return entry.sps_id == pps->sequence_parameter_set_id;
                });
            if (sps == sequence_parameter_sets.end()) {
                throw std::runtime_error(
                    "slice PPS references an SPS missing from avcC");
            }
            if (selected_pps != nullptr &&
                (selected_pps->picture_parameter_set_id !=
                     pps->picture_parameter_set_id ||
                 selected_sps->sps_id != sps->sps_id)) {
                throw std::runtime_error(
                    "one MP4 sample contains slices using different parameter sets");
            }
            selected_sps = &*sps;
            selected_pps = &*pps;
            slices.push_back(nal);
        }
        if (slices.empty()) {
            if (sample_index == target_sample_index) {
                throw std::runtime_error(
                    "requested MP4 sample contains no H.264 picture");
            }
            continue;
        }
        const bool current_picture_is_reference = std::any_of(
            slices.begin(), slices.end(), [](const auto& nal) {
                return pygorvid::parse_nal_header(nal).reference_idc != 0;
            });
        const auto& selected_sps_info = *selected_sps;
        const auto& selected_pps_info = *selected_pps;
        const auto slice_header =
            pygorvid::parse_nal_header(slices.front()).unit_type == 5
            ? std::optional<pygorvid::PSliceHeader>{}
            : std::optional<pygorvid::PSliceHeader>{
                  pygorvid::parse_p_slice_header(
                      slices.front(), selected_sps_info,                       selected_pps_info)};
        const auto idr_header =
            slice_header
                ? std::optional<pygorvid::IdrSliceHeader>{}
                : std::optional<pygorvid::IdrSliceHeader>{
                      pygorvid::parse_idr_i_slice_header(
                          slices.front(), selected_sps_info,
                          selected_pps_info)};
        const auto current_frame_num =
            slice_header
                ? slice_header->frame_num
                : idr_header->frame_num;
        if (idr_header) {
            reference_frames.clear();
            reference_frame_nums.clear();
            reference_long_term_frame_idxs.clear();
            reference_picture_order.clear();
            maximum_long_term_frame_idx =
                idr_header->long_term_reference
                    ? std::optional<std::uint32_t>{0}
                    : std::nullopt;
        }
        const auto maximum_frame_num =
            std::uint32_t{1}
            << (selected_sps_info.log2_max_frame_num_minus4 + 4);
        const auto picture_number = [&](std::uint32_t reference_frame_num) {
            return reference_frame_num > current_frame_num
                       ? static_cast<std::int64_t>(reference_frame_num) -
                             maximum_frame_num
                       : static_cast<std::int64_t>(reference_frame_num);
        };
        std::vector<std::size_t> reference_indices;
        reference_indices.reserve(reference_frames.size());
        for (std::size_t index = 0; index < reference_frames.size(); ++index) {
            reference_indices.push_back(index);
        }
        std::sort(reference_indices.begin(), reference_indices.end(),
                  [&](std::size_t left, std::size_t right) {
                      const auto left_long_term =
                          reference_long_term_frame_idxs[left];
                      const auto right_long_term =
                          reference_long_term_frame_idxs[right];
                      if (left_long_term.has_value() !=
                          right_long_term.has_value()) {
                          return !left_long_term.has_value();
                      }
                      if (left_long_term && right_long_term) {
                          return *left_long_term < *right_long_term;
                      }
                      return picture_number(reference_frame_nums[left]) >
                             picture_number(reference_frame_nums[right]);
                  });
        auto reference_indices_l0 = reference_indices;
        std::vector<std::size_t> reference_indices_l1;
        if (slice_header && slice_header->slice_type == 1) {
            std::vector<std::size_t> before;
            std::vector<std::size_t> equal;
            std::vector<std::size_t> after;
            std::vector<std::size_t> long_term;
            for (std::size_t index = 0; index < reference_frames.size();
                 ++index) {
                if (reference_long_term_frame_idxs[index]) {
                    long_term.push_back(index);
                } else if (reference_picture_order[index] < sample.pts_ticks) {
                    before.push_back(index);
                } else if (reference_picture_order[index] == sample.pts_ticks) {
                    equal.push_back(index);
                } else {
                    after.push_back(index);
                }
            }
            std::sort(before.begin(), before.end(),
                      [&](std::size_t left, std::size_t right) {
                          return reference_picture_order[left] >
                                 reference_picture_order[right];
                      });
            std::sort(after.begin(), after.end(),
                      [&](std::size_t left, std::size_t right) {
                          return reference_picture_order[left] <
                                 reference_picture_order[right];
                      });
            std::sort(long_term.begin(), long_term.end(),
                      [&](std::size_t left, std::size_t right) {
                          return *reference_long_term_frame_idxs[left] <
                                 *reference_long_term_frame_idxs[right];
                      });
            reference_indices_l0 = before;
            reference_indices_l0.insert(reference_indices_l0.end(),
                                        equal.begin(), equal.end());
            reference_indices_l0.insert(reference_indices_l0.end(),
                                        after.begin(), after.end());
            reference_indices_l0.insert(reference_indices_l0.end(),
                                        long_term.begin(), long_term.end());
            reference_indices_l1 = after;
            reference_indices_l1.insert(reference_indices_l1.end(),
                                        equal.begin(), equal.end());
            reference_indices_l1.insert(reference_indices_l1.end(),
                                        before.begin(), before.end());
            reference_indices_l1.insert(reference_indices_l1.end(),
                                        long_term.begin(), long_term.end());
            if (reference_indices_l0 == reference_indices_l1 &&
                reference_indices_l1.size() > 1) {
                std::swap(reference_indices_l1[0], reference_indices_l1[1]);
            }
        }
        std::vector<const pygorvid::Yuv420Frame*> reference_list_l0;
        std::vector<const pygorvid::Yuv420Frame*> reference_list_l1;
        reference_list_l0.reserve(reference_indices_l0.size());
        reference_list_l1.reserve(reference_indices_l1.size());
        for (const auto index : reference_indices_l0) {
            reference_list_l0.push_back(&reference_frames[index]);
        }
        for (const auto index : reference_indices_l1) {
            reference_list_l1.push_back(&reference_frames[index]);
        }
        const auto apply_modifications =
            [&](const std::vector<pygorvid::ReferenceListModification>&
                    modifications,
                std::vector<const pygorvid::Yuv420Frame*>& list,
                std::uint32_t active_count) {
                auto predicted_picture_number =
                    static_cast<std::uint64_t>(current_frame_num);
                const auto picture_number_modulus =
                    static_cast<std::uint64_t>(maximum_frame_num);
                std::size_t insertion_index = 0;
                for (const auto& modification : modifications) {
                    if (modification.modification_of_pic_nums_idc == 2) {
                        const auto selected_index = std::find(
                            reference_long_term_frame_idxs.begin(),
                            reference_long_term_frame_idxs.end(),
                            std::optional<std::uint32_t>{
                                modification.long_term_pic_num});
                        if (selected_index ==
                            reference_long_term_frame_idxs.end()) {
                            throw std::runtime_error(
                                "reference-list modification selects an unavailable long-term picture");
                        }
                        const auto index = static_cast<std::size_t>(
                            std::distance(reference_long_term_frame_idxs.begin(),
                                          selected_index));
                        const auto* selected_frame = &reference_frames[index];
                        list.erase(std::remove(list.begin(), list.end(),
                                               selected_frame),
                                   list.end());
                        list.insert(list.begin() +
                                        static_cast<std::ptrdiff_t>(
                                            insertion_index),
                                    selected_frame);
                        ++insertion_index;
                        continue;
                    }
                    const auto difference =
                        static_cast<std::uint64_t>(
                            modification.abs_diff_pic_num_minus1) +
                        1;
                    if (modification.modification_of_pic_nums_idc == 0) {
                        if (difference > picture_number_modulus) {
                            throw std::runtime_error(
                                "reference-list modification difference exceeds MaxPicNum");
                        }
                        predicted_picture_number =
                            (predicted_picture_number +
                             picture_number_modulus - difference) %
                            picture_number_modulus;
                    } else {
                        if (difference > picture_number_modulus) {
                            throw std::runtime_error(
                                "reference-list modification difference exceeds MaxPicNum");
                        }
                        predicted_picture_number =
                            (predicted_picture_number + difference) %
                            picture_number_modulus;
                    }
                    const auto target_picture_number =
                        predicted_picture_number > current_frame_num
                            ? static_cast<std::int64_t>(
                                  predicted_picture_number) -
                                  maximum_frame_num
                            : static_cast<std::int64_t>(
                                  predicted_picture_number);
                    std::optional<std::size_t> selected_index;
                    for (std::size_t index = 0;
                         index < reference_frame_nums.size(); ++index) {
                        if (reference_long_term_frame_idxs[index]) {
                            continue;
                        }
                        if (picture_number(reference_frame_nums[index]) ==
                            target_picture_number) {
                            selected_index = index;
                            break;
                        }
                    }
                    if (!selected_index) {
                        throw std::runtime_error(
                            "reference-list modification selects an unavailable picture");
                    }
                    const auto* selected_frame =
                        &reference_frames[*selected_index];
                    list.erase(std::remove(list.begin(), list.end(),
                                           selected_frame),
                               list.end());
                    list.insert(list.begin() +
                                    static_cast<std::ptrdiff_t>(
                                        insertion_index),
                                selected_frame);
                    ++insertion_index;
                }
                if (list.size() > active_count) {
                    list.resize(active_count);
                }
            };
        if (slice_header && slice_header->slice_type == 1) {
            const auto active_l0 =
                slice_header->num_ref_idx_l0_active_minus1 + 1;
            const auto active_l1 =
                slice_header->num_ref_idx_l1_active_minus1 + 1;
            apply_modifications(
                slice_header->ref_pic_list_modifications_l0, reference_list_l0,
                active_l0);
            apply_modifications(
                slice_header->ref_pic_list_modifications_l1, reference_list_l1,
                active_l1);
        } else if (slice_header) {
            const auto active_l0 =
                slice_header->num_ref_idx_l0_active_minus1 + 1;
            apply_modifications(
                slice_header->ref_pic_list_modifications_l0, reference_list_l0,
                active_l0);
        }
        auto decoded = pygorvid::decode_h264_reference_frame(
            slices, selected_sps_info, selected_pps_info, reference_list_l0,
            reference_list_l1);
        decoded_frame = decoded;
        if (current_picture_is_reference) {
            auto stored_frame_num = current_frame_num;
            std::optional<std::uint32_t> current_long_term_frame_idx =
                idr_header && idr_header->long_term_reference
                    ? std::optional<std::uint32_t>{0}
                    : std::nullopt;
            if (slice_header &&
                slice_header->adaptive_ref_pic_marking_mode) {
                const auto erase_reference = [&](std::size_t index) {
                    reference_frames.erase(reference_frames.begin() +
                                           static_cast<std::ptrdiff_t>(index));
                    reference_frame_nums.erase(reference_frame_nums.begin() +
                                               static_cast<std::ptrdiff_t>(index));
                    reference_long_term_frame_idxs.erase(
                        reference_long_term_frame_idxs.begin() +
                        static_cast<std::ptrdiff_t>(index));
                    reference_picture_order.erase(
                        reference_picture_order.begin() +
                        static_cast<std::ptrdiff_t>(index));
                };
                const auto short_term_index =
                    [&](std::uint32_t difference_minus1)
                    -> std::optional<std::size_t> {
                    const auto picture_number =
                        static_cast<std::int64_t>(current_frame_num) -
                        (static_cast<std::int64_t>(difference_minus1) + 1);
                    for (std::size_t index = 0;
                         index < reference_frames.size(); ++index) {
                        if (reference_long_term_frame_idxs[index]) {
                            continue;
                        }
                        const auto frame_num_wrap =
                            reference_frame_nums[index] > current_frame_num
                                ? static_cast<std::int64_t>(
                                      reference_frame_nums[index]) -
                                      maximum_frame_num
                                : static_cast<std::int64_t>(
                                      reference_frame_nums[index]);
                        if (frame_num_wrap == picture_number) {
                            return index;
                        }
                    }
                    return std::nullopt;
                };
                for (const auto& operation :
                     slice_header->memory_management_operations) {
                    switch (operation.operation) {
                    case 1:
                    case 3: {
                        const auto target_index =
                            short_term_index(operation.operands[0]);
                        if (!target_index) {
                            throw std::runtime_error(
                                "adaptive reference marking selects an unavailable short-term picture");
                        }
                        if (operation.operation == 1) {
                            erase_reference(*target_index);
                        } else {
                            const auto long_term_index =
                                operation.operands[1];
                            for (std::size_t index =
                                     reference_long_term_frame_idxs.size();
                                 index > 0; --index) {
                                if (index - 1 != *target_index &&
                                    reference_long_term_frame_idxs[index - 1] ==
                                        long_term_index) {
                                    erase_reference(index - 1);
                                }
                            }
                            const auto updated_target_index =
                                short_term_index(operation.operands[0]);
                            if (!updated_target_index) {
                                throw std::runtime_error(
                                    "adaptive reference marking target disappeared");
                            }
                            reference_long_term_frame_idxs[
                                *updated_target_index] = long_term_index;
                        }
                        break;
                    }
                    case 2: {
                        const auto target = std::find(
                            reference_long_term_frame_idxs.begin(),
                            reference_long_term_frame_idxs.end(),
                            std::optional<std::uint32_t>{
                                operation.operands[0]});
                        if (target == reference_long_term_frame_idxs.end()) {
                            throw std::runtime_error(
                                "adaptive reference marking selects an unavailable long-term picture");
                        }
                        erase_reference(static_cast<std::size_t>(
                            std::distance(reference_long_term_frame_idxs.begin(),
                                          target)));
                        break;
                    }
                    case 4: {
                        const auto maximum_plus_one = operation.operands[0];
                        maximum_long_term_frame_idx =
                            maximum_plus_one == 0
                                ? std::nullopt
                                : std::optional<std::uint32_t>{
                                      maximum_plus_one - 1};
                        for (std::size_t index =
                                 reference_long_term_frame_idxs.size();
                             index > 0; --index) {
                            const auto long_term_index =
                                reference_long_term_frame_idxs[index - 1];
                            if (long_term_index &&
                                (!maximum_long_term_frame_idx ||
                                 *long_term_index >
                                     *maximum_long_term_frame_idx)) {
                                erase_reference(index - 1);
                            }
                        }
                        break;
                    }
                    case 5:
                        reference_frames.clear();
                        reference_frame_nums.clear();
                        reference_long_term_frame_idxs.clear();
                        reference_picture_order.clear();
                        maximum_long_term_frame_idx.reset();
                        stored_frame_num = 0;
                        current_long_term_frame_idx.reset();
                        break;
                    case 6: {
                        const auto long_term_index = operation.operands[0];
                        if (!maximum_long_term_frame_idx ||
                            long_term_index > *maximum_long_term_frame_idx) {
                            throw std::runtime_error(
                                "adaptive reference marking exceeds MaxLongTermFrameIdx");
                        }
                        for (std::size_t index =
                                 reference_long_term_frame_idxs.size();
                             index > 0; --index) {
                            if (reference_long_term_frame_idxs[index - 1] ==
                                long_term_index) {
                                erase_reference(index - 1);
                            }
                        }
                        current_long_term_frame_idx = long_term_index;
                        break;
                    }
                    default:
                        throw std::runtime_error(
                            "adaptive reference marking operation is invalid");
                    }
                }
            } else if (!idr_header) {
                const auto maximum_references = std::max<std::size_t>(
                    1, selected_sps_info.max_num_ref_frames);
                if (reference_frames.size() >= maximum_references) {
                    std::optional<std::size_t> oldest_index;
                    auto oldest_picture_number =
                        std::numeric_limits<std::int64_t>::max();
                    for (std::size_t index = 0;
                         index < reference_frames.size(); ++index) {
                        if (reference_long_term_frame_idxs[index]) {
                            continue;
                        }
                        const auto wrapped_picture_number =
                            picture_number(reference_frame_nums[index]);
                        if (wrapped_picture_number < oldest_picture_number) {
                            oldest_picture_number = wrapped_picture_number;
                            oldest_index = index;
                        }
                    }
                    if (!oldest_index) {
                        throw std::runtime_error(
                            "sliding-window marking found no short-term reference to evict");
                    }
                    reference_frames.erase(reference_frames.begin() +
                                           static_cast<std::ptrdiff_t>(
                                               *oldest_index));
                    reference_frame_nums.erase(reference_frame_nums.begin() +
                                               static_cast<std::ptrdiff_t>(
                                                   *oldest_index));
                    reference_long_term_frame_idxs.erase(
                        reference_long_term_frame_idxs.begin() +
                        static_cast<std::ptrdiff_t>(*oldest_index));
                    reference_picture_order.erase(
                        reference_picture_order.begin() +
                        static_cast<std::ptrdiff_t>(*oldest_index));
                }
            }
            decoded.long_term_reference =
                current_long_term_frame_idx.has_value();
            reference_frames.push_back(std::move(decoded));
            reference_frame_nums.push_back(stored_frame_num);
            reference_long_term_frame_idxs.push_back(
                current_long_term_frame_idx);
            reference_picture_order.push_back(sample.pts_ticks);
        }
    }
    if (!decoded_frame) {
        throw std::runtime_error("no supported H.264 picture was decoded");
    }
    save_image(output_path, *decoded_frame);
}

Options parse_options(const std::vector<std::string>& args) {
    Options options;
    for (std::size_t index = 0; index < args.size(); ++index) {
        const auto& arg = args[index];
        if (arg == "--input") {
            if (++index == args.size()) {
                throw std::invalid_argument("--input requires a value");
            }
            options.files.insert(options.files.begin(), args[index]);
        } else if (arg.rfind("--input=", 0) == 0) {
            options.files.insert(options.files.begin(), arg.substr(8));
        } else if (arg == "--extractframe") {
            if (++index == args.size()) {
                throw std::invalid_argument("--extractframe requires an index");
            }
            options.extract_frame = parse_frame_index(args[index]);
        } else if (arg.rfind("--extractframe=", 0) == 0) {
            options.extract_frame = parse_frame_index(arg.substr(15));
        } else if (arg == "--output") {
            if (++index == args.size()) {
                throw std::invalid_argument("--output requires a value");
            }
            options.output_path = args[index];
        } else if (arg.rfind("--output=", 0) == 0) {
            options.output_path = arg.substr(9);
        } else if (arg == "--prefix") {
            if (++index == args.size()) {
                throw std::invalid_argument("--prefix requires a value");
            }
            options.output_prefix = args[index];
        } else if (arg.rfind("--prefix=", 0) == 0) {
            options.output_prefix = arg.substr(9);
        } else if (!arg.empty() && arg.front() == '-') {
            throw std::invalid_argument("unknown option: " + arg);
        } else {
            options.files.push_back(arg);
        }
    }
    return options;
}

int run(const std::vector<std::string>& args) {
    if (args.size() == 1 && args[0] == "--help") {
        usage();
        return 0;
    }
    Options options;
    try {
        options = parse_options(args);
    } catch (const std::exception& error) {
        std::cerr << error.what() << '\n';
        usage();
        return 2;
    }
    if (options.files.empty()) {
        usage();
        return 2;
    }
    if (options.extract_frame.has_value() != options.output_path.has_value()) {
        std::cerr << "--extractframe and --output must be used together\n";
        usage();
        return 2;
    }
    if (options.extract_frame && options.files.size() != 1) {
        std::cerr << "frame extraction requires exactly one input file\n";
        usage();
        return 2;
    }
    if (options.extract_frame) {
        const auto output_path = prefixed_output_path(
            *options.output_path, options.output_prefix);
        try {
            extract_frame(options.files[0], *options.extract_frame,
                          output_path);
            std::cout << options.files[0] << ": wrote frame "
                      << *options.extract_frame << " to "
                      << output_path << '\n';
            return 0;
        } catch (const std::exception& error) {
            std::cerr << options.files[0] << ": " << error.what() << '\n';
            return 1;
        }
    }
    int status = 0;
    for (const auto& path : options.files) {
        try {
            const auto format = pygorvid::probe_file(path);
            if (format != "mp4") {
                std::cerr << path << ": " << format << " metadata is not supported\n";
                status = 1;
                continue;
            }
            pygorvid::Mp4File file;
            if (!file.open(path)) {
                std::cerr << path << ": " << file.error_info() << '\n';
                status = 1;
                continue;
            }
            const auto info = file.get_basic_info();
            if (!file.error_info().empty()) {
                std::cerr << path << ": mp4: " << file.error_info() << '\n';
                status = 1;
                continue;
            }
            std::cout << path << ": mp4 video_streams=" << info.videostreams.size()
                      << " audio_streams=" << info.audiostreams.size();
            for (const auto& stream : info.videostreams) {
                std::cout << " video=" << stream.width << 'x' << stream.height
                          << " frames=" << stream.frame_count
                          << " duration_seconds=" << stream.duration_seconds;
            }
            std::cout << '\n';
        } catch (const std::exception& error) {
            std::cerr << path << ": " << error.what() << '\n';
            status = 1;
        }
    }
    return status;
}

} // namespace

int main(int argc, char** argv) {
    std::vector<std::string> args;
    for (int index = 1; index < argc; ++index) {
        args.emplace_back(argv[index]);
    }
    return run(args);
}
