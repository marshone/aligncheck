package layout

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"
)

type (
	padded struct {
		IsActive   bool
		DataBuffer []byte
		Flag       byte
		Name       string
		CallbackID uint64
	}
	noPadding struct {
		A int64
		B int32
		C int32
		D int64
	}
	odd struct {
		A [3]byte
		B uint16
		C uint8
	}
	unavoidable struct {
		Name     string
		Head     uint64
		Dialect  uint32
		Lifetime uint8
		Gen      uint64
	}
	trailingZero struct {
		ID   int64
		Mark struct{}
	}
	embedded struct {
		Ready bool
		sync.Mutex
		Count int64
	}
	mixed struct {
		Flag    bool
		Ctx     context.Context
		At      time.Time
		Small   int16
		Ptr     *int
		Arr     [5]uint32
		Empty   struct{}
		Fn      func()
		Another bool
	}
	allZero struct {
		A struct{}
		B [0]int64
	}
)

func fieldsOf(typ reflect.Type) []Field {
	fields := make([]Field, typ.NumField())
	for i := range fields {
		f := typ.Field(i)
		name := f.Name
		if f.Anonymous {
			name = ""
		}
		fields[i] = Field{Name: name, Type: f.Type.String(), Size: int64(f.Type.Size()), Align: int64(f.Type.FieldAlign())}
	}
	return fields
}

var all = []any{padded{}, noPadding{}, odd{}, unavoidable{}, trailingZero{}, embedded{}, mixed{}, allZero{}}

// TestPlaceMatchesCompiler checks the simulation against the real compiler:
// laying out the declared order must give the compiler's size and offsets.
func TestPlaceMatchesCompiler(t *testing.T) {
	for _, instance := range all {
		typ := reflect.TypeOf(instance)
		placed, size := Place(fieldsOf(typ), int64(typ.Align()))
		if size != int64(typ.Size()) {
			t.Errorf("%s: simulated %d bytes, compiler says %d", typ.Name(), size, typ.Size())
		}
		for i, p := range placed {
			if want := int64(typ.Field(i).Offset); p.Offset != want {
				t.Errorf("%s.%s: simulated offset %d, compiler says %d", typ.Name(), typ.Field(i).Name, p.Offset, want)
			}
		}
	}
}

func TestAnalyze(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("expected sizes assume a 64-bit platform")
	}
	for _, tc := range []struct {
		instance            any
		size, data, optimal int64
	}{
		{padded{}, 64, 50, 56},
		{noPadding{}, 24, 24, 24},   // not in descending size order, but no padding
		{odd{}, 8, 6, 6},            // [3]byte before uint16 wastes 2 bytes
		{unavoidable{}, 40, 37, 40}, // 3 padding bytes can only move
		{trailingZero{}, 16, 8, 8},
		{embedded{}, 24, 17, 24},
		{allZero{}, 0, 0, 0},
	} {
		typ := reflect.TypeOf(tc.instance)
		r := Analyze("x.go:1", typ.Name(), fieldsOf(typ), int64(typ.Align()))
		if r.Size != tc.size || r.Data != tc.data || r.Optimal != tc.optimal {
			t.Errorf("%s: size %d data %d optimal %d; want %d %d %d",
				typ.Name(), r.Size, r.Data, r.Optimal, tc.size, tc.data, tc.optimal)
		}
	}
}

func TestReportText(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("expected text assumes a 64-bit platform")
	}
	typ := reflect.TypeOf(padded{})
	r := Analyze("models.go:3", "BrokenStruct", fieldsOf(typ), int64(typ.Align()))

	if got, want := r.Headline(), "BrokenStruct: 64 bytes (50 data + 14 padding), could be 56 (saves 8)"; got != want {
		t.Errorf("headline:\n got %s\nwant %s", got, want)
	}
	wantCurrent := "    current layout:\n" +
		"        0  IsActive   bool    // 1 byte\n" +
		"        1  (padding)          // 7 bytes\n" +
		"        8  DataBuffer []uint8 // 24 bytes\n" +
		"       32  Flag       uint8   // 1 byte\n" +
		"       33  (padding)          // 7 bytes\n" +
		"       40  Name       string  // 16 bytes\n" +
		"       56  CallbackID uint64  // 8 bytes\n"
	if got := r.CurrentLayout(); got != wantCurrent {
		t.Errorf("current layout:\n%s\nwant:\n%s", got, wantCurrent)
	}
	wantSuggested := "    suggested layout:\n" +
		"    type BrokenStruct struct {\n" +
		"        DataBuffer []uint8 // 24 bytes\n" +
		"        Name       string  // 16 bytes\n" +
		"        CallbackID uint64  // 8 bytes\n" +
		"        IsActive   bool    // 1 byte\n" +
		"        Flag       uint8   // 1 byte\n" +
		"    }\n"
	if got := r.SuggestedLayout(); got != wantSuggested {
		t.Errorf("suggested layout:\n%s\nwant:\n%s", got, wantSuggested)
	}

	u := reflect.TypeOf(unavoidable{})
	ur := Analyze("memory.go:132", "BranchState", fieldsOf(u), int64(u.Align()))
	if !strings.Contains(ur.Headline(), "optimal: the 3 padding bytes are unavoidable") {
		t.Errorf("unexpected headline: %s", ur.Headline())
	}
	// Reordered, the 3 bytes move to the end: that is trailing padding.
	_, size := Place(ur.Suggested, int64(u.Align()))
	placed, _ := Place(ur.Suggested, int64(u.Align()))
	if last := placed[len(placed)-1]; !last.Trailing || last.Padding != 3 || size != 40 {
		t.Errorf("expected 3 bytes of trailing padding at the end, got %+v (size %d)", last, size)
	}
}

// TestDescendingSize: the strict-mode order must obey the strict rule, even
// where the optimal order would not ([3]byte is bigger but less aligned).
func TestDescendingSize(t *testing.T) {
	fields := []Field{
		{Name: "B", Type: "uint16", Size: 2, Align: 2},
		{Name: "A", Type: "[3]byte", Size: 3, Align: 1},
		{Name: "Z", Type: "struct{}", Size: 0, Align: 1},
	}
	var got, opt []string
	for _, f := range DescendingSize(fields) {
		got = append(got, f.Name)
	}
	for _, f := range Optimal(fields) {
		opt = append(opt, f.Name)
	}
	if want := []string{"Z", "A", "B"}; !reflect.DeepEqual(got, want) {
		t.Errorf("DescendingSize order %v, want %v", got, want)
	}
	if want := []string{"Z", "B", "A"}; !reflect.DeepEqual(opt, want) {
		t.Errorf("Optimal order %v, want %v", opt, want)
	}
}
