// Package raptorgo implements the RaptorQ forward error correction scheme from
// RFC 6330 in pure Go.
//
// ObjectEncoder produces systematic and repair encoding packets for an OTI.
// ObjectDecoder accepts single-symbol or grouped encoding packets
// incrementally and requires an application verifier before it invokes
// delivery. FEC recovery is not an authenticity check; callers remain
// responsible for packet and object trust.
//
// All allocation-sensitive public operations take Limits or retain limits from
// construction. DefaultLimits is conservative operational policy;
// RFCWireLimits is intended for parsing and conformance across the complete RFC
// wire domain.
//
// All errors returned by this package wrap one of the exported sentinel
// errors and can be matched with errors.Is.
package raptorgo
