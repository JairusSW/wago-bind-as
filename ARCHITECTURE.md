# Architecture

`wago-bind-as` is a shared-memory object ABI, not a serializer. The authoritative value is an Exact32 region in WebAssembly linear memory. Generated Go and AssemblyScript code are typed views over that region.

Users do not author an ABI schema. Exported AssemblyScript function signatures and their reachable classes form the default binding surface. An explicitly marked Go struct can instead be a source declaration for an imported `.bind.ts`; the transform refreshes that file before AssemblyScript parses entry modules. The resolved schema is an internal, deterministic compiler artifact.

## Exact32 v1

- little-endian
- 32-bit offsets relative to the region base
- fixed-width record fields
- natural field alignment capped at 8 bytes
- no host pointers, object headers, vtables, field directories, or per-object schema IDs
- one 128-bit fingerprint for the complete resolved schema

The detached 32-byte envelope is:

| Offset | Field                      |
| -----: | -------------------------- |
|      0 | `WBAS` magic               |
|      4 | `u16` format version       |
|      6 | `u16` flags                |
|      8 | `u32` initialized extent   |
|     12 | `u32` root offset          |
|     16 | 128-bit schema fingerprint |

Live calls bind schema and function identity once and do not interpret this header per access.

## Access paths

The checked path uses `(regionBase, regionLength, rootOffset)`. Generated accessors compile offsets and strides into code. Fixed record extent is checked once; spans and vectors are snapshotted and checked only when accessed.

The property path turns classes reachable from exported record functions into AssemblyScript unmanaged records. A record parameter is its absolute linear-memory address, so native scalar property operations are already direct Exact32 accesses. The transform generates a Go `Module` facade, reusable argument and result storage, typed result-indirection wrappers, hidden allocator and fingerprint exports, and native structs during the same `asc` invocation. Explicit `@bind` remains as a compatibility mechanism for lower-level pointer-shaped exports.

For native facade results, Go passes a reusable result pointer as the wrapper's final argument. The wrapper calls the user function and copies fields into that slot. A direct `return new T(...)` establishes transfer ownership, so the wrapper frees the transient object before returning; a returned record parameter is borrowed and is not freed. Other result flows are rejected until ownership can be represented explicitly rather than inferred unsafely. This keeps copying and reclamation within one Go-to-Wasm invocation.

Go-authored source declarations use `//wago:bind path/to/models.bind.ts`. Transform initialization scans these declarations and atomically refreshes missing or stale bind artifacts before parsing, preserving normal AssemblyScript imports and editor tooling. A source fingerprint controls regeneration; the distinct ABI fingerprint covers resolved layouts and function signatures and is checked once when Go binds the instantiated module.

## Ownership

`Arena` is a phase-owned bump allocator over a bounded memory range. `Reset` clears used bytes and increments a generation. Guest execution refreshes the host memory slice and increments the generation without reclaiming the data. Every Go view carries the generation it was opened under and fails with `ErrStale` after either transition.

`Prepared.Transaction` is the default boundary:

```text
refresh → reset → host build → guest invoke → refresh → host read → zero/reset
```

`Prepared.Call` retains the image and is intended for repeated processing of already-wire-native data.

## Validation

All host range calculations widen before addition or multiplication. A span is valid when `offset <= limit` and `length <= limit - offset`. A vector additionally requires `length <= capacity`, aligned payload storage, and `capacity * stride` representability and containment.

Validation snapshots descriptor words before checking them. Guest invocation invalidates cached views. The format does not treat read-only generated APIs as a guest sandbox: Wago capability policy remains the authority boundary.

## Strings

The interoperable default is UTF-8. Go exposes allocation-free comparison plus explicit `CopyString` and low-level `BorrowedString`. AssemblyScript exposes byte-oriented comparison helpers so hot paths can keep text encoded. Native UTF-16 conversion is an explicit future adapter, never an implicit getter behavior.

## Roadmap

1. Complete property lowering for vector, reference, optional, and union fields without wrapper allocation.
2. Generate schema-specialized validators in addition to the current bounded manifest validator.
3. Add maps, tagged unions, tensors, and batch/columnar profiles when measured workloads justify them.
4. Add compact export for detached transport; the current dense export already sanitizes initialized bytes and alignment gaps.
5. Evaluate a dedicated Wago pre/post-invocation memory scope only if public APIs cannot preserve a required lifecycle invariant.
