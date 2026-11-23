#include "pygorvid/decoder.hpp"
#include "pygorvid/pygorvid.hpp"
#include "pygorvid/h264.hpp"
#include "pygorvid/video_samples.hpp"
#include "pygorvid/av1.hpp"

#include <algorithm>
#include <array>
#include <fstream>
#include <limits>
#include <optional>
#include <stdexcept>
#include <future>
#include <functional>
#include <map>
#include <thread>
#include <unistd.h>
#include <unordered_set>
#include <utility>

namespace pygorvid {
namespace {

constexpr std::size_t kHeaderSize = 380;

std::uint32_t be32(const std::array<std::uint8_t, 4>& bytes) {
    return (static_cast<std::uint32_t>(bytes[0]) << 24) |
           (static_cast<std::uint32_t>(bytes[1]) << 16) |
           (static_cast<std::uint32_t>(bytes[2]) << 8) |
           static_cast<std::uint32_t>(bytes[3]);
}

std::uint32_t read_u32(std::ifstream& file, std::uint64_t offset,
                       std::uint64_t limit, const char* error) {
    if (offset > limit || limit - offset < 4) {
        throw std::runtime_error(error);
    }
    std::array<std::uint8_t, 4> bytes{};
    file.seekg(static_cast<std::streamoff>(offset));
    file.read(reinterpret_cast<char*>(bytes.data()), 4);
    if (!file) {
        throw std::runtime_error(error);
    }
    return be32(bytes);
}

struct Box {
    std::string type;
    std::uint64_t start;
    std::uint64_t payload;
    std::uint64_t end;
    std::vector<Box> children;
};

bool is_container(const std::string& type) {
    return type == "moov" || type == "trak" || type == "mdia" ||
           type == "minf" || type == "stbl" || type == "edts" ||
           type == "dinf";
}

std::vector<Box> read_boxes(std::ifstream& file, std::uint64_t start,
                            std::uint64_t end, unsigned depth = 0) {
    if (depth > 16) {
        throw std::runtime_error("MP4 box nesting exceeds 16 levels");
    }
    std::vector<Box> boxes;
    auto offset = start;
    while (offset < end) {
        if (end - offset < 8) {
            throw std::runtime_error("truncated MP4 box header");
        }
        std::array<std::uint8_t, 8> header{};
        file.seekg(static_cast<std::streamoff>(offset));
        file.read(reinterpret_cast<char*>(header.data()), 8);
        if (!file) {
            throw std::runtime_error("cannot read MP4 box header");
        }
        const auto size32 = be32({header[0], header[1], header[2], header[3]});
        std::string type(reinterpret_cast<const char*>(header.data() + 4), 4);
        std::uint64_t header_size = 8;
        std::uint64_t box_size = size32;
        if (size32 == 1) {
            if (end - offset < 16) {
                throw std::runtime_error("truncated extended MP4 box header");
            }
            std::array<std::uint8_t, 8> extended{};
            file.read(reinterpret_cast<char*>(extended.data()), 8);
            if (!file) {
                throw std::runtime_error("cannot read extended MP4 box size");
            }
            box_size = 0;
            for (const auto byte : extended) {
                box_size = (box_size << 8) | byte;
            }
            header_size = 16;
        } else if (size32 == 0) {
            box_size = end - offset;
        }
        if (box_size < header_size || box_size > end - offset) {
            throw std::runtime_error("MP4 box size exceeds parent bounds");
        }
        Box box{type, offset, offset + header_size, offset + box_size, {}};
        if (is_container(type)) {
            box.children = read_boxes(file, box.payload, box.end, depth + 1);
        }
        boxes.push_back(std::move(box));
        offset += box_size;
    }
    return boxes;
}

std::vector<const Box*> find_boxes(const std::vector<Box>& boxes,
                                   const std::vector<std::string>& path,
                                   std::size_t index = 0) {
    std::vector<const Box*> found;
    if (index >= path.size()) {
        return found;
    }
    for (const auto& box : boxes) {
        if (box.type != path[index]) {
            continue;
        }
        if (index + 1 == path.size()) {
            found.push_back(&box);
        } else {
            auto nested = find_boxes(box.children, path, index + 1);
            found.insert(found.end(), nested.begin(), nested.end());
        }
    }
    return found;
}

std::vector<const Box*> find_in_track(const Box& track,
                                      const std::vector<std::string>& path) {
    return find_boxes(track.children, path);
}

std::uint32_t box_u32(std::ifstream& file, const Box& box,
                      std::uint64_t offset, const char* error) {
    return read_u32(file, offset, box.end, error);
}

std::uint8_t box_u8(std::ifstream& file, const Box& box,
                    std::uint64_t offset, const char* error) {
    if (offset >= box.end) {
        throw std::runtime_error(error);
    }
    char value = 0;
    file.seekg(static_cast<std::streamoff>(offset));
    file.get(value);
    if (!file) {
        throw std::runtime_error(error);
    }
    return static_cast<std::uint8_t>(value);
}

BasicVideoStreamInfo video_stream_info(std::ifstream& file, const Box& track) {
    BasicVideoStreamInfo stream;
    for (const auto* box : find_in_track(track, {"tkhd"})) {
        const auto payload_size = box->end - box->payload;
        if (payload_size < 84) {
            throw std::runtime_error("tkhd box too short");
        }
        const auto version = box_u8(file, *box, box->payload, "tkhd box too short");
        const std::uint64_t dimensions_offset = version == 1 ? 88 : 76;
        if (payload_size < dimensions_offset + 8) {
            throw std::runtime_error("tkhd box too short");
        }
        stream.width = box_u32(file, *box, box->payload + dimensions_offset,
                               "tkhd box too short") >> 16;
        stream.height = box_u32(file, *box, box->payload + dimensions_offset + 4,
                                "tkhd box too short") >> 16;
    }

    std::uint32_t timescale = 0;
    for (const auto* box : find_in_track(track, {"mdia", "mdhd"})) {
        const auto payload_size = box->end - box->payload;
        if (payload_size < 20) {
            throw std::runtime_error("mdhd box too short");
        }
        const auto version = box_u8(file, *box, box->payload, "mdhd box too short");
        const std::uint64_t timescale_offset = version == 1 ? 20 : 12;
        if (payload_size < timescale_offset + 4) {
            throw std::runtime_error("mdhd box too short");
        }
        timescale = box_u32(file, *box, box->payload + timescale_offset,
                            "mdhd box too short");
    }

    long double duration_ticks = 0;
    for (const auto* box : find_in_track(track, {"mdia", "minf", "stbl", "stts"})) {
        if (box->end - box->payload < 8) {
            throw std::runtime_error("stts box too short");
        }
        const auto count = box_u32(file, *box, box->payload + 4, "stts box too short");
        const auto entries_start = box->payload + 8;
        if (count > (box->end - entries_start) / 8) {
            throw std::runtime_error("stts entries exceed box size");
        }
        for (std::uint32_t i = 0; i < count; ++i) {
            const auto entry = entries_start + static_cast<std::uint64_t>(i) * 8;
            const auto sample_count = box_u32(file, *box, entry, "stts box too short");
            const auto sample_delta = box_u32(file, *box, entry + 4, "stts box too short");
            stream.frame_count += sample_count;
            duration_ticks += static_cast<long double>(sample_count) * sample_delta;
        }
    }
    if (timescale != 0 && duration_ticks > 0) {
        stream.duration_seconds = static_cast<double>(duration_ticks / timescale);
        stream.framerate = static_cast<double>(stream.frame_count) / stream.duration_seconds;
    }
    return stream;
}

BasicAudioStreamInfo audio_stream_info(std::ifstream& file, const Box& track) {
    BasicAudioStreamInfo stream;
    for (const auto* stsd : find_in_track(track, {"mdia", "minf", "stbl", "stsd"})) {
        if (stsd->end - stsd->payload < 8) {
            throw std::runtime_error("stsd box too short");
        }
        const auto entry_count = box_u32(file, *stsd, stsd->payload + 4, "stsd box too short");
        auto offset = stsd->payload + 8;
        for (std::uint32_t i = 0; i < entry_count; ++i) {
            if (stsd->end - offset < 8) {
                throw std::runtime_error("sample entry box too short");
            }
            std::array<std::uint8_t, 8> header{};
            file.seekg(static_cast<std::streamoff>(offset));
            file.read(reinterpret_cast<char*>(header.data()), 8);
            if (!file) {
                throw std::runtime_error("cannot read sample entry box");
            }
            const auto size = be32({header[0], header[1], header[2], header[3]});
            if (size < 8 || size > stsd->end - offset) {
                throw std::runtime_error("sample entry box exceeds stsd bounds");
            }
            const std::string type(reinterpret_cast<const char*>(header.data() + 4), 4);
            if (type == "mp4a") {
                const auto payload = offset + 8;
                if (size < 36) {
                    throw std::runtime_error("audio sample entry too short");
                }
                std::array<std::uint8_t, 4> values{};
                file.seekg(static_cast<std::streamoff>(payload + 16));
                file.read(reinterpret_cast<char*>(values.data()), 4);
                if (!file) {
                    throw std::runtime_error("audio sample entry too short");
                }
                stream.channels = static_cast<std::uint16_t>(
                    (static_cast<std::uint16_t>(values[0]) << 8) | values[1]);
                std::array<std::uint8_t, 4> rate{};
                file.seekg(static_cast<std::streamoff>(payload + 24));
                file.read(reinterpret_cast<char*>(rate.data()), 4);
                if (!file) {
                    throw std::runtime_error("audio sample entry too short");
                }
                stream.sample_rate = be32(rate) >> 16;
                break;
            }
            offset += size;
        }
    }
    return stream;
}

std::uint64_t read_u64(std::ifstream& file, std::uint64_t offset,
                       std::uint64_t limit, const char* error) {
    if (offset > limit || limit - offset < 8) {
        throw std::runtime_error(error);
    }
    std::array<std::uint8_t, 8> bytes{};
    file.seekg(static_cast<std::streamoff>(offset));
    file.read(reinterpret_cast<char*>(bytes.data()), 8);
    if (!file) {
        throw std::runtime_error(error);
    }
    std::uint64_t value = 0;
    for (const auto byte : bytes) {
        value = (value << 8) | byte;
    }
    return value;
}

std::vector<std::uint8_t> read_box_payload(std::ifstream& file, const Box& box) {
    const auto payload_size = box.end - box.payload;
    if (payload_size > std::numeric_limits<std::size_t>::max() ||
        payload_size > static_cast<std::uint64_t>(
                           std::numeric_limits<std::streamsize>::max())) {
        throw std::runtime_error("MP4 box payload exceeds supported size");
    }
    std::vector<std::uint8_t> data(static_cast<std::size_t>(payload_size));
    file.seekg(static_cast<std::streamoff>(box.payload));
    file.read(reinterpret_cast<char*>(data.data()),
              static_cast<std::streamsize>(data.size()));
    if (!file) {
        throw std::runtime_error("cannot read MP4 box payload");
    }
    return data;
}

H264SampleConfiguration parse_avc_configuration(std::ifstream& file,
                                                 const Box& box) {
    const auto data = read_box_payload(file, box);
    if (data.size() < 7 || data[0] != 1) {
        throw std::runtime_error("invalid or truncated avcC configuration");
    }

    if ((data[4] & 0xfcU) != 0xfcU || (data[5] & 0xe0U) != 0xe0U) {
        throw std::runtime_error("avcC reserved bits are invalid");
    }
    const auto nal_length_size =
        static_cast<std::uint8_t>((data[4] & 0x03U) + 1U);
    if (nal_length_size != 4) {
        throw std::runtime_error(
            "only four-byte AVC NAL length prefixes are supported");
    }

    auto read_nals = [&data](std::size_t& offset, std::size_t count,
                             std::uint8_t expected_type) {
        std::vector<std::vector<std::uint8_t>> nals;
        nals.reserve(count);
        for (std::size_t index = 0; index < count; ++index) {
            if (data.size() - offset < 2) {
                throw std::runtime_error("avcC NAL length is truncated");
            }
            const auto size = (static_cast<std::size_t>(data[offset]) << 8) |
                              static_cast<std::size_t>(data[offset + 1]);
            offset += 2;
            if (size == 0 || size > data.size() - offset) {
                throw std::runtime_error("avcC NAL exceeds configuration size");
            }
            if ((data[offset] & 0x80U) != 0 ||
                (data[offset] & 0x1fU) != expected_type) {
                throw std::runtime_error(
                    "avcC parameter-set NAL has an unexpected type");
            }
            nals.emplace_back(data.begin() + static_cast<std::ptrdiff_t>(offset),
                              data.begin() + static_cast<std::ptrdiff_t>(offset + size));
            offset += size;
        }
        return nals;
    };

    const auto sequence_count = static_cast<std::size_t>(data[5] & 0x1fU);
    if (sequence_count == 0) {
        throw std::runtime_error("avcC contains no sequence parameter sets");
    }
    std::size_t offset = 6;
    auto sequence_parameter_sets = read_nals(offset, sequence_count, 7);
    if (offset >= data.size()) {
        throw std::runtime_error("avcC picture-set count is missing");
    }
    const auto picture_count = static_cast<std::size_t>(data[offset++]);
    if (picture_count == 0) {
        throw std::runtime_error("avcC contains no picture parameter sets");
    }
    auto picture_parameter_sets = read_nals(offset, picture_count, 8);
    if (offset < data.size()) {
        const auto has_extension = [&data] {
            switch (data[1]) {
            case 44: case 83: case 86: case 100: case 110: case 118:
            case 122: case 128: case 134: case 135: case 138: case 139:
            case 144: case 244:
                return true;
            default:
                return false;
            }
        }();
        if (!has_extension || data.size() - offset < 4) {
            throw std::runtime_error("avcC has invalid trailing data");
        }
        if ((data[offset] & 0xfcU) != 0xfcU ||
            (data[offset + 1] & 0xf8U) != 0xf8U ||
            (data[offset + 2] & 0xf8U) != 0xf8U) {
            throw std::runtime_error("avcC extension reserved bits are invalid");
        }
        const auto extension_count =
            static_cast<std::size_t>(data[offset + 3]);
        offset += 4;
        (void)read_nals(offset, extension_count, 13);
        if (offset != data.size()) {
            throw std::runtime_error("avcC has invalid trailing data");
        }
    }

    return H264SampleConfiguration{
        data[1], data[2], data[3], nal_length_size,
        std::move(sequence_parameter_sets), std::move(picture_parameter_sets)};
}

AV1SampleConfiguration parse_av1_configuration(std::ifstream& file,
                                                const Box& box) {
    const auto data = read_box_payload(file, box);
    if (data.size() < 4 || data[0] != 0x81U) {
        throw std::runtime_error(
            "invalid or truncated AV1DecoderConfigurationRecord");
    }
    if ((data[3] & 0xe0U) != 0) {
        throw std::runtime_error("av1C reserved bits are invalid");
    }
    AV1SampleConfiguration result;
    result.profile = static_cast<std::uint8_t>(data[1] >> 5U);
    result.level = static_cast<std::uint8_t>(data[1] & 0x1fU);
    result.tier = static_cast<std::uint8_t>((data[2] >> 7U) & 1U);
    const bool high_bitdepth = (data[2] & 0x40U) != 0;
    const bool twelve_bit = (data[2] & 0x20U) != 0;
    result.bit_depth = twelve_bit ? 12 : high_bitdepth ? 10 : 8;
    result.monochrome = (data[2] & 0x10U) != 0;
    result.chroma_subsampling_x = (data[2] & 0x08U) != 0;
    result.chroma_subsampling_y = (data[2] & 0x04U) != 0;
    result.chroma_sample_position =
        static_cast<std::uint8_t>(data[2] & 0x03U);
    result.config_obus.assign(data.begin() + 4, data.end());
    return result;
}

void require_full_box_version_zero(std::ifstream& file, const Box& box,
                                   const char* name) {
    if (box.end - box.payload < 4 ||
        box_u8(file, box, box.payload, "full box is too short") != 0) {
        throw std::runtime_error(std::string("unsupported ") + name +
                                 " version");
    }
}

std::vector<std::int64_t> expand_sample_timing(
    const std::vector<std::pair<std::uint32_t, std::uint32_t>>& entries,
    std::size_t sample_count, const char* box_name) {
    std::vector<std::int64_t> values;
    values.reserve(sample_count);
    for (const auto& entry : entries) {
        if (entry.first > sample_count - values.size()) {
            throw std::runtime_error(std::string(box_name) +
                                     " timing entries exceed sample count");
        }
        values.insert(values.end(), entry.first,
                      static_cast<std::int64_t>(entry.second));
    }
    if (values.size() != sample_count) {
        throw std::runtime_error(std::string(box_name) +
                                 " timing entries do not cover every sample");
    }
    return values;
}

std::vector<std::int64_t> expand_composition_timing(
    const std::vector<std::pair<std::uint32_t, std::int64_t>>& entries,
    std::size_t sample_count) {
    std::vector<std::int64_t> values;
    values.reserve(sample_count);
    for (const auto& entry : entries) {
        if (entry.first > sample_count - values.size()) {
            throw std::runtime_error("ctts entries exceed sample count");
        }
        values.insert(values.end(), entry.first, entry.second);
    }
    if (values.size() != sample_count) {
        throw std::runtime_error("ctts entries do not cover every sample");
    }
    return values;
}

} // namespace

BitReader::BitReader(const std::vector<std::uint8_t>& data) : data_(data) {}

std::uint32_t BitReader::read_bits(std::uint8_t count) {
    if (count > 32) {
        throw std::invalid_argument("bit read width must be between 0 and 32");
    }
    if (data_.size() > std::numeric_limits<std::size_t>::max() / 8 ||
        bit_offset_ > data_.size() * 8 ||
        count > data_.size() * 8 - bit_offset_) {
        throw std::runtime_error("truncated bitstream");
    }
    std::uint32_t value = 0;
    for (std::uint8_t i = 0; i < count; ++i) {
        const auto byte_index = bit_offset_ / 8;
        const auto shift = 7 - bit_offset_ % 8;
        value = (value << 1) | ((data_[byte_index] >> shift) & 1U);
        ++bit_offset_;
    }
    return value;
}

bool BitReader::read_bit() {
    return read_bits(1) != 0;
}

std::size_t BitReader::bit_offset() const noexcept {
    return bit_offset_;
}

void BitReader::restore_bit_offset(std::size_t bit_offset) {
    if (data_.size() > std::numeric_limits<std::size_t>::max() / 8 ||
        bit_offset > data_.size() * 8) {
        throw std::invalid_argument("bit offset exceeds bitstream length");
    }
    bit_offset_ = bit_offset;
}

void BitReader::align_to_byte() noexcept {
    const auto remainder = bit_offset_ % 8;
    if (remainder != 0) {
        bit_offset_ += 8 - remainder;
    }
}

bool BitReader::more_rbsp_data() const noexcept {
    const auto bit_count = data_.size() * 8;
    if (bit_offset_ >= bit_count) {
        return false;
    }
    const auto bit_at = [this](std::size_t offset) {
        return ((data_[offset / 8] >> (7 - offset % 8)) & 1U) != 0;
    };
    if (!bit_at(bit_offset_)) {
        return true;
    }
    for (auto offset = bit_offset_ + 1; offset < bit_count; ++offset) {
        if (bit_at(offset)) {
            return true;
        }
    }
    return false;
}

std::uint32_t BitReader::read_ue() {
    const auto start = bit_offset_;
    unsigned leading_zeros = 0;
    try {
        while (!read_bit()) {
            if (++leading_zeros > 32) {
                throw std::runtime_error("Exp-Golomb value exceeds uint32");
            }
        }
        const auto suffix = read_bits(static_cast<std::uint8_t>(leading_zeros));
        const auto value = ((std::uint64_t{1} << leading_zeros) - 1) + suffix;
        if (value > std::numeric_limits<std::uint32_t>::max()) {
            throw std::runtime_error("Exp-Golomb value exceeds uint32");
        }
        return static_cast<std::uint32_t>(value);
    } catch (...) {
        bit_offset_ = start;
        throw;
    }
}

std::int64_t BitReader::read_se() {
    const auto code_number = read_ue();
    const auto magnitude = (static_cast<std::uint64_t>(code_number) + 1) / 2;
    return (code_number & 1U) == 0
        ? -static_cast<std::int64_t>(magnitude)
        : static_cast<std::int64_t>(magnitude);
}

std::string detect_format(const std::vector<std::uint8_t>& header) {
    const auto at = [&header](std::size_t offset, const std::string& value) {
        return header.size() >= offset + value.size() &&
               std::equal(value.begin(), value.end(), header.begin() + offset);
    };
    if (at(4, "ftyp")) {
        return at(8, "qt  ") ? "mov" : "mp4";
    }
    if (at(0, std::string("\x1a\x45\xdf\xa3", 4))) return "matroska";
    if (at(0, "RIFF") && at(8, "AVI ")) return "avi";
    if (at(0, "OggS")) return "ogg";
    if (at(0, "FLV")) return "flv";
    if (header.size() > 188 && header[0] == 0x47 && header[188] == 0x47) {
        return "mpegts";
    }
    return "unknown";
}

std::string probe_file(const std::string& path) {
    std::ifstream file(path, std::ios::binary);
    if (!file) {
        throw std::runtime_error("cannot open file: " + path);
    }
    std::vector<std::uint8_t> header(kHeaderSize);
    file.read(reinterpret_cast<char*>(header.data()), static_cast<std::streamsize>(header.size()));
    header.resize(static_cast<std::size_t>(file.gcount()));
    return detect_format(header);
}

bool Mp4File::open(const std::string& path) {
    close();
    error_.clear();
    try {
        if (probe_file(path) != "mp4") {
            error_ = path + ": not an mp4 file";
            return false;
        }
        path_ = path;
        return true;
    } catch (const std::exception& error) {
        error_ = error.what();
        return false;
    }
}

void Mp4File::close() noexcept {
    path_.clear();
}

bool Mp4File::is_open() const noexcept {
    return !path_.empty();
}

const std::string& Mp4File::error_info() const noexcept {
    return error_;
}

BasicInfo Mp4File::get_basic_info() {
    error_.clear();
    if (!is_open()) {
        error_ = "file not open";
        return {};
    }
    try {
        std::ifstream file(path_, std::ios::binary);
        if (!file) {
            throw std::runtime_error("cannot open file: " + path_);
        }
        file.seekg(0, std::ios::end);
        const auto size = file.tellg();
        if (size < 0) {
            throw std::runtime_error("cannot determine MP4 file size");
        }
        const auto boxes = read_boxes(file, 0, static_cast<std::uint64_t>(size));
        BasicInfo info;
        for (const auto* track : find_boxes(boxes, {"moov", "trak"})) {
            std::string handler;
            for (const auto* box : find_in_track(*track, {"mdia", "hdlr"})) {
                if (box->end - box->payload < 12) {
                    throw std::runtime_error("hdlr box too short");
                }
                std::array<char, 4> value{};
                file.seekg(static_cast<std::streamoff>(box->payload + 8));
                file.read(value.data(), 4);
                if (!file) {
                    throw std::runtime_error("hdlr box too short");
                }
                handler.assign(value.data(), value.size());
            }
            if (handler == "vide") {
                info.hasvideo = true;
                info.videostreams.push_back(video_stream_info(file, *track));
            } else if (handler == "soun") {
                info.hasaudio = true;
                info.audiostreams.push_back(audio_stream_info(file, *track));
            }
        }
        return info;
    } catch (const std::exception& error) {
        error_ = error.what();
        return {};
    }
}

bool VideoSampleReader::open(const std::string& path) {
    close();
    error_.clear();
    try {
        file_.open(path, std::ios::binary);
        if (!file_) {
            throw std::runtime_error("cannot open file: " + path);
        }
        file_.seekg(0, std::ios::end);
        const auto file_size = file_.tellg();
        if (file_size < 0) {
            throw std::runtime_error("cannot determine MP4 file size");
        }
        const auto boxes =
            read_boxes(file_, 0, static_cast<std::uint64_t>(file_size));
        const auto tracks = find_boxes(boxes, {"moov", "trak"});
        const Box* video_track = nullptr;
        for (const auto* track : tracks) {
            const auto handlers = find_in_track(*track, {"mdia", "hdlr"});
            for (const auto* handler : handlers) {
                if (handler->end - handler->payload < 12) {
                    throw std::runtime_error("hdlr box too short");
                }
                std::array<char, 4> handler_type{};
                file_.seekg(static_cast<std::streamoff>(handler->payload + 8));
                file_.read(handler_type.data(), 4);
                if (!file_) {
                    throw std::runtime_error("cannot read hdlr box");
                }
                if (std::string(handler_type.data(), handler_type.size()) == "vide") {
                    video_track = track;
                    break;
                }
            }
            if (video_track != nullptr) {
                break;
            }
        }
        if (video_track == nullptr) {
            throw std::runtime_error("MP4 contains no video track");
        }

        const auto one_track_box = [this, video_track](
                                      const std::vector<std::string>& box_path,
                                      const char* name) -> const Box& {
            const auto matches = find_in_track(*video_track, box_path);
            if (matches.size() != 1) {
                throw std::runtime_error(std::string("video track must contain one ") +
                                         name + " box");
            }
            return *matches.front();
        };

        const auto& media_header =
            one_track_box({"mdia", "mdhd"}, "mdhd");
        const auto media_header_version =
            box_u8(file_, media_header, media_header.payload,
                   "mdhd box too short");
        if (media_header_version > 1) {
            throw std::runtime_error("unsupported mdhd version");
        }
        const auto timescale_offset =
            media_header_version == 1 ? 20U : 12U;
        if (media_header.end - media_header.payload < timescale_offset + 4) {
            throw std::runtime_error("mdhd box too short");
        }
        timescale_ =
            box_u32(file_, media_header,
                    media_header.payload + timescale_offset,
                    "mdhd box too short");
        if (timescale_ == 0) {
            throw std::runtime_error("video track has a zero media timescale");
        }

        const auto& sample_description =
            one_track_box({"mdia", "minf", "stbl", "stsd"}, "stsd");
        require_full_box_version_zero(file_, sample_description, "stsd");
        if (sample_description.end - sample_description.payload < 8) {
            throw std::runtime_error("stsd box too short");
        }
        const auto description_count =
            box_u32(file_, sample_description, sample_description.payload + 4,
                    "stsd box too short");
        const auto sample_entries =
            read_boxes(file_, sample_description.payload + 8,
                       sample_description.end);
        if (description_count != 1 || sample_entries.size() != 1) {
            throw std::runtime_error(
                "video track must contain one sample description");
        }
        if (sample_entries.front().type != "avc1" &&
            sample_entries.front().type != "av01") {
            throw std::runtime_error("unsupported video sample entry '" +
                                     sample_entries.front().type +
                                     "'; H.264 avc1 or AV1 av01 is required");
        }
        const auto& sample_entry = sample_entries.front();
        if (sample_entry.end - sample_entry.payload < 78) {
            throw std::runtime_error("avc1 sample entry is too short");
        }
        const auto sample_entry_children =
            read_boxes(file_, sample_entry.payload + 78, sample_entry.end, 1);
        std::vector<const Box*> configuration_boxes;
        for (const auto& child : sample_entry_children) {
            if ((sample_entry.type == "avc1" && child.type == "avcC") ||
                (sample_entry.type == "av01" && child.type == "av1C")) {
                configuration_boxes.push_back(&child);
            }
        }
        if (configuration_boxes.size() != 1) {
            throw std::runtime_error(sample_entry.type +
                                     " sample entry must contain one " +
                                     (sample_entry.type == "avc1" ? "avcC"
                                                                  : "av1C") +
                                     " box");
        }
        if (sample_entry.type == "avc1") {
            configuration_ =
                parse_avc_configuration(file_, *configuration_boxes.front());
            codec_ = VideoCodec::H264;
        } else {
            av1_configuration_ =
                parse_av1_configuration(file_, *configuration_boxes.front());
            codec_ = VideoCodec::AV1;
        }

        const auto& sample_size_box =
            one_track_box({"mdia", "minf", "stbl", "stsz"}, "stsz");
        require_full_box_version_zero(file_, sample_size_box, "stsz");
        if (sample_size_box.end - sample_size_box.payload < 12) {
            throw std::runtime_error("stsz box too short");
        }
        const auto constant_sample_size =
            box_u32(file_, sample_size_box, sample_size_box.payload + 4,
                    "stsz box too short");
        const auto sample_count_u32 =
            box_u32(file_, sample_size_box, sample_size_box.payload + 8,
                    "stsz box too short");
        const auto sample_count = static_cast<std::size_t>(sample_count_u32);
        std::vector<std::uint32_t> sample_sizes;
        if (constant_sample_size == 0) {
            const auto available = sample_size_box.end -
                                   (sample_size_box.payload + 12);
            if (sample_count_u32 > available / 4 ||
                available != static_cast<std::uint64_t>(sample_count_u32) * 4) {
                throw std::runtime_error("stsz entries do not match sample count");
            }
            sample_sizes.reserve(sample_count);
            for (std::uint32_t index = 0; index < sample_count_u32; ++index) {
                sample_sizes.push_back(box_u32(
                    file_, sample_size_box,
                    sample_size_box.payload + 12 +
                        static_cast<std::uint64_t>(index) * 4,
                    "stsz entry is truncated"));
            }
        } else if (sample_size_box.end != sample_size_box.payload + 12) {
            throw std::runtime_error("constant-size stsz has trailing entries");
        }

        const auto& sample_to_chunk_box =
            one_track_box({"mdia", "minf", "stbl", "stsc"}, "stsc");
        require_full_box_version_zero(file_, sample_to_chunk_box, "stsc");
        if (sample_to_chunk_box.end - sample_to_chunk_box.payload < 8) {
            throw std::runtime_error("stsc box too short");
        }
        const auto sample_to_chunk_count =
            box_u32(file_, sample_to_chunk_box, sample_to_chunk_box.payload + 4,
                    "stsc box too short");
        const auto sample_to_chunk_bytes =
            sample_to_chunk_box.end - (sample_to_chunk_box.payload + 8);
        if (sample_to_chunk_count > sample_to_chunk_bytes / 12 ||
            sample_to_chunk_bytes !=
                static_cast<std::uint64_t>(sample_to_chunk_count) * 12) {
            throw std::runtime_error("stsc entries exceed box size");
        }
        std::vector<std::array<std::uint32_t, 3>> sample_to_chunk;
        sample_to_chunk.reserve(sample_to_chunk_count);
        for (std::uint32_t index = 0; index < sample_to_chunk_count; ++index) {
            const auto entry = sample_to_chunk_box.payload + 8 +
                               static_cast<std::uint64_t>(index) * 12;
            sample_to_chunk.push_back({
                box_u32(file_, sample_to_chunk_box, entry, "stsc entry is truncated"),
                box_u32(file_, sample_to_chunk_box, entry + 4,
                        "stsc entry is truncated"),
                box_u32(file_, sample_to_chunk_box, entry + 8,
                        "stsc entry is truncated")});
        }

        const auto& timing_box =
            one_track_box({"mdia", "minf", "stbl", "stts"}, "stts");
        require_full_box_version_zero(file_, timing_box, "stts");
        if (timing_box.end - timing_box.payload < 8) {
            throw std::runtime_error("stts box too short");
        }
        const auto timing_count =
            box_u32(file_, timing_box, timing_box.payload + 4, "stts box too short");
        const auto timing_bytes = timing_box.end - (timing_box.payload + 8);
        if (timing_count > timing_bytes / 8 ||
            timing_bytes != static_cast<std::uint64_t>(timing_count) * 8) {
            throw std::runtime_error("stts entries exceed box size");
        }
        std::vector<std::pair<std::uint32_t, std::uint32_t>> timing_entries;
        timing_entries.reserve(timing_count);
        for (std::uint32_t index = 0; index < timing_count; ++index) {
            const auto entry = timing_box.payload + 8 +
                               static_cast<std::uint64_t>(index) * 8;
            timing_entries.emplace_back(
                box_u32(file_, timing_box, entry, "stts entry is truncated"),
                box_u32(file_, timing_box, entry + 4,
                        "stts entry is truncated"));
        }
        const auto durations =
            expand_sample_timing(timing_entries, sample_count, "stts");

        auto composition_boxes =
            find_in_track(*video_track, {"mdia", "minf", "stbl", "ctts"});
        if (composition_boxes.size() > 1) {
            throw std::runtime_error("multiple ctts boxes are unsupported");
        }
        std::vector<std::int64_t> composition_offsets(sample_count, 0);
        if (!composition_boxes.empty()) {
            const auto& composition_box = *composition_boxes.front();
            if (composition_box.end - composition_box.payload < 8) {
                throw std::runtime_error("ctts box too short");
            }
            const auto version =
                box_u8(file_, composition_box, composition_box.payload,
                       "ctts box too short");
            if (version > 1) {
                throw std::runtime_error("unsupported ctts version");
            }
            const auto entry_count =
                box_u32(file_, composition_box, composition_box.payload + 4,
                        "ctts box too short");
            const auto entry_bytes =
                composition_box.end - (composition_box.payload + 8);
            if (entry_count > entry_bytes / 8 ||
                entry_bytes != static_cast<std::uint64_t>(entry_count) * 8) {
                throw std::runtime_error("ctts entries exceed box size");
            }
            std::vector<std::pair<std::uint32_t, std::int64_t>> entries;
            entries.reserve(entry_count);
            for (std::uint32_t index = 0; index < entry_count; ++index) {
                const auto entry = composition_box.payload + 8 +
                                   static_cast<std::uint64_t>(index) * 8;
                const auto raw_offset =
                    box_u32(file_, composition_box, entry + 4,
                            "ctts entry is truncated");
                const auto signed_offset =
                    version == 0 || raw_offset < 0x80000000U
                        ? static_cast<std::int64_t>(raw_offset)
                        : static_cast<std::int64_t>(raw_offset) -
                              0x100000000LL;
                entries.emplace_back(
                    box_u32(file_, composition_box, entry,
                            "ctts entry is truncated"),
                    signed_offset);
            }
            composition_offsets =
                expand_composition_timing(entries, sample_count);
        }

        auto sync_boxes =
            find_in_track(*video_track, {"mdia", "minf", "stbl", "stss"});
        if (sync_boxes.size() > 1) {
            throw std::runtime_error("multiple stss boxes are unsupported");
        }
        std::optional<std::unordered_set<std::uint32_t>> sync_numbers;
        if (!sync_boxes.empty()) {
            const auto& sync_box = *sync_boxes.front();
            require_full_box_version_zero(file_, sync_box, "stss");
            if (sync_box.end - sync_box.payload < 8) {
                throw std::runtime_error("stss box too short");
            }
            const auto sync_count =
                box_u32(file_, sync_box, sync_box.payload + 4,
                        "stss box too short");
            const auto sync_bytes = sync_box.end - (sync_box.payload + 8);
            if (sync_count > sync_bytes / 4 ||
                sync_bytes != static_cast<std::uint64_t>(sync_count) * 4) {
                throw std::runtime_error("stss entries exceed box size");
            }
            sync_numbers.emplace();
            for (std::uint32_t index = 0; index < sync_count; ++index) {
                const auto sample_number = box_u32(
                    file_, sync_box,
                    sync_box.payload + 8 +
                        static_cast<std::uint64_t>(index) * 4,
                    "stss entry is truncated");
                if (sample_number == 0 || sample_number > sample_count_u32) {
                    throw std::runtime_error(
                        "stss sample number exceeds sample count");
                }
                sync_numbers->insert(sample_number);
            }
        }

        auto chunk_offset_boxes =
            find_in_track(*video_track, {"mdia", "minf", "stbl", "stco"});
        const auto co64_boxes =
            find_in_track(*video_track, {"mdia", "minf", "stbl", "co64"});
        chunk_offset_boxes.insert(chunk_offset_boxes.end(), co64_boxes.begin(),
                                  co64_boxes.end());
        if (chunk_offset_boxes.size() != 1) {
            throw std::runtime_error(
                "video track must contain one stco or co64 box");
        }
        const auto& chunk_offset_box = *chunk_offset_boxes.front();
        require_full_box_version_zero(file_, chunk_offset_box,
                                      chunk_offset_box.type.c_str());
        if (chunk_offset_box.end - chunk_offset_box.payload < 8) {
            throw std::runtime_error("chunk offset box too short");
        }
        const auto chunk_count =
            box_u32(file_, chunk_offset_box, chunk_offset_box.payload + 4,
                    "chunk offset box too short");
        const auto offset_width = chunk_offset_box.type == "stco" ? 4U : 8U;
        const auto chunk_offset_bytes =
            chunk_offset_box.end - (chunk_offset_box.payload + 8);
        if (chunk_count > chunk_offset_bytes / offset_width ||
            chunk_offset_bytes !=
                static_cast<std::uint64_t>(chunk_count) * offset_width) {
            throw std::runtime_error("chunk offsets exceed box size");
        }
        std::vector<std::uint64_t> chunk_offsets;
        chunk_offsets.reserve(chunk_count);
        for (std::uint32_t index = 0; index < chunk_count; ++index) {
            const auto entry = chunk_offset_box.payload + 8 +
                               static_cast<std::uint64_t>(index) * offset_width;
            chunk_offsets.push_back(offset_width == 4
                                        ? box_u32(file_, chunk_offset_box, entry,
                                                  "chunk offset is truncated")
                                        : read_u64(file_, entry, chunk_offset_box.end,
                                                   "chunk offset is truncated"));
        }

        const auto media_boxes = find_boxes(boxes, {"mdat"});
        std::vector<std::pair<std::uint64_t, std::uint64_t>> media_ranges;
        media_ranges.reserve(media_boxes.size());
        for (const auto* media_box : media_boxes) {
            media_ranges.emplace_back(media_box->payload, media_box->end);
        }
        if (sample_count != 0 && media_ranges.empty()) {
            throw std::runtime_error("video samples exist but no mdat box was found");
        }
        if (sample_count != 0 &&
            (sample_to_chunk.empty() || chunk_offsets.empty())) {
            throw std::runtime_error("sample table lacks chunk mapping entries");
        }
        for (std::size_t index = 0; index < sample_to_chunk.size(); ++index) {
            const auto& entry = sample_to_chunk[index];
            if (entry[0] == 0 || entry[1] == 0 ||
                (index == 0 && entry[0] != 1) ||
                (index != 0 && entry[0] <= sample_to_chunk[index - 1][0]) ||
                entry[0] > chunk_offsets.size()) {
                throw std::runtime_error("invalid stsc chunk mapping");
            }
        }

        samples_.clear();
        samples_.reserve(sample_count);
        std::size_t sample_index = 0;
        std::size_t mapping_index = 0;
        std::int64_t dts_ticks = 0;
        for (std::size_t chunk_index = 0; chunk_index < chunk_offsets.size();
             ++chunk_index) {
            const auto chunk_number = chunk_index + 1;
            while (mapping_index + 1 < sample_to_chunk.size() &&
                   sample_to_chunk[mapping_index + 1][0] <= chunk_number) {
                ++mapping_index;
            }
            if (sample_to_chunk.empty()) {
                if (sample_count != 0) {
                    throw std::runtime_error("sample table lacks stsc entries");
                }
                break;
            }
            const auto& mapping = sample_to_chunk[mapping_index];
            if (mapping[0] > chunk_number || mapping[2] != 1) {
                throw std::runtime_error(
                    "chunk uses an unsupported sample description");
            }
            auto sample_offset = chunk_offsets[chunk_index];
            for (std::uint32_t in_chunk = 0; in_chunk < mapping[1]; ++in_chunk) {
                if (sample_index >= sample_count) {
                    throw std::runtime_error(
                        "stsc map contains more samples than stsz");
                }
                const auto sample_size = constant_sample_size != 0
                                             ? constant_sample_size
                                             : sample_sizes[sample_index];
                if (sample_size == 0 ||
                    sample_offset >
                        std::numeric_limits<std::uint64_t>::max() - sample_size) {
                    throw std::runtime_error("invalid or overflowing sample range");
                }
                const auto sample_end =
                    sample_offset + static_cast<std::uint64_t>(sample_size);
                const auto in_media = std::any_of(
                    media_ranges.begin(), media_ranges.end(),
                    [sample_offset, sample_end](const auto& range) {
                        return range.first <= sample_offset &&
                               sample_end <= range.second;
                    });
                if (!in_media) {
                    throw std::runtime_error(
                        "sample byte range is outside every mdat payload");
                }
                if (dts_ticks >
                    std::numeric_limits<std::int64_t>::max() -
                        durations[sample_index]) {
                    throw std::runtime_error("decode timestamp overflows");
                }
                const auto composition_offset =
                    composition_offsets[sample_index];
                if ((composition_offset > 0 &&
                     dts_ticks > std::numeric_limits<std::int64_t>::max() -
                                     composition_offset) ||
                    (composition_offset < 0 &&
                     dts_ticks < std::numeric_limits<std::int64_t>::min() -
                                     composition_offset)) {
                    throw std::runtime_error(
                        "presentation timestamp overflows");
                }
                const auto pts_ticks = dts_ticks + composition_offset;
                samples_.push_back(SampleLocation{
                    sample_offset, sample_size, dts_ticks, pts_ticks,
                    static_cast<std::uint32_t>(durations[sample_index]),
                    !sync_numbers.has_value() ||
                        sync_numbers->count(
                            static_cast<std::uint32_t>(sample_index + 1)) != 0});
                dts_ticks += durations[sample_index];
                sample_offset = sample_end;
                ++sample_index;
            }
        }
        if (sample_index != sample_count) {
            throw std::runtime_error("stsc map does not cover every stsz sample");
        }
        path_ = path;
        next_index_ = 0;
        return true;
    } catch (const std::exception& error) {
        const auto message = error.what();
        close();
        error_ = message;
        return false;
    }
}

void VideoSampleReader::close() noexcept {
    if (file_.is_open()) {
        file_.close();
    }
    file_.clear();
    path_.clear();
    timescale_ = 0;
    configuration_ = {};
    av1_configuration_ = {};
    codec_ = VideoCodec::H264;
    samples_.clear();
    next_index_ = 0;
}

bool VideoSampleReader::is_open() const noexcept {
    return file_.is_open();
}

const std::string& VideoSampleReader::error_info() const noexcept {
    return error_;
}

std::size_t VideoSampleReader::sample_count() const noexcept {
    return samples_.size();
}

std::vector<std::size_t> VideoSampleReader::presentation_sample_indices() const {
    std::vector<std::size_t> display_order(samples_.size());
    for (std::size_t sample_index = 0; sample_index < samples_.size();
         ++sample_index) {
        display_order[sample_index] = sample_index;
    }
    std::sort(display_order.begin(), display_order.end(),
              [this](std::size_t left, std::size_t right) {
                  if (samples_[left].pts_ticks != samples_[right].pts_ticks) {
                      return samples_[left].pts_ticks < samples_[right].pts_ticks;
                  }
                  return left < right;
              });
    return display_order;
}

std::size_t VideoSampleReader::presentation_sample_index(
    std::size_t index) const {
    if (index >= samples_.size()) {
        throw std::out_of_range("presentation frame index is out of range");
    }
    return presentation_sample_indices()[index];
}

std::uint32_t VideoSampleReader::timescale() const noexcept {
    return timescale_;
}

const H264SampleConfiguration& VideoSampleReader::configuration() const noexcept {
    return configuration_;
}

std::vector<std::size_t> VideoSampleReader::sync_sample_indices() const {
    std::vector<std::size_t> result;
    for (std::size_t index = 0; index < samples_.size(); ++index) {
        if (samples_[index].is_sync) {
            result.push_back(index);
        }
    }
    return result;
}

const AV1SampleConfiguration&
VideoSampleReader::av1_configuration() const noexcept {
    return av1_configuration_;
}

VideoCodec VideoSampleReader::codec() const noexcept {
    return codec_;
}

CompressedVideoSample VideoSampleReader::sample_at(std::size_t index) {
    if (index >= samples_.size()) {
        throw std::out_of_range("video sample index is out of range");
    }
    const auto display_order = presentation_sample_indices();
    const auto presentation_index = static_cast<std::size_t>(
        std::find(display_order.begin(), display_order.end(), index) -
        display_order.begin());
    if (!is_open()) {
        throw std::runtime_error(
            "presentation frame " + std::to_string(presentation_index) +
            " (sample " + std::to_string(index) + "): sample reader is closed");
    }
    const auto& location = samples_[index];
    if (location.size >
        static_cast<std::uint64_t>(std::numeric_limits<std::streamsize>::max())) {
        throw std::runtime_error(
            "presentation frame " + std::to_string(presentation_index) +
            " (sample " + std::to_string(index) +
            "): video sample exceeds supported read size");
    }
    std::vector<std::uint8_t> data(location.size);
    file_.clear();
    file_.seekg(static_cast<std::streamoff>(location.offset));
    file_.read(reinterpret_cast<char*>(data.data()),
               static_cast<std::streamsize>(data.size()));
    if (!file_) {
        throw std::runtime_error(
            "presentation frame " + std::to_string(presentation_index) +
            " (sample " + std::to_string(index) +
            "): cannot read compressed video sample");
    }
    if (codec_ == VideoCodec::H264) {
        try {
            const auto nals =
                split_length_prefixed_nals(data, configuration_.nal_length_size);
            if (nals.empty()) {
                throw std::runtime_error(
                    "compressed video sample contains no NAL units");
            }
            for (const auto& nal : nals) {
                (void)parse_nal_header(nal);
            }
        } catch (const std::exception& error) {
            throw std::runtime_error(
                "presentation frame " + std::to_string(presentation_index) +
                " (sample " + std::to_string(index) + "): " + error.what());
        }
    }
    return CompressedVideoSample{index, std::move(data), location.dts_ticks,
                                 location.pts_ticks, location.duration_ticks,
                                 location.is_sync};
}

void AV1Decoder::check_available() const {
    throw std::runtime_error("AV1 decoding is not implemented");
}

Yuv420Frame AV1Decoder::decode_frame(std::uint64_t) const {
    check_available();
    return {};
}

bool VideoSampleReader::read_next(CompressedVideoSample& sample) {
    if (!is_open()) {
        throw std::runtime_error("sample reader is closed");
    }
    if (next_index_ >= samples_.size()) {
        return false;
    }
    sample = sample_at(next_index_);
    ++next_index_;
    return true;
}

} // namespace pygorvid

namespace pygorvid {
namespace {

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

std::size_t decode_video(
    const std::string& input_path,
    const std::optional<std::size_t>& frame_index,
    std::size_t threads,
    std::size_t sample_start,
    std::optional<std::size_t> sample_end,
    const std::function<void(std::size_t, pygorvid::Yuv420Frame&&)>&
        frame_consumer) {
    std::vector<std::size_t> display_indices;

    pygorvid::VideoSampleReader sample_reader;
    if (!sample_reader.open(input_path)) {
        throw std::runtime_error(sample_reader.error_info());
    }
    if (sample_reader.codec() == pygorvid::VideoCodec::AV1) {
        pygorvid::AV1Decoder decoder;
        decoder.check_available();
    }
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
        : std::min(sample_end.value_or(sample_reader.sample_count()),
                   sample_reader.sample_count());
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
    std::size_t decoded_count = 0;
    for (std::size_t sample_index = sample_start;
         sample_index < sample_count;
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
        ++decoded_count;
        if (current_picture_is_reference) {
            frame_consumer(display_indices[sample_index],
                           pygorvid::Yuv420Frame(decoded));
        } else {
            frame_consumer(display_indices[sample_index],
                           std::move(decoded));
        }
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
    if (decoded_count == 0) {
        throw std::runtime_error("no supported H.264 picture was decoded");
    }
    return decoded_count;
}
using DecodedFrame = std::pair<std::size_t, pygorvid::Yuv420Frame>;

std::vector<std::pair<std::size_t, std::size_t>> closed_gop_ranges(
    const std::string& input_path) {
    pygorvid::VideoSampleReader reader;
    if (!reader.open(input_path)) {
        throw std::runtime_error(reader.error_info());
    }
    auto starts = reader.sync_sample_indices();
    if (starts.size() < 2) {
        return {};
    }
    if (starts.front() != 0) {
        starts.insert(starts.begin(), 0);
    }
    std::vector<std::pair<std::size_t, std::size_t>> ranges;
    ranges.reserve(starts.size());
    for (std::size_t index = 0; index < starts.size(); ++index) {
        ranges.emplace_back(
            starts[index],
            index + 1 < starts.size()
                ? starts[index + 1]
                : reader.sample_count());
    }
    return ranges;
}

std::vector<DecodedFrame> decode_gop_range(
    const std::string& input_path, std::size_t threads,
    std::size_t start, std::size_t end,
    std::promise<void>* started = nullptr) {
    if (started) {
        VideoSampleReader reader;
        if (!reader.open(input_path)) {
            started->set_exception(std::make_exception_ptr(
                std::runtime_error(reader.error_info())));
            throw std::runtime_error(reader.error_info());
        }
        started->set_value();
    }
    std::vector<DecodedFrame> frames;
    const std::function<void(std::size_t,
                             pygorvid::Yuv420Frame&&)> consumer =
        [&frames](std::size_t index, pygorvid::Yuv420Frame&& frame) {
            frames.emplace_back(index, std::move(frame));
        };
    (void)decode_video(
        input_path, std::nullopt, threads, start, end, consumer);
    std::sort(frames.begin(), frames.end(),
              [](const auto& left, const auto& right) {
                  return left.first < right.first;
              });
    return frames;
}

using DecoderConsumer = std::function<void(std::size_t, Yuv420Frame&&)>;

struct DecodeStopped {};

std::size_t decode_sequential(
    const std::string& input_path, std::size_t threads,
    const std::optional<std::size_t>& presentation_frame,
    const DecoderConsumer& consumer) {
    VideoSampleReader reader;
    if (!reader.open(input_path)) throw std::runtime_error(reader.error_info());
    std::size_t decoded = 0;
    std::size_t next = 0;
    std::map<std::size_t, Yuv420Frame> pending;
    const std::function<void(std::size_t, Yuv420Frame&&)> receive =
        [&](std::size_t index, Yuv420Frame&& frame) {
            if (presentation_frame) {
                if (index != *presentation_frame) return;
                consumer(index, std::move(frame));
                ++decoded;
                return;
            }
            pending.emplace(index, std::move(frame));
            while (pending.find(next) != pending.end()) {
                auto selected = std::move(pending.at(next));
                pending.erase(next);
                consumer(next, std::move(selected));
                ++next;
                ++decoded;
            }
        };
    (void)decode_video(input_path, presentation_frame, threads, 0,
                       std::nullopt, receive);
    return decoded;
}

std::size_t decode_readahead(
    const std::string& input_path, std::size_t threads,
    const DecoderConsumer& consumer) {
    auto ranges = closed_gop_ranges(input_path);
    if (ranges.size() < 2 || threads < 2) {
        return decode_sequential(input_path, threads, std::nullopt, consumer);
    }
    VideoSampleReader reader;
    if (!reader.open(input_path)) throw std::runtime_error(reader.error_info());
    auto worker_limit = std::min<std::size_t>(threads - 1, ranges.size() - 1);
    std::size_t maximum_gop_frames = 0;
    for (const auto& range : ranges) {
        maximum_gop_frames = std::max(maximum_gop_frames,
                                      range.second - range.first);
    }
    if (!reader.configuration().sequence_parameter_sets.empty()) {
        const auto sps = parse_sps(
            reader.configuration().sequence_parameter_sets.front());
        const auto page_size = sysconf(_SC_PAGESIZE);
        const auto available_pages = sysconf(_SC_AVPHYS_PAGES);
        if (page_size > 0 && available_pages > 0 && sps.width != 0 &&
            sps.height != 0 && maximum_gop_frames != 0) {
            const auto available_bytes =
                static_cast<std::uint64_t>(page_size) *
                static_cast<std::uint64_t>(available_pages);
            const auto frame_bytes = static_cast<std::uint64_t>(sps.width) *
                static_cast<std::uint64_t>(sps.height) * 3 / 2;
            const auto batch_bytes = frame_bytes * maximum_gop_frames;
            if (batch_bytes != 0) {
                worker_limit = std::min(worker_limit,
                    static_cast<std::size_t>((available_bytes / 2) /
                                             batch_bytes));
            }
        }
    }
    if (worker_limit == 0) {
        return decode_sequential(input_path, threads, std::nullopt, consumer);
    }

    std::map<std::size_t, std::future<std::vector<DecodedFrame>>> jobs;
    std::size_t next_group = 1;
    const auto start_job = [&](std::size_t group) {
        auto started = std::make_shared<std::promise<void>>();
        auto ready = started->get_future();
        jobs.emplace(group, std::async(std::launch::async, [&, group, started] {
            return decode_gop_range(input_path, threads,
                                    ranges[group].first,
                                    ranges[group].second, started.get());
        }));
        ready.get();
    };
    for (; next_group <= worker_limit; ++next_group) start_job(next_group);

    std::map<std::size_t, Yuv420Frame> foreground;
    std::size_t next_presentation = 0;
    std::size_t consumed = 0;
    const auto receive = [&](std::size_t index, Yuv420Frame frame) {
        foreground.emplace(index, std::move(frame));
        while (foreground.find(next_presentation) != foreground.end()) {
            auto selected = std::move(foreground.at(next_presentation));
            foreground.erase(next_presentation);
            consumer(next_presentation, std::move(selected));
            ++next_presentation;
            ++consumed;
        }
    };
    for (std::size_t group = 0; group < ranges.size(); ++group) {
        if (group == 0) {
            const std::function<void(std::size_t, Yuv420Frame&&)> forward =
                [&](std::size_t index, Yuv420Frame&& frame) {
                    receive(index, std::move(frame));
                };
            (void)decode_video(input_path, std::nullopt, threads,
                               ranges[group].first, ranges[group].second,
                               forward);
        } else {
            auto frames = jobs.at(group).get();
            jobs.erase(group);
            for (auto& [index, frame] : frames) {
                receive(index, std::move(frame));
            }
        }
        if (next_group < ranges.size()) start_job(next_group++);
    }
    return consumed;
}

} // namespace

VideoDecoder::VideoDecoder(std::string path) : path_(std::move(path)) {}

std::size_t VideoDecoder::decode(
    const VideoDecodeOptions& options, const FrameConsumer& consumer) const {
    if (!consumer) throw std::invalid_argument("frame consumer is required");
    VideoSampleReader reader;
    if (!reader.open(path_)) throw std::runtime_error(reader.error_info());
    if (options.presentation_frame &&
        *options.presentation_frame >= reader.sample_count()) {
        throw std::out_of_range("requested frame index exceeds the sample count");
    }
    const auto presentation = reader.presentation_sample_indices();
    const auto timescale = reader.timescale();
    bool stopped = false;
    std::size_t emitted = 0;
    const DecoderConsumer emit = [&](std::size_t index, Yuv420Frame&& frame) {
        if (stopped || index >= presentation.size()) return;
        const auto sample = reader.sample_at(presentation[index]);
        DecodedVideoFrame decoded{index,
            timescale == 0 ? 0.0 : static_cast<double>(sample.duration_ticks) /
                                      static_cast<double>(timescale),
            std::move(frame)};
        stopped = !consumer(std::move(decoded));
        ++emitted;
        if (stopped) throw DecodeStopped{};
    };
    try {
        if (options.presentation_frame) {
            (void)decode_sequential(path_, options.threads,
                                    options.presentation_frame, emit);
        } else if (options.readahead) {
            (void)decode_readahead(path_, options.threads, emit);
        } else {
            (void)decode_sequential(path_, options.threads, std::nullopt, emit);
        }
    } catch (const DecodeStopped&) {
    }
    return emitted;
}

DecodedVideoFrame VideoDecoder::decode_frame(
    std::size_t presentation_index, std::size_t threads) const {
    std::optional<DecodedVideoFrame> result;
    decode(VideoDecodeOptions{threads, false, presentation_index},
           [&](DecodedVideoFrame&& frame) {
               result.emplace(std::move(frame));
               return false;
           });
    if (!result) throw std::runtime_error("no supported H.264 picture was decoded");
    return std::move(*result);
}

} // namespace pygorvid
