# Contributing to wago-bind-as

Thank you for helping improve `wago-bind-as`.

## Setup

Requirements:

- Go 1.22 or newer
- Node.js 20 or newer
- npm

```bash
git clone https://github.com/JairusSW/wago-bind-as.git
cd wago-bind-as
npm install
```

## Checks

Run all required checks before opening a pull request:

```bash
npm test
GOCACHE=/tmp/wago-bind-as-go-cache go test -race ./...
GOCACHE=/tmp/wago-bind-as-go-cache go vet ./...
cd bench && go test -run '^$' -bench BenchmarkWago -benchmem
```

Changes to layout, validation, or ownership require malformed-input and lifecycle coverage. Transform changes must compile a real AssemblyScript fixture and inspect the emitted Wasm shape. Performance claims need before/after samples on the same host and must report allocations and construction separately from access.

Use focused conventional commit messages such as `feat:`, `fix:`, `perf:`, `test:`, and `docs:`.
