#include "pygorvid/h264.hpp"
#include "pygorvid/h264_annexb.hpp"
#include "pygorvid/h264_encoder.hpp"
#include "pygorvid/mp4_writer.hpp"
#include "pygorvid/jpeg.hpp"
#include "pygorvid/pygorvid.hpp"
#include "pygorvid/png.hpp"
#include "pygorvid/video_samples.hpp"
#include "pygorvid/av1.hpp"

#include <algorithm>
#include <array>
#include <cctype>
#include <chrono>
#include <filesystem>
#include <fstream>
#include <iomanip>
#include <iostream>
#include <limits>
#include <map>
#include <memory>
#include <optional>
#include <sstream>
#include <stdexcept>
#include <string>
#include <thread>
#include <vector>

#ifdef PYGORVID_SLIDESHOW
#include "slideshow.hpp"
#endif

namespace {

struct Options {
    std::vector<std::string> files;
    std::optional<std::size_t> extract_frame;
    std::optional<std::string> output_path;
    std::string output_prefix;
    std::optional<std::string> generate_path;
    bool read_all = false;
    bool hash_frames = false;
    bool slideshow = false;
    bool readahead = false;
    std::size_t threads = std::max<std::size_t>(
        1, std::thread::hardware_concurrency());
};

void usage() {
    std::cerr << "       vidprobe --generate OUTPUT.mp4\n";
    std::cerr << "usage: vidprobe [--threads N] [--readahead] [--hash] [--slideshow | --readall | "
                 "--extractframe INDEX --output IMAGE] [--prefix TEXT] "
                 "[--input FILE] FILE...\n";
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

std::size_t parse_thread_count(const std::string& value) {
    if (value.empty() || value.front() == '-') {
        throw std::invalid_argument("--threads requires a positive integer");
    }
    std::size_t parsed_characters = 0;
    const auto parsed = std::stoull(value, &parsed_characters);
    if (parsed_characters != value.size() || parsed == 0) {
        throw std::invalid_argument("--threads requires a positive integer");
    }
    return parsed;
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

std::uint32_t slice_type(const std::vector<std::uint8_t>& nal) {
    if (nal.size() < 2) {
        throw std::runtime_error("H.264 slice NAL is truncated");
    }
    const std::vector<std::uint8_t> ebsp(nal.begin() + 1, nal.end());
    const auto rbsp = pygorvid::ebsp_to_rbsp(ebsp);
    pygorvid::BitReader reader(rbsp);
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

std::string sha256_frame(const pygorvid::Yuv420Frame& frame) {
    static constexpr std::array<std::uint32_t, 64> constants{{
        0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5,
        0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
        0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3,
        0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
        0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc,
        0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
        0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7,
        0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
        0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13,
        0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
        0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3,
        0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
        0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5,
        0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
        0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208,
        0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
    }};
    std::vector<std::uint8_t> data;
    const auto frame_bytes =
        frame.luma.size() + frame.chroma_u.size() + frame.chroma_v.size();
    if (frame_bytes > data.max_size() - 72) {
        throw std::overflow_error("decoded frame is too large to hash");
    }
    data.reserve(frame_bytes + 72);
    data.insert(data.end(), frame.luma.begin(), frame.luma.end());
    data.insert(data.end(), frame.chroma_u.begin(), frame.chroma_u.end());
    data.insert(data.end(), frame.chroma_v.begin(), frame.chroma_v.end());
    const auto bit_length = static_cast<std::uint64_t>(frame_bytes) * 8;
    data.push_back(0x80);
    while (data.size() % 64 != 56) data.push_back(0);
    for (int shift = 56; shift >= 0; shift -= 8) {
        data.push_back(static_cast<std::uint8_t>(bit_length >> shift));
    }

    std::array<std::uint32_t, 8> state{{
        0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a,
        0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19,
    }};
    const auto rotate_right = [](std::uint32_t value, unsigned int count) {
        return (value >> count) | (value << (32 - count));
    };
    for (std::size_t offset = 0; offset < data.size(); offset += 64) {
        std::array<std::uint32_t, 64> schedule{};
        for (std::size_t index = 0; index < 16; ++index) {
            const auto base = offset + index * 4;
            schedule[index] =
                (static_cast<std::uint32_t>(data[base]) << 24) |
                (static_cast<std::uint32_t>(data[base + 1]) << 16) |
                (static_cast<std::uint32_t>(data[base + 2]) << 8) |
                static_cast<std::uint32_t>(data[base + 3]);
        }
        for (std::size_t index = 16; index < schedule.size(); ++index) {
            const auto previous_15 = schedule[index - 15];
            const auto previous_2 = schedule[index - 2];
            const auto sigma_0 = rotate_right(previous_15, 7) ^
                rotate_right(previous_15, 18) ^ (previous_15 >> 3);
            const auto sigma_1 = rotate_right(previous_2, 17) ^
                rotate_right(previous_2, 19) ^ (previous_2 >> 10);
            schedule[index] = schedule[index - 16] + sigma_0 +
                schedule[index - 7] + sigma_1;
        }
        auto [a, b, c, d, e, f, g, h] = state;
        for (std::size_t index = 0; index < schedule.size(); ++index) {
            const auto big_sigma_1 =
                rotate_right(e, 6) ^ rotate_right(e, 11) ^ rotate_right(e, 25);
            const auto choose = (e & f) ^ (~e & g);
            const auto temporary_1 = h + big_sigma_1 + choose +
                constants[index] + schedule[index];
            const auto big_sigma_0 =
                rotate_right(a, 2) ^ rotate_right(a, 13) ^ rotate_right(a, 22);
            const auto majority = (a & b) ^ (a & c) ^ (b & c);
            const auto temporary_2 = big_sigma_0 + majority;
            h = g;
            g = f;
            f = e;
            e = d + temporary_1;
            d = c;
            c = b;
            b = a;
            a = temporary_1 + temporary_2;
        }
        const std::array<std::uint32_t, 8> additions{{
            a, b, c, d, e, f, g, h,
        }};
        for (std::size_t index = 0; index < state.size(); ++index) {
            state[index] += additions[index];
        }
    }
    std::ostringstream output;
    output << std::hex << std::setfill('0');
    for (const auto word : state) output << std::setw(8) << word;
    return output.str();
}

std::size_t decode_video(
    const std::string& input_path,
    const std::optional<std::size_t>& frame_index = std::nullopt,
    const std::optional<std::string>& output_path = std::nullopt,
    bool slideshow = false, std::size_t threads = 0,
    bool hash_frames = false) {
#ifndef PYGORVID_SLIDESHOW
    if (slideshow) throw std::runtime_error(
        "optional slideshow build feature was not included; rebuild with -DPYGORVID_SLIDESHOW=ON");
#else
    std::unique_ptr<Slideshow> window;
    std::map<std::size_t, std::pair<pygorvid::Yuv420Frame, std::uint32_t>> pending;
    std::size_t displayed_count = 0;
    auto deadline = std::chrono::steady_clock::now();
#endif
    std::vector<std::size_t> display_indices;
    if (output_path) {
        std::error_code path_error;
        const auto normalized_input =
            std::filesystem::absolute(input_path, path_error).lexically_normal();
        if (path_error) {
            throw std::runtime_error("cannot resolve input file path");
        }
        const auto normalized_output =
            std::filesystem::absolute(*output_path, path_error).lexically_normal();
        if (path_error || normalized_input == normalized_output) {
            throw std::invalid_argument(
                "image output path must not overwrite the input video");
        }
        const bool output_exists =
            std::filesystem::exists(*output_path, path_error);
        if (path_error) {
            throw std::runtime_error("cannot inspect image output path");
        }
        if (output_exists &&
            std::filesystem::equivalent(input_path, *output_path, path_error)) {
            throw std::invalid_argument(
                "image output path must not overwrite the input video");
        }
        if (path_error) {
            throw std::runtime_error("cannot compare input and output paths");
        }
    }

    pygorvid::VideoSampleReader sample_reader;
    if (!sample_reader.open(input_path)) {
        throw std::runtime_error(sample_reader.error_info());
    }
    if (sample_reader.codec() == pygorvid::VideoCodec::AV1) {
        pygorvid::AV1Decoder decoder;
        decoder.check_available();
    }
#ifdef PYGORVID_SLIDESHOW
    if (slideshow) {
        window = std::make_unique<Slideshow>(input_path);
    }
#endif
    display_indices.resize(sample_reader.sample_count());
    const auto presentation_order = sample_reader.presentation_sample_indices();
    for (std::size_t index = 0; index < presentation_order.size(); ++index) {
        display_indices[presentation_order[index]] = index;
    }
    if (frame_index && *frame_index >= sample_reader.sample_count()) {
        throw std::out_of_range("requested frame index exceeds the sample count");
    }
    std::optional<std::size_t> target_sample_index;
    if (frame_index) {
        target_sample_index.emplace(
            sample_reader.presentation_sample_index(*frame_index));
    }
    const auto sample_count = target_sample_index
        ? *target_sample_index + 1
        : sample_reader.sample_count();
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
    std::int64_t previous_poc_msb = 0;
    std::int64_t previous_poc_lsb = 0;
    std::uint32_t previous_poc_frame_num = 0;
    std::int64_t previous_poc_frame_num_offset = 0;
    std::optional<std::uint32_t> maximum_long_term_frame_idx;
    std::optional<pygorvid::Yuv420Frame> decoded_frame;
    std::size_t decoded_count = 0;
    std::vector<std::string> frame_hashes;
    std::size_t next_hash_frame = 0;
    if (hash_frames && !frame_index) {
        frame_hashes.resize(sample_reader.sample_count());
    }
    for (std::size_t sample_index = 0; sample_index < sample_count;
         ++sample_index) {
        const auto sample = sample_reader.sample_at(sample_index);
        auto nals = pygorvid::split_h264_sample_nals(
            sample.data, configuration.nal_length_size);
        for (const auto& nal : nals) {
            if (pygorvid::parse_nal_header(nal).unit_type != 2) {
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
                    "partitioned slice references a PPS missing from avcC");
            }
            const auto sps = std::find_if(
                sequence_parameter_sets.begin(), sequence_parameter_sets.end(),
                [&pps](const auto& entry) {
                    return entry.sps_id == pps->sequence_parameter_set_id;
                });
            if (sps == sequence_parameter_sets.end()) {
                throw std::runtime_error(
                    "partitioned slice PPS references an SPS missing from avcC");
            }
            nals = pygorvid::coalesce_data_partition_slices(
                nals, *sps, *pps);
            break;
        }
        std::vector<std::vector<std::uint8_t>> slices;
        const pygorvid::SpsInfo* selected_sps = nullptr;
        const pygorvid::PpsInfo* selected_pps = nullptr;
        for (const auto& nal : nals) {
            const auto header = pygorvid::parse_nal_header(nal);
            if (header.unit_type != 1 && header.unit_type != 5) {
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
            if (target_sample_index && sample_index == *target_sample_index) {
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
        const auto normalized_slice_type = slice_type(slices.front()) % 5;
        const bool is_i_slice =
            normalized_slice_type == 2 || normalized_slice_type == 4;
        const bool is_idr_slice =
            pygorvid::parse_nal_header(slices.front()).unit_type == 5;
        std::optional<pygorvid::PSliceHeader> slice_header;
        std::optional<pygorvid::IdrSliceHeader> idr_header;
        if (is_i_slice) {
            idr_header.emplace(pygorvid::parse_idr_i_slice_header(
                slices.front(), selected_sps_info, selected_pps_info));
        } else {
            slice_header.emplace(pygorvid::parse_p_slice_header(
                slices.front(), selected_sps_info, selected_pps_info));
        }
        const auto current_frame_num =
            slice_header
                ? slice_header->frame_num
                : idr_header->frame_num;
        const auto current_nal_ref_idc =
            pygorvid::parse_nal_header(slices.front()).reference_idc;
        const auto maximum_frame_num =
            std::uint32_t{1}
            << (selected_sps_info.log2_max_frame_num_minus4 + 4);
        std::int64_t current_picture_order_count = 0;
        if (selected_sps_info.pic_order_cnt_type == 0) {
            const auto maximum_poc_lsb = std::int64_t{1} <<
                (*selected_sps_info.log2_max_pic_order_cnt_lsb_minus4 + 4);
            const auto prior_msb = is_idr_slice ? 0 : previous_poc_msb;
            const auto prior_lsb = is_idr_slice ? 0 : previous_poc_lsb;
            const auto poc_lsb = slice_header
                ? slice_header->pic_order_cnt_lsb
                : idr_header->pic_order_cnt_lsb;
            auto current_msb = prior_msb;
            if (poc_lsb < prior_lsb &&
                prior_lsb - poc_lsb >= maximum_poc_lsb / 2) {
                current_msb += maximum_poc_lsb;
            } else if (poc_lsb > prior_lsb &&
                       poc_lsb - prior_lsb > maximum_poc_lsb / 2) {
                current_msb -= maximum_poc_lsb;
            }
            current_picture_order_count = current_msb + poc_lsb;
            if (current_nal_ref_idc != 0) {
                previous_poc_msb = current_msb;
                previous_poc_lsb = poc_lsb;
            }
        } else {
            if (is_idr_slice) {
                previous_poc_frame_num = 0;
                previous_poc_frame_num_offset = 0;
            }
            auto frame_num_offset = previous_poc_frame_num_offset;
            if (previous_poc_frame_num > current_frame_num) {
                frame_num_offset += maximum_frame_num;
            }
            if (selected_sps_info.pic_order_cnt_type == 2 && !is_idr_slice) {
                current_picture_order_count =
                    2 * (frame_num_offset + current_frame_num);
                if (current_nal_ref_idc == 0) {
                    --current_picture_order_count;
                }
            }
            if (current_nal_ref_idc != 0) {
                previous_poc_frame_num = current_frame_num;
                previous_poc_frame_num_offset = frame_num_offset;
            }
        }
        if (is_idr_slice) {
            reference_frames.clear();
            reference_frame_nums.clear();
            reference_long_term_frame_idxs.clear();
            reference_picture_order.clear();
            maximum_long_term_frame_idx =
                idr_header->long_term_reference
                    ? std::optional<std::uint32_t>{0}
                    : std::nullopt;
        }
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
            reference_list_l1, display_indices[sample_index],
            current_picture_order_count, threads);
        decoded.picture_order_count = current_picture_order_count;
#ifdef PYGORVID_SLIDESHOW
        if (window) {
            pending.emplace(display_indices[sample_index], std::make_pair(decoded, sample.duration_ticks));
            while (!pending.empty() && pending.begin()->first == displayed_count) {
                if (!window->wait(deadline)) return displayed_count;
                window->show(pending.begin()->second.first);
                const auto duration_seconds = std::chrono::duration<double>(
                    static_cast<double>(pending.begin()->second.second) / sample_reader.timescale());
                deadline = std::chrono::steady_clock::now() +
                    std::chrono::duration_cast<std::chrono::steady_clock::duration>(duration_seconds);
                pending.erase(pending.begin());
                ++displayed_count;
            }
        }
#endif
        decoded_frame = decoded;
        if (hash_frames) {
            const auto digest = sha256_frame(decoded);
            if (frame_index) {
                if (sample_index == *target_sample_index) {
                    std::cout << input_path << ": frame " << *frame_index
                              << " sha256=" << digest << '\n';
                }
            } else {
                frame_hashes[display_indices[sample_index]] = digest;
                while (next_hash_frame < frame_hashes.size() &&
                       !frame_hashes[next_hash_frame].empty()) {
                    std::cout << input_path << ": frame " << next_hash_frame
                              << " sha256=" << frame_hashes[next_hash_frame]
                              << '\n';
                    frame_hashes[next_hash_frame].clear();
                    ++next_hash_frame;
                }
            }
        }
        ++decoded_count;
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
    if (output_path) {
        save_image(*output_path, *decoded_frame);
    }
#ifdef PYGORVID_SLIDESHOW
    if (window) {
        if (displayed_count) window->wait(deadline);
        return displayed_count;
    }
#endif
    return decoded_count;
}

Options parse_options(const std::vector<std::string>& args) {
    Options options;
    for (std::size_t index = 0; index < args.size(); ++index) {
        const auto& arg = args[index];
        if (arg == "--generate") {
            if (++index == args.size()) {
                throw std::invalid_argument("--generate requires a value");
            }
            options.generate_path = args[index];
        } else if (arg.rfind("--generate=", 0) == 0) {
            options.generate_path = arg.substr(11);
        } else if (arg == "--input") {
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
        } else if (arg == "--readall") {
            options.read_all = true;
        } else if (arg == "--hash") {
            options.hash_frames = true;
        } else if (arg == "--slideshow") {
            options.slideshow = true;
        } else if (arg == "--readahead") {
            options.readahead = true;
        } else if (arg == "--threads") {
            if (++index == args.size()) {
                throw std::invalid_argument("--threads requires a positive integer");
            }
            options.threads = parse_thread_count(args[index]);
        } else if (arg.rfind("--threads=", 0) == 0) {
            options.threads = parse_thread_count(arg.substr(10));
        } else if (!arg.empty() && arg.front() == '-') {
            throw std::invalid_argument("unknown option: " + arg);
        } else {
            options.files.push_back(arg);
        }
    }
    return options;
}

void generate_video(const std::string& output_path) {
    const pygorvid::Mp4Metadata metadata{
        "Title", "Artist", "Album", "Publisher", "Language", "Now Playing",
        "Album Artist", "Genre", "1", "1", "2016-12-31 23:59:60 UTC",
        "Composer", "Comments", "Description", "Copyright", "Camera make", "Camera model",
        "firmware version", u8"11.3733\u00b0 N 142.5917\u00b0 E", "-10925.0 meters",
        "180.0", "pygorvid",
    };
    const auto payload = pygorvid::Mp4Writer().encode(pygorvid::H264Encoder(), &metadata);
    const auto companions = pygorvid::generate_h264_companion_files();
    const std::filesystem::path output(output_path);
    std::error_code output_error;
    if (std::filesystem::is_directory(output, output_error)) {
        throw std::runtime_error("video output path is a directory");
    }
    if (output_error &&
        output_error != std::errc::no_such_file_or_directory) {
        throw std::runtime_error("cannot inspect video output path: " +
                                 output_error.message());
    }
    const auto parent = output.has_parent_path() ? output.parent_path()
                                                 : std::filesystem::path(".");
    std::vector<std::filesystem::path> companion_paths;
    companion_paths.reserve(companions.size());
    for (const auto& companion : companions) {
        const auto path =
            parent / (output.stem().string() + "-" + companion.suffix);
        std::error_code error;
        if (std::filesystem::is_directory(path, error)) {
            throw std::runtime_error(
                "companion output path is a directory: " + path.string());
        }
        if (error && error != std::errc::no_such_file_or_directory) {
            throw std::runtime_error(
                "cannot inspect companion output path " + path.string() + ": " +
                error.message());
        }
        companion_paths.push_back(path);
    }
    const auto nonce = std::chrono::steady_clock::now().time_since_epoch().count();
    std::filesystem::path temporary;
    for (int attempt = 0; attempt < 100; ++attempt) {
        const auto candidate = parent / (".pygorvid-" + std::to_string(nonce) +
                                         "-" + std::to_string(attempt));
        if (std::filesystem::create_directory(candidate)) {
            temporary = candidate;
            break;
        }
    }
    if (temporary.empty()) {
        throw std::runtime_error("cannot create temporary output directory");
    }
    struct Cleanup {
        std::filesystem::path directory;
        ~Cleanup() {
            std::error_code error;
            std::filesystem::remove_all(directory, error);
            if (error) {
                std::cerr << "cannot clean temporary output directory: " << error.message() << '\n';
            }
        }
    } cleanup{temporary};
    const auto video = temporary / "video";
    std::ofstream file(video, std::ios::binary);
    if (!file) {
        throw std::runtime_error("cannot open video output file");
    }
    file.write(reinterpret_cast<const char*>(payload.data()),
               static_cast<std::streamsize>(payload.size()));
    file.close();
    if (!file) {
        throw std::runtime_error("cannot write complete video output file");
    }
    for (std::size_t index = 0; index < companions.size(); ++index) {
        std::ofstream companion(temporary / std::to_string(index),
                                std::ios::binary);
        if (!companion) {
            throw std::runtime_error("cannot open companion output file");
        }
        companion.write(
            reinterpret_cast<const char*>(companions[index].data.data()),
            static_cast<std::streamsize>(companions[index].data.size()));
        companion.close();
        if (!companion) {
            throw std::runtime_error(
                "cannot write complete companion output file");
        }
    }
    for (std::size_t index = 0; index < companions.size(); ++index) {
        std::filesystem::rename(temporary / std::to_string(index),
                                companion_paths[index]);
    }
    std::filesystem::rename(video, output);
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
    if (options.generate_path) {
        if (!options.files.empty() || options.extract_frame || options.output_path ||
            options.read_all || options.hash_frames || options.slideshow) {
            std::cerr << "--generate cannot be combined with inputs, --readall, --hash, --extractframe or --output\n";
            return 2;
        }
        auto extension = std::filesystem::path(*options.generate_path).extension().string();
        std::transform(extension.begin(), extension.end(), extension.begin(),
                       [](unsigned char ch) { return static_cast<char>(std::tolower(ch)); });
        if (extension != ".mp4") {
            std::cerr << "--generate must end in .mp4\n";
            return 2;
        }
        const auto output_path = prefixed_output_path(
            *options.generate_path, options.output_prefix);
        try {
            generate_video(output_path);
            std::cout << output_path
                      << ": generated one second of 64x64 H.264 feature-test video with stereo silence"
                      << " and 15 companion files\n";
            return 0;
        } catch (const std::exception& error) {
            std::cerr << output_path << ": " << error.what() << '\n';
            return 1;
        }
    }
    if (options.files.empty()) {
        usage();
        return 2;
    }
    if ((options.read_all && options.slideshow) ||
        ((options.read_all || options.slideshow) && (options.extract_frame || options.output_path))) {
        std::cerr << "--slideshow, --readall and extraction options cannot be combined\n";
        usage();
        return 2;
    }
    if (options.hash_frames && !options.read_all &&
        !options.extract_frame) {
        std::cerr << "--hash requires --readall or --extractframe\n";
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
            (void)decode_video(options.files[0], options.extract_frame,
                               output_path, false, options.threads,
                               options.hash_frames);
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
        if (options.slideshow) {
            try {
                const auto count = decode_video(
                    path, std::nullopt, std::nullopt, true, options.threads);
                std::cout << path << ": displayed " << count << " frames\n";
            } catch (const std::exception& error) {
                std::cerr << path << ": " << error.what() << '\n';
                status = 1;
            }
            continue;
        }
        if (options.read_all) {
            try {
                const auto decoded_count = decode_video(
                    path, std::nullopt, std::nullopt, false, options.threads,
                    options.hash_frames);
                std::cout << path << ": read " << decoded_count << " frames\n";
            } catch (const std::exception& error) {
                std::cerr << path << ": " << error.what() << '\n';
                status = 1;
            }
            continue;
        }
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
