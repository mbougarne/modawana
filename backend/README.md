# Backend

[العربية](README.ar.md) · [Project](../README.md)

The Go module contains the application server (`cmd/blog`) and scoped publisher
(`cmd/publisher`). Internal packages implement authentication, content, media,
configuration, learning, storage, portability, backup and recovery. No owner,
credentials or content are seeded.

From this directory:

```sh
go test ./...
go test -race ./...
go vet ./...
go mod verify
```

From the repository root use `node scripts/project.mjs build` to build both binaries
and the frontend. Go serves the production assets without Node. See
[architecture](../docs/architecture.md), [API/configuration](../docs/api/README.md),
[migrations](migrations/README.md), and [deployment](../docs/deployment.md).
