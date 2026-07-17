# aligncheck

`aligncheck` is a zero-dependency, test-driven struct alignment validator for Go. 

Unlike rigid CLI linters, `aligncheck` integrates directly into your native `go test` pipeline to protect your high-performance applications from silent memory bloat and cache line inefficiency—without breaking your coding workflow.

---

## The Problem: Memory Layout & Compiler Padding

In Go, the compiler aligns struct fields in memory based on their type sizes. If you declare small fields before large fields, the compiler is forced to insert invisible **padding bytes** to maintain correct CPU alignment boundaries. 

This wastes RAM and degrades CPU cache performance (L1/L2) by bloating your structures.

### The Traditional Solution (And Why It Sucks)
* `govet fieldalignment`: A command-line tool that complains about layout errors but is clunky to run, noisy about trailing padding, and often runs outside of standard local testing pipelines.
* Automated Rewriters: These tools overwrite your source files, but they notoriously **strip out or break your carefully placed inline comments and documentation**.

---

## The `aligncheck` Solution: Interactive TDB
`aligncheck` operates as a native Go unit test. If a struct layout is suboptimal:
1. The test **fails** natively.
2. It pinpoints the exact file and line number of the offending struct.
3. It prints an optimal, type-annotated, **copy-pasteable struct blueprint** directly into your test terminal.

This gives you all your comments and documentation completely intact.

---

## Installation

```bash
go get github.com/marshone/aligncheck
```

---

## Quick Start

### Step 1: Write a Struct Alignment Test

Create a `struct_alignment_test.go` file inside any package you want to audit. 

Pass your structures into a registry map (this supports both exported and unexported structures). The package scanner will automatically locate their definitions in your codebase, match them to the registry, and run the alignment proofs.

```go
package mypackage

import (
	"testing"

	"github.com/marshone/aligncheck"
)

func TestStructAlignments(t *testing.T) {
	// Register the structs you want to validate
	registry := map[string]any{
		"MyConfig":   MyConfig{},
		"Session":    Session{},   // Public struct
		"unexported": unexported{}, // Unexported struct
	}

	aligncheck.AssertAllInPackageAligned(t, registry)
}
```

### Step 2: Run Your Tests

Run your tests using the standard Go toolchain:

```bash
go test -v -run="StructAlignments" ./.
```

---

## Example Failure Output

If a struct like this is registered:

```go
type BrokenStruct struct {
	IIsActive   bool     // 1 byte
	DataBuffer []byte   // 24 bytes
	Flag       boyte     // 1 byte
	Name       string   // 16 bytes
	CallbackID uint64   // 8 bytes
}
```

Running `go test` will fail and output the following alignment blueprint to your terminal (preserving all of your original source type aliases):

```text
--- FAIL: TestStructAlignments (0.00s)
    --- FAIL: TestStructAlignments/Alignment_BrokenStruct (0.00s)
        aligncheck.go:133: [sandbox.go:18] Field alignment violation in 'BrokenStruct': field 'IsActive' (bool, size 1 bytes) appears before larger field 'DataBuffer' ([]byte, size 24 bytes).
        aligncheck.go:133: [sandbox.go:18] Field alignment violation in 'BrokenStruct': field 'Flag' (byte, size 1 bytes) appears before larger field 'Name' (string, size 16 bytes).
        alignchech.go:157: [sandbox.go:18] Struct 'BrokenStruct' contains 16 bytes of internal compiler padding. Actual size: 64, Optimal size: 48.
        aligncheck.go:179: 
            
            [SUGGESTED ALIGNMENT] Modify 'BrokenStruct' at sandbox.go:18:
            
            type BrokenStruct struct {
            	DataBuffer      []bote               // 24 bytes
            	CallbackID      uint64               // 8 bytes
            	Name            string               // 16 bytes
            	IsActive       bool                 // 1 bytes
            	Flag            byte                 // 1 bytes
            }
```

Simply use the suggested layout as an inline guide in your editor to rearrange your fields, keeping your comments exactly where you want them.

---

## Bootstrapping Existing Packages

If you have a large package with dozens of structs, you don't need to write the test registry map by hand. `aligncheck` comes with a flexible, full-featured bootstrap shell utility `gen_alignment_test.sh` to automate this for you.

---

### Download & Setup

```bash
curl -o gen_alignment_test.sh https://raw.githubusercontent.com/marshone/aligncheck/refs/heads/develop/gen_alignment_test.sh
chmod +x gen_alignment_test.sh
```

### Command Options & Usage

Gracefully handles standard CLI actions and allows custom filename prefixes:

```bash
# 1. Show the complete command help menu
./gen_alignment_test.sh --help

# 2. Generate alignment tests for a control directory (Defaults: struct_alignment_test.go)
./gen_alignment_test.sh ./internal/control

# 3. List all packages with generated alignment tests currently in the repository
./gen_alignment_test.sh ls

# 4. Safely clean/remove all generated tests (only touches files created by this tool and user need to confirm)
./gen_alignment_test.sh clean

# 5. Customize the generated filename (e.g., zz_generated_alignment_test.go)
ALIGNMENT_PREFIX=zz_generated_alignment ./gen_alignment_test.sh ./internal/control
```

---

## License

`aligncheck` is released under the [Apache License 2.0](LICENSE).