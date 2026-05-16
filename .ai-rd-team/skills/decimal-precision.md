# 金额精度规范（fin-vault 项目级，强制）

> 本 Skill 是 ai-rd-team 内置规范的**新增**项，无 builtin 同名覆盖。
> 任何金额 / 数量 / 单价 / 净值 / 汇率字段都必须遵守本规范。
> 违反会直接导致**财务数据不可信**，reviewer 见到立刻打回，零讨论空间。

## 适用场景

- 设计 / 修改任何含"金额、数量、单价、净值、汇率"的字段
- 写 Service 中的成本计算、盈亏计算、币种折算逻辑
- 写 Handler 中接收金额参数 / 序列化金额响应
- 接入第三方行情 API、解析返回的浮点数
- 写定时任务中的批量金额计算

## 核心原则

### 1. 唯一合法的金额类型：`decimal.Decimal`

```go
import "github.com/shopspring/decimal"
```

- **必用**：`shopspring/decimal`
- **禁用**：`float32` / `float64` / `big.Float`
- **禁用**：`int64` 配合"分"或"厘"自造定点（如 `amountInCents int64`）
- **禁用**：自造 `type Money struct{ value int64; scale int }`

理由：金融场景必须保留 8 位小数精度（基金净值、汇率）；自造定点在序列化、跨模块传递时极易出错；`shopspring/decimal` 是 Go 生态事实标准。

### 2. 数据库列类型固定

| 用途 | DB 类型 | Go 类型 |
|---|---|---|
| 金额（CNY/USD 等本位） | `DECIMAL(20,2)` | `decimal.Decimal` |
| 数量 / 单价 / 净值 / 汇率 | `DECIMAL(20,8)` | `decimal.Decimal` |
| 比率（百分比） | `DECIMAL(10,6)` | `decimal.Decimal` |

> 20 位总长度足够覆盖 18 位整数 + 2 位小数，永不溢出个人理财场景。
> 8 位小数对齐 ETH 之类高精度需求，未来扩展加密资产无须改 schema。

### 3. JSON 序列化保持字符串

`shopspring/decimal` 默认序列化为 JSON **字符串**：

```json
{"avg_cost": "1.23456789", "total_value": "100000.00"}
```

**不要**改为 number。原因：JavaScript 的 number 是 IEEE-754 double，`0.1 + 0.2 ≠ 0.3` 的精度损失会传染到前端展示和回传。前端拿字符串后用 `decimal.js` 处理。

### 4. 比较与运算只用方法，禁用运算符

```go
// ❌ 禁止
if a.Amount > b.Amount { ... }
total := a.Amount + b.Amount

// ✅ 必须
if a.Amount.GreaterThan(b.Amount) { ... }
total := a.Amount.Add(b.Amount)
```

`decimal.Decimal` 是 struct，运算符不会触发隐式调用，编译器虽然报错但容易被新手误用 `*` 解引用绕过。统一走方法。

### 5. 零值用 `decimal.Zero`，不要写 `decimal.NewFromInt(0)`

```go
var total decimal.Decimal               // 等价于 decimal.Zero
total = total.Add(item.Amount)          // OK
if total.IsZero() { ... }               // OK
```

### 6. 除法必须显式精度

```go
// ❌ 危险：默认精度 16 位，结果不可重现
ratio := profit.Div(cost)

// ✅ 必须：固定精度 + 舍入策略
ratio := profit.DivRound(cost, 8)                                // 8 位小数，HALF_UP
pct   := profit.Div(cost).Mul(decimal.NewFromInt(100)).Round(4)   // 4 位小数百分比
```

约定：
- 数量 / 比率类除法：保留 **8 位小数**，`HALF_UP`
- 百分比展示：保留 **4 位小数**（前端再格式化）
- 金额舍入：保留 **2 位小数**，`HALF_EVEN`（银行家舍入，长期统计无偏）

## 常用模式

### 模式 A：从字符串构造（首选）

```go
amt, err := decimal.NewFromString(req.Amount)   // req.Amount 是 string
if err != nil {
    return errs.InvalidParam.WithDetail("amount: " + err.Error())
}
```

### 模式 B：从 int / float 构造（仅在确信无精度损失时）

```go
qty := decimal.NewFromInt(100)             // OK
unit := decimal.NewFromInt32(50)           // OK

// 仅当 float 来自可信源（如 float64 字面量、且小数位 ≤ 6）
ratio := decimal.NewFromFloat(0.05)        // OK
ratio2 := decimal.NewFromFloatWithExponent(0.1, -2) // 显式 0.10
```

> 第三方 API 返回的 float64**必须**先转字符串再 `NewFromString`：
> ```go
> raw := apiResp.Price                              // float64
> price, err := decimal.NewFromString(strconv.FormatFloat(raw, 'f', 8, 64))
> ```

### 模式 C：加权平均成本计算

```go
// 买入 N 股，移动加权平均
func RecomputeAvgCost(oldQty, oldAvg, newQty, newPrice decimal.Decimal) decimal.Decimal {
    if newQty.IsZero() {
        return oldAvg
    }
    totalCost := oldQty.Mul(oldAvg).Add(newQty.Mul(newPrice))
    totalQty := oldQty.Add(newQty)
    if totalQty.IsZero() {
        return decimal.Zero
    }
    return totalCost.DivRound(totalQty, 8)
}
```

### 模式 D：盈亏计算

```go
func ComputePnL(h *domain.Holding, latestPrice decimal.Decimal) PnL {
    marketValue := h.Quantity.Mul(latestPrice)
    unrealized  := marketValue.Sub(h.TotalCost).Add(h.RealizedPnL)
    total       := unrealized.Add(h.RealizedPnL).Add(h.TotalDividend)
    var ratio decimal.Decimal
    if !h.TotalCost.IsZero() {
        ratio = total.DivRound(h.TotalCost, 4) // 0.0234 = 2.34%
    }
    return PnL{MarketValue: marketValue, Unrealized: unrealized, Total: total, Ratio: ratio}
}
```

### 模式 E：多币种折算（取最近汇率）

```go
func ConvertToCNY(ctx context.Context, amount decimal.Decimal, currency string,
                  ratesRepo repository.RateRepository, asOf time.Time) (decimal.Decimal, error) {
    if currency == "CNY" {
        return amount, nil
    }
    rate, err := ratesRepo.GetLatestBefore(ctx, currency, "CNY", asOf)
    if err != nil {
        return decimal.Zero, fmt.Errorf("convert %s→CNY: %w", currency, err)
    }
    return amount.Mul(rate.Rate).Round(2), nil
}
```

### 模式 F：用 `Round` 在响应边界统一精度

业务计算保留 8 位，**只在向前端响应**或**向 DB 写金额字段**时 `Round`：

```go
type AssetResp struct {
    AvgCost     string `json:"avg_cost"`      // 8 位
    MarketValue string `json:"market_value"`  // 2 位（金额）
    PnLRatio    string `json:"pnl_ratio"`     // 4 位
}

func FromHolding(h *domain.Holding, pnl PnL) AssetResp {
    return AssetResp{
        AvgCost:     h.AvgCost.StringFixed(8),
        MarketValue: pnl.MarketValue.StringFixed(2),
        PnLRatio:    pnl.Ratio.StringFixed(4),
    }
}
```

## 禁止

| ❌ 反模式 | ✅ 正确做法 |
|---|---|
| `var amount float64` | `var amount decimal.Decimal` |
| `amountCents int64`（自造定点） | `decimal.Decimal` + DB DECIMAL |
| `decimal.NewFromFloat(req.Amount)` 接收 float | DTO 字段定义为 string，`NewFromString` |
| `a.Amount + b.Amount`（编译错） | `a.Amount.Add(b.Amount)` |
| `if pnl > 0` | `if pnl.IsPositive()` 或 `pnl.GreaterThan(decimal.Zero)` |
| `profit / cost` 不指定精度 | `profit.DivRound(cost, 8)` |
| JSON 序列化为 number | 默认字符串，**不要**改回 number |
| 在 service 里反复 `Round`，再传给下一个函数 | 仅在响应/落库**边界**统一 Round |
| MySQL 用 `FLOAT` / `DOUBLE` 列 | `DECIMAL(20,2)` 或 `DECIMAL(20,8)` |
| 用 `decimal.NewFromInt(0)` 表示零值 | 直接 `decimal.Zero` 或 `var x decimal.Decimal` |
| 拿到第三方 API 的 `float64` 直接 `NewFromFloat` | 先 `FormatFloat('f', 8, 64)` 再 `NewFromString` |

## 示例

### 完整的 buy 交易 Service（含金额处理）

```go
func (s *txnService) Buy(ctx context.Context, in BuyInput) (*domain.Transaction, error) {
    if in.Quantity.IsZero() || in.Quantity.IsNegative() {
        return nil, errs.InvalidParam.WithDetail("quantity must be positive")
    }
    if in.UnitPrice.IsNegative() {
        return nil, errs.InvalidParam.WithDetail("unit_price cannot be negative")
    }

    var txn *domain.Transaction
    err := s.uow.WithTx(ctx, func(tx repository.Tx) error {
        h, err := tx.Holdings().GetByAssetPlatform(ctx, in.UserID, in.AssetID, in.PlatformID)
        if err != nil && !errors.Is(err, errs.NotFound) {
            return err
        }

        var (
            oldQty, oldAvg decimal.Decimal
        )
        if h != nil {
            oldQty, oldAvg = h.Quantity, h.AvgCost
        }
        newQty := oldQty.Add(in.Quantity)
        newAvg := RecomputeAvgCost(oldQty, oldAvg, in.Quantity, in.UnitPrice)
        amount := in.Quantity.Mul(in.UnitPrice).Add(in.Fee).Round(2) // 金额边界 Round

        txn = &domain.Transaction{
            UserID: in.UserID, AssetID: in.AssetID, PlatformID: in.PlatformID,
            TxnType: domain.TxnTypeBuy, Quantity: in.Quantity, UnitPrice: in.UnitPrice,
            Amount: amount, Fee: in.Fee, OccurredAt: in.OccurredAt,
        }
        if err := tx.Transactions().Create(ctx, txn); err != nil {
            return err
        }
        return tx.Holdings().Upsert(ctx, &domain.Holding{
            UserID: in.UserID, AssetID: in.AssetID, PlatformID: in.PlatformID,
            Quantity: newQty, AvgCost: newAvg,
            TotalCost: newQty.Mul(newAvg).Round(2),
        })
    })
    return txn, err
}
```

### DTO 定义（前端传字符串）

```go
type BuyReq struct {
    AssetID    uint64 `json:"asset_id" binding:"required"`
    PlatformID uint64 `json:"platform_id" binding:"required"`
    Quantity   string `json:"quantity" binding:"required"`     // "100.5"
    UnitPrice  string `json:"unit_price" binding:"required"`   // "1.2345"
    Fee        string `json:"fee"`                             // "5.00" 可空
    OccurredAt string `json:"occurred_at" binding:"required"`
}

func (r *BuyReq) ToInput() (BuyInput, error) {
    qty, err := decimal.NewFromString(r.Quantity)
    if err != nil { return BuyInput{}, fmt.Errorf("quantity: %w", err) }
    price, err := decimal.NewFromString(r.UnitPrice)
    if err != nil { return BuyInput{}, fmt.Errorf("unit_price: %w", err) }
    fee := decimal.Zero
    if r.Fee != "" {
        fee, err = decimal.NewFromString(r.Fee)
        if err != nil { return BuyInput{}, fmt.Errorf("fee: %w", err) }
    }
    t, err := time.Parse(time.RFC3339, r.OccurredAt)
    if err != nil { return BuyInput{}, fmt.Errorf("occurred_at: %w", err) }
    return BuyInput{
        AssetID: r.AssetID, PlatformID: r.PlatformID,
        Quantity: qty, UnitPrice: price, Fee: fee, OccurredAt: t,
    }, nil
}
```

> 完整业务规则参见 `docs/domain-model.md`。
> 命名约束见 `naming-conventions` skill。
