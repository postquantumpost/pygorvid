#include "pygorvid/decoder.hpp"
#include "pygorvid/h264.hpp"
#include "pygorvid/h264_annexb.hpp"
#include "pygorvid/h264_encoder.hpp"
#include "pygorvid/mp4_writer.hpp"
#include "pygorvid/jpeg.hpp"
#include "pygorvid/pygorvid.hpp"
#include "pygorvid/png.hpp"

#include <algorithm>
#include <array>
#include <cctype>
#include <chrono>
#include <filesystem>
#include <fstream>
#include <functional>
#include <iomanip>
#include <iostream>
#include <limits>
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

void validate_image_output(const std::string& input_path,
                           const std::string& output_path) {
    std::error_code error;
    const auto normalized_input =
        std::filesystem::absolute(input_path, error).lexically_normal();
    if (error) throw std::runtime_error("cannot resolve input file path");
    const auto normalized_output =
        std::filesystem::absolute(output_path, error).lexically_normal();
    if (error || normalized_input == normalized_output) {
        throw std::invalid_argument(
            "image output path must not overwrite the input video");
    }
    const bool output_exists = std::filesystem::exists(output_path, error);
    if (error) throw std::runtime_error("cannot inspect image output path");
    if (output_exists &&
        std::filesystem::equivalent(input_path, output_path, error)) {
        throw std::invalid_argument(
            "image output path must not overwrite the input video");
    }
    if (error) {
        throw std::runtime_error("cannot compare input and output paths");
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
            validate_image_output(options.files[0], output_path);
            pygorvid::VideoDecoder decoder(options.files[0]);
            const auto frame = decoder.decode_frame(
                *options.extract_frame, options.threads);
            save_image(output_path, frame.frame);
            if (options.hash_frames) {
                std::cout << options.files[0] << ": frame "
                          << *options.extract_frame << " sha256="
                          << sha256_frame(frame.frame) << '\n';
            }
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
#ifndef PYGORVID_SLIDESHOW
                throw std::runtime_error(
                    "optional slideshow build feature was not included; rebuild with -DPYGORVID_SLIDESHOW=ON");
#else
                Slideshow window(path);
                auto deadline = std::chrono::steady_clock::now();
                std::size_t displayed_count = 0;
                pygorvid::VideoDecoder decoder(path);
                (void)decoder.decode(
                    {options.threads, options.readahead, std::nullopt},
                    [&](pygorvid::DecodedVideoFrame&& frame) {
                        if (!window.wait(deadline)) return false;
                        window.show(frame.frame);
                        ++displayed_count;
                        deadline = std::chrono::steady_clock::now() +
                            std::chrono::duration_cast<
                                std::chrono::steady_clock::duration>(
                                std::chrono::duration<double>(
                                    frame.duration_seconds));
                        return true;
                    });
                if (displayed_count != 0) (void)window.wait(deadline);
                std::cout << path << ": displayed " << displayed_count
                          << " frames\n";
#endif
            } catch (const std::exception& error) {
                std::cerr << path << ": " << error.what() << '\n';
                status = 1;
            }
            continue;
        }
        if (options.read_all) {
            try {
                pygorvid::VideoDecoder decoder(path);
                const auto decoded_count = decoder.decode(
                    {options.threads, options.readahead, std::nullopt},
                    [&](pygorvid::DecodedVideoFrame&& frame) {
                        if (options.hash_frames) {
                            std::cout << path << ": frame "
                                      << frame.presentation_index
                                      << " sha256="
                                      << sha256_frame(frame.frame) << '\n';
                        }
                        return true;
                    });
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
    std::cout << std::unitbuf;
    std::cerr << std::unitbuf;
    std::vector<std::string> args;
    for (int index = 1; index < argc; ++index) {
        args.emplace_back(argv[index]);
    }
    return run(args);
}
