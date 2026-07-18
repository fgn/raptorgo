// Package harvest drives deterministic ObjectDecoder executions through the
// public API and records them with objtrace.
package harvest

import (
	"fmt"
	"sort"

	"github.com/fgn/raptorgo/dev/internal/objtrace"
	"github.com/fgn/raptorgo/dev/internal/tracecorpus"
)

const (
	FeatureMultiBlock           tracecorpus.Feature = "multi_block"
	FeatureTrailingEmpty        tracecorpus.Feature = "trailing_empty_blocks"
	FeatureTrailingEmptyZ255    tracecorpus.Feature = "trailing_empty_blocks_z255"
	FeatureSuccess              tracecorpus.Feature = "success"
	FeatureVerifyFailure        tracecorpus.Feature = "verification_failure"
	FeatureDeliveryFailure      tracecorpus.Feature = "delivery_failure"
	FeatureDecodeFailure        tracecorpus.Feature = "decode_failure"
	FeatureConflictReject       tracecorpus.Feature = "conflict_reject"
	FeatureInvalidReject        tracecorpus.Feature = "invalid_reject"
	FeatureLimitReject          tracecorpus.Feature = "limit_reject"
	FeatureTerminalReject       tracecorpus.Feature = "terminal_reject"
	FeatureConstructSuccess     tracecorpus.Feature = "construct_success"
	FeatureConstructVerifyFail  tracecorpus.Feature = "construct_verification_failure"
	FeatureConstructDeliverFail tracecorpus.Feature = "construct_delivery_failure"
	FeatureGrouped              tracecorpus.Feature = "grouped_packet"
	FeatureVerifierReentrant    tracecorpus.Feature = "verifier_reentrant_reject"
	FeatureDelivererReentrant   tracecorpus.Feature = "deliverer_reentrant_reject"
	FeatureDuplicate            tracecorpus.Feature = "duplicate_packet"
	FeatureLoss                 tracecorpus.Feature = "source_loss_repair"
	FeatureContiguousLayout     tracecorpus.Feature = "contiguous_layout"
	FeatureSubBlockedLayout     tracecorpus.Feature = "subblocked_layout"
	FeatureShortened            tracecorpus.Feature = "shortened_source"
	FeatureInlineFuzzSeed       tracecorpus.Feature = "inline_fuzz_seed"
)

var semanticRequirements = []tracecorpus.Feature{
	FeatureMultiBlock,
	FeatureTrailingEmpty,
	FeatureTrailingEmptyZ255,
	FeatureSuccess,
	FeatureVerifyFailure,
	FeatureDeliveryFailure,
	FeatureDecodeFailure,
	FeatureConflictReject,
	FeatureInvalidReject,
	FeatureLimitReject,
	FeatureTerminalReject,
	FeatureConstructSuccess,
	FeatureConstructVerifyFail,
	FeatureConstructDeliverFail,
	FeatureGrouped,
	FeatureVerifierReentrant,
	FeatureDelivererReentrant,
	FeatureDuplicate,
	FeatureLoss,
	FeatureContiguousLayout,
	FeatureSubBlockedLayout,
	FeatureShortened,
}

type scenario struct {
	name     string
	features []tracecorpus.Feature
	required bool
	run      func() ([]objtrace.Event, error)
}

// Candidates runs every built-in scenario in a stable table order.
func Candidates() ([]tracecorpus.Candidate, error) {
	result := make([]tracecorpus.Candidate, 0, len(scenarios))
	for _, scenario := range scenarios {
		events, err := scenario.run()
		if err != nil {
			return nil, fmt.Errorf("harvest: scenario %s: %w", scenario.name, err)
		}
		features := append([]tracecorpus.Feature(nil), scenario.features...)
		if scenario.required {
			features = append(features, caseFeature(scenario.name))
		}
		result = append(result, tracecorpus.Candidate{
			Source: scenario.name, Features: features, Events: events,
		})
	}
	return result, nil
}

// RequiredFeatures returns the semantic and per-case obligations that prevent
// required fixture families from being erased by abstract deduplication.
func RequiredFeatures() []tracecorpus.Feature {
	result := append([]tracecorpus.Feature(nil), semanticRequirements...)
	for _, scenario := range scenarios {
		if scenario.required {
			result = append(result, caseFeature(scenario.name))
		}
	}
	sort.Slice(result, func(left, right int) bool { return result[left] < result[right] })
	return result
}

func caseFeature(name string) tracecorpus.Feature {
	return tracecorpus.Feature("case_" + name)
}

func patternedObject(length uint64) []byte {
	result := make([]byte, int(length))
	for index := range result {
		result[index] = byte(index*37 + 11)
	}
	return result
}
