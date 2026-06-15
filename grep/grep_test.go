package grep

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
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

func TestGrep_SearchManyPreservesInputOrder(t *testing.T) {
	dir := t.TempDir()
	files := []string{
		writeTestFile(t, dir, "a.txt", "needle a\n"),
		writeTestFile(t, dir, "b.txt", "needle b\n"),
		writeTestFile(t, dir, "c.txt", "needle c\n"),
	}

	m := New(Options{Concurrency: 3})
	results, err := m.SearchMany(files, "needle", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != len(files) {
		t.Fatalf("expected %d results, got %d", len(files), len(results))
	}
	for i, result := range results {
		if result.Path != files[i] {
			t.Fatalf("result %d should preserve input order path %q, got %q", i, files[i], result.Path)
		}
	}
}

func TestGrep_SearchManyReturnsMatchesAndJoinedErrors(t *testing.T) {
	dir := t.TempDir()
	good := writeTestFile(t, dir, "good.txt", "alpha needle\n")
	missing := filepath.Join(dir, "missing.txt")

	m := New(Options{Concurrency: 2})
	results, err := m.SearchMany([]string{good, missing}, "needle", 5)

	if err == nil {
		t.Fatal("SearchMany 应返回缺失文件的聚合 error")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("SearchMany error 应保留 os.ErrNotExist 链，got %v", err)
	}
	if !strings.Contains(err.Error(), missing) {
		t.Fatalf("SearchMany error 应包含失败文件路径 %q，got %v", missing, err)
	}
	if len(results) != 1 {
		t.Fatalf("SearchMany 应同时返回成功文件的命中结果，got %d: %+v", len(results), results)
	}
	if results[0].Path != good || len(results[0].Lines) != 1 {
		t.Fatalf("SearchMany 成功结果不正确: %+v", results)
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

// ================================================================
// 11. 回归: Unicode 大小写不敏感匹配 & 字节位置正确
//     Bug: 原 findCaseInsensitive 用 strings.ToLower(line) 比较后返回
//     Index 位置，但对非 ASCII 文本，字节位置会错位。
// ================================================================

func TestGrep_CaseInsensitiveUnicode(t *testing.T) {
	dir := t.TempDir()
	// 包含 CJK 字符 + 英文大小写
	content := "行号 hello world\n第 2 行 HELLO 中文\nline 3 Café\n"
	fp := writeTestFile(t, dir, "unicode.txt", content)

	// 测试 1: 小写查询应找到大写 HELLO
	m := New(Options{})
	results, err := m.SearchFile(fp, "hello", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("hello 应命中 2 行， got %d", len(results))
	}

	// 测试 2: 大小写敏感不应命中
	m2 := New(Options{CaseInsensitive: false})
	results2, err := m2.SearchFile(fp, "HELLO", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results2) != 1 {
		t.Fatalf("HELLO 大小写敏感应命中 1 行， got %d", len(results2))
	}

	// 测试 3: Unicode 字符的匹配 —— Café 应能被 café 命中（不敏感）
	results3, err := m.SearchFile(fp, "café", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results3) != 1 {
		t.Fatalf("café 应命中 1 行（Unicode 小写化）， got %d", len(results3))
	}

	// 测试 4: 匹配位置的字节范围必须合理
	results4, err := m.SearchFile(fp, "world", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results4) != 1 {
		t.Fatalf("world 应命中 1 行， got %d", len(results4))
	}
	// MatchRange.Start 必须小于 line 的长度（防止 Unicode 错位导致越界）
	if results4[0].Matches[0].Start >= len(results4[0].Text) {
		t.Fatalf("MatchRange.Start (%d) 超出 line 长度 (%d)，位置错位",
			results4[0].Matches[0].Start, len(results4[0].Text))
	}
}

func TestGrep_UnicodeMultiTokenCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	content := "CAFÉ world\ncafé hello\nrandom\n"
	fp := writeTestFile(t, dir, "u2.txt", content)

	m := New(Options{})
	results, err := m.SearchFile(fp, "café hello", 5)
	if err != nil {
		t.Fatal(err)
	}
	// 多 token 大小写不敏感：所有 token 都在同一行
	if len(results) < 1 {
		t.Fatalf("café hello 应至少命中 1 行， got %d", len(results))
	}
}

// ================================================================
// 12. 回归: io.SeekStart 替代魔数 0（文件遍历后重置位置）
//     通过 large file 的 binary 检测间接覆盖：large 测试已
//     走 fp.Read + 路径重置逻辑。此处确保小文件 + 小查询不 panic。
// ================================================================

func TestGrep_SmallFileBinaryDetection(t *testing.T) {
	dir := t.TempDir()
	// 小文件纯文本 + 再搜索 —— 覆盖 Read -> SeekStart 路径
	fp := writeTestFile(t, dir, "small.txt", "hello world\n")
	m := New(Options{})
	// 搜索两次，确保 Seek 后第二次搜索正确
	for i := 0; i < 2; i++ {
		results, err := m.SearchFile(fp, "hello", 5)
		if err != nil {
			t.Fatalf("第 %d 次搜索失败: %v", i+1, err)
		}
		if len(results) != 1 {
			t.Fatalf("第 %d 次搜索应命中 1 行， got %d", i+1, len(results))
		}
	}
}

func TestGrep_LongLineBeyondScannerDefaultStillMatches(t *testing.T) {
	dir := t.TempDir()
	longLine := strings.Repeat("x", 70*1024) + "needle\n"
	fp := writeTestFile(t, dir, "long.txt", longLine)

	m := New(Options{})
	results, err := m.SearchFile(fp, "needle", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("long line should still be searched, got %d results", len(results))
	}
	if len(results[0].Matches) != 1 || results[0].Matches[0].Start != 70*1024 {
		t.Fatalf("match range should point into original long line, got %+v", results[0].Matches)
	}
}

func TestGrep_OverMaxLineStillSearchesPrefix(t *testing.T) {
	dir := t.TempDir()
	line := "needle" + strings.Repeat("x", 70*1024)
	fp := writeTestFile(t, dir, "over-max-line.txt", line)

	m := New(Options{MaxBytes: 64 * 1024})
	results, err := m.SearchFile(fp, "needle", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("over-max line should still search the in-budget prefix, got %d results", len(results))
	}
	if len(results[0].Matches) != 1 || results[0].Matches[0].Start != 0 {
		t.Fatalf("match range should point to prefix match, got %+v", results[0].Matches)
	}
}

func TestGrep_CaseInsensitiveSingleTokenReturnsOriginalUnicodeByteRange(t *testing.T) {
	dir := t.TempDir()
	line := "ẞfoo"
	fp := writeTestFile(t, dir, "unicode-range.txt", line+"\n")

	m := New(Options{})
	results, err := m.SearchFile(fp, "foo", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || len(results[0].Matches) != 1 {
		t.Fatalf("expected one match, got %+v", results)
	}
	match := results[0].Matches[0]
	if match.Start != len("ẞ") || match.End != len(line) {
		t.Fatalf("match should use original byte offsets [%d,%d), got [%d,%d)", len("ẞ"), len(line), match.Start, match.End)
	}
	if !utf8.ValidString(results[0].Text[match.Start:match.End]) {
		t.Fatalf("match range should not split unicode runes: %+v", match)
	}
}

func TestGrep_CaseInsensitiveLowercaseExpansionReturnsOriginalUnicodeRange(t *testing.T) {
	dir := t.TempDir()
	line := "Ⱥ"
	fp := writeTestFile(t, dir, "unicode-expansion.txt", line+"\n")

	m := New(Options{})
	results, err := m.SearchFile(fp, "ⱥ", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || len(results[0].Matches) != 1 {
		t.Fatalf("expected one match, got %+v", results)
	}
	match := results[0].Matches[0]
	if match.Start != 0 || match.End != len(line) {
		t.Fatalf("match should use original byte offsets [0,%d), got [%d,%d)", len(line), match.Start, match.End)
	}
}
