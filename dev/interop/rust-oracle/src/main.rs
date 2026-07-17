use raptorq::{
    Decoder, Encoder, EncodingPacket, ObjectTransmissionInformation, calculate_p1, deg,
    enc_indices, extended_source_block_symbols, intermediate_tuple, num_hdpc_symbols,
    num_intermediate_symbols, num_ldpc_symbols, num_lt_symbols, num_pi_symbols, rand,
    systematic_index,
};
use std::env;
use std::error::Error;
use std::fs;
use std::io::{BufWriter, Error as IoError, ErrorKind, Write};

const MAGIC: &[u8; 4] = b"RQO1";
const MAX_TRANSFER_LENGTH: u64 = 942_574_504_275;
const MAX_SOURCE_SYMBOLS_PER_BLOCK: u64 = 56_403;
const MAX_FRAME_BYTES: u64 = 1 << 30;

fn usage(program: &str) -> String {
    format!(
        "usage:\n  {program} encode-default <input> <mtu> <repair-per-block> <output>\n  \
         {program} encode-range <input> <oti-hex> <sbn> <start-esi> <count> <output>\n  \
         {program} encode-grouped-range <input> <oti-hex> <sbn> <start-esi> <count> <group> <shorten> <output>\n  \
         {program} encode-all-source <input> <oti-hex> <output>\n  \
         {program} encode-all-source-grouped <input> <oti-hex> <group> <shorten> <output>\n  \
         {program} encode-loss-repair-grouped <input> <oti-hex> <group> <repair-count> <output>\n  \
         {program} decode <frame> <output>\n  \
         {program} extended-map\n  \
         {program} primitive-vectors\n  \
         {program} raw-vectors\n  \
         {program} raw-vectors-release"
    )
}

fn invalid(message: impl Into<String>) -> Box<dyn Error> {
    Box::new(IoError::new(ErrorKind::InvalidInput, message.into()))
}

fn main() -> Result<(), Box<dyn Error>> {
    let args: Vec<String> = env::args().collect();
    if args.len() < 2 {
        return Err(usage(&args[0]).into());
    }
    match args[1].as_str() {
        "encode-default" if args.len() == 6 => encode_default(&args[2..]),
        "encode-range" if args.len() == 8 => encode_range(&args[2..]),
        "encode-grouped-range" if args.len() == 10 => encode_grouped_range(&args[2..]),
        "encode-all-source" if args.len() == 5 => encode_all_source(&args[2..]),
        "encode-all-source-grouped" if args.len() == 7 => encode_all_source_grouped(&args[2..]),
        "encode-loss-repair-grouped" if args.len() == 7 => encode_loss_repair_grouped(&args[2..]),
        "decode" if args.len() == 4 => decode(&args[2..]),
        "extended-map" if args.len() == 2 => extended_map(),
        "primitive-vectors" if args.len() == 2 => primitive_vectors(),
        "raw-vectors" if args.len() == 2 => raw_vectors(),
        "raw-vectors-release" if args.len() == 2 => raw_vectors_release(),
        _ => Err(usage(&args[0]).into()),
    }
}

fn raw_vectors() -> Result<(), Box<dyn Error>> {
    let stdout = std::io::stdout();
    let mut output = BufWriter::new(stdout.lock());
    for k in [1u32, 10, 11, 12, 13, 47, 48, 49, 50] {
        for symbol_size in [1u16, 7, 31, 255] {
            write_raw_vectors(&mut output, k, symbol_size)?;
        }
    }
    output.flush()?;
    Ok(())
}

fn raw_vectors_release() -> Result<(), Box<dyn Error>> {
    let stdout = std::io::stdout();
    let mut output = BufWriter::new(stdout.lock());
    for k in [1_024u32, 1_698, 5_225, 8_837, 50_511, 55_844, 56_403] {
        write_raw_vectors(&mut output, k, 1)?;
    }
    for (k, symbol_size) in [(1_024u32, 7u16), (55_844, 7), (56_403, 7), (1_024, 255)] {
        write_raw_vectors(&mut output, k, symbol_size)?;
    }
    output.flush()?;
    Ok(())
}

fn write_raw_vectors(
    output: &mut impl Write,
    k: u32,
    symbol_size: u16,
) -> Result<(), Box<dyn Error>> {
    let mut data = vec![0u8; usize::try_from(k)? * usize::from(symbol_size)];
    for symbol in 0..k {
        for index in 0..u32::from(symbol_size) {
            data[usize::try_from(symbol * u32::from(symbol_size) + index)?] = symbol
                .wrapping_mul(131)
                .wrapping_add(index * 37)
                .wrapping_add(19)
                as u8;
        }
    }
    let oti = ObjectTransmissionInformation::new(u64::try_from(data.len())?, symbol_size, 1, 1, 1);
    let encoder = Encoder::new(&data, oti);
    let block = &encoder.get_block_encoders()[0];
    let source = block.source_packets();
    let mut symbol_ids = vec![0, k - 1, k, k + 1, k + 17, (1 << 24) - 1];
    symbol_ids.dedup();
    for esi in symbol_ids {
        let payload = if esi < k {
            source[usize::try_from(esi)?].data()
        } else {
            let packet = block.repair_packets(esi - k, 1).remove(0);
            writeln!(output, "s {k} {symbol_size} {esi} {}", hex(packet.data()))?;
            continue;
        };
        writeln!(output, "s {k} {symbol_size} {esi} {}", hex(payload))?;
    }
    Ok(())
}

fn hex(bytes: &[u8]) -> String {
    const DIGITS: &[u8; 16] = b"0123456789abcdef";
    let mut encoded = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        encoded.push(DIGITS[usize::from(byte >> 4)] as char);
        encoded.push(DIGITS[usize::from(byte & 0x0f)] as char);
    }
    encoded
}

fn primitive_vectors() -> Result<(), Box<dyn Error>> {
    let stdout = std::io::stdout();
    let mut output = BufWriter::with_capacity(1 << 20, stdout.lock());
    write_parameters_and_tuples(&mut output)?;
    write_random_vectors(&mut output)?;
    write_degree_vectors(&mut output)?;
    output.flush()?;
    Ok(())
}

fn write_parameters_and_tuples(output: &mut impl Write) -> Result<(), Box<dyn Error>> {
    let mut last_k_prime = 0;
    for k in 1..=u32::try_from(MAX_SOURCE_SYMBOLS_PER_BLOCK)? {
        let k_prime = extended_source_block_symbols(k);
        let last_source = k - 1;
        let first_repair = k_prime;
        let last_repair = (1u32 << 24) - 1 + k_prime - k;
        writeln!(
            output,
            "k {k} {k_prime} 0 {last_source} {first_repair} {last_repair}"
        )?;
        if k_prime == last_k_prime {
            continue;
        }
        last_k_prime = k_prime;
        let j = systematic_index(k_prime);
        let s = num_ldpc_symbols(k_prime);
        let h = num_hdpc_symbols(k_prime);
        let w = num_lt_symbols(k_prime);
        let l = num_intermediate_symbols(k_prime);
        let p = num_pi_symbols(k_prime);
        let p1 = calculate_p1(k_prime);
        writeln!(
            output,
            "p {k_prime} {j} {s} {h} {w} {l} {p} {p1} {} {}",
            w - s,
            p - h
        )?;
        for x in tuple_ids(k_prime) {
            let tuple @ (d, a, b, d1, a1, b1) = intermediate_tuple(x, w, j, p1);
            writeln!(output, "t {k_prime} {x} {d} {a} {b} {d1} {a1} {b1}")?;
            let mut indices = Vec::with_capacity((d + d1) as usize);
            enc_indices(tuple, w, p, p1, |index| indices.push(index));
            write!(output, "e {k_prime} {x}")?;
            for index in indices {
                write!(output, " {index}")?;
            }
            writeln!(output)?;
        }
    }
    Ok(())
}

fn tuple_ids(k_prime: u32) -> [u32; 7] {
    [
        0,
        k_prime - 1,
        k_prime,
        (1 << 24) - 1,
        u32::MAX,
        k_prime.wrapping_mul(2_654_435_761),
        k_prime
            .wrapping_mul(2_246_822_519)
            .wrapping_add(3_266_489_917),
    ]
}

fn write_random_vectors(output: &mut impl Write) -> Result<(), Box<dyn Error>> {
    let explicit = [
        (0, 0, u32::MAX),
        (1, 1, 1 << 20),
        (0x0123_4567, 89, 56_951),
        (u32::MAX, u32::from(u8::MAX), 379),
        (0xdead_beef, 17, 1),
    ];
    for (y, i, modulus) in explicit {
        write_random(output, y, i as u8, modulus)?;
    }
    let moduli = [1, 2, 3, 11, 251, 256, 379, 56_951, 1 << 20, u32::MAX];
    for index in 0u32..1 << 16 {
        let y = index.wrapping_mul(0x9e37_79b9).wrapping_add(0x7f4a_7c15);
        let i = index.wrapping_mul(73).wrapping_add(19) as u8;
        write_random(output, y, i, moduli[index as usize % moduli.len()])?;
    }
    Ok(())
}

fn write_random(
    output: &mut impl Write,
    y: u32,
    i: u8,
    modulus: u32,
) -> Result<(), Box<dyn Error>> {
    writeln!(output, "r {y} {i} {modulus} {}", rand(y, i, modulus))?;
    Ok(())
}

fn write_degree_vectors(output: &mut impl Write) -> Result<(), Box<dyn Error>> {
    for v in 0..1 << 20 {
        writeln!(
            output,
            "d {v} {} {} {} {}",
            deg(v, 17),
            deg(v, 19),
            deg(v, 61),
            deg(v, 56_951)
        )?;
    }
    Ok(())
}

fn extended_map() -> Result<(), Box<dyn Error>> {
    let stdout = std::io::stdout();
    let mut output = BufWriter::new(stdout.lock());
    for k in 1..=u32::try_from(MAX_SOURCE_SYMBOLS_PER_BLOCK)? {
        writeln!(output, "{k} {}", extended_source_block_symbols(k))?;
    }
    output.flush()?;
    Ok(())
}

fn read_nonempty(path: &str) -> Result<Vec<u8>, Box<dyn Error>> {
    let input = fs::read(path)?;
    if input.is_empty() {
        return Err(invalid("input object must not be empty"));
    }
    Ok(input)
}

fn encode_default(args: &[String]) -> Result<(), Box<dyn Error>> {
    let input = read_nonempty(&args[0])?;
    let mtu: u16 = args[1].parse()?;
    if mtu == 0 {
        return Err(invalid("MTU must be positive"));
    }
    let repair_packets: u32 = args[2].parse()?;
    let encoder = Encoder::with_defaults(&input, mtu);
    write_frame(
        &args[3],
        encoder.get_config(),
        encoder.get_encoded_packets(repair_packets),
    )
}

fn encode_range(args: &[String]) -> Result<(), Box<dyn Error>> {
    let input = read_nonempty(&args[0])?;
    let oti = parse_oti(&args[1])?;
    if oti.transfer_length() != input.len() as u64 {
        return Err(invalid("OTI transfer length does not match input"));
    }
    let source_block: usize = args[2].parse()?;
    let start_esi: u32 = args[3].parse()?;
    let count: u32 = args[4].parse()?;
    if count == 0 {
        return Err(invalid("packet count must be positive"));
    }
    let last_esi = start_esi
        .checked_add(count - 1)
        .ok_or_else(|| invalid("ESI range overflow"))?;
    if last_esi >= 1 << 24 {
        return Err(invalid("ESI range exceeds 24 bits"));
    }

    let encoder = Encoder::new(&input, oti);
    let block = encoder
        .get_block_encoders()
        .get(source_block)
        .ok_or_else(|| invalid("source block is out of range"))?;
    let source_packets = block.source_packets();
    let k = u32::try_from(source_packets.len())?;
    let mut packets = Vec::with_capacity(usize::try_from(count)?);
    for esi in start_esi..=last_esi {
        if esi < k {
            packets.push(source_packets[usize::try_from(esi)?].clone());
        } else {
            packets.push(block.repair_packets(esi - k, 1).remove(0));
        }
    }
    write_frame(&args[5], oti, packets)
}

fn encode_all_source(args: &[String]) -> Result<(), Box<dyn Error>> {
    let input = read_nonempty(&args[0])?;
    let oti = parse_oti(&args[1])?;
    if oti.transfer_length() != input.len() as u64 {
        return Err(invalid("OTI transfer length does not match input"));
    }
    let encoder = Encoder::new(&input, oti);
    let mut packets = Vec::new();
    for block in encoder.get_block_encoders() {
        packets.extend(block.source_packets());
    }
    write_frame(&args[2], oti, packets)
}

fn encode_all_source_grouped(args: &[String]) -> Result<(), Box<dyn Error>> {
    let input = read_nonempty(&args[0])?;
    let oti = parse_oti(&args[1])?;
    if oti.transfer_length() != input.len() as u64 {
        return Err(invalid("OTI transfer length does not match input"));
    }
    let group = parse_positive_usize(&args[2], "group")?;
    let shorten: bool = args[3].parse()?;
    let encoder = Encoder::new(&input, oti);
    let mut packets = Vec::new();
    for block in encoder.get_block_encoders() {
        packets.extend(group_packets(oti, block.source_packets(), group, shorten)?);
    }
    write_serialized_frame(&args[4], oti, packets)
}

fn encode_loss_repair_grouped(args: &[String]) -> Result<(), Box<dyn Error>> {
    let input = read_nonempty(&args[0])?;
    let oti = parse_oti(&args[1])?;
    if oti.transfer_length() != input.len() as u64 {
        return Err(invalid("OTI transfer length does not match input"));
    }
    let group = parse_positive_usize(&args[2], "group")?;
    let repair_count: u32 = args[3].parse()?;
    if repair_count == 0 {
        return Err(invalid("repair count must be positive"));
    }

    let encoder = Encoder::new(&input, oti);
    let mut packets = Vec::new();
    for block in encoder.get_block_encoders() {
        let source_packets = block.source_packets();
        if source_packets.len() < 2 {
            return Err(invalid(
                "loss/shortening corpus requires at least two source symbols per block",
            ));
        }
        packets.extend(group_packets(
            oti,
            source_packets.into_iter().skip(1).collect(),
            group,
            true,
        )?);
        packets.extend(group_packets(
            oti,
            block.repair_packets(0, repair_count),
            group,
            false,
        )?);
    }
    write_serialized_frame(&args[4], oti, packets)
}

fn encode_grouped_range(args: &[String]) -> Result<(), Box<dyn Error>> {
    let input = read_nonempty(&args[0])?;
    let oti = parse_oti(&args[1])?;
    if oti.transfer_length() != input.len() as u64 {
        return Err(invalid("OTI transfer length does not match input"));
    }
    let source_block: usize = args[2].parse()?;
    let start_esi: u32 = args[3].parse()?;
    let count: u32 = args[4].parse()?;
    let group = parse_positive_usize(&args[5], "group")?;
    let shorten: bool = args[6].parse()?;
    if count == 0 {
        return Err(invalid("packet count must be positive"));
    }
    let last_esi = start_esi
        .checked_add(count - 1)
        .ok_or_else(|| invalid("ESI range overflow"))?;
    if last_esi >= 1 << 24 {
        return Err(invalid("ESI range exceeds 24 bits"));
    }

    let encoder = Encoder::new(&input, oti);
    let block = encoder
        .get_block_encoders()
        .get(source_block)
        .ok_or_else(|| invalid("source block is out of range"))?;
    let source_packets = block.source_packets();
    let k = u32::try_from(source_packets.len())?;
    if start_esi < k && last_esi >= k {
        return Err(invalid("grouped range crosses source/repair boundary"));
    }
    let mut packets = Vec::with_capacity(usize::try_from(count)?);
    for esi in start_esi..=last_esi {
        if esi < k {
            packets.push(source_packets[usize::try_from(esi)?].clone());
        } else {
            packets.push(block.repair_packets(esi - k, 1).remove(0));
        }
    }
    let grouped = group_packets(oti, packets, group, shorten)?;
    write_serialized_frame(&args[7], oti, grouped)
}

fn parse_positive_usize(value: &str, name: &str) -> Result<usize, Box<dyn Error>> {
    let parsed: usize = value.parse()?;
    if parsed == 0 {
        return Err(invalid(format!("{name} must be positive")));
    }
    Ok(parsed)
}

fn group_packets(
    oti: ObjectTransmissionInformation,
    packets: Vec<EncodingPacket>,
    group: usize,
    shorten: bool,
) -> Result<Vec<Vec<u8>>, Box<dyn Error>> {
    let parameters = OperationalOti::from_oti(oti)?;
    let mut grouped = Vec::with_capacity(packets.len().div_ceil(group));
    for chunk in packets.chunks(group) {
        let first = chunk[0].serialize();
        let source_block = first[0];
        let first_esi = decode_esi(&first[..4]);
        let last_esi = first_esi
            .checked_add(u32::try_from(chunk.len() - 1)?)
            .ok_or_else(|| invalid("group ESI overflow"))?;
        if last_esi >= 1 << 24 {
            return Err(invalid("group ESI exceeds 24 bits"));
        }
        let k = parameters.source_block_symbols(source_block)?;
        if first_esi < k && last_esi >= k {
            return Err(invalid("group crosses source/repair boundary"));
        }
        let mut serialized = Vec::with_capacity(4 + chunk.len() * parameters.symbol_size);
        serialized.extend_from_slice(&first[..4]);
        for (index, packet) in chunk.iter().enumerate() {
            let packet = packet.serialize();
            if packet.len() != 4 + parameters.symbol_size || packet[0] != source_block {
                return Err(invalid("encoder returned an unexpected packet shape"));
            }
            let expected = first_esi + u32::try_from(index)?;
            if decode_esi(&packet[..4]) != expected {
                return Err(invalid("encoder returned nonconsecutive packets"));
            }
            serialized.extend_from_slice(&packet[4..]);
        }
        if shorten && first_esi < k {
            let omitted = parameters.trailing_source_padding(source_block, last_esi)?;
            serialized.truncate(serialized.len() - omitted);
        }
        grouped.push(serialized);
    }
    Ok(grouped)
}

fn decode(args: &[String]) -> Result<(), Box<dyn Error>> {
    let (oti, packets) = read_frame(&args[0])?;
    let mut decoder = Decoder::new(oti);
    let mut result = None;
    for packet in packets {
        if let Some(decoded) = decoder.decode(packet) {
            result = Some(decoded);
            break;
        }
    }
    let decoded = result.ok_or_else(|| invalid("frame does not contain a decodable set"))?;
    fs::write(&args[1], decoded)?;
    Ok(())
}

fn parse_oti(encoded: &str) -> Result<ObjectTransmissionInformation, Box<dyn Error>> {
    if encoded.len() != 24 {
        return Err(invalid("OTI hex must encode exactly 12 octets"));
    }
    let mut bytes = [0u8; 12];
    for (index, byte) in bytes.iter_mut().enumerate() {
        *byte = u8::from_str_radix(&encoded[index * 2..index * 2 + 2], 16)?;
    }
    validate_operational_oti(&bytes)?;
    Ok(ObjectTransmissionInformation::deserialize(&bytes))
}

fn write_frame(
    path: &str,
    oti: ObjectTransmissionInformation,
    packets: Vec<EncodingPacket>,
) -> Result<(), Box<dyn Error>> {
    let packet_count = u32::try_from(packets.len())?;
    let mut output = BufWriter::new(fs::File::create(path)?);
    output.write_all(MAGIC)?;
    output.write_all(&oti.serialize())?;
    output.write_all(&packet_count.to_be_bytes())?;
    for packet in packets {
        let serialized = packet.serialize();
        let packet_length = u32::try_from(serialized.len())?;
        output.write_all(&packet_length.to_be_bytes())?;
        output.write_all(&serialized)?;
    }
    output.flush()?;
    Ok(())
}

fn write_serialized_frame(
    path: &str,
    oti: ObjectTransmissionInformation,
    packets: Vec<Vec<u8>>,
) -> Result<(), Box<dyn Error>> {
    let packet_count = u32::try_from(packets.len())?;
    let mut output = BufWriter::new(fs::File::create(path)?);
    output.write_all(MAGIC)?;
    output.write_all(&oti.serialize())?;
    output.write_all(&packet_count.to_be_bytes())?;
    for packet in packets {
        let packet_length = u32::try_from(packet.len())?;
        output.write_all(&packet_length.to_be_bytes())?;
        output.write_all(&packet)?;
    }
    output.flush()?;
    Ok(())
}

fn read_frame(
    path: &str,
) -> Result<(ObjectTransmissionInformation, Vec<EncodingPacket>), Box<dyn Error>> {
    let metadata = fs::metadata(path)?;
    if metadata.len() > MAX_FRAME_BYTES {
        return Err(invalid("oracle frame exceeds the size limit"));
    }
    let data = fs::read(path)?;
    parse_frame(&data)
}

fn parse_frame(
    data: &[u8],
) -> Result<(ObjectTransmissionInformation, Vec<EncodingPacket>), Box<dyn Error>> {
    if data.len() < 20 || &data[..4] != MAGIC {
        return Err(invalid("invalid oracle frame header"));
    }
    let oti_bytes: [u8; 12] = data[4..16].try_into()?;
    validate_operational_oti(&oti_bytes)?;
    let oti = ObjectTransmissionInformation::deserialize(&oti_bytes);
    let packet_count = u32::from_be_bytes(data[16..20].try_into()?);
    let packet_count = usize::try_from(packet_count)?;
    if packet_count > (data.len() - 20) / 9 {
        return Err(invalid("packet count exceeds the possible frame entries"));
    }
    let mut offset = 20usize;
    let mut packets = Vec::new();
    packets
        .try_reserve_exact(packet_count)
        .map_err(|_| invalid("packet count exceeds allocation limits"))?;
    let parameters = OperationalOti::from_bytes(&oti_bytes)?;
    for _ in 0..packet_count {
        let length_end = offset
            .checked_add(4)
            .ok_or_else(|| invalid("frame offset overflow"))?;
        if length_end > data.len() {
            return Err(invalid("truncated packet length"));
        }
        let length = u32::from_be_bytes(data[offset..length_end].try_into()?);
        offset = length_end;
        let packet_end = offset
            .checked_add(usize::try_from(length)?)
            .ok_or_else(|| invalid("frame offset overflow"))?;
        if packet_end > data.len() {
            return Err(invalid("truncated encoding packet"));
        }
        normalize_packet(&data[offset..packet_end], &parameters, &mut packets)?;
        offset = packet_end;
    }
    if offset != data.len() {
        return Err(invalid("trailing bytes after oracle frame"));
    }
    Ok((oti, packets))
}

#[derive(Clone, Copy)]
struct OperationalOti {
    transfer_length: u64,
    symbol_size: usize,
    source_blocks: u8,
    sub_blocks: u16,
    symbol_alignment: u8,
}

impl OperationalOti {
    fn from_oti(oti: ObjectTransmissionInformation) -> Result<Self, Box<dyn Error>> {
        Self::from_bytes(&oti.serialize())
    }

    fn from_bytes(bytes: &[u8; 12]) -> Result<Self, Box<dyn Error>> {
        validate_operational_oti(bytes)?;
        Ok(Self {
            transfer_length: u64::from(bytes[0]) << 32
                | u64::from(u32::from_be_bytes(bytes[1..5].try_into()?)),
            symbol_size: usize::from(u16::from_be_bytes(bytes[6..8].try_into()?)),
            source_blocks: bytes[8],
            sub_blocks: u16::from_be_bytes(bytes[9..11].try_into()?),
            symbol_alignment: bytes[11],
        })
    }

    fn source_block_symbols(&self, source_block: u8) -> Result<u32, Box<dyn Error>> {
        if source_block >= self.source_blocks {
            return Err(invalid("encoding packet source block is out of range"));
        }
        let total_symbols = self.transfer_length.div_ceil(self.symbol_size as u64);
        let symbols = partition_size(
            total_symbols,
            u64::from(self.source_blocks),
            u64::from(source_block),
        )?;
        Ok(u32::try_from(symbols)?)
    }

    fn trailing_source_padding(&self, source_block: u8, esi: u32) -> Result<usize, Box<dyn Error>> {
        let k = self.source_block_symbols(source_block)?;
        if esi >= k {
            return Err(invalid("source ESI is outside its source block"));
        }
        let total_symbols = self.transfer_length.div_ceil(self.symbol_size as u64);
        let total_padding = total_symbols * self.symbol_size as u64 - self.transfer_length;
        if total_padding == 0 || source_block + 1 != self.source_blocks {
            return Ok(0);
        }

        let sub_symbol_units = self.symbol_size as u64 / u64::from(self.symbol_alignment);
        let mut remaining = total_padding;
        let mut common = 0u64;
        let mut boundary_size = 0u64;
        let mut boundary_padding = 0u64;
        for sub_block in (0..u64::from(self.sub_blocks)).rev() {
            if remaining == 0 {
                break;
            }
            let size = partition_size(sub_symbol_units, u64::from(self.sub_blocks), sub_block)?
                * u64::from(self.symbol_alignment);
            let sub_block_bytes = u64::from(k) * size;
            if remaining >= sub_block_bytes {
                common += size;
                remaining -= sub_block_bytes;
            } else {
                boundary_size = size;
                boundary_padding = remaining;
                remaining = 0;
            }
        }
        if remaining != 0 || common >= self.symbol_size as u64 {
            return Err(invalid("invalid source padding layout"));
        }
        let mut padding = common;
        if boundary_padding != 0 {
            let boundary_bytes = u64::from(k) * boundary_size;
            let padding_start = boundary_bytes - boundary_padding;
            let segment_start = u64::from(esi) * boundary_size;
            let segment_end = segment_start + boundary_size;
            if segment_end > padding_start {
                padding += segment_end - segment_start.max(padding_start);
            }
        }
        Ok(usize::try_from(padding)?)
    }
}

fn partition_size(total: u64, parts: u64, index: u64) -> Result<u64, Box<dyn Error>> {
    if parts == 0 || index >= parts {
        return Err(invalid("invalid partition index"));
    }
    let small = total / parts;
    let large_count = total - small * parts;
    Ok(if index < large_count {
        small + 1
    } else {
        small
    })
}

fn decode_esi(payload_id: &[u8]) -> u32 {
    u32::from(payload_id[1]) << 16 | u32::from(payload_id[2]) << 8 | u32::from(payload_id[3])
}

fn normalize_packet(
    serialized: &[u8],
    parameters: &OperationalOti,
    packets: &mut Vec<EncodingPacket>,
) -> Result<(), Box<dyn Error>> {
    if serialized.len() < 5 {
        return Err(invalid("encoding packet is too short"));
    }
    let source_block = serialized[0];
    let first_esi = decode_esi(&serialized[..4]);
    let k = parameters.source_block_symbols(source_block)?;
    let payload = &serialized[4..];
    let symbol_count = payload.len().div_ceil(parameters.symbol_size);
    let last_esi = first_esi
        .checked_add(u32::try_from(symbol_count - 1)?)
        .ok_or_else(|| invalid("grouped ESI range overflow"))?;
    if last_esi >= 1 << 24 {
        return Err(invalid("grouped ESI range exceeds 24 bits"));
    }
    let source = first_esi < k;
    if source && last_esi >= k {
        return Err(invalid("packet crosses source/repair boundary"));
    }
    let remainder = payload.len() % parameters.symbol_size;
    if !source && remainder != 0 {
        return Err(invalid("repair packet contains a partial symbol"));
    }
    if source && remainder != 0 {
        let omitted = parameters.symbol_size - remainder;
        let padding = parameters.trailing_source_padding(source_block, last_esi)?;
        if padding == 0 || omitted > padding {
            return Err(invalid("illegal shortened source symbol"));
        }
    }
    if source {
        for index in 0..symbol_count {
            let esi = first_esi + u32::try_from(index)?;
            let padding = parameters.trailing_source_padding(source_block, esi)?;
            let transmitted = if index + 1 == symbol_count && remainder != 0 {
                remainder
            } else {
                parameters.symbol_size
            };
            let padding_start = parameters.symbol_size - padding;
            if transmitted > padding_start {
                let start = index * parameters.symbol_size + padding_start;
                let end = index * parameters.symbol_size + transmitted;
                if payload[start..end].iter().any(|value| *value != 0) {
                    return Err(invalid("source symbol has nonzero FEC padding"));
                }
            }
        }
    }

    packets
        .try_reserve(symbol_count)
        .map_err(|_| invalid("normalized packet count exceeds allocation limits"))?;
    for index in 0..symbol_count {
        let esi = first_esi + u32::try_from(index)?;
        let start = index * parameters.symbol_size;
        let end = (start + parameters.symbol_size).min(payload.len());
        let mut packet = Vec::with_capacity(4 + parameters.symbol_size);
        packet.extend_from_slice(&[source_block, (esi >> 16) as u8, (esi >> 8) as u8, esi as u8]);
        packet.extend_from_slice(&payload[start..end]);
        packet.resize(4 + parameters.symbol_size, 0);
        packets.push(EncodingPacket::deserialize(&packet));
    }
    Ok(())
}

fn validate_operational_oti(bytes: &[u8; 12]) -> Result<(), Box<dyn Error>> {
    let transfer_length =
        u64::from(bytes[0]) << 32 | u64::from(u32::from_be_bytes(bytes[1..5].try_into()?));
    let symbol_size = u64::from(u16::from_be_bytes(bytes[6..8].try_into()?));
    let source_blocks = u64::from(bytes[8]);
    let sub_blocks = u64::from(u16::from_be_bytes(bytes[9..11].try_into()?));
    let symbol_alignment = u64::from(bytes[11]);
    if transfer_length == 0 || transfer_length > MAX_TRANSFER_LENGTH {
        return Err(invalid("oracle requires an RFC-sized nonempty object"));
    }
    if symbol_size == 0 || source_blocks == 0 || sub_blocks == 0 || symbol_alignment == 0 {
        return Err(invalid("OTI size and partition fields must be positive"));
    }
    if symbol_size % symbol_alignment != 0 {
        return Err(invalid("OTI symbol size is not alignment-divisible"));
    }
    let total_symbols = transfer_length.div_ceil(symbol_size);
    if total_symbols < source_blocks {
        return Err(invalid("oracle does not support empty source blocks"));
    }
    if total_symbols.div_ceil(source_blocks) > MAX_SOURCE_SYMBOLS_PER_BLOCK {
        return Err(invalid("OTI source block exceeds the RFC symbol limit"));
    }
    if sub_blocks > symbol_size / symbol_alignment {
        return Err(invalid("oracle does not support empty sub-blocks"));
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn valid_header() -> Vec<u8> {
        let mut frame = Vec::from(MAGIC.as_slice());
        frame.extend_from_slice(&[0, 0, 0, 0, 1, 0, 0, 1, 1, 0, 1, 1]);
        frame.extend_from_slice(&0u32.to_be_bytes());
        frame
    }

    #[test]
    fn rejects_impossible_packet_count_before_reserve() {
        let mut frame = valid_header();
        frame[16..20].copy_from_slice(&u32::MAX.to_be_bytes());
        let error = parse_frame(&frame).unwrap_err().to_string();
        assert!(error.contains("packet count exceeds"), "{error}");
    }

    #[test]
    fn rejects_zero_oti_without_panicking() {
        let mut frame = Vec::from(MAGIC.as_slice());
        frame.extend_from_slice(&[0; 12]);
        frame.extend_from_slice(&0u32.to_be_bytes());
        let error = parse_frame(&frame).unwrap_err().to_string();
        assert!(error.contains("nonempty object"), "{error}");
    }

    #[test]
    fn rejects_out_of_range_source_block() {
        let mut frame = valid_header();
        frame[16..20].copy_from_slice(&1u32.to_be_bytes());
        frame.extend_from_slice(&5u32.to_be_bytes());
        frame.extend_from_slice(&[1, 0, 0, 0, 7]);
        let error = parse_frame(&frame).unwrap_err().to_string();
        assert!(error.contains("source block is out of range"), "{error}");
    }

    #[test]
    fn normalizes_grouped_shortened_n_greater_than_one_source() {
        let oti = ObjectTransmissionInformation::new(13, 5, 1, 3, 1);
        let parameters = OperationalOti::from_oti(oti).unwrap();
        let serialized = [0, 0, 0, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9];
        let mut packets = Vec::new();
        normalize_packet(&serialized, &parameters, &mut packets).unwrap();
        assert_eq!(packets.len(), 2);
        assert_eq!(packets[0].data(), &[1, 2, 3, 4, 5]);
        assert_eq!(packets[1].data(), &[6, 7, 8, 9, 0]);
    }

    #[test]
    fn rejects_nonzero_distributed_source_padding() {
        let oti = ObjectTransmissionInformation::new(13, 5, 1, 3, 1);
        let parameters = OperationalOti::from_oti(oti).unwrap();
        let serialized = [0, 0, 0, 1, 6, 7, 8, 9, 1];
        let mut packets = Vec::new();
        let error = normalize_packet(&serialized, &parameters, &mut packets)
            .unwrap_err()
            .to_string();
        assert!(error.contains("nonzero FEC padding"), "{error}");
    }
}
