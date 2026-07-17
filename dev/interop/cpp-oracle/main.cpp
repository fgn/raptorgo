#include <RaptorQ/RaptorQ_v1_hdr.hpp>

#include <algorithm>
#include <cstdint>
#include <fstream>
#include <iomanip>
#include <iostream>
#include <iterator>
#include <limits>
#include <set>
#include <stdexcept>
#include <string>
#include <utility>
#include <vector>

namespace RaptorQ = RaptorQ__v1;

namespace {

RaptorQ::Block_Size block_for(std::uint32_t symbols) {
    for (auto block : *RaptorQ::blocks) {
        if (static_cast<std::uint32_t>(block) >= symbols) {
            return block;
        }
    }
    throw std::runtime_error("no libRaptorQ block for K");
}

void print_vector(std::uint32_t k, std::uint16_t symbol_size, std::uint32_t esi,
                  const std::vector<std::uint8_t>& payload) {
    std::cout << "s " << k << ' ' << symbol_size << ' ' << esi << ' ';
    for (const auto octet : payload) {
        std::cout << std::hex << std::setfill('0') << std::setw(2)
                  << static_cast<unsigned>(octet);
    }
    std::cout << std::dec << '\n';
}

template <typename Symbol>
std::vector<std::uint8_t> materialize(Symbol&& symbol, std::uint16_t symbol_size) {
    std::vector<std::uint8_t> payload(symbol_size, 0);
    auto output = payload.begin();
    const auto written = symbol(output, payload.end());
    if (written != symbol_size) {
        throw std::runtime_error("libRaptorQ wrote a partial symbol");
    }
    return payload;
}

void raw_vectors(const std::vector<std::uint32_t>& block_sizes) {
    // The libRaptorQ RAW API takes an extended Table-2 block size, so this
    // oracle corpus uses K values for which K'=K. Padding cases remain covered
    // by the independent Rust RFC API.
    for (const std::uint32_t k : block_sizes) {
        for (const std::uint16_t symbol_size : {1u, 7u, 31u}) {
            std::vector<std::uint8_t> input(
                static_cast<std::size_t>(k) * symbol_size);
            for (std::uint32_t symbol = 0; symbol < k; ++symbol) {
                for (std::uint32_t index = 0; index < symbol_size; ++index) {
                    input[static_cast<std::size_t>(symbol) * symbol_size + index] =
                        static_cast<std::uint8_t>(symbol * 131u + index * 37u + 19u);
                }
            }

            using iterator = std::vector<std::uint8_t>::iterator;
            RaptorQ::Encoder<iterator, iterator> encoder(block_for(k), symbol_size);
            if (encoder.set_data(input.begin(), input.end()) != input.size()) {
                throw std::runtime_error("libRaptorQ rejected source data");
            }
            if (encoder.symbols() != k) {
                throw std::runtime_error("libRaptorQ K does not match input K");
            }
            if (!encoder.compute_sync()) {
                throw std::runtime_error("libRaptorQ encoder computation failed");
            }

            const std::vector<std::uint32_t> wanted = {0, k - 1, k, k + 1, k + 17};
            for (auto symbol = encoder.begin_source(); symbol != encoder.end_source(); ++symbol) {
                const auto esi = (*symbol).id();
                if (std::find(wanted.begin(), wanted.end(), esi) != wanted.end()) {
                    print_vector(k, symbol_size, esi, materialize(*symbol, symbol_size));
                }
            }
            const auto last = k + 17;
            for (auto symbol = encoder.begin_repair();
                 symbol != encoder.end_repair(last + 1); ++symbol) {
                const auto esi = (*symbol).id();
                if (std::find(wanted.begin(), wanted.end(), esi) != wanted.end()) {
                    print_vector(k, symbol_size, esi, materialize(*symbol, symbol_size));
                }
            }
        }
    }
}

std::uint32_t read_u32(const std::vector<std::uint8_t>& data, std::size_t offset) {
    if (offset > data.size() || data.size() - offset < 4) {
        throw std::runtime_error("truncated 32-bit field");
    }
    return static_cast<std::uint32_t>(data[offset]) << 24 |
           static_cast<std::uint32_t>(data[offset + 1]) << 16 |
           static_cast<std::uint32_t>(data[offset + 2]) << 8 |
           static_cast<std::uint32_t>(data[offset + 3]);
}

void write_u32(std::ostream& output, std::uint32_t value) {
    const std::uint8_t bytes[] = {
        static_cast<std::uint8_t>(value >> 24),
        static_cast<std::uint8_t>(value >> 16),
        static_cast<std::uint8_t>(value >> 8),
        static_cast<std::uint8_t>(value),
    };
    output.write(reinterpret_cast<const char*>(bytes), sizeof(bytes));
}

std::vector<std::uint8_t> read_file(const std::string& path) {
    std::ifstream input(path, std::ios::binary);
    if (!input) {
        throw std::runtime_error("cannot open input file");
    }
    return std::vector<std::uint8_t>(std::istreambuf_iterator<char>(input),
                                     std::istreambuf_iterator<char>());
}

void write_file(const std::string& path, const std::vector<std::uint8_t>& data) {
    std::ofstream output(path, std::ios::binary | std::ios::trunc);
    if (!output ||
        !output.write(reinterpret_cast<const char*>(data.data()), data.size())) {
        throw std::runtime_error("cannot write output file");
    }
}

std::vector<std::uint32_t> pattern_esis(std::uint32_t k,
                                        const std::string& pattern) {
    std::vector<std::uint32_t> esis;
    if (pattern == "repair") {
        for (std::uint32_t esi = k; esi < 2 * k + 4; ++esi) {
            esis.push_back(esi);
        }
    } else if (pattern == "mixed") {
        const std::set<std::uint32_t> dropped = {1, k / 2, k - 1};
        for (std::uint32_t esi = k; esi > 0; --esi) {
            if (dropped.count(esi - 1) == 0) {
                esis.push_back(esi - 1);
            }
        }
        for (std::uint32_t esi = k + 8; esi > k; --esi) {
            esis.push_back(esi - 1);
        }
        if (!esis.empty()) {
            esis.push_back(esis.front());
        }
    } else if (pattern == "deficient") {
        for (std::uint32_t esi = 0; esi + 1 < k; ++esi) {
            esis.push_back(esi);
        }
    } else {
        throw std::runtime_error("unknown cross-decoding pattern");
    }
    return esis;
}

std::vector<std::vector<std::uint8_t>> encode_payloads(
    std::vector<std::uint8_t> input, std::uint32_t k,
    std::uint16_t symbol_size, const std::vector<std::uint32_t>& esis) {
    using iterator = std::vector<std::uint8_t>::iterator;
    RaptorQ::Encoder<iterator, iterator> encoder(block_for(k), symbol_size);
    if (encoder.set_data(input.begin(), input.end()) != input.size() ||
        encoder.symbols() != k || !encoder.compute_sync()) {
        throw std::runtime_error("libRaptorQ cross encoder initialization failed");
    }
    const auto max_id = *std::max_element(esis.begin(), esis.end());
    std::vector<std::vector<std::uint8_t>> payloads(max_id + 1);
    for (auto symbol = encoder.begin_source(); symbol != encoder.end_source();
         ++symbol) {
        if ((*symbol).id() <= max_id) {
            payloads[(*symbol).id()] = materialize(*symbol, symbol_size);
        }
    }
    if (max_id >= k) {
        for (auto symbol = encoder.begin_repair();
             symbol != encoder.end_repair(max_id + 1); ++symbol) {
            if ((*symbol).id() <= max_id) {
                payloads[(*symbol).id()] = materialize(*symbol, symbol_size);
            }
        }
    }
    return payloads;
}

void encode_cross(const std::string& input_path, std::uint32_t k,
                  std::uint16_t symbol_size, const std::string& pattern,
                  const std::string& output_path) {
    const auto input = read_file(input_path);
    if (symbol_size == 0 || input.size() != static_cast<std::size_t>(k) * symbol_size) {
        throw std::runtime_error("cross input size does not equal K*T");
    }
    const auto esis = pattern_esis(k, pattern);
    if (esis.empty()) {
        throw std::runtime_error("empty cross pattern");
    }
    const auto payloads = encode_payloads(input, k, symbol_size, esis);

    std::ofstream output(output_path, std::ios::binary | std::ios::trunc);
    if (!output) {
        throw std::runtime_error("cannot create cross frame");
    }
    output.write("RQO1", 4);
    const std::uint64_t transfer_length = input.size();
    const std::uint8_t oti[] = {
        static_cast<std::uint8_t>(transfer_length >> 32),
        static_cast<std::uint8_t>(transfer_length >> 24),
        static_cast<std::uint8_t>(transfer_length >> 16),
        static_cast<std::uint8_t>(transfer_length >> 8),
        static_cast<std::uint8_t>(transfer_length),
        0,
        static_cast<std::uint8_t>(symbol_size >> 8),
        static_cast<std::uint8_t>(symbol_size),
        1,
        0,
        1,
        1,
    };
    output.write(reinterpret_cast<const char*>(oti), sizeof(oti));
    write_u32(output, static_cast<std::uint32_t>(esis.size()));
    for (const auto esi : esis) {
        const auto& payload = payloads[esi];
        if (payload.size() != symbol_size) {
            throw std::runtime_error("libRaptorQ did not emit a requested symbol");
        }
        write_u32(output, 4 + symbol_size);
        const std::uint8_t payload_id[] = {
            0,
            static_cast<std::uint8_t>(esi >> 16),
            static_cast<std::uint8_t>(esi >> 8),
            static_cast<std::uint8_t>(esi),
        };
        output.write(reinterpret_cast<const char*>(payload_id), sizeof(payload_id));
        output.write(reinterpret_cast<const char*>(payload.data()), payload.size());
    }
    if (!output) {
        throw std::runtime_error("cannot write cross frame");
    }
}

struct CrossFrame {
    std::uint32_t k;
    std::uint16_t symbol_size;
    std::vector<std::pair<std::uint32_t, std::vector<std::uint8_t>>> symbols;
};

CrossFrame parse_cross_frame(const std::string& path) {
    const auto data = read_file(path);
    if (data.size() < 20 || !std::equal(data.begin(), data.begin() + 4, "RQO1") ||
        data[9] != 0 || data[12] != 1 || data[13] != 0 || data[14] != 1 ||
        data[15] != 1) {
        throw std::runtime_error("unsupported cross frame header or OTI");
    }
    const std::uint64_t transfer_length =
        static_cast<std::uint64_t>(data[4]) << 32 |
        static_cast<std::uint64_t>(read_u32(data, 5));
    const auto symbol_size = static_cast<std::uint16_t>(
        static_cast<std::uint16_t>(data[10]) << 8 | data[11]);
    if (symbol_size == 0 || transfer_length == 0 || transfer_length % symbol_size != 0) {
        throw std::runtime_error("cross frame is not a whole one-block RAW object");
    }
    const auto k64 = transfer_length / symbol_size;
    if (k64 > std::numeric_limits<std::uint32_t>::max()) {
        throw std::runtime_error("cross frame K is too large");
    }
    CrossFrame frame{static_cast<std::uint32_t>(k64), symbol_size, {}};
    const auto count = read_u32(data, 16);
    std::size_t offset = 20;
    frame.symbols.reserve(count);
    for (std::uint32_t index = 0; index < count; ++index) {
        const auto length = read_u32(data, offset);
        offset += 4;
        if (length != 4 + symbol_size || offset > data.size() ||
            data.size() - offset < length || data[offset] != 0) {
            throw std::runtime_error("unsupported cross encoding packet");
        }
        const auto esi = static_cast<std::uint32_t>(data[offset + 1]) << 16 |
                         static_cast<std::uint32_t>(data[offset + 2]) << 8 |
                         static_cast<std::uint32_t>(data[offset + 3]);
        frame.symbols.emplace_back(
            esi, std::vector<std::uint8_t>(data.begin() + offset + 4,
                                           data.begin() + offset + length));
        offset += length;
    }
    if (offset != data.size()) {
        throw std::runtime_error("trailing cross frame data");
    }
    return frame;
}

void decode_cross(const std::string& input_path, const std::string& output_path) {
    auto frame = parse_cross_frame(input_path);
    using iterator = std::vector<std::uint8_t>::iterator;
    using decoder_type = RaptorQ::Decoder<iterator, iterator>;
    decoder_type decoder(block_for(frame.k), frame.symbol_size,
                         decoder_type::Report::COMPLETE);
    if (!decoder || decoder.symbols() != frame.k) {
        throw std::runtime_error("libRaptorQ cross decoder initialization failed");
    }
    for (auto& symbol : frame.symbols) {
        auto start = symbol.second.begin();
        const auto error = decoder.add_symbol(start, symbol.second.end(), symbol.first);
        if (error != RaptorQ::Error::NONE && error != RaptorQ::Error::NOT_NEEDED) {
            throw std::runtime_error("libRaptorQ rejected a cross symbol");
        }
    }
    decoder.end_of_input(RaptorQ::Fill_With_Zeros::NO);
    const auto result = decoder.wait_sync();
    if (result.error != RaptorQ::Error::NONE) {
        throw std::runtime_error("cross frame is not decodable");
    }
    std::vector<std::uint8_t> decoded(static_cast<std::size_t>(frame.k) *
                                     frame.symbol_size);
    auto output = decoded.begin();
    const auto written = decoder.decode_bytes(output, decoded.end(), 0, 0);
    if (written.written != decoded.size()) {
        throw std::runtime_error("libRaptorQ cross decoder wrote a partial object");
    }
    write_file(output_path, decoded);
}

}  // namespace

int main(int argc, char** argv) {
    try {
        if (argc < 2) {
            std::cerr << "usage: cpp-raptorq-oracle raw-vectors|k49-vectors|"
                         "encode-cross|decode-cross ...\n";
            return 2;
        }
        const std::string command(argv[1]);
        if (argc == 2 && command == "raw-vectors") {
            raw_vectors({10u, 12u, 48u});
        } else if (argc == 2 && command == "k49-vectors") {
            raw_vectors({49u});
        } else if (argc == 7 && command == "encode-cross") {
            encode_cross(argv[2], static_cast<std::uint32_t>(std::stoul(argv[3])),
                         static_cast<std::uint16_t>(std::stoul(argv[4])), argv[5],
                         argv[6]);
        } else if (argc == 4 && command == "decode-cross") {
            decode_cross(argv[2], argv[3]);
        } else {
            std::cerr << "usage: cpp-raptorq-oracle raw-vectors|k49-vectors|"
                         "encode-cross|decode-cross ...\n";
            return 2;
        }
        return 0;
    } catch (const std::exception& error) {
        std::cerr << error.what() << '\n';
        return 1;
    }
}
