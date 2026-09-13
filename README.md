<div align="center">
  <h1><code>wago-bind-as</code></h1>
  <p>
    <a href="https://github.com/JairusSW/wago-bind-as/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/JairusSW/wago-bind-as/actions/workflows/ci.yml/badge.svg"></a>
    <a href="https://www.npmjs.com/package/wago-bind-as"><img alt="npm" src="https://img.shields.io/npm/v/wago-bind-as"></a>
    <a href="https://pkg.go.dev/github.com/JairusSW/wago-bind-as"><img alt="Go Reference" src="https://pkg.go.dev/badge/github.com/JairusSW/wago-bind-as.svg"></a>
    <a href="LICENSE"><img alt="MIT License" src="https://img.shields.io/github/license/JairusSW/wago-bind-as"></a>
  </p>
  <p>Generated, strongly typed bindings between Go and AssemblyScript on Wago</p>
</div>

Write normal AssemblyScript classes and functions. Add one transform. Call them from Go. You never write a schema.

## Quick start

### 1. Install

```bash
npm install --save-dev assemblyscript wago-bind-as
go get github.com/JairusSW/wago-bind-as github.com/wago-org/wago
```

### 2. Write AssemblyScript

Create `assembly/index.ts`:

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

No decorators, IDL, or schema file are required. Exported functions define the API; their classes define its types.

### 3. Compile once

```bash
npx asc assembly/index.ts \
  --transform wago-bind-as \
  --outFile build/module.wasm \
  --runtime stub \
  --optimize
```

The two files your application uses are:

```text
build/module.wasm
bindings/wago_bindings.bind.go
```

### 4. Call it from Go

Assuming your Go module is `example.com/velocity`:

```go
package main

import (
	"fmt"
	"os"

	bindings "example.com/velocity/bindings"
	wago "github.com/wago-org/wago"
)

func must[T any](value T, err error) T {
	if err != nil {
		panic(err)
	}
	return value
}

func main() {
	wasm := must(os.ReadFile("build/module.wasm"))
	compiled := must(wago.Compile(nil, wasm))
	instance := must(wago.Instantiate(compiled, wago.InstantiateOptions{}))
	defer instance.Close()

	api := must(bindings.Bind(instance))
	velocity := must(api.GetVelocity(
		bindings.Vec3{X: 1, Y: 2, Z: 3},
		bindings.Vec3{X: 4, Y: 8, Z: 5},
	))

	fmt.Println(velocity) // {3 6 2}
}
```

That's it. The transform inferred the ABI, generated the Go structs and methods, embedded a fingerprint on both sides, and compiled AssemblyScript once.

## How-to guides

### Put the transform in `asconfig.json`

Instead of passing `--transform` on every build:

```json
{
  "targets": {
    "release": {
      "outFile": "build/module.wasm",
      "optimizeLevel": 3,
      "shrinkLevel": 0
    }
  },
  "options": {
    "runtime": "stub",
    "transform": ["wago-bind-as"]
  }
}
```

Then compile with:

```bash
npx asc assembly/index.ts --target release
```

### Choose where generated files go

The defaults are `bindings/wago_bindings.bind.go`, `build/wago-bind.json`, and `build/wago-bind.manifest.json`. Override them when your project uses a different layout:

```bash
WAGO_BIND_PACKAGE=velocity \
WAGO_BIND_GO=internal/velocity/bindings.bind.go \
WAGO_BIND_SCHEMA=build/velocity.schema.json \
WAGO_BIND_MANIFEST=build/velocity.manifest.json \
npx asc assembly/index.ts --transform wago-bind-as -o build/module.wasm
```

The schema and manifest are generated build artifacts. Application authors do not maintain them.

### Start from a Go struct

Go can be the source of truth too. Point a Go declaration at a generated `.bind.ts` file:

```go
//wago:bind assembly/models.bind.ts
type Vec3 struct {
	X float32
	Y float32
	Z float32
}
```

Import it from AssemblyScript like any other typed source file:

```ts
import { Vec3 } from "./models.bind";

export function subtract(a: Vec3, b: Vec3): Vec3 {
  return new Vec3(a.x - b.x, a.y - b.y, a.z - b.z);
}
```

On the next `asc` run, the transform creates or refreshes `models.bind.ts` before parsing the program. A source fingerprint avoids rewriting an unchanged file, so Go and AssemblyScript stay strongly typed without an extra compile.

Use a `wago:"name"` struct tag when the AssemblyScript property name should differ from the Go field name.

### Share large payloads without rebuilding the object

The native facade above is the easiest API. It copies small fixed-size structs into reusable Wasm storage and performs no Go allocations.

For large objects, generated views let Go and AssemblyScript access the same bytes in Wasm memory. This is the zero-copy path:

```go
arena, err := bindas.NewArena(memory, 0, uint32(len(memory)))
if err != nil {
	return err
}

request, err := requestbindings.BuildRequest(arena, requestbindings.RequestInput{
	Id:   42,
	Name: "admin",
	Body: body,
})
if err != nil {
	return err
}

id, err := request.Id()
```

Scalar getters are fixed-offset loads. Strings, bytes, vectors, nested records, and references remain views over the same region. Reading `Id` does not scan or copy `Body`.

See [`examples/request`](examples/request) for the complete lower-level request model and guest call.

### Fill a payload directly

When data already comes from a file or network reader, reserve its final shared destination instead of staging another byte slice:

```go
span, destination, err := arena.ReserveBytes(contentLength, 1)
if err != nil {
	return err
}
if _, err := io.ReadFull(reader, destination); err != nil {
	return err
}
if err := request.SetBody(span); err != nil {
	return err
}
```

For text, call `arena.ValidateUTF8(span)` before assigning a directly filled span to a UTF-8 field.

### Detect stale or mixed-region values

Views remember their arena and generation:

```go
arena.Reset()
_, err := request.Id()
// errors.Is(err, bindas.ErrStale) == true
```

Generated child, reference, string, byte, and vector operations reject values from a different arena with `bindas.ErrRegionMismatch`.

## What gets generated

For the quick-start example, the transform generates:

- `bindings.Vec3`, a normal Go struct.
- `(*bindings.Module).GetVelocity`, a prebound typed call.
- Reusable Wasm argument and result storage.
- An Exact32 ABI manifest and fingerprint.
- AssemblyScript wrappers and allocator metadata in memory during the same compile.

`bindings.Bind` checks the Wasm fingerprint, prepares exported functions once, and rejects a mismatched module before the first call.

## Supported API

The inferred native facade currently accepts records made from fixed-width scalar fields:

```text
bool  i8  i16  i32  i64  u8  u16  u32  u64  f32  f64
```

Void functions accept up to four record parameters. Record-returning functions accept up to three because the fourth allocation-free call argument points to reusable result storage.

Return a newly owned value directly:

```ts
export function add(a: Vec3, b: Vec3): Vec3 {
  return new Vec3(a.x + b.x, a.y + b.y, a.z + b.z);
}
```

Or return one of the parameters as a borrowed value:

```ts
export function choose(value: Vec3): Vec3 {
  return value;
}
```

The transform rejects ownership it cannot prove. In particular, lowered records cannot be allocated as helper temporaries, and record returns inside branches or loops are not yet supported.

The lower-level shared-memory API supports the full Exact32 type surface:

| Type                          | Representation                                |
| ----------------------------- | --------------------------------------------- |
| Scalars                       | Little-endian fixed-width values              |
| `utf8`, `bytes`               | `{ offset: u32, length: u32 }`                |
| `ref<T>`                      | Region-relative `u32` offset                  |
| `vec<T>`                      | `{ offset: u32, length: u32, capacity: u32 }` |
| Named records                 | Inline fixed-layout records                   |
| `array<T, N>`                 | Inline fixed-size elements                    |
| `timestamp_ns`, `duration_ns` | Signed nanoseconds                            |
| `uuid`, `digest256`           | 16 or 32 inline bytes                         |

`Utf8` and `Bytes` are not yet accepted as properties of automatically lowered AssemblyScript classes. Use generated shared-memory views for those fields until descriptor property access is lowered explicitly.

## Ownership and safety

Exact32 stores values in a bounded region of Wasm linear memory. Related records and payloads are allocated and reset together; references are region-relative offsets, never host pointers.

- Reacquire views after guest execution because descriptors may have changed.
- Do not retain `Record`, `SpanView`, `UTF8View`, `Vector`, slices, or borrowed strings beyond their documented scope.
- `BorrowedString` aliases Wasm memory; `CopyString` owns its result.
- Mutable calls through one generated module are serialized.
- Host imports should open memory with `WithGuestRegion` so Wago's callback lifecycle invalidates escaped views.
- Reset zeroes initialized bytes by default. Use `WithoutResetZeroing` only inside a proven single-trust-domain loop.
- Bounds arithmetic is widened, vector capacity is validated, and stale structural mutations fail.

## How it works

The bytes in WebAssembly linear memory are the object:

```text
Go producer ──writes──▶ one Exact32 region ◀──reads/mutates── AssemblyScript
```

There is no reflection, field-name lookup, recursive serialization, or duplicate guest object graph on the shared-memory hot path. Generated scalar accessors become direct loads and stores; validation stays lazy, so untouched payloads do not affect call latency.

The project is under active development. Exact32 version 1 is implemented, but the schema and generator APIs may still change before 1.0.

## Performance

The generated `Vec3` facade benchmark includes two native struct writes, one Wago call, the result read, and owned-result reclamation:

| Host                         | Typed `GetVelocity` call | Allocations |
| ---------------------------- | -----------------------: | ----------: |
| Apple M4 Max, Darwin/arm64   |        73.03–73.74 ns/op |           0 |
| Ryzen 7 7800X3D, Linux/amd64 |        68.31–72.46 ns/op |           0 |

The checked shared-memory benchmark validates a 56-byte root and UTF-8 span, compares `admin`, and changes one `f32` field:

| Host                         |        Empty body | 32 KiB untouched body | Prepared-call floor | Allocations |
| ---------------------------- | ----------------: | --------------------: | ------------------: | ----------: |
| Apple M4 Max, Darwin/arm64   | 73.83–75.74 ns/op |     74.92–75.59 ns/op |   52.35–53.45 ns/op |           0 |
| Ryzen 7 7800X3D, Linux/amd64 | 63.97–66.56 ns/op |     63.96–67.06 ns/op |   42.08–43.22 ns/op |           0 |

The untouched 32 KiB payload does not change call latency because the guest never reads it. The prepared-call floor invokes the already-prepared, import-free guest directly without the binding layer's region checks.

Construction is measured separately and includes strings and body bytes copied into final Wasm memory:

| Host                         |     Empty body | 32 KiB body copy | Allocations |
| ---------------------------- | -------------: | ---------------: | ----------: |
| Apple M4 Max, Darwin/arm64   | 478.5–497.4 ns |   1.109–1.224 µs |           0 |
| Ryzen 7 7800X3D, Linux/amd64 | 905.2–916.1 ns |   1.655–1.782 µs |           0 |

These are ten 750 ms samples using wago-bind-as [`81c85c5`](https://github.com/JairusSW/wago-bind-as/commit/81c85c54512f830814ec726c025dba4ac5ea81c7) against Wago `main` at [`317a693`](https://github.com/wago-org/wago/commit/317a69310db31f2e0d8d184fc1504523e4a4ca64). The ARM64 host used Go 1.26.5; AMD64 used Go 1.22.2.

Run them locally:

```bash
npm test
go test ./build/testdata/velocity -run '^$' -bench BenchmarkGeneratedVelocityCall -benchmem -benchtime=750ms -count=10

cd bench
go test -run '^$' -bench 'Benchmark(WagoBindAS|WagoPreparedFloor|BuildNativeInput)$' -benchmem -benchtime=750ms -count=10
```

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
