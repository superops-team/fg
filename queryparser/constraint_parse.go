package queryparser

import "strings"

// parseConstraintToken 尝试把一个 token 解析为结构化约束。
// 解析失败（即这是普通模糊匹配文本）时返回 nil。
// 返回值为非 nil 时，*Constraint 是一个具体约束；返回 nil 时该 token 应并入 FuzzyQuery。
func parseConstraintToken(token string) *Constraint {
	if token == "" {
		return nil
	}
	// 取反: !xxx —— 先递归解析子 token。
	if strings.HasPrefix(token, "!") && len(token) > 1 {
		child := parseConstraintToken(token[1:])
		if child == nil {
			// !some_text —— 退化为 Not(Text)
			return &Constraint{Kind: CNot, Child: &Constraint{Kind: CText, Value: token[1:]}}
		}
		return &Constraint{Kind: CNot, Child: child}
	}

	// Extension: *.xxx / *xxx
	if len(token) > 2 && token[0] == '*' && token[1] == '.' {
		ext := strings.ToLower(token[2:])
		if ext != "" {
			return &Constraint{Kind: CExtension, Value: ext}
		}
	}

	// Glob: 包含 "**" 或 "*" + 至少一个 "/"
	if strings.Contains(token, "**") || strings.ContainsAny(token, "*?") {
		// 避免误把 "*." 错当作 glob
		if !strings.HasPrefix(token, "*.") || strings.Contains(token[2:], "/") {
			if strings.Contains(token, "*") || strings.Contains(token, "?") {
				return &Constraint{Kind: CGlob, Value: token}
			}
		}
	}

	// Path segment: /xxx/ (两端斜杠) 或 /xxx (开头斜杠, 视为段)
	if len(token) >= 3 && strings.HasPrefix(token, "/") {
		seg := strings.Trim(token, "/")
		if seg != "" {
			return &Constraint{Kind: CPathSegment, Value: strings.ToLower(seg)}
		}
	}

	// key:value 形式
	if idx := strings.Index(token, ":"); idx > 0 && idx < len(token)-1 {
		key := strings.ToLower(token[:idx])
		val := token[idx+1:]
		switch key {
		case "status":
			// status:modified / status:added / status:deleted / status:untracked / status:clean
			return &Constraint{Kind: CGitStatus, Value: strings.ToLower(val)}
		case "type":
			return &Constraint{Kind: CFileType, Value: strings.ToLower(val)}
		case "modified":
			// modified:7d / modified:24h
			return &Constraint{Kind: CModifiedAgo, Value: val}
		}
	}

	// SizeCmp: >1MB / <10KB / =1KB / >=100B / <=5MB
	if len(token) >= 2 {
		op, rest := "", token
		switch {
		case strings.HasPrefix(token, ">="):
			op, rest = ">=", token[2:]
		case strings.HasPrefix(token, "<="):
			op, rest = "<=", token[2:]
		case strings.HasPrefix(token, ">"):
			op, rest = ">", token[1:]
		case strings.HasPrefix(token, "<"):
			op, rest = "<", token[1:]
		case strings.HasPrefix(token, "="):
			op, rest = "=", token[1:]
		}
		if op != "" {
			if n, ok := ParseHumanSize(rest); ok {
				var op_ SizeOp
				switch op {
				case ">":
					op_ = SizeGt
				case "<":
					op_ = SizeLt
				case ">=":
					op_ = SizeGte
				case "<=":
					op_ = SizeLte
				case "=":
					op_ = SizeEq
				}
				return &Constraint{Kind: CSizeCmp, SizeOp: op_, SizeBytes: n, Value: rest}
			}
		}
	}

	return nil
}
