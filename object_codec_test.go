package raptorgo

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestObjectEncoderGroupedSourceAndRepairPackets(t *testing.T) {
	t.Parallel()

	oti := OTI{TransferLength: 29, SymbolSize: 6, SourceBlocks: 2, SubBlocks: 4, SymbolAlignment: 1}
	object := patternedObject(oti.TransferLength)
	encoder, err := NewObjectEncoder(object, oti, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	object[0] ^= 0xff

	for sourceBlock := uint8(0); sourceBlock < oti.SourceBlocks; sourceBlock++ {
		k, err := SourceBlockSymbols(oti, sourceBlock)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := encoder.Packet(sourceBlock, 0, k, true)
		if err != nil {
			t.Fatal(err)
		}
		packet, err := ParseEncodingPacket(encoded, oti, DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		if !packet.IsSource() || packet.SymbolCount() != k {
			t.Fatalf("SBN %d packet metadata=%+v", sourceBlock, packet)
		}
		block, err := BuildSourceBlock(patternedObject(oti.TransferLength), oti, sourceBlock, DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		canonical := make([]byte, oti.SymbolSize)
		for index := uint32(0); index < k; index++ {
			if err := packet.CopySymbol(index, canonical); err != nil {
				t.Fatal(err)
			}
			want, err := block.Symbol(index)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(canonical, want) {
				t.Fatalf("SBN %d source %d=%x want=%x", sourceBlock, index, canonical, want)
			}
		}

		repair, err := encoder.Packet(sourceBlock, k, 3, false)
		if err != nil {
			t.Fatal(err)
		}
		repairView, err := ParseEncodingPacket(repair, oti, DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		if repairView.IsSource() || repairView.SymbolCount() != 3 {
			t.Fatalf("SBN %d repair metadata=%+v", sourceBlock, repairView)
		}
	}
}

func TestObjectCodecMultiBlockSubBlockRoundTripAndAuthentication(t *testing.T) {
	t.Parallel()

	oti := OTI{TransferLength: 29, SymbolSize: 6, SourceBlocks: 2, SubBlocks: 4, SymbolAlignment: 1}
	want := patternedObject(oti.TransferLength)
	encoder, err := NewObjectEncoder(want, oti, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	var sequence []string
	var delivered VerifiedObject
	decoder, err := NewObjectDecoder(
		oti,
		DefaultLimits(),
		"generation-42",
		func(request VerificationRequest) error {
			sequence = append(sequence, "verify")
			if request.OTI != oti || request.Context != "generation-42" || request.Length != oti.TransferLength {
				return fmt.Errorf("wrong verification binding")
			}
			got, err := io.ReadAll(request.Object)
			if err != nil {
				return err
			}
			if !bytes.Equal(got, want) {
				return fmt.Errorf("candidate differs")
			}
			return nil
		},
		func(object VerifiedObject) error {
			sequence = append(sequence, "deliver")
			delivered = object
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	for sourceBlock := uint8(0); sourceBlock < oti.SourceBlocks; sourceBlock++ {
		k, err := SourceBlockSymbols(oti, sourceBlock)
		if err != nil {
			t.Fatal(err)
		}
		packet, err := encoder.Packet(sourceBlock, 0, k, true)
		if err != nil {
			t.Fatal(err)
		}
		state, err := decoder.AddPacket(packet)
		if err != nil {
			t.Fatal(err)
		}
		if state != ObjectReceiving && state != ObjectDelivered {
			t.Fatalf("SBN %d state=%s", sourceBlock, state)
		}
	}
	if decoder.State() != ObjectDelivered || decoder.CompletedBlocks() != 2 {
		t.Fatalf("state=%s completed=%d", decoder.State(), decoder.CompletedBlocks())
	}
	if fmt.Sprint(sequence) != "[verify deliver]" {
		t.Fatalf("callback sequence=%v", sequence)
	}
	if delivered.OTI != oti || delivered.Context != "generation-42" || !bytes.Equal(delivered.Bytes, want) {
		t.Fatalf("delivered=%+v", delivered)
	}
	delivered.Bytes[0] ^= 0xff
	if state, err := decoder.AddPacket([]byte{0}); !errors.Is(err, ErrObjectTerminal) || state != ObjectDelivered {
		t.Fatalf("post-delivery state=%s error=%v", state, err)
	}
}

func TestObjectDecoderRecoversMixedLossWithRepairs(t *testing.T) {
	t.Parallel()

	oti := OTI{TransferLength: 73, SymbolSize: 4, SourceBlocks: 3, SubBlocks: 1, SymbolAlignment: 1}
	want := patternedObject(oti.TransferLength)
	encoder, err := NewObjectEncoder(want, oti, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	var delivered []byte
	decoder, err := NewObjectDecoder(
		oti, DefaultLimits(), "mixed-loss",
		func(request VerificationRequest) error {
			got, err := io.ReadAll(request.Object)
			if err != nil {
				return err
			}
			if !bytes.Equal(got, want) {
				return errors.New("wrong candidate")
			}
			return nil
		},
		func(object VerifiedObject) error {
			delivered = append([]byte(nil), object.Bytes...)
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	for sourceBlock := uint8(0); sourceBlock < oti.SourceBlocks; sourceBlock++ {
		k, err := SourceBlockSymbols(oti, sourceBlock)
		if err != nil {
			t.Fatal(err)
		}
		for esi := uint32(0); esi+1 < k; esi++ {
			packet, err := encoder.Packet(sourceBlock, esi, 1, true)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decoder.AddPacket(packet); err != nil {
				t.Fatal(err)
			}
		}
		for repair := uint32(0); repair < 32 && decoder.State() == ObjectReceiving; repair++ {
			packet, err := encoder.Packet(sourceBlock, k+repair, 1, false)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decoder.AddPacket(packet); err != nil {
				t.Fatal(err)
			}
			if decoder.CompletedBlocks() > uint32(sourceBlock) {
				break
			}
		}
	}
	if decoder.State() != ObjectDelivered || !bytes.Equal(delivered, want) {
		t.Fatalf("state=%s delivered=%x", decoder.State(), delivered)
	}
}

func TestObjectDecoderVerificationAndDeliveryFailuresAreTerminal(t *testing.T) {
	t.Parallel()

	oti := OTI{TransferLength: 9, SymbolSize: 4, SourceBlocks: 1, SubBlocks: 1, SymbolAlignment: 1}
	object := patternedObject(oti.TransferLength)
	encoder, err := NewObjectEncoder(object, oti, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	k, err := SourceBlockSymbols(oti, 0)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := encoder.Packet(0, 0, k, true)
	if err != nil {
		t.Fatal(err)
	}

	var deliveries atomic.Int32
	verificationFailure, err := NewObjectDecoder(
		oti, DefaultLimits(), "bad-generation",
		func(VerificationRequest) error { return errors.New("signature mismatch") },
		func(VerifiedObject) error { deliveries.Add(1); return nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	if state, err := verificationFailure.AddPacket(packet); !errors.Is(err, ErrObjectVerification) || state != ObjectVerificationFailed {
		t.Fatalf("verification state=%s error=%v", state, err)
	}
	if deliveries.Load() != 0 {
		t.Fatal("verification failure crossed delivery boundary")
	}
	if _, err := verificationFailure.AddPacket(packet); !errors.Is(err, ErrObjectTerminal) {
		t.Fatalf("post-verification-failure error=%v", err)
	}

	var deliveryCalls atomic.Int32
	deliveryFailure, err := NewObjectDecoder(
		oti, DefaultLimits(), "delivery-failure",
		func(VerificationRequest) error { return nil },
		func(VerifiedObject) error { deliveryCalls.Add(1); return errors.New("sink unavailable") },
	)
	if err != nil {
		t.Fatal(err)
	}
	if state, err := deliveryFailure.AddPacket(packet); !errors.Is(err, ErrObjectDelivery) || state != ObjectDeliveryFailed {
		t.Fatalf("delivery state=%s error=%v", state, err)
	}
	if _, err := deliveryFailure.AddPacket(packet); !errors.Is(err, ErrObjectTerminal) {
		t.Fatalf("post-delivery-failure error=%v", err)
	}
	if deliveryCalls.Load() != 1 {
		t.Fatalf("delivery invoked %d times", deliveryCalls.Load())
	}
}

func TestObjectDecoderCallbacksRunUnlockedAtExplicitStates(t *testing.T) {
	t.Parallel()

	oti := OTI{TransferLength: 3, SymbolSize: 4, SourceBlocks: 1, SubBlocks: 1, SymbolAlignment: 1}
	encoder, err := NewObjectEncoder([]byte{1, 2, 3}, oti, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	packet, err := encoder.Packet(0, 0, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	var decoder *ObjectDecoder
	var observed []ObjectDecoderState
	decoder, err = NewObjectDecoder(
		oti, DefaultLimits(), "reentrant",
		func(VerificationRequest) error {
			observed = append(observed, decoder.State())
			state, addErr := decoder.AddPacket(packet)
			if !errors.Is(addErr, ErrObjectTerminal) || state != ObjectDecoded {
				return fmt.Errorf("verifier reentry state=%s error=%v", state, addErr)
			}
			return nil
		},
		func(VerifiedObject) error {
			observed = append(observed, decoder.State())
			state, addErr := decoder.AddPacket(packet)
			if !errors.Is(addErr, ErrObjectTerminal) || state != ObjectVerified {
				return fmt.Errorf("deliverer reentry state=%s error=%v", state, addErr)
			}
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if state, err := decoder.AddPacket(packet); err != nil || state != ObjectDelivered {
		t.Fatalf("final state=%s error=%v", state, err)
	}
	if fmt.Sprint(observed) != "[decoded verified]" {
		t.Fatalf("callback states=%v", observed)
	}
}

func TestObjectDecoderEmptyObjectFinalizesSynchronously(t *testing.T) {
	t.Parallel()

	oti := OTI{TransferLength: 0, SymbolSize: 4, SourceBlocks: 3, SubBlocks: 1, SymbolAlignment: 1}
	var verified, delivered atomic.Int32
	decoder, err := NewObjectDecoder(
		oti, DefaultLimits(), "empty",
		func(request VerificationRequest) error {
			verified.Add(1)
			data, readErr := io.ReadAll(request.Object)
			if readErr != nil || request.Length != 0 || len(data) != 0 {
				return fmt.Errorf("empty verification length=%d data=%x: %v", request.Length, data, readErr)
			}
			return nil
		},
		func(object VerifiedObject) error {
			delivered.Add(1)
			if object.Context != "empty" || object.OTI != oti || len(object.Bytes) != 0 {
				return fmt.Errorf("wrong empty delivery: %+v", object)
			}
			return nil
		},
	)
	if err != nil || decoder == nil || decoder.State() != ObjectDelivered {
		t.Fatalf("decoder=%v state=%v error=%v", decoder, decoder.State(), err)
	}
	if verified.Load() != 1 || delivered.Load() != 1 || decoder.CompletedBlocks() != 0 {
		t.Fatalf("verified=%d delivered=%d completed=%d", verified.Load(), delivered.Load(), decoder.CompletedBlocks())
	}

	var failedDeliveries atomic.Int32
	failed, err := NewObjectDecoder(
		oti, DefaultLimits(), "empty-failure",
		func(VerificationRequest) error { return errors.New("empty signature rejected") },
		func(VerifiedObject) error { failedDeliveries.Add(1); return nil },
	)
	if !errors.Is(err, ErrObjectVerification) || failed == nil || failed.State() != ObjectVerificationFailed {
		t.Fatalf("failed decoder=%v state=%v error=%v", failed, failed.State(), err)
	}
	if failedDeliveries.Load() != 0 {
		t.Fatal("verification-failed empty object was delivered")
	}
}

func TestObjectEncoderConcurrentRepairPackets(t *testing.T) {
	oti := OTI{TransferLength: 40, SymbolSize: 4, SourceBlocks: 1, SubBlocks: 1, SymbolAlignment: 1}
	encoder, err := NewObjectEncoder(patternedObject(oti.TransferLength), oti, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	k, err := SourceBlockSymbols(oti, 0)
	if err != nil {
		t.Fatal(err)
	}
	want, err := encoder.Packet(0, k+10, 2, false)
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	errorsChannel := make(chan error, 16)
	for worker := 0; worker < 16; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			got, err := encoder.Packet(0, k+10, 2, false)
			if err != nil {
				errorsChannel <- err
				return
			}
			if !bytes.Equal(got, want) {
				errorsChannel <- errors.New("concurrent repair packet differs")
			}
		}()
	}
	wait.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		t.Error(err)
	}
}

func TestObjectEncoderPacketsOwnIndependentBacking(t *testing.T) {
	t.Parallel()

	oti := OTI{TransferLength: 29, SymbolSize: 6, SourceBlocks: 2, SubBlocks: 4, SymbolAlignment: 1}
	encoder, err := NewObjectEncoder(patternedObject(oti.TransferLength), oti, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	k, err := SourceBlockSymbols(oti, 1)
	if err != nil {
		t.Fatal(err)
	}
	source, err := encoder.Packet(1, 0, k, true)
	if err != nil {
		t.Fatal(err)
	}
	wantSource := append([]byte(nil), source...)
	for index := range source {
		source[index] ^= 0xff
	}
	sourceAgain, err := encoder.Packet(1, 0, k, true)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sourceAgain, wantSource) {
		t.Fatalf("source packet mutation affected later output: got %x, want %x", sourceAgain, wantSource)
	}

	repair, err := encoder.Packet(1, k+7, 2, false)
	if err != nil {
		t.Fatal(err)
	}
	wantRepair := append([]byte(nil), repair...)
	for index := range repair {
		repair[index] ^= 0xff
	}
	repairAgain, err := encoder.Packet(1, k+7, 2, false)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(repairAgain, wantRepair) {
		t.Fatalf("repair packet mutation affected later output: got %x, want %x", repairAgain, wantRepair)
	}
}

func TestObjectEncoderConcurrentMixedSourceAndRepairPackets(t *testing.T) {
	oti := OTI{TransferLength: 29, SymbolSize: 6, SourceBlocks: 2, SubBlocks: 4, SymbolAlignment: 1}
	encoder, err := NewObjectEncoder(patternedObject(oti.TransferLength), oti, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	type packetRequest struct {
		sourceBlock     uint8
		firstESI, count uint32
		shorten         bool
		want            []byte
	}
	requests := make([]packetRequest, 0, 4)
	for sourceBlockIndex := 0; sourceBlockIndex < int(oti.SourceBlocks); sourceBlockIndex++ {
		sourceBlock := uint8(sourceBlockIndex)
		k, err := SourceBlockSymbols(oti, sourceBlock)
		if err != nil {
			t.Fatal(err)
		}
		for _, request := range []packetRequest{
			{sourceBlock: sourceBlock, firstESI: 0, count: k, shorten: true},
			{sourceBlock: sourceBlock, firstESI: k + 9, count: 2},
		} {
			request.want, err = encoder.Packet(request.sourceBlock, request.firstESI, request.count, request.shorten)
			if err != nil {
				t.Fatal(err)
			}
			requests = append(requests, request)
		}
	}

	var wait sync.WaitGroup
	errorsChannel := make(chan error, 16)
	for worker := 0; worker < 16; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for iteration := 0; iteration < 8; iteration++ {
				for _, request := range requests {
					got, err := encoder.Packet(request.sourceBlock, request.firstESI, request.count, request.shorten)
					if err != nil {
						errorsChannel <- err
						return
					}
					if !bytes.Equal(got, request.want) {
						errorsChannel <- errors.New("concurrent mixed packet differs")
						return
					}
				}
			}
		}()
	}
	wait.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		t.Error(err)
	}
}

func TestObjectDecoderConcurrentSourcePacketsDeliverOnce(t *testing.T) {
	oti := OTI{TransferLength: 80, SymbolSize: 4, SourceBlocks: 1, SubBlocks: 1, SymbolAlignment: 1}
	want := patternedObject(oti.TransferLength)
	encoder, err := NewObjectEncoder(want, oti, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	var deliveries atomic.Int32
	decoder, err := NewObjectDecoder(
		oti, DefaultLimits(), "concurrent",
		func(request VerificationRequest) error {
			got, err := io.ReadAll(request.Object)
			if err != nil {
				return err
			}
			if !bytes.Equal(got, want) {
				return errors.New("wrong concurrent candidate")
			}
			return nil
		},
		func(VerifiedObject) error { deliveries.Add(1); return nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	k, err := SourceBlockSymbols(oti, 0)
	if err != nil {
		t.Fatal(err)
	}
	packets := make([][]byte, k)
	for esi := uint32(0); esi < k; esi++ {
		packets[esi], err = encoder.Packet(0, esi, 1, true)
		if err != nil {
			t.Fatal(err)
		}
	}
	var wait sync.WaitGroup
	errorsChannel := make(chan error, k)
	for _, packet := range packets {
		packet := packet
		wait.Add(1)
		go func() {
			defer wait.Done()
			if _, err := decoder.AddPacket(packet); err != nil {
				errorsChannel <- err
			}
		}()
	}
	wait.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		t.Error(err)
	}
	if decoder.State() != ObjectDelivered || deliveries.Load() != 1 {
		t.Fatalf("state=%s deliveries=%d", decoder.State(), deliveries.Load())
	}
}

func TestObjectCodecInputAndResourceValidation(t *testing.T) {
	t.Parallel()

	oti := OTI{TransferLength: 8, SymbolSize: 4, SourceBlocks: 1, SubBlocks: 1, SymbolAlignment: 1}
	if encoder, err := NewObjectEncoder([]byte{1}, oti, DefaultLimits()); !errors.Is(err, ErrInvalidObject) || encoder != nil {
		t.Fatalf("short object encoder=%v error=%v", encoder, err)
	}
	limits := DefaultLimits()
	limits.MaxWorkingMemory = 8
	if encoder, err := NewObjectEncoder(make([]byte, 8), oti, limits); !errors.Is(err, ErrLimitExceeded) || encoder != nil {
		t.Fatalf("under-budget encoder=%v error=%v", encoder, err)
	}
	encoderLimits := DefaultLimits()
	encoderRequired, err := ObjectEncoderWorkspaceBackingBytes(oti, encoderLimits)
	if err != nil {
		t.Fatal(err)
	}
	encoderLimits.MaxWorkingMemory = encoderRequired - 1
	if encoder, err := NewObjectEncoder(make([]byte, 8), oti, encoderLimits); !errors.Is(err, ErrLimitExceeded) || encoder != nil {
		t.Fatalf("budget-1 encoder=%v error=%v", encoder, err)
	}
	encoderLimits.MaxWorkingMemory = encoderRequired
	if encoder, err := NewObjectEncoder(make([]byte, 8), oti, encoderLimits); err != nil || encoder == nil {
		t.Fatalf("exact-budget encoder=%v error=%v", encoder, err)
	}
	if decoder, err := NewObjectDecoder(oti, DefaultLimits(), "", nil, func(VerifiedObject) error { return nil }); !errors.Is(err, ErrObjectDecoder) || decoder != nil {
		t.Fatalf("nil-verifier decoder=%v error=%v", decoder, err)
	}
	decoderLimits := DefaultLimits()
	required, err := ObjectDecoderWorkspaceBackingBytes(oti, decoderLimits)
	if err != nil {
		t.Fatal(err)
	}
	decoderLimits.MaxWorkingMemory = required - 1
	if decoder, err := NewObjectDecoder(
		oti, decoderLimits, "budget-1",
		func(VerificationRequest) error { return nil }, func(VerifiedObject) error { return nil },
	); !errors.Is(err, ErrLimitExceeded) || decoder != nil {
		t.Fatalf("budget-1 decoder=%v error=%v", decoder, err)
	}
	decoderLimits.MaxWorkingMemory = required
	if decoder, err := NewObjectDecoder(
		oti, decoderLimits, "exact",
		func(VerificationRequest) error { return nil }, func(VerifiedObject) error { return nil },
	); err != nil || decoder == nil || decoder.PersistentBackingBytes() == 0 {
		t.Fatalf("exact-budget decoder=%v error=%v", decoder, err)
	}
	encoder, err := NewObjectEncoder(make([]byte, 8), oti, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if packet, err := encoder.Packet(0, 1, 2, false); !errors.Is(err, ErrInvalidPacket) || packet != nil {
		t.Fatalf("cross-boundary packet=%v error=%v", packet, err)
	}
	if packet, err := encoder.Packet(1, 0, 1, false); !errors.Is(err, ErrInvalidPacket) || packet != nil {
		t.Fatalf("unknown-block packet=%v error=%v", packet, err)
	}
}

func TestObjectCodecRFCMaximumSourceBlocks(t *testing.T) {
	done := make(chan error, 1)
	go func() {
		const sourceBlocks = 255
		oti := OTI{
			TransferLength: sourceBlocks, SymbolSize: 1,
			SourceBlocks: sourceBlocks, SubBlocks: 1, SymbolAlignment: 1,
		}
		want := patternedObject(oti.TransferLength)
		encoder, err := NewObjectEncoder(want, oti, DefaultLimits())
		if err != nil {
			done <- err
			return
		}
		decoder, err := NewObjectDecoder(
			oti, DefaultLimits(), "maximum-source-blocks",
			func(request VerificationRequest) error {
				got, readErr := io.ReadAll(request.Object)
				if readErr != nil {
					return readErr
				}
				if !bytes.Equal(got, want) {
					return errors.New("maximum-source-block object differs")
				}
				return nil
			},
			func(object VerifiedObject) error {
				if !bytes.Equal(object.Bytes, want) {
					return errors.New("maximum-source-block delivery differs")
				}
				return nil
			},
		)
		if err != nil {
			done <- err
			return
		}
		for sourceBlockIndex := 0; sourceBlockIndex < sourceBlocks; sourceBlockIndex++ {
			sourceBlock := uint8(sourceBlockIndex)
			packet, packetErr := encoder.Packet(sourceBlock, 0, 1, false)
			if packetErr != nil {
				done <- packetErr
				return
			}
			if _, addErr := decoder.AddPacket(packet); addErr != nil {
				done <- addErr
				return
			}
		}
		if decoder.State() != ObjectDelivered || decoder.CompletedBlocks() != sourceBlocks {
			done <- fmt.Errorf("state=%s completed=%d", decoder.State(), decoder.CompletedBlocks())
			return
		}
		done <- nil
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Z=255 object codec did not terminate")
	}
}

func patternedObject(length uint64) []byte {
	result := make([]byte, int(length))
	for index := range result {
		result[index] = byte(index*37 + 11)
	}
	return result
}

func FuzzObjectCodecSourcePacketRoundTrip(f *testing.F) {
	f.Add([]byte("raptorq"), uint8(4), uint8(2), uint8(2))
	f.Add([]byte{1}, uint8(1), uint8(4), uint8(1))
	f.Add([]byte{}, uint8(8), uint8(1), uint8(8))
	f.Fuzz(func(t *testing.T, data []byte, sizeSeed, blocksSeed, subBlocksSeed uint8) {
		if len(data) > 128 {
			data = data[:128]
		}
		symbolSize := uint16(sizeSeed%16 + 1)
		sourceBlocks := blocksSeed%4 + 1
		subBlocks := uint16(subBlocksSeed)%symbolSize + 1
		oti := OTI{
			TransferLength: uint64(len(data)), SymbolSize: symbolSize,
			SourceBlocks: sourceBlocks, SubBlocks: subBlocks, SymbolAlignment: 1,
		}
		limits := DefaultLimits()
		encoder, err := NewObjectEncoder(data, oti, limits)
		if err != nil {
			t.Fatal(err)
		}
		var delivered []byte
		decoder, err := NewObjectDecoder(
			oti, limits, "fuzz-source",
			func(request VerificationRequest) error {
				got, err := io.ReadAll(request.Object)
				if err != nil {
					return err
				}
				if !bytes.Equal(got, data) {
					return errors.New("fuzz candidate differs")
				}
				return nil
			},
			func(object VerifiedObject) error {
				delivered = append([]byte(nil), object.Bytes...)
				return nil
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		for sourceBlock := uint8(0); sourceBlock < sourceBlocks && decoder.State() == ObjectReceiving; sourceBlock++ {
			k, err := SourceBlockSymbols(oti, sourceBlock)
			if err != nil {
				t.Fatal(err)
			}
			if k == 0 {
				continue
			}
			packet, err := encoder.Packet(sourceBlock, 0, k, true)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decoder.AddPacket(packet); err != nil {
				t.Fatal(err)
			}
		}
		if decoder.State() != ObjectDelivered || !bytes.Equal(delivered, data) {
			t.Fatalf("state=%s delivered=%x want=%x", decoder.State(), delivered, data)
		}
	})
}
