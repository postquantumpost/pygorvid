#include "pygorvid/decoder.hpp"

#include <cassert>
#include <filesystem>
#include <optional>
#include <vector>

int main() {
    const auto fixture = std::filesystem::path(PYGORVID_SOURCE_DIR) /
                         "samples/decoder-smoke-16x16.mp4";
    pygorvid::VideoDecoder decoder(fixture.string());
    std::vector<std::size_t> sequential_indices;
    const auto sequential_count = decoder.decode(
        {2, false, std::nullopt},
        [&](pygorvid::DecodedVideoFrame&& frame) {
            sequential_indices.push_back(frame.presentation_index);
            assert(frame.frame.width == 16);
            assert(frame.frame.height == 16);
            assert(frame.duration_seconds > 0);
            return true;
        });
    assert(sequential_count == sequential_indices.size());
    assert(!sequential_indices.empty());
    for (std::size_t index = 0; index < sequential_indices.size(); ++index) {
        assert(sequential_indices[index] == index);
    }

    std::vector<std::size_t> readahead_indices;
    const auto readahead_count = decoder.decode(
        {2, true, std::nullopt},
        [&](pygorvid::DecodedVideoFrame&& frame) {
            readahead_indices.push_back(frame.presentation_index);
            return true;
        });
    assert(readahead_count == sequential_count);
    assert(readahead_indices == sequential_indices);

    std::vector<std::size_t> no_readahead_indices;
    const auto no_readahead_count = decoder.decode(
        {2, true, std::nullopt, 0},
        [&](pygorvid::DecodedVideoFrame&& frame) {
            no_readahead_indices.push_back(frame.presentation_index);
            return true;
        });
    assert(no_readahead_count == sequential_count);
    assert(no_readahead_indices == sequential_indices);

    auto selected = decoder.decode_frame(sequential_indices.back(), 2);
    assert(selected.presentation_index == sequential_indices.back());
    assert(selected.frame.luma.size() == 256);
}
