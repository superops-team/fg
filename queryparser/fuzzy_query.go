package queryparser

import "strings"

// FuzzyQueryKind 区分 FuzzyQuery 的形态。
type FuzzyQueryKind int

const (
	FuzzyEmpty FuzzyQueryKind = iota // 未指定文本 token
	FuzzyText                        // 单 token：直接字符串匹配
	FuzzyParts                       // 多 token：各部分均应命中
)

// FuzzyQuery 保存纯文本模糊匹配的部分。
type FuzzyQuery struct {
	Kind  FuzzyQueryKind
	Text  string   // FuzzyText 时有效
	Parts []string // FuzzyParts 时有效
}

// IsEmpty 返回 true 表示没有模糊匹配词。
func (f FuzzyQuery) IsEmpty() bool { return f.Kind == FuzzyEmpty }

// Joined 将所有 token 用空格连起来（供 bigram 使用时需要一个统一的串去生成 bigram 索引词）。
func (f FuzzyQuery) Joined() string {
	switch f.Kind {
	case FuzzyEmpty:
		return ""
	case FuzzyText:
		return f.Text
	case FuzzyParts:
		return strings.Join(f.Parts, " ")
	}
	return ""
}

// Tokens 返回模糊匹配的每个 token（便于逐词打分）。
// FuzzyText 时返回 1 元素切片；FuzzyEmpty 返回空切片。
func (f FuzzyQuery) Tokens() []string {
	switch f.Kind {
	case FuzzyEmpty:
		return nil
	case FuzzyText:
		return []string{f.Text}
	case FuzzyParts:
		return f.Parts
	}
	return nil
}
