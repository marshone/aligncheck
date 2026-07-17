package aligncheck

import (
	"context"
	"time"
)

// AlignedStruct is optimized.
type AlignedStruct struct {
	Slice []byte // 24 bytes
	Str   string // 16 bytes
	Ptr   *int   // 8 bytes
}

// Custom type alias to test if we preserve user-defined types
type UserID string

type BrokenStruct struct {
	// 1-byte types (Offset: 0)
	IsActive       bool

	// 24-byte types (Offset: 8 - due to alignment padding)
	DataBuffer     []byte

	// 1-byte types (Offset: 32)
	Flag           byte

	// 16-byte types (Offset: 40)
	Name           string

	// 8-byte types (Offset: 56)
	CallbackID     uint64

	// Pointers (8 bytes)
	NextNode       *BrokenStruct

	// Custom Aliases (16 bytes - inherits string's header size)
	OwnerID        UserID

	// Maps (8 bytes)
	Metadata       map[string]any

	// Channels (8 bytes)
	Notification   chan bool

	// Interfaces (16 bytes)
	Context        context.Context

	// Fixed Arrays (8 bytes: 4 * 2-byte int16)
	Coordinate     [4]int16

	// Small integers (1 byte)
	Priority       int8

	// Large Structs (24 bytes)
	CreatedAt      time.Time

	// Functions (8 bytes)
	OnError        func(err error)
}
