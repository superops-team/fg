#!/usr/bin/env bash
# 压力测试：大目录 + 多 goroutine 并发扫描/搜索
set -e
cd "$(dirname "$0")/.."
echo "=== fg pressure test ==="
# 用 go test -race -count=3 保证 3 轮稳定通过
go test -race -count=3 ./... 2>&1 | tail -30
echo "=== bench: bigram/picker/grep ==="
go test -bench=. -benchmem -run=^$ ./bigram ./grep ./picker 2>&1 | tail -40
