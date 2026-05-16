# 命名规范（fin-vault 项目级，强制）

> 本 Skill 是 ai-rd-team 内置规范的**新增**项，无 builtin 同名覆盖。
> 与 `agent.d/coding-conventions.md` 配套：那里给"是什么"，本文给"怎么写代码体现"。
> 任何违反由 reviewer 直接打回，无需讨论。

## 适用场景

- 新建数据库表 / 字段 / 索引时
- 新建 Go domain struct / 字段 tag 时
- 新建 Go 包、文件、函数、变量时
- code review 检查命名一致性时
- 定义业务错误码、配置 key、API 路由路径时

## 核心原则

### 1. 数据库三件套（强约束）

| 对象 | 规则 | 示例 |
|---|---|---|
| 表名 | `t_fv_{module}_{name}`，**全小写复数** | `t_fv_core_holdings` / `t_fv_quote_price_quotes` |
| 字段名 | `f_` 前缀 + 全小写下划线 | `f_id` / `f_user_id` / `f_avg_cost` |
| 主键 | 统一 `f_id`，`bigint unsigned` | GORM `uint64` |
| 外键 | `f_{表单数}_id` | `f_user_id` / `f_asset_id` |
| 时间字段 | `f_created_at` / `f_updated_at` / `f_deleted_at` | 软删用 `gorm.DeletedAt` |
| 唯一索引 | `uk_{字段简写}`，**不带模块前缀** | `uk_user_asset_platform` |
| 普通索引 | `idx_{字段简写}` | `idx_user_status_platform` |

> 索引为何不加 `t_fv_` 前缀：MySQL 索引名作用域在表内，加全局前缀冗余且超长。

### 2. 七个固定模块前缀

仅以下 7 个模块前缀合法，新增前缀须先写 ADR：

| 前缀 | 含义 | 典型表 |
|---|---|---|
| `user` | 用户 | `t_fv_user_users` |
| `dict` | 字典 / 元数据 | `t_fv_dict_platforms` / `t_fv_dict_currencies` |
| `core` | 核心业务（资产/持仓/交易） | `t_fv_core_assets` / `t_fv_core_holdings` |
| `quote` | 行情 / 汇率 | `t_fv_quote_price_quotes` / `t_fv_quote_exchange_rates` |
| `ai` | AI 对话 | `t_fv_ai_conversations` / `t_fv_ai_messages` |
| `report` | 报表 | `t_fv_report_snapshots`（第二阶段） |
| `sys` | 系统配置 | `t_fv_sys_configs`（预留） |

### 3. Go 标识符命名

| 对象 | 规则 | 示例 |
|---|---|---|
| 包名 | 全小写、单数、无下划线 | `asset` / `holding` / `llm` |
| 接口名 | `XxxRepository` / `XxxService` / `XxxProvider` | `AssetRepository` / `LLMProvider` |
| 实现 struct | 小写驼峰（包内私有） | `assetRepo` / `openaiProvider` |
| 构造函数 | `NewXxx` 返回**接口**而非 struct | `func NewAssetRepo(db *gorm.DB) AssetRepository` |
| 错误变量 | `Err` 前缀 + 大驼峰 | `ErrAssetNotFound` / `ErrInvalidCurrency` |
| 错误码常量 | `ErrCodeXxx` 大驼峰 | `ErrCodeAssetCodeDuplicated = 40001` |
| 枚举常量 | `Type` + 大驼峰，类型别名分隔 | `TxnTypeBuy TxnType = "buy"` |
| context key | 包私有 `type ctxKey struct{}` | 不用 string 常量 |

### 4. domain 字段与 GORM tag 的精确对应

```go
type Asset struct {
    ID         uint64          `gorm:"column:f_id;primaryKey;autoIncrement"`
    UserID     uint64          `gorm:"column:f_user_id;index:idx_user_code"`
    AssetCode  string          `gorm:"column:f_asset_code;type:varchar(64);uniqueIndex:uk_user_code"`
    AssetName  string          `gorm:"column:f_asset_name;type:varchar(128)"`
    AssetType  AssetType       `gorm:"column:f_asset_type;type:varchar(20)"`
    Currency   string          `gorm:"column:f_currency;type:varchar(10)"`
    AvgCost    decimal.Decimal `gorm:"column:f_avg_cost;type:decimal(20,8)"`
    Quantity   decimal.Decimal `gorm:"column:f_quantity;type:decimal(20,8)"`
    CreatedAt  time.Time       `gorm:"column:f_created_at"`
    UpdatedAt  time.Time       `gorm:"column:f_updated_at"`
    DeletedAt  gorm.DeletedAt  `gorm:"column:f_deleted_at;index"`
}

func (Asset) TableName() string { return "t_fv_core_assets" }
```

**强约束**：
- 每个字段必须显式写 `column:` tag（不能依赖 GORM 自动转换）
- Go 字段大驼峰，DB 字段下划线，**不靠默认转换**
- 每个 model 必须实现 `TableName()`，不依赖复数自动推导

### 5. API 路径

```
/api/v{N}/{resource}            # 资源根
/api/v{N}/{resource}/:id        # 单资源
/api/v{N}/{resource}/:id/{sub}  # 子资源
```

- 资源名**全小写复数**（与表名风格一致）：`/assets` `/holdings` `/transactions`
- 路径段不超过 4 层
- 动词类操作（如平仓、回放）用 POST + 子路径动词：`POST /holdings/:id/recompute`

### 6. 配置 key

YAML 配置 key 全小写下划线：

```yaml
database:
  driver: sqlite
  dsn: data/fin-vault.db
  max_open_conns: 20
llm:
  default_provider: deepseek
  providers:
    deepseek:
      base_url: https://api.deepseek.com/v1
      api_key: ${FV_LLM_DEEPSEEK_KEY}
```

环境变量：`FV_` 前缀 + 路径下划线大写。`database.dsn` → `FV_DATABASE_DSN`。

### 7. 错误码编号区间（必背）

| 区间 | 用途 | 示例 |
|---|---|---|
| `10000-19999` | 通用（参数、未授权、未找到） | `10001 InvalidParam` / `10404 NotFound` |
| `20000-29999` | user / dict 模块 | `20001 UserNotFound` |
| `30000-39999` | core 模块 | `30001 AssetCodeDuplicated` |
| `40000-49999` | quote 模块 | `40001 ExchangeRateMissing` |
| `50000-59999` | ai 模块 | `50001 LLMProviderUnavailable` |
| `60000-69999` | report 模块 | 预留 |
| `90000-99999` | 系统级（DB / Cache / 第三方） | `90001 DBConnLost` |

新增错误码必须在 `pkg/errs/codes.go` 集中定义，附中文 message。

## 常用模式

### 模式 A：新建一张表的命名清单

为 `core` 模块加一张「成本批次」表：

1. 表名：`t_fv_core_cost_lots`（core 模块 + 复数 cost_lots）
2. 字段：`f_id` / `f_holding_id` / `f_quantity` / `f_unit_cost` / `f_purchased_at` / `f_remaining_quantity` / `f_created_at` / `f_updated_at`
3. 索引：
   - 唯一索引：无（同一持仓允许多批次）
   - 普通索引：`idx_holding` (`f_holding_id`)、`idx_holding_purchased` (`f_holding_id`, `f_purchased_at`)
4. Go struct：`CostLot`，`TableName()` 返回 `t_fv_core_cost_lots`
5. Repository：`CostLotRepository` 接口 + `costLotRepo` 实现
6. Service 方法：`FIFOConsume(ctx, holdingID, qty)`

### 模式 B：枚举的"DB-Go-API"三层一致

```go
// domain/transaction.go
type TxnType string

const (
    TxnTypeBuy             TxnType = "buy"
    TxnTypeSell            TxnType = "sell"
    TxnTypeDividend        TxnType = "dividend"
    TxnTypeDividendReinvest TxnType = "dividend_reinvest"
    // ... 完整 13 种见 domain-terms
)

func (t TxnType) Valid() bool {
    switch t {
    case TxnTypeBuy, TxnTypeSell, /* ... */ :
        return true
    }
    return false
}
```

- DB 存原值字符串（`'buy'`），不要 ENUM
- API JSON 也是原值字符串（不要数字枚举）
- 三层一致，零翻译开销

## 禁止

| ❌ 反模式 | ✅ 正确做法 |
|---|---|
| 表名 `assets` / `t_assets` | `t_fv_core_assets` |
| 字段 `id` / `user_id` | `f_id` / `f_user_id` |
| 索引 `t_fv_core_assets_uk_user_code` | `uk_user_code`（表内即可） |
| 模块前缀自创 `biz` / `data` | 仅七个合法前缀 |
| Go 字段不写 `column:` tag | 每个字段显式 `column:` |
| 错误变量 `AssetNotFoundErr` | `ErrAssetNotFound`（Err 前缀） |
| 路由 `/getAsset` / `/asset_list` | `GET /assets` / `GET /assets/:id` |
| 配置 key 大驼峰 `MaxOpenConns` | `max_open_conns` |
| 错误码自由分配 | 严格按区间分配 |
| 同一概念 DB 用 `txn_type`、Go 用 `Type` | DB 字段名与 Go 字段名形成映射，但概念名一致 |

## 示例

### 全合规建表 SQL（cost_lots）

```sql
CREATE TABLE `t_fv_core_cost_lots` (
  `f_id`                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `f_holding_id`         BIGINT UNSIGNED NOT NULL,
  `f_quantity`           DECIMAL(20,8)   NOT NULL,
  `f_unit_cost`          DECIMAL(20,8)   NOT NULL,
  `f_remaining_quantity` DECIMAL(20,8)   NOT NULL,
  `f_purchased_at`       DATETIME        NOT NULL,
  `f_created_at`         DATETIME        NOT NULL,
  `f_updated_at`         DATETIME        NOT NULL,
  `f_deleted_at`         DATETIME        NULL,
  PRIMARY KEY (`f_id`),
  KEY `idx_holding`           (`f_holding_id`),
  KEY `idx_holding_purchased` (`f_holding_id`, `f_purchased_at`),
  KEY `idx_deleted_at`        (`f_deleted_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='成本批次（FIFO）';
```

### 全合规 Go Model

```go
type CostLot struct {
    ID                uint64          `gorm:"column:f_id;primaryKey;autoIncrement"`
    HoldingID         uint64          `gorm:"column:f_holding_id;index:idx_holding;index:idx_holding_purchased,priority:1"`
    Quantity          decimal.Decimal `gorm:"column:f_quantity;type:decimal(20,8)"`
    UnitCost          decimal.Decimal `gorm:"column:f_unit_cost;type:decimal(20,8)"`
    RemainingQuantity decimal.Decimal `gorm:"column:f_remaining_quantity;type:decimal(20,8)"`
    PurchasedAt       time.Time       `gorm:"column:f_purchased_at;index:idx_holding_purchased,priority:2"`
    CreatedAt         time.Time       `gorm:"column:f_created_at"`
    UpdatedAt         time.Time       `gorm:"column:f_updated_at"`
    DeletedAt         gorm.DeletedAt  `gorm:"column:f_deleted_at;index"`
}

func (CostLot) TableName() string { return "t_fv_core_cost_lots" }
```

### 全合规错误码片段

```go
// pkg/errs/codes.go
const (
    ErrCodeInvalidParam            = 10001
    ErrCodeNotFound                = 10404
    ErrCodeAssetCodeDuplicated     = 30001
    ErrCodeHoldingQuantityNotEnough = 30010
    ErrCodeExchangeRateMissing     = 40001
    ErrCodeLLMProviderUnavailable  = 50001
)

var (
    ErrAssetCodeDuplicated     = New(ErrCodeAssetCodeDuplicated, "资产代码已存在")
    ErrHoldingQuantityNotEnough = New(ErrCodeHoldingQuantityNotEnough, "持仓数量不足，无法卖出")
)
```

> 完整数据库 schema 参见 `docs/database-schema.md`。
> 错误处理细节见 `error-handling` skill。
