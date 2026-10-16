package example

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"unsafe"

	"github.com/marshone/aligncheck"
)

// demoEnv switches TestStructAlignments from "skipped" to "live demo";
// strictEnv additionally enables aligncheck.WithStrictOrder.
const (
	demoEnv   = "ALIGNCHECK_DEMO"
	strictEnv = "ALIGNCHECK_DEMO_STRICT"
)

// TestStructAlignments is the live demo. BrokenStruct is badly laid out on
// purpose, so this test FAILS when it runs. It only runs with ALIGNCHECK_DEMO=1,
// which keeps `go test ./...` green:
//
//	ALIGNCHECK_DEMO=1 go test -v -run=StructAlignments ./example
func TestStructAlignments(t *testing.T) {
	if os.Getenv(demoEnv) != "1" {
		t.Skipf("demo only: run with %s=1 to see the failure output", demoEnv)
	}

	registry := map[string]any{
		"AlignedStruct": AlignedStruct{},
		"BrokenStruct":  BrokenStruct{},
	}

	var opts []aligncheck.Option
	if os.Getenv(strictEnv) == "1" {
		opts = append(opts, aligncheck.WithStrictOrder())
	}
	aligncheck.AssertAllInPackageAligned(t, registry, opts...)
}

// runDemo runs the demo in a child process and returns its output; the demo
// is expected to fail.
func runDemo(t *testing.T, extraEnv ...string) string {
	t.Helper()
	if os.Getenv(demoEnv) == "1" {
		t.Skip("running inside the demo child process")
	}
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("expected sizes below assume a 64-bit platform")
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestStructAlignments$", "-test.v")
	cmd.Env = append(append(os.Environ(), demoEnv+"=1"), extraEnv...)
	out, err := cmd.CombinedOutput()
	output := string(out)

	var exitErr *exec.ExitError
	if err == nil {
		t.Fatalf("expected the demo to fail, but it passed:\n%s", output)
	}
	if !errors.As(err, &exitErr) {
		t.Fatalf("could not run the demo: %v\n%s", err, output)
	}
	return output
}

func expectAll(t *testing.T, output string, expected []string) {
	t.Helper()
	for _, want := range expected {
		if !strings.Contains(output, want) {
			t.Errorf("demo output is missing:\n  %s", want)
		}
	}
	if t.Failed() {
		t.Logf("full demo output:\n%s", output)
	}
}

// TestDemoDetectsBrokenStruct checks the default mode: BrokenStruct fails
// because reordering would save 16 bytes, with the optimal layout suggested.
func TestDemoDetectsBrokenStruct(t *testing.T) {
	output := runDemo(t)
	expectAll(t, output, []string{
		"--- PASS: TestStructAlignments/Alignment_AlignedStruct",
		"--- FAIL: TestStructAlignments/Alignment_BrokenStruct",
		"sandbox.go:18: BrokenStruct: 168 bytes (147 data + 21 padding), could be 152 (saves 16)",
		"current layout:",
		"1  (padding)                    // 7 bytes",
		"suggested layout:",
		"type BrokenStruct struct {",
		// Source type names (here the UserID alias) must survive in the suggestion.
		"OwnerID      UserID          // 16 bytes",
	})
	// Field order is only enforced with WithStrictOrder, and the struct declared
	// inside helper() is not a package-level type.
	for _, unwanted := range []string{"[strict order]", "not registered"} {
		if strings.Contains(output, unwanted) {
			t.Errorf("default mode should not report %q:\n%s", unwanted, output)
		}
	}
}

// TestDemoStrictOrder checks WithStrictOrder: field order violations are
// reported too.
func TestDemoStrictOrder(t *testing.T) {
	output := runDemo(t, strictEnv+"=1")
	expectAll(t, output, []string{
		"sandbox.go:18: BrokenStruct: field IsActive (bool, 1 byte) is declared before larger field DataBuffer ([]byte, 24 bytes) [strict order]",
		"sandbox.go:18: BrokenStruct: field NextNode (*BrokenStruct, 8 bytes) is declared before larger field OwnerID (UserID, 16 bytes) [strict order]",
		"sandbox.go:18: BrokenStruct: 168 bytes (147 data + 21 padding), could be 152 (saves 16)",
	})
}
