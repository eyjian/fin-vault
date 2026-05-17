// Package agent 定义业务侧 Agent 运行时的抽象接口。
//
// 本包是 service / handler 层访问 Agent 能力的唯一入口；具体实现（包装
// trpc-agent-go 的 Runner / Session / Tool）位于本包内 *_trpc.go 文件，
// 由 §5.2 落地。
//
// 业务代码绝不直接 import trpc-agent-go（铁律 F2 / design.md D8）。
package agent
