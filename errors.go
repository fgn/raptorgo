package raptorgo

import "errors"

var (
	// ErrInvalidLimits is returned when a Limits value has a zero,
	// out-of-range, or otherwise unsafe field.
	ErrInvalidLimits = errors.New("raptorgo: invalid limits")
	// ErrLimitExceeded is returned when an operation would exceed a
	// caller-supplied Limits field, the RFC 6330 wire domain, or the
	// address space of the platform.
	ErrLimitExceeded = errors.New("raptorgo: resource limit exceeded")
	// ErrInvalidOTI is returned when Object Transmission Information
	// violates an RFC 6330 constraint or has the wrong encoded length.
	ErrInvalidOTI = errors.New("raptorgo: invalid object transmission information")
	// ErrInvalidDerivation is returned when DeriveOTI input is inconsistent
	// or admits no valid RFC 6330 Section 4.3 parameter derivation.
	ErrInvalidDerivation = errors.New("raptorgo: invalid RFC parameter derivation input")
	// ErrInvalidPayloadID is returned when a FEC Payload ID has the wrong
	// encoded length or an encoding symbol ID that exceeds 24 bits.
	ErrInvalidPayloadID = errors.New("raptorgo: invalid FEC payload ID")
	// ErrInvalidPacket is returned when an encoding packet violates
	// RFC 6330 grouped-symbol or shortening semantics, targets an invalid
	// or empty source block, or requests an out-of-range symbol.
	ErrInvalidPacket = errors.New("raptorgo: invalid encoding packet")
	// ErrInvalidObject is returned when a source object does not match its
	// OTI, for example when its length differs from the transfer length.
	ErrInvalidObject = errors.New("raptorgo: invalid source object")
	// ErrObjectEncoder is returned when object encoding fails, including
	// nil-encoder use and internal source-block encoder failures.
	ErrObjectEncoder = errors.New("raptorgo: object encoder failed")
	// ErrObjectDecoder is returned when object decoding fails, including
	// nil-decoder use, construction preflight failures, and internal
	// source-block decoder failures.
	ErrObjectDecoder = errors.New("raptorgo: object decoder failed")
	// ErrObjectVerification is returned when the application verifier
	// rejects a reconstructed object.
	ErrObjectVerification = errors.New("raptorgo: decoded object verification failed")
	// ErrObjectDelivery is returned when the application deliverer rejects
	// a verified object.
	ErrObjectDelivery = errors.New("raptorgo: verified object delivery failed")
	// ErrObjectTerminal is returned when a packet is added to an
	// ObjectDecoder that has already left the ObjectReceiving state.
	ErrObjectTerminal = errors.New("raptorgo: object decoder is terminal")
)
