#ifndef PYGORVID_PNG_HPP
#define PYGORVID_PNG_HPP

#include "pygorvid/h264.hpp"

#include <cstdint>
#include <vector>

namespace pygorvid {

std::vector<std::uint8_t> encode_png(const Yuv420Frame& frame);

} // namespace pygorvid

#endif
