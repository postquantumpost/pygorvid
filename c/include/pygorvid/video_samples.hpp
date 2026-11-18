#ifndef PYGORVID_VIDEO_SAMPLES_HPP
#define PYGORVID_VIDEO_SAMPLES_HPP

#include <cstddef>
#include <cstdint>
#include <fstream>
#include <string>
#include <vector>

namespace pygorvid {

enum class VideoCodec {
    H264,
    AV1,
};

struct H264SampleConfiguration {
    std::uint8_t profile_idc = 0;
    std::uint8_t profile_compatibility = 0;
    std::uint8_t level_idc = 0;
    std::uint8_t nal_length_size = 0;
    std::vector<std::vector<std::uint8_t>> sequence_parameter_sets;
    std::vector<std::vector<std::uint8_t>> picture_parameter_sets;
};

struct AV1SampleConfiguration {
    std::uint8_t profile = 0;
    std::uint8_t level = 0;
    std::uint8_t tier = 0;
    std::uint8_t bit_depth = 0;
    bool monochrome = false;
    bool chroma_subsampling_x = false;
    bool chroma_subsampling_y = false;
    std::uint8_t chroma_sample_position = 0;
    std::vector<std::uint8_t> config_obus;
};

struct CompressedVideoSample {
    std::size_t index = 0;
    std::vector<std::uint8_t> data;
    std::int64_t dts_ticks = 0;
    std::int64_t pts_ticks = 0;
    std::uint32_t duration_ticks = 0;
    bool is_sync = false;
};

class VideoSampleReader {
public:
    VideoSampleReader() = default;
    VideoSampleReader(const VideoSampleReader&) = delete;
    VideoSampleReader& operator=(const VideoSampleReader&) = delete;

    bool open(const std::string& path);
    void close() noexcept;
    bool is_open() const noexcept;
    const std::string& error_info() const noexcept;
    std::size_t sample_count() const noexcept;
    std::vector<std::size_t> presentation_sample_indices() const;
    std::size_t presentation_sample_index(std::size_t index) const;
    std::uint32_t timescale() const noexcept;
    const H264SampleConfiguration& configuration() const noexcept;
    const AV1SampleConfiguration& av1_configuration() const noexcept;
    VideoCodec codec() const noexcept;
    CompressedVideoSample sample_at(std::size_t index);
    bool read_next(CompressedVideoSample& sample);

private:
    struct SampleLocation {
        std::uint64_t offset = 0;
        std::uint32_t size = 0;
        std::int64_t dts_ticks = 0;
        std::int64_t pts_ticks = 0;
        std::uint32_t duration_ticks = 0;
        bool is_sync = false;
    };

    std::ifstream file_;
    std::string path_;
    std::string error_;
    std::uint32_t timescale_ = 0;
    H264SampleConfiguration configuration_;
    AV1SampleConfiguration av1_configuration_;
    VideoCodec codec_ = VideoCodec::H264;
    std::vector<SampleLocation> samples_;
    std::size_t next_index_ = 0;
};

} // namespace pygorvid

#endif
