package matrix

import (
	"errors"
	"reflect"
	"testing"
)

func TestPermutationSwapInverseAndClone(t *testing.T) {
	t.Parallel()

	permutation, err := NewIdentityPermutation(7, 28)
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]uint32{{0, 6}, {1, 4}, {2, 5}, {6, 3}} {
		if err := permutation.Swap(pair[0], pair[1]); err != nil {
			t.Fatal(err)
		}
	}
	inverse, err := permutation.Inverse(28)
	if err != nil {
		t.Fatal(err)
	}
	seen := make([]bool, permutation.Len())
	for position := uint32(0); position < permutation.Len(); position++ {
		original, err := permutation.At(position)
		if err != nil {
			t.Fatal(err)
		}
		if seen[original] {
			t.Fatalf("duplicate original index %d", original)
		}
		seen[original] = true
		inversePosition, err := inverse.At(original)
		if err != nil {
			t.Fatal(err)
		}
		if inversePosition != position {
			t.Fatalf("inverse[%d] = %d, want %d", original, inversePosition, position)
		}
	}
	clone, err := permutation.Clone(28)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(clone.values, permutation.values) {
		t.Fatalf("clone = %v, want %v", clone.values, permutation.values)
	}
	if err := clone.Swap(0, 1); err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(clone.values, permutation.values) {
		t.Fatal("clone aliases original")
	}
}

func TestPermutationBoundsAreTransactional(t *testing.T) {
	t.Parallel()

	if _, err := NewIdentityPermutation(0, 0); !errors.Is(err, ErrInvalidPermutationSize) {
		t.Fatalf("zero-size error = %v", err)
	}
	if _, err := NewIdentityPermutation(2, 7); !errors.Is(err, ErrPermutationLimit) {
		t.Fatalf("under-budget error = %v", err)
	}
	permutation, err := NewIdentityPermutation(2, 8)
	if err != nil {
		t.Fatal(err)
	}
	before := append([]uint32(nil), permutation.values...)
	if err := permutation.Swap(0, 2); !errors.Is(err, ErrPermutationIndex) {
		t.Fatalf("Swap error = %v", err)
	}
	if !reflect.DeepEqual(permutation.values, before) {
		t.Fatal("rejected Swap mutated permutation")
	}
	if _, err := permutation.At(2); !errors.Is(err, ErrPermutationIndex) {
		t.Fatalf("At error = %v", err)
	}
	if _, err := permutation.Inverse(7); !errors.Is(err, ErrPermutationLimit) {
		t.Fatalf("under-budget Inverse error = %v", err)
	}
	if _, err := permutation.Clone(7); !errors.Is(err, ErrPermutationLimit) {
		t.Fatalf("under-budget Clone error = %v", err)
	}
}

func TestPermutationInverseRejectsCorruptBijection(t *testing.T) {
	t.Parallel()

	for _, values := range [][]uint32{{0, 0}, {0, 2}, {0, 1, 3}} {
		permutation := &Permutation{values: values}
		if inverse, err := permutation.Inverse(uint64(len(values) * 4)); !errors.Is(err, ErrPermutationIndex) || inverse != nil {
			t.Fatalf("Inverse(%v) = %v, %v", values, inverse, err)
		}
	}
}
