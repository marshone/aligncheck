package aligncheck

import (
	"testing"
)

func TestStructAlignments(t *testing.T) {
	registry := map[string]interface{}{
		"AlignedStruct": AlignedStruct{},
		"BrokenStruct":  BrokenStruct{},
	}

	AssertAllInPackageAligned(t, registry)
}
