package gf256

import (
	"errors"
	"math"
	"strconv"
	"testing"
)

func TestWorkspaceEstimatorsRejectAggregateBeyond32BitAddressSpace(t *testing.T) {
	if strconv.IntSize != 32 {
		t.Skip("32-bit address-space regression")
	}
	if _, err := SolveWorkspaceBytes(1, 1<<30, 1); !errors.Is(err, ErrSolverWorkspaceLimit) {
		t.Fatalf("aggregate Solve workspace error = %v", err)
	}
	if _, err := InvertWorkspaceBytes(32768); !errors.Is(err, ErrSolverWorkspaceLimit) {
		t.Fatalf("aggregate Invert workspace error = %v", err)
	}
	if _, err := SolveWorkspaceBytes(math.MaxUint32, 1, 1); !errors.Is(err, ErrSolverWorkspaceLimit) {
		t.Fatalf("single-allocation Solve workspace error = %v", err)
	}
}
