## What does this PR do?

<!-- A short description of the change and why it's needed. -->

## Type of change

- [ ] Bug fix
- [ ] New command or flag
- [ ] Skeleton template change (affects `gofasta new` output)
- [ ] Generation template change (affects `gofasta g` output)
- [ ] Refactor / cleanup
- [ ] Documentation
- [ ] Other (describe below)

## How to test

<!-- Steps to verify the change works. If you changed skeleton or generation templates, include: -->

```bash
# Build the CLI
go build -o /tmp/gofasta ./cmd/gofasta/

# Create a test project
/tmp/gofasta new /tmp/testproject

# Verify it compiles
cd /tmp/testproject && go build ./...
```

## Checklist

- [ ] `go test ./...` passes
- [ ] `go vet ./...` passes
- [ ] If skeleton templates changed: generated project compiles with `go build ./...`
- [ ] If generation templates changed: tested with `gofasta g scaffold`
- [ ] Related issue linked (e.g. `Fixes #123`)
