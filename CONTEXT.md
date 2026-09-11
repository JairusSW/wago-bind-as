# Wago Bindings

This context describes typed values and calls shared between Go and AssemblyScript without a user-maintained schema.

## Language

**Binding surface**:
The exported functions whose parameters or results contain shared records.
_Avoid_: API schema, hand-written manifest

**Shared record**:
A class or struct reachable from the binding surface and represented consistently in Go and AssemblyScript.
_Avoid_: serialized object, DTO

**Source declaration**:
The authoritative handwritten AssemblyScript class or explicitly marked Go struct for a shared record.
_Avoid_: schema

**Bind artifact**:
A generated, strongly typed `.bind.go` or `.bind.ts` file derived from source declarations.
_Avoid_: source model, hand-written binding
