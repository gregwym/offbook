# Colima sizing for local development

The backend lints via a `golangci-lint` container (`make lint`). Since M3 the build graph includes the `github.com/plaid/plaid-go/v40/plaid` package — a large, OpenAPI-generated SDK whose typecheck has a hefty peak resident-set size. On Colima's default 2 GiB profile, the kernel OOM-killer terminates the lint compile step:

```
typecheck: could not import github.com/plaid/plaid-go/v40/plaid
  (signal: killed during compile)
```

`go build`, `go vet`, and `go test ./...` all stay well under the limit — only `golangci-lint run` blows past it.

## Fix

Restart Colima with at least 4 GiB:

```sh
colima stop
colima start --memory 4
```

Persist it so future starts pick the bigger profile:

```sh
colima start --memory 4 --save-config
```

Docker Desktop / Podman Desktop users: bump the VM's memory in the GUI to ≥4 GiB. CI is unaffected — GitHub-hosted runners ship with ~7 GiB.

## Verifying

```sh
cd backend && command make lint
```

Should complete without `signal: killed`.
