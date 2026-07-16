package matrix

import (
	"errors"
	"math"
	"strconv"
	"testing"
)

func TestSparseAndPermutationRejectByteLengthsBeyond32BitAddressSpace(t *testing.T) {
	if strconv.IntSize != 32 {
		t.Skip("32-bit address-space regression")
	}
	if _, err := SparseBinaryBackingBytes((1<<28)-1, 1, 0); !errors.Is(err, ErrBackingLimitExceeded) {
		t.Fatalf("offset backing error = %v", err)
	}
	if _, err := SparseBinaryBackingBytes(1, 1<<29, 0); !errors.Is(err, ErrBackingLimitExceeded) {
		t.Fatalf("scratch backing error = %v", err)
	}
	if _, err := SparseOctetBackingBytes(1, 1<<29, 0); !errors.Is(err, ErrBackingLimitExceeded) {
		t.Fatalf("octet scratch backing error = %v", err)
	}
	if _, err := NewIdentityPermutation(1<<29, math.MaxUint64); !errors.Is(err, ErrPermutationLimit) {
		t.Fatalf("permutation backing error = %v", err)
	}
}
