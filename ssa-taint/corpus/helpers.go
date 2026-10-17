package corpus

import (
	"path/filepath"
	"strings"
)

// Pass-through helpers. These exist so the interprocedural summary has
// something to prove: a flow that crosses a project function boundary is
// invisible to a purely intraprocedural walk.

// wrap1 — one hop. EXPECT: summary says it taints its result.
func wrap1(s string) string { return "prefix:" + s }

// wrap2 — two hops. wrap2 calls wrap1, so wrap2's summary can only be decided
// after wrap1's. This is why the summary loop is a fixpoint.
func wrap2(s string) string { return wrap1(s) + ":suffix" }

// identity — a passthrough. EXPECT: summary says it taints its result.
func identity(s string) string { return s }

// joinPath — taint through strings.Join. EXPECT: summary says it taints.
func joinPath(parts []string) string { return strings.Join(parts, "/") }

// baseName — wraps filepath.Base. EXPECT: summary says it taints, which is why
// CleanGuard is reported.
func baseName(s string) string { return filepath.Base(s) }
