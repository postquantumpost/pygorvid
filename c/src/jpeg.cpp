#include "pygorvid/jpeg.hpp"

#include <algorithm>
#include <array>
#include <cmath>
#include <limits>
#include <stdexcept>

namespace pygorvid {
namespace {

constexpr std::array<unsigned, 64> zigzag = {
    0, 1, 8, 16, 9, 2, 3, 10, 17, 24, 32, 25, 18, 11, 4, 5,
    12, 19, 26, 33, 40, 48, 41, 34, 27, 20, 13, 6, 7, 14, 21, 28,
    35, 42, 49, 56, 57, 50, 43, 36, 29, 22, 15, 23, 30, 37, 44, 51,
    58, 59, 52, 45, 38, 31, 39, 46, 53, 60, 61, 54, 47, 55, 62, 63,
};

constexpr std::array<unsigned, 64> quantization = {
    8, 6, 5, 8, 12, 20, 26, 31,
    6, 6, 7, 10, 13, 29, 30, 28,
    7, 7, 8, 12, 20, 29, 35, 28,
    7, 9, 11, 15, 26, 44, 40, 31,
    9, 11, 19, 28, 34, 55, 52, 39,
    12, 18, 28, 32, 41, 52, 57, 46,
    25, 32, 39, 44, 52, 61, 60, 51,
    36, 46, 48, 49, 56, 50, 52, 50,
};

constexpr std::array<unsigned, 64> chroma_quantization = {
    9, 9, 12, 24, 50, 50, 50, 50,
    9, 11, 13, 33, 50, 50, 50, 50,
    12, 13, 28, 50, 50, 50, 50, 50,
    24, 33, 50, 50, 50, 50, 50, 50,
    50, 50, 50, 50, 50, 50, 50, 50,
    50, 50, 50, 50, 50, 50, 50, 50,
    50, 50, 50, 50, 50, 50, 50, 50,
    50, 50, 50, 50, 50, 50, 50, 50,
};

// Q20 basis: 1 for DC, sqrt(2)*cos((2*x+1)*u*pi/16) otherwise.
// Integer accumulation keeps quantization ties independent of the math library.
constexpr std::array<std::array<std::int64_t, 8>, 8> dct_basis = {{
    {1048576, 1048576, 1048576, 1048576, 1048576, 1048576, 1048576, 1048576},
    {1454417, 1232995, 823861, 289301, -289301, -823861, -1232995, -1454417},
    {1370031, 567485, -567485, -1370031, -1370031, -567485, 567485, 1370031},
    {1232995, -289301, -1454417, -823861, 823861, 1454417, 289301, -1232995},
    {1048576, -1048576, -1048576, 1048576, 1048576, -1048576, -1048576, 1048576},
    {823861, -1454417, 289301, 1232995, -1232995, -289301, 1454417, -823861},
    {567485, -1370031, 1370031, -567485, -567485, 1370031, -1370031, 567485},
    {289301, -823861, 1232995, -1454417, 1454417, -1232995, 823861, -289301},
}};

void marker(std::vector<std::uint8_t>& output, std::uint8_t code,
            const std::vector<std::uint8_t>& payload) {
    const auto length = payload.size() + 2;
    output.insert(output.end(), {0xff, code,
        static_cast<std::uint8_t>(length >> 8),
        static_cast<std::uint8_t>(length)});
    output.insert(output.end(), payload.begin(), payload.end());
}

class BitWriter {
public:
    explicit BitWriter(std::vector<std::uint8_t>& output) : output_(output) {}

    void write(unsigned value, unsigned length) {
        bits_ = (bits_ << length) | value;
        count_ += length;
        while (count_ >= 8) {
            count_ -= 8;
            const auto byte = static_cast<std::uint8_t>(bits_ >> count_);
            output_.push_back(byte);
            if (byte == 0xff) {
                output_.push_back(0);
            }
        }
        bits_ &= (1U << count_) - 1;
    }

    void finish() {
        if (count_ != 0) {
            const auto padding = 8 - count_;
            write((1U << padding) - 1, padding);
        }
    }

private:
    std::vector<std::uint8_t>& output_;
    unsigned bits_ = 0;
    unsigned count_ = 0;
};

unsigned magnitude(int value) {
    unsigned magnitude_value = static_cast<unsigned>(value < 0 ? -value : value);
    unsigned length = 0;
    while (magnitude_value != 0) {
        ++length;
        magnitude_value >>= 1;
    }
    return length;
}

void amplitude(BitWriter& writer, int value, unsigned length) {
    if (length != 0) {
        const auto encoded = value < 0 ? value + (1 << length) - 1 : value;
        writer.write(static_cast<unsigned>(encoded), length);
    }
}

void encode_block(BitWriter& writer, const std::array<int, 64>& samples,
                  const std::array<unsigned, 64>& table, int& previous_dc) {
    std::array<std::int64_t, 64> horizontal{};
    std::array<int, 64> coefficients{};
    for (unsigned row = 0; row < 8; ++row) {
        for (unsigned frequency = 0; frequency < 8; ++frequency) {
            for (unsigned column = 0; column < 8; ++column) {
                horizontal[row * 8 + frequency] +=
                    (samples[row * 8 + column] - 128) * dct_basis[frequency][column];
            }
        }
    }
    for (unsigned vertical = 0; vertical < 8; ++vertical) {
        for (unsigned horizontal_frequency = 0; horizontal_frequency < 8;
             ++horizontal_frequency) {
            std::int64_t sum = 0;
            for (unsigned row = 0; row < 8; ++row) {
                sum += horizontal[row * 8 + horizontal_frequency] *
                       dct_basis[vertical][row];
            }
            const auto index = vertical * 8 + horizontal_frequency;
            const auto scaled = static_cast<double>(sum) / (8ULL * (1ULL << 40));
            coefficients[index] = static_cast<int>(std::round(scaled / table[index]));
        }
    }

    const auto difference = coefficients[0] - previous_dc;
    previous_dc = coefficients[0];
    const auto dc_length = magnitude(difference);
    if (dc_length > 11) {
        throw std::runtime_error("JPEG DC coefficient exceeds baseline range");
    }
    writer.write(dc_length, 4);
    amplitude(writer, difference, dc_length);
    unsigned zero_run = 0;
    for (unsigned index = 1; index < 64; ++index) {
        const auto value = coefficients[zigzag[index]];
        if (value == 0) {
            ++zero_run;
            continue;
        }
        while (zero_run >= 16) {
            writer.write(1, 8);
            zero_run -= 16;
        }
        const auto length = magnitude(value);
        if (length > 10) {
            throw std::runtime_error("JPEG AC coefficient exceeds baseline range");
        }
        writer.write(2 + zero_run * 10 + length - 1, 8);
        amplitude(writer, value, length);
        zero_run = 0;
    }
    if (zero_run != 0) {
        writer.write(0, 8);
    }
}

double clamp_byte(double value) {
    return std::clamp(std::round(value), 0.0, 255.0);
}

} // namespace

std::vector<std::uint8_t> encode_jpeg(const Yuv420Frame& frame) {
    const auto width = static_cast<std::size_t>(frame.width);
    const auto height = static_cast<std::size_t>(frame.height);
    if (width == 0 || height == 0 || width % 2 != 0 || height % 2 != 0 ||
        width > 65535 || height > 65535 ||
        width > std::numeric_limits<std::size_t>::max() / height) {
        throw std::invalid_argument("JPEG requires valid even 4:2:0 dimensions");
    }
    if (frame.luma.size() != width * height ||
        frame.chroma_u.size() != (width / 2) * (height / 2) ||
        frame.chroma_v.size() != (width / 2) * (height / 2)) {
        throw std::invalid_argument("JPEG frame planes do not match dimensions");
    }

    std::vector<std::uint8_t> output{0xff, 0xd8};
    marker(output, 0xe0, {'J', 'F', 'I', 'F', 0, 1, 1, 0, 0, 1, 0, 1, 0, 0});
    std::vector<std::uint8_t> quantization_header;
    for (unsigned table_index = 0; table_index < 2; ++table_index) {
        quantization_header.push_back(static_cast<std::uint8_t>(table_index));
        const auto& table = table_index == 0 ? quantization : chroma_quantization;
        for (const auto index : zigzag) {
            quantization_header.push_back(static_cast<std::uint8_t>(table[index]));
        }
    }
    marker(output, 0xdb, quantization_header);
    marker(output, 0xc0, {8, static_cast<std::uint8_t>(height >> 8),
        static_cast<std::uint8_t>(height), static_cast<std::uint8_t>(width >> 8),
        static_cast<std::uint8_t>(width), 3, 1, 0x11, 0, 2, 0x11, 1, 3, 0x11, 1});

    std::vector<std::uint8_t> huffman;
    for (unsigned table_index = 0; table_index < 2; ++table_index) {
        huffman.push_back(static_cast<std::uint8_t>(table_index));
        for (unsigned length = 1; length <= 16; ++length) {
            huffman.push_back(length == 4 ? 12 : 0);
        }
        for (unsigned symbol = 0; symbol <= 11; ++symbol) {
            huffman.push_back(static_cast<std::uint8_t>(symbol));
        }
        huffman.push_back(static_cast<std::uint8_t>(0x10 | table_index));
        for (unsigned length = 1; length <= 16; ++length) {
            huffman.push_back(length == 8 ? 162 : 0);
        }
        huffman.insert(huffman.end(), {0x00, 0xf0});
        for (unsigned run = 0; run < 16; ++run) {
            for (unsigned length = 1; length <= 10; ++length) {
                huffman.push_back(static_cast<std::uint8_t>((run << 4) | length));
            }
        }
    }
    marker(output, 0xc4, huffman);
    marker(output, 0xda, {3, 1, 0, 2, 0x11, 3, 0x11, 0, 63, 0});

    BitWriter writer(output);
    std::array<int, 3> previous_dc{};
    for (std::size_t block_y = 0; block_y < height; block_y += 8) {
        for (std::size_t block_x = 0; block_x < width; block_x += 8) {
            std::array<std::array<int, 64>, 3> samples{};
            for (std::size_t row = 0; row < 8; ++row) {
                const auto y = std::min(block_y + row, height - 1);
                for (std::size_t column = 0; column < 8; ++column) {
                    const auto x = std::min(block_x + column, width - 1);
                    const auto luma = static_cast<double>(frame.luma[y * width + x]) - 16;
                    const auto chroma_index = (y / 2) * (width / 2) + x / 2;
                    const auto cb = static_cast<double>(frame.chroma_u[chroma_index]) - 128;
                    const auto cr = static_cast<double>(frame.chroma_v[chroma_index]) - 128;
                    const auto red = static_cast<int>(clamp_byte(1.164 * luma + 1.596 * cr));
                    const auto green = static_cast<int>(clamp_byte(1.164 * luma - 0.392 * cb - 0.813 * cr));
                    const auto blue = static_cast<int>(clamp_byte(1.164 * luma + 2.017 * cb));
                    const auto index = row * 8 + column;
                    samples[0][index] = std::clamp(
                        (19595 * red + 38470 * green + 7471 * blue + 32768) >> 16, 0, 255);
                    samples[1][index] = std::clamp(
                        (-11059 * red - 21709 * green + 32768 * blue + (128 << 16) + 32768) >> 16, 0, 255);
                    samples[2][index] = std::clamp(
                        (32768 * red - 27439 * green - 5329 * blue + (128 << 16) + 32768) >> 16, 0, 255);
                }
            }
            for (unsigned component = 0; component < 3; ++component) {
                const auto& table = component == 0 ? quantization : chroma_quantization;
                encode_block(writer, samples[component], table, previous_dc[component]);
            }
        }
    }
    writer.finish();
    output.insert(output.end(), {0xff, 0xd9});
    return output;
}

} // namespace pygorvid
