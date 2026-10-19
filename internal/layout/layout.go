// Package layout computes struct layouts and formats layout reports. It is
// shared by the aligncheck library (sizes from reflect) and the aligncheck
// command (sizes from go/types), so both always tell the same story.
package layout

import (
	"fmt"
	"sort"
	"strings"
)

// Field is one struct field as declared.
type Field struct {
	Name     string // empty for embedded fields, which are written as just their type
	Type     string // type as written in the source
	Size     int64
	Align    int64
	Pointers int64 // bytes from the field's start through its last pointer word; 0 if it holds no pointers
}

// Placed is a field at its position in a layout.
type Placed struct {
	Field
	Offset   int64
	Padding  int64 // padding bytes after this field
	Trailing bool  // the padding is at the end of the struct
}

// Report describes a struct's current layout and the best one reachable by
// reordering its fields.
type Report struct {
	Location  string // "file.go:12"
	Name      string
	Size      int64
	Data      int64 // sum of field sizes
	Optimal   int64 // smallest size any field order can reach
	Current   []Placed
	Suggested []Field // optimal order

	Scan          int64 // bytes the GC scans in the current layout (0: no pointers)
	SuggestedScan int64 // bytes the GC scans in the suggested layout
	Options       Options
}

// Options adjust the analysis and the report text.
type Options struct {
	Heap      bool  // report heap allocation size and GC scan length
	HeapFirst bool  // among equally small layouts, suggest the one the GC scans least
	WordSize  int64 // pointer size in bytes; 8 if zero
}

// Analyze lays out fields in declaration order and in optimal order. align is
// the struct's alignment (the largest field alignment, at least 1).
func Analyze(location, name string, fields []Field, align int64) Report {
	return AnalyzeWith(location, name, fields, align, Options{})
}

// AnalyzeWith is Analyze with options.
func AnalyzeWith(location, name string, fields []Field, align int64, opts Options) Report {
	if opts.WordSize == 0 {
		opts.WordSize = 8
	}
	current, size := Place(fields, align)
	var data int64
	for _, f := range fields {
		data += f.Size
	}
	suggested := Optimal(fields)
	if opts.HeapFirst {
		suggested = HeapFirst(fields)
	}
	placedSuggested, optimal := Place(suggested, align)
	return Report{Location: location, Name: name, Size: size, Data: data,
		Optimal: optimal, Current: current, Suggested: suggested,
		Scan: scanEnd(current), SuggestedScan: scanEnd(placedSuggested), Options: opts}
}

// HeapFirst is Optimal, except that within each alignment group the fields
// holding pointers come first, ordered so the GC can stop scanning as early
// as possible. Reordering within an alignment group never adds padding.
func HeapFirst(fields []Field) []Field {
	out := append([]Field(nil), fields...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.Size == 0) != (b.Size == 0) {
			return a.Size == 0
		}
		if a.Align != b.Align {
			return a.Align > b.Align
		}
		if (a.Pointers > 0) != (b.Pointers > 0) {
			return a.Pointers > 0
		}
		if a.Pointers > 0 {
			// The scan ends inside the last pointer field: put the one with
			// the most non-pointer bytes after its pointers last.
			return a.Size-a.Pointers < b.Size-b.Pointers
		}
		return a.Size > b.Size
	})
	return out
}

// scanEnd is how many bytes from the start the GC must scan: the end of the
// last pointer word in the layout.
func scanEnd(placed []Placed) int64 {
	var end int64
	for _, p := range placed {
		if p.Pointers > 0 {
			end = max(end, p.Offset+p.Pointers)
		}
	}
	return end
}

// Optimal returns the fields in an order with the smallest size. Every Go
// type's size is a multiple of its alignment, so sorting by decreasing
// alignment leaves no internal padding. Zero-size fields go first, because a
// trailing zero-size field makes the compiler add padding. The sort is stable,
// so fields that compare equal keep their declared order.
func Optimal(fields []Field) []Field {
	out := append([]Field(nil), fields...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.Size == 0) != (b.Size == 0) {
			return a.Size == 0
		}
		if a.Align != b.Align {
			return a.Align > b.Align
		}
		return a.Size > b.Size
	})
	return out
}

// DescendingSize returns the fields sorted by decreasing size, zero-size
// fields first: the order WithStrictOrder requires.
func DescendingSize(fields []Field) []Field {
	out := append([]Field(nil), fields...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Size, out[j].Size
		if (a == 0) != (b == 0) {
			return a == 0
		}
		return a > b
	})
	return out
}

// Place lays fields out in the given order following the gc compiler's rules
// and returns each field's position and the struct's total size.
func Place(fields []Field, align int64) ([]Placed, int64) {
	if align < 1 {
		align = 1
	}
	placed := make([]Placed, len(fields))
	var off int64
	for i, f := range fields {
		a := max(f.Align, 1)
		aligned := (off + a - 1) &^ (a - 1)
		if i > 0 {
			placed[i-1].Padding = aligned - off
		}
		placed[i] = Placed{Field: f, Offset: aligned}
		off = aligned + f.Size
	}
	end := off
	if n := len(fields); n > 0 && fields[n-1].Size == 0 && off > 0 {
		end++ // a trailing zero-size field must not point past the struct
	}
	size := (end + align - 1) &^ (align - 1)
	if n := len(placed); n > 0 && size > off {
		placed[n-1].Padding = size - off
		placed[n-1].Trailing = true
	}
	return placed, size
}

// Headline is the one-line verdict, e.g.
// "Broken: 64 bytes (50 data + 14 padding), could be 56 (saves 8)".
func (r Report) Headline() string {
	pad := r.Size - r.Data
	head := fmt.Sprintf("%s: %s (%d data + %d padding)", r.Name, Plural(r.Size, "byte"), r.Data, pad)
	switch {
	case r.Optimal < r.Size:
		return fmt.Sprintf("%s, could be %d (saves %d)", head, r.Optimal, r.Size-r.Optimal)
	case pad == 0:
		return head + ", optimal, no padding"
	case pad == 1:
		return head + ", optimal: the padding byte is unavoidable"
	default:
		return fmt.Sprintf("%s, optimal: the %d padding bytes are unavoidable", head, pad)
	}
}

// CurrentLayout lists every field with its offset, and every padding hole.
func (r Report) CurrentLayout() string {
	var b strings.Builder
	b.WriteString("    current layout:\n")
	fields := make([]Field, len(r.Current))
	for i, p := range r.Current {
		fields[i] = p.Field
	}
	nameWidth, typeWidth := widths(fields)
	for _, p := range r.Current {
		if p.Padding > 0 {
			typeWidth = max(typeWidth, len(padLabel(p)))
		}
	}
	for _, p := range r.Current {
		fmt.Fprintf(&b, "    %5d  %-*s // %s\n", p.Offset, typeWidth, decl(p.Field, nameWidth), r.sizeNote(p.Field))
		if r.Options.Heap && r.Scan > 0 && p.Pointers > 0 && p.Offset+p.Pointers == r.Scan {
			fmt.Fprintf(&b, "           -- GC scan ends at %d --\n", r.Scan)
		}
		if p.Padding > 0 {
			fmt.Fprintf(&b, "    %5d  %-*s // %s\n", p.Offset+p.Size, typeWidth, padLabel(p), Plural(p.Padding, "byte"))
		}
	}
	return b.String()
}

// sizeNote is the trailing comment of a field line: its size, and with the
// heap view whether it holds pointers.
func (r Report) sizeNote(f Field) string {
	note := Plural(f.Size, "byte")
	if r.Options.Heap && f.Pointers > 0 {
		note += ", pointer"
	}
	return note
}

func padLabel(p Placed) string {
	if p.Trailing {
		return "(trailing padding)"
	}
	return "(padding)"
}

// SuggestedLayout is the optimal declaration, ready to copy.
func (r Report) SuggestedLayout() string {
	var b strings.Builder
	fmt.Fprintf(&b, "    suggested layout:\n    type %s struct {\n", r.Name)
	nameWidth, typeWidth := widths(r.Suggested)
	for _, f := range r.Suggested {
		fmt.Fprintf(&b, "        %-*s // %s\n", typeWidth, decl(f, nameWidth), r.sizeNote(f))
	}
	b.WriteString("    }\n")
	return b.String()
}

// widths returns the name column width and the width of "name type".
func widths(fields []Field) (nameWidth, typeWidth int) {
	for _, f := range fields {
		nameWidth = max(nameWidth, len(f.Name))
	}
	for _, f := range fields {
		typeWidth = max(typeWidth, len(decl(f, nameWidth)))
	}
	return nameWidth, typeWidth
}

func decl(f Field, nameWidth int) string {
	if f.Name == "" {
		return f.Type
	}
	return fmt.Sprintf("%-*s %s", nameWidth, f.Name, f.Type)
}

// Plural formats a count with its noun, e.g. "1 byte", "16 bytes".
func Plural(n int64, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}
