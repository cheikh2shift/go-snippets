package main

import (
	"fmt"
	"go/types"
	"sort"
)

// layoutRow is one sink rule measured against the callee's declared signature.
//
// The point of this file is that every argument index in specs.go is a guess
// about someone else's API. Rather than trust the guess, the probe looks up the
// real signature and reports what actually sits at that position.
type layoutRow struct {
	callee     key
	ruleArg    int
	kind       string
	params     int
	variadic   bool
	resolved   bool
	declType   string
	note       string
	suspicious bool
}

// probeLayout checks every sink rule against the declared signature of the
// callee it names.
//
// The receiver asymmetry is the reason this is not a one-liner. In SSA, a
// static method call puts the receiver in CallCommon.Args[0], but the declared
// *types.Signature does not include the receiver in Params(). So a rule index
// of 2 on a method means Params()[1], and the probe has to shift by one before
// comparing. Interface (invoke-mode) calls are the other way round: Args
// excludes the receiver, so no shift applies. The probe cannot tell which
// convention a given call site used, so it reports the static-call reading and
// says so.
func probeLayout(p *program) []layoutRow {
	var rows []layoutRow

	for _, sp := range sinkSpecs {
		row := layoutRow{callee: sp.key, ruleArg: sp.arg, kind: sp.kind}

		sig, ok := p.declared[sp.key]
		if !ok {
			row.note = "callee not found in loaded dependencies"
			row.suspicious = true
			rows = append(rows, row)
			continue
		}
		row.resolved = true
		row.params = sig.Params().Len()
		row.variadic = sig.Variadic()

		if sp.arg == -1 {
			if row.params == 0 {
				row.note = "rule says any argument, callee declares none"
				row.suspicious = true
			} else {
				row.note = "any argument (variadic rule)"
			}
			rows = append(rows, row)
			continue
		}

		paramIdx := sp.arg
		if sp.key.recv != "" {
			paramIdx--
		}

		if paramIdx < 0 {
			row.note = "rule index points at the receiver, not a parameter"
			row.suspicious = true
			rows = append(rows, row)
			continue
		}
		if paramIdx >= row.params {
			row.note = fmt.Sprintf("fewer than declared: index %d, %d parameter(s)",
				paramIdx, row.params)
			row.suspicious = true
			rows = append(rows, row)
			continue
		}

		row.declType = types.TypeString(sig.Params().At(paramIdx).Type(), nil)
		if row.variadic && paramIdx == row.params-1 {
			row.note = "variadic tail; index is the slice, not one element"
		}
		rows = append(rows, row)
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].callee.pkg != rows[j].callee.pkg {
			return rows[i].callee.pkg < rows[j].callee.pkg
		}
		return rows[i].callee.String() < rows[j].callee.String()
	})
	return rows
}
