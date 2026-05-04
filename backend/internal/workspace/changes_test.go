package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestChangesServiceSnapshotMode(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for workspace change tests")
	}

	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "src", "main.ts"), "export const version = 1\n")
	mustWriteFile(t, filepath.Join(root, "docs", "guide.md"), "# guide\n")

	service := NewChangesService()
	t.Cleanup(func() {
		if err := service.DisposeAll(); err != nil {
			t.Fatalf("DisposeAll() error = %v", err)
		}
	})

	initialChanges, err := service.ListChanges(root)
	if err != nil {
		t.Fatalf("ListChanges() error = %v", err)
	}
	if len(initialChanges) != 0 {
		t.Fatalf("expected no initial changes, got %#v", initialChanges)
	}

	mustWriteFile(t, filepath.Join(root, "src", "main.ts"), "export const version = 2\n")
	if err := os.Remove(filepath.Join(root, "docs", "guide.md")); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	mustWriteFile(t, filepath.Join(root, "src", "feature.ts"), "export const feature = true\n")

	changes, err := service.ListChanges(root)
	if err != nil {
		t.Fatalf("ListChanges() error = %v", err)
	}

	if len(changes) != 3 {
		t.Fatalf("expected 3 changes, got %#v", changes)
	}

	assertChange(t, changes, "src/main.ts", ChangeStatusModified, 1, 1)
	assertChange(t, changes, "docs/guide.md", ChangeStatusDeleted, 0, 1)
	assertChange(t, changes, "src/feature.ts", ChangeStatusAdded, 1, 0)

	diff, err := service.UnifiedDiff(root, "src/main.ts")
	if err != nil {
		t.Fatalf("UnifiedDiff() error = %v", err)
	}
	if !containsAll(diff,
		"diff --git a/src/main.ts b/src/main.ts",
		"-export const version = 1",
		"+export const version = 2",
	) {
		t.Fatalf("unexpected diff output:\n%s", diff)
	}
}

func TestChangesServiceGitRepoMode(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for workspace change tests")
	}

	root := t.TempDir()
	runGitCommand(t, root, "init")
	mustWriteFile(t, filepath.Join(root, "README.md"), "hello\n")
	runGitCommand(t, root, "add", "README.md")
	runGitCommand(t, root, "-c", "user.name=lumi", "-c", "user.email=test@lumi.local", "commit", "-m", "initial")

	service := NewChangesService()
	t.Cleanup(func() {
		if err := service.DisposeAll(); err != nil {
			t.Fatalf("DisposeAll() error = %v", err)
		}
	})

	initialChanges, err := service.ListChanges(root)
	if err != nil {
		t.Fatalf("ListChanges() error = %v", err)
	}
	if len(initialChanges) != 0 {
		t.Fatalf("expected clean git repo, got %#v", initialChanges)
	}

	mustWriteFile(t, filepath.Join(root, "README.md"), "hello world\n")
	mustWriteFile(t, filepath.Join(root, "notes.txt"), "new file\n")

	changes, err := service.ListChanges(root)
	if err != nil {
		t.Fatalf("ListChanges() error = %v", err)
	}

	if len(changes) != 2 {
		t.Fatalf("expected 2 changes, got %#v", changes)
	}

	assertChange(t, changes, "README.md", ChangeStatusModified, 1, 1)
	assertChange(t, changes, "notes.txt", ChangeStatusAdded, 1, 0)

	diff, err := service.UnifiedDiff(root, "notes.txt")
	if err != nil {
		t.Fatalf("UnifiedDiff() error = %v", err)
	}
	if !containsAll(diff,
		"diff --git a/notes.txt b/notes.txt",
		"new file mode",
		"+new file",
	) {
		t.Fatalf("unexpected diff output:\n%s", diff)
	}
}

func TestChangesServiceGitRepoSubdirectoryWorkspace(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for workspace change tests")
	}

	repoRoot := t.TempDir()
	workspaceRoot := filepath.Join(repoRoot, "workspace")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	runGitCommand(t, repoRoot, "init")
	mustWriteFile(t, filepath.Join(workspaceRoot, "index.html"), "<h1>v1</h1>\n")
	runGitCommand(t, repoRoot, "add", "workspace/index.html")
	runGitCommand(t, repoRoot, "-c", "user.name=lumi", "-c", "user.email=test@lumi.local", "commit", "-m", "initial")

	service := NewChangesService()
	t.Cleanup(func() {
		if err := service.DisposeAll(); err != nil {
			t.Fatalf("DisposeAll() error = %v", err)
		}
	})

	mustWriteFile(t, filepath.Join(workspaceRoot, "index.html"), "<h1>v2</h1>\n")
	mustWriteFile(t, filepath.Join(workspaceRoot, "a.yaml"), "name: demo\n")

	changes, err := service.ListChanges(workspaceRoot)
	if err != nil {
		t.Fatalf("ListChanges() error = %v", err)
	}

	if len(changes) != 2 {
		t.Fatalf("expected 2 changes, got %#v", changes)
	}

	assertChange(t, changes, "index.html", ChangeStatusModified, 1, 1)
	assertChange(t, changes, "a.yaml", ChangeStatusAdded, 1, 0)

	diff, err := service.UnifiedDiff(workspaceRoot, "a.yaml")
	if err != nil {
		t.Fatalf("UnifiedDiff() error = %v", err)
	}
	if !containsAll(diff,
		"diff --git a/a.yaml b/a.yaml",
		"new file mode",
		"+name: demo",
	) {
		t.Fatalf("unexpected diff output:\n%s", diff)
	}
}

func assertChange(t *testing.T, changes []Change, path string, status ChangeStatus, insertions int, deletions int) {
	t.Helper()

	for _, change := range changes {
		if change.Path == path {
			if change.Status != status {
				t.Fatalf("change %q status = %q, want %q", path, change.Status, status)
			}
			if change.Insertions != insertions {
				t.Fatalf("change %q insertions = %d, want %d", path, change.Insertions, insertions)
			}
			if change.Deletions != deletions {
				t.Fatalf("change %q deletions = %d, want %d", path, change.Deletions, deletions)
			}
			return
		}
	}

	t.Fatalf("expected change for %q in %#v", path, changes)
}

func runGitCommand(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(output))
	}
}

func containsAll(content string, expected ...string) bool {
	for _, value := range expected {
		if !strings.Contains(content, value) {
			return false
		}
	}

	return true
}
