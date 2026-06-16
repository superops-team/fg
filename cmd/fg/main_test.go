package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/superops-team/fg/grep"
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
	for _, want := range []string{
		"Usage:",
		"Modes:",
		"fg [flags] \"type:go main\"",
		"fg [flags] \"type:go main\" --grep \"TODO\"",
		"Query constraints:",
		"Root ignore:",
		"Library API:",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("help should contain %q, got %q", want, stdout)
		}
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

func TestCLI_GrepHonorsRootIgnoreWithoutFileQuery(t *testing.T) {
	root := t.TempDir()
	writeCLITestFile(t, root, ".gitignore", "ignored.log\nbuild\n")
	writeCLITestFile(t, root, "keep.txt", "TODO keep\n")
	writeCLITestFile(t, root, "ignored.log", "TODO ignored\n")
	writeCLITestFile(t, root, "build/generated.txt", "TODO generated\n")

	stdout, stderr, code := runCLIForTest("-r", root, "--grep", "TODO")
	if code != 0 {
		t.Fatalf("exit code=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "keep.txt") {
		t.Fatalf("grep output should contain keep.txt, got %q", stdout)
	}
	if strings.Contains(stdout, "ignored.log") || strings.Contains(stdout, "generated.txt") {
		t.Fatalf("grep should honor root ignore rules, got %q", stdout)
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

func TestCLI_QueryCanSpanMultiplePositionalArgs(t *testing.T) {
	root := t.TempDir()
	writeCLITestFile(t, root, "main.go", "package main\n")
	writeCLITestFile(t, root, "helper.go", "package helper\n")

	stdout, stderr, code := runCLIForTest("-r", root, "main", "type:go")
	if code != 0 {
		t.Fatalf("exit code=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "main.go") {
		t.Fatalf("stdout should contain main.go, got %q", stdout)
	}
	if strings.Contains(stdout, "helper.go") {
		t.Fatalf("multi-token query should filter to main.go, got %q", stdout)
	}
}

func TestCLI_GrepPrintsPartialResultsBeforeJoinedError(t *testing.T) {
	root := t.TempDir()
	good := writeCLITestFile(t, root, "good.txt", "needle\n")
	missing := filepath.Join(root, "missing.txt")

	var stdout, stderr bytes.Buffer
	code := printGrepResults(&stdout, &stderr, []grep.FileResult{
		{Path: good, Lines: []grep.LineResult{{Lineno: 1, Text: "needle"}}},
	}, errors.Join(os.ErrNotExist, errors.New(missing)), 10)

	if code == 0 {
		t.Fatal("expected non-zero exit code for partial grep error")
	}
	if !strings.Contains(stdout.String(), "good.txt:1:needle") {
		t.Fatalf("stdout should include successful partial match, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), missing) || !strings.Contains(stderr.String(), "file does not exist") {
		t.Fatalf("stderr should include joined error with missing path, got %q", stderr.String())
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
