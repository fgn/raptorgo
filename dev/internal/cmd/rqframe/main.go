package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/fgn/raptorgo"
)

const (
	maxFrameBytes   = 1 << 30
	maxFramePackets = 1 << 20
)

func main() {
	if len(os.Args) < 2 {
		fatal(errors.New("usage: rqframe encode-range|encode-pattern|encode-grouped-range|encode-all-source|encode-all-source-grouped|encode-loss-repair-grouped|decode ..."))
	}
	var err error
	switch os.Args[1] {
	case "encode-range":
		if len(os.Args) != 8 {
			err = errors.New("usage: rqframe encode-range <input> <oti-hex> <sbn> <start-esi> <count> <output>")
		} else {
			err = encodeRange(os.Args[2:])
		}
	case "encode-pattern":
		if len(os.Args) != 7 {
			err = errors.New("usage: rqframe encode-pattern <input> <oti-hex> <sbn> <repair|mixed|deficient> <output>")
		} else {
			err = encodePattern(os.Args[2:])
		}
	case "encode-all-source":
		if len(os.Args) != 5 {
			err = errors.New("usage: rqframe encode-all-source <input> <oti-hex> <output>")
		} else {
			err = encodeAllSource(os.Args[2:])
		}
	case "encode-all-source-grouped":
		if len(os.Args) != 7 {
			err = errors.New("usage: rqframe encode-all-source-grouped <input> <oti-hex> <group> <shorten> <output>")
		} else {
			err = encodeAllSourceGrouped(os.Args[2:])
		}
	case "encode-loss-repair-grouped":
		if len(os.Args) != 7 {
			err = errors.New("usage: rqframe encode-loss-repair-grouped <input> <oti-hex> <group> <repair-count> <output>")
		} else {
			err = encodeLossRepairGrouped(os.Args[2:])
		}
	case "encode-grouped-range":
		if len(os.Args) != 10 {
			err = errors.New("usage: rqframe encode-grouped-range <input> <oti-hex> <sbn> <start-esi> <count> <group> <shorten> <output>")
		} else {
			err = encodeGroupedRange(os.Args[2:])
		}
	case "decode":
		if len(os.Args) != 4 {
			err = errors.New("usage: rqframe decode <frame> <output>")
		} else {
			err = decodeFrame(os.Args[2], os.Args[3])
		}
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fatal(err)
	}
}

func encodePattern(arguments []string) error {
	input, oti, err := readInputAndOTI(arguments[0], arguments[1])
	if err != nil {
		return err
	}
	sourceBlock, err := parseUint(arguments[2], 8)
	if err != nil || sourceBlock >= uint64(oti.SourceBlocks) {
		return errors.New("invalid source block")
	}
	k, err := raptorgo.SourceBlockSymbols(oti, uint8(sourceBlock))
	if err != nil {
		return err
	}
	esis, err := patternESIs(k, arguments[3])
	if err != nil {
		return err
	}
	encoder, err := raptorgo.NewObjectEncoder(input, oti, raptorgo.DefaultLimits())
	if err != nil {
		return err
	}
	packets := make([][]byte, 0, len(esis))
	for _, esi := range esis {
		packet, packetErr := encoder.Packet(uint8(sourceBlock), esi, 1, false)
		if packetErr != nil {
			return packetErr
		}
		packets = append(packets, packet)
	}
	return writeFrame(arguments[4], oti, packets)
}

func patternESIs(k uint32, pattern string) ([]uint32, error) {
	switch pattern {
	case "repair":
		esis := make([]uint32, 0, k+4)
		for esi := k; esi < 2*k+4; esi++ {
			esis = append(esis, esi)
		}
		return esis, nil
	case "mixed":
		dropped := map[uint32]bool{1: true, k / 2: true, k - 1: true}
		esis := make([]uint32, 0, k+8)
		for esi := k; esi > 0; esi-- {
			candidate := esi - 1
			if !dropped[candidate] {
				esis = append(esis, candidate)
			}
		}
		for esi := k + 8; esi > k; esi-- {
			esis = append(esis, esi-1)
		}
		if len(esis) > 0 {
			esis = append(esis, esis[0])
		}
		return esis, nil
	case "deficient":
		esis := make([]uint32, 0, k-1)
		for esi := uint32(0); esi+1 < k; esi++ {
			esis = append(esis, esi)
		}
		return esis, nil
	default:
		return nil, fmt.Errorf("unknown packet pattern %q", pattern)
	}
}

func encodeAllSourceGrouped(arguments []string) error {
	input, oti, err := readInputAndOTI(arguments[0], arguments[1])
	if err != nil {
		return err
	}
	group, err := parsePositiveUint32(arguments[2], "group")
	if err != nil {
		return err
	}
	shorten, err := strconv.ParseBool(arguments[3])
	if err != nil {
		return fmt.Errorf("parse shorten: %w", err)
	}
	encoder, err := raptorgo.NewObjectEncoder(input, oti, raptorgo.DefaultLimits())
	if err != nil {
		return err
	}
	var packets [][]byte
	for sourceBlock := uint8(0); sourceBlock < oti.SourceBlocks; sourceBlock++ {
		k, symbolsErr := raptorgo.SourceBlockSymbols(oti, sourceBlock)
		if symbolsErr != nil {
			return symbolsErr
		}
		blockPackets, packetErr := groupedRange(encoder, sourceBlock, 0, k, group, shorten)
		if packetErr != nil {
			return packetErr
		}
		packets = append(packets, blockPackets...)
	}
	return writeFrame(arguments[4], oti, packets)
}

// encodeLossRepairGrouped emits a deterministic object-level erasure corpus.
// ESI zero is omitted from every nonempty block, remaining source symbols are
// grouped and legally shortened, and grouped repairs complete each block.
func encodeLossRepairGrouped(arguments []string) error {
	input, oti, err := readInputAndOTI(arguments[0], arguments[1])
	if err != nil {
		return err
	}
	group, err := parsePositiveUint32(arguments[2], "group")
	if err != nil {
		return err
	}
	repairCount, err := parsePositiveUint32(arguments[3], "repair count")
	if err != nil {
		return err
	}
	encoder, err := raptorgo.NewObjectEncoder(input, oti, raptorgo.DefaultLimits())
	if err != nil {
		return err
	}
	var packets [][]byte
	for sourceBlock := uint8(0); sourceBlock < oti.SourceBlocks; sourceBlock++ {
		k, symbolsErr := raptorgo.SourceBlockSymbols(oti, sourceBlock)
		if symbolsErr != nil {
			return symbolsErr
		}
		if k < 2 {
			return fmt.Errorf("source block %d has K=%d; loss/shortening corpus requires K >= 2", sourceBlock, k)
		}
		sourcePackets, packetErr := groupedRange(encoder, sourceBlock, 1, k-1, group, true)
		if packetErr != nil {
			return packetErr
		}
		repairPackets, packetErr := groupedRange(encoder, sourceBlock, k, repairCount, group, false)
		if packetErr != nil {
			return packetErr
		}
		packets = append(packets, sourcePackets...)
		packets = append(packets, repairPackets...)
	}
	return writeFrame(arguments[4], oti, packets)
}

func encodeGroupedRange(arguments []string) error {
	input, oti, err := readInputAndOTI(arguments[0], arguments[1])
	if err != nil {
		return err
	}
	sourceBlock, err := parseUint(arguments[2], 8)
	if err != nil {
		return err
	}
	startESI, err := parseUint(arguments[3], 32)
	if err != nil {
		return err
	}
	count, err := parsePositiveUint32(arguments[4], "count")
	if err != nil {
		return err
	}
	group, err := parsePositiveUint32(arguments[5], "group")
	if err != nil {
		return err
	}
	shorten, err := strconv.ParseBool(arguments[6])
	if err != nil {
		return fmt.Errorf("parse shorten: %w", err)
	}
	if startESI+uint64(count)-1 > uint64(raptorgo.MaxEncodingSymbolID) {
		return errors.New("invalid ESI range")
	}
	encoder, err := raptorgo.NewObjectEncoder(input, oti, raptorgo.DefaultLimits())
	if err != nil {
		return err
	}
	packets, err := groupedRange(encoder, uint8(sourceBlock), uint32(startESI), count, group, shorten)
	if err != nil {
		return err
	}
	return writeFrame(arguments[7], oti, packets)
}

func groupedRange(
	encoder *raptorgo.ObjectEncoder,
	sourceBlock uint8,
	startESI, count, group uint32,
	shorten bool,
) ([][]byte, error) {
	packets := make([][]byte, 0, (uint64(count)+uint64(group)-1)/uint64(group))
	for offset := uint32(0); offset < count; {
		packetSymbols := min(group, count-offset)
		packet, err := encoder.Packet(sourceBlock, startESI+offset, packetSymbols, shorten)
		if err != nil {
			return nil, err
		}
		packets = append(packets, packet)
		offset += packetSymbols
	}
	return packets, nil
}

func encodeAllSource(arguments []string) error {
	input, oti, err := readInputAndOTI(arguments[0], arguments[1])
	if err != nil {
		return err
	}
	encoder, err := raptorgo.NewObjectEncoder(input, oti, raptorgo.DefaultLimits())
	if err != nil {
		return err
	}
	var packets [][]byte
	for sourceBlock := uint8(0); sourceBlock < oti.SourceBlocks; sourceBlock++ {
		k, symbolsErr := raptorgo.SourceBlockSymbols(oti, sourceBlock)
		if symbolsErr != nil {
			return symbolsErr
		}
		for esi := uint32(0); esi < k; esi++ {
			packet, packetErr := encoder.Packet(sourceBlock, esi, 1, false)
			if packetErr != nil {
				return packetErr
			}
			packets = append(packets, packet)
		}
	}
	return writeFrame(arguments[2], oti, packets)
}

func encodeRange(arguments []string) error {
	input, oti, err := readInputAndOTI(arguments[0], arguments[1])
	if err != nil {
		return err
	}
	sourceBlock, err := parseUint(arguments[2], 8)
	if err != nil {
		return err
	}
	startESI, err := parseUint(arguments[3], 32)
	if err != nil {
		return err
	}
	count, err := parseUint(arguments[4], 32)
	if err != nil {
		return err
	}
	if count == 0 || startESI+count-1 > uint64(raptorgo.MaxEncodingSymbolID) {
		return errors.New("invalid ESI range")
	}
	encoder, err := raptorgo.NewObjectEncoder(input, oti, raptorgo.DefaultLimits())
	if err != nil {
		return err
	}
	packets := make([][]byte, 0, count)
	for esi := startESI; esi < startESI+count; esi++ {
		packet, err := encoder.Packet(uint8(sourceBlock), uint32(esi), 1, false)
		if err != nil {
			return err
		}
		packets = append(packets, packet)
	}
	return writeFrame(arguments[5], oti, packets)
}

func readInputAndOTI(inputPath, otiHex string) ([]byte, raptorgo.OTI, error) {
	input, err := os.ReadFile(inputPath)
	if err != nil {
		return nil, raptorgo.OTI{}, err
	}
	otiBytes, err := hex.DecodeString(otiHex)
	if err != nil {
		return nil, raptorgo.OTI{}, err
	}
	oti, err := raptorgo.ParseOTI(otiBytes, raptorgo.DefaultLimits())
	if err != nil {
		return nil, raptorgo.OTI{}, err
	}
	if uint64(len(input)) != oti.TransferLength {
		return nil, raptorgo.OTI{}, fmt.Errorf("input has %d bytes, OTI declares %d", len(input), oti.TransferLength)
	}
	return input, oti, nil
}

func decodeFrame(framePath, outputPath string) error {
	oti, packets, err := readFrame(framePath)
	if err != nil {
		return err
	}
	var decoded []byte
	decoder, err := raptorgo.NewObjectDecoder(
		oti,
		raptorgo.DefaultLimits(),
		"development-rqframe",
		func(request raptorgo.VerificationRequest) error {
			_, err := io.Copy(io.Discard, request.Object)
			return err
		},
		func(object raptorgo.VerifiedObject) error {
			decoded = append([]byte(nil), object.Bytes...)
			return nil
		},
	)
	if err != nil {
		return err
	}
	for _, packet := range packets {
		state, addErr := decoder.AddPacket(packet)
		if addErr != nil {
			return addErr
		}
		if state == raptorgo.ObjectDelivered {
			break
		}
	}
	if decoder.State() != raptorgo.ObjectDelivered {
		return errors.New("frame does not contain a decodable set")
	}
	return os.WriteFile(outputPath, decoded, 0o600)
}

func writeFrame(path string, oti raptorgo.OTI, packets [][]byte) error {
	if len(packets) > maxFramePackets {
		return fmt.Errorf(
			"too many packets: %d exceeds limit %d",
			len(packets), maxFramePackets,
		)
	}
	otiBytes, err := oti.MarshalBinary()
	if err != nil {
		return err
	}
	var frame bytes.Buffer
	frame.WriteString("RQO1")
	frame.Write(otiBytes)
	_ = binary.Write(&frame, binary.BigEndian, uint32(len(packets)))
	for _, packet := range packets {
		if uint64(len(packet)) > uint64(^uint32(0)) {
			return errors.New("packet is too large")
		}
		_ = binary.Write(&frame, binary.BigEndian, uint32(len(packet)))
		frame.Write(packet)
		if frame.Len() > maxFrameBytes {
			return errors.New("frame exceeds size limit")
		}
	}
	return os.WriteFile(path, frame.Bytes(), 0o600)
}

func readFrame(path string) (raptorgo.OTI, [][]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return raptorgo.OTI{}, nil, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return raptorgo.OTI{}, nil, err
	}
	if info.Mode().IsRegular() && info.Size() > int64(maxFrameBytes) {
		return raptorgo.OTI{}, nil, errors.New("invalid frame header or size")
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(maxFrameBytes)+1))
	if err != nil {
		return raptorgo.OTI{}, nil, err
	}
	if len(data) < 20 || len(data) > maxFrameBytes || string(data[:4]) != "RQO1" {
		return raptorgo.OTI{}, nil, errors.New("invalid frame header or size")
	}
	oti, err := raptorgo.ParseOTI(data[4:16], raptorgo.DefaultLimits())
	if err != nil {
		return raptorgo.OTI{}, nil, err
	}
	packetCount := binary.BigEndian.Uint32(data[16:20])
	offset := 20
	// Every packet consumes at least one four-octet length field. Reject an
	// impossible or operationally excessive count before converting it to int
	// or using it as a slice capacity.
	maximumCountForData := uint64(len(data)-offset) / 4
	if uint64(packetCount) > maximumCountForData || packetCount > maxFramePackets {
		return raptorgo.OTI{}, nil, fmt.Errorf(
			"invalid frame packet count %d (remaining bytes %d)",
			packetCount, len(data)-offset,
		)
	}
	packets := make([][]byte, 0, int(packetCount))
	for packet := uint32(0); packet < packetCount; packet++ {
		if offset > len(data)-4 {
			return raptorgo.OTI{}, nil, errors.New("truncated frame length")
		}
		length := binary.BigEndian.Uint32(data[offset : offset+4])
		offset += 4
		if uint64(length) > uint64(len(data)-offset) {
			return raptorgo.OTI{}, nil, errors.New("truncated frame packet")
		}
		end := offset + int(length)
		packets = append(packets, data[offset:end:end])
		offset = end
	}
	if offset != len(data) {
		return raptorgo.OTI{}, nil, errors.New("trailing frame bytes")
	}
	return oti, packets, nil
}

func parseUint(value string, bits int) (uint64, error) {
	return strconv.ParseUint(value, 10, bits)
}

func parsePositiveUint32(value, name string) (uint32, error) {
	parsed, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0, err
	}
	if parsed == 0 {
		return 0, fmt.Errorf("%s must be positive", name)
	}
	return uint32(parsed), nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
