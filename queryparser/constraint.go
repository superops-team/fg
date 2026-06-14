package queryparser

import (
	"strconv"
	"strings"
)

// ConstraintKind 是约束的种类。
type ConstraintKind int

const (
	CText ConstraintKind = iota // 通用文本约束 —— 被 parser 合并到 FuzzyQuery
	CNot                        // 取反：Children[0] 为被取反的约束
	CExtension                  // *.go / *.rs
	CGlob                       // **/*.go / src/**/*.rs
	CPathSegment                // /src/ (斜杠包裹的 segment)
	CGitStatus                  // status:modified / status:added / status:untracked
	CFileType                   // type:go / type:rust (语言类型，内部映射到常见扩展名)
	CSizeCmp                    // >1MB / <10KB / =1KB / >=100B
	CModifiedAgo                // modified:7d / modified:24h
)

// SizeOp 用于 CSizeCmp。
type SizeOp int

const (
	SizeEq SizeOp = iota
	SizeGt
	SizeLt
	SizeGte
	SizeLte
)

// Constraint 是解析出的单个约束。
type Constraint struct {
	Kind   ConstraintKind
	Value  string      // 文本字段；按 Kind 含义不同
	Child  *Constraint // CNot 时指向被取反的子约束
	// CSizeCmp 专用
	SizeOp    SizeOp
	SizeBytes int64
}

// String 返回约束的可读表示（便于测试/调试）。
func (c Constraint) String() string {
	switch c.Kind {
	case CExtension:
		return "*." + c.Value
	case CGlob:
		return "glob:" + c.Value
	case CPathSegment:
		return "/" + c.Value + "/"
	case CGitStatus:
		return "status:" + c.Value
	case CFileType:
		return "type:" + c.Value
	case CSizeCmp:
		return opStr(c.SizeOp) + strconv.FormatInt(c.SizeBytes, 10) + "B"
	case CModifiedAgo:
		return "modified:" + c.Value
	case CNot:
		if c.Child != nil {
			return "!" + c.Child.String()
		}
		return "!?"
	}
	return "text:" + c.Value
}

func opStr(op SizeOp) string {
	switch op {
	case SizeEq:
		return "="
	case SizeGt:
		return ">"
	case SizeLt:
		return "<"
	case SizeGte:
		return ">="
	case SizeLte:
		return "<="
	}
	return "?"
}

// ExtensionFor 把 "type:go" / "type:rs" 等语言名映射为常见扩展名集（用于文件过滤）。
// 返回 nil 表示不识别此语言名。
func ExtensionFor(lang string) []string {
	switch strings.ToLower(lang) {
	case "go", "golang":
		return []string{".go"}
	case "rs", "rust":
		return []string{".rs"}
	case "py", "python":
		return []string{".py", ".pyi", ".pyw"}
	case "js", "javascript":
		return []string{".js", ".mjs", ".cjs"}
	case "ts", "typescript":
		return []string{".ts", ".tsx"}
	case "java":
		return []string{".java"}
	case "c":
		return []string{".c", ".h"}
	case "cpp", "c++":
		return []string{".cpp", ".cc", ".hpp", ".h"}
	case "rb", "ruby":
		return []string{".rb"}
	case "sh", "shell", "bash":
		return []string{".sh", ".bash", ".zsh"}
	case "md", "markdown":
		return []string{".md", ".markdown"}
	case "json":
		return []string{".json"}
	case "yaml", "yml":
		return []string{".yaml", ".yml"}
	case "toml":
		return []string{".toml"}
	case "html":
		return []string{".html", ".htm"}
	case "css":
		return []string{".css"}
	}
	return nil
}

// ParseHumanSize 解析 "1KB" / "10MB" / "1B" / "1" 为字节数。大小写不敏感。
// 数字 + 可选单位（B / KB / MB / GB / TB，按 1024 幂）。
// 解析失败返回 0, false。
func ParseHumanSize(s string) (int64, bool) {
	s = strings.TrimSpace(strings.ToUpper(s))
	if s == "" {
		return 0, false
	}
	// 分离数字前缀与单位后缀
	i := 0
	for i < len(s) && (s[i] >= '0' && s[i] <= '9') {
		i++
	}
	if i == 0 {
		return 0, false
	}
	numStr := s[:i]
	unit := strings.TrimSpace(s[i:])
	n, err := strconv.ParseInt(numStr, 10, 64)
	if err != nil {
		return 0, false
	}
	var mult int64 = 1
	switch unit {
	case "", "B":
		mult = 1
	case "KB":
		mult = 1024
	case "MB":
		mult = 1024 * 1024
	case "GB":
		mult = 1024 * 1024 * 1024
	case "TB":
		mult = 1024 * 1024 * 1024 * 1024
	default:
		return 0, false
	}
	return n * mult, true
}

// ParseDurationAgo 解析 "7d" / "24h" / "60m" 为秒数。
func ParseDurationAgo(s string) (seconds int64, ok bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0, false
	}
	i := 0
	for i < len(s) && (s[i] >= '0' && s[i] <= '9') {
		i++
	}
	if i == 0 {
		return 0, false
	}
	num, err := strconv.ParseInt(s[:i], 10, 64)
	if err != nil {
		return 0, false
	}
	unit := strings.TrimSpace(s[i:])
	var sec int64
	switch unit {
	case "s":
		sec = 1
	case "m":
		sec = 60
	case "h":
		sec = 3600
	case "d":
		sec = 86400
	case "w":
		sec = 7 * 86400
	default:
		return 0, false
	}
	return num * sec, true
}
