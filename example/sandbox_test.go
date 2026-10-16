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

// demoEnv switches TestStructAlignments from "skipped" to "live demo".
const demoEnv = "ALIGNCHECK_DEMO"

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

	aligncheck.AssertAllInPackageAligned(t, registry)
}

// TestDemoDetectsBrokenStruct runs the demo in a child process and checks that
// aligncheck fails it for the right reasons: AlignedStruct passes, BrokenStruct
// fails with the expected location, type names, sizes and suggestion.
func TestDemoDetectsBrokenStruct(t *testing.T) {
	if os.Getenv(demoEnv) == "1" {
		t.Skip("running inside the demo child process")
	}
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("expected sizes below assume a 64-bit platform")
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestStructAlignments$", "-test.v")
	cmd.Env = append(os.Environ(), demoEnv+"=1")
	out, err := cmd.CombinedOutput()
	output := string(out)

	var exitErr *exec.ExitError
	if err == nil {
		t.Fatalf("expected the demo to fail, but it passed:\n%s", output)
	}
	if !errors.As(err, &exitErr) {
		t.Fatalf("could not run the demo: %v\n%s", err, output)
	}

	expected := []string{
		"--- PASS: TestStructAlignments/Alignment_AlignedStruct",
		"--- FAIL: TestStructAlignments/Alignment_BrokenStruct",
		"[sandbox.go:18] Field alignment violation in 'BrokenStruct': field 'IsActive' (bool, size 1 bytes) appears before larger field 'DataBuffer' ([]byte, size 24 bytes).",
		// Source type names (here the UserID alias) must survive in the messages.
		"field 'NextNode' (*BrokenStruct, size 8 bytes) appears before larger field 'OwnerID' (UserID, size 16 bytes).",
		"Actual size: 168, Optimal size: 152.",
		"[SUGGESTED ALIGNMENT] Modify 'BrokenStruct' at sandbox.go:18:",
	}
	for _, want := range expected {
		if !strings.Contains(output, want) {
			t.Errorf("demo output is missing:\n  %s", want)
		}
	}
	if t.Failed() {
		t.Logf("full demo output:\n%s", output)
	}
}
