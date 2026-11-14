#pragma once

#include <cstdint>
#include <vector>

namespace pygorvid {

class H264Encoder {
public:
    std::vector<std::uint8_t> sequence_parameter_set() const;
    std::vector<std::uint8_t> picture_parameter_set() const;
    std::vector<std::uint8_t> encode_grey_frame(std::uint16_t idr_pic_id = 0) const;
};

} // namespace pygorvid
