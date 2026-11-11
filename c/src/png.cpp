#include "pygorvid/png.hpp"

#include <algorithm>
#include <cmath>
#include <limits>
#include <stdexcept>

namespace pygorvid {
namespace {

void append_u32_be(std::vector<std::uint8_t>& output, std::uint32_t value) {
    output.push_back(static_cast<std::uint8_t>(value >> 24));
    output.push_back(static_cast<std::uint8_t>(value >> 16));
    output.push_back(static_cast<std::uint8_t>(value >> 8));
    output.push_back(static_cast<std::uint8_t>(value));
}

std::uint32_t crc32(const std::vector<std::uint8_t>& data,
                    std::size_t begin, std::size_t end) {
    std::uint32_t crc = 0xffffffffU;
    for (auto index = begin; index < end; ++index) {
        crc ^= data[index];
        for (unsigned bit = 0; bit < 8; ++bit) {
            crc = (crc >> 1) ^ ((crc & 1U) != 0 ? 0xedb88320U : 0U);
        }
    }
    return crc ^ 0xffffffffU;
}

void append_chunk(std::vector<std::uint8_t>& png, const char* type,
                  const std::vector<std::uint8_t>& data) {
    if (data.size() > std::numeric_limits<std::uint32_t>::max()) {
        throw std::runtime_error("PNG chunk exceeds the supported size");
    }
    append_u32_be(png, static_cast<std::uint32_t>(data.size()));
    const auto type_begin = png.size();
    for (unsigned index = 0; index < 4; ++index) {
        png.push_back(static_cast<std::uint8_t>(type[index]));
    }
    png.insert(png.end(), data.begin(), data.end());
    append_u32_be(png, crc32(png, type_begin, png.size()));
}

std::uint8_t round_and_clamp_byte(double value) {
    return static_cast<std::uint8_t>(
        std::clamp(std::round(value), 0.0, 255.0));
}

std::vector<std::uint8_t> zlib_store(const std::vector<std::uint8_t>& raw) {
    const auto block_count =
        raw.size() / 65535 + (raw.size() % 65535 == 0 ? 0 : 1);
    constexpr auto maximum_chunk_size =
        std::numeric_limits<std::uint32_t>::max();
    if (raw.size() > maximum_chunk_size - 6 ||
        block_count > (maximum_chunk_size - raw.size() - 6) / 5) {
        throw std::runtime_error("PNG compressed image exceeds the chunk limit");
    }
    std::vector<std::uint8_t> compressed;
    compressed.reserve(raw.size() + block_count * 5 + 6);
    compressed.push_back(0x78);
    compressed.push_back(0x01);
    std::size_t offset = 0;
    while (offset < raw.size()) {
        const auto block_size =
            std::min<std::size_t>(raw.size() - offset, 65535);
        const bool final_block = offset + block_size == raw.size();
        compressed.push_back(final_block ? 1 : 0);
        const auto length = static_cast<std::uint16_t>(block_size);
        const auto inverse_length = static_cast<std::uint16_t>(~length);
        compressed.push_back(static_cast<std::uint8_t>(length));
        compressed.push_back(static_cast<std::uint8_t>(length >> 8));
        compressed.push_back(static_cast<std::uint8_t>(inverse_length));
        compressed.push_back(static_cast<std::uint8_t>(inverse_length >> 8));
        compressed.insert(
            compressed.end(),
            raw.begin() + static_cast<std::ptrdiff_t>(offset),
            raw.begin() + static_cast<std::ptrdiff_t>(offset + block_size));
        offset += block_size;
    }
    std::uint32_t adler_a = 1;
    std::uint32_t adler_b = 0;
    for (const auto value : raw) {
        adler_a = (adler_a + value) % 65521;
        adler_b = (adler_b + adler_a) % 65521;
    }
    append_u32_be(compressed, (adler_b << 16) | adler_a);
    return compressed;
}

} // namespace

std::vector<std::uint8_t> encode_png(const Yuv420Frame& frame) {
    if (frame.width == 0 || frame.height == 0 || frame.width % 2 != 0 ||
        frame.height % 2 != 0) {
        throw std::invalid_argument(
            "PNG encoding requires nonzero, even 4:2:0 frame dimensions");
    }
    const auto width = static_cast<std::size_t>(frame.width);
    const auto height = static_cast<std::size_t>(frame.height);
    if (width > std::numeric_limits<std::size_t>::max() / height ||
        width > (std::numeric_limits<std::size_t>::max() - 1) / 3) {
        throw std::runtime_error("PNG frame dimensions overflow");
    }
    const auto luma_size = width * height;
    const auto chroma_size = (width / 2) * (height / 2);
    if (frame.luma.size() != luma_size ||
        frame.chroma_u.size() != chroma_size ||
        frame.chroma_v.size() != chroma_size) {
        throw std::invalid_argument(
            "PNG frame planes do not match the declared dimensions");
    }
    const auto row_size = width * 3 + 1;
    if (height > std::numeric_limits<std::size_t>::max() / row_size) {
        throw std::runtime_error("PNG image data size overflows");
    }
    const auto raw_size = height * row_size;
    std::vector<std::uint8_t> raw;
    raw.reserve(raw_size);
    for (std::size_t row = 0; row < height; ++row) {
        raw.push_back(0);
        for (std::size_t column = 0; column < width; ++column) {
            const auto luma = frame.luma[row * width + column];
            const auto chroma_index =
                (row / 2) * (width / 2) + column / 2;
            const auto luma_difference =
                static_cast<double>(luma) - 16.0;
            const auto blue_difference =
                static_cast<double>(frame.chroma_u[chroma_index]) - 128.0;
            const auto red_difference =
                static_cast<double>(frame.chroma_v[chroma_index]) - 128.0;
            raw.push_back(round_and_clamp_byte(
                1.164 * luma_difference + 1.596 * red_difference));
            raw.push_back(round_and_clamp_byte(
                1.164 * luma_difference - 0.392 * blue_difference -
                0.813 * red_difference));
            raw.push_back(round_and_clamp_byte(
                1.164 * luma_difference + 2.017 * blue_difference));
        }
    }

    std::vector<std::uint8_t> png{
        0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a,
    };
    std::vector<std::uint8_t> header;
    append_u32_be(header, frame.width);
    append_u32_be(header, frame.height);
    header.insert(header.end(), {8, 2, 0, 0, 0});
    append_chunk(png, "IHDR", header);
    append_chunk(png, "IDAT", zlib_store(raw));
    append_chunk(png, "IEND", {});
    return png;
}

} // namespace pygorvid
