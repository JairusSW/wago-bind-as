# Changelog

## 2026-09-11 - v0.1.0

- feat: add the Exact32 arena, records, spans, vectors, UTF-8 views, detached envelopes, and generation-based stale-view rejection.
- feat: add deterministic schema compilation and allocation-free Go and AssemblyScript code generation.
- feat: add serialized Wago prepared-call transactions plus retained wire-object calls.
- feat(transform): lower fixed-width scalar `@bind` classes to unmanaged AssemblyScript records and emit the shared schema.
- feat(transform): infer undecorated shared records and exported function bindings directly from AssemblyScript, generate a typed Go module facade during the same `asc` invocation, and verify the ABI fingerprint when binding Wago.
- feat(transform): refresh Go-authored `.bind.ts` imports before AssemblyScript parsing using `//wago:bind` declarations and canonical source fingerprints.
- perf(transform): generate typed caller-provided result wrappers so record copying and owned-result reclamation complete in one prepared Wasm call.
- perf(transform): replace AssemblyScript's `env.abort` import with a local `unreachable()` trap so Wago can use its isolated prepared-entry path.
- security: validate record, span, and vector ranges with widened arithmetic; zero reclaimed regions by default.
- fix(runtime): preserve region and generation provenance across generated child, reference, span, payload-allocation, and vector-growth operations; reject cross-arena use.
- fix(runtime): reject structural mutation through a stale vector descriptor snapshot.
- fix(transform): reject nested ownership paths, unsafe temporary record allocations, descriptor layout mismatches, unsupported scalar results, and ambiguous type identities.
- fix(generate): compile void facades correctly and reject collisions with generated Go and AssemblyScript names.
- test: add lifecycle, malformed-descriptor, deterministic-layout, real Wago round-trip, emitted-Wasm, race, and cross-architecture benchmarks.
- test: compile every supported facade arity and cover stale views, cross-region values, layout agreement, ownership control flow, type identity, and generated-name hygiene.
