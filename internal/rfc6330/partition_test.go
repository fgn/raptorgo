package rfc6330

import "testing"

func TestPartitionExamples(t *testing.T) {
	tests := []struct {
		total, parts uint64
		want         PartitionResult
	}{
		{0, 1, PartitionResult{0, 0, 0, 1}},
		{1, 1, PartitionResult{1, 1, 0, 1}},
		{3, 5, PartitionResult{1, 0, 3, 2}},
		{10, 2, PartitionResult{5, 5, 0, 2}},
		{17, 5, PartitionResult{4, 3, 2, 3}},
	}

	for _, test := range tests {
		got, err := Partition(test.total, test.parts)
		if err != nil {
			t.Fatalf("Partition(%d, %d): %v", test.total, test.parts, err)
		}
		if got != test.want {
			t.Errorf("Partition(%d, %d) = %+v, want %+v", test.total, test.parts, got, test.want)
		}
	}
}

func TestPartitionRejectsZeroParts(t *testing.T) {
	if _, err := Partition(1, 0); err == nil {
		t.Fatal("Partition(1, 0) unexpectedly succeeded")
	}
}

func TestPartitionMaterializationOrder(t *testing.T) {
	partition, err := Partition(17, 5)
	if err != nil {
		t.Fatal(err)
	}
	want := []uint64{4, 4, 3, 3, 3}
	var total uint64
	for index, wantSize := range want {
		got, err := partition.Size(uint64(index))
		if err != nil {
			t.Fatal(err)
		}
		if got != wantSize {
			t.Fatalf("partition %d has size %d, want %d", index, got, wantSize)
		}
		total += got
	}
	if total != 17 {
		t.Fatalf("materialized total is %d", total)
	}
	if _, err := partition.Size(uint64(len(want))); err == nil {
		t.Fatal("out-of-range partition index unexpectedly succeeded")
	}
}

func TestPartitionInvariantsBoundedExhaustive(t *testing.T) {
	for total := uint64(0); total <= 10_000; total++ {
		for parts := uint64(1); parts <= 255; parts++ {
			got, err := Partition(total, parts)
			if err != nil {
				t.Fatalf("Partition(%d, %d): %v", total, parts, err)
			}
			if got.LargeCount+got.SmallCount != parts {
				t.Fatalf("Partition(%d, %d) does not conserve block count", total, parts)
			}
			if got.LargeSize*got.LargeCount+got.SmallSize*got.SmallCount != total {
				t.Fatalf("Partition(%d, %d) does not conserve total size", total, parts)
			}
			if got.LargeCount != total%parts || got.SmallSize != total/parts {
				t.Fatalf("Partition(%d, %d) differs from quotient/remainder", total, parts)
			}
			if got.LargeSize < got.SmallSize || got.LargeSize-got.SmallSize > 1 {
				t.Fatalf("Partition(%d, %d) is not balanced", total, parts)
			}
		}
	}
}

func FuzzPartition(f *testing.F) {
	f.Add(uint64(17), uint64(5))
	f.Add(^uint64(0), uint64(255))
	f.Fuzz(func(t *testing.T, total, parts uint64) {
		if parts == 0 {
			t.Skip()
		}
		got, err := Partition(total, parts)
		if err != nil {
			t.Fatal(err)
		}
		if got.LargeCount+got.SmallCount != parts {
			t.Fatal("partition count is not conserved")
		}
		if got.LargeCount > total {
			t.Fatal("large block count exceeds total")
		}
		if got.LargeCount != 0 && got.LargeSize != got.SmallSize+1 {
			t.Fatal("block sizes differ by more than one")
		}
		// Avoid an overflowing reconstruction multiplication. The identities
		// below are equivalent to the RFC formulas and individually bounded.
		if got.SmallSize != total/parts || got.LargeCount != total%parts {
			t.Fatal("partition differs from quotient/remainder")
		}
	})
}
