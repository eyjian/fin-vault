---
title: fin-vault × ai-rd-team 接入方案
version: v1.0
status: 已定稿（待执行）
last_updated: 2026-05-16
authors: 主理人 + AI 配对
---

# fin-vault × ai-rd-team 接入方案

> 本文档描述如何把 fin-vault 项目接入 [ai-rd-team](https://github.com/eyjian/ai-rd-team) 数字人研发团队框架，由 AI 团队（PM / 架构师 / Dev×N / Reviewer / Tester）协作产出代码。
>
> 这不是工作流编排。我们不写步骤，只交付**需求 + 规范**，让团队自己拆分和实现。

---

## 1. 为什么用 ai-rd-team

把"主理人 + AI 单兵"升级成"主理人 + AI 团队"，核心收益有三点：

1. **角色分工**：架构师做切分，开发者写代码，Reviewer 做检视，Tester 写测试 — 与单兵相比天然减少"自己审自己"的盲区。
2. **规范可注入**：fin-vault 的命名规范（`t_fv_` / `f_`）、金额必须 `decimal`、Provider 抽象等约束，写成 Skill 注入到每个成员的 Prompt，不再依赖人工 review 把关。
3. **成本透明**：用 RP（Resource Points）计量，每个里程碑可独立预算（默认 400 RP/次 ≈ Standard 档），超 75% 自动告警。

ai-rd-team 已有 5 次真实 CodeBuddy E2E 跑通记录（含一个 28 文件的 Go + Kratos 博客 API 示例），可信度足够第一阶段押注。

---

## 2. 关键决策（已锁定）

| 编号 | 决策 | 选项 | 理由 |
|------|------|------|------|
| D1 | 接入策略 | **B. 仓库内接入 + 反哺上游** | 主战场是 fin-vault 项目级 skill；但 `go-gin-basics` 是空白，反哺一个 PR 让其他 Go + Gin 用户也受益 |
| D2 | 任务粒度 | **A. 一次跑全部** | 让团队自主拆分，更接近"数字人团队"的设计意图；超预算/卡死再改批次 |
| D3 | Skill 数量 | **A. 全建 6 个** | 命名 / decimal / 错误处理 / Provider 抽象都是 fin-vault 强约束，少一个都会让产出走样 |
| D4 | 落地节奏 | **B. 先文档入档，再执行** | 与 v2.1 的"先架构后代码"节奏一致；onboarding 文档下次做小程序也能复用 |

---

## 3. ai-rd-team 关键概念速通

只列与 fin-vault 直接相关的部分。完整文档见 `ai-rd-team/docs/`。

### 3.1 三档运行

| 档位 | 默认成员 | 预算 | fin-vault 用法 |
|------|---------|------|---------------|
| Lite | developer × 1 | 120 RP | 不用 |
| **Standard** | architect + developer × 2 + tester | **400 RP** | ← **fin-vault 第一阶段全程用这一档** |
| Full | 7 角色全启 | 1500 RP | 微信小程序里程碑、或团队跑飞需要 reviewer 介入时升档 |

### 3.2 三层 Skill 加载

```
Skill 'fin-vault-naming' 加载顺序（高 → 低，同名覆盖）：
  1. workspace  → fin-vault/.ai-rd-team/skills/fin-vault-naming.md   ← 我们的位置
  2. global     → ~/.ai-rd-team/skills/fin-vault-naming.md
  3. builtin    → ai_rd_team/skills/builtin/fin-vault-naming.md
```

引用语法（写在 `config.advanced.yaml`）：

```yaml
roles:
  developer:
    skills:
      - "fin-vault-naming"           # 走三层优先级
      - "builtin:code-review-checklist"  # 强制走 builtin
```

### 3.3 三层 Memory（启动注入）

```
.ai-rd-team/memory/
├── agent.d/        # 启动时注入到所有成员 Prompt（适合"团队公约"）  ← 我们用这一层
├── memory.d/       # 按需加载（适合"老旧背景"）
└── decisions/      # 决策记录（适合"为什么这么选"）
```

### 3.4 三个核心约束（必须遵守，否则 skill 不生效）

> 这三条来自 ai-rd-team `docs/04-skills.md`，是踩过坑的硬约束。

1. **单 Skill ≤ 2000 字**：超过会稀释 Prompt 注意力
2. **单角色 ≤ 2-3 个 Skill**：超过会让成员"哪条都记不牢"
3. **必须"正面示例 + 禁止清单"**：只写"要 X"不够，必须配"不要 Y"

第 2 条尤其关键 — 这影响下面 §6 的角色 → skill 映射设计。

---

## 4. fin-vault 接入目录结构

```
fin-vault/
├── REQUIREMENT.md                          # ← 给 AI 团队的总需求（拆自现有 docs/）
├── .ai-rd-team/
│   ├── config.yaml                         # 档位 standard / 预算 400 RP
│   ├── config.advanced.yaml                # skill 到角色的映射（关键）
│   ├── skills/                             # 6 份项目级 skill
│   │   ├── fin-vault-go-gin.md
│   │   ├── fin-vault-naming.md
│   │   ├── fin-vault-decimal.md
│   │   ├── fin-vault-llm-provider.md
│   │   ├── fin-vault-error-handling.md
│   │   └── fin-vault-vue3-frontend.md
│   └── memory/agent.d/                     # 4 份启动注入记忆
│       ├── tech-stack-selected.md
│       ├── architecture-summary.md
│       ├── domain-model-summary.md
│       └── api-contracts.md
├── backend/                                # 团队产出落这里
├── frontend/                               # 团队产出落这里
└── docs/
    ├── architecture-design.md              # 已有
    ├── domain-model.md                     # 已有
    ├── database-schema.md                  # 已有
    ├── upgrade-guide.md                    # 已有
    └── ai-rd-team-onboarding.md            # 本文档
```

**`.ai-rd-team/runtime/`** 是引擎运行期产物（adapter-intents / state / reports），**应进入 `.gitignore`**。

---

## 5. 6 份项目级 Skill 的写作要点

每份 skill 的最终文件 ≤ 2000 字，结构遵循 ai-rd-team `docs/04-skills.md` 推荐：

```
# <skill 名>
## 适用场景
## 核心原则
## 常用模式（带正面代码示例）
## 禁止（负面清单 — 关键）
## 参考
```

下面只列**每份 skill 的关键约束点**，正式动笔在执行阶段。

### 5.1 `fin-vault-go-gin.md`

> **目标**：替代 builtin 的 `go-kratos-basics`，约束 Gin + GORM + 四层分层。

核心原则：
- 四层分层：`api/` (HTTP handler) → `service/` (业务) → `repo/` (存储) → `model/` (DTO/PO)
- handler 不能直接 import GORM（必须经 repo 接口）
- 依赖装配走 `bootstrap.Wire(cfg)`，禁止包级 init / 全局变量
- 配置走 Viper，禁止硬编码

常用模式（带代码示例）：
- Handler 标准签名 `func (h *AccountHandler) Create(c *gin.Context)`
- Service 接口定义在 service 包，实现在 service 包内
- Repo 接口定义在 service 层（依赖倒置），实现在 repo 包

禁止：
- ❌ handler 里写业务逻辑
- ❌ service 直接调 `gorm.DB`
- ❌ 用全局变量传 DB / Logger
- ❌ panic 替代 error
- ❌ 忽略 err（`_ = svc.Do()`）

### 5.2 `fin-vault-naming.md`

> **目标**：约束所有数据库 / 文件 / 包命名规范。

核心原则：
- 表名：`t_fv_<module>_<name>`（如 `t_fv_acct_account`）
- 字段名：`f_<name>`（如 `f_id` / `f_name` / `f_created_at`）
- 唯一索引：`uk_<table>_<col>`；普通索引：`idx_<table>_<col>`
- Go 包名：单数小写，无下划线（`account` 不是 `accounts` / `account_pkg`）
- 文件名：`account_handler.go` / `account_service.go` / `account_repo.go`

常用模式：

```sql
-- 正确
CREATE TABLE t_fv_acct_account (
  f_id BIGINT PRIMARY KEY,
  f_user_id BIGINT NOT NULL,
  f_name VARCHAR(64) NOT NULL,
  f_created_at TIMESTAMP NOT NULL,
  CONSTRAINT uk_t_fv_acct_account_user_name UNIQUE (f_user_id, f_name)
);
CREATE INDEX idx_t_fv_acct_account_user ON t_fv_acct_account(f_user_id);
```

禁止：
- ❌ 表名不带 `t_fv_` 前缀
- ❌ 字段名不带 `f_` 前缀
- ❌ 用 `accounts`（复数）作为表名或包名
- ❌ 索引名不带 `uk_` / `idx_` 前缀

### 5.3 `fin-vault-decimal.md`

> **目标**：所有金额字段必须用 `shopspring/decimal`，杜绝 float 精度坑。

核心原则：
- 所有"钱"相关字段（金额 / 单价 / 汇率 / 比例）的 Go 类型必须是 `decimal.Decimal`
- DB 列类型必须是 `DECIMAL(20, 8)`（20 位精度，8 位小数）
- 计算前必须用 `decimal.NewFromString` 或 `decimal.NewFromInt`，禁止从 `float64` 转
- 比较用 `.Equal()` / `.Cmp()`，禁止 `==`

常用模式：

```go
import "github.com/shopspring/decimal"

// 正确
amount, err := decimal.NewFromString("1234.5678")
total := amount.Mul(quantity).Round(8)

// JSON 序列化保持字符串
type Trade struct {
    Amount decimal.Decimal `json:"amount" gorm:"type:decimal(20,8)"`
}
```

禁止：
- ❌ 用 `float64` / `float32` 存金额
- ❌ `decimal.NewFromFloat(...)`（精度已损失）
- ❌ `a == b` 比较两个 decimal
- ❌ DB 列用 `DOUBLE` / `FLOAT`

### 5.4 `fin-vault-llm-provider.md`

> **目标**：约束 LLM 接入必须走 `LLMProvider` 抽象，禁止业务代码直接调 `go-openai`。

核心原则：
- 业务代码只依赖 `service.LLMProvider` 接口（位于 `internal/service/llm_provider.go`）
- 具体实现（OpenAI / Claude / 通义）放在 `internal/llm/<vendor>/` 子目录
- Tool Calling 循环写在 Service 层（不在 LLMProvider 实现里），方便未来切 Eino
- Token 用量必须返回 `TokenUsage`，由 Service 层记录到 `t_fv_llm_call_log`

常用模式：

```go
// 接口定义（service 层）
type LLMProvider interface {
    Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
    ChatWithTools(ctx context.Context, req ChatRequest, tools []Tool) (*ChatResponse, error)
}

// Service 层 Tool Calling 循环
func (s *AdvisorService) Ask(ctx context.Context, q string) (string, error) {
    msgs := []Message{{Role: "user", Content: q}}
    for i := 0; i < maxIter; i++ {
        resp, err := s.llm.ChatWithTools(ctx, ChatRequest{Messages: msgs}, s.tools)
        if err != nil { return "", err }
        if len(resp.ToolCalls) == 0 { return resp.Content, nil }
        // 执行工具，append 结果回 msgs
    }
    return "", errors.New("tool calling exceeded max iterations")
}
```

禁止：
- ❌ 业务代码 `import "github.com/sashabaranov/go-openai"`
- ❌ 在 LLMProvider 实现里写 Tool Calling 循环
- ❌ 把 API Key 写死在代码里（必须走配置 + 环境变量）
- ❌ 不记录 TokenUsage 直接返回

### 5.5 `fin-vault-error-handling.md`

> **目标**：约束错误码、日志、context 传递。

核心原则：
- 错误用 `errors.New` / `fmt.Errorf` + `%w` 包装，禁止 panic
- 业务错误码：`ErrCode` 枚举（如 `ErrAccountNotFound = 1001`），通过 `apperror.New(code, msg)` 抛出
- 日志用标准库 `slog`，结构化字段，禁止 `fmt.Println`
- 所有跨层调用必须传 `ctx context.Context` 作为第一个参数
- HTTP 响应统一格式 `{code, message, data}`，由 middleware 包装

常用模式：

```go
// 错误定义
var ErrAccountNotFound = apperror.New(1001, "account not found")

// repo 层抛错
func (r *accountRepo) Get(ctx context.Context, id int64) (*Account, error) {
    var po Account
    if err := r.db.WithContext(ctx).First(&po, id).Error; err != nil {
        if errors.Is(err, gorm.ErrRecordNotFound) {
            return nil, ErrAccountNotFound
        }
        return nil, fmt.Errorf("query account: %w", err)
    }
    return &po, nil
}

// 日志
slog.InfoContext(ctx, "account created",
    "user_id", userID,
    "account_id", acct.ID)
```

禁止：
- ❌ `panic` / `log.Fatal`（除了 main 启动失败）
- ❌ `fmt.Println` / `log.Println`（必须用 slog）
- ❌ 函数不接 ctx 直接做 DB / HTTP / LLM 调用
- ❌ `return errors.New("xxx")` 不带错误码（业务错误必须有 code）
- ❌ 用字符串比对错误（必须 `errors.Is` / `errors.As`）

### 5.6 `fin-vault-vue3-frontend.md`

> **目标**：补充 builtin `vue3-basics` 没覆盖的 fin-vault 前端约定。

核心原则：
- 状态管理用 Pinia（一个领域一个 store，文件 `stores/<domain>.ts`）
- UI 库统一 Element Plus，禁止混用 Ant Design / Naive UI
- HTTP 客户端封装在 `api/<domain>.ts`，统一处理错误码
- 路由懒加载（`() => import('@/views/...')`）
- 金额展示统一用工具函数 `formatAmount(decimal, currency)`，禁止前端做金额计算

常用模式：

```typescript
// stores/account.ts
import { defineStore } from 'pinia'
import { listAccounts } from '@/api/account'

export const useAccountStore = defineStore('account', {
  state: () => ({ items: [] as Account[] }),
  actions: {
    async fetch() {
      this.items = await listAccounts()
    }
  }
})
```

禁止：
- ❌ 在组件里直接 `axios.get(...)`（必须走 `api/<domain>.ts`）
- ❌ 在前端用 `Number(amount) * price` 做金额计算（金额计算必须后端）
- ❌ 用 Vuex（已弃用，统一 Pinia）
- ❌ 路由不懒加载（首屏会很大）

---

## 6. 角色 → Skill 映射（关键约束：每角色 ≤ 3 个）

> ai-rd-team `docs/04-skills.md` 明确"单角色建议 ≤ 2-3 个 Skill，更多会稀释注意力"。下面映射严格遵守。

| 角色 | 数量 | 注入的 Skill | 设计依据 |
|------|------|--------------|---------|
| **architect** | 3 | `builtin:code-review-checklist` + `fin-vault-go-gin` + `fin-vault-naming` | 架构师负责切分、定接口、定命名 |
| **developer** | 3 | `fin-vault-go-gin` + `fin-vault-naming` + `fin-vault-decimal` | 后端开发的高频规范，Provider/error 由 reviewer 兜底 |
| **developer-frontend**（同 developer，但前端角色另起一个实例） | 2 | `builtin:vue3-basics` + `fin-vault-vue3-frontend` | 前端规范聚焦 |
| **reviewer** | 3 | `builtin:code-review-checklist` + `fin-vault-error-handling` + `fin-vault-llm-provider` | reviewer 兜底 dev 没注入的两个高阶约束 |
| **tester** | 2 | `fin-vault-go-gin`（了解分层结构）+ `fin-vault-decimal`（金额测试用例） | tester 不需要看命名规范（看代码即可识别） |
| pm / analyst / devops | 0 | - | 这几个角色不直接产代码，靠 memory/agent.d 拿背景就够 |

> ⚠️ **注意覆盖默认行为**：ai-rd-team 默认会给 developer 挂 `python-best-practices` + `pytest-guide`，给 reviewer 挂 `code-review-checklist + python-best-practices`。我们必须**显式覆盖整个 skills 列表**，否则 Python 规范会和 Go 规范同时注入造成矛盾。

---

## 7. 配置文件设计

### 7.1 `.ai-rd-team/config.yaml`

```yaml
config_version: "1.0"

project:
  description: "fin-vault - AI 个人财务管家（Go + Vue3 单机部署）"

run_mode: "standard"

tech_stack:
  backend: "go"
  frontend: "vue3"
  mobile: null            # 第二阶段再做小程序

budget:
  per_run: 400
  per_day: 1500
```

### 7.2 `.ai-rd-team/config.advanced.yaml`

```yaml
adapter:
  bridge_timeout_seconds: 300

# 每个角色的 skills 列表是"全量替换"语义，不是"追加"
# 显式列出所有要的 skill，覆盖框架默认（避免混入 Python 规范）
roles:
  architect:
    skills:
      - "builtin:code-review-checklist"
      - "fin-vault-go-gin"
      - "fin-vault-naming"

  developer:
    skills:
      - "fin-vault-go-gin"
      - "fin-vault-naming"
      - "fin-vault-decimal"

  reviewer:
    skills:
      - "builtin:code-review-checklist"
      - "fin-vault-error-handling"
      - "fin-vault-llm-provider"

  tester:
    skills:
      - "fin-vault-go-gin"
      - "fin-vault-decimal"
```

> 💡 前端 skill 暂不在第一里程碑注入。第一里程碑只跑后端骨架（见 §9）。前端进场时再追加一个 `developer-frontend` 角色或临时切 skill。

---

## 8. Memory（agent.d/）设计

每份 ≤ 500 token，写最关键的"团队公约"。

### 8.1 `tech-stack-selected.md`

精简自 [docs/architecture-design.md](architecture-design.md) v2.1 的"修正后完整技术栈"表，13 个核心库 + 暂不引入清单。

### 8.2 `architecture-summary.md`

精简自 [docs/architecture-design.md](architecture-design.md) 的：
- 四层分层图
- LLMProvider / IDGenerator / Migrator / ReportExporter 4 个核心接口签名
- bootstrap.Wire(cfg) 装配模式

### 8.3 `domain-model-summary.md`

精简自 [docs/domain-model.md](domain-model.md)，列出 8+ 个核心实体的：
- 名称 + 一句话职责
- 关键字段（不含全部）
- 跨实体的事务边界（如"交易流水必须和持仓更新同事务"）

### 8.4 `api-contracts.md`

精简自 [docs/architecture-design.md](architecture-design.md) 的 REST API 章节，列出：
- URL prefix / 错误码格式
- 关键端点表（路径 / 方法 / 一句话说明）
- 鉴权方式（第一阶段：本地无鉴权 / 第二阶段：JWT）

---

## 9. 执行计划

### 9.1 Phase 0：项目接入（不跑团队，只搭配置）

1. 在 fin-vault 仓库创建 `.ai-rd-team/` 目录结构（§4）
2. 写 6 份 skill（§5），每份 ≤ 2000 字，含正面示例 + 禁止清单
3. 写 4 份 agent.d memory（§8），每份 ≤ 500 token
4. 写 `config.yaml` + `config.advanced.yaml`（§7）
5. 写 `REQUIREMENT.md`（项目根，作为团队的入口）
6. 把 `.ai-rd-team/runtime/` 加入 `.gitignore`
7. 跑 `ai-rd-team config validate` 校验配置无误
8. commit & push

### 9.2 Phase 1：第一次跑团队（冒烟）

> 决策 D2 选了"一次跑全部"，但第一次跑前需要降低风险：先做一次"骨架冒烟"，跑通了再追加完整需求。

```bash
cd /data/workspace/github/eyjian/fin-vault

# 启动 Web 面板（127.0.0.1:8765）
ai-rd-team serve --port 8765 &

# 在 CodeBuddy 会话中：
ai-rd-team run "$(cat REQUIREMENT.md)"
```

观察点：
- [ ] 6 份 skill 是否都被加载（看 `runtime/adapter-intents/*.json` 的 `# Skills` 段）
- [ ] memory 是否被注入（看 `# 记忆` 段）
- [ ] 产出代码的命名是否遵守 `t_fv_` / `f_` / `uk_` / `idx_`
- [ ] 金额字段是否用了 `decimal.Decimal`
- [ ] handler 是否没 import gorm

### 9.3 Phase 2：反哺上游

> 决策 D1 选了 B（接入 + 反哺）。fin-vault 跑通后，把 `fin-vault-go-gin.md` **泛化**为通用的 `go-gin-basics.md`（去掉 fin-vault 业务约束，只留 Gin + GORM + 四层分层），提 PR 给 ai-rd-team。

PR 改动清单（基于 ai-rd-team `docs/04-skills.md` 的"贡献回内置"指引）：

1. 新增 `src/ai_rd_team/skills/builtin/go-gin-basics.md`（通用化版本）
2. 更新 `docs/04-skills.md` 的"内置 Skills（6 个）"表 → 改为 7 个
3. 更新 `README.md` 的内置 skill 数（如有）
4. 不需要改 `__init__.py`（实测是空的，文件名约定即可）

PR 标题建议：`feat(skills): 新增 go-gin-basics 内置 skill`

PR 描述要点：
- 动机：现有 go-kratos-basics 只覆盖 Kratos，Gin 是 Go 后端最广泛的选择，缺一份会让用户卡壳
- 实测来源：fin-vault 项目，已有 X 个文件产出
- 与 go-kratos-basics 的区别：分层名称（biz/data/service vs api/service/repo/model）+ 不依赖 proto + 不依赖 wire

### 9.4 Phase 3：第二阶段（小程序）

> 等 fin-vault 第一阶段稳定后再做。

- 注入 `builtin:wxmini-basics`
- 新增一份 `fin-vault-wxmini.md`（fin-vault 业务约束）
- 升档到 Full（加 reviewer + devops）

---

## 10. 风险与对策

| 风险 | 概率 | 影响 | 对策 |
|------|------|------|------|
| 团队跑飞，超 400 RP 没产出 | 中 | 高 | Phase 1 先做骨架冒烟，确认 skill/memory 加载正确再放大 |
| Python skill 默认值未被覆盖，导致 Go + Python 规范冲突 | 中 | 高 | 在 §6 强调"显式列出整个 skills 列表覆盖默认"，并在 Phase 1 第一步检查 Prompt |
| Skill 写得太空，成员不遵守 | 中 | 中 | 严格遵守"正面示例 + 禁止清单"模板，先在 reviewer skill 里压"金额必须 decimal"作为兜底 |
| Bridge 超时（go-openai / Tool Calling 慢） | 低 | 中 | `bridge_timeout_seconds: 300`（已配置），不够再加到 600 |
| 反哺 PR 被拒 | 低 | 低 | 即便被拒，fin-vault 项目级 skill 不受影响，可继续用 |
| 团队产出代码与 docs/architecture-design.md v2.1 偏离 | 中 | 高 | 把 architecture-summary.md（agent.d）写得足够具体，且 reviewer 角色注入 `code-review-checklist` 兜底 |

---

## 11. 验收标准

第一次跑团队（Phase 1）完成后，达成以下任一条件即视为成功：

**最低标准**（验证 ai-rd-team 接入本身可行）：
- [ ] `runtime/adapter-intents/*.json` 中能看到 fin-vault 的 6 份 skill 内容
- [ ] 4 份 memory 出现在 Prompt 的"# 记忆"段
- [ ] 团队产出至少 1 个 .go 文件且符合分层规范

**理想标准**（验证团队真能干活）：
- [ ] 产出后端骨架（main.go / bootstrap / config / 一个 CRUD demo）
- [ ] `go build ./...` 通过
- [ ] 命名规范 100% 遵守（grep 验证 `t_fv_` / `f_` 前缀）
- [ ] 至少一个金额字段用了 `decimal.Decimal`
- [ ] 总消耗 ≤ 400 RP

---

## 12. 参考

- ai-rd-team 仓库：https://github.com/eyjian/ai-rd-team
- 关键文档：
  - `docs/01-getting-started.md` — 安装与三种启动方式
  - `docs/04-skills.md` — Skills 完整指南（**写 skill 必读**）
  - `examples/02-blog-api/` — Go + Kratos 示例（fin-vault 接入直接借鉴）
  - `examples/04-custom-skill/` — 自定义 skill walkthrough
- fin-vault 关联文档：
  - [docs/architecture-design.md](architecture-design.md) — 架构设计 v2.1（agent.d memory 来源）
  - [docs/domain-model.md](domain-model.md) — 领域模型
  - [docs/database-schema.md](database-schema.md) — 建表 SQL（命名规范来源）
  - [docs/upgrade-guide.md](upgrade-guide.md) — 升级路线（第二阶段触发条件）
