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
	"strings"
	"testing"

	"github.com/marshone/aligncheck/internal/layout"
)

/*
Go type size cheat sheet (64-bit architectures)

Size  Go types
32    netip.AddrPort
24    slices ([]T), time.Time, netip.Addr
16    strings, interfaces (interface{} / any)
8     pointers (*T), maps, channels, functions (func), int, int64, uint64, float64, uintptr
4     int32, uint32, float32, rune
2     int16, uint16
1     int8, uint8, byte, bool
0     struct{}, [0]T
*/

// Option configures AssertAllInPackageAligned.
type Option func(*config)

type config struct {
	strictOrder bool
}

// WithStrictOrder also requires fields to be declared in descending size order,
// even where reordering would not save any memory. Use it to enforce a layout
// convention; without it, a struct fails only when reordering would shrink it.
func WithStrictOrder() Option {
	return func(c *config) { c.strictOrder = true }
}

// AssertAllInPackageAligned parses every top-level struct declared in the
// current package directory and checks that no field reordering would make it
// smaller. Every struct must be registered, so new ones can't slip through.
func AssertAllInPackageAligned(t *testing.T, registry map[string]interface{}, opts ...Option) {
	var cfg config
	for _, opt := range opts {
		opt(&cfg)
	}

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
			// Only top-level declarations: a struct declared inside a function
			// can't be registered from a test.
			for _, decl := range file.Decls {
				gen, ok := decl.(*ast.GenDecl)
				if !ok || gen.Tok != token.TYPE {
					continue
				}
				for _, spec := range gen.Specs {
					typeSpec := spec.(*ast.TypeSpec)
					structType, ok := typeSpec.Type.(*ast.StructType)
					if !ok || typeSpec.Assign.IsValid() {
						continue // not a struct, or an alias
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
						t.Fatalf("%s: struct %s is declared but not registered in the alignment test: "+
							"add %q: %s{} to the registry map", location, structName, structName, structName)
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
							if embedName := getEmbeddedFieldName(field.Type); embedName != "" {
								astFieldTypes[embedName] = typeStr
							}
						}
					}

					check(t, instance, location, astFieldTypes, cfg)
				}
			}
		}
	}
}

// AssertOptimalAlignmentWithSuggestions checks one struct: it fails when a field
// reordering would make the struct smaller, and logs the optimal layout.
func AssertOptimalAlignmentWithSuggestions(t *testing.T, instance interface{}, location string, astFieldTypes ...map[string]string) {
	var fieldTypeMap map[string]string
	if len(astFieldTypes) > 0 {
		fieldTypeMap = astFieldTypes[0]
	}
	check(t, instance, location, fieldTypeMap, config{})
}

// fieldsOf describes a struct's fields for the layout engine: sizes and
// alignments from reflect, type text as written in the source when known.
func fieldsOf(typ reflect.Type, fieldTypeMap map[string]string) []layout.Field {
	fields := make([]layout.Field, typ.NumField())
	for i := range fields {
		f := typ.Field(i)
		typeText := f.Type.String()
		if astType, ok := fieldTypeMap[f.Name]; ok && !strings.Contains(astType, "\n") {
			typeText = astType // as written, unless it spans several lines
		}
		name := f.Name
		if f.Anonymous {
			name = "" // embedded fields are written as just their type
		}
		fields[i] = layout.Field{Name: name, Type: typeText,
			Size: int64(f.Type.Size()), Align: int64(f.Type.FieldAlign())}
	}
	return fields
}

func check(t *testing.T, instance interface{}, location string, fieldTypeMap map[string]string, cfg config) {
	t.Helper()
	typ := reflect.TypeOf(instance)
	if typ.Kind() != reflect.Struct {
		return
	}
	fields := fieldsOf(typ, fieldTypeMap)
	r := layout.Analyze(location, typ.Name(), fields, int64(typ.Align()))

	t.Run("Alignment_"+typ.Name(), func(t *testing.T) {
		failed := false

		// Optional convention: fields declared in descending size order.
		if cfg.strictOrder {
			for i := 0; i+1 < len(fields); i++ {
				f1, f2 := fields[i], fields[i+1]
				if f1.Size > 0 && f2.Size > 0 && f1.Size < f2.Size {
					failed = true
					t.Errorf("%s: %s: field %s (%s, %s) is declared before larger field %s (%s, %s) [strict order]",
						location, typ.Name(), displayName(f1), f1.Type, layout.Plural(f1.Size, "byte"),
						displayName(f2), f2.Type, layout.Plural(f2.Size, "byte"))
				}
			}
			// The suggestion must satisfy the strict rule itself.
			r.Suggested = layout.DescendingSize(fields)
		}

		if r.Optimal < r.Size {
			failed = true
			t.Errorf("%s: %s", location, r.Headline())
		}

		if failed {
			t.Log("\n" + r.CurrentLayout() + "\n" + r.SuggestedLayout())
		}
	})
}

func displayName(f layout.Field) string {
	if f.Name == "" {
		return f.Type
	}
	return f.Name
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
