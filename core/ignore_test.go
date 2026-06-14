package core

import "testing"

func TestIgnoreMatcher(t *testing.T) {
	var m IgnoreMatcher = NeverIgnore{}
	if m.Match("anything") {
		t.Fatal("NeverIgnore 应永远不忽略")
	}
	m = AlwaysIgnore{}
	if !m.Match("anything") {
		t.Fatal("AlwaysIgnore 应总是忽略")
	}
}
