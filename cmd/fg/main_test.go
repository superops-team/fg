package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCLITestFile(t *testing.T, root, name, content string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func runCLIForTest(args ...string) (stdout string, stderr string, code int) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	code = run(args, &out, &errOut)
	return out.String(), errOut.String(), code
}

func TestCLI_Help(t *testing.T) {
	stdout, stderr, code := runCLIForTest("--help")
	if code != 0 {
		t.Fatalf("exit code=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "Usage:") {
		t.Fatalf("help should contain Usage, got %q", stdout)
	}
}

func TestCLI_Search(t *testing.T) {
	root := t.TempDir()
	writeCLITestFile(t, root, "main.go", "package main\n")

	stdout, stderr, code := runCLIForTest("-r", root, "type:go main")
	if code != 0 {
		t.Fatalf("exit code=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "main.go") {
		t.Fatalf("stdout should contain main.go, got %q", stdout)
	}
}

func TestCLI_Score(t *testing.T) {
	root := t.TempDir()
	writeCLITestFile(t, root, "main.go", "package main\n")

	stdout, stderr, code := runCLIForTest("-r", root, "--score", "type:go main")
	if code != 0 {
		t.Fatalf("exit code=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "\t") || !strings.Contains(stdout, "main.go") {
		t.Fatalf("score output should be score<TAB>path, got %q", stdout)
	}
}

func TestCLI_Grep(t *testing.T) {
	root := t.TempDir()
	writeCLITestFile(t, root, "note.txt", "hello\nTODO item\n")

	stdout, stderr, code := runCLIForTest("-r", root, "--grep", "TODO")
	if code != 0 {
		t.Fatalf("exit code=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "note.txt") || !strings.Contains(stdout, "TODO item") {
		t.Fatalf("grep output should contain file and matching line, got %q", stdout)
	}
}

func TestCLI_SearchThenGrepAcceptsGrepFlagAfterQuery(t *testing.T) {
	root := t.TempDir()
	writeCLITestFile(t, root, "main.go", "package main\n// TODO main\n")
	writeCLITestFile(t, root, "note.txt", "TODO note\n")

	stdout, stderr, code := runCLIForTest("-r", root, "type:go main", "--grep", "TODO")
	if code != 0 {
		t.Fatalf("exit code=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "main.go") || !strings.Contains(stdout, "TODO main") {
		t.Fatalf("grep output should contain filtered Go file and matching line, got %q", stdout)
	}
	if strings.Contains(stdout, "note.txt") {
		t.Fatalf("grep with file query should not search non-matching files, got %q", stdout)
	}
}

func TestCLI_InvalidRoot(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	_, stderr, code := runCLIForTest("-r", missing, "type:go")
	if code == 0 {
		t.Fatalf("expected non-zero exit code")
	}
	if !strings.Contains(stderr, "stat") {
		t.Fatalf("stderr should contain stat error, got %q", stderr)
	}
}
