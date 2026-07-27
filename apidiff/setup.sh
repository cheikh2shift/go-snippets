#!/usr/bin/env bash
set -euo pipefail

WORKDIR="$(pwd)/examplelib-demo"
rm -rf "$WORKDIR"
mkdir -p "$WORKDIR"
cd "$WORKDIR"

git init -q
git config user.email "demo@example.com"
git config user.name "apidiff demo"
git config tag.gpgsign false

cat > go.mod <<'EOF'
module example.com/examplelib

go 1.26
EOF

# ---- v1.0.0: clean public API ----
cat > examplelib.go <<'EOF'
package examplelib

// User is a demo entity.
type User struct {
	ID   int
	Name string
}

// GetUser returns the user for the given ID.
func GetUser(id int) (*User, error) {
	return &User{ID: id, Name: "user"}, nil
}

// FormatName returns a display name.
func FormatName(u *User) string {
	return u.Name
}
EOF

git add -A
git commit -qm "v1.0.0"
git tag v1.0.0

# ---- v1.1.0: intentionally BREAKING public API ----
cat > examplelib.go <<'EOF'
package examplelib

// User is a demo entity.
type User struct {
	ID   int
	Name string
}

// GetUser now takes a string key instead of an int ID (BREAKING).
func GetUser(key string) (*User, error) {
	return &User{ID: 0, Name: key}, nil
}

// FormatName was removed (BREAKING).
EOF

git add -A
git commit -qm "v1.1.0 (breaking)"
git tag v1.1.0

# ---- CI / local helper files ----
cat > Makefile <<'MAKEFILE'
.PHONY: apidiff

apidiff:
	@go install golang.org/x/exp/cmd/apidiff@latest
	@wt=$$(mktemp -d); \
	git worktree add -q "$$wt" v1.0.0; \
	(cd "$$wt" && apidiff -w "$$wt/old.export" .); \
	apidiff -w new.export .; \
	apidiff "$$wt/old.export" new.export; \
	status=$$?; \
	rm -f old.export new.export; \
	git worktree remove --force "$$wt"; \
	exit $$status
MAKEFILE

mkdir -p .github/workflows
cat > .github/workflows/apidiff.yml <<'EOF'
name: apidiff
on:
  pull_request:
  push:
    tags: ['v*']
jobs:
  apidiff:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with: { fetch-depth: 0 }
      - uses: actions/setup-go@v5
        with: { go-version: '1.26' }
      - run: go install golang.org/x/exp/cmd/apidiff@latest
      - run: |
          wt=$(mktemp -d)
          git worktree add -q "$wt" v1.0.0
          (cd "$wt" && apidiff -w "$wt/old.export" .)
          apidiff -w new.export .
          apidiff "$wt/old.export" new.export
EOF

echo "Generated demo at $WORKDIR"
echo "Run: cd examplelib-demo && make apidiff"
