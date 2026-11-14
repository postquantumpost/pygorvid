#pragma once

#include "pygorvid/h264_encoder.hpp"

namespace pygorvid {

class Mp4Writer {
public:
    std::vector<std::uint8_t> encode(const H264Encoder& encoder) const;
};

} // namespace pygorvid
