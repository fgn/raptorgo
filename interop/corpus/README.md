# Reproducible interoperability corpus

These manifests pin the complete bytes emitted by the differential and
cross-decoding scenarios. The Taskfile regenerates every named artifact in a
temporary directory, checks its digest, compares independent implementations,
and then discards it. Keeping the compact golden digests rather than generated
payloads makes stale-oracle detection mandatory while avoiding checked-in
copies of large repetitive symbols.

- `raw.sha256`: 212 systematic/repair vectors shared by Go and Rust, including
  K/K' boundaries and ESI 16,777,215.
- `raw-release.sha256`: 66 Go/Rust vectors through RFC-maximum K=56,403,
  including large `T=7` and `T=255` sentinels.
- `cross.sha256`: byte-identical RQO1 frames and bidirectional decoding for
  source-only, repair-only, grouped source/repair, shortened source, multi-block,
  N>1 sub-block layouts, and combined Z=2/N=4 source loss plus grouped repair in
  every block.
- `cpp.sha256`: the 45-vector Go/Rust/C++ consensus set, bidirectional Go/C++
  repair-only and loss/reorder/duplicate decoding, deficient-set rejection, and
  the pinned C++ K=49 divergence control.
- `recovery-profile.txt`: bounded preregistered RFC Section 5.8 K'=10 trials
  from a fixed 65,536-ESI pool at overheads 0/1/2 with one-sided 95% confidence
  bounds; it is not an all-Table-2 proof.

RQO1 is only a deterministic test-corpus container; it is not an RFC wire
format.
