package grep

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// helper: 写测试文件
func writeTestFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	fp := filepath.Join(dir, name)
	if err := os.WriteFile(fp, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return fp
}

// ================================================================
// 1. 子串匹配（大小写不敏感）
// ================================================================
func TestGrep_SubstringCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	content := `line 1 hello world
line 2 HELLO again
line 3 nothing here
`
	fp := writeTestFile(t, dir, "a.txt", content)

	m := New(Options{CaseInsensitive: true})
	results, err := m.SearchFile(fp, "hello", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("应命中 2 行， got %d", len(results))
	}
	if !strings.Contains(strings.ToLower(results[0].Text), "hello") {
		t.Fatalf("第 1 行应含 hello: %q", results[0].Text)
	}
	if results[0].Lineno != 1 {
		t.Fatalf("第 1 行行号应为 1， got %d", results[0].Lineno)
	}
	if results[1].Lineno != 2 {
		t.Fatalf("第 2 行行号应为 2， got %d", results[1].Lineno)
	}
}

// ================================================================
// 2. 区分大小写
// ================================================================
func TestGrep_SubstringCaseSensitive(t *testing.T) {
	dir := t.TempDir()
	content := "hello\nHELLO\nHello\n"
	fp := writeTestFile(t, dir, "b.txt", content)

	m := New(Options{CaseInsensitive: false})
	results, err := m.SearchFile(fp, "HELLO", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("HELLO 大小写敏感应命中 1 行， got %d", len(results))
	}
}

// ================================================================
// 3. limit 限制
// ================================================================
func TestGrep_Limit(t *testing.T) {
	dir := t.TempDir()
	var sb strings.Builder
	for i := 0; i < 100; i++ {
		sb.WriteString("needle line " + string(rune('A'+i%26)) + "\n")
	}
	fp := writeTestFile(t, dir, "c.txt", sb.String())

	m := New(Options{})
	results, err := m.SearchFile(fp, "needle", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 5 {
		t.Fatalf("limit=5 应返回 5 行， got %d", len(results))
	}
}

// ================================================================
// 4. 空串查询返回空
// ================================================================
func TestGrep_EmptyQuery(t *testing.T) {
	dir := t.TempDir()
	fp := writeTestFile(t, dir, "d.txt", "hello world\n")

	m := New(Options{})
	results, err := m.SearchFile(fp, "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("空查询应返回 0 行， got %d", len(results))
	}
}

// ================================================================
// 5. binary 文件跳过
// ================================================================
func TestGrep_BinaryFileSkipped(t *testing.T) {
	dir := t.TempDir()
	fp := writeTestFile(t, dir, "e.bin", "\x00\x01needle\x00\x02")

	m := New(Options{})
	results, err := m.SearchFile(fp, "needle", 5)
	if err != nil {
		t.Fatal(err)
	}
	// 默认跳过 binary
	if len(results) != 0 {
		t.Fatalf("binary 文件不应返回结果， got %d", len(results))
	}
	// 强制开启 binary 搜索
	m2 := New(Options{IncludeBinary: true})
	results2, err := m2.SearchFile(fp, "needle", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results2) != 1 {
		t.Fatalf("强制 binary 搜索应命中 1 行， got %d", len(results2))
	}
}

// ================================================================
// 6. 不存在的文件返回 error
// ================================================================
func TestGrep_FileNotFound(t *testing.T) {
	m := New(Options{})
	_, err := m.SearchFile("/path/to/missing/file.txt", "x", 5)
	if err == nil {
		t.Fatal("不存在的文件应返回错误")
	}
}

// ================================================================
// 7. 高亮区间：返回 match 的 [start,end)
// ================================================================
func TestGrep_MatchRanges(t *testing.T) {
	dir := t.TempDir()
	fp := writeTestFile(t, dir, "f.txt", "foo bar baz foo\n")
	m := New(Options{})
	results, err := m.SearchFile(fp, "foo", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("应命中 1 行， got %d", len(results))
	}
	if len(results[0].Matches) < 2 {
		t.Fatalf("该行内应有 2 个 match 区间， got %v", results[0].Matches)
	}
	// 第 1 个 match 应从 0 开始，长度 3
	if results[0].Matches[0].Start != 0 || results[0].Matches[0].End != 3 {
		t.Fatalf("第 1 个 match 应为 [0,3)， got [%d,%d)", results[0].Matches[0].Start, results[0].Matches[0].End)
	}
}

// ================================================================
// 8. 并发搜索多个文件（GrepMany）
// ================================================================
func TestGrep_ManyConcurrent(t *testing.T) {
	dir := t.TempDir()
	files := []string{
		writeTestFile(t, dir, "1.txt", "alpha needle\nbeta no\n"),
		writeTestFile(t, dir, "2.txt", "gamma needle\n"),
		writeTestFile(t, dir, "3.txt", "delta nothing\n"),
		writeTestFile(t, dir, "4.bin", "\x00\x01\x02"),
	}
	m := New(Options{Concurrency: 2})
	results, err := m.SearchMany(files, "needle", 5)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, r := range results {
		total += len(r.Lines)
	}
	if total != 2 {
		t.Fatalf("应共命中 2 行， got %d (results=%+v)", total, results)
	}
}

// ================================================================
// 9. 大文件（>1MB）只扫前 1MB，避免超时
// ================================================================
func TestGrep_LargeFileLimit(t *testing.T) {
	dir := t.TempDir()
	// 构建一个 1.5MB 文件，只在前 500KB 放 needle
	var buf bytes.Buffer
	for i := 0; i < 500*1024/10; i++ {
		buf.WriteString("line " + strings.Repeat("x", 4) + "\n")
	}
	buf.WriteString("needle_here\n")
	rem := 1.5*1024*1024 - float64(buf.Len())
	buf.Write(bytes.Repeat([]byte("y"), int(rem)))
	fp := writeTestFile(t, dir, "large.txt", buf.String())

	m := New(Options{MaxBytes: 2 * 1024 * 1024}) // 2MB 上限足够
	results, err := m.SearchFile(fp, "needle_here", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("大文件应命中 needle_here， got %d 行", len(results))
	}
}

// ================================================================
// 10. fuzzy token 匹配（多个 tokens，都包含才命中）
// ================================================================
func TestGrep_FuzzyTokens(t *testing.T) {
	dir := t.TempDir()
	fp := writeTestFile(t, dir, "g.txt", "foo bar baz\nfoo qux\nbar baz\n")
	m := New(Options{})
	results, err := m.SearchFile(fp, "foo bar", 5)
	if err != nil {
		t.Fatal(err)
	}
	// "foo bar" 作为空格分割的 tokens，所有 token 都包含的行才命中（第 1 行）
	if len(results) != 1 {
		t.Fatalf("fuzzy 'foo bar' 应命中 1 行， got %d", len(results))
	}
}
