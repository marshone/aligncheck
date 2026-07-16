package aligncheck

// AlignedStruct is optimized.
type AlignedStruct struct {
	Slice []byte // 24 bytes
	Str   string // 16 bytes
	Ptr   *int   // 8 bytes
}

// BrokenStruct is intentionally misaligned to test our diagnostic output.
type BrokenStruct struct {
	IsActive   bool     // 1 byte   (Offset: 0)
	DataBuffer []byte   // 24 bytes (Offset: 24)
	Flag       byte     // 1 byte   (Offset: 48)
	Name       string   // 16 bytes (Offset: 56)
	CallbackID uint64   // 8 bytes  (Offset: 72)
}
