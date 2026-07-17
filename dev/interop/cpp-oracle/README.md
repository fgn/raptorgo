# Pinned C++ differential oracle

This development-only adapter compiles against `libRaptorQ` at commit
`a394b22af6e8fc742ce88eacf9af5343ecded9d7` from
`https://github.com/LucaFulchir/libRaptorQ`. The pinned GitHub archive has
SHA-256:

```text
2cc35b7b68fb3d7624d1be2660099be75373c018e6d3cb84203a231b37485892
```

The source is fetched into a temporary build directory and is never linked
into the Go package. `libRaptorQ` is LGPL-3.0-or-later; its bundled Eigen copy
contains its own notices. The adapter itself is repository test tooling.

The RAW API requires a Table-2 extended block size, so its consensus corpus is
limited to `K=K'` cases. `K=10,12,48` agree byte-for-byte with Go and the pinned
Rust RFC implementation for source and repair payloads. At `K=49`, current
`libRaptorQ` agrees on systematic payloads but produces different repair
payloads while Go and Rust agree with each other. That case is deliberately
excluded from the consensus set and retained as a documented independent
implementation divergence rather than changing the RFC implementation to
match it.

