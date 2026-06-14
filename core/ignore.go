package core

// IgnoreMatcher 判断某个相对路径/文件名是否应被忽略。
// 提供多种实现: 空实现（从不忽略，用于不启用 ignore 规则时）、
// 未来可接入 go-git/gitignore 实现（picker/ 层负责）。
type IgnoreMatcher interface {
	Match(relativePath string) bool
}

// NeverIgnore 从不忽略任何路径 —— 作为默认实现和测试桩。
type NeverIgnore struct{}

func (NeverIgnore) Match(string) bool { return false }

// AlwaysIgnore 总是忽略 —— 测试用。
type AlwaysIgnore struct{}

func (AlwaysIgnore) Match(string) bool { return true }
