package verifytrace

//go:generate go run ./cmd/gentrace -scenario deliver -output ../../formal/DecoderTraceData.tla
//go:generate go run ./cmd/gentrace -scenario abort -output ../../formal/DecoderAbortTraceData.tla
//go:generate go run ./cmd/gentrace -scenario verify-fail -output ../../formal/DecoderVerifyFailureTraceData.tla
//go:generate go run ./cmd/gentrace -scenario rejected-calls -output ../../formal/DecoderRejectedCallsTraceData.tla
