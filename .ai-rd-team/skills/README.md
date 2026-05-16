# skills/ —— fin-vault 项目级 Skills

本目录存放**项目级 Skill**，优先级最高（`workspace > global > builtin`）。
与 ai-rd-team builtin 同名的文件会**完全覆盖** builtin 版本（不是合并）。

## 已完成的 6 份 Skill（Phase 0 - Batch B ✅）

| 文件名 | 作用 | 与 builtin 关系 |
|---|---|---|
| `go-gin-development.md` | Go + Gin + GORM + Viper 开发规范，五层目录、UnitOfWork 事务 | **覆盖** builtin:`go-kratos-development`（项目用 Gin 而非 Kratos） |
| `naming-conventions.md` | 表 `t_fv_*` / 字段 `f_*` / 索引 `uk_*`/`idx_*` / Go 标识符 / 错误码区间 | 新增（builtin 无） |
| `decimal-precision.md` | 金额必须 `shopspring/decimal`，禁用 float / 自造定点 | 新增 |
| `llm-provider-pattern.md` | `LLMProvider` 接口 + go-openai Tool Calling ReAct 循环 | 新增 |
| `error-handling.md` | `pkg/errs` 错误码、Repo→Service→Handler 错误传递、Recovery | 新增 |
| `repository-pattern.md` | Repository 抽象、UnitOfWork 事务、Cache 装饰器、Mock 测试 | 新增 |

> Vue3 / 微信小程序 / pytest 等通用规范**直接复用 ai-rd-team builtin**，本目录不重复造。

## Skill 文件格式（ai-rd-team 规范）

来源：ai-rd-team `openspec/specs/design/05-roles-skills.md` §6.3。

- 单文件 ≤ 500 行（token 成本考虑）
- **纯文本无特殊语法，不写 frontmatter**（与 memory/agent.d/ 不同）
- 标题结构稳定：

  ```markdown
  # Skill 名称（与文件名一致）

  ## 适用场景
  ## 核心原则
  ## 常用模式
  ## 禁止
  ## 示例（可选）
  ```

- 不含可执行代码（纯文档；代码片段用 fenced code block 展示规范）

## 引用方式

角色配置中 `<scope>:<name>` 三段式：
- `workspace:go-gin-development` —— 强制只用项目级
- `go-gin-development` —— 不带 scope，按 `workspace > global > builtin` 优先级查找（**推荐**）

我们 `config.advanced.yaml` 全部使用**不带 scope 短名**（除了 `builtin:code-review-checklist` 显式锁内置）。

## 文件名为何不带 `fin-vault-` 前缀

scope 由 `.ai-rd-team/skills/` 这个路径决定（即 workspace scope），不需要文件名再带项目名前缀。
ai-rd-team SkillsLoader 是按文件名（去 `.md`）查找的，路径只用来分 scope。

## FAQ

**Q：团队启动报 `SkillNotFoundError: code-review-checklist` 怎么办？**

A：这是 ai-rd-team builtin skills 尚未实现时的已知风险。临时方案二选一：

1. 在 `config.advanced.yaml` 里把 `builtin:code-review-checklist` 删掉，让该角色仅依赖项目级 skill；
2. 在本目录新建一份占位 `code-review-checklist.md`（结构同 6 份），workspace 优先级覆盖即可。

**Q：要不要给 6 份 skill 写 frontmatter？**

A：**不要**。规范明确"纯文本无特殊语法"。`agent.d/memory/` 文件带 frontmatter 是另一套规范（memory 体系），别混淆。

**Q：单份 skill 写多长合适？**

A：300-450 行 / ~1000-1300 tokens 是当前预算。第一期是**全文注入到 prompt**，每角色注入 ≤ 3 份 skill 以遵守 ai-rd-team 注意力上限约束。

**Q：通用 Go 语法（如 goroutine 用法、context 取消）要不要写进来？**

A：**不要**。基础 LLM 已掌握。本目录只写 fin-vault 的**项目特有约束 + 反模式 + 可复制片段**。
