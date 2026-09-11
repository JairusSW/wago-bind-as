# Changelog

## 2026-09-11 - v0.1.0

- feat: add the Exact32 arena, records, spans, vectors, UTF-8 views, detached envelopes, and generation-based stale-view rejection.
- feat: add deterministic schema compilation and allocation-free Go and AssemblyScript code generation.
- feat: add serialized Wago prepared-call transactions plus retained wire-object calls.
- feat(transform): lower scalar and packed-span `@bind` classes to unmanaged AssemblyScript records and emit the shared schema.
- feat(transform): infer undecorated shared records and exported function bindings directly from AssemblyScript, generate a typed Go module facade during the same `asc` invocation, and verify the ABI fingerprint when binding Wago.
- feat(transform): refresh Go-authored `.bind.ts` imports before AssemblyScript parsing using `//wago:bind` declarations and canonical source fingerprints.
- perf(transform): generate typed caller-provided result wrappers so record copying and owned-result reclamation complete in one prepared Wasm call.
- perf(transform): replace AssemblyScript's `env.abort` import with a local `unreachable()` trap so Wago can use its isolated prepared-entry path.
- security: validate record, span, and vector ranges with widened arithmetic; zero reclaimed regions by default.
- test: add lifecycle, malformed-descriptor, deterministic-layout, real Wago round-trip, emitted-Wasm, race, and cross-architecture benchmarks.
