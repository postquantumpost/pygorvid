#ifndef PYGORVID_DECODER_HPP
#define PYGORVID_DECODER_HPP

#include "pygorvid/h264.hpp"

#include <cstddef>
#include <functional>
#include <optional>
#include <string>

namespace pygorvid {

struct DecodedVideoFrame {
    std::size_t presentation_index = 0;
    double duration_seconds = 0;
    Yuv420Frame frame;
};

struct VideoDecodeOptions {
    std::size_t threads = 0;
    bool readahead = true;
    std::optional<std::size_t> presentation_frame;
    std::optional<std::size_t> threads_readahead;
};

class VideoDecoder {
public:
    explicit VideoDecoder(std::string path);

    using FrameConsumer = std::function<bool(DecodedVideoFrame&&)>;

    std::size_t decode(const VideoDecodeOptions& options,
                       const FrameConsumer& consumer) const;
    DecodedVideoFrame decode_frame(std::size_t presentation_index,
                                   std::size_t threads = 0) const;

private:
    std::string path_;
};

} // namespace pygorvid

#endif
