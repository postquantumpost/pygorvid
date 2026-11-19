#include "pygorvid/h264.hpp"
#include "pygorvid/h264_encoder.hpp"
#include "pygorvid/mp4_writer.hpp"

#include <cassert>
#include <algorithm>
#include <array>
#include <cstdint>
#include <vector>
#include <stdexcept>

int main() {
    const pygorvid::H264Encoder encoder;
    const auto sps = pygorvid::parse_sps(encoder.sequence_parameter_set());
    const auto pps = pygorvid::parse_pps(encoder.picture_parameter_set());
    for (const std::uint16_t index : {0, 24, 65535}) {
        const auto sample = encoder.encode_grey_frame(index);
        const auto frame = pygorvid::decode_idr_i_pcm_sample(sample, 4, sps, pps);
        assert(frame.width == 64 && frame.height == 64);
        assert(frame.luma == std::vector<std::uint8_t>(4096, 128));
        assert(frame.chroma_u == std::vector<std::uint8_t>(1024, 128));
        assert(frame.chroma_v == std::vector<std::uint8_t>(1024, 128));
    }
    const auto feature_sample = encoder.encode_generated_frame(1);
    const std::vector<std::uint8_t> prevention_sequence{0, 0, 3, 1};
    assert(std::search(feature_sample.begin() + 4, feature_sample.end(),
        prevention_sequence.begin(), prevention_sequence.end()) != feature_sample.end());
    const auto feature_frame = pygorvid::decode_idr_i_pcm_sample(feature_sample, 4, sps, pps);
    assert(feature_frame.luma[0] == 0 && feature_frame.luma[1] == 0 && feature_frame.luma[2] == 1);
    assert(std::all_of(feature_frame.luma.begin() + 3, feature_frame.luma.end(),
        [](std::uint8_t value) { return value == 128; }));
    assert(feature_frame.chroma_u == std::vector<std::uint8_t>(1024, 128));
    assert(feature_frame.chroma_v == std::vector<std::uint8_t>(1024, 128));
    const auto non_idr_sample = encoder.encode_generated_frame(2);
    assert(pygorvid::parse_nal_header(std::vector<std::uint8_t>(
        non_idr_sample.begin() + 4, non_idr_sample.end())).unit_type == 1);
    const auto non_idr_frame = pygorvid::decode_h264_reference_frame(
        {std::vector<std::uint8_t>(
             non_idr_sample.begin() + 4, non_idr_sample.end())},
        sps, pps);
    assert(non_idr_frame.luma == std::vector<std::uint8_t>(4096, 128));
    assert(non_idr_frame.chroma_u ==
           std::vector<std::uint8_t>(1024, 128));
    assert(non_idr_frame.chroma_v ==
           std::vector<std::uint8_t>(1024, 128));
    const auto non_idr_header = pygorvid::parse_idr_i_slice_header(
        std::vector<std::uint8_t>(
            non_idr_sample.begin() + 4, non_idr_sample.end()),
        sps, pps);
    assert(non_idr_header.frame_num == 1);
    const auto dc_sample = encoder.encode_generated_frame(3);
    const auto dc_nal = std::vector<std::uint8_t>(
        dc_sample.begin() + 4, dc_sample.end());
    assert(pygorvid::parse_nal_header(dc_nal).unit_type == 1);
    const auto dc_header =
        pygorvid::parse_idr_i_slice_header(dc_nal, sps, pps);
    assert(dc_header.frame_num == 2 && dc_header.slice_type == 2);
    const auto dc_frame =
        pygorvid::decode_h264_reference_frame({dc_nal}, sps, pps);
    assert(dc_frame.luma == std::vector<std::uint8_t>(4096, 128));
    assert(dc_frame.chroma_u == std::vector<std::uint8_t>(1024, 128));
    assert(dc_frame.chroma_v == std::vector<std::uint8_t>(1024, 128));
    const auto modes_sample = encoder.encode_generated_frame(4);
    const auto modes_nal = std::vector<std::uint8_t>(
        modes_sample.begin() + 4, modes_sample.end());
    const auto modes_header =
        pygorvid::parse_idr_i_slice_header(modes_nal, sps, pps);
    assert(modes_header.frame_num == 3);
    assert(pygorvid::parse_nal_header(modes_nal).unit_type == 1);
    const auto intra4_sample = encoder.encode_generated_frame(5);
    const auto intra4_nal = std::vector<std::uint8_t>(
        intra4_sample.begin() + 4, intra4_sample.end());
    const auto intra4_header =
        pygorvid::parse_idr_i_slice_header(intra4_nal, sps, pps);
    assert(intra4_header.frame_num == 4);
    const auto intra4_frame =
        pygorvid::decode_h264_reference_frame({intra4_nal}, sps, pps);
    assert(intra4_frame.luma == std::vector<std::uint8_t>(4096, 128));
    const auto directional_sample = encoder.encode_generated_frame(6);
    const auto directional_nal = std::vector<std::uint8_t>(
        directional_sample.begin() + 4, directional_sample.end());
    const auto directional_header =
        pygorvid::parse_idr_i_slice_header(directional_nal, sps, pps);
    assert(directional_header.frame_num == 5);
    assert(pygorvid::parse_nal_header(directional_nal).unit_type == 1);
    const auto directional_frame =
        pygorvid::decode_h264_reference_frame({directional_nal}, sps, pps);
    for (int column = 0; column < 16; ++column) {
        assert(directional_frame.luma[column] == 32 + 3 * column);
    }
    assert(std::minmax_element(
        directional_frame.luma.begin(), directional_frame.luma.end()).first !=
        std::minmax_element(
            directional_frame.luma.begin(), directional_frame.luma.end()).second);
    const auto residual_sample = encoder.encode_generated_frame(7);
    const auto residual_nal = std::vector<std::uint8_t>(
        residual_sample.begin() + 4, residual_sample.end());
    const auto residual_header =
        pygorvid::parse_idr_i_slice_header(residual_nal, sps, pps);
    assert(residual_header.frame_num == 6);
    assert(pygorvid::parse_nal_header(residual_nal).unit_type == 1);
    const auto residual_frame =
        pygorvid::decode_h264_reference_frame({residual_nal}, sps, pps);
    const std::size_t residual_origin = 48 * 64 + 48;
    assert(residual_frame.luma[residual_origin] ==
           directional_frame.luma[residual_origin] + 4);
    for (int row = 0; row < 64; ++row) {
        for (int column = 0; column < 64; ++column) {
            if (row < 48 || column < 48) {
                const auto index = static_cast<std::size_t>(row * 64 + column);
                assert(residual_frame.luma[index] == directional_frame.luma[index]);
            }
        }
    }
    assert(residual_frame.chroma_u == directional_frame.chroma_u);
    assert(residual_frame.chroma_v == directional_frame.chroma_v);
    const auto chroma_sample = encoder.encode_generated_frame(8);
    const auto chroma_nal = std::vector<std::uint8_t>(
        chroma_sample.begin() + 4, chroma_sample.end());
    const auto chroma_header =
        pygorvid::parse_idr_i_slice_header(chroma_nal, sps, pps);
    assert(chroma_header.frame_num == 7);
    assert(pygorvid::parse_nal_header(chroma_nal).unit_type == 1);
    const auto chroma_frame =
        pygorvid::decode_h264_reference_frame({chroma_nal}, sps, pps);
    assert(chroma_frame.luma == residual_frame.luma);
    bool chroma_u_changed = false;
    bool chroma_v_changed = false;
    for (int row = 0; row < 32; ++row) {
        for (int column = 0; column < 32; ++column) {
            const auto index = static_cast<std::size_t>(row * 32 + column);
            if (row < 24 || column < 24) {
                assert(chroma_frame.chroma_u[index] == residual_frame.chroma_u[index]);
                assert(chroma_frame.chroma_v[index] == residual_frame.chroma_v[index]);
            } else {
                chroma_u_changed |=
                    chroma_frame.chroma_u[index] != residual_frame.chroma_u[index];
                chroma_v_changed |=
                    chroma_frame.chroma_v[index] != residual_frame.chroma_v[index];
            }
        }
    }
    assert(chroma_u_changed && chroma_v_changed);
    const auto expanded_sample = encoder.encode_generated_frame(9);
    const auto expanded_nal = std::vector<std::uint8_t>(
        expanded_sample.begin() + 4, expanded_sample.end());
    const auto expanded_header =
        pygorvid::parse_idr_i_slice_header(expanded_nal, sps, pps);
    assert(expanded_header.frame_num == 8);
    assert(pygorvid::parse_nal_header(expanded_nal).unit_type == 1);
    const auto expanded_frame =
        pygorvid::decode_h264_reference_frame({expanded_nal}, sps, pps);
    assert(expanded_frame.chroma_u == chroma_frame.chroma_u);
    assert(expanded_frame.chroma_v == chroma_frame.chroma_v);
    bool luma_changed = false;
    for (int row = 0; row < 64; ++row) {
        for (int column = 0; column < 64; ++column) {
            const auto index = static_cast<std::size_t>(row * 64 + column);
            if (row < 48 || column < 48) {
                assert(expanded_frame.luma[index] == chroma_frame.luma[index]);
            } else {
                luma_changed |= expanded_frame.luma[index] != chroma_frame.luma[index];
            }
        }
    }
    assert(luma_changed);
    const std::vector<std::uint32_t> feature_nal_counts{
        1, 2, 2, 1, 1, 1, 1, 1, 5,
    };
    const std::array<std::uint32_t, 9> feature_pps{4, 9, 9, 6, 5, 5, 7, 8, 8};
    for (std::uint16_t frame_index = 31; frame_index <= 39; ++frame_index) {
        const auto sample = encoder.encode_generated_frame(frame_index);
        const auto nals = pygorvid::split_h264_sample_nals(sample, 4);
        assert(nals.size() == feature_nal_counts[frame_index - 31]);
        const auto& picture = nals[frame_index == 39 ? 2 : 0];
        const auto rbsp = pygorvid::ebsp_to_rbsp(
            std::vector<std::uint8_t>(picture.begin() + 1, picture.end()));
        auto bit = [&rbsp](std::size_t& offset) {
            const auto value = (rbsp[offset / 8] >> (7 - offset % 8)) & 1;
            ++offset;
            return value;
        };
        auto ue = [&bit](std::size_t& offset) {
            std::uint32_t zeros = 0;
            while (!bit(offset)) {
                ++zeros;
            }
            std::uint32_t value = 1;
            for (std::uint32_t index = 0; index < zeros; ++index) {
                value = (value << 1) | bit(offset);
            }
            return value - 1;
        };
        std::size_t offset = 0;
        (void)ue(offset);
        assert(ue(offset) == 2);
        assert(ue(offset) == feature_pps[frame_index - 31]);
        assert(sample == encoder.encode_generated_frame(frame_index));
        if (frame_index == 39) {
            assert(pygorvid::parse_nal_header(nals[0]).unit_type == 9);
            assert(pygorvid::parse_nal_header(nals[1]).unit_type == 6);
            assert(pygorvid::parse_nal_header(nals[3]).unit_type == 10);
            assert(pygorvid::parse_nal_header(nals[4]).unit_type == 11);
            const auto sei_rbsp = pygorvid::ebsp_to_rbsp(
                std::vector<std::uint8_t>(nals[1].begin() + 1, nals[1].end()));
            assert(sei_rbsp[0] == 5 && sei_rbsp[1] == 0x12);
            const std::string marker(sei_rbsp.begin() + 2,
                                     sei_rbsp.begin() + 2 + 0x12);
            assert(marker == "PYGORVID-FRAME-39!");
        }
    }
    const pygorvid::Mp4Writer writer;
    const auto payload = writer.encode(encoder);
    assert(payload == writer.encode(encoder));
    const pygorvid::Mp4Metadata empty;
    assert(payload == writer.encode(encoder, &empty));
    pygorvid::Mp4Metadata metadata;
    metadata.title = u8"Caf\u00e9 \u65e5\u672c \U0001f4f7";
    metadata.title += std::string("\0tail", 5);
    const auto tagged = writer.encode(encoder, &metadata);
    assert(std::search(tagged.begin(), tagged.end(), metadata.title.begin(), metadata.title.end(),
        [](std::uint8_t byte, char ch) { return byte == static_cast<std::uint8_t>(ch); }) != tagged.end());
    assert(tagged.size() > payload.size() + 69 + 32 + 5 + metadata.title.size());
    const std::string handler = "mdirappl";
    assert(std::search(tagged.begin(), tagged.end(), handler.begin(), handler.end()) != tagged.end());
    pygorvid::Mp4Metadata windows;
    windows.title = u8"A\u00e9\u65e5\U0001f4f7";
    windows.title += std::string("\0tail", 5);
    const std::vector<std::uint8_t> utf16{
        'A', 0, 0xe9, 0, 0xe5, 0x65, 0x3d, 0xd8, 0xf7, 0xdc, 0, 0,
        't', 0, 'a', 0, 'i', 0, 'l', 0, 0, 0,
    };
    std::vector<std::uint8_t> xtra{0, 0, 0, 107, 'X', 't', 'r', 'a'};
    for (const auto& name : {std::string("Title"), std::string("pygorvid_title")}) {
        xtra.insert(xtra.end(), {0, 0, 0, static_cast<std::uint8_t>(18 + name.size() + utf16.size()),
                                0, 0, 0, static_cast<std::uint8_t>(name.size())});
        xtra.insert(xtra.end(), name.begin(), name.end());
        xtra.insert(xtra.end(), {0, 0, 0, 1, 0, 0, 0, 28, 0, 8});
        xtra.insert(xtra.end(), utf16.begin(), utf16.end());
    }
    const auto windows_payload = writer.encode(encoder, &windows);
    assert(std::search(windows_payload.begin(), windows_payload.end(), xtra.begin(), xtra.end()) != windows_payload.end());
    const std::string xtra_tag = "Xtra";
    assert(std::search(payload.begin(), payload.end(), xtra_tag.begin(), xtra_tag.end()) == payload.end());
    for (const auto& date : {"2016", "2016-12-31 23:59:60 UTC", "2016junk", "-2016", u8"\u0662\u0660\u0661\u0666", ""}) {
        windows.title.clear();
        windows.date = date;
        const auto date_payload = writer.encode(encoder, &windows);
        const std::string year_tag = "WM/Year";
        const bool year = windows.date == "2016" || windows.date == "2016-12-31 23:59:60 UTC";
        assert((std::search(date_payload.begin(), date_payload.end(), year_tag.begin(), year_tag.end()) != date_payload.end()) == year);
    }
    for (const auto& number : {"0", "1", "001", "65535", "65536", "-1", "1/2", u8"\u0661"}) {
        pygorvid::Mp4Metadata numeric;
        numeric.track = number;
        numeric.disc = number;
        const auto encoded = writer.encode(encoder, &numeric);
        const std::string track_tag = "trkn";
        const std::string disc_tag = "disk";
        const bool binary = numeric.track == "0" || numeric.track == "1" ||
                            numeric.track == "001" || numeric.track == "65535";
        assert((std::search(encoded.begin(), encoded.end(), track_tag.begin(), track_tag.end()) != encoded.end()) == binary);
        assert((std::search(encoded.begin(), encoded.end(), disc_tag.begin(), disc_tag.end()) != encoded.end()) == binary);
    }
    for (const auto& invalid : {std::string("\xff"), std::string("\xc0\x80"),
            std::string("\xed\xa0\x80"), std::string("\xf4\x90\x80\x80"), std::string("\xe2\x82")}) {
        metadata.title = invalid;
        bool rejected = false;
        try {
            (void)writer.encode(encoder, &metadata);
        } catch (const std::invalid_argument& error) {
            rejected = std::string(error.what()).find("title is not valid UTF-8") != std::string::npos;
        }
        assert(rejected);
    }
    assert((std::vector<std::uint8_t>(payload.begin() + 4, payload.begin() + 8) ==
            std::vector<std::uint8_t>{'f', 't', 'y', 'p'}));
}
