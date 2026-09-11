<div align="center">
  <h1><code>wago-bind-as</code></h1>
  <p>
    <a href="https://github.com/JairusSW/wago-bind-as/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/JairusSW/wago-bind-as/actions/workflows/ci.yml/badge.svg"></a>
    <a href="https://www.npmjs.com/package/wago-bind-as"><img alt="npm" src="https://img.shields.io/npm/v/wago-bind-as"></a>
    <a href="https://pkg.go.dev/github.com/JairusSW/wago-bind-as"><img alt="Go Reference" src="https://pkg.go.dev/badge/github.com/JairusSW/wago-bind-as.svg"></a>
    <a href="LICENSE"><img alt="MIT License" src="https://img.shields.io/github/license/JairusSW/wago-bind-as"></a>
  </p>
  <p>Zero-copy, generated bindings between Go and AssemblyScript on Wago</p>
</div>

The bytes in WebAssembly linear memory are the object. Go and AssemblyScript access the same fields directly—there is no recursive serialization, reflection, field-name lookup, or duplicate guest object graph on the hot path.

> This repository is under active development. Exact32 version 1 is implemented; the schema and generator surface may still change before 1.0.

<details>
<summary>Table of Contents</summary>

- [Why wago-bind-as](#why-wago-bind-as)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [Go-authored models](#go-authored-models)
- [Ownership and safety](#ownership-and-safety)
- [Wire types](#wire-types)
- [Performance](#performance)
- [Development](#development)
- [License](#license)
- [Contact](#contact)

</details>

## Why wago-bind-as

Conventional bindings rebuild a host object as a guest object before useful work begins. That cost scales with everything in the object—even fields the guest never reads.

`wago-bind-as` uses a compact little-endian ABI called Exact32:

```text
Go producer ──writes──▶ one region in Wasm memory ◀──reads/mutates── AssemblyScript
```

- Scalars are fixed-offset loads and stores.
- `Utf8` and `Bytes` are 8-byte `(offset, length)` spans.
- `Vec<T>` is a 12-byte `(offset, length, capacity)` descriptor.
- References are 32-bit offsets relative to the region, never host pointers.
- Generated Go views use no reflection.
- Generated AssemblyScript accessors allocate nothing.
- Validation is lazy: reading two scalar fields never scans an untouched body.
- Arena generations reject stale Go views after reset or guest execution.
- `ReserveBytes` lets an I/O producer write directly into its final shared payload; `BeginImage`/`SealImage` create sanitized relocatable images when transport is needed.

## Installation

Install the AssemblyScript package and Go runtime:

```bash
npm install wago-bind-as
go get github.com/JairusSW/wago-bind-as
```

## Quick Start

Write ordinary AssemblyScript. Exported functions define the binding surface; reachable record classes define the shared types:

```ts
class Vec3 {
  x: f32;
  y: f32;
  z: f32;

  constructor(x: f32 = 0, y: f32 = 0, z: f32 = 0) {
    this.x = x;
    this.y = y;
    this.z = z;
  }
}

export function getVelocity(lastTick: Vec3, current: Vec3): Vec3 {
  return new Vec3(
    current.x - lastTick.x,
    current.y - lastTick.y,
    current.z - lastTick.z,
  );
}
```

Enable the transform and compile once:

```json
{
  "options": {
    "transform": ["wago-bind-as"]
  }
}
```

```bash
asc assembly/index.ts --transform wago-bind-as -o module.wasm
```

During that single compilation the transform infers the type graph and function signatures, updates `bindings/wago_bindings.bind.go`, injects allocator and ABI metadata, then lets AssemblyScript compile the already-lowered program. There is no user-authored schema or second AS compilation.

The generated Go package exposes native structs and prebound methods:

```go
module, err := bindings.Bind(instance)
if err != nil {
    return err
}

velocity, err := module.GetVelocity(
    bindings.Vec3{X: 1, Y: 2, Z: 3},
    bindings.Vec3{X: 4, Y: 8, Z: 5},
)
// velocity == bindings.Vec3{X: 3, Y: 6, Z: 2}
```

`Bind` checks the ABI fingerprint exported by the Wasm module, resolves functions once, and allocates reusable argument and result storage. Calls are serialized and copy only the fixed-size native values being adapted. The generated wire-view interface remains available for large, already-shared data.

The inferred function facade currently supports records made from fixed-width scalar fields. Void functions accept up to four record parameters; record-returning functions accept three because the fourth allocation-free call argument points to reusable result storage. A result returned directly as `new Result(...)` is treated as owned and reclaimed by the generated wrapper before it returns; returning one of the input parameters is borrowed. The transform rejects ambiguous ownership instead of guessing.

## Go-authored models

Go can be the source of truth when AssemblyScript should import an existing Go model. Point the declaration at a generated `.bind.ts` file:

```go
//wago:bind assembly/models.bind.ts
type Vec3 struct {
    X float32
    Y float32
    Z float32
}
```

AssemblyScript imports it as normal source:

```ts
import { Vec3 } from "./models.bind";
```

When `asc` starts, the transform checks Go declarations before parsing entry files. A missing or stale `.bind.ts` is regenerated atomically, so the same compiler invocation sees the current strongly typed class. Unchanged files are not rewritten.

Go-authored bind classes currently support exported fixed-width scalar fields. Use a `wago:"name"` struct tag when the AssemblyScript property name should differ from the Go field name.

Generated source records its provenance:

```ts
// Code generated by wago-bind-as. DO NOT EDIT.
// Generator: 0.1.0
// Source fingerprint: 51c3...
```

The resolved manifest remains an internal build artifact. A separate ABI fingerprint covers layouts and function signatures; generated Go refuses to bind a mismatched Wasm module.

## Ownership and safety

A region is the ownership and reclamation unit for the wire-view path. Related records and payloads are bump-allocated together and reset together, with no per-object free, reference count, or cross-language tracing collector. For the native function facade, a generated AssemblyScript wrapper copies a transient result into caller-provided storage and reclaims an owned value before the same Wasm call returns.

Important rules:

- Do not retain `Record`, `SpanView`, `UTF8View`, `Vector`, slices, or borrowed strings outside their documented scope.
- Reacquire views after guest execution. Descriptors may have changed.
- `BorrowedString` is explicitly unsafe to retain or use across mutation. `CopyString` owns its result.
- Mutable access is serialized. Do not invoke or close the same Wago instance through another handle concurrently.
- Host imports should use `WithGuestRegion`; it borrows memory through Wago's callback-scoped `GuestStorage` lifecycle gate.
- Reset zeroes initialized bytes by default. `WithoutResetZeroing` is only for a proven single-trust-domain hot loop.
- Bounds use widened arithmetic, and vector capacity—not only length—is validated before writable access.

## Wire types

| Schema type                      | Exact32 representation                        |          Size |
| -------------------------------- | --------------------------------------------- | ------------: |
| `bool`, `i8`…`u64`, `f32`, `f64` | Little-endian scalar                          |     1–8 bytes |
| `utf8`, `bytes`                  | `{ offset: u32, length: u32 }`                |       8 bytes |
| `ref<T>`                         | Region-relative offset                        |       4 bytes |
| `vec<T>`                         | `{ offset: u32, length: u32, capacity: u32 }` |      12 bytes |
| named record                     | Inline fixed-layout record                    |      resolved |
| `array<T, N>`                    | Inline contiguous fixed-size elements         | `sizeof(T)*N` |
| `timestamp_ns`, `duration_ns`    | Signed nanoseconds                            |       8 bytes |
| `uuid`, `digest256`              | Inline fixed bytes                            | 16 / 32 bytes |

Records are naturally aligned up to 8 bytes. The compiler sorts type definitions before fingerprinting, retains field order, rejects inline cycles, and permits cycles only through reference-like descriptors.

## Performance

The generated `Vec3` facade benchmark includes two native struct writes, one Wago call, the result read, and reclamation of the owned AssemblyScript result inside that call:

| Host                         | Typed `GetVelocity` call | Allocations |
| ---------------------------- | -----------------------: | ----------: |
| Apple M4 Max, Darwin/arm64   |          76.9–80.1 ns/op |           0 |
| Ryzen 7 7800X3D, Linux/amd64 |          69.0–70.7 ns/op |           0 |

Ten 750 ms samples were run with Go 1.26.5 on arm64 and Go 1.22.2 on amd64 against Wago `v0.1.0-beta.8`. The transform replaces AssemblyScript's imported `env.abort` with a local `unreachable()` trap, allowing Wago to select its import-free prepared-entry path. This preserves trapping but omits formatted abort messages and source locations.

The checked Wago binding benchmark calls a guest that validates a 56-byte root and UTF-8 span, compares the five-byte prefix `admin`, and changes one `f32` field.

| Host                         |      Empty body | 32 KiB untouched body | Allocations |
| ---------------------------- | --------------: | --------------------: | ----------: |
| Apple M4 Max, Darwin/arm64   | 74.3–75.3 ns/op |       74.9–76.0 ns/op |           0 |
| Ryzen 7 7800X3D, Linux/amd64 | 63.8–66.2 ns/op |       63.9–65.5 ns/op |           0 |

These are call/access measurements over an already-built wire object. The important result is that the untouched 32 KiB body does not change call latency.

Construction from the example native Go input is measured separately. It includes UTF-8 and body copies into final Wasm memory:

| Host                         | Empty body | 32 KiB body copy | Allocations |
| ---------------------------- | ---------: | ---------------: | ----------: |
| Apple M4 Max, Darwin/arm64   | 315–325 ns |     0.87–1.03 µs |           0 |
| Ryzen 7 7800X3D, Linux/amd64 | 588–597 ns |     1.35–1.37 µs |           0 |

Run the benchmark locally:

```bash
npm test # builds the generated Vec3 integration fixture
go test ./build/testdata/velocity -run '^$' -bench BenchmarkGeneratedVelocityCall -benchmem -benchtime=750ms -count=5

cd bench
go test -run '^$' -bench 'Benchmark(WagoBindAS|WagoPreparedFloor|BuildNativeInput)$' -benchmem -benchtime=750ms -count=5
```

The lower-level wire-view request example is in [`examples/request`](examples/request); the schema-free typed facade is exercised end-to-end by the transform integration test.

## Development

```bash
npm install
npm test
GOCACHE=/tmp/wago-bind-as-go-cache go test -race ./...
GOCACHE=/tmp/wago-bind-as-go-cache go vet ./...
```

See [CONTRIBUTING.md](CONTRIBUTING.md) before submitting a change. Security-sensitive issues should follow [SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE)

## Contact

- [GitHub Issues](https://github.com/JairusSW/wago-bind-as/issues)
- [me@jairus.dev](mailto:me@jairus.dev)
