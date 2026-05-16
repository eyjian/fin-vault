---
type: memory
layer: agent.d
author: manual
created: 2026-05-16T18:30:00+08:00
updated: 2026-05-16T18:30:00+08:00
related:
  - REQUIREMENT.md
  - docs/architecture-design.md
tags: [project, overview, scope]
estimated_tokens: 280
---

# 项目总览（fin-vault）

## 一句话

**本地起步、升级改动最小**的个人理财管理系统。把分散在多家银行 / 券商 / 基金平台的资产（基金、股票、理财、现金，含多币种）集中记录，提供持仓总览、盈亏分析、买卖建议、智能问答，AI 能力可在 DeepSeek / GLM / Kimi / 通义千问 / Ollama 之间切换。

## 第一阶段必做（9 项）

资产记录 / 持仓视图 / 13 类交易流水 / 行情接入 / 多币种折算 / 理财到期自动处理 / AI 对话（4 场景）/ 多模型切换 / Excel+Markdown 导出。

## 第一阶段不做（明确推迟，YAGNI）

报表生成（表已建，第二阶段）、微信小程序（第三阶段）、多用户/SaaS（第四阶段）、分布式部署（第五阶段）、Eino/langchaingo、golang-migrate、Wire DI、Snowflake、gopdf、Multi-Agent、RAG。

## 权威文档（必读）

| 文档 | 作用 |
|---|---|
| `REQUIREMENT.md` | 总入口与范围（你正在间接读它） |
| `docs/architecture-design.md` | 架构、技术栈、目录、bootstrap.Wire |
| `docs/domain-model.md` | 15 个实体、业务规则、事务边界 |
| `docs/database-schema.md` | 完整建表 SQL + GORM Model 草稿 |
| `docs/upgrade-guide.md` | 本地→分布式升级路径 |

> 上述文档是**只读源**。任何与之冲突的指令请上升给 architect / pm 而非自行解释。
