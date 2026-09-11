import assert from "node:assert/strict";
import { mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import test from "node:test";

test("@bind lowers to an allocation-free unmanaged record", async () => {
  rmSync("build/testdata/transform-fixture", { recursive: true, force: true });
  const result = spawnSync(
    process.execPath,
    [
      "node_modules/assemblyscript/bin/asc.js",
      "transform/__tests__/fixture.ts",
      "--transform",
      "./transform/lib/index.js",
      "--outFile",
      "build/testdata/transform-fixture/fixture.wasm",
      "--textFile",
      "build/testdata/transform-fixture/fixture.wat",
      "--runtime",
      "stub",
      "--optimize",
    ],
    {
      encoding: "utf8",
      env: {
        ...process.env,
        WAGO_BIND_SCHEMA: "build/testdata/transform-fixture/schema.json",
        WAGO_BIND_MANIFEST: "build/testdata/transform-fixture/manifest.json",
        WAGO_BIND_GO: "build/testdata/transform-fixture/bindings.bind.go",
        WAGO_BIND_PACKAGE: "fixture",
      },
    },
  );
  assert.equal(result.status, 0, result.stderr || result.stdout);
  const wat = readFileSync(
    "build/testdata/transform-fixture/fixture.wat",
    "utf8",
  );
  assert.match(wat, /f32\.load offset=16/);
  assert.match(wat, /f32\.store offset=16/);
  assert.doesNotMatch(wat, /call .*__new|call .*~lib\/rt/);
  const wasm = await WebAssembly.instantiate(
    readFileSync("build/testdata/transform-fixture/fixture.wasm"),
    {
      env: {
        abort() {
          throw new Error("AssemblyScript abort");
        },
      },
    },
  );
  const exports = wasm.instance.exports;
  assert.equal(exports.requestSize(), 40);
  assert.equal(exports.enabledOffset(), 8);
  assert.equal(exports.flagsOffset(), 12);
  assert.equal(exports.scoreOffset(), 16);
  assert.equal(exports.ratioOffset(), 24);
  assert.equal(exports.nameOffset(), 32);
  const schema = JSON.parse(
    readFileSync("build/testdata/transform-fixture/schema.json", "utf8"),
  );
  assert.deepEqual(schema.types[0], {
    name: "Request",
    fields: [
      { name: "id", type: "u64" },
      { name: "enabled", type: "bool" },
      { name: "priority", type: "u8" },
      { name: "code", type: "i16" },
      { name: "flags", type: "u32" },
      { name: "score", type: "f32" },
      { name: "ratio", type: "f64" },
      { name: "name", type: "utf8" },
    ],
  });
  const generated = spawnSync(
    "go",
    [
      "run",
      "./cmd/wago-bind-as",
      "generate",
      "-schema",
      "build/testdata/transform-fixture/schema.json",
      "-manifest",
      "build/testdata/transform-fixture/manifest.json",
    ],
    { encoding: "utf8" },
  );
  assert.equal(generated.status, 0, generated.stderr || generated.stdout);
  const manifest = JSON.parse(
    readFileSync("build/testdata/transform-fixture/manifest.json", "utf8"),
  );
  assert.equal(manifest.types[0].size, exports.requestSize());
  assert.deepEqual(
    manifest.types[0].fields.map((field) => field.offset),
    [0, 8, 9, 10, 12, 16, 24, 32],
  );
});

test("@bind rejects records with no wire extent", () => {
  const result = spawnSync(
    process.execPath,
    [
      "node_modules/assemblyscript/bin/asc.js",
      "transform/__tests__/empty.ts",
      "--transform",
      "./transform/lib/index.js",
      "--outFile",
      "build/testdata/transform-fixture/empty.wasm",
    ],
    { encoding: "utf8" },
  );
  assert.notEqual(result.status, 0);
  assert.match(
    result.stderr + result.stdout,
    /needs at least one instance field/,
  );
});

test("exported record functions generate callable Go bindings in one asc pass", () => {
  rmSync("build/testdata/velocity", { recursive: true, force: true });
  const compile = spawnSync(
    process.execPath,
    [
      "node_modules/assemblyscript/bin/asc.js",
      "transform/__tests__/velocity.ts",
      "--transform",
      "./transform/lib/index.js",
      "--outFile",
      "build/testdata/velocity/velocity.wasm",
      "--runtime",
      "stub",
      "--optimize",
    ],
    {
      encoding: "utf8",
      env: {
        ...process.env,
        WAGO_BIND_SCHEMA: "build/testdata/velocity/schema.json",
        WAGO_BIND_MANIFEST: "build/testdata/velocity/manifest.json",
        WAGO_BIND_GO: "build/testdata/velocity/bindings.bind.go",
        WAGO_BIND_PACKAGE: "velocitybindings",
      },
    },
  );
  assert.equal(compile.status, 0, compile.stderr || compile.stdout);
  assert.doesNotMatch(compile.stderr, /AS235/);
  const generated = readFileSync(
    "build/testdata/velocity/bindings.bind.go",
    "utf8",
  );
  assert.match(generated, /type Vec3 struct|type Vec3 = Vec3Input/);
  assert.match(generated, /func \(m \*Module\) GetVelocity\(/);
  assert.match(generated, /PrepareFunction\("__wbas_call_getVelocity"\)/);
  assert.match(generated, /getVelocityResult uint32/);
  assert.doesNotMatch(generated, /__wbas_free|releaseErr/);
  const wasmModule = new WebAssembly.Module(
    readFileSync("build/testdata/velocity/velocity.wasm"),
  );
  assert.deepEqual(WebAssembly.Module.imports(wasmModule), []);
  const exportNames = WebAssembly.Module.exports(wasmModule).map(
    (value) => value.name,
  );
  assert.ok(exportNames.includes("__wbas_call_getVelocity"));
  assert.ok(!exportNames.includes("__wbas_free"));
  const jsInstance = new WebAssembly.Instance(wasmModule, {});
  assert.throws(
    () => jsInstance.exports.forceAbort(),
    WebAssembly.RuntimeError,
  );

  writeFileSync(
    "build/testdata/velocity/bindings_test.go",
    `package velocitybindings

import (
  "os"
  "testing"
  wago "github.com/wago-org/wago"
)

func TestGeneratedVelocityCall(t *testing.T) {
  wasm, err := os.ReadFile("velocity.wasm")
  if err != nil { t.Fatal(err) }
  compiled, err := wago.Compile(nil, wasm)
  if err != nil { t.Fatal(err) }
  instance, err := wago.Instantiate(compiled, wago.InstantiateOptions{})
  if err != nil { t.Fatal(err) }
  defer instance.Close()
  module, err := Bind(instance)
  if err != nil { t.Fatal(err) }
  got, err := module.GetVelocity(Vec3{X: 1, Y: 2, Z: 3}, Vec3{X: 4, Y: 8, Z: 5})
  if err != nil { t.Fatal(err) }
  if got != (Vec3{X: 3, Y: 6, Z: 2}) { t.Fatalf("velocity = %#v", got) }
  memorySize := len(instance.Memory().UnsafeBytes())
  for i := 0; i < 10000; i++ {
    if _, err := module.GetVelocity(Vec3{X: 1}, Vec3{X: 2}); err != nil { t.Fatal(err) }
  }
  if got := len(instance.Memory().UnsafeBytes()); got != memorySize { t.Fatalf("guest memory grew from %d to %d", memorySize, got) }
}

func BenchmarkGeneratedVelocityCall(b *testing.B) {
  wasm, err := os.ReadFile("velocity.wasm")
  if err != nil { b.Fatal(err) }
  compiled, err := wago.Compile(nil, wasm)
  if err != nil { b.Fatal(err) }
  instance, err := wago.Instantiate(compiled, wago.InstantiateOptions{})
  if err != nil { b.Fatal(err) }
  defer instance.Close()
  module, err := Bind(instance)
  if err != nil { b.Fatal(err) }
  last := Vec3{X: 1, Y: 2, Z: 3}
  current := Vec3{X: 4, Y: 8, Z: 5}
  var got Vec3
  b.ReportAllocs()
  b.ResetTimer()
  for i := 0; i < b.N; i++ {
    got, err = module.GetVelocity(last, current)
    if err != nil { b.Fatal(err) }
  }
  b.StopTimer()
  if got != (Vec3{X: 3, Y: 6, Z: 2}) { b.Fatalf("velocity = %#v", got) }
}
`,
  );
  const goTest = spawnSync("go", ["test", "./build/testdata/velocity"], {
    encoding: "utf8",
  });
  assert.equal(goTest.status, 0, goTest.stderr || goTest.stdout);
});

test("the transform refreshes a stale Go-authored .bind.ts before parsing", () => {
  rmSync("build/testdata/go-first", { recursive: true, force: true });
  mkdirSync("build/testdata/go-first", { recursive: true });
  writeFileSync(
    "build/testdata/go-first/models.go",
    `package model

//wago:bind models.bind.ts
type Vec3 struct {
  X float32
  Y float32
  Z float32
}
`,
  );
  writeFileSync(
    "build/testdata/go-first/main.ts",
    `import { Vec3 } from "./models.bind";

export function subtract(a: Vec3, b: Vec3): Vec3 {
  return new Vec3(a.x - b.x, a.y - b.y, a.z - b.z);
}
`,
  );
  writeFileSync(
    "build/testdata/go-first/models.bind.ts",
    `// Code generated by wago-bind-as. DO NOT EDIT.
// Generator: 0.0.0
// Source fingerprint: 00000000000000000000000000000000

@unmanaged
export class Vec3 {
  x: f32;
  constructor(x: f32 = 0) { this.x = x; }
}
`,
  );
  const compile = spawnSync(
    process.execPath,
    [
      "node_modules/assemblyscript/bin/asc.js",
      "build/testdata/go-first/main.ts",
      "--transform",
      "./transform/lib/index.js",
      "--outFile",
      "build/testdata/go-first/module.wasm",
      "--runtime",
      "stub",
      "--optimize",
    ],
    {
      encoding: "utf8",
      env: {
        ...process.env,
        WAGO_BIND_SCHEMA: "build/testdata/go-first/schema.json",
        WAGO_BIND_MANIFEST: "build/testdata/go-first/manifest.json",
        WAGO_BIND_GO: "build/testdata/go-first/bindings.bind.go",
        WAGO_BIND_PACKAGE: "gofirstbindings",
        WAGO_BIND_SYNC_ROOT: "build/testdata/go-first",
      },
    },
  );
  assert.equal(compile.status, 0, compile.stderr || compile.stdout);
  const assembly = readFileSync(
    "build/testdata/go-first/models.bind.ts",
    "utf8",
  );
  assert.match(assembly, /Source fingerprint: [0-9a-f]{32}/);
  assert.doesNotMatch(assembly, /Source fingerprint: 0{32}/);
  assert.match(assembly, /export class Vec3/);
  assert.match(assembly, /constructor\(x: f32 = 0, y: f32 = 0, z: f32 = 0\)/);
  assert.match(
    readFileSync("build/testdata/go-first/bindings.bind.go", "utf8"),
    /func \(m \*Module\) Subtract\(/,
  );
});
