#pragma once

#include <cstdint>
#include <string>
#include <vector>

namespace pygorvid {

struct H264AnnexBStream {
    std::string suffix;
    std::vector<std::uint8_t> data;
};

std::vector<H264AnnexBStream> generate_h264_annex_b_streams();

std::vector<H264AnnexBStream> generate_h264_companion_files();

inline std::vector<H264AnnexBStream>
generate_h264_annex_b_companions() {
    return generate_h264_companion_files();
}

} // namespace pygorvid
