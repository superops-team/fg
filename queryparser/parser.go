package queryparser

import "strings"

// QueryParser 解析器。目前无状态；保留 struct 以便未来添加配置。
type QueryParser struct {
	cfg ParserConfig
}

// ParserConfig 预留扩展点。
type ParserConfig struct {
	// （预留给未来：自定义 key-val 处理器、自定义 glob 模式、禁用取反等）
}

// New 返回一个默认 Parser。
func New() *QueryParser { return &QueryParser{} }

// Parse 将原始查询字符串解析为 FFFQuery。
// 行为摘要:
//  1. 按空白切分 tokens（保留 Unicode 大小写；大小写由下游处理）
//  2. 每个 token 先试 parseConstraintToken
//  3. 剩余 token 合并为 FuzzyQuery（1 个 → FuzzyText；多个 → FuzzyParts）
func (p *QueryParser) Parse(raw string) FFFQuery {
	tokens := strings.Fields(raw)
	out := FFFQuery{}
	if len(tokens) == 0 {
		return out
	}

	fuzzyTokens := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		if c := parseConstraintToken(tok); c != nil {
			out.Constraints = append(out.Constraints, *c)
		} else {
			fuzzyTokens = append(fuzzyTokens, tok)
		}
	}

	switch len(fuzzyTokens) {
	case 0:
		out.Fuzzy.Kind = FuzzyEmpty
	case 1:
		out.Fuzzy.Kind = FuzzyText
		out.Fuzzy.Text = fuzzyTokens[0]
	default:
		out.Fuzzy.Kind = FuzzyParts
		out.Fuzzy.Parts = fuzzyTokens
	}
	return out
}

// FFFQuery 是解析的最终结果。
type FFFQuery struct {
	Fuzzy       FuzzyQuery
	Constraints []Constraint
}
