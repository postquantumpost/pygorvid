#pragma once

#include "pygorvid/h264_encoder.hpp"
#include <string>

namespace pygorvid {

struct Mp4Metadata {
    std::string title;
    std::string artist;
    std::string album_artist;
    std::string genre;
    std::string track;
    std::string disc;
    std::string date;
    std::string composer;
    std::string comment;
    std::string description;
    std::string copyright;
    std::string make;
    std::string model;
    std::string firmware;
    std::string location;
    std::string altitude;
    std::string heading;
    std::string encoder;
};

class Mp4Writer {
public:
    std::vector<std::uint8_t> encode(const H264Encoder& encoder,
                                   const Mp4Metadata* metadata = nullptr) const;
};

} // namespace pygorvid
