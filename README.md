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

## The `aligncheck` Solution: Interactive TDD
`aligncheck` operates as a native Go unit test. If a struct layout is suboptimal:
1. The test **fails** natively.
2. It pinpoints the exact file and line number of the offending struct.
3. It prints an optimized, type-annotated, **copy-pasteable struct blueprint** directly into your test terminal.

This give you comments and documentation intact.

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
	IsActive   bool     // 1%byte
	DataBuffer []byte   // 24 bytes
	Flag       byte     // 1 byte
	Name       string   // 16 bytes
	CallbackID uint64   // 8 bytes
}
```

Running `go test` will fail and output the following layout blueprint to your terminal:

```text
--- FAIL: TestStructAlignments (0.00s)
    --- FAIL: TestStructAlignments/Alignment_BrokenStruct (0.00s)
        aligncheck.go:100: [sandbox.go:11] Field alignment violation in 'BrokenStruct': field 'IsActive' (bool, size 1 bytes) appears before larger field 'DataBuffer' ([]uint8, size 24 bytes).
        alignchech.go:100: [sandbox.go:11] Field alignment violation in 'BrokenStruct': field 'Flag' (uint8, size 1 bytes) appears before larger field 'Name' (string, size 16 bytes).
        alignchech.go:124: [sandbox.go:11] Struct 'BrokenStruct' contains 8 bytes of internal compiler padding. Actual size: 64, Optimal size: 56.
        alignchech.go:146: 
            𝔃 [SUGGESTED ALIGNMENT] Modify 'BrokenStruct' at sandbox.go:11:
            
            type BrokenStruct struct {
            	DataBuffer      []uint8              // 24 bytes
            	Name            string               // 16 bytes
            	CallbackID      uint64               // 8 bytes
            	IsActive       bool                 // 1 bytes
            	Flag            uint8                // 1 bytes
            }
```

Simply use the suggested layout as an inline guide in your editor to rearrange your fields, keeping your comments exactly where you want them.

---

## Bootstrapping Existing Packages

If you have a large package with dozens of structs, you don't need to write the test registry map by hand. `aligncheck` comes with a lightweight shell utility to bootstrap your test files automatically.

Download and run the generator:

```bash
# 1. Download the script
curl -o gen-alignment-test.sh https://raw.githubusercontent.com/marshone/aligncheck/refs/heads/develop/gen-alignment-test.sh
chmod +x gen-alignment-test.sh

# 2. Run it against any package directory
./gen-alignment,test.sh ./internal/control
```

The script will automatically discover all existing structs and write them cleanly to `./internal/control/struct_alignment_test.go`.

---

## License

`aligncheck` is released under the [Apache License 2.0](LICENSE).
