package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fgn/raptorgo"
)

func TestReadFrameRejectsImpossiblePacketCountBeforeAllocation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		packetCount uint32
		lengthBytes int
	}{
		{name: "uint32 maximum without lengths", packetCount: ^uint32(0)},
		{name: "two packets with one length", packetCount: 2, lengthBytes: 4},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			path := writeTestFrame(t, test.packetCount, make([]byte, test.lengthBytes))
			if _, _, err := readFrame(path); err == nil || !strings.Contains(err.Error(), "invalid frame packet count") {
				t.Fatalf("readFrame() error = %v, want invalid packet count", err)
			}
		})
	}
}

func TestReadFrameRejectsPacketCountAboveOperationalLimit(t *testing.T) {
	t.Parallel()

	packetCount := uint32(maxFramePackets + 1)
	path := writeTestFrame(t, packetCount, make([]byte, int(packetCount)*4))
	if _, _, err := readFrame(path); err == nil || !strings.Contains(err.Error(), "invalid frame packet count") {
		t.Fatalf("readFrame() error = %v, want invalid packet count", err)
	}
}

func TestReadFrameRejectsOversizedSparseFileBeforeReading(t *testing.T) {
	t.Parallel()

	path := writeTestFrame(t, 0, nil)
	if err := os.Truncate(path, int64(maxFrameBytes)+1); err != nil {
		t.Fatalf("Truncate() error = %v", err)
	}
	if _, _, err := readFrame(path); err == nil || !strings.Contains(err.Error(), "invalid frame header or size") {
		t.Fatalf("readFrame() error = %v, want invalid frame size", err)
	}
}

func TestReadFrameAcceptsBoundedPacketCount(t *testing.T) {
	t.Parallel()

	packet := []byte{0, 0, 0, 0, 0x5a}
	body := make([]byte, 4+len(packet))
	binary.BigEndian.PutUint32(body[:4], uint32(len(packet)))
	copy(body[4:], packet)
	path := writeTestFrame(t, 1, body)

	oti, packets, err := readFrame(path)
	if err != nil {
		t.Fatalf("readFrame() error = %v", err)
	}
	if oti.TransferLength != 1 || len(packets) != 1 || string(packets[0]) != string(packet) {
		t.Fatalf("readFrame() = OTI %+v packets %x", oti, packets)
	}
}

func TestWriteFramePacketCountBoundaryRoundTrips(t *testing.T) {
	oti := frameTestOTI(t)
	packets := make([][]byte, maxFramePackets+1)
	path := filepath.Join(t.TempDir(), "maximum.rqo1")

	if err := writeFrame(path, oti, packets[:maxFramePackets]); err != nil {
		t.Fatalf("writeFrame(maximum) error = %v", err)
	}
	gotOTI, gotPackets, err := readFrame(path)
	if err != nil {
		t.Fatalf("readFrame(maximum) error = %v", err)
	}
	if gotOTI != oti || len(gotPackets) != maxFramePackets {
		t.Fatalf("readFrame(maximum) = OTI %+v, %d packets", gotOTI, len(gotPackets))
	}

	overLimitPath := filepath.Join(t.TempDir(), "over-limit.rqo1")
	err = writeFrame(overLimitPath, oti, packets)
	if err == nil || !strings.Contains(err.Error(), "too many packets") {
		t.Fatalf("writeFrame(over limit) error = %v, want too many packets", err)
	}
	if _, statErr := os.Stat(overLimitPath); !os.IsNotExist(statErr) {
		t.Fatalf("writeFrame(over limit) created output: stat error = %v", statErr)
	}
}

func writeTestFrame(t *testing.T, packetCount uint32, body []byte) string {
	t.Helper()

	otiBytes, err := frameTestOTI(t).MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	frame := make([]byte, 20+len(body))
	copy(frame[:4], "RQO1")
	copy(frame[4:16], otiBytes)
	binary.BigEndian.PutUint32(frame[16:20], packetCount)
	copy(frame[20:], body)

	path := filepath.Join(t.TempDir(), "frame.rqo1")
	if err := os.WriteFile(path, frame, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

func frameTestOTI(t *testing.T) raptorgo.OTI {
	t.Helper()

	oti := raptorgo.OTI{
		TransferLength:  1,
		SymbolSize:      1,
		SourceBlocks:    1,
		SubBlocks:       1,
		SymbolAlignment: 1,
	}
	if err := oti.Validate(raptorgo.DefaultLimits()); err != nil {
		t.Fatalf("test OTI validation error = %v", err)
	}
	return oti
}
