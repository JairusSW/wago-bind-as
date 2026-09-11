# Modus performance research

Date: 2026-09-11

Studied revision: [`hypermodeinc/modus@b2a472c`](https://github.com/hypermodeinc/modus/tree/b2a472c0cf869fab67868d8f5067a3bd44609e58)

## Executive finding

Modus is optimized for low application latency, not for the minimum possible Go-to-Wasm call latency. Its strongest ideas are to do discovery once, compile once, construct a typed execution plan once, and make aggregate returns use caller-provided memory. Those ideas transfer well to `wago-bind-as`. Its reflective `map[string]any` boundary, per-invocation module instantiation, tracing, recovery, cleaners, and managed AssemblyScript object adapters do not.

The most valuable transfer was result indirection. Previously an owned record result required two Wago crossings: call the user's function, copy the returned object, then call `__wbas_free`. The transform now emits one hidden typed wrapper. Go allocates stable result storage once during `Bind`; the wrapper calls the user's unchanged function, copies the fields into that storage, and frees the transient result before returning. The generated Go method makes one prepared call and reads the stable slot.

On an Apple M4 Max, the complete `Vec3` facade first improved from roughly **280 ns/op** to **149.5–153.2 ns/op** through result indirection. Installing an import-free `abort` override in the transform then reduced it to a ten-run range of **76.9–80.1 ns/op**, with **0 B/op and 0 allocs/op**. Ten matched samples on a Ryzen 7 7800X3D measured **69.0–70.7 ns/op**, also with zero allocations. Removing `env.abort` lets Wago select its isolated prepared-entry path; the remaining time is the typed facade's two argument writes, guest allocation/copy/free, one Wago call, and result read.

## What Modus actually does

### Compile and plan outside the request path

Modus owns one wazero runtime and stores `wazero.CompiledModule` on each plugin. Module bytes are compiled at load time, not for each function call. A plugin then builds an `ExecutionPlans` map for every imported and exported function from embedded language metadata and the compiled Wasm signatures. See [`wasmhost.go`](https://github.com/hypermodeinc/modus/blob/b2a472c0cf869fab67868d8f5067a3bd44609e58/runtime/wasmhost/wasmhost.go) and [`plugins.go`](https://github.com/hypermodeinc/modus/blob/b2a472c0cf869fab67868d8f5067a3bd44609e58/runtime/plugins/plugins.go).

The language planners cache type descriptions and type handlers. An execution plan retains parameter handlers, result handlers, whether result indirection is needed, and the indirect result size. This keeps type discovery and ABI decisions out of the ordinary call path. See the [AssemblyScript planner](https://github.com/hypermodeinc/modus/blob/b2a472c0cf869fab67868d8f5067a3bd44609e58/runtime/languages/assemblyscript/planner.go), [Go planner](https://github.com/hypermodeinc/modus/blob/b2a472c0cf869fab67868d8f5067a3bd44609e58/runtime/languages/golang/planner.go), and [`ExecutionPlan`](https://github.com/hypermodeinc/modus/blob/b2a472c0cf869fab67868d8f5067a3bd44609e58/runtime/langsupport/executionplan.go).

`wago-bind-as` already follows the deeper version of this pattern: code generation specializes record readers and writers, `Bind` prepares each Wago function once, and argument addresses are allocated once. There is no runtime reflection or handler lookup to remove.

### Use caller-provided memory for aggregate results

The Go planner detects TinyGo's indirect-return ABI: metadata says a result exists, but the Wasm signature has no direct result. It calculates the required size and the execution plan allocates result memory, passes its pointer as the first Wasm argument, and reads the result after the call. This is the direct inspiration for the new generated wrapper here. See [`getIndirectResultSize`](https://github.com/hypermodeinc/modus/blob/b2a472c0cf869fab67868d8f5067a3bd44609e58/runtime/languages/golang/planner.go) and the [indirect parameter/result path](https://github.com/hypermodeinc/modus/blob/b2a472c0cf869fab67868d8f5067a3bd44609e58/runtime/langsupport/executionplan.go).

The adaptation here is deliberately more specialized than Modus:

```ts
// Generated in memory; the user's getVelocity remains unchanged.
export function __wbas_call_getVelocity(
  lastTick: Vec3,
  current: Vec3,
  result: Vec3,
): void {
  const value = getVelocity(lastTick, current);
  memory.copy(changetype<usize>(result), changetype<usize>(value), 12);
  heap.free(changetype<usize>(value));
}
```

The corresponding Go call is a precomputed `Invoke3(arg0Ptr, arg1Ptr, resultPtr)`. There is no returned pointer to validate and no second prepared call for reclamation.

### Generate metadata from source

Modus's AssemblyScript transform walks exported/imported functions and recursively reachable classes, then emits deterministic metadata into Wasm custom sections. That provides strong language-aware descriptions without a handwritten schema. See the [extractor](https://github.com/hypermodeinc/modus/blob/b2a472c0cf869fab67868d8f5067a3bd44609e58/sdk/assemblyscript/src/transform/src/extractor.ts) and [custom-section writer](https://github.com/hypermodeinc/modus/blob/b2a472c0cf869fab67868d8f5067a3bd44609e58/sdk/assemblyscript/src/transform/src/metadata.ts).

This validates the current `wago-bind-as` direction: infer the schema from the AssemblyScript compiler AST, generate Go during the same `asc --transform` run, and fail compilation on ambiguous ownership. Embedding the manifest in a custom section is a possible packaging improvement, but it does not reduce hot-path latency; the existing fingerprint exports already make mismatches fail at `Bind`.

### Compile for speed and control the runtime surface

The Modus AssemblyScript release target uses optimization level 3, raw bindings, and explicit runtime overrides. Its abort override still calls host logging and WASI exit functions, so copying it would preserve imports rather than unlock Wago's isolated call path. See [`plugin.asconfig.json`](https://github.com/hypermodeinc/modus/blob/b2a472c0cf869fab67868d8f5067a3bd44609e58/sdk/assemblyscript/src/plugin.asconfig.json) and [`modus_abort.ts`](https://github.com/hypermodeinc/modus/blob/b2a472c0cf869fab67868d8f5067a3bd44609e58/sdk/assemblyscript/src/assembly/overrides/modus_abort.ts).

Modus also uses anonymous module names when instantiating a compiled module for concurrency and performance, referencing [wazero PR 2275](https://github.com/tetratelabs/wazero/pull/2275). That is relevant to a multi-instance wazero server but not to the existing long-lived Wago instance used by this binding facade.

## What not to copy

The general Modus boundary handles dynamic application inputs. Each call accepts `map[string]any`, creates parameter slices and cleaners, invokes handler interfaces, creates tracing scopes and spans, installs panic recovery, and may allocate/pin/unpin managed AssemblyScript objects. Modus also instantiates a module from the cached compiled artifact for a single invocation. Those are reasonable flexibility and isolation tradeoffs, but they are the opposite of a nanosecond-scale typed facade.

No relevant raw Wasm-call benchmark was found in the repository. Consequently, claims that Modus itself has a faster language boundary would not be evidence-based. Its documented architecture and commit history support system-level startup, concurrency, cache, and JSON improvements; they do not establish a lower raw call floor than Wago.

## Applied design and remaining opportunity

The implementation takes these Modus principles:

1. Infer and validate the ABI at compile time.
2. Generate specialized code instead of runtime reflection.
3. Prepare functions and allocate argument/result slots once.
4. Return records through caller-owned memory.
5. Complete owned-result reclamation inside the same Wasm call.

It intentionally rejects result-bearing functions with more than three record parameters because the result slot consumes Wago's fourth allocation-free prepared argument. Void functions can still have four.

The transform now also applies the safe part of Modus's runtime-override pattern. During transform construction, before AssemblyScript resolves its standard library, it aliases the global `abort` to a generated library function whose body is `unreachable()`. The resulting module has no `env.abort` import, so Wago can use its isolated prepared-entry path. Tests inspect the compiled Wasm import table, instantiate it without host imports, exercise the generated Go call, and verify that an explicit AssemblyScript `abort()` still traps.

This is an intentional diagnostics tradeoff: trap behavior is preserved, but the abort message, source file, line, and column are not delivered to a host formatter. Applications that require formatted guest assertions should not use this import-free policy without adding a separate out-of-band diagnostics design; restoring a host `abort` import also restores Wago's synchronized call path.

## Research method and limitations

The official repository was cloned and inspected at the pinned revision above. Git history was searched for performance-specific changes, and a demand-driven graph was built over the 254-file execution-critical `runtime/` corpus: 2,767 graph nodes, 5,679 edges, and a measured 18.1x token reduction for representative graph queries. The graph reported dangling external-type edges and edge collapsing in its undirected projection, so it was used for navigation only; every conclusion above was checked in primary source files. The repository is archived and read-only, so these findings describe its final archived revision rather than an actively evolving main branch.

## Sources

- [Modus repository at the studied revision](https://github.com/hypermodeinc/modus/tree/b2a472c0cf869fab67868d8f5067a3bd44609e58)
- [Wasm host compilation and instantiation](https://github.com/hypermodeinc/modus/blob/b2a472c0cf869fab67868d8f5067a3bd44609e58/runtime/wasmhost/wasmhost.go)
- [Plugin execution-plan construction](https://github.com/hypermodeinc/modus/blob/b2a472c0cf869fab67868d8f5067a3bd44609e58/runtime/plugins/plugins.go)
- [Execution plan and indirect results](https://github.com/hypermodeinc/modus/blob/b2a472c0cf869fab67868d8f5067a3bd44609e58/runtime/langsupport/executionplan.go)
- [TinyGo result-indirection detection](https://github.com/hypermodeinc/modus/blob/b2a472c0cf869fab67868d8f5067a3bd44609e58/runtime/languages/golang/planner.go)
- [AssemblyScript transform extractor](https://github.com/hypermodeinc/modus/blob/b2a472c0cf869fab67868d8f5067a3bd44609e58/sdk/assemblyscript/src/transform/src/extractor.ts)
- [AssemblyScript runtime configuration](https://github.com/hypermodeinc/modus/blob/b2a472c0cf869fab67868d8f5067a3bd44609e58/sdk/assemblyscript/src/plugin.asconfig.json)
