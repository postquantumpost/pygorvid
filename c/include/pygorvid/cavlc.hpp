#ifndef PYGORVID_CAVLC_HPP
#define PYGORVID_CAVLC_HPP

#include "pygorvid/pygorvid.hpp"

namespace pygorvid {

struct CavlcResidualBlock {
    std::vector<std::int32_t> coefficients;
    std::uint8_t total_coeff = 0;
};

CavlcResidualBlock decode_cavlc_residual_block(BitReader& reader,
                                                std::int32_t nc,
                                                std::size_t max_coeff);

} // namespace pygorvid

#endif
