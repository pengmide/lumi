package workspace

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type ChangeStatus string

const (
	ChangeStatusAdded    ChangeStatus = "added"
	ChangeStatusModified ChangeStatus = "modified"
	ChangeStatusDeleted  ChangeStatus = "deleted"
)

type Change struct {
	Path       string       `json:"path"`
	Status     ChangeStatus `json:"status"`
	Insertions int          `json:"insertions,omitempty"`
	Deletions  int          `json:"deletions,omitempty"`
}

type changeMode string

const (
	changeModeGitRepo  changeMode = "git-repo"
	changeModeSnapshot changeMode = "snapshot"
)

type changeState struct {
	mode          changeMode
	workspacePath string
	gitDir        string
	gitPrefix     string
	baselineRef   string
	hasHead       bool
}

type compareContent struct {
	exists bool
	data   []byte
}

type ChangesService struct {
	files  *Service
	mu     sync.Mutex
	states map[string]*changeState
}

func NewChangesService() *ChangesService {
	return &ChangesService{
		files:  NewService(),
		states: make(map[string]*changeState),
	}
}

func (s *ChangesService) ListChanges(workspacePath string) ([]Change, error) {
	state, err := s.ensureState(workspacePath)
	if err != nil {
		return nil, err
	}

	var changes []Change
	switch state.mode {
	case changeModeGitRepo:
		changes, err = s.listGitRepoChanges(state)
	case changeModeSnapshot:
		changes, err = s.listSnapshotChanges(state)
	default:
		err = fmt.Errorf("unsupported change mode %q", state.mode)
	}
	if err != nil {
		return nil, err
	}

	for i := range changes {
		before, after, err := s.loadCompareContent(state, changes[i].Path)
		if err != nil {
			return nil, err
		}

		insertions, deletions, err := buildNumstat(changes[i].Path, before, after)
		if err != nil {
			return nil, err
		}

		changes[i].Insertions = insertions
		changes[i].Deletions = deletions
	}

	sort.Slice(changes, func(i, j int) bool {
		return changes[i].Path < changes[j].Path
	})

	return changes, nil
}

func (s *ChangesService) UnifiedDiff(workspacePath string, relativePath string) (string, error) {
	state, err := s.ensureState(workspacePath)
	if err != nil {
		return "", err
	}

	normalized, err := normalizeRelativePath(relativePath)
	if err != nil {
		return "", err
	}
	normalized = filepath.ToSlash(normalized)
	if !shouldTrackRelativePath(normalized) {
		return "", ErrNotFound
	}

	before, after, err := s.loadCompareContent(state, normalized)
	if err != nil {
		return "", err
	}
	if !before.exists && !after.exists {
		return "", ErrNotFound
	}

	return buildUnifiedDiff(normalized, before, after)
}

func (s *ChangesService) DisposeAll() error {
	s.mu.Lock()
	states := make([]*changeState, 0, len(s.states))
	for _, state := range s.states {
		states = append(states, state)
	}
	s.states = make(map[string]*changeState)
	s.mu.Unlock()

	var firstErr error
	for _, state := range states {
		if err := cleanupChangeState(state); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

func (s *ChangesService) ensureState(workspacePath string) (*changeState, error) {
	rootAbs, err := filepath.Abs(workspacePath)
	if err != nil {
		return nil, ErrWorkspaceUnavailable
	}

	info, err := os.Stat(rootAbs)
	if err != nil || !info.IsDir() {
		return nil, ErrWorkspaceUnavailable
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if state, ok := s.states[rootAbs]; ok {
		return state, nil
	}

	state, err := s.initState(rootAbs)
	if err != nil {
		return nil, err
	}

	s.states[rootAbs] = state
	return state, nil
}

func (s *ChangesService) initState(workspacePath string) (*changeState, error) {
	if isGitRepo(workspacePath) {
		hasHead := gitHasRef(workspacePath, "HEAD")
		gitPrefix, err := execGitOutput(workspacePath, "rev-parse", "--show-prefix")
		if err != nil {
			return nil, err
		}
		return &changeState{
			mode:          changeModeGitRepo,
			workspacePath: workspacePath,
			gitPrefix:     strings.TrimSpace(gitPrefix),
			baselineRef:   "HEAD",
			hasHead:       hasHead,
		}, nil
	}

	gitDir, err := os.MkdirTemp("", "lumi-workspace-snapshot-")
	if err != nil {
		return nil, err
	}

	if err := execGit(workspacePath, "init", "--bare", gitDir); err != nil {
		_ = os.RemoveAll(gitDir)
		return nil, err
	}

	if err := writeSnapshotExcludes(gitDir); err != nil {
		_ = os.RemoveAll(gitDir)
		return nil, err
	}

	gitArgs := []string{
		fmt.Sprintf("--git-dir=%s", gitDir),
		fmt.Sprintf("--work-tree=%s", workspacePath),
		"add",
		"--all",
		".",
	}
	if err := execGit(workspacePath, gitArgs...); err != nil {
		_ = os.RemoveAll(gitDir)
		return nil, err
	}

	commitArgs := []string{
		fmt.Sprintf("--git-dir=%s", gitDir),
		fmt.Sprintf("--work-tree=%s", workspacePath),
		"-c",
		"user.name=lumi",
		"-c",
		"user.email=snapshot@lumi.local",
		"commit",
		"--allow-empty",
		"-m",
		"baseline",
	}
	if err := execGit(workspacePath, commitArgs...); err != nil {
		_ = os.RemoveAll(gitDir)
		return nil, err
	}

	baselineRef, err := execGitOutput(workspacePath, fmt.Sprintf("--git-dir=%s", gitDir), "rev-parse", "HEAD")
	if err != nil {
		_ = os.RemoveAll(gitDir)
		return nil, err
	}

	return &changeState{
		mode:          changeModeSnapshot,
		workspacePath: workspacePath,
		gitDir:        gitDir,
		baselineRef:   strings.TrimSpace(baselineRef),
		hasHead:       true,
	}, nil
}

func (s *ChangesService) listGitRepoChanges(state *changeState) ([]Change, error) {
	output, err := execGitOutput(state.workspacePath, "status", "--porcelain")
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	changes := make([]Change, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		change, ok := parseGitStatusLine(line)
		if !ok {
			continue
		}
		change.Path, ok = repoPathToWorkspacePath(state, change.Path)
		if !ok {
			continue
		}

		changes = append(changes, change)
	}

	return changes, nil
}

func (s *ChangesService) listSnapshotChanges(state *changeState) ([]Change, error) {
	gitArgs := []string{
		fmt.Sprintf("--git-dir=%s", state.gitDir),
		fmt.Sprintf("--work-tree=%s", state.workspacePath),
		"diff",
		"--name-status",
		state.baselineRef,
	}
	output, err := execGitOutput(state.workspacePath, gitArgs...)
	if err != nil {
		return nil, err
	}

	changes := make([]Change, 0)
	for _, line := range strings.Split(strings.TrimRight(output, "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}

		change, ok := parseNameStatusLine(line)
		if !ok {
			continue
		}

		changes = append(changes, change)
	}

	untrackedArgs := []string{
		fmt.Sprintf("--git-dir=%s", state.gitDir),
		fmt.Sprintf("--work-tree=%s", state.workspacePath),
		"ls-files",
		"--others",
		"--exclude-standard",
	}
	untrackedOutput, err := execGitOutput(state.workspacePath, untrackedArgs...)
	if err != nil {
		return nil, err
	}

	for _, line := range strings.Split(strings.TrimRight(untrackedOutput, "\n"), "\n") {
		path := filepath.ToSlash(strings.TrimSpace(line))
		if path == "" || !shouldTrackRelativePath(path) {
			continue
		}

		changes = append(changes, Change{
			Path:   path,
			Status: ChangeStatusAdded,
		})
	}

	return dedupeChanges(changes), nil
}

func (s *ChangesService) loadCompareContent(state *changeState, relativePath string) (compareContent, compareContent, error) {
	before, err := s.loadBaselineContent(state, relativePath)
	if err != nil {
		return compareContent{}, compareContent{}, err
	}

	after, err := s.loadCurrentContent(state, relativePath)
	if err != nil {
		return compareContent{}, compareContent{}, err
	}

	return before, after, nil
}

func (s *ChangesService) loadBaselineContent(state *changeState, relativePath string) (compareContent, error) {
	if !state.hasHead {
		return compareContent{}, nil
	}

	gitPath := workspacePathToRepoPath(state, relativePath)
	args := []string{"show", fmt.Sprintf("%s:%s", state.baselineRef, gitPath)}
	if state.mode == changeModeSnapshot {
		args = append([]string{
			fmt.Sprintf("--git-dir=%s", state.gitDir),
			fmt.Sprintf("--work-tree=%s", state.workspacePath),
		}, args...)
	}

	data, err := execGitBinary(state.workspacePath, args...)
	if err != nil {
		if isGitObjectMissing(err) {
			return compareContent{}, nil
		}
		return compareContent{}, err
	}

	return compareContent{exists: true, data: data}, nil
}

func (s *ChangesService) loadCurrentContent(state *changeState, relativePath string) (compareContent, error) {
	resolved, err := s.files.ResolveFile(state.workspacePath, relativePath)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return compareContent{}, nil
		}
		return compareContent{}, err
	}
	if resolved.Info.IsDir() {
		return compareContent{}, ErrIsDirectory
	}

	data, err := os.ReadFile(resolved.AbsolutePath)
	if err != nil {
		if os.IsNotExist(err) {
			return compareContent{}, nil
		}
		return compareContent{}, err
	}

	return compareContent{exists: true, data: data}, nil
}

func buildNumstat(relativePath string, before compareContent, after compareContent) (int, int, error) {
	output, err := runNoIndexGitDiff(relativePath, before, after, "--numstat")
	if err != nil {
		return 0, 0, err
	}

	line := strings.TrimSpace(output)
	if line == "" {
		return 0, 0, nil
	}

	parts := strings.SplitN(line, "\t", 3)
	if len(parts) < 3 {
		return 0, 0, nil
	}
	if parts[0] == "-" || parts[1] == "-" {
		return 0, 0, nil
	}

	insertions, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, nil
	}
	deletions, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, nil
	}

	return insertions, deletions, nil
}

func buildUnifiedDiff(relativePath string, before compareContent, after compareContent) (string, error) {
	output, err := runNoIndexGitDiff(relativePath, before, after, "--no-ext-diff", "--unified=3")
	if err != nil {
		return "", err
	}

	return output, nil
}

func runNoIndexGitDiff(relativePath string, before compareContent, after compareContent, extraArgs ...string) (string, error) {
	if !before.exists && !after.exists {
		return "", nil
	}

	tempDir, err := os.MkdirTemp("", "lumi-workspace-diff-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tempDir)

	normalizedPath := filepath.FromSlash(relativePath)
	oldPath := filepath.Join(tempDir, "before", normalizedPath)
	newPath := filepath.Join(tempDir, "after", normalizedPath)
	oldArg := oldPath
	newArg := newPath

	if before.exists {
		if err := writeTempDiffFile(oldPath, before.data); err != nil {
			return "", err
		}
	} else {
		oldArg = os.DevNull
	}
	if after.exists {
		if err := writeTempDiffFile(newPath, after.data); err != nil {
			return "", err
		}
	} else {
		newArg = os.DevNull
	}

	args := []string{"diff", "--no-index"}
	args = append(args, extraArgs...)
	args = append(args, "--", oldArg, newArg)

	output, err := execGitBinaryWithDiffExit(tempDir, args...)
	if err != nil {
		return "", err
	}

	return rewriteDiffPaths(string(output), relativePath, oldPath, newPath), nil
}

func writeTempDiffFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o644)
}

func rewriteDiffPaths(content string, relativePath string, oldPath string, newPath string) string {
	displayPath := filepath.ToSlash(relativePath)
	oldSlashPath := filepath.ToSlash(oldPath)
	newSlashPath := filepath.ToSlash(newPath)

	replacements := []struct {
		old string
		new string
	}{
		{"a" + oldSlashPath, "a/" + displayPath},
		{"b" + oldSlashPath, "b/" + displayPath},
		{"b" + newSlashPath, "b/" + displayPath},
		{"a" + newSlashPath, "a/" + displayPath},
		{"a/" + oldSlashPath, "a/" + displayPath},
		{"b/" + oldSlashPath, "b/" + displayPath},
		{"b/" + newSlashPath, "b/" + displayPath},
		{"a/" + newSlashPath, "a/" + displayPath},
		{oldSlashPath, displayPath},
		{newSlashPath, displayPath},
		{oldPath, filepath.FromSlash(displayPath)},
		{newPath, filepath.FromSlash(displayPath)},
	}

	rewritten := content
	for _, replacement := range replacements {
		rewritten = strings.ReplaceAll(rewritten, replacement.old, replacement.new)
	}

	return rewritten
}

func cleanupChangeState(state *changeState) error {
	if state == nil || state.mode != changeModeSnapshot || state.gitDir == "" {
		return nil
	}

	return os.RemoveAll(state.gitDir)
}

func parseGitStatusLine(line string) (Change, bool) {
	if len(line) < 3 {
		return Change{}, false
	}

	x := line[0]
	y := line[1]
	if x == '!' && y == '!' {
		return Change{}, false
	}

	path := parseStatusPath(line[3:])
	if path == "" || !shouldTrackRelativePath(path) {
		return Change{}, false
	}

	return Change{
		Path:   path,
		Status: statusFromGitCodes(x, y),
	}, true
}

func parseNameStatusLine(line string) (Change, bool) {
	fields := strings.Split(line, "\t")
	if len(fields) < 2 {
		return Change{}, false
	}

	statusField := fields[0]
	path := fields[len(fields)-1]
	path = filepath.ToSlash(strings.TrimSpace(path))
	if path == "" || !shouldTrackRelativePath(path) {
		return Change{}, false
	}

	return Change{
		Path:   path,
		Status: statusFromNameStatus(statusField),
	}, true
}

func parseStatusPath(raw string) string {
	path := strings.TrimSpace(raw)
	if strings.Contains(path, " -> ") {
		parts := strings.Split(path, " -> ")
		path = parts[len(parts)-1]
	}

	return filepath.ToSlash(path)
}

func statusFromGitCodes(x byte, y byte) ChangeStatus {
	switch {
	case x == '?' && y == '?':
		return ChangeStatusAdded
	case x == 'A' || x == 'C':
		return ChangeStatusAdded
	case x == 'D' || y == 'D':
		return ChangeStatusDeleted
	default:
		return ChangeStatusModified
	}
}

func statusFromNameStatus(value string) ChangeStatus {
	if value == "" {
		return ChangeStatusModified
	}

	switch value[0] {
	case 'A', 'C':
		return ChangeStatusAdded
	case 'D':
		return ChangeStatusDeleted
	default:
		return ChangeStatusModified
	}
}

func shouldTrackRelativePath(relativePath string) bool {
	parts := strings.Split(filepath.ToSlash(relativePath), "/")
	for index, part := range parts {
		if part == "" || part == "." {
			continue
		}
		if shouldSkipWorkspaceEntry(part, index < len(parts)-1) {
			return false
		}
	}

	return true
}

func repoPathToWorkspacePath(state *changeState, path string) (string, bool) {
	if state == nil || state.gitPrefix == "" {
		return path, true
	}

	prefix := filepath.ToSlash(strings.TrimSpace(state.gitPrefix))
	if prefix == "" {
		return path, true
	}
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	if !strings.HasPrefix(path, prefix) {
		return "", false
	}

	trimmed := strings.TrimPrefix(path, prefix)
	if trimmed == "" {
		return "", false
	}

	return trimmed, true
}

func workspacePathToRepoPath(state *changeState, path string) string {
	if state == nil || state.gitPrefix == "" {
		return path
	}

	prefix := filepath.ToSlash(strings.TrimSpace(state.gitPrefix))
	if prefix == "" {
		return path
	}
	if strings.HasSuffix(prefix, "/") {
		return prefix + path
	}

	return prefix + "/" + path
}

func dedupeChanges(changes []Change) []Change {
	seen := make(map[string]Change, len(changes))
	for _, change := range changes {
		seen[change.Path] = change
	}

	result := make([]Change, 0, len(seen))
	for _, change := range seen {
		result = append(result, change)
	}

	return result
}

func isGitRepo(workspacePath string) bool {
	return execGit(workspacePath, "rev-parse", "--git-dir") == nil
}

func gitHasRef(workspacePath string, ref string) bool {
	return execGit(workspacePath, "rev-parse", "--verify", ref) == nil
}

func writeSnapshotExcludes(gitDir string) error {
	excludes := []string{
		".git/",
		".lumi-uploads/",
		"node_modules/",
		"vendor/",
		"build/",
		"dist/",
		"coverage/",
		".next/",
		".nuxt/",
		".cache/",
		"__pycache__/",
	}

	infoDir := filepath.Join(gitDir, "info")
	if err := os.MkdirAll(infoDir, 0o755); err != nil {
		return err
	}

	content := strings.Join(excludes, "\n") + "\n"
	return os.WriteFile(filepath.Join(infoDir, "exclude"), []byte(content), 0o644)
}

func execGit(workspacePath string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = workspacePath
	return cmd.Run()
}

func execGitOutput(workspacePath string, args ...string) (string, error) {
	output, err := execGitBinary(workspacePath, args...)
	if err != nil {
		return "", err
	}

	return string(output), nil
}

func execGitBinary(workspacePath string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = workspacePath
	return cmd.Output()
}

func execGitBinaryWithDiffExit(workspacePath string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = workspacePath
	output, err := cmd.CombinedOutput()
	if err == nil {
		return output, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return output, nil
	}

	return nil, fmt.Errorf("git %s failed: %w", strings.Join(args, " "), err)
}

func isGitObjectMissing(err error) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}

	stderr := string(exitErr.Stderr)
	return strings.Contains(stderr, "does not exist in") ||
		strings.Contains(stderr, "exists on disk, but not in") ||
		strings.Contains(stderr, "invalid object name")
}
