package main

// ast.go is the only file that touches concrete ebnf node types.
//
// It dispatches on the bare type name (pointer stripped) and reads
// struct fields by name with fallbacks, so it compiles whether the
// package stores nodes as pointers or values, and whether a field is
// called Expr or Expression.
//
// Verified on this machine:
//   Grammar    = map[string]*ebnf.Production
//   Production = { Name *ebnf.Name; Expr ebnf.Expression }
//   AST nodes  = Alternative, Name, Option, Range, Repetition, Sequence, Token

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"golang.org/x/exp/ebnf"
)

func kindOf(e ebnf.Expression) string {
	if e == nil {
		return "<nil>"
	}
	t := reflect.TypeOf(e)
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t.Name()
}

func deref(v reflect.Value) reflect.Value {
	for v.IsValid() && v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return reflect.Value{}
		}
		v = v.Elem()
	}
	return v
}

// fieldOf returns the first exported struct field whose name matches one
// of names, case-insensitively.
func fieldOf(x any, names ...string) reflect.Value {
	v := deref(reflect.ValueOf(x))
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return reflect.Value{}
	}
	t := v.Type()
	for _, want := range names {
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.PkgPath == "" && strings.EqualFold(f.Name, want) {
				return v.Field(i)
			}
		}
	}
	return reflect.Value{}
}

func stringOf(e ebnf.Expression) string {
	for _, n := range []string{"String", "Text", "Value", "Lit", "Literal"} {
		f := fieldOf(e, n)
		if f.IsValid() && f.Kind() == reflect.String {
			return f.String()
		}
	}
	return ""
}

func bodyOf(e ebnf.Expression) ebnf.Expression {
	for _, n := range []string{"Body", "Expr", "Expression", "X", "Sub"} {
		f := fieldOf(e, n)
		if !f.IsValid() {
			continue
		}
		if x, ok := f.Interface().(ebnf.Expression); ok {
			return x
		}
	}
	// last resort: any field that is an ebnf.Expression
	v := deref(reflect.ValueOf(e))
	if v.IsValid() && v.Kind() == reflect.Struct {
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			if t.Field(i).PkgPath != "" {
				continue
			}
			if x, ok := v.Field(i).Interface().(ebnf.Expression); ok {
				return x
			}
		}
	}
	return nil
}

func elementsOf(e ebnf.Expression) []ebnf.Expression {
	v := deref(reflect.ValueOf(e))
	if !v.IsValid() || v.Kind() != reflect.Slice {
		return nil
	}
	out := make([]ebnf.Expression, 0, v.Len())
	for i := 0; i < v.Len(); i++ {
		if x, ok := v.Index(i).Interface().(ebnf.Expression); ok {
			out = append(out, x)
		}
	}
	return out
}

func tokenString(v reflect.Value) string {
	v = deref(v)
	if !v.IsValid() {
		return ""
	}
	if v.Kind() == reflect.String {
		return v.String()
	}
	if v.Kind() == reflect.Struct {
		t := v.Type()
		for _, want := range []string{"String", "Text", "Value"} {
			for i := 0; i < t.NumField(); i++ {
				f := t.Field(i)
				if f.PkgPath == "" && strings.EqualFold(f.Name, want) && v.Field(i).Kind() == reflect.String {
					return v.Field(i).String()
				}
			}
		}
	}
	return ""
}

func rangeOf(e ebnf.Expression) (string, string, bool) {
	b := fieldOf(e, "Begin", "Lo", "Start", "From", "Min")
	n := fieldOf(e, "End", "Hi", "Stop", "To", "Max")
	if !b.IsValid() || !n.IsValid() {
		return "", "", false
	}
	bs, ns := tokenString(b), tokenString(n)
	if bs == "" || ns == "" {
		return "", "", false
	}
	return bs, ns, true
}

func productionExpr(p *ebnf.Production) ebnf.Expression {
	v := deref(reflect.ValueOf(p))
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return nil
	}
	t := v.Type()
	for _, want := range []string{"Expr", "Expression", "Body"} {
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.PkgPath != "" || !strings.EqualFold(f.Name, want) {
				continue
			}
			if x, ok := v.Field(i).Interface().(ebnf.Expression); ok {
				return x
			}
		}
	}
	return nil
}

// walk counts node kinds. It does not follow Name references, so the
// census describes the grammar text, not the expanded language.
func walk(e ebnf.Expression, counts map[string]int) {
	if e == nil {
		return
	}
	k := kindOf(e)
	counts[k]++
	switch k {
	case "Alternative", "Sequence":
		for _, x := range elementsOf(e) {
			walk(x, counts)
		}
	case "Group", "Option", "Repetition":
		walk(bodyOf(e), counts)
	}
}

// dumpAPI prints the real API of the installed package, so the article
// states what this Go version uses instead of what was assumed.
func dumpAPI(g ebnf.Grammar) {
	fmt.Printf("  Grammar    = %T\n", g)

	var p *ebnf.Production
	t := reflect.TypeOf(p).Elem()
	var fields []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		fields = append(fields, fmt.Sprintf("%s %s", f.Name, f.Type))
	}
	fmt.Printf("  Production = { %s }\n", strings.Join(fields, "; "))

	counts := map[string]int{}
	for _, prod := range g {
		walk(productionExpr(prod), counts)
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s x%d", k, counts[k]))
	}
	fmt.Printf("  AST nodes  = %s\n", strings.Join(parts, ", "))
}
