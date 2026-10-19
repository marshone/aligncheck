package layout

import "fmt"

// sizeClasses are the Go runtime's small-object size classes
// (class_to_size in runtime/sizeclasses.go). Heap objects up to 32 KiB are
// rounded up to one of these; larger ones to whole 8 KiB pages.
var sizeClasses = []int64{0, 8, 16, 24, 32, 48, 64, 80, 96, 112, 128, 144, 160, 176, 192, 208, 224,
	240, 256, 288, 320, 352, 384, 416, 448, 480, 512, 576, 640, 704, 768, 896, 1024, 1152, 1280,
	1408, 1536, 1792, 2048, 2304, 2688, 3072, 3200, 3456, 4096, 4864, 5376, 6144, 6528, 6784,
	6912, 8192, 9472, 9728, 10240, 10880, 12288, 13568, 14336, 16384, 18432, 19072, 20480,
	21760, 24576, 27264, 28672, 32768}

const (
	maxSmallSize = 32768
	pageSize     = 8192
	tinySize     = 16
)

// HeapSize is how many bytes the runtime uses for one value allocated on its
// own (new(T), &T{}). tiny reports that the value is small and pointer-free,
// so the runtime packs it with others into shared 16-byte blocks.
func HeapSize(size int64, hasPointers bool, wordSize int64) (alloc int64, tiny bool) {
	if size == 0 {
		return 0, false
	}
	if !hasPointers && size < tinySize {
		return size, true
	}
	// Since Go 1.22, small objects with pointers larger than wordSize*wordSize*8
	// bytes (512 on 64-bit) carry a one-word header describing their pointers.
	// Objects too big to stay small with it are large objects: no header.
	if hasPointers && size > wordSize*wordSize*8 && size+wordSize <= maxSmallSize {
		size += wordSize
	}
	if size > maxSmallSize {
		return (size + pageSize - 1) / pageSize * pageSize, false
	}
	for _, c := range sizeClasses {
		if c >= size {
			return c, false
		}
	}
	return size, false
}

// HeapLine summarizes the heap view: allocation size and GC scan length,
// e.g. "heap: allocated as 96 bytes, could be 80 (saves 16) · GC scans 64 of 88 bytes, could be 24".
func (r Report) HeapLine() string {
	w := r.Options.WordSize
	if w == 0 {
		w = 8
	}
	if r.Size == 0 {
		return "    heap: zero size, never allocated"
	}

	var alloc string
	cur, tiny := HeapSize(r.Size, r.Scan > 0, w)
	opt, _ := HeapSize(r.Optimal, r.SuggestedScan > 0, w)
	switch {
	case tiny:
		alloc = "tiny allocator: packed with other small pointer-free values"
	case r.ShowsSuggestion() && opt < cur:
		alloc = fmt.Sprintf("allocated as %s, could be %d (saves %d)", Plural(cur, "byte"), opt, cur-opt)
	case cur > r.Size:
		alloc = fmt.Sprintf("allocated as %s (%s of size-class slack)", Plural(cur, "byte"), Plural(cur-r.Size, "byte"))
	default:
		alloc = "allocated as " + Plural(cur, "byte")
	}

	scan := "no pointers: the GC never scans it"
	if r.Scan > 0 {
		scan = fmt.Sprintf("GC scans %d of %s", r.Scan, Plural(r.Size, "byte"))
		if r.ShowsSuggestion() && r.SuggestedScan < r.Scan {
			scan += fmt.Sprintf(", could be %d", r.SuggestedScan)
		}
	}
	return "    heap: " + alloc + " · " + scan
}

// ScanImprovable reports that the suggested layout is scanned less by the GC.
func (r Report) ScanImprovable() bool { return r.SuggestedScan < r.Scan }

// ShowsSuggestion reports whether a suggested layout is worth showing: it is
// smaller, or, with HeapFirst, scanned less by the GC.
func (r Report) ShowsSuggestion() bool {
	return r.Optimal < r.Size || (r.Options.HeapFirst && r.ScanImprovable())
}
