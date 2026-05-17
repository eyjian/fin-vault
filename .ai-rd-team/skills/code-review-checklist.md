# 代码评审清单（fin-vault 项目级 · workspace 占位）

> 本 Skill 是 `builtin:code-review-checklist` 的 **workspace 兜底版本**，用于 ai-rd-team builtin 实现尚未就绪时避免 `SkillNotFoundError`。
> 一旦 builtin 上线且经过验证，可在 [config.advanced.yaml](../config.advanced.yaml) 把引用换回 `builtin:code-review-checklist` 并删除本文件。

## 适用场景

- reviewer 角色拿到 PR / commit / patch 时，按本清单逐项过
- architect 在合并前的最终把关
- 任何角色自检"我提交前是否漏了点什么"

## 一、强制项（任一不通过即打回，不留情）

### F1 金额精度
- [ ] 涉及金额、汇率、价格的字段**未出现** `float32` / `float64`
- [ ] 全部使用 `github.com/shopspring/decimal` 的 `decimal.Decimal`
- [ ] DB 字段类型是 `DECIMAL(20,8)` 或更高精度（见 `docs/database-schema.md`）
- [ ] 序列化到 JSON 时使用字符串而非数字（避免前端 JS Number 精度丢失）

### F2 分层依赖
- [ ] `internal/service/` 下**未** import：`gorm.io/gorm`、`github.com/redis/go-redis`、`github.com/sashabaranov/go-openai`、`trpc.group/trpc-go/trpc-agent-go`
- [ ] service 仅依赖 `internal/repository`、`internal/cache`、`internal/llm` 三个抽象层
- [ ] handler 不直接 import repository（必须经过 service）

### F3 命名规范
- [ ] 新增表名匹配 `t_fv_{module}_{name}` 正则
- [ ] 新增字段以 `f_` 开头
- [ ] Go struct tag 中 `gorm:"column:f_xxx"` 与 DB 字段一致
- [ ] 错误码落在 `naming-conventions.md` 划分的区间内

### F4 依赖引入
- [ ] 引入新的 `go.mod` 依赖前，`.ai-rd-team/memory/decisions/` 下有对应 ADR
- [ ] ADR 论证了 YAGNI / 替代方案 / 维护成本

### F5 不做清单
- [ ] 改动未触碰 [REQUIREMENT.md](../../REQUIREMENT.md) §3 列出的"不做项"

### F6 / F7 文档锁
- [ ] 未修改 `REQUIREMENT.md` / `docs/*.md` / `.ai-rd-team/memory/agent.d/*`
- [ ] 若涉及未归档 OpenSpec 议题（`openspec/changes/<id>/`），有 architect + 主理人双签证据

## 二、强烈建议项（不通过给评论但不直接打回）

### 错误处理
- [ ] 业务错误统一走 `pkg/errs`，未直接返回 `errors.New("...")` 中文字符串
- [ ] 错误链使用 `fmt.Errorf("...: %w", err)` 包裹，未丢失底层 cause
- [ ] handler 层将 `*errs.AppError` 映射到 HTTP 状态码与统一响应结构

### 日志与可观测性
- [ ] 关键路径（金额计算、AI 调用、外部 API）有结构化日志（zap / slog）
- [ ] 日志中**不出现**密码、token、API key 等敏感字段
- [ ] LLM 调用记录 provider / model / token 用量 / 耗时

### 测试
- [ ] 涉及金额的函数有单测，且覆盖边界（0、负数、极大值、精度边界）
- [ ] repository 层的新方法至少有一个集成测试（用 sqlite in-memory 或 testcontainers）
- [ ] mock 用 `gomock` 或接口手写，避免对具体实现打 patch

### Provider 抽象（LLM）
- [ ] 新增 LLM 能力先在 `internal/llm` 定义接口，再加 provider 实现
- [ ] 走配置驱动（`config.yaml` 的 `llm.providers[]`），不硬编码 endpoint / model
- [ ] tool calling 失败、超时、限流有明确的降级或重试策略

## 三、提示项（仅供参考）

- [ ] 函数 ≤ 80 行；超过考虑拆分
- [ ] 单文件 ≤ 500 行；超过考虑按职责拆包
- [ ] 公开符号（首字母大写）有 godoc 注释
- [ ] 新增公共接口有使用示例（在测试或 doc.go 里）

## 四、特殊场景红线

| 场景 | 红线 |
|---|---|
| AI / Agent 改动 | 必须看 `openspec/changes/replace-ai-with-trpc-agent-go/` 当前 task 是否覆盖此变更 |
| 数据库迁移 | 必须有 up + down 脚本；禁止直接改老迁移文件 |
| 配置项新增 | `config.yaml.example` / `config.yaml` 同步更新；敏感项走环境变量 |
| 第三方 API | 必须有超时、重试上限、熔断（或显式说明为什么不需要） |

## 五、Review 输出格式建议

```
✅ 通过项：F1 金额、F3 命名、测试覆盖
⚠️  建议项：日志缺少 LLM token 用量字段（见 §二·日志）
❌ 阻塞项：service/portfolio_service.go 直接 import gorm（违反 F2）
```

> 本占位文件由 launcher 在 2026-05-17 创建，作为 builtin 就绪前的兜底。
> 维护者：主理人 / architect；其他角色仅读。
