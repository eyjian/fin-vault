// Package llm — trpc-agent-go 依赖占位（§1.1 临时文件）。
//
// 议题 replace-ai-with-trpc-agent-go §3.1 包结构落地后，本文件应被删除：
// 真实代码（agent/runner.go、model/factory.go 等）会自然 import 这些子包，
// 届时 go mod tidy 会自动保留依赖，不再需要占位。
//
// DO NOT add real logic here. DO NOT import additional packages.
package llm

import (
	_ "trpc.group/trpc-go/trpc-agent-go/runner" // 占位：在 §3 真实代码落地前锁定依赖
)
