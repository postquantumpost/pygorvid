#ifndef PYGORVID_AV1_HPP
#define PYGORVID_AV1_HPP

#include "pygorvid/h264.hpp"

#include <cstdint>

namespace pygorvid {

class AV1Decoder {
public:
    void check_available() const;
    Yuv420Frame decode_frame(std::uint64_t index) const;
};

} // namespace pygorvid

#endif
