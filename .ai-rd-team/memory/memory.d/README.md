# memory.d/ —— 按需检索的详细历史

本目录存放**按需 Read** 的详细历史与案例库（不强制加载到 prompt，避免 token 膨胀）。

子目录约定（参见 ai-rd-team `06-memory-system.md` §3.1）：

```
memory.d/
├── domain/          # 业务规则详解、用户画像
├── past-cases/      # 历史需求 / 历史实现 / 重构记录
├── bug-patterns/    # 已知 Bug 模式
├── anti-patterns/   # 反模式案例
└── benchmarks/      # 性能基线
```

## 写入约定

- 单文件 ≤ 5K tokens（必须遵守 ai-rd-team spec）
- 必带 frontmatter（type/layer/author/created/updated/related/tags/estimated_tokens）
- 由角色主动写入（pm / architect / reviewer 整理重要案例时）

## 第一阶段（Phase 0）

暂无内容。团队首次跑起来产生足够素材后，由 pm/architect 整理沉淀。
