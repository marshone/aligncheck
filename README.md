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
3. It shows where the padding sits, and prints a type-annotated, **copy-pasteable layout** in the test output.

Your source file is never modified. You reorder the fields yourself, and your comments stay exactly where you put them.

---

## Installation

Add the library to your module:

```bash
go get github.com/marshone/aligncheck
```

Optionally, install the `aligncheck` command, which writes and runs alignment tests for you and can inspect struct layouts without any test (see [Bootstrapping Existing Packages](#bootstrapping-existing-packages)):

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
    aligncheck.go:176: models.go:3: BrokenStruct: 64 bytes (50 data + 14 padding), could be 56 (saves 8)
    aligncheck.go:180:
            current layout:
                0  IsActive   bool   // 1 byte
                1  (padding)         // 7 bytes
                8  DataBuffer []byte // 24 bytes
               32  Flag       byte   // 1 byte
               33  (padding)         // 7 bytes
               40  Name       string // 16 bytes
               56  CallbackID uint64 // 8 bytes

            suggested layout:
            type BrokenStruct struct {
                DataBuffer []byte // 24 bytes
                Name       string // 16 bytes
                CallbackID uint64 // 8 bytes
                IsActive   bool   // 1 byte
                Flag       byte   // 1 byte
            }

--- FAIL: TestStructAlignments (0.00s)
    --- FAIL: TestStructAlignments/Alignment_BrokenStruct (0.00s)
```

The first line uses the compiler's `file:line: message` form, so editors make it clickable. The same report, word for word, is what `aligncheck inspect -v` prints for this struct.

Where the numbers come from:

| Layout | Field bytes | Padding | Total size |
| --- | --- | --- | --- |
| Current | 50 | 14 internal (7 after `IsActive`, 7 after `Flag`) | 64 |
| Suggested | 50 | 6 trailing (unavoidable) | 56 |

Reordering saves 8 bytes per instance. The 6 bytes of trailing padding remain in any order, because the struct's total size must be a multiple of its 8-byte alignment, so `aligncheck` doesn't count them against you.

Use the suggested layout as a guide in your editor, move your fields, and keep your comments intact.

---

## How It Checks

For each registered struct, `aligncheck` computes the smallest size any field order can reach and compares it with the struct's actual size. Every Go type's size is a multiple of its alignment, so sorting fields by decreasing alignment leaves no internal padding; zero-size fields go first, because a trailing zero-size field makes the compiler add padding.

A struct fails only when reordering would make it smaller. Padding that can only move, such as the 6 trailing bytes above, never fails a test.

When a struct fails, the suggested layout is that optimal order. Fields of equal alignment and size keep their original relative order.

### Strict Field Order

Some teams prefer a fixed convention: fields always declared in descending size order, even where that saves no memory. Opt in with `WithStrictOrder`:

```go
aligncheck.AssertAllInPackageAligned(t, registry, aligncheck.WithStrictOrder())
```

Each field placed before a larger one is then reported as well, and the suggested layout follows descending size.

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

# Run the alignment tests (go test -run TestStructAlignments); go test flags pass through
aligncheck test ./...
aligncheck test -v ./internal/control

# Report struct layouts directly, without any test file (see below); -v adds offsets
aligncheck inspect ./...
aligncheck inspect -v ./internal/control

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

### Inspecting Without Tests

`aligncheck inspect` reads a package's source and reports the layout of every top-level struct. It writes no file, needs no test, and doesn't require `aligncheck` in your `go.mod`, so it's the quickest way to try `aligncheck` on any codebase:

```bash
aligncheck inspect ./...
```

Each struct gets one line: its size, how much of it is data and how much is padding, and a verdict. When a reordering can shrink it, the suggested layout follows:

```text
example/sandbox.go:9: AlignedStruct: 48 bytes (48 data + 0 padding), optimal, no padding
example/sandbox.go:18: BrokenStruct: 168 bytes (147 data + 21 padding), could be 152 (saves 16)

    suggested layout:
    type BrokenStruct struct {
        DataBuffer   []byte          // 24 bytes
        CreatedAt    time.Time       // 24 bytes
        ...
        Priority     int8            // 1 byte
    }

2 structs inspected (amd64): 21 bytes of padding, 16 bytes removable by reordering 1 struct
```

Padding isn't always removable. A struct's size must be a multiple of its alignment, so some padding can only move, not disappear:

```text
internal/engine/memory.go:6: BranchState: 40 bytes (37 data + 3 padding), optimal: the 3 padding bytes are unavoidable
```

Add `-v` to see where the padding sits, field by field. Holes between fields are marked `(padding)`; padding at the end is `(trailing padding)`:

```text
    current layout:
        0  Name     string   // 16 bytes
       16  Head     uint64   // 8 bytes
       24  Dialect  uint32   // 4 bytes
       28  Lifetime Lifetime // 1 byte
       29  (padding)         // 3 bytes
       32  Gen      uint64   // 8 bytes
```

Trailing padding that no reordering can remove is free space: in a struct with 3 bytes of it, up to three 1-byte fields can be added without the struct growing.

Sizes come from Go's own type checker for the target architecture; `-arch` selects another one (for example `-arch 386` or `-arch arm64`).

`inspect` and the alignment test use the same calculation, so they agree: a struct `inspect` reports as optimal passes the test, unless you opted into `WithStrictOrder`.

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
