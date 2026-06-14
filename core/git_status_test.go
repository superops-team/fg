package core

import "testing"

func TestGitStatus_IsDirty(t *testing.T) {
	tests := []struct {
		kind   GitStatusKind
		expect bool
	}{
		{GitStatusClean, false},
		{GitStatusModified, true},
		{GitStatusAdded, true},
		{GitStatusDeleted, true},
		{GitStatusRenamed, true},
		{GitStatusUntracked, false},
		{GitStatusIgnored, false},
	}
	for _, tt := range tests {
		t.Run(tt.kind.String(), func(t *testing.T) {
			gs := GitStatus{Kind: tt.kind}
			if got := gs.IsDirty(); got != tt.expect {
				t.Fatalf("IsDirty()=%v, want %v", got, tt.expect)
			}
		})
	}
}

func TestGitStatus_NilPtrVsClean(t *testing.T) {
	// 验证 nil 与 Clean 是不同语义（nil = 未探测，Clean = 已探测且无更改）
	f := FileItem{}
	if f.GitStatus() != nil {
		t.Fatalf("新 FileItem 的 GitStatus 应为 nil")
	}
	f.SetGitStatus(&GitStatus{Kind: GitStatusClean})
	if f.GitStatus() == nil {
		t.Fatalf("Set Clean 后不应为 nil")
	}
	if f.GitStatus().IsDirty() {
		t.Fatalf("Clean 不应 dirty")
	}
}

func TestGitStatusKind_String(t *testing.T) {
	// 至少覆盖所有值返回非空（避免 panic）
	for _, k := range []GitStatusKind{
		GitStatusClean, GitStatusModified, GitStatusAdded,
		GitStatusDeleted, GitStatusRenamed, GitStatusUntracked,
		GitStatusIgnored, GitStatusKind(99), // 未知值也不应 panic
	} {
		s := k.String()
		if s == "" {
			t.Fatalf("GitStatusKind(%d).String() 不应返回空", k)
		}
	}
}
