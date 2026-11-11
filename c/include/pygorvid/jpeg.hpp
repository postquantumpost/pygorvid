#ifndef PYGORVID_JPEG_HPP
#define PYGORVID_JPEG_HPP

#include "pygorvid/h264.hpp"

#include <cstdint>
#include <vector>

namespace pygorvid {

std::vector<std::uint8_t> encode_jpeg(const Yuv420Frame& frame);

} // namespace pygorvid

#endif
