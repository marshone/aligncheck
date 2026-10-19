package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/printer"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/marshone/aligncheck/internal/layout"
)

func runInspect(args []string) error {
	flags := flag.NewFlagSet("inspect", flag.ContinueOnError)
	arch := flags.String("arch", defaultArch(), "target architecture for sizes (e.g. amd64, arm64, 386)")
	verbose := flags.Bool("v", false, "also show each struct's current layout: field offsets and padding")
	heap := flags.Bool("heap", false, "also show heap allocation size and how much of each struct the GC scans")
	heapFirst := flags.Bool("heap-first", false, "suggest layouts the GC scans least (implies -heap)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	patterns := flags.Args()
	if len(patterns) == 0 {
		patterns = []string{"."}
	}
	opts := layout.Options{Heap: *heap || *heapFirst, HeapFirst: *heapFirst}
	return inspect(os.Stdout, patterns, *arch, *verbose, opts)
}

func defaultArch() string {
	if a := os.Getenv("GOARCH"); a != "" {
		return a
	}
	return runtime.GOARCH
}

// listedPackage is the subset of `go list -json` output inspect needs.
type listedPackage struct {
	ImportPath string
	Name       string
	Dir        string
	GoFiles    []string
	Export     string
	ImportMap  map[string]string
	DepOnly    bool
	Error      *struct{ Err string }
}

// inspect type-checks the packages matching patterns and reports, for each
// top-level struct, its size and the smallest size a reordering can reach.
func inspect(w io.Writer, patterns []string, arch string, verbose bool, opts layout.Options) error {
	sizes := types.SizesFor("gc", arch)
	if sizes == nil {
		return fmt.Errorf("unknown architecture %q", arch)
	}
	opts.WordSize = sizes.Sizeof(types.Typ[types.UnsafePointer])

	pkgs, err := goList(patterns)
	if err != nil {
		return err
	}
	exports := map[string]string{}
	for _, p := range pkgs {
		if p.Export != "" {
			exports[p.ImportPath] = p.Export
		}
	}

	var inspected, improvable, scanImprovable int
	var saved, padding int64
	for _, p := range pkgs {
		if p.DepOnly {
			continue
		}
		if p.Error != nil {
			return fmt.Errorf("%s: %s", p.ImportPath, p.Error.Err)
		}
		reports, err := inspectPackage(p, exports, sizes, opts)
		if err != nil {
			return err
		}
		for _, r := range reports {
			inspected++
			padding += r.Size - r.Data
			if r.Optimal < r.Size {
				improvable++
				saved += r.Size - r.Optimal
			}
			if opts.HeapFirst && r.ScanImprovable() {
				scanImprovable++
			}
			printReport(w, r, verbose)
		}
	}

	removable := "none removable by reordering"
	if saved > 0 {
		removable = fmt.Sprintf("%s removable by reordering %s", layout.Plural(saved, "byte"), layout.Plural(int64(improvable), "struct"))
	}
	fmt.Fprintf(w, "%s inspected (%s): %s of padding, %s\n",
		layout.Plural(int64(inspected), "struct"), arch, layout.Plural(padding, "byte"), removable)
	if opts.HeapFirst {
		fmt.Fprintf(w, "%s could be scanned less by the GC with pointer fields first\n",
			layout.Plural(int64(scanImprovable), "struct"))
	}
	return nil
}

// goList runs `go list -export -deps -json`, which builds export data for every
// dependency so the targets can be type-checked without x/tools.
func goList(patterns []string) ([]listedPackage, error) {
	args := append([]string{"list", "-e", "-export", "-deps", "-json"}, patterns...)
	cmd := exec.Command("go", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list: %v\n%s", err, stderr.String())
	}
	var pkgs []listedPackage
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p listedPackage
		if err := dec.Decode(&p); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, fmt.Errorf("go list output: %w", err)
		}
		pkgs = append(pkgs, p)
	}
	return pkgs, nil
}

func inspectPackage(p listedPackage, exports map[string]string, sizes types.Sizes, opts layout.Options) ([]layout.Report, error) {
	fset := token.NewFileSet()
	var files []*ast.File
	for _, name := range p.GoFiles {
		f, err := parser.ParseFile(fset, filepath.Join(p.Dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}

	lookup := func(path string) (io.ReadCloser, error) {
		if mapped, ok := p.ImportMap[path]; ok {
			path = mapped
		}
		file, ok := exports[path]
		if !ok {
			return nil, fmt.Errorf("no export data for %s", path)
		}
		return os.Open(file)
	}
	conf := types.Config{Importer: importer.ForCompiler(fset, "gc", lookup), Sizes: sizes}
	pkg, err := conf.Check(p.ImportPath, fset, files, nil)
	if err != nil {
		return nil, fmt.Errorf("type-check %s: %w", p.ImportPath, err)
	}

	var reports []layout.Report
	for _, f := range files {
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				ts := spec.(*ast.TypeSpec)
				astStruct, ok := ts.Type.(*ast.StructType)
				if !ok || ts.TypeParams != nil || ts.Assign.IsValid() {
					continue // not a struct, generic, or an alias
				}
				obj, ok := pkg.Scope().Lookup(ts.Name.Name).(*types.TypeName)
				if !ok {
					continue
				}
				st, ok := obj.Type().Underlying().(*types.Struct)
				if !ok {
					continue
				}
				pos := fset.Position(ts.Pos())
				location := fmt.Sprintf("%s:%d", relPath(pos.Filename), pos.Line)
				fields := fieldsOf(fset, astStruct, st, sizes)
				reports = append(reports, layout.AnalyzeWith(location, ts.Name.Name, fields, sizes.Alignof(obj.Type()), opts))
			}
		}
	}
	return reports, nil
}

// fieldsOf describes a struct's fields for the layout engine: sizes and
// alignments from go/types, type text exactly as written in the source.
func fieldsOf(fset *token.FileSet, astStruct *ast.StructType, st *types.Struct, sizes types.Sizes) []layout.Field {
	var typeText []string
	for _, field := range astStruct.Fields.List {
		var buf bytes.Buffer
		_ = printer.Fprint(&buf, fset, field.Type)
		n := len(field.Names)
		if n == 0 {
			n = 1 // embedded field
		}
		for i := 0; i < n; i++ {
			typeText = append(typeText, buf.String())
		}
	}

	fields := make([]layout.Field, st.NumFields())
	for i := range fields {
		v := st.Field(i)
		text := types.TypeString(v.Type(), types.RelativeTo(v.Pkg()))
		if len(typeText) == len(fields) && !strings.Contains(typeText[i], "\n") {
			text = typeText[i] // as written, unless it spans several lines
		}
		name := v.Name()
		if v.Embedded() {
			name = "" // embedded fields are written as just their type
		}
		fields[i] = layout.Field{Name: name, Type: text, Size: sizes.Sizeof(v.Type()), Align: sizes.Alignof(v.Type()),
			Pointers: pointerBytes(v.Type(), sizes)}
	}
	return fields
}

// pointerBytes is how many bytes from the start of a value of type t the GC
// must scan: the end of its last pointer word (the runtime's "ptrdata").
func pointerBytes(t types.Type, sizes types.Sizes) int64 {
	word := sizes.Sizeof(types.Typ[types.UnsafePointer])
	switch u := t.Underlying().(type) {
	case *types.Basic:
		if u.Kind() == types.String || u.Kind() == types.UnsafePointer {
			return word
		}
	case *types.Pointer, *types.Map, *types.Chan, *types.Signature, *types.Slice:
		return word
	case *types.Interface:
		return 2 * word
	case *types.Array:
		if e := pointerBytes(u.Elem(), sizes); e > 0 && u.Len() > 0 {
			return (u.Len()-1)*sizes.Sizeof(u.Elem()) + e
		}
	case *types.Struct:
		vars := make([]*types.Var, u.NumFields())
		for i := range vars {
			vars[i] = u.Field(i)
		}
		offsets := sizes.Offsetsof(vars)
		var end int64
		for i, v := range vars {
			if p := pointerBytes(v.Type(), sizes); p > 0 {
				end = max(end, offsets[i]+p)
			}
		}
		return end
	}
	return 0
}

func printReport(w io.Writer, r layout.Report, verbose bool) {
	fmt.Fprintf(w, "%s: %s\n", r.Location, r.Headline())
	if r.Options.Heap {
		fmt.Fprintln(w, r.HeapLine())
	}
	suggest := r.ShowsSuggestion()
	if verbose {
		fmt.Fprint(w, "\n"+r.CurrentLayout())
	}
	if suggest {
		fmt.Fprint(w, "\n"+r.SuggestedLayout())
	}
	if verbose || suggest {
		fmt.Fprintln(w)
	}
}

func relPath(path string) string {
	if wd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(wd, path); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return path
}
