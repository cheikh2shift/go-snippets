package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const sampleDir = "sample"

func main() {
	fmt.Println("==============================================")
	fmt.Println("  AUDITING AN AI-WRITTEN CODEBASE: FIELD GUIDE")
	fmt.Println("==============================================")
	fmt.Println()

	if err := writeSample(); err != nil {
		fmt.Println("ERROR generating sample:", err)
		os.Exit(1)
	}
	fmt.Println("[setup] sample AI-written codebase written to ./sample")
	fmt.Println()

	auditUnusedDeps()
	auditUnusedImports()
	auditShadowing()
	auditDuplication()
	auditDeadCode()

	fmt.Println("==============================================")
	fmt.Println("  AUDIT COMPLETE — see findings above")
	fmt.Println("==============================================")
}

// ---------------------------------------------------------------------------
// [1] UNUSED DEPENDENCIES
//     A dep is "unused" when it's required in go.mod but never imported.
//     (In the real world you'd run `go mod tidy -diff`; here we do it offline.)
// ---------------------------------------------------------------------------
func auditUnusedDeps() {
	fmt.Println("--- [1] UNUSED DEPENDENCIES ---")
	data, err := os.ReadFile(filepath.Join(sampleDir, "go.mod"))
	if err != nil {
		fmt.Println("  error:", err)
		return
	}
	var requires []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "github.com/") || strings.HasPrefix(line, "golang.org/") {
			fields := strings.Fields(line)
			if len(fields) >= 1 {
				requires = append(requires, fields[0])
			}
		}
	}
	imports := map[string]bool{}
	fset := token.NewFileSet()
	for _, f := range parseFiles(fset) {
		for _, imp := range f.Imports {
			imports[strings.Trim(imp.Path.Value, "\"")] = true
		}
	}
	found := false
	for _, r := range requires {
		if !imports[r] {
			fmt.Printf("  UNUSED DEP: %q required in go.mod but never imported\n", r)
			found = true
		}
	}
	if !found {
		fmt.Println("  no unused dependencies")
	}
	fmt.Println()
}

// ---------------------------------------------------------------------------
// [2] UNUSED IMPORTS
//     An import is unused when its package name never appears in the file.
// ---------------------------------------------------------------------------
func auditUnusedImports() {
	fmt.Println("--- [2] UNUSED IMPORTS ---")
	fset := token.NewFileSet()
	for _, f := range parseFiles(fset) {
		used := map[string]bool{}
		ast.Inspect(f, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				used[id.Name] = true
			}
			return true
		})
		for _, imp := range f.Imports {
			name := importName(imp)
			if !used[name] {
				fmt.Printf("  UNUSED IMPORT: %s (imported at %s)\n",
					imp.Path.Value, fset.Position(imp.Pos()))
			}
		}
	}
	fmt.Println()
}

// ---------------------------------------------------------------------------
// [3] SHADOWED NAMES
//     A name is shadowed when an inner scope redeclares a name that already
//     exists in an outer scope. Classic source of "why did my value change?"
// ---------------------------------------------------------------------------
func auditShadowing() {
	fmt.Println("--- [3] SHADOWED NAMES ---")
	fset := token.NewFileSet()
	for _, f := range parseFiles(fset) {
		v := &shadowVisitor{fset: fset, scopes: []map[string]bool{{}}}
		ast.Walk(v, f)
	}
	fmt.Println()
}

type shadowVisitor struct {
	fset   *token.FileSet
	scopes []map[string]bool
}

func (v *shadowVisitor) Enter(n ast.Node) ast.Visitor {
	switch node := n.(type) {
	case *ast.FuncDecl:
		v.scopes = append(v.scopes, map[string]bool{})
		if node.Type.Params != nil {
			for _, p := range node.Type.Params.List {
				for _, name := range p.Names {
					v.scopes[len(v.scopes)-1][name.Name] = true
				}
			}
		}
	case *ast.BlockStmt:
		v.scopes = append(v.scopes, map[string]bool{})
	case *ast.AssignStmt:
		if node.Tok == token.DEFINE {
			for _, e := range node.Lhs {
				if id, ok := e.(*ast.Ident); ok && id.Name != "_" {
					v.check(id.Name)
					v.scopes[len(v.scopes)-1][id.Name] = true
				}
			}
		}
	case *ast.ValueSpec:
		for _, name := range node.Names {
			if name.Name != "_" {
				v.check(name.Name)
				v.scopes[len(v.scopes)-1][name.Name] = true
			}
		}
	}
	return v
}

func (v *shadowVisitor) Leave(n ast.Node) {
	switch n.(type) {
	case *ast.FuncDecl, *ast.BlockStmt:
		v.scopes = v.scopes[:len(v.scopes)-1]
	}
}

func (v *shadowVisitor) check(name string) {
	for i := 0; i < len(v.scopes)-1; i++ {
		if v.scopes[i][name] {
			fmt.Printf("  SHADOWED: %q redeclared in an inner scope\n", name)
			return
		}
	}
}

// ---------------------------------------------------------------------------
// [4] DUPLICATED LOGIC
//     Normalize each function body (identifiers -> X, literals -> L, ops -> OP)
//     and compare. Identical normalized bodies = copy-pasted logic.
// ---------------------------------------------------------------------------
func auditDuplication() {
	fmt.Println("--- [4] DUPLICATED LOGIC ---")
	fset := token.NewFileSet()
	type fn struct {
		name string
		norm string
	}
	var fns []fn
	for _, f := range parseFiles(fset) {
		for _, decl := range f.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok && fd.Body != nil {
				fns = append(fns, fn{fd.Name.Name, normalize(fd.Body)})
			}
		}
	}
	found := false
	for i := 0; i < len(fns); i++ {
		for j := i + 1; j < len(fns); j++ {
			if fns[i].norm == fns[j].norm {
				fmt.Printf("  DUPLICATED: %s() and %s() have identical logic\n",
					fns[i].name, fns[j].name)
				found = true
			}
		}
	}
	if !found {
		fmt.Println("  no duplicated logic found")
	}
	fmt.Println()
}

func normalize(n ast.Node) string {
	var buf bytes.Buffer
	ast.Inspect(n, func(x ast.Node) bool {
		switch t := x.(type) {
		case *ast.Ident:
			buf.WriteString("X")
		case *ast.BasicLit:
			buf.WriteString("L")
		case *ast.BinaryExpr:
			buf.WriteString("OP")
		case *ast.ReturnStmt:
			buf.WriteString("RET")
		case *ast.IfStmt:
			buf.WriteString("IF")
		case *ast.AssignStmt:
			buf.WriteString("ASGN")
		}
		return true
	})
	return buf.String()
}

// ---------------------------------------------------------------------------
// [5] DEAD CODE
//     A package-level func/var that is declared but never referenced.
// ---------------------------------------------------------------------------
func auditDeadCode() {
	fmt.Println("--- [5] DEAD CODE (declared but never referenced) ---")
	fset := token.NewFileSet()
	files := parseFiles(fset)
	declared := map[string]string{}
	referenced := map[string]bool{}
	for _, f := range files {
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				declared[d.Name.Name] = "func"
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					if vs, ok := spec.(*ast.ValueSpec); ok {
						for _, name := range vs.Names {
							declared[name.Name] = "var"
						}
					}
				}
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				referenced[id.Name] = true
			}
			return true
		})
	}
	var dead []string
	for name, kind := range declared {
		if name == "main" || isExported(name) {
			continue
		}
		if !referenced[name] {
			dead = append(dead, fmt.Sprintf("%s %s", kind, name))
		}
	}
	sort.Strings(dead)
	if len(dead) == 0 {
		fmt.Println("  no dead code found")
	} else {
		for _, d := range dead {
			fmt.Printf("  DEAD: %s is declared but never referenced\n", d)
		}
	}
	fmt.Println()
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------
func importName(imp *ast.ImportSpec) string {
	if imp.Name != nil {
		return imp.Name.Name
	}
	path := strings.Trim(imp.Path.Value, "\"")
	parts := strings.Split(path, "/")
	return parts[len(parts)-1]
}

func isExported(name string) bool {
	return name != "" && name[0] >= 'A' && name[0] <= 'Z'
}

func parseFiles(fset *token.FileSet) []*ast.File {
	var files []*ast.File
	entries, err := os.ReadDir(sampleDir)
	if err != nil {
		return files
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(sampleDir, e.Name()), nil, 0)
		if err != nil {
			fmt.Println("  parse error:", err)
			continue
		}
		files = append(files, f)
	}
	return files
}

// ---------------------------------------------------------------------------
// generate the sample "AI-written" codebase
// ---------------------------------------------------------------------------
func writeSample() error {
	if err := os.MkdirAll(sampleDir, 0o755); err != nil {
		return err
	}
	files := map[string]string{
		"go.mod": `module example.com/aiwritten

go 1.21

require (
	github.com/google/uuid v1.6.0
	github.com/pkg/errors v0.9.1
)
`,
		"main.go": `package main

import (
	"fmt"
	"os" // imported but never used

	"github.com/google/uuid"
)

func main() {
	id := uuid.New()
	fmt.Println("order id:", id)

	order := buildOrder()
	fmt.Println("order:", order)

	total1 := applyDiscount(100.0, 0.1)
	total2 := applyTax(200.0, 0.2)
	fmt.Println("totals:", total1, total2)
}
`,
		"service.go": `package main

import (
	"fmt"
	"strings"
)

// buildOrder returns a fake order. Contains a shadowed variable.
func buildOrder() string {
	name := "widget"
	if len(name) > 0 {
		name := strings.ToUpper(name) // shadows the outer name
		fmt.Println("inner:", name)
	}
	return name
}

// applyDiscount applies a discount.
func applyDiscount(price, rate float64) float64 {
	if rate < 0 {
		rate = 0
	}
	if rate > 1 {
		rate = 1
	}
	return price * (1 - rate)
}

// applyTax applies tax. Duplicated logic vs applyDiscount.
func applyTax(price, rate float64) float64 {
	if rate < 0 {
		rate = 0
	}
	if rate > 1 {
		rate = 1
	}
	return price * (1 + rate)
}

// deadCode is never called anywhere.
func deadCode() string {
	return "nobody calls me"
}
`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(sampleDir, name), []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}