# aligncheck

`aligncheck` is a zero-dependency, test-driven struct alignment validator for Go.

Unlike standalone linters, `aligncheck` runs inside your normal `go test` pipeline. It protects performance-sensitive code from silent memory bloat and poor cache usage, without rewriting your source files or getting in the way of your workflow.

---

## The Problem: Memory Layout and Compiler Padding

The Go compiler aligns every struct field on a boundary that matches its type. When a small field sits in front of a larger one, the compiler inserts invisible **padding bytes** to keep the larger field correctly aligned.

That padding wastes memory and spreads your data over more CPU cache lines (L1/L2). For structs allocated by the thousands or millions, it adds up quickly.

### Existing Tools, and Their Drawbacks

- **`fieldalignment` (go vet analyzer):** reports layout problems, but runs as a separate step outside your usual test loop, and its output is noisy about trailing padding you can't remove anyway.
- **Automatic rewriters:** reorder fields for you, but they tend to strip or misplace the comments and documentation attached to those fields.

---

## The `aligncheck` Approach: Test-Driven Layouts

`aligncheck` is a regular Go unit test. When a struct layout is suboptimal:

1. The test **fails**, like any other test.
2. It points to the exact file and line of the struct.
3. It prints a type-annotated, **copy-pasteable layout** in the test output.

Your source file is never modified. You reorder the fields yourself, and your comments stay exactly where you put them.

---

## Installation

Add the library to your module:

```bash
go get github.com/marshone/aligncheck
```

Optionally, install the `aligncheck` command, which writes alignment tests for you (see [Bootstrapping Existing Packages](#bootstrapping-existing-packages)):

```bash
go install github.com/marshone/aligncheck/cmd/aligncheck@latest
```

---

## Quick Start

### Step 1: Write an Alignment Test

Create a `struct_alignment_test.go` file in the package you want to audit, and register its structs (exported and unexported) in a map:

```go
package mypackage

import (
	"testing"

	"github.com/marshone/aligncheck"
)

func TestStructAlignments(t *testing.T) {
	registry := map[string]any{
		"MyConfig":   MyConfig{},
		"Session":    Session{},
		"unexported": unexported{},
	}

	aligncheck.AssertAllInPackageAligned(t, registry)
}
```

`AssertAllInPackageAligned` parses every non-test `.go` file in the package, finds each struct declaration, and checks it against the instance you registered.

**Every struct in the package must be registered.** If a struct is declared but missing from the map, the test fails and tells you which one to add. New structs can't slip through unchecked.

### Step 2: Run Your Tests

```bash
go test -v -run=StructAlignments .
```

---

## Example Failure Output

Given this struct:

```go
type BrokenStruct struct {
	IsActive   bool   // 1 byte
	DataBuffer []byte // 24 bytes
	Flag       byte   // 1 byte
	Name       string // 16 bytes
	CallbackID uint64 // 8 bytes
}
```

`go test -v` fails with the following output (64-bit platform). Source type names, including your own type aliases, are preserved in the messages and in the suggested layout.

```text
=== RUN   TestStructAlignments/Alignment_BrokenStruct
    aligncheck.go:133: [models.go:3] Field alignment violation in 'BrokenStruct': field 'IsActive' (bool, size 1 bytes) appears before larger field 'DataBuffer' ([]byte, size 24 bytes).
    aligncheck.go:133: [models.go:3] Field alignment violation in 'BrokenStruct': field 'Flag' (byte, size 1 bytes) appears before larger field 'Name' (string, size 16 bytes).
    aligncheck.go:157: [models.go:3] Struct 'BrokenStruct' contains 8 bytes of internal compiler padding. Actual size: 64, Optimal size: 56.
    aligncheck.go:179:
        [SUGGESTED ALIGNMENT] Modify 'BrokenStruct' at models.go:3:

        type BrokenStruct struct {
        	DataBuffer      []byte               // 24 bytes
        	Name            string               // 16 bytes
        	CallbackID      uint64               // 8 bytes
        	IsActive        bool                 // 1 bytes
        	Flag            byte                 // 1 bytes
        }

--- FAIL: TestStructAlignments (0.00s)
    --- FAIL: TestStructAlignments/Alignment_BrokenStruct (0.00s)
```

Where the numbers come from:

| Layout | Field bytes | Padding | Total size |
| --- | --- | --- | --- |
| Current | 50 | 14 internal (7 after `IsActive`, 7 after `Flag`) | 64 |
| Suggested | 50 | 6 trailing (unavoidable) | 56 |

Reordering saves 8 bytes per instance. The 6 bytes of trailing padding remain in any order, because the struct's total size must be a multiple of its 8-byte alignment, so `aligncheck` doesn't count them against you.

Use the suggested layout as a guide in your editor, move your fields, and keep your comments intact.

---

## How It Checks

For each registered struct, `aligncheck` runs two checks:

1. **Field order:** fields must be sorted by descending size. Each field placed before a larger one is reported.
2. **Size:** the struct's actual size is compared with the smallest possible size, which is the sum of its field sizes rounded up to the struct's alignment. Any difference is reported as removable padding. Trailing padding that no ordering can remove is ignored.

If either check fails, the suggested layout lists the fields sorted by descending size. Fields of equal size keep their original relative order.

### Checking a Single Struct

To check one struct without scanning the whole package, call the lower-level assertion directly:

```go
func TestSessionAlignment(t *testing.T) {
	aligncheck.AssertOptimalAlignmentWithSuggestions(t, Session{}, "session.go")
}
```

The third argument is a location label printed in the messages.

---

## Bootstrapping Existing Packages

For a package with dozens of structs, you don't have to write the registry by hand. The `aligncheck` command finds every struct declared in a package and generates the test file for you.

### Install

```bash
go install github.com/marshone/aligncheck/cmd/aligncheck@latest
```

Go builds the command from source with your own toolchain and puts it in `$GOBIN`, or in `$GOPATH/bin` (usually `~/go/bin`) when `GOBIN` is unset. Make sure that directory is on your `PATH`.

`@latest` is the newest release; use a tag such as `@v0.1.0` to pin a version. Downloads go through the Go module proxy and are verified against the Go checksum database.

To run it once without installing:

```bash
go run github.com/marshone/aligncheck/cmd/aligncheck@latest gen ./internal/control
```

### Usage

```bash
# Generate an alignment test for a package (default file: struct_alignment_test.go)
aligncheck gen ./internal/control

# Use a custom file name prefix (e.g. zz_generated_alignment_test.go)
aligncheck gen -prefix zz_generated_alignment ./internal/control

# Regenerate after adding structs, overwriting the previously generated file
aligncheck gen -f ./internal/control

# List every generated alignment test under the current directory
aligncheck ls

# Show which generated tests would be removed, then remove them
aligncheck clean
aligncheck clean -y

# Print the installed version
aligncheck version
```

Flags go before the directory argument.

### What `gen` Does

- Registers every top-level struct in the package's non-test files, including structs declared in grouped `type ( … )` blocks.
- Never overwrites a hand-written file, even with `-f`. Only files it generated can be replaced.
- Refuses to run if another test file in the package already declares `TestStructAlignments`, since two would not compile.
- Writes the same output for the same input: no timestamps, so regenerating doesn't create noise in your diffs.

Generated files start with an `Auto Generated by github.com/marshone/aligncheck` line, which is how `ls` and `clean` recognize them (including files created by the earlier `gen_alignment_test.sh` script), and a standard `// Code generated … DO NOT EDIT.` line, which Go tooling and linters recognize.

### Generic Structs

A generic struct can't be registered without a type argument, so `gen` skips it with a warning. For packages with generic structs, generate the file once, add an instantiation to the registry yourself:

```go
"Cache": Cache[string]{},
```

and remove the two header lines. The file then becomes yours, and `aligncheck gen` will no longer overwrite it.

---

## Development

```bash
make test      # run all tests: library, CLI and the example self-test
make demo      # show aligncheck failing on a deliberately broken struct
make build     # build the CLI into bin/
make install   # install the CLI into $GOBIN (or $GOPATH/bin)
```

`make demo` fails on purpose: `example/sandbox.go` contains a badly laid-out struct so you can see the full failure output. The regular suite stays green because it runs the same demo in a child process and checks that `aligncheck` reports the expected violations, sizes and suggested layout.

---

## License

`aligncheck` is released under the [Apache License 2.0](LICENSE).
