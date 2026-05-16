# config.advanced.yaml 使用说明

本目录里有两份配置：

| 文件 | 内容 | 来源 |
|---|---|---|
| `config.yaml` | Basic 层（5 个字段） | 手写 / `ai-rd-team init` 生成 |
| `config.advanced.yaml` | **精简的** Advanced 覆盖（仅角色 skills 等关键自定义） | **手写** |

## 为什么 advanced.yaml 是精简的（推荐做法）

依据 ai-rd-team v0.1.0b1 spec：`openspec/specs/design/10-config-schema.md` §3A.4 与 §0.4：

> 生成的 `config.advanced.yaml` 是完整可读的全量配置（不含推断源），用户可以放心**删除不想改的部分**（会回退到 Basic 或默认）。

加载优先级（低层覆盖高层）：

```
defaults.yaml < ~/.ai-rd-team/config.yaml < .ai-rd-team/config.yaml
  < .ai-rd-team/config.advanced.yaml   ← 最高优先
```

**数组字段（如 `roles.*.skills`）的合并策略是"低层完全替换高层"**（spec §2.2），这正是我们需要的——
让本项目的 Go 规范 skills 完全替换 ai-rd-team builtin 的 Python 规范，不会被合并追加。

## 当前 advanced.yaml 覆盖的内容

只覆盖**真正需要改**的两类字段：

1. `adapter.bridge_timeout_seconds`：调高 Tool Calling 多轮场景下的桥接超时
2. `roles.{architect,developer,reviewer,tester}.skills`：用项目级 6 份 skill（fin-vault-*）+ 必要的 builtin 替换默认 Python 规范

其余字段（成本权重、安全约束、Web 端口等）全部回退到 ai-rd-team 默认值。

## 何时重新生成全量 advanced.yaml

只有在需要诊断"某个字段最终生效值是什么"时才执行：

```bash
ai-rd-team config advanced --print-effective > /tmp/effective.yaml   # 仅查看，不写入
```

**不要**直接 `ai-rd-team config advanced` 生成全量并 commit，否则每次 ai-rd-team 升级都需要 diff 手工合并。

## 校验

启动前可执行：

```bash
ai-rd-team config validate
```

看到 `config_version` / `roles.*.skills` 全绿即合格。
