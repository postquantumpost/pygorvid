#ifndef PYGORVID_VIDEO_SAMPLES_HPP
#define PYGORVID_VIDEO_SAMPLES_HPP

#include <cstddef>
#include <cstdint>
#include <fstream>
#include <string>
#include <vector>

namespace pygorvid {

struct H264SampleConfiguration {
    std::uint8_t profile_idc = 0;
    std::uint8_t profile_compatibility = 0;
    std::uint8_t level_idc = 0;
    std::uint8_t nal_length_size = 0;
    std::vector<std::vector<std::uint8_t>> sequence_parameter_sets;
    std::vector<std::vector<std::uint8_t>> picture_parameter_sets;
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
    std::uint32_t timescale() const noexcept;
    const H264SampleConfiguration& configuration() const noexcept;
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
    std::vector<SampleLocation> samples_;
    std::size_t next_index_ = 0;
};

} // namespace pygorvid

#endif
