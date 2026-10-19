package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"unsafe"

	"github.com/marshone/aligncheck/internal/layout"
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
		if err := inspect(&out, []string{"./pkg"}, "amd64", false, layout.Options{}); err != nil {
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
		if err := inspect(&out, []string{"./pkg"}, "amd64", true, layout.Options{}); err != nil {
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
		if err := inspect(&out, []string{"./pkg"}, "amd64", false, layout.Options{}); err != nil {
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
		if err := inspect(&out, []string{"./pkg"}, "386", false, layout.Options{}); err != nil {
			t.Fatal(err)
		}
		// On 386, slices and strings are half the size and uint64 aligns to 4:
		// Broken is 36 bytes (1+3 pad, 12, 1+3 pad, 8, 8), optimal 32 (30 rounded up to 4).
		if got := out.String(); !strings.Contains(got, "Broken: 36 bytes (30 data + 6 padding), could be 32 (saves 4)") {
			t.Errorf("unexpected 386 layout:\n%s", got)
		}
		if err := inspect(&out, []string{"./pkg"}, "z80", false, layout.Options{}); err == nil {
			t.Error("expected an error for an unknown architecture")
		}
	})
}

// heapMix covers every kind of pointer-holding field. It is declared here for
// reflect and, identically, as source text for inspect.
type heapMix struct {
	N     int64
	S     string
	Arr   [3]string
	I     any
	Inner struct {
		X int64
		P *int
	}
	U     uintptr
	Bytes [16]byte
}

const heapMixSource = `package pkg

type HeapMix struct {
	N     int64
	S     string
	Arr   [3]string
	I     any
	Inner struct {
		X int64
		P *int
	}
	U     uintptr
	Bytes [16]byte
}
`

// reflectPointerBytes is the runtime-side reference for pointerBytes.
func reflectPointerBytes(t reflect.Type) int64 {
	word := int64(unsafe.Sizeof(uintptr(0)))
	switch t.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Chan, reflect.Func, reflect.UnsafePointer,
		reflect.String, reflect.Slice:
		return word
	case reflect.Interface:
		return 2 * word
	case reflect.Array:
		if e := reflectPointerBytes(t.Elem()); e > 0 && t.Len() > 0 {
			return int64(t.Len()-1)*int64(t.Elem().Size()) + e
		}
	case reflect.Struct:
		var end int64
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if p := reflectPointerBytes(f.Type); p > 0 {
				end = max(end, int64(f.Offset)+p)
			}
		}
		return end
	}
	return 0
}

// TestInspectHeapMatchesReflect: the go/types pointer analysis used by inspect
// must agree with reflect, field by field and for the whole struct.
func TestInspectHeapMatchesReflect(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("expected sizes assume a 64-bit platform")
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/heapmix\n\ngo 1.21\n")
	writeFile(t, filepath.Join(dir, "pkg", "m.go"), heapMixSource)
	wd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)

	var out bytes.Buffer
	if err := inspect(&out, []string{"./pkg"}, "amd64", true, layout.Options{Heap: true}); err != nil {
		t.Fatal(err)
	}
	got := out.String()

	// HeapMix is already optimal in size: no suggestion, so no "could be".
	if strings.Contains(got, "could be") {
		t.Errorf("no suggestion is shown, so nothing should say \"could be\":\n%s", got)
	}
	// Multi-line types are printed on one line.
	if !strings.Contains(got, "Inner struct{X int64; P *int} // 16 bytes, pointer") {
		t.Errorf("anonymous struct field should be on one line:\n%s", got)
	}

	typ := reflect.TypeOf(heapMix{})
	scan := reflectPointerBytes(typ)
	if want := fmt.Sprintf("GC scans %d of %d bytes", scan, typ.Size()); !strings.Contains(got, want) {
		t.Errorf("want %q (from reflect) in:\n%s", want, got)
	}
	// Every field reflect says holds pointers is marked, and no other.
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		marked := regexp.MustCompile(`(?m)^\s+\d+\s+` + f.Name + `\s.*, pointer$`).MatchString(got)
		if hasPtr := reflectPointerBytes(f.Type) > 0; marked != hasPtr {
			t.Errorf("field %s: marked as pointer %v, reflect says %v", f.Name, marked, hasPtr)
		}
	}
}
