# skills/ —— fin-vault 项目级 Skills

本目录存放**项目级 Skill**，优先级最高（`workspace > global > builtin`）。
与 ai-rd-team builtin 同名的文件会**完全覆盖** builtin 版本（不是合并）。

## 计划中的 6 份 Skill（Phase 0 - Batch B 开发）

| 文件名 | 作用 | 与 builtin 关系 |
|---|---|---|
| `go-gin-development.md` | Go + Gin + GORM + Viper 开发规范 | **覆盖** builtin:`go-kratos-backend`（项目用 Gin 而非 Kratos） |
| `naming-conventions.md` | 表名 `t_fv_*` / 字段 `f_*` / 索引 `uk_*`/`idx_*` 强制规范 | 新增（builtin 无） |
| `decimal-precision.md` | 金额必须 `shopspring/decimal`，禁用 float / 自造定点 | 新增 |
| `llm-provider-pattern.md` | `LLMProvider` 接口 + go-openai Tool Calling 循环写法 | 新增 |
| `error-handling.md` | 业务错误码、Service/Handler 错误传递、Gin Recovery | 新增 |
| `repository-pattern.md` | Repository 抽象、GORM 实现禁泄漏到 service | 新增 |

> Vue3 / 微信小程序 / pytest 等通用规范**直接复用 ai-rd-team builtin**，本目录不重复造。

## Skill 文件格式

参见 ai-rd-team `openspec/specs/design/05-roles-skills.md` §6.3：

- 单文件 ≤ 500 行
- 标题结构稳定（## 适用场景 / ## 核心原则 / ## 常用模式 / ## 禁止 / ## 示例）
- 纯 Markdown，不含可执行代码
- 估算 token 写在 frontmatter（可选）

## 引用方式

角色配置中 `<scope>:<name>` 三段式：
- `workspace:go-gin-development` —— 强制只用项目级
- `go-gin-development` —— 不带 scope，按 workspace > global > builtin 优先级查找（推荐）
