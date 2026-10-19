package aligncheck

import (
	"reflect"
	"sync"
	"testing"
)

type UserID string

type adapterSample struct {
	Flag bool
	sync.Mutex
	Owner UserID
	Count int64
}

// TestFieldsOf checks the reflect adapter: sizes and alignments come from the
// compiler, type text from the source when known, embedded fields are unnamed.
func TestFieldsOf(t *testing.T) {
	typ := reflect.TypeOf(adapterSample{})
	fields := fieldsOf(typ, map[string]string{"Owner": "UserID", "Mutex": "sync.Mutex"})

	if len(fields) != 4 {
		t.Fatalf("got %d fields, want 4", len(fields))
	}
	for i, f := range fields {
		rf := typ.Field(i)
		if f.Size != int64(rf.Type.Size()) || f.Align != int64(rf.Type.FieldAlign()) {
			t.Errorf("%s: size/align %d/%d, compiler says %d/%d", rf.Name, f.Size, f.Align, rf.Type.Size(), rf.Type.FieldAlign())
		}
	}
	if fields[1].Name != "" || fields[1].Type != "sync.Mutex" {
		t.Errorf("embedded field: got name %q type %q, want \"\" and sync.Mutex", fields[1].Name, fields[1].Type)
	}
	if fields[2].Type != "UserID" {
		t.Errorf("source type name lost: got %q, want UserID", fields[2].Type)
	}
	if fields[3].Type != "int64" {
		t.Errorf("fallback to reflect type: got %q, want int64", fields[3].Type)
	}
}
