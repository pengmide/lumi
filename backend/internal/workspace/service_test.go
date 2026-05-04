package workspace

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestListTreeSkipsHiddenAndBuildsKinds(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "src", "main.ts"), "export const ready = true\n")
	mustWriteFile(t, filepath.Join(root, "docs", "readme.md"), "# hello\n")
	mustWriteFile(t, filepath.Join(root, "public", "index.html"), "<!DOCTYPE html><html></html>\n")
	mustWriteFile(t, filepath.Join(root, "assets", "logo.png"), "png")
	mustWriteFile(t, filepath.Join(root, ".env"), "SECRET=1\n")
	mustWriteFile(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")

	service := NewService()
	tree, err := service.ListTree(root)
	if err != nil {
		t.Fatalf("ListTree() error = %v", err)
	}

	if len(tree) != 4 {
		t.Fatalf("expected 4 top-level nodes, got %d", len(tree))
	}

	srcNode := findTreeNode(tree, "src/main.ts")
	if srcNode == nil {
		t.Fatalf("expected src/main.ts to exist in tree")
	}
	if srcNode.PreviewKind != PreviewKindCode {
		t.Fatalf("expected src/main.ts preview kind %q, got %q", PreviewKindCode, srcNode.PreviewKind)
	}

	mdNode := findTreeNode(tree, "docs/readme.md")
	if mdNode == nil || mdNode.PreviewKind != PreviewKindMarkdown {
		t.Fatalf("expected docs/readme.md markdown preview kind, got %#v", mdNode)
	}

	htmlNode := findTreeNode(tree, "public/index.html")
	if htmlNode == nil || htmlNode.PreviewKind != PreviewKindHTML {
		t.Fatalf("expected public/index.html html preview kind, got %#v", htmlNode)
	}

	if hiddenNode := findTreeNode(tree, ".env"); hiddenNode != nil {
		t.Fatalf("expected hidden file to be skipped")
	}
	if gitNode := findTreeNode(tree, ".git/HEAD"); gitNode != nil {
		t.Fatalf("expected .git contents to be skipped")
	}
}

func TestResolveFileRejectsTraversal(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "src", "main.ts"), "const ready = true\n")

	service := NewService()
	_, err := service.ResolveFile(root, "../outside.txt")
	if err != ErrPathEscape {
		t.Fatalf("ResolveFile() error = %v, want %v", err, ErrPathEscape)
	}
}

func TestReadTextFileTruncatesLargeFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	content := strings.Repeat("a", maxTextPreviewBytes+64)
	mustWriteFile(t, filepath.Join(root, "src", "huge.ts"), content)

	service := NewService()
	textFile, err := service.ReadTextFile(root, "src/huge.ts")
	if err != nil {
		t.Fatalf("ReadTextFile() error = %v", err)
	}
	if !textFile.Truncated {
		t.Fatalf("expected ReadTextFile() to report truncated content")
	}
	if len(textFile.Content) != maxTextPreviewBytes {
		t.Fatalf("expected truncated content length %d, got %d", maxTextPreviewBytes, len(textFile.Content))
	}
}

func TestResolveFileRejectsSymlinkOutsideRoot(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior is permission-sensitive on windows")
	}

	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.ts")
	mustWriteFile(t, outside, "const outside = true\n")
	if err := os.Symlink(outside, filepath.Join(root, "linked.ts")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	service := NewService()
	_, err := service.ResolveFile(root, "linked.ts")
	if err != ErrPathEscape {
		t.Fatalf("ResolveFile() error = %v, want %v", err, ErrPathEscape)
	}
}

func mustWriteFile(t *testing.T, path string, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func findTreeNode(nodes []TreeNode, targetPath string) *TreeNode {
	for i := range nodes {
		node := &nodes[i]
		if node.Path == targetPath {
			return node
		}
		if child := findTreeNode(node.Children, targetPath); child != nil {
			return child
		}
	}

	return nil
}
