package core

import (
	"bytes"
	"testing"
)

func TestDetectBinaryContent(t *testing.T) {
	tests := []struct {
		name   string
		input  []byte
		expect bool
	}{
		{"空切片", nil, false},
		{"纯文本", []byte("hello world\nline2\n"), false},
		{"包含 NUL", []byte("hello\x00world"), true},
		{"NUL 在开头", []byte{0, 'a', 'b'}, true},
		{"NUL 在末尾", []byte{'a', 'b', 0}, true},
		{"纯 ASCII 大文本但无 NUL", bytes.Repeat([]byte{'A'}, 16*1024), false},
		{"超过 16KB 但 NUL 在 20KB 处 → 被忽略", append(bytes.Repeat([]byte{'A'}, 20*1024), 0), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DetectBinaryContent(tt.input); got != tt.expect {
				t.Fatalf("DetectBinaryContent()=%v, want %v", got, tt.expect)
			}
		})
	}
}
