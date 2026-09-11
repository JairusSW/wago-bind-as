# Source declarations define bindings

AssemblyScript exports and explicitly marked Go structs are the authoritative binding declarations; users do not maintain a parallel schema. The transform refreshes strongly typed bind artifacts before AssemblyScript type checking and embeds a canonical ABI fingerprint because duplicated hand-written declarations would drift, while runtime reflection would sacrifice both static tooling and predictable performance.
