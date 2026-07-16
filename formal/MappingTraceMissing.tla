--------------------- MODULE MappingTraceMissing ---------------------
EXTENDS MappingTraceMissingData

VARIABLE traceIndex

INSTANCE MappingTraceSpec
    WITH Trace <- ImplementationTrace,
         KPrimes <- ImplementationKPrimes,
         MaxESI <- ImplementationMaxESI,
         traceIndex <- traceIndex

=====================================================================
