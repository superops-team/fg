package core

import "bytes"

// DetectBinaryContent 判断一个字节切片是否包含二进制内容。
// 规则: 首 16KB 内若存在 NUL 字节 → 视为二进制（与 Git 的检测策略一致）。
func DetectBinaryContent(content []byte) bool {
	// bytes.IndexByte 在 amd64 上内部用 AVX2 实现，性能足够。
	// 不做大小写处理，因为我们只找 0 字节。
	const max = 16 * 1024
	if len(content) > max {
		content = content[:max]
	}
	return bytes.IndexByte(content, 0) >= 0
}
