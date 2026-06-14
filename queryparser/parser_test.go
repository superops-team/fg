package queryparser

import (
	"testing"
)

// ========================================
// 辅助: 快速断言
// ========================================

func countByKind(cs []Constraint, kind ConstraintKind) int {
	n := 0
	for _, c := range cs {
		if c.Kind == kind {
			n++
		}
	}
	return n
}

func firstByKind(cs []Constraint, kind ConstraintKind) *Constraint {
	for i := range cs {
		if cs[i].Kind == kind {
			return &cs[i]
		}
	}
	return nil
}

// ========================================
// Parse: 基础形态
// ========================================

func TestParse_Empty(t *testing.T) {
	q := New().Parse("")
	if !q.Fuzzy.IsEmpty() {
		t.Fatal("空串应为 FuzzyEmpty")
	}
	if len(q.Constraints) != 0 {
		t.Fatal("空串不应有约束")
	}
	q2 := New().Parse("   \t  ")
	if !q2.Fuzzy.IsEmpty() || len(q2.Constraints) != 0 {
		t.Fatal("纯空白串应解析为空")
	}
}

func TestParse_SingleFuzzyText(t *testing.T) {
	q := New().Parse("hello")
	if q.Fuzzy.Kind != FuzzyText || q.Fuzzy.Text != "hello" {
		t.Fatalf("应解析为 FuzzyText(hello), got %+v", q.Fuzzy)
	}
	if len(q.Constraints) != 0 {
		t.Fatal("不应有约束")
	}
	if got := q.Fuzzy.Joined(); got != "hello" {
		t.Fatalf("Joined=%q", got)
	}
	toks := q.Fuzzy.Tokens()
	if len(toks) != 1 || toks[0] != "hello" {
		t.Fatalf("Tokens=%v", toks)
	}
}

func TestParse_MultiFuzzyParts(t *testing.T) {
	q := New().Parse("src hello foo")
	if q.Fuzzy.Kind != FuzzyParts {
		t.Fatalf("应为 FuzzyParts, got %v", q.Fuzzy.Kind)
	}
	if len(q.Fuzzy.Parts) != 3 {
		t.Fatalf("Parts len=%d, want 3", len(q.Fuzzy.Parts))
	}
	if q.Fuzzy.Joined() != "src hello foo" {
		t.Fatalf("Joined=%q", q.Fuzzy.Joined())
	}
}

// ========================================
// Parse: Extension
// ========================================

func TestParse_ExtensionOnly(t *testing.T) {
	q := New().Parse("*.go")
	if countByKind(q.Constraints, CExtension) != 1 {
		t.Fatalf("应有 1 个 Extension, got %+v", q.Constraints)
	}
	c := firstByKind(q.Constraints, CExtension)
	if c.Value != "go" {
		t.Fatalf("Extension Value=%q, want go", c.Value)
	}
	if !q.Fuzzy.IsEmpty() {
		t.Fatal("仅 *.go 时 Fuzzy 应是空")
	}
}

func TestParse_Extension_WithText(t *testing.T) {
	q := New().Parse("foo *.rs main")
	if q.Fuzzy.Kind != FuzzyParts || len(q.Fuzzy.Parts) != 2 {
		t.Fatalf("Fuzzy 应为 2 个 Parts, got %+v", q.Fuzzy)
	}
	if countByKind(q.Constraints, CExtension) != 1 {
		t.Fatalf("应有 1 个 Extension")
	}
}

// ========================================
// Parse: Glob
// ========================================

func TestParse_Glob(t *testing.T) {
	q := New().Parse("**/*.go foo")
	if countByKind(q.Constraints, CGlob) != 1 {
		t.Fatalf("应有 1 个 Glob, got %+v", q.Constraints)
	}
	if q.Fuzzy.Kind != FuzzyText || q.Fuzzy.Text != "foo" {
		t.Fatalf("Fuzzy 应为 FuzzyText(foo), got %+v", q.Fuzzy)
	}
}

func TestParse_Glob_NestedPath(t *testing.T) {
	q := New().Parse("src/**/*.go")
	if countByKind(q.Constraints, CGlob) != 1 {
		t.Fatalf("应有 1 个 Glob, got %+v", q.Constraints)
	}
}

// ========================================
// Parse: Path segment
// ========================================

func TestParse_PathSegment(t *testing.T) {
	q := New().Parse("/src/ foo")
	if countByKind(q.Constraints, CPathSegment) != 1 {
		t.Fatalf("应有 1 个 PathSegment, got %+v", q.Constraints)
	}
	c := firstByKind(q.Constraints, CPathSegment)
	if c.Value != "src" {
		t.Fatalf("segment=%q, want src", c.Value)
	}
}

func TestParse_PathSegment_SingleTrailing(t *testing.T) {
	// "/src" 也应解析为 segment
	q := New().Parse("/src foo")
	if countByKind(q.Constraints, CPathSegment) != 1 {
		t.Fatalf("/src 应解析为 PathSegment, got %+v", q.Constraints)
	}
}

// ========================================
// Parse: Git status / File type
// ========================================

func TestParse_GitStatus(t *testing.T) {
	q := New().Parse("status:modified foo")
	if countByKind(q.Constraints, CGitStatus) != 1 {
		t.Fatalf("应有 1 个 GitStatus")
	}
	if c := firstByKind(q.Constraints, CGitStatus); c.Value != "modified" {
		t.Fatalf("Value=%q, want modified", c.Value)
	}
}

func TestParse_FileType(t *testing.T) {
	q := New().Parse("type:rust main")
	if countByKind(q.Constraints, CFileType) != 1 {
		t.Fatalf("应有 1 个 FileType")
	}
	if c := firstByKind(q.Constraints, CFileType); c.Value != "rust" {
		t.Fatalf("Value=%q, want rust", c.Value)
	}
}

// ========================================
// Parse: 取反
// ========================================

func TestParse_NotText(t *testing.T) {
	q := New().Parse("!secret")
	if countByKind(q.Constraints, CNot) != 1 {
		t.Fatalf("应有 1 个 Not, got %+v", q.Constraints)
	}
	c := firstByKind(q.Constraints, CNot)
	if c.Child == nil || c.Child.Kind != CText || c.Child.Value != "secret" {
		t.Fatalf("!secret 的 Child 应为 Text(secret), got %+v", c.Child)
	}
}

func TestParse_NotExtension(t *testing.T) {
	q := New().Parse("!*.go foo")
	if countByKind(q.Constraints, CNot) != 1 {
		t.Fatal("应有 1 个 Not")
	}
	c := firstByKind(q.Constraints, CNot)
	if c.Child == nil || c.Child.Kind != CExtension || c.Child.Value != "go" {
		t.Fatalf("!*.go 的 Child 应为 Extension(go), got %+v", c.Child)
	}
}

// ========================================
// Parse: SizeCmp
// ========================================

func TestParse_SizeCmp(t *testing.T) {
	tests := []struct {
		in        string
		wantOp    SizeOp
		wantBytes int64
	}{
		{">1MB", SizeGt, 1024 * 1024},
		{"<10KB", SizeLt, 10 * 1024},
		{"=1B", SizeEq, 1},
		{">=100B", SizeGte, 100},
		{"<=5MB", SizeLte, 5 * 1024 * 1024},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			q := New().Parse(tt.in + " foo")
			if countByKind(q.Constraints, CSizeCmp) != 1 {
				t.Fatalf("%s 应产生 1 个 SizeCmp, got %+v", tt.in, q.Constraints)
			}
			c := firstByKind(q.Constraints, CSizeCmp)
			if c.SizeOp != tt.wantOp {
				t.Fatalf("%s: op=%v, want %v", tt.in, c.SizeOp, tt.wantOp)
			}
			if c.SizeBytes != tt.wantBytes {
				t.Fatalf("%s: bytes=%d, want %d", tt.in, c.SizeBytes, tt.wantBytes)
			}
		})
	}
}

// ========================================
// Parse: modified:duration
// ========================================

func TestParse_ModifiedAgo(t *testing.T) {
	q := New().Parse("modified:7d hello")
	if countByKind(q.Constraints, CModifiedAgo) != 1 {
		t.Fatalf("应有 1 个 ModifiedAgo, got %+v", q.Constraints)
	}
	if c := firstByKind(q.Constraints, CModifiedAgo); c.Value != "7d" {
		t.Fatalf("Value=%q, want 7d", c.Value)
	}
}

// ========================================
// Parse: 综合
// ========================================

func TestParse_Complex(t *testing.T) {
	// 2 个模糊 token + 1 个 Extension + 1 个 Not Text + 1 个 PathSegment + 1 个 GitStatus
	q := New().Parse("src name *.rs !test /lib/ status:modified")
	if q.Fuzzy.Kind != FuzzyParts || len(q.Fuzzy.Parts) != 2 {
		t.Fatalf("Fuzzy 应有 2 个 Parts, got %+v", q.Fuzzy)
	}
	if got := len(q.Constraints); got < 4 {
		t.Fatalf("至少应有 4 个约束, got %d: %+v", got, q.Constraints)
	}
	if countByKind(q.Constraints, CExtension) != 1 {
		t.Fatal("应有 1 Extension")
	}
	if countByKind(q.Constraints, CNot) != 1 {
		t.Fatal("应有 1 Not")
	}
	if countByKind(q.Constraints, CPathSegment) != 1 {
		t.Fatal("应有 1 PathSegment")
	}
	if countByKind(q.Constraints, CGitStatus) != 1 {
		t.Fatal("应有 1 GitStatus")
	}
}

// ========================================
// 辅助函数测试
// ========================================

func TestParseHumanSize_Valid(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"1", 1},
		{"1B", 1},
		{"1KB", 1024},
		{"10MB", 10 * 1024 * 1024},
		{"2GB", 2 * 1024 * 1024 * 1024},
		{"100kb", 100 * 1024}, // 大小写不敏感
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, ok := ParseHumanSize(tt.in)
			if !ok || got != tt.want {
				t.Fatalf("%s → (%d, %v), want (%d, true)", tt.in, got, ok, tt.want)
			}
		})
	}
}

func TestParseHumanSize_Invalid(t *testing.T) {
	bad := []string{"", "abc", "1XB", "-5MB"}
	for _, s := range bad {
		if _, ok := ParseHumanSize(s); ok {
			t.Fatalf("%q 应解析失败", s)
		}
	}
}

func TestParseDurationAgo_Valid(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"7d", 7 * 86400},
		{"24h", 24 * 3600},
		{"60m", 60 * 60},
		{"120s", 120},
		{"2w", 2 * 7 * 86400},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, ok := ParseDurationAgo(tt.in)
			if !ok || got != tt.want {
				t.Fatalf("%s → (%d, %v), want (%d, true)", tt.in, got, ok, tt.want)
			}
		})
	}
}

func TestParseDurationAgo_Invalid(t *testing.T) {
	if _, ok := ParseDurationAgo(""); ok {
		t.Fatal("空串应拒绝")
	}
	if _, ok := ParseDurationAgo("1x"); ok {
		t.Fatal("未知单位应拒绝")
	}
}

func TestExtensionFor(t *testing.T) {
	tests := []struct {
		in     string
		exts   []string
		isNil  bool
	}{
		{"go", []string{".go"}, false},
		{"Go", []string{".go"}, false}, // 大小写不敏感
		{"rust", []string{".rs"}, false},
		{"rs", []string{".rs"}, false},
		{"python", []string{".py", ".pyi", ".pyw"}, false},
		{"xxxxx", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := ExtensionFor(tt.in)
			if tt.isNil {
				if got != nil {
					t.Fatalf("ExtensionFor(%q) 应返回 nil, got %v", tt.in, got)
				}
				return
			}
			if len(got) != len(tt.exts) {
				t.Fatalf("len=%d, want %d (%v)", len(got), len(tt.exts), got)
			}
			for i := range got {
				if got[i] != tt.exts[i] {
					t.Fatalf("exts[%d]=%q, want %q", i, got[i], tt.exts[i])
				}
			}
		})
	}
}

func TestConstraint_String(t *testing.T) {
	// 只是确保不 panic / 返回非空
	cs := []Constraint{
		{Kind: CExtension, Value: "go"},
		{Kind: CGlob, Value: "**/*.go"},
		{Kind: CPathSegment, Value: "src"},
		{Kind: CGitStatus, Value: "modified"},
		{Kind: CNot, Child: &Constraint{Kind: CText, Value: "x"}},
		{Kind: CSizeCmp, SizeOp: SizeGt, SizeBytes: 1024},
		{Kind: CModifiedAgo, Value: "7d"},
		{Kind: CText, Value: "foo"},
	}
	for _, c := range cs {
		s := c.String()
		if s == "" {
			t.Fatalf("%+v 返回空串", c)
		}
	}
}
