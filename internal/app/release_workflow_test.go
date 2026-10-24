// SPDX-License-Identifier: Apache-2.0
package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReleaseWorkflowGuards enforces the release-process controls that the
// owner required for G07: a non-publishing dry-run, a fail-closed G08
// authorization check bound to repository/tag/commit, validated workflow
// inputs that cannot inject outputs, a tag-mode-only attestation, consumer
// re-verification, per-tag release notes, and no silent replacement of an
// existing draft release.
//
// Changing these guards requires a deliberate edit here as well as in the
// workflow, so a regression cannot pass review unnoticed.
func TestReleaseWorkflowGuards(t *testing.T) {
	path := filepath.Join(repoRoot(t), ".github", "workflows", "release.yml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}
	workflow := string(raw)

	required := []string{
		"workflow_dispatch:",
		"default: dry-run",
		"- dry-run",
		"- tag",
		".github/scripts/resolve_release_identity.py",
		".github/scripts/verify_g08_authorization.py",
		"if: github.event_name == 'push' || (github.event_name == 'workflow_dispatch' && inputs.mode == 'tag')",
		"if [ \"$sha\" != \"$main_sha\" ]; then",
		"git fetch --prune --prune-tags --tags origin",
		"git ls-remote origin",
		"GOTOOLCHAIN: local",
		`go-version: "1.27.3"`,
		"go env GOVERSION",
		"node-version-file: .nvmrc",
		".github/scripts/package_release.py",
		".github/scripts/write_build_manifest.py",
		".github/scripts/write_attestation_record.py",
		".github/scripts/verify_release_artifacts.py",
		".github/scripts/render_release_notes.py",
		"if: github.event.repository.private == false && needs.preflight.outputs.mode == 'tag'",
		"actions/attest-build-provenance@",
		"--notes-file release/RELEASE-NOTES.md",
		"--draft",
		"sha256sum -c SHA256SUMS",
		"ATTESTATION.json",
		"persist-credentials: false",
		"Re-run is refused",
	}
	for _, needle := range required {
		if !strings.Contains(workflow, needle) {
			t.Errorf("release workflow is missing required control %q", needle)
		}
	}

	forbidden := []string{
		"gh release delete",
		"--draft=false",
		"gh release edit",
		"--notes-file docs/",
		"echo \"version=$version\"",
		"echo \"tag=$tag\"",
	}
	for _, needle := range forbidden {
		if strings.Contains(workflow, needle) {
			t.Errorf("release workflow must not contain %q", needle)
		}
	}

	if !strings.Contains(workflow, "needs: preflight") {
		t.Error("build job must depend on the preflight job")
	}
	if !strings.Contains(workflow, "needs: [preflight, build, smoke]") {
		t.Error("draft job must depend on preflight, build, and smoke")
	}
	if strings.Count(workflow, "sha256sum -c SHA256SUMS") < 2 {
		t.Error("both smoke and draft must verify SHA256SUMS before use")
	}
}
