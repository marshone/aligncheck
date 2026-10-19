package layout

import (
	"os"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"unsafe"
)

// goMinor returns the minor version of a release toolchain ("go1.21.13" -> 21),
// or -1 for anything else (devel builds are newer than any release).
func goMinor() int {
	v, ok := strings.CutPrefix(runtime.Version(), "go1.")
	if !ok {
		return -1
	}
	for i, r := range v {
		if r < '0' || r > '9' {
			v = v[:i]
			break
		}
	}
	minor, err := strconv.Atoi(v)
	if err != nil {
		return -1
	}
	return minor
}

// pointerBytes mirrors the runtime's "ptrdata": bytes from the start of a
// value of type t through its last pointer word.
func pointerBytes(t reflect.Type) int64 {
	word := int64(unsafe.Sizeof(uintptr(0)))
	switch t.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Chan, reflect.Func, reflect.UnsafePointer,
		reflect.String, reflect.Slice:
		return word
	case reflect.Interface:
		return 2 * word
	case reflect.Array:
		if e := pointerBytes(t.Elem()); e > 0 && t.Len() > 0 {
			return int64(t.Len()-1)*int64(t.Elem().Size()) + e
		}
	case reflect.Struct:
		var end int64
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if p := pointerBytes(f.Type); p > 0 {
				end = max(end, int64(f.Offset)+p)
			}
		}
		return end
	}
	return 0
}

func heapFields(typ reflect.Type) []Field {
	fields := fieldsOf(typ)
	for i := range fields {
		fields[i].Pointers = pointerBytes(typ.Field(i).Type)
	}
	return fields
}

// measuredHeapSize allocates many values of type t and asks the runtime how
// many bytes each one really took. Stray allocations by the runtime or other
// goroutines during the loop can only add bytes (one 4 KiB block across 4096
// values already shifts the average by a byte), so it takes the smallest of
// several trials.
func measuredHeapSize(t reflect.Type) int64 {
	const n, trials = 4096, 5
	best := int64(-1)
	for trial := 0; trial < trials; trial++ {
		keep := make([]reflect.Value, n)
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		for i := range keep {
			keep[i] = reflect.New(t)
		}
		runtime.ReadMemStats(&after)
		runtime.KeepAlive(keep)
		if got := int64(after.TotalAlloc-before.TotalAlloc) / n; best < 0 || got < best {
			best = got
		}
	}
	return best
}

// TestHeapSizeMatchesRuntime checks the size-class model against what the
// running Go runtime actually allocates, with and without pointers, including
// sizes above 512 bytes where pointerful objects carry an allocation header.
func TestHeapSizeMatchesRuntime(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("model checked on 64-bit")
	}
	// The model includes the Go 1.22+ allocation header; Go 1.21 (the go.mod
	// minimum) has none, so the check is skipped on that leg of GitHub Actions.
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		if m := goMinor(); m >= 0 && m < 22 {
			t.Skipf("Go 1.%d has no allocation header; model targets Go 1.22+", m)
		}
	}
	byteType := reflect.TypeOf(byte(0))
	ptrType := reflect.TypeOf((*int)(nil))
	for _, size := range []int{16, 24, 40, 56, 80, 88, 200, 512, 520, 576, 600, 1000, 1024, 4000, 4096, 32760, 32768, 33000} {
		noscan := reflect.ArrayOf(size, byteType)
		withPtr := reflect.StructOf([]reflect.StructField{
			{Name: "P", Type: ptrType},
			{Name: "B", Type: reflect.ArrayOf(size-8, byteType)},
		})
		for _, tc := range []struct {
			typ         reflect.Type
			hasPointers bool
		}{{noscan, false}, {withPtr, true}} {
			want, _ := HeapSize(int64(tc.typ.Size()), tc.hasPointers, 8)
			if got := measuredHeapSize(tc.typ); got != want {
				t.Errorf("size %d (pointers %v): runtime allocates %d bytes, model says %d",
					size, tc.hasPointers, got, want)
			}
		}
	}
}

func TestPointerBytesMatchesExpectation(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("expected sizes assume a 64-bit platform")
	}
	for _, tc := range []struct {
		v    any
		want int64
	}{
		{"", 8}, {[]string{}, 8}, {(*int)(nil), 8}, {map[int]int{}, 8},
		{any(nil), 16}, {[3]string{}, 40}, {[4]int{}, 0}, {uintptr(0), 0},
		{struct {
			A int64
			S string
			B [2]byte
		}{}, 16},
	} {
		typ := reflect.TypeOf(tc.v)
		if typ == nil {
			typ = reflect.TypeOf((*any)(nil)).Elem()
		}
		if got := pointerBytes(typ); got != tc.want {
			t.Errorf("%v: %d pointer bytes, want %d", typ, got, tc.want)
		}
	}
}

type event struct {
	ID      uint64
	Source  uint64
	Name    string
	Code    uint32
	Retries int
	Size    int
	Tags    []string
	Urgent  bool
	Done    bool
}

func TestHeapViewOfEvent(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("expected sizes assume a 64-bit platform")
	}
	typ := reflect.TypeOf(event{})
	fields := heapFields(typ)

	plain := AnalyzeWith("events.go:3", "Event", fields, int64(typ.Align()), Options{Heap: true})
	if plain.Size != 88 || plain.Optimal != 80 || plain.Scan != 64 {
		t.Fatalf("size %d optimal %d scan %d; want 88 80 64", plain.Size, plain.Optimal, plain.Scan)
	}
	if want := "    heap: allocated as 96 bytes, could be 80 (saves 16) · GC scans 64 of 88 bytes, could be 32"; plain.HeapLine() != want {
		t.Errorf("heap line:\n got %s\nwant %s", plain.HeapLine(), want)
	}

	first := AnalyzeWith("events.go:3", "Event", fields, int64(typ.Align()), Options{Heap: true, HeapFirst: true})
	var order []string
	for _, f := range first.Suggested {
		order = append(order, f.Name)
	}
	if got, want := strings.Join(order, " "), "Name Tags ID Source Retries Size Code Urgent Done"; got != want {
		t.Errorf("heap-first order:\n got %s\nwant %s", got, want)
	}
	if first.Optimal != 80 || first.SuggestedScan != 24 {
		t.Errorf("heap-first: size %d scan %d; want 80 24", first.Optimal, first.SuggestedScan)
	}

	layout := plain.CurrentLayout()
	for _, want := range []string{
		"16  Name    string     // 16 bytes, pointer",
		"56  Tags    []string   // 24 bytes, pointer\n           -- GC scan ends at 64 --\n",
		"80  Urgent  bool       // 1 byte\n",
	} {
		if !strings.Contains(layout, want) {
			t.Errorf("current layout is missing %q:\n%s", want, layout)
		}
	}
}
