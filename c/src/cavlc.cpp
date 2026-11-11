#include "pygorvid/cavlc.hpp"

#include <algorithm>
#include <array>
#include <cstdlib>
#include <limits>
#include <memory>
#include <sstream>
#include <stdexcept>
#include <string>

namespace pygorvid {
namespace {

[[noreturn]] void invalid(const std::string& message) {
    throw std::runtime_error(message);
}

constexpr const char* kCoeffToken = R"(
0 0 1 11 1111 01 1
1 0 000101 001011 001111 000111 0001111
1 1 01 10 1110 1 01
2 0 00000111 000111 001011 000100 0001110
2 1 000100 00111 01111 000110 0001101
2 2 001 011 1101 001 001
3 0 000000111 0000111 001000 000011 000000111
3 1 00000110 001010 01100 0000011 0001100
3 2 0000101 001001 01110 0000010 0001011
3 3 00011 0101 1100 000101 00001
4 0 0000000111 00000111 0001111 000010 000000110
4 1 000000110 000110 01010 00000011 000000101
4 2 00000101 000101 01011 00000010 0001010
4 3 000011 0100 1011 0000000 000001
5 0 00000000111 00000100 0001011 - 0000000111
5 1 0000000110 0000110 01000 - 0000000110
5 2 000000101 0000101 01001 - 000000100
5 3 0000100 00110 1010 - 0001001
6 0 0000000001111 000000111 0001001 - 00000000111
6 1 00000000110 00000110 001110 - 00000000110
6 2 0000000101 00000101 001101 - 0000000101
6 3 00000100 001000 1001 - 0001000
7 0 0000000001011 00000001111 0001000 - 000000000111
7 1 0000000001110 000000110 001010 - 000000000110
7 2 00000000101 000000101 001001 - 00000000101
7 3 000000100 000100 1000 - 0000000100
8 0 0000000001000 00000001011 00001111 - 0000000000111
8 1 0000000001010 00000001110 0001110 - 000000000101
8 2 0000000001101 00000001101 0001101 - 000000000100
8 3 0000000100 0000100 01101 - 00000000100
9 0 00000000001111 000000001111 00001011 - -
9 1 00000000001110 00000001010 00001110 - -
9 2 0000000001001 00000001001 0001010 - -
10 0 00000000001011 000000001011 000001111 - -
10 1 00000000001010 000000001110 00001010 - -
10 2 00000000001101 000000001101 00001101 - -
11 0 000000000001111 000000001000 000001011 - -
11 1 000000000001110 000000001010 000001110 - -
11 2 00000000001001 000000001001 00001001 - -
12 0 000000000001011 0000000001111 000001000 - -
12 1 000000000001010 0000000001110 000001010 - -
12 2 000000000001101 0000000001101 000001101 - -
13 0 0000000000001111 0000000001011 0000001101 - -
13 1 000000000000001 0000000001010 000000111 - -
13 2 000000000001001 0000000001001 000001001 - -
13 3 000000000001100 0000000001100 000001100 - -
14 0 0000000000001011 0000000000111 0000001001 - -
14 1 0000000000001110 00000000001011 0000001100 - -
14 2 0000000000001101 0000000000110 0000001011 - -
14 3 000000000001000 0000000001000 0000001010 - -
15 0 0000000000000111 00000000001001 0000000101 - -
15 1 0000000000001010 00000000001000 0000001000 - -
15 2 0000000000001001 00000000001010 0000000111 - -
15 3 0000000000001100 0000000000001 0000000110 - -
16 0 0000000000000100 00000000000111 0000000001 - -
16 1 0000000000000110 00000000000110 0000000100 - -
16 2 0000000000000101 00000000000101 0000000011 - -
16 3 0000000000001000 00000000000100 0000000010 - -)";

constexpr std::array<const char*, 15> kTotalZeros{{
    "1 011 010 0011 0010 00011 00010 000011 000010 0000011 0000010 00000011 00000010 000000011 000000010 000000001",
    "111 110 101 100 011 0101 0100 0011 0010 00011 00010 000011 000010 000001 000000",
    "0101 111 110 101 0100 0011 100 011 0010 00011 00010 000001 00001 000000",
    "00011 111 0101 0100 110 101 100 0011 011 0010 00010 00001 00000",
    "0101 0100 0011 111 110 101 100 011 0010 00001 0001 00000",
    "000001 00001 111 110 101 100 011 010 0001 001 000000",
    "000001 00001 101 100 011 11 010 0001 001 000000",
    "000001 0001 00001 011 11 10 010 001 000000",
    "000001 000000 0001 11 10 001 01 00001",
    "00001 00000 001 11 10 01 0001",
    "0000 0001 001 010 1 011",
    "0000 0001 01 1 001",
    "000 001 1 01",
    "00 01 1",
    "0 1",
}};
constexpr std::array<const char*, 3> kChromaZeros4{{
    "1 01 001 000", "1 01 00", "1 0",
}};
constexpr std::array<const char*, 7> kChromaZeros8{{
    "1 010 011 0010 0011 0001 00001 00000",
    "000 01 001 100 101 110 111",
    "000 001 01 10 110 111",
    "110 00 01 10 111",
    "00 01 10 11",
    "00 01 1",
    "0 1",
}};
constexpr std::array<const char*, 7> kRunBefore{{
    "1 0",
    "1 01 00",
    "11 10 01 00",
    "11 10 01 001 000",
    "11 10 011 010 001 000",
    "11 000 001 011 010 101 100",
    "111 110 101 100 011 010 001 0001 00001 000001 0000001 00000001 000000001 0000000001 00000000001",
}};

class Vlc {
public:
    Vlc() : nodes_(1) {}

    void insert(const std::string& code, std::uint8_t value) {
        std::size_t index = 0;
        for (const char bit : code) {
            const auto branch = static_cast<std::size_t>(bit - '0');
            auto next = nodes_[index].next[branch];
            if (next == kMissing) {
                next = nodes_.size();
                nodes_[index].next[branch] = next;
                nodes_.emplace_back();
            }
            index = next;
        }
        if (nodes_[index].value != kNoValue) {
            invalid("duplicate CAVLC codeword " + code);
        }
        nodes_[index].value = value;
    }

    std::uint8_t read(BitReader& reader) const {
        std::size_t index = 0;
        while (nodes_[index].value == kNoValue) {
            const auto branch = static_cast<std::size_t>(reader.read_bit());
            const auto next = nodes_[index].next[branch];
            if (next == kMissing) {
                invalid("invalid CAVLC codeword");
            }
            index = next;
        }
        return nodes_[index].value;
    }

private:
    static constexpr std::size_t kMissing = std::numeric_limits<std::size_t>::max();
    static constexpr std::uint8_t kNoValue = std::numeric_limits<std::uint8_t>::max();
    struct Node {
        std::array<std::size_t, 2> next{kMissing, kMissing};
        std::uint8_t value = kNoValue;
    };
    std::vector<Node> nodes_;
};

template <typename Rows>
std::vector<Vlc> make_vlcs(const Rows& rows) {
    std::vector<Vlc> tables;
    tables.reserve(rows.size());
    for (const auto* row : rows) {
        Vlc table;
        std::istringstream codes(row);
        std::string code;
        std::uint8_t value = 0;
        while (codes >> code) {
            table.insert(code, value++);
        }
        tables.push_back(std::move(table));
    }
    return tables;
}

struct Tables {
    std::vector<Vlc> tokens;
    std::vector<Vlc> zeros;
    std::vector<Vlc> chroma4;
    std::vector<Vlc> chroma8;
    std::vector<Vlc> runs;

    Tables() : tokens(5), zeros(make_vlcs(kTotalZeros)),
               chroma4(make_vlcs(kChromaZeros4)),
               chroma8(make_vlcs(kChromaZeros8)),
               runs(make_vlcs(kRunBefore)) {
        std::istringstream rows(kCoeffToken);
        std::string row;
        while (std::getline(rows, row)) {
            std::istringstream fields(row);
            unsigned total = 0;
            unsigned trailing = 0;
            if (!(fields >> total >> trailing)) {
                continue;
            }
            std::string code;
            for (auto& table : tokens) {
                if (!(fields >> code)) {
                    invalid("invalid CAVLC token table");
                }
                if (code != "-") {
                    table.insert(code, static_cast<std::uint8_t>(total * 4 + trailing));
                }
            }
        }
    }
};

const Tables& tables() {
    static const Tables value;
    return value;
}

CavlcResidualBlock decode_block(BitReader& reader, std::int32_t nc,
                                std::size_t max_coeff) {
    const auto& vlcs = tables();
    std::uint8_t token = 0;
    if (nc >= 8) {
        const auto bits = static_cast<std::uint8_t>(reader.read_bits(6));
        token = bits == 3 ? 0 : static_cast<std::uint8_t>((bits / 4 + 1) * 4 + bits % 4);
    } else {
        std::size_t column = 2;
        if (nc == -2) column = 4;
        else if (nc == -1) column = 3;
        else if (nc <= 1) column = 0;
        else if (nc <= 3) column = 1;
        token = vlcs.tokens[column].read(reader);
    }
    const auto total = static_cast<std::size_t>(token / 4);
    const auto trailing = static_cast<std::size_t>(token % 4);
    if (total > max_coeff || trailing > total) {
        invalid("CAVLC coeff_token exceeds block capacity");
    }
    CavlcResidualBlock block{std::vector<std::int32_t>(max_coeff, 0),
                             static_cast<std::uint8_t>(total)};
    if (total == 0) {
        return block;
    }

    std::vector<std::int64_t> levels;
    levels.reserve(total);
    for (std::size_t i = 0; i < trailing; ++i) {
        levels.push_back(reader.read_bit() ? -1 : 1);
    }
    std::uint8_t suffix_length = static_cast<std::uint8_t>(total > 10 && trailing < 3);
    for (std::size_t index = trailing; index < total; ++index) {
        std::uint8_t prefix = 0;
        while (!reader.read_bit()) {
            if (++prefix > 31) {
                invalid("CAVLC level prefix exceeds supported range");
            }
        }
        const std::uint8_t suffix_size =
            prefix == 14 && suffix_length == 0 ? 4 :
            prefix >= 15 ? static_cast<std::uint8_t>(prefix - 3) : suffix_length;
        const auto suffix = reader.read_bits(suffix_size);
        std::int64_t code =
            (static_cast<std::int64_t>(std::min<std::uint8_t>(prefix, 15)) << suffix_length) +
            suffix;
        if (prefix >= 15 && suffix_length == 0) {
            code += 15;
        }
        if (prefix >= 16) {
            code += (std::int64_t{1} << (prefix - 3)) - 4096;
        }
        if (index == trailing && trailing < 3) {
            code += 2;
        }
        const auto level = code % 2 == 0 ? (code + 2) / 2 : -(code + 1) / 2;
        if (level < std::numeric_limits<std::int32_t>::min() ||
            level > std::numeric_limits<std::int32_t>::max()) {
            invalid("CAVLC coefficient exceeds int32");
        }
        levels.push_back(level);
        suffix_length = std::max<std::uint8_t>(suffix_length, 1);
        if (suffix_length < 6 && std::abs(level) > (3LL << (suffix_length - 1))) {
            ++suffix_length;
        }
    }

    std::size_t zeros_left = 0;
    if (total < max_coeff) {
        const std::vector<Vlc>* zeros = &vlcs.zeros;
        if (max_coeff == 4) zeros = &vlcs.chroma4;
        else if (max_coeff == 8) zeros = &vlcs.chroma8;
        zeros_left = (*zeros)[total - 1].read(reader);
    }
    if (zeros_left > max_coeff - total) {
        invalid("CAVLC total_zeros exceeds block capacity");
    }
    std::vector<std::size_t> runs(total, 0);
    for (std::size_t index = 0; index + 1 < total; ++index) {
        if (zeros_left != 0) {
            const auto run = vlcs.runs[std::min<std::size_t>(zeros_left, 7) - 1].read(reader);
            if (run > zeros_left) {
                invalid("CAVLC run_before exceeds zeros left");
            }
            runs[index] = run;
            zeros_left -= run;
        }
    }
    runs[total - 1] = zeros_left;
    std::size_t position = 0;
    for (std::size_t index = total; index-- > 0;) {
        position += runs[index];
        block.coefficients[position] = static_cast<std::int32_t>(levels[index]);
        ++position;
    }
    return block;
}

} // namespace

CavlcResidualBlock decode_cavlc_residual_block(BitReader& reader,
                                                std::int32_t nc,
                                                std::size_t max_coeff) {
    if (!((nc == -1 && max_coeff == 4) || (nc == -2 && max_coeff == 8) ||
          (nc >= 0 && nc <= 16 && (max_coeff == 15 || max_coeff == 16)))) {
        invalid("invalid CAVLC block context or coefficient count");
    }
    const auto start = reader.bit_offset();
    try {
        return decode_block(reader, nc, max_coeff);
    } catch (...) {
        reader.restore_bit_offset(start);
        throw;
    }
}

} // namespace pygorvid
