package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const inspectFixture = `package pkg

import "sync"

type Broken struct {
	IsActive   bool
	DataBuffer []byte
	Flag       byte
	Name       string
	CallbackID uint64
}

type NoPadding struct {
	A int64
	B int32
	C int32
	D int64
}

type Odd struct {
	A [3]byte
	B uint16
	C uint8
}

type WithEmbed struct {
	Ready bool
	sync.Mutex
	Count int64
}

type (
	Grouped struct {
		Flag bool
		ID   int64
	}
	Alias = Odd
)

type Generic[T any] struct {
	Flag bool
	V    T
}

type TrailingZero struct {
	ID   int64
	Mark struct{}
}
`

// inModule writes a small module to a temp dir and runs fn from inside it,
// since inspect resolves package patterns like go list does.
func inModule(t *testing.T, fn func()) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/fixture\n\ngo 1.21\n")
	writeFile(t, filepath.Join(dir, "pkg", "m.go"), inspectFixture)

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	fn()
}

func TestInspectReportsLayouts(t *testing.T) {
	inModule(t, func() {
		var out bytes.Buffer
		if err := inspect(&out, []string{"./pkg"}, "amd64", false); err != nil {
			t.Fatal(err)
		}
		got := out.String()
		for _, want := range []string{
			"Broken: 64 bytes (50 data + 14 padding), could be 56 (saves 8)",
			"NoPadding: 24 bytes (24 data + 0 padding), optimal, no padding",
			"Odd: 8 bytes (6 data + 2 padding), could be 6 (saves 2)",
			"WithEmbed: 24 bytes (17 data + 7 padding), optimal: the 7 padding bytes are unavoidable",
			"Grouped: 16 bytes (9 data + 7 padding), optimal: the 7 padding bytes are unavoidable",
			"TrailingZero: 16 bytes (8 data + 8 padding), could be 8 (saves 8)",
			"6 structs inspected (amd64): 38 bytes of padding, 18 bytes removable by reordering 3 structs",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("output is missing %q:\n%s", want, got)
			}
		}
		for _, notWant := range []string{"Alias:", "Generic:", "current layout"} {
			if strings.Contains(got, notWant) {
				t.Errorf("output should not contain %q:\n%s", notWant, got)
			}
		}
	})
}

func TestInspectVerboseShowsOffsetsAndPadding(t *testing.T) {
	inModule(t, func() {
		var out bytes.Buffer
		if err := inspect(&out, []string{"./pkg"}, "amd64", true); err != nil {
			t.Fatal(err)
		}
		want := "    current layout:\n" +
			"        0  IsActive   bool   // 1 byte\n" +
			"        1  (padding)         // 7 bytes\n" +
			"        8  DataBuffer []byte // 24 bytes\n" +
			"       32  Flag       byte   // 1 byte\n" +
			"       33  (padding)         // 7 bytes\n" +
			"       40  Name       string // 16 bytes\n" +
			"       56  CallbackID uint64 // 8 bytes\n"
		if got := out.String(); !strings.Contains(got, want) {
			t.Errorf("verbose layout for Broken is missing:\n%s\ngot:\n%s", want, got)
		}
	})
}

func TestInspectSuggestedOrder(t *testing.T) {
	inModule(t, func() {
		var out bytes.Buffer
		if err := inspect(&out, []string{"./pkg"}, "amd64", false); err != nil {
			t.Fatal(err)
		}
		got := out.String()
		// The suggestion must reach the optimal size: B (align 2) before A ([3]byte),
		// and the zero-size field first.
		for _, want := range []string{
			"type Odd struct {\n        B uint16  // 2 bytes\n        A [3]byte // 3 bytes\n        C uint8   // 1 byte\n    }",
			"type TrailingZero struct {\n        Mark struct{} // 0 bytes\n        ID   int64    // 8 bytes\n    }",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("output is missing suggestion:\n%s\n\ngot:\n%s", want, got)
			}
		}
	})
}

func TestInspectArchitecture(t *testing.T) {
	inModule(t, func() {
		var out bytes.Buffer
		if err := inspect(&out, []string{"./pkg"}, "386", false); err != nil {
			t.Fatal(err)
		}
		// On 386, slices and strings are half the size and uint64 aligns to 4:
		// Broken is 36 bytes (1+3 pad, 12, 1+3 pad, 8, 8), optimal 32 (30 rounded up to 4).
		if got := out.String(); !strings.Contains(got, "Broken: 36 bytes (30 data + 6 padding), could be 32 (saves 4)") {
			t.Errorf("unexpected 386 layout:\n%s", got)
		}
		if err := inspect(&out, []string{"./pkg"}, "z80", false); err == nil {
			t.Error("expected an error for an unknown architecture")
		}
	})
}
