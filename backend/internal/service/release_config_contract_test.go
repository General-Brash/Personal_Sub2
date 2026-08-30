//go:build unit

package service

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to locate release config contract test")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", "..", ".."))
}

func readRepositoryFile(t *testing.T, root, relativePath string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, relativePath))
	if err != nil {
		t.Fatalf("read %s: %v", relativePath, err)
	}
	return string(content)
}

func requireContains(t *testing.T, text, expected string) {
	t.Helper()
	if !strings.Contains(text, expected) {
		t.Fatalf("missing expected text %q", expected)
	}
}

func requireNotContains(t *testing.T, text, unexpected string) {
	t.Helper()
	if strings.Contains(text, unexpected) {
		t.Fatalf("unexpected text %q is present", unexpected)
	}
}

func TestReleaseConfigsUsePersonalGHCRImage(t *testing.T) {
	root := repositoryRoot(t)
	files := []string{
		".goreleaser.yaml",
		".goreleaser.simple.yaml",
		filepath.Join(".github", "workflows", "release.yml"),
		filepath.Join(".github", "workflows", "publish-personal-ghcr.yml"),
		filepath.Join(".github", "workflows", "publish-intent-classifier-ghcr.yml"),
	}
	const oldGHCRImage = "ghcr.io/{{ .Env.GITHUB_REPO_OWNER_LOWER }}/sub2api"
	const oldPackagePath = "/pkgs/container/sub2api"

	for _, relativePath := range files {
		relativePath := relativePath
		t.Run(relativePath, func(t *testing.T) {
			text := readRepositoryFile(t, root, relativePath)
			if !strings.Contains(text, "personal_sub2") {
				t.Fatalf("%s does not reference the personal_sub2 GHCR image", relativePath)
			}
			if strings.Contains(text, oldGHCRImage) {
				t.Fatalf("%s still references the old GHCR image path %q", relativePath, oldGHCRImage)
			}
			if strings.Contains(text, oldPackagePath) {
				t.Fatalf("%s still references the old GitHub Packages path %q", relativePath, oldPackagePath)
			}
		})
	}
}

func TestReleasePublicationContracts(t *testing.T) {
	root := repositoryRoot(t)
	release := readRepositoryFile(t, root, filepath.Join(".github", "workflows", "release.yml"))
	personal := readRepositoryFile(t, root, filepath.Join(".github", "workflows", "publish-personal-ghcr.yml"))
	classifier := readRepositoryFile(t, root, filepath.Join(".github", "workflows", "publish-intent-classifier-ghcr.yml"))
	full := readRepositoryFile(t, root, ".goreleaser.yaml")
	simple := readRepositoryFile(t, root, ".goreleaser.simple.yaml")

	requireContains(t, release, "concurrency:\n  group: personal-sub2-release\n  cancel-in-progress: false")
	requireContains(t, release, "EVENT_SHA: ${{ github.sha }}")
	requireContains(t, release, `if [[ "$target_commit" != "$EVENT_SHA" ]]`)
	requireContains(t, release, "printf 'publish_moving_tags=%s\\n' \"$publish_moving_tags\"")
	requireContains(t, release, "PUBLISH_MOVING_TAGS: ${{ needs.resolve_release.outputs.publish_moving_tags }}")

	requireContains(t, personal, "type=raw,value=latest")
	requireContains(t, personal, "type=sha,prefix=sha-,format=long")
	requireNotContains(t, personal, "type=raw,value=${{ steps.version.outputs.value }}")

	requireContains(t, classifier, "concurrency:\n  group: publish-intent-classifier\n  cancel-in-progress: false")
	requireContains(t, classifier, "EVENT_SHA: ${{ github.sha }}")
	requireContains(t, classifier, `if [[ "$target_commit" != "$EVENT_SHA" ]]`)
	requireContains(t, classifier, "printf 'publish_moving_tags=%s\\n' \"$publish_moving_tags\"")
	requireContains(t, classifier, "type=raw,value=latest,enable=${{ needs.resolve_release.outputs.publish_moving_tags == 'true' }}")

	const ghcrMovingSkip = `    skip_push: '{{ if ne .Env.PUBLISH_MOVING_TAGS "true" }}true{{ else }}false{{ end }}'`
	const dockerHubMovingSkip = `    skip_push: '{{ if eq .Env.DOCKERHUB_USERNAME "skip" }}true{{ else if ne .Env.PUBLISH_MOVING_TAGS "true" }}true{{ else }}false{{ end }}'`
	if count := strings.Count(full, ghcrMovingSkip); count != 3 {
		t.Fatalf("full GoReleaser config gates %d GHCR moving manifests, want 3", count)
	}
	if count := strings.Count(full, dockerHubMovingSkip); count != 3 {
		t.Fatalf("full GoReleaser config gates %d DockerHub moving manifests, want 3", count)
	}
	requireContains(t, full, "sha-{{ .Commit }}")
	requireNotContains(t, simple, "ghcr.io/general-brash/personal_sub2:latest")
	requireContains(t, simple, "sha-{{ .Commit }}")
}
