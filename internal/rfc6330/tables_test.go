package rfc6330

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"testing"
)

func TestRandomTablesCardinalityKnownValuesAndChecksum(t *testing.T) {
	t.Parallel()

	if len(randomTables) != 4 {
		t.Fatalf("random table count = %d", len(randomTables))
	}
	for index, table := range randomTables {
		if len(table) != 256 {
			t.Fatalf("V%d length = %d", index, len(table))
		}
	}
	known := []struct {
		table int
		index int
		want  uint32
	}{
		{0, 0, 251291136},
		{0, 255, 1358307511},
		{1, 0, 807385413},
		{1, 255, 4135048896},
		{2, 0, 1629829892},
		{2, 255, 3497665928},
		{3, 0, 1191369816},
		{3, 255, 3432275192},
	}
	for _, check := range known {
		if got := randomTables[check.table][check.index]; got != check.want {
			t.Errorf("V%d[%d] = %d, want %d", check.table, check.index, got, check.want)
		}
	}
	if got, want := checksumRandomTables(), "4c0a7a17e651eb25da8e5c2aa8aecc54c9614486e9371db35df94725e87dd06c"; got != want {
		t.Fatalf("random-table checksum = %s, want %s", got, want)
	}
}

func TestDegreeThresholdsExact(t *testing.T) {
	t.Parallel()

	want := [...]uint32{
		0, 5243, 529531, 704294, 791675, 844104, 879057, 904023,
		922747, 937311, 948962, 958494, 966438, 973160, 978921,
		983914, 988283, 992138, 995565, 998631, 1001391, 1003887,
		1006157, 1008229, 1010129, 1011876, 1013490, 1014983,
		1016370, 1017662, 1048576,
	}
	if degreeThresholds != want {
		t.Fatalf("degree thresholds differ from RFC Table 1")
	}
	if got, expected := checksumUint32s(degreeThresholds[:]), "8a5898baeb62f4648fb3b86d181efcc8170f8e9b6a831a18b87c56cb87c5d114"; got != expected {
		t.Fatalf("degree-table checksum = %s, want %s", got, expected)
	}
}

func TestSystematicTableInvariantsAndChecksum(t *testing.T) {
	t.Parallel()

	if len(systematicTable) != 477 {
		t.Fatalf("systematic table rows = %d, want 477", len(systematicTable))
	}
	if got, want := systematicTable[0], (systematicParameters{kPrime: 10, j: 254, s: 7, h: 10, w: 17}); got != want {
		t.Fatalf("first systematic row = %+v, want %+v", got, want)
	}
	if got, want := systematicTable[len(systematicTable)-1], (systematicParameters{kPrime: 56403, j: 471, s: 907, h: 16, w: 56951}); got != want {
		t.Fatalf("last systematic row = %+v, want %+v", got, want)
	}
	for index, row := range systematicTable {
		if index != 0 && row.kPrime <= systematicTable[index-1].kPrime {
			t.Fatalf("K' is not increasing at row %d: %d then %d", index, systematicTable[index-1].kPrime, row.kPrime)
		}
		if !isPrimeReference(row.s) {
			t.Errorf("row %d S=%d is not prime", index, row.s)
		}
		if !isPrimeReference(row.w) {
			t.Errorf("row %d W=%d is not prime", index, row.w)
		}
		if row.w <= row.kPrime || row.kPrime+row.s+row.h <= row.w {
			t.Errorf("row %d violates K' < W < L: %+v", index, row)
		}
	}
	if got, want := checksumSystematicTable(), "9d3657cf3255cf51f9a93b762a37fbe740598e3b14259acc5641abedcd0da7d6"; got != want {
		t.Fatalf("systematic-table checksum = %s, want %s", got, want)
	}
}

func checksumRandomTables() string {
	hash := sha256.New()
	for _, table := range randomTables {
		writeUint32Checksum(hash, table[:])
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func checksumSystematicTable() string {
	hash := sha256.New()
	for _, row := range systematicTable {
		writeUint32Checksum(hash, []uint32{row.kPrime, row.j, row.s, row.h, row.w})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func checksumUint32s(values []uint32) string {
	hash := sha256.New()
	writeUint32Checksum(hash, values)
	return hex.EncodeToString(hash.Sum(nil))
}

type byteWriter interface {
	Write([]byte) (int, error)
}

func writeUint32Checksum(writer byteWriter, values []uint32) {
	var encoded [4]byte
	for _, value := range values {
		binary.BigEndian.PutUint32(encoded[:], value)
		_, _ = writer.Write(encoded[:])
	}
}

func isPrimeReference(value uint32) bool {
	if value < 2 {
		return false
	}
	for divisor := uint32(2); divisor <= value/divisor; divisor++ {
		if value%divisor == 0 {
			return false
		}
	}
	return true
}
