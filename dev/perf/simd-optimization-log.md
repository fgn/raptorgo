# Experimental SIMD optimization log

This log records the evidence for the opt-in Go 1.26 `simd/archsimd` path.
The default build remains scalar. The SIMD implementation is selected only by
the `goexperiment.simd` build tag, checks AVX2 at runtime, and falls back to the
same scalar kernels when AVX2 is unavailable.

## Scope and invariants

The optimized operations are the two byte-vector primitives used by hybrid
inactivation execution:

- XOR, including exact aliasing and partial-overlap snapshot semantics;
- `destination ^= coefficient * source` over RFC 6330 GF(256), whose reduction
  polynomial is 0x11d rather than the AES/GFNI polynomial 0x11b.

The public functions still reject length mismatches without a partial write.
Hazardous forward overlap retains the backward scalar traversal. Non-amd64,
default, and runtime AVX2-disabled builds retain scalar execution. The hottest
reverse-substitution loops now call the checked XOR primitive instead of
duplicating byte loops, ensuring both scalar and SIMD builds exercise one
semantic boundary.

## Implementation

The SIMD build uses `archsimd.Uint8x32`:

- XOR loads, XORs, and stores 32 bytes per iteration;
- `MulAdd` computes the eight powers of the source vector with `VPADDB`, signed
  high-bit masks, and conditional reduction by 0x1d, XORs the powers selected
  by the scalar coefficient, then XORs the product into the destination;
- scalar helpers process tails shorter than 32 bytes and hazardous overlap.

`Uint8x32.GaloisFieldMul` is deliberately not used. In Go 1.26 it requires
AVX-512 GFNI and implements the incompatible polynomial 0x11b. The
i9-14900HX test host supports AVX2 but not AVX-512.

## Rejected nibble-shuffle experiment

The first `MulAdd` experiment precomputed two 16-entry product tables per
coefficient and used AVX2 `VPSHUFB` for low/high-nibble lookup. It was correct
but regressed the 1,280-byte kernel from about 1.00 microsecond scalar to 4.44
microseconds SIMD on Go 1.26.5. It was removed. The retained polynomial-step
implementation measured about 223 ns on the same workload.

## Environment and paired methodology

- CPU: Intel Core i9-14900HX, CPU 8 (P-core), linux/amd64;
- Go: go1.26.5;
- build: `GOEXPERIMENT=simd GOAMD64=v3` for both frozen end-to-end binaries;
- execution: `taskset -c 8`, `GOMAXPROCS=1`;
- end-to-end samples: ten alternating binary pairs, one benchmark iteration;
- kernel samples: ten alternating scalar/SIMD binary pairs, 500 ms minimum.

Frozen end-to-end binary hashes:

```text
969d23e831c24869a39a66093a0beb3f074f6089f79ca5383bf8cefb601ece40  raptorgo-simd-base.test
2e5443284dff1b3804e4fefd50aadd06e6d948d3da687ca273ba2fa5a3d364fc  raptorgo-simd-candidate-final.test
```

Kernel binary hashes:

```text
61d7fb08b4a164bdbf8953dc4fa55e9c4e87bd760cdfafa101154755571fbabf  gf256-scalar.test
41b499d8a20672db7d0263e136915e3373fd9c871719dcc90db70aff8cb9db61  gf256-simd-final.test
```

Equivalent end-to-end sample commands:

```sh
taskset -c 8 env GOMAXPROCS=1 ./raptorgo-simd-base.test \
  -test.run '^$' \
  -test.bench '^BenchmarkObjectEncoderPacket/construct-and-repair/K=10000/T=1280$' \
  -test.benchmem -test.benchtime=1x -test.count=1

taskset -c 8 env GOMAXPROCS=1 ./raptorgo-simd-candidate-final.test \
  -test.run '^$' \
  -test.bench '^BenchmarkObjectDecoder/K=10000/T=1280/loss=1000/overhead=4$' \
  -test.benchmem -test.benchtime=1x -test.count=1
```

## Results

| Kernel, 1,280 bytes | Scalar median | SIMD median | SIMD range | Median paired speedup | B/op |
|---|---:|---:|---:|---:|---:|
| XOR | 361.0 ns | 58.48 ns | 58.12–58.64 ns | 6.18x | 0 |
| GF(256) `MulAdd` | 1,002.5 ns | 223.1 ns | 222.6–223.7 ns | 4.49x | 0 |

| K=10,000, T=1280 workload | Scalar median | SIMD median | SIMD range | Median paired speedup |
|---|---:|---:|---:|---:|
| Construct + first repair | 644.8 ms | 233.3 ms | 232.4–234.1 ms | 2.77x |
| Decode, 10% loss + 4 overhead | 669.7 ms | 227.1 ms | 225.3–234.0 ms | 2.95x |

Construct-plus-repair retained 105,204,320 B/op and 125 steady allocations.
Decode retained 122,861,984 B/op and 10,073 steady allocations. SIMD changes
execution width, not workspace accounting or ownership.

A five-iteration profile of the final code measured construct-plus-repair at
236.35 ms/op. `xorForward` was 22.69% cumulative, vector polynomial
multiplication was 6.72% flat, and `runtime.memmove` was 7.56% flat. The dense
matrix traversal path was 50.42% cumulative, with active-column planning at
6.72% flat. The previous profile's single 56% scalar substitution loop is no
longer dominant; traversal and planning are now the larger optimization
surface.

## Validation

The focused validation matrix includes:

- exhaustive coefficient/value comparison against independent polynomial
  arithmetic in scalar and SIMD builds;
- vector-boundary and tail lengths from 0 through 1,280 bytes;
- separate, exact-alias, destination-before-source, and
  destination-after-source layouts;
- default amd64, SIMD amd64, runtime AVX2-disabled SIMD, default 386, and
  `GOEXPERIMENT=simd` 386 fallback builds;
- default and SIMD vet, pure-Go, checkptr, race, fuzz, and benchmark-smoke
  gates;
- the repository's complete default correctness gate and a SIMD-enabled
  generated-artifact/static verification pass.

Reproduction entry points:

```sh
task test:simd
task bench:simd:smoke
task bench:simd
```
