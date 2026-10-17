package main

import (
	"fmt"
	"go/types"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

// program holds everything the analysis needs: the loaded packages, the SSA
// program, the set of functions that belong to the analysed packages, and an
// index of every callee the spec tables mention.
type program struct {
	pkgs     []*packages.Package
	prog     *ssa.Program
	project  map[string]*ssa.Package    // import path -> SSA package
	funcs    []*ssa.Function            // project functions, sorted by name
	byKey    map[key]*ssa.Function      // project functions by key
	declared map[key]*types.Signature   // every resolvable callee, incl. deps
	guards   map[key]*guardInfo         // classified project guards
	summary  map[*ssa.Function]bool     // does any parameter taint the result?
}

func loadProgram(patterns []string) (*program, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedCompiledGoFiles |
			packages.NeedImports |
			packages.NeedDeps |
			packages.NeedTypes |
			packages.NeedSyntax |
			packages.NeedTypesInfo |
			packages.NeedTypesSizes,
	}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, err
	}
	if n := packages.PrintErrors(pkgs); n > 0 {
		return nil, fmt.Errorf("%d package error(s); refusing to build SSA on ill-typed input", n)
	}

	prog, ssaPkgs := ssautil.AllPackages(pkgs, ssa.InstantiateGenerics)
	prog.Build()

	p := &program{
		pkgs:     pkgs,
		prog:     prog,
		project:  map[string]*ssa.Package{},
		byKey:    map[key]*ssa.Function{},
		declared: buildDeclaredIndex(pkgs),
		guards:   map[key]*guardInfo{},
		summary:  map[*ssa.Function]bool{},
	}

	for _, sp := range ssaPkgs {
		if sp == nil || sp.Pkg == nil {
			continue
		}
		p.project[sp.Pkg.Path()] = sp
	}
	if len(p.project) == 0 {
		return nil, fmt.Errorf("no packages matched %v", patterns)
	}

	// AllFunctions walks the whole program, including methods and closures.
	// Filtering by package path is what separates "code under analysis" from
	// "the standard library".
	for fn := range ssautil.AllFunctions(prog) {
		if fn == nil || fn.Pkg == nil || fn.Pkg.Pkg == nil {
			continue
		}
		if _, ok := p.project[fn.Pkg.Pkg.Path()]; !ok {
			continue
		}
		if fn.Synthetic != "" {
			continue // wrappers, bound methods, thunks
		}
		if strings.Contains(fn.Name(), "$") {
			continue
		}
		p.funcs = append(p.funcs, fn)
	}
	sort.Slice(p.funcs, func(i, j int) bool {
		return p.funcs[i].String() < p.funcs[j].String()
	})
	for _, fn := range p.funcs {
		p.byKey[funcKey(fn)] = fn
	}
	return p, nil
}

// isProject reports whether fn lives in one of the analysed packages.
func (p *program) isProject(fn *ssa.Function) bool {
	if fn == nil || fn.Pkg == nil || fn.Pkg.Pkg == nil {
		return false
	}
	_, ok := p.project[fn.Pkg.Pkg.Path()]
	return ok
}

// funcKey turns an SSA function into a spec-table key.
func funcKey(fn *ssa.Function) key {
	kk := key{name: fn.Name()}
	if fn.Pkg != nil && fn.Pkg.Pkg != nil {
		kk.pkg = fn.Pkg.Pkg.Path()
	}
	if fn.Signature != nil && fn.Signature.Recv() != nil {
		kk.recv = baseTypeName(fn.Signature.Recv().Type())
	}
	return kk
}

// callKey resolves a call site to a spec-table key. It returns false for
// builtins and for calls through a closure value, where there is no static
// callee to name.
func callKey(c *ssa.CallCommon) (key, bool) {
	if c == nil {
		return key{}, false
	}
	if c.IsInvoke() {
		if c.Method == nil {
			return key{}, false
		}
		kk := key{recv: baseTypeName(c.Value.Type()), name: c.Method.Name()}
		if c.Method.Pkg() != nil {
			kk.pkg = c.Method.Pkg().Path()
		}
		return kk, true
	}
	if callee := c.StaticCallee(); callee != nil {
		return funcKey(callee), true
	}
	return key{}, false
}

// baseTypeName strips a pointer and returns the named type's bare name.
func baseTypeName(t types.Type) string {
	if t == nil {
		return ""
	}
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	if named, ok := t.(*types.Named); ok {
		return named.Obj().Name()
	}
	return t.String()
}

// buildDeclaredIndex walks every loaded package — including dependencies — and
// records the declared signature of each function and method it can name.
//
// This is what makes probe.go possible. Without it, the argument-layout table
// could only cover callees that the corpus happens to call, and a rule for an
// uncalled sink would go unverified.
func buildDeclaredIndex(pkgs []*packages.Package) map[key]*types.Signature {
	out := map[key]*types.Signature{}
	seen := map[*types.Package]bool{}

	var walk func(p *packages.Package)
	walk = func(p *packages.Package) {
		if p == nil || p.Types == nil || seen[p.Types] {
			return
		}
		seen[p.Types] = true

		scope := p.Types.Scope()
		for _, name := range scope.Names() {
			switch obj := scope.Lookup(name).(type) {
			case *types.Func:
				if sig, ok := obj.Type().(*types.Signature); ok {
					out[key{p.Types.Path(), "", obj.Name()}] = sig
				}
			case *types.TypeName:
				named, ok := obj.Type().(*types.Named)
				if !ok {
					continue
				}
				recv := named.Obj().Name()
				for i := 0; i < named.NumMethods(); i++ {
					m := named.Method(i)
					if sig, ok := m.Type().(*types.Signature); ok {
						out[key{p.Types.Path(), recv, m.Name()}] = sig
					}
				}
				// Interface methods are not in NumMethods; they live on the
				// underlying type. net/http.ResponseWriter.Write is one.
				if iface, ok := named.Underlying().(*types.Interface); ok {
					for i := 0; i < iface.NumMethods(); i++ {
						m := iface.Method(i)
						if sig, ok := m.Type().(*types.Signature); ok {
							out[key{p.Types.Path(), recv, m.Name()}] = sig
						}
					}
				}
			}
		}
		for _, imp := range p.Imports {
			walk(imp)
		}
	}

	for _, p := range pkgs {
		walk(p)
	}
	return out
}

// countInstructions totals the SSA instructions across the project functions.
func (p *program) countInstructions() int {
	n := 0
	for _, fn := range p.funcs {
		for _, b := range fn.Blocks {
			n += len(b.Instrs)
		}
	}
	return n
}
