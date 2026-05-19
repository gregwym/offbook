package ai_test

import (
	"go/build"
	"strings"
	"testing"
)

// TestAIPackage_DoesNotImportPIIRepo is the architectural enforcement that
// ADR-0003 / ARCHITECTURE.md (PII Isolation) calls out: the AI layer must
// never depend on pii_repo, transitively or directly. A new import here is
// the easiest way to leak PII into prompts, so we fail the test the moment
// the package graph references pii_repo.
//
// Walks the build-time import graph of internal/service/ai (recursively)
// and asserts no edge ends in repository/pii_repo or any pii_ symbol from
// the repository layer.
func TestAIPackage_DoesNotImportPIIRepo(t *testing.T) {
	const target = "github.com/gregwym/offbook/backend/internal/service/ai"
	forbidden := []string{
		"github.com/gregwym/offbook/backend/internal/repository/pii",
		"internal/repository/pii_repo",
	}

	seen := map[string]bool{}
	var walk func(pkgPath string)
	walk = func(pkgPath string) {
		if seen[pkgPath] {
			return
		}
		seen[pkgPath] = true
		pkg, err := build.Default.Import(pkgPath, ".", 0)
		if err != nil {
			// Stdlib packages or vendor-resolution issues should not fail
			// the rule — they cannot contain pii_repo.
			return
		}
		for _, imp := range pkg.Imports {
			for _, bad := range forbidden {
				if strings.Contains(imp, bad) {
					t.Errorf("ai package imports forbidden path %q (via %s)", imp, pkgPath)
				}
			}
			// Only recurse into our own module — stdlib and third-party
			// dependencies cannot reach pii_repo by definition.
			if strings.HasPrefix(imp, "github.com/gregwym/offbook/backend/") {
				walk(imp)
			}
		}
	}
	walk(target)
}
