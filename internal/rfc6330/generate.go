package rfc6330

//go:generate go run ./cmd/extracttables -rfc ../../rfc6330.txt -output tables_gen.go
//go:generate go run ./cmd/genmappingtrace -scenario valid -output ../../formal/MappingTraceData.tla
//go:generate go run ./cmd/genmappingtrace -scenario invalid -output ../../formal/MappingTraceInvalidData.tla
//go:generate go run ./cmd/genmappingtrace -scenario missing -output ../../formal/MappingTraceMissingData.tla
