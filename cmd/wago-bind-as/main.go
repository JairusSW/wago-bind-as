package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/JairusSW/wago-bind-as/generate"
	"github.com/JairusSW/wago-bind-as/gobind"
	"github.com/JairusSW/wago-bind-as/schema"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "generate":
		generateCommand(os.Args[2:])
	case "sync-go":
		syncGoCommand(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
}

func generateCommand(args []string) {
	flags := flag.NewFlagSet("generate", flag.ExitOnError)
	schemaPath := flags.String("schema", "wago-bind.json", "source schema JSON")
	goPath := flags.String("go", "", "generated Go file")
	asPath := flags.String("as", "", "generated AssemblyScript file")
	manifestPath := flags.String("manifest", "", "resolved manifest JSON")
	goPackage := flags.String("package", "bindings", "generated Go package")
	_ = flags.Parse(args)
	if *goPath == "" && *asPath == "" && *manifestPath == "" {
		fatalf("select at least one of -go, -as, or -manifest")
	}
	raw, err := os.ReadFile(*schemaPath)
	if err != nil {
		fatalf("read schema: %v", err)
	}
	var source schema.Schema
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&source); err != nil {
		fatalf("decode schema: %v", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		fatalf("decode schema: trailing JSON value")
	}
	manifest, err := schema.Compile(source)
	if err != nil {
		fatalf("compile schema: %v", err)
	}
	if *goPath != "" {
		data, err := generate.Go(manifest, *goPackage)
		if err != nil {
			fatalf("generate Go: %v", err)
		}
		write(*goPath, data)
	}
	if *asPath != "" {
		data, err := generate.AssemblyScript(manifest)
		if err != nil {
			fatalf("generate AssemblyScript: %v", err)
		}
		write(*asPath, data)
	}
	if *manifestPath != "" {
		data, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			fatalf("encode manifest: %v", err)
		}
		data = append(data, '\n')
		write(*manifestPath, data)
	}
}

func syncGoCommand(args []string) {
	flags := flag.NewFlagSet("sync-go", flag.ExitOnError)
	root := flags.String("root", ".", "Go project root")
	_ = flags.Parse(args)
	changed, err := gobind.Sync(*root)
	if err != nil {
		fatalf("sync Go bindings: %v", err)
	}
	for _, path := range changed {
		fmt.Fprintln(os.Stdout, path)
	}
}

func write(path string, data []byte) {
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, data) {
		return
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		fatalf("create output directory: %v", err)
	}
	temporary, err := os.CreateTemp(directory, ".wago-bind-as-*")
	if err != nil {
		fatalf("create temporary output: %v", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err = temporary.Chmod(0o644); err == nil {
		_, err = temporary.Write(data)
	}
	if err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(temporaryName, path)
	}
	if err != nil {
		fatalf("write %s: %v", path, err)
	}
}
func usage() {
	fmt.Fprintln(os.Stderr, "usage: wago-bind-as <generate|sync-go> [options]")
}
func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "wago-bind-as: "+format+"\n", args...)
	os.Exit(1)
}
