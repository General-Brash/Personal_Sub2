//go:build unit

package service

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReleaseConfigsUsePersonalGHCRImage(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to locate release config contract test")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", "..", ".."))

	files := []string{
		".goreleaser.yaml",
		".goreleaser.simple.yaml",
		filepath.Join(".github", "workflows", "release.yml"),
		filepath.Join(".github", "workflows", "publish-personal-ghcr.yml"),
	}
	const oldGHCRImage = "ghcr.io/{{ .Env.GITHUB_REPO_OWNER_LOWER }}/sub2api"
	const oldPackagePath = "/pkgs/container/sub2api"

	for _, relativePath := range files {
		relativePath := relativePath
		t.Run(relativePath, func(t *testing.T) {
			content, err := os.ReadFile(filepath.Join(root, relativePath))
			if err != nil {
				t.Fatalf("read %s: %v", relativePath, err)
			}
			text := string(content)
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
