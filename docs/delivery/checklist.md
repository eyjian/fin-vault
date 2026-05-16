# FinVault 第一阶段交付清单（M1）

> 交付日期：2026-05-16
> 团队：ai-rd-team-aa691137（架构师 + 2 开发 + 测试）
> 运行档位：standard

---

## 1. 交付总览

| 维度 | 数量 / 状态 |
|------|------------|
| 后端 Go 文件 | 94 |
| 后端测试文件 | 9（103 用例全 PASS） |
| 前端 Vue 文件 | 35（11 个 View + 通用组件 + API + Store） |
| 设计文档 | 2（ARCHITECTURE.md / data-interfaces.yaml） |
| 编译状态 | `go build ./...` ✅ 通过 |
| 测试状态 | `go test ./...` ✅ 全 PASS |

---

## 2. 用户强调的"必交付"页面（Vue 3 + Element Plus）

| # | 页面 | 文件 | 功能 |
|---|------|------|------|
| 1 | **基金管理** | `frontend/src/views/asset/FundManage.vue` | 基金 CRUD + 列表筛选（代码/类型/经理/公司）+ 净值刷新 + 申购/赎回/分红流水 |
| 2 | **股票管理** | `frontend/src/views/asset/StockManage.vue` | 股票 CRUD + 筛选（代码/SH/SZ/HK/US/BJ 市场/行业）+ 行情刷新 + 5 种交易流水 |
| 3 | **理财产品管理** | `frontend/src/views/asset/WealthManage.vue` | 理财产品 CRUD + 起息日/到期日/预期年化/起购金额/赎回规则/自动续期 + 即将到期标记 |
| 4 | 现金管理 | `frontend/src/views/asset/CashManage.vue` | 多平台多币种现金账户管理 |
| 5 | 持仓总览 | `frontend/src/views/Dashboard.vue` + `HoldingView.vue` | 按类型/平台/币种聚合 + 盈亏卡片 |
| 6 | 交易流水 | `frontend/src/views/transaction/TransactionList.vue` | 13 种交易类型筛选 + 录入对话框 |
| 7 | AI 对话 | `frontend/src/views/AIChat.vue` | SSE 流式输出，4 场景（自由问答/盈亏分析/买卖建议/持仓建议） |
| 8 | 行情/汇率/导出 | `QuoteManage.vue` / `RateManage.vue` / `ExportPage.vue` | 辅助管理页 |

---

## 3. 后端模块分布

```
backend/
├── cmd/api/main.go                # 入口（仅 wire 组装）
├── configs/config.yaml            # 多源配置
├── go.mod / go.sum                # 锁定 v2.1 技术栈
├── internal/
│   ├── domain/                    # 实体 + 13 种 TxnType + 枚举（零业务依赖）
│   ├── repository/
│   │   ├── interfaces.go          # 7 个 Repository 接口 + UnitOfWork
│   │   └── gormimpl/              # GORM 实现 + Tx 包装
│   ├── cache/                     # CacheProvider 接口 + sync.Map+TTL 本地实现
│   ├── service/                   # asset/holding/transaction/quote/rate/wealth/chat/advisor/analysis/export/mature
│   ├── handler/                   # Gin handler（DTO + 三段式）
│   ├── llm/                       # LLMProvider 接口 + OpenAIProvider（DeepSeek/GLM/Kimi/通义/Ollama 仅切 baseURL）
│   ├── llm/tools/                 # 5 个 Tool Calling 工具（持仓/行情/盈亏/历史/平台聚合）
│   ├── platformapi/               # 东方财富/新浪/腾讯行情聚合（ants 协程池 + 多源降级）
│   ├── report/                    # Excel + Markdown 导出
│   ├── middleware/                # RequestID/Logger/Recovery/CORS
│   └── bootstrap/                 # config / db / migrate / cache / cron / wire / router / seed / logger
└── pkg/
    ├── errs/                      # 业务错误码（按区间 10000/30000/40000/50000/90000）
    └── utils/{response,decimalx}/ # 统一响应 + decimal 工具
```

### 第一阶段 9 大能力覆盖

| # | 能力 | 落地 |
|---|------|------|
| 1 | 资产记录（基金/股票/理财/现金，多平台多币种） | domain + repository + asset/wealth service & handler |
| 2 | 持仓视图（实时市值、盈亏、按平台/类型/币种聚合） | holding service + Dashboard + HoldingView |
| 3 | 13 种交易流水 | TxnType 枚举 + transaction service & handler |
| 4 | 行情接入（手动 + 东方财富/新浪/腾讯 公开 API） | platformapi/ + quote service |
| 5 | 多币种折算（ExchangeRate 历史汇率） | rate service + ConvertToCNY |
| 6 | 理财到期定时任务（每天 00:30 扫描） | mature service + cron/v3 调度（bootstrap/cron.go） |
| 7 | AI 对话（Tool Calling + SSE，4 场景） | chat/advisor/analysis service + ai_meta/chat handler |
| 8 | 多模型切换（DeepSeek/GLM/Kimi/通义/Ollama） | LLMRegistry 按 provider name 路由 |
| 9 | 数据导出（Excel + Markdown） | report/ + export service |

---

## 4. 测试覆盖

103 用例全 PASS，0 FAIL，0 SKIP。

| 模块 | 用例数 | 覆盖率 |
|------|--------|--------|
| pkg/utils/decimalx | 12 | 100% |
| internal/llm | 12 | 32.1% |
| internal/platformapi | 18+19 | 40.7% |
| internal/service（rate/quote/mature/ai/export） | 7+5+8+18+13 | 23.8% |

需求覆盖：#1 金额精度 / #4 理财到期 / #5 多币种 / #6 LLMRegistry / #9 数据导出 已 ✅，
#2 13 种 txn_type / #3 持仓重算 / #7 资产去重 / #8 GORM Repository 已具备代码，待第二轮 sqlmock 测试补齐。

---

## 5. 设计文档

- `docs/design/ARCHITECTURE.md` —— 最终落地架构 + 目录结构 + 模块依赖图
- `docs/design/data-interfaces.yaml` —— HTTP API 接口契约（资产/持仓/交易/行情/汇率/AI/导出）
- `.ai-rd-team/runtime/data-task-breakdown.yaml` —— D1/D2 并行任务拆分
- `.ai-rd-team/runtime/reports/report-{architect,developer,tester}.md` —— 三份过程报告

---

## 6. 规范合规检查

| 规范 | 状态 |
|------|------|
| 五层目录铁律（handler→service→repository 接口→gorm 实现） | ✅ |
| service 包不 import gorm/redis/openai | ✅（grep 0 命中） |
| 金额一律 decimal.Decimal，禁 float64 | ✅ |
| 表名 t_fv_{module}_{name}，字段 f_xxx | ✅ |
| 错误码区间合规（10000/30000/40000/50000/90000） | ✅ |
| context 全链路传递 | ✅ |
| 配置只在 bootstrap.LoadConfig 读 | ✅ |
| API 路径 /api/v1/{resource} 复数小写 | ✅ |
| 项目级 skill 优先级最高（无 Python 规范出现） | ✅ |

---

## 7. 本地启动指南

### 后端
```bash
cd backend
go mod tidy
go test ./...                # 应全 PASS
go run ./cmd/api             # 默认读 configs/config.yaml + FV_ 前缀环境变量
```

### 前端
```bash
cd frontend
npm install                  # 沙箱中未执行（网络受限），用户本地需跑
npm run dev                  # 默认连 http://localhost:8080/api/v1
```

### LLM 配置
`configs/config.yaml` 中所有 provider 的 `api_key` 留空即可；运行时按需在 `FV_LLM_DEEPSEEK_API_KEY` 等环境变量注入。
未配 key 的 provider 会被 LLMRegistry 自动跳过；AI 接口在无可用 provider 时返回 50002 错误码。

---

## 8. 已知遗留 / 后续

1. **前端 npm install 未在沙箱中跑**：源码完整，本地拉下来即可。
2. **dev_1 部分 service（asset/holding/transaction）的单测**：留给 tester 第二轮（用 mock repo + sqlmock）。
3. **gin httptest e2e 端到端**：第二阶段。
4. **PDF 导出**：第一阶段不做，由前端浏览器打印解决（`docs/upgrade-guide.md`）。
5. **报表生成（周/月/年报）**：第二阶段。

---

> 本清单由 ai-rd-team 主 Agent 在团队产出基础上整理；详细过程见 `.ai-rd-team/runtime/reports/`。
