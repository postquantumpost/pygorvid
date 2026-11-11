#ifndef PYGORVID_PYGORVID_HPP
#define PYGORVID_PYGORVID_HPP

#include <cstddef>
#include <cstdint>
#include <string>
#include <vector>

namespace pygorvid {

class BitReader {
public:
    explicit BitReader(const std::vector<std::uint8_t>& data);

    std::uint32_t read_bits(std::uint8_t count);
    bool read_bit();
    std::size_t bit_offset() const noexcept;
    void restore_bit_offset(std::size_t bit_offset);
    void align_to_byte() noexcept;
    bool more_rbsp_data() const noexcept;
    std::uint32_t read_ue();
    std::int64_t read_se();

private:
    const std::vector<std::uint8_t>& data_;
    std::size_t bit_offset_ = 0;
};

struct BasicVideoStreamInfo {
    std::uint32_t width = 0;
    std::uint32_t height = 0;
    double framerate = 0;
    std::uint64_t frame_count = 0;
    double duration_seconds = 0;
};

struct BasicAudioStreamInfo {
    std::uint32_t sample_rate = 0;
    std::uint16_t channels = 0;
};

struct BasicInfo {
    bool hasvideo = false;
    bool hasaudio = false;
    std::vector<BasicVideoStreamInfo> videostreams;
    std::vector<BasicAudioStreamInfo> audiostreams;
};

std::string detect_format(const std::vector<std::uint8_t>& header);
std::string probe_file(const std::string& path);

class Mp4File {
public:
    Mp4File() = default;
    bool open(const std::string& path);
    void close() noexcept;
    bool is_open() const noexcept;
    const std::string& error_info() const noexcept;
    BasicInfo get_basic_info();

private:
    std::string path_;
    std::string error_;
};

} // namespace pygorvid

#endif
