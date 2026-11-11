# The buf CLI runs from this pinned version through `go run`, so it does
# not have to be installed (nor added to go.mod).
BUF := go run github.com/bufbuild/buf/cmd/buf@v1.75.0

.PHONY: generate lint

# generate rewrites gen from proto/. The result is committed, so
# that building the server does not need buf.
generate:
	$(BUF) generate

lint:
	$(BUF) lint
