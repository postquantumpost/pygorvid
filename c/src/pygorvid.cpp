#include "pygorvid/pygorvid.hpp"
#include "pygorvid/h264.hpp"
#include "pygorvid/video_samples.hpp"

#include <algorithm>
#include <array>
#include <fstream>
#include <limits>
#include <optional>
#include <stdexcept>
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
        if (description_count != 1 || sample_entries.size() != 1 ||
            sample_entries.front().type != "avc1") {
            throw std::runtime_error(
                "only one avc1 video sample description is supported");
        }
        const auto& sample_entry = sample_entries.front();
        if (sample_entry.end - sample_entry.payload < 78) {
            throw std::runtime_error("avc1 sample entry is too short");
        }
        const auto sample_entry_children =
            read_boxes(file_, sample_entry.payload + 78, sample_entry.end, 1);
        std::vector<const Box*> avcc_boxes;
        for (const auto& child : sample_entry_children) {
            if (child.type == "avcC") {
                avcc_boxes.push_back(&child);
            }
        }
        if (avcc_boxes.size() != 1) {
            throw std::runtime_error(
                "avc1 sample entry must contain one avcC box");
        }
        configuration_ = parse_avc_configuration(file_, *avcc_boxes.front());

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

std::uint32_t VideoSampleReader::timescale() const noexcept {
    return timescale_;
}

const H264SampleConfiguration& VideoSampleReader::configuration() const noexcept {
    return configuration_;
}

CompressedVideoSample VideoSampleReader::sample_at(std::size_t index) {
    if (!is_open()) {
        throw std::runtime_error("sample reader is closed");
    }
    if (index >= samples_.size()) {
        throw std::out_of_range("video sample index is out of range");
    }
    const auto& location = samples_[index];
    if (location.size >
        static_cast<std::uint64_t>(std::numeric_limits<std::streamsize>::max())) {
        throw std::runtime_error("video sample exceeds supported read size");
    }
    std::vector<std::uint8_t> data(location.size);
    file_.clear();
    file_.seekg(static_cast<std::streamoff>(location.offset));
    file_.read(reinterpret_cast<char*>(data.data()),
               static_cast<std::streamsize>(data.size()));
    if (!file_) {
        throw std::runtime_error("cannot read compressed video sample");
    }
    const auto nals =
        split_length_prefixed_nals(data, configuration_.nal_length_size);
    if (nals.empty()) {
        throw std::runtime_error("compressed video sample contains no NAL units");
    }
    for (const auto& nal : nals) {
        (void)parse_nal_header(nal);
    }
    return CompressedVideoSample{index, std::move(data), location.dts_ticks,
                                 location.pts_ticks, location.duration_ticks,
                                 location.is_sync};
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
