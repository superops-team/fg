package core

// GitStatusKind 表示文件在 git 工作区的状态。
// iota 从 0 开始，GitStatusClean = 0，表示明确探测到的"干净"状态。
// FileItem.GitStatusPtr = nil 表示"未探测"（与 Clean 语义不同）。
type GitStatusKind uint8

const (
	GitStatusClean GitStatusKind = iota
	GitStatusModified
	GitStatusAdded
	GitStatusDeleted
	GitStatusRenamed
	GitStatusUntracked
	GitStatusIgnored
)

// GitStatus 表示一个文件/目录的 git 状态。
// 保留为 struct 是为了 v1.x 可能加 rename-from 等字段。
type GitStatus struct {
	Kind GitStatusKind
}

// IsDirty 返回 true 表示该文件在工作区中存在变更（modified/added/deleted/renamed）
func (g GitStatus) IsDirty() bool {
	switch g.Kind {
	case GitStatusModified, GitStatusAdded, GitStatusDeleted, GitStatusRenamed:
		return true
	}
	return false
}

// String 返回可读字符串，方便日志/测试
func (g GitStatusKind) String() string {
	switch g {
	case GitStatusClean:
		return "clean"
	case GitStatusModified:
		return "modified"
	case GitStatusAdded:
		return "added"
	case GitStatusDeleted:
		return "deleted"
	case GitStatusRenamed:
		return "renamed"
	case GitStatusUntracked:
		return "untracked"
	case GitStatusIgnored:
		return "ignored"
	}
	return "unknown"
}
