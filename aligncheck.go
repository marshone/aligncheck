package aligncheck

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

/*
Go Type Size Cheat Sheet (64-bit Architectures)
Size 	Go Types / Structures
32 	Bytesnetip.AddrPort
24 	Bytestime.Time, slices ([]T), strings (wait, strings are 16 bytes: ptr + len. Slices are 24 bytes: ptr + len + cap)
16 	Bytesstring, interfaces (interface{} / any), netip.Addr
8 	BytesAll pointers (*T), maps, channels, functions (func), int64, uint64, float64
4 	Bytesint32, uint32, float32, rune
2 	Bytesint16, uint16
1 	Byteint8, uint8, byte, bool
*/

// AssertAllInPackageAligned automatically parses every production struct declared
// in the current package directory and runs optimal alignment checks on it.
func AssertAllInPackageAligned(t *testing.T, registry map[string]interface{}) {
	fset := token.NewFileSet()

	filter := func(info fs.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}

	pkgs, err := parser.ParseDir(fset, ".", filter, parser.AllErrors)
	if err != nil {
		t.Fatalf("failed to parse package files: %v", err)
	}

	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				typeSpec, ok := n.(*ast.TypeSpec)
				if !ok {
					return true
				}
				structType, ok := typeSpec.Type.(*ast.StructType)
				if !ok {
					return true
				}

				structName := typeSpec.Name.Name

				pos := fset.Position(typeSpec.Pos())
				relPath, err := filepath.Rel(".", pos.Filename)
				if err != nil {
					relPath = pos.Filename
				}
				location := fmt.Sprintf("%s:%d", relPath, pos.Line)

				instance, registered := registry[structName]
				if !registered {
					t.Fatalf("[%s] Struct %q is declared but NOT registered in alignment test! "+
						"Please add %s{} to your test registry map to ensure it is validated.",
						location, structName, structName)
					return true
				}

				// Map AST fields to their raw code-defined types
				astFieldTypes := make(map[string]string)
				for _, field := range structType.Fields.List {
					typeStr := getASTTypeString(fset, field.Type)
					for _, nameNode := range field.Names {
						astFieldTypes[nameNode.Name] = typeStr
					}
					// Handle anonymous (embedded) fields
					if len(field.Names) == 0 {
						embedName := getEmbeddedFieldName(field.Type)
						if embedName != "" {
							astFieldTypes[embedName] = typeStr
						}
					}
				}

				AssertOptimalAlignmentWithSuggestions(t, instance, location, astFieldTypes)
				return true
			})
		}
	}
}

// AssertOptimalAlignmentWithSuggestions checks the field order sorting,
// isolates internal compiler padding, and suggests an optimal layout on failure.
func AssertOptimalAlignmentWithSuggestions(t *testing.T, instance interface{}, location string, astFieldTypes ...map[string]string) {
	typ := reflect.TypeOf(instance)
	if typ.Kind() != reflect.Struct {
		return
	}

	var fieldTypeMap map[string]string
	if len(astFieldTypes) > 0 {
		fieldTypeMap = astFieldTypes[0]
	}

	// Helper to resolve AST type if available, falling back to reflection
	getTypeStr := func(f reflect.StructField) string {
		if fieldTypeMap != nil {
			if astType, exists := fieldTypeMap[f.Name]; exists {
				return astType
			}
		}
		return f.Type.String()
	}

	t.Run("Alignment_"+typ.Name(), func(t *testing.T) {
		hasViolation := false

		// 1. Verify fields are sorted descending by size
		for i := 0; i < typ.NumField()-1; i++ {
			f1 := typ.Field(i)
			f2 := typ.Field(i + 1)

			if f1.Type.Size() == 0 || f2.Type.Size() == 0 {
				continue
			}

			if f1.Type.Size() < f2.Type.Size() {
				hasViolation = true
				t.Errorf("[%s] Field alignment violation in '%s': field '%s' (%s, size %d bytes) appears before larger field '%s' (%s, size %d bytes).",
					location, typ.Name(), f1.Name, getTypeStr(f1), f1.Type.Size(), f2.Name, getTypeStr(f2), f2.Type.Size())
			}
		}

		// 2. Identify and isolate internal padding (ignoring trailing padding)
		largestFieldAlign := uintptr(1)
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			if f.Type.Align() > int(largestFieldAlign) {
				largestFieldAlign = uintptr(f.Type.Align())
			}
		}

		sumOfFields := uintptr(0)
		for i := 0; i < typ.NumField(); i++ {
			sumOfFields += typ.Field(i).Type.Size()
		}

		expectedOptimalSize := (sumOfFields + largestFieldAlign - 1) &^ (largestFieldAlign - 1)

		if typ.Size() > expectedOptimalSize {
			hasViolation = true
			internalPadding := typ.Size() - expectedOptimalSize
			t.Errorf("[%s] Struct '%s' contains %d bytes of internal compiler padding. Actual size: %d, Optimal size: %d.",
				location, typ.Name(), internalPadding, typ.Size(), expectedOptimalSize)
		}

		// 3. Print Suggestion on Failure
		if hasViolation {
			fields := make([]reflect.StructField, typ.NumField())
			for i := 0; i < typ.NumField(); i++ {
				fields[i] = typ.Field(i)
			}

			// Sort stable to keep user's equal-sized fields in their relative order
			sort.SliceStable(fields, func(i, j int) bool {
				return fields[i].Type.Size() > fields[j].Type.Size()
			})

			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("\n[SUGGESTED ALIGNMENT] Modify '%s' at %s:\n\ntype %s struct {\n", typ.Name(), location, typ.Name()))
			for _, f := range fields {
				sb.WriteString(fmt.Sprintf("\t%-15s %-20s // %d bytes\n", f.Name, getTypeStr(f), f.Type.Size()))
			}
			sb.WriteString("}\n")
			t.Log(sb.String())
		}
	})
}

// getASTTypeString converts an AST expression back into its literal source representation.
func getASTTypeString(fset *token.FileSet, expr ast.Expr) string {
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, expr); err != nil {
		return ""
	}
	return buf.String()
}

// getEmbeddedFieldName returns the base name of an embedded/anonymous field.
func getEmbeddedFieldName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return getEmbeddedFieldName(t.X)
	case *ast.SelectorExpr:
		return t.Sel.Name
	default:
		return ""
	}
}
