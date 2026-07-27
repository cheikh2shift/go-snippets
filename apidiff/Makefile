.PHONY: apidiff

apidiff:
	@echo "Running apidiff (old=v1.0.0 -> new=working tree)..."
	@wt=$$(mktemp -d); \
	git worktree add -q "$$wt" v1.0.0; \
	(cd "$$wt" && apidiff -w "$$wt/old.export" .); \
	apidiff -w new.export .; \
	apidiff "$$wt/old.export" new.export; \
	status=$$?; \
	rm -f old.export new.export; \
	git worktree remove --force "$$wt"; \
	exit $$status
