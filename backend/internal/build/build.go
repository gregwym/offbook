// Package build carries build-time metadata stamped into the binary via
// linker flags. It has no dependencies so any layer can read it without
// creating an import cycle.
package build

// Version is the short git SHA of the build, injected at compile time with
//
//	-ldflags "-X github.com/gregwym/offbook/backend/internal/build.Version=<sha>"
//
// (see backend/Dockerfile + the GIT_SHA build-arg in docker-compose.yml).
// It defaults to "dev" for local `go run` / `make dev` builds that don't
// pass the flag. Surfaced through GET /api/v1/health so deploy freshness is
// a one-request check (#310).
var Version = "dev"
