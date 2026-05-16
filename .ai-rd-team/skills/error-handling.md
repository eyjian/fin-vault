# 错误处理规范（fin-vault 项目级，强制）

> 本 Skill 是 ai-rd-team 内置规范的**新增**项，无 builtin 同名覆盖。
> 业务错误码体系是后端可观测性的根基，违反此规范会导致前端无法精准提示用户、运维无法按错误码聚类告警。

## 适用场景

- 在 service / repository 里返回错误
- 在 handler 里把错误转换为 HTTP 响应
- 定义新业务错误码
- 写 Gin Recovery / Logger 中间件
- 处理第三方 API（LLM、行情、平台）调用失败
- 处理 GORM 错误（NotFound、Duplicate、ConnLost）

## 核心原则

### 1. 业务错误统一用 `pkg/errs` 包

```go
// pkg/errs/errs.go
type AppError struct {
    Code    int    // 业务错误码（见 naming-conventions §错误码区间）
    Message string // 中文用户可见消息
    Detail  string // 开发者可见详情（不返给前端）
    cause   error  // 原始错误（用 errors.Unwrap 取出）
}

func (e *AppError) Error() string {
    if e.Detail != "" {
        return fmt.Sprintf("[%d] %s: %s", e.Code, e.Message, e.Detail)
    }
    return fmt.Sprintf("[%d] %s", e.Code, e.Message)
}

func (e *AppError) Unwrap() error { return e.cause }

func New(code int, msg string) *AppError { return &AppError{Code: code, Message: msg} }

func (e *AppError) WithDetail(d string) *AppError {
    cp := *e
    cp.Detail = d
    return &cp
}

func (e *AppError) Wrap(err error) *AppError {
    cp := *e
    cp.cause = err
    return &cp
}
```

### 2. 预定义错误变量集中在 `pkg/errs/codes.go`

```go
var (
    InvalidParam            = New(10001, "参数不合法")
    Unauthorized            = New(10401, "未认证")
    Forbidden               = New(10403, "无权访问")
    NotFound                = New(10404, "资源不存在")
    Conflict                = New(10409, "资源冲突")
    Internal                = New(10500, "服务内部错误")

    AssetCodeDuplicated     = New(30001, "资产代码已存在")
    HoldingQuantityNotEnough = New(30010, "持仓数量不足")
    ExchangeRateMissing     = New(40001, "缺少汇率数据")
    LLMProviderUnavailable  = New(50001, "AI 服务不可用")
    DBConnLost              = New(90001, "数据库连接异常")
)
```

新增错误必须在此处定义，**禁止**在业务代码里 `errs.New(...)` 临时拼。

### 3. 错误传递三段法

```
repository  →  service  →  handler
  GORM 错误   翻译为业务错误   翻译为 HTTP 响应
```

- **repository**：捕获 GORM 哨兵（`gorm.ErrRecordNotFound` 等），翻译为 `errs.NotFound` / `errs.Conflict` / `errs.DBConnLost`，**保留原 cause**：`return errs.NotFound.Wrap(err)`
- **service**：业务规则校验失败直接返 `errs.XxxBusinessError`；底层错误透传或包一层语义；不写 HTTP 状态码
- **handler**：`response.Error(c, err)` 统一转 HTTP，**不写**任何 `c.JSON(500, ...)`

### 4. 错误判定用 `errors.Is`，不要字符串匹配

```go
// ❌ 禁止
if strings.Contains(err.Error(), "not found") { ... }

// ✅ 必须
if errors.Is(err, errs.NotFound) { ... }
```

理由：`AppError` 的 `Is` 默认按 `Code` 比较，包了 `Wrap` 也能识别。

### 5. 包了 `Wrap` 的错误才能被定位，否则等于丢栈

底层（如 `gorm.ErrRecordNotFound`、`*net.OpError`）必须用 `Wrap` 保留：

```go
return errs.DBConnLost.Wrap(err).WithDetail("ping timeout 5s")
```

日志打印时同时输出 `Code` / `Message` / `Detail` / `cause`，运维能一眼看到链路。

### 6. Recovery 中间件捕获 panic 转 500

```go
func Recovery() gin.HandlerFunc {
    return gin.CustomRecovery(func(c *gin.Context, recovered any) {
        slog.Error("panic_recovered",
            "request_id", c.GetString("request_id"),
            "panic", recovered,
            "stack", string(debug.Stack()),
        )
        response.Error(c, errs.Internal.WithDetail(fmt.Sprintf("%v", recovered)))
    })
}
```

panic 是 unrecoverable 兜底，**业务错误不要用 panic**。

### 7. 第三方调用失败必须包语义

```go
resp, err := s.platformClient.GetPrice(ctx, code)
if err != nil {
    return errs.New(40010, "行情接口调用失败").Wrap(err)
}
```

不要直接把第三方错误返回给上层，否则错误码区间会被污染。

## 常用模式

### 模式 A：Repository 翻译 GORM 错误

```go
func (r *assetRepo) GetByID(ctx context.Context, id uint64) (*domain.Asset, error) {
    var a domain.Asset
    err := r.db.WithContext(ctx).First(&a, id).Error
    switch {
    case err == nil:
        return &a, nil
    case errors.Is(err, gorm.ErrRecordNotFound):
        return nil, errs.NotFound.WithDetail(fmt.Sprintf("asset id=%d", id))
    case isDuplicateKeyErr(err):
        return nil, errs.Conflict.Wrap(err)
    default:
        return nil, errs.DBConnLost.Wrap(err)
    }
}
```

### 模式 B：Service 业务校验

```go
func (s *txnService) Sell(ctx context.Context, in SellInput) (*domain.Transaction, error) {
    h, err := s.holdingRepo.Get(ctx, in.HoldingID)
    if err != nil {
        return nil, err // 透传，可能是 NotFound / DBConnLost
    }
    if h.Quantity.LessThan(in.Quantity) {
        return nil, errs.HoldingQuantityNotEnough.WithDetail(
            fmt.Sprintf("holding=%d available=%s want=%s",
                h.ID, h.Quantity.String(), in.Quantity.String()),
        )
    }
    // ... 继续
}
```

### 模式 C：统一响应封装

```go
// pkg/utils/response/response.go
type Response struct {
    Code    int    `json:"code"`              // 0=成功，非 0=业务错误码
    Message string `json:"message"`
    Data    any    `json:"data,omitempty"`
    TraceID string `json:"trace_id,omitempty"`
}

func OK(c *gin.Context, data any) {
    c.JSON(http.StatusOK, Response{
        Code: 0, Message: "ok", Data: data, TraceID: c.GetString("request_id"),
    })
}

func Error(c *gin.Context, err error) {
    var ae *errs.AppError
    if !errors.As(err, &ae) {
        ae = errs.Internal.Wrap(err)
    }
    httpStatus := mapHTTPStatus(ae.Code)
    slog.Error("request_failed",
        "request_id", c.GetString("request_id"),
        "code", ae.Code, "message", ae.Message, "detail", ae.Detail,
        "cause", fmt.Sprintf("%v", errors.Unwrap(ae)),
    )
    c.JSON(httpStatus, Response{
        Code: ae.Code, Message: ae.Message, TraceID: c.GetString("request_id"),
    })
}

func mapHTTPStatus(code int) int {
    switch {
    case code == 10401: return http.StatusUnauthorized
    case code == 10403: return http.StatusForbidden
    case code == 10404: return http.StatusNotFound
    case code == 10409: return http.StatusConflict
    case code >= 10001 && code < 20000: return http.StatusBadRequest
    case code >= 90000: return http.StatusInternalServerError
    default: return http.StatusBadRequest
    }
}
```

> Detail 进日志，不进响应体。前端只看 `code` + `message`。

### 模式 D：Logger 中间件 + RequestID

```go
func RequestID() gin.HandlerFunc {
    return func(c *gin.Context) {
        rid := c.GetHeader("X-Request-ID")
        if rid == "" {
            rid = uuid.NewString()
        }
        c.Set("request_id", rid)
        c.Header("X-Request-ID", rid)
        c.Next()
    }
}

func Logger() gin.HandlerFunc {
    return func(c *gin.Context) {
        start := time.Now()
        c.Next()
        slog.Info("http_access",
            "request_id", c.GetString("request_id"),
            "method", c.Request.Method,
            "path", c.Request.URL.Path,
            "status", c.Writer.Status(),
            "latency_ms", time.Since(start).Milliseconds(),
        )
    }
}
```

### 模式 E：错误测试（用 `ErrorIs`）

```go
func TestSell_QuantityNotEnough(t *testing.T) {
    // ... 装配 mock ...
    _, err := svc.Sell(ctx, SellInput{HoldingID: 1, Quantity: dec("100")})
    require.ErrorIs(t, err, errs.HoldingQuantityNotEnough)
}
```

不要用 `assert.Contains(err.Error(), "不足")`——脆弱且与文案耦合。

## 禁止

| ❌ 反模式 | ✅ 正确做法 |
|---|---|
| `panic("xxx 错误")` 表达业务错误 | `return errs.XxxError` |
| handler 里 `c.JSON(500, gin.H{"error": "xxx"})` | `response.Error(c, err)` 统一封装 |
| repository 直接返回 `gorm.ErrRecordNotFound` | 翻译为 `errs.NotFound.Wrap(err)` |
| service 里写 HTTP 状态码 | service 只返业务错误，HTTP 由 handler 决定 |
| `strings.Contains(err.Error(), "...")` | `errors.Is(err, errs.Xxx)` |
| `fmt.Errorf("xxx: %v", err)`（丢失 wrap） | `fmt.Errorf("xxx: %w", err)` |
| 临时 `errs.New(99999, "...")` | 在 `pkg/errs/codes.go` 集中定义 |
| 错误细节直接返给前端 | Detail 进日志、不进响应 |
| 第三方错误直接透传到上层 | 用 `errs.Wrap` 包一层业务语义 |
| 多个 Recovery 嵌套 | 全局只挂一次 |
| 日志只打 `err.Error()` | 打 `code` / `message` / `detail` / `cause` |
| 错误码自由分配 | 严格按 `naming-conventions` 区间分配 |

## 示例

### 完整的 Handler（错误处理一气呵成）

```go
func (h *TxnHandler) Sell(c *gin.Context) {
    var req dto.SellReq
    if err := c.ShouldBindJSON(&req); err != nil {
        response.Error(c, errs.InvalidParam.WithDetail(err.Error()))
        return
    }
    in, err := req.ToInput()
    if err != nil {
        response.Error(c, errs.InvalidParam.Wrap(err))
        return
    }
    txn, err := h.svc.Sell(c.Request.Context(), in)
    if err != nil {
        response.Error(c, err) // 全权交给 response.Error 翻译
        return
    }
    response.OK(c, dto.FromTransaction(txn))
}
```

### 错误码全景（节选）

```go
// pkg/errs/codes.go（节选）
var (
    // 通用 1xxxx
    InvalidParam            = New(10001, "参数不合法")
    NotFound                = New(10404, "资源不存在")
    Conflict                = New(10409, "资源冲突")
    Internal                = New(10500, "服务内部错误")
    Unauthorized            = New(10401, "未认证")
    Forbidden               = New(10403, "无权访问")

    // user 2xxxx（预留多用户阶段）
    UserNotFound            = New(20001, "用户不存在")

    // core 3xxxx
    AssetCodeDuplicated     = New(30001, "资产代码已存在")
    AssetTypeInvalid        = New(30002, "资产类型不合法")
    HoldingQuantityNotEnough = New(30010, "持仓数量不足")
    TxnTypeInvalid          = New(30020, "交易类型不合法")
    CashAccountMissing      = New(30030, "未找到对应现金账户，无法联动")

    // quote 4xxxx
    ExchangeRateMissing     = New(40001, "缺少汇率数据")
    PriceFetchFailed        = New(40010, "行情接口调用失败")

    // ai 5xxxx
    LLMProviderUnavailable  = New(50001, "AI 服务不可用")
    LLMToolRoundsExceeded   = New(50010, "AI 工具调用超过最大轮次")

    // 系统 9xxxx
    DBConnLost              = New(90001, "数据库连接异常")
    CacheUnavailable        = New(90010, "缓存不可用")
)
```

### 完整的 Recovery 测试

```go
func TestRecovery_Panic(t *testing.T) {
    r := gin.New()
    r.Use(middleware.RequestID(), middleware.Recovery())
    r.GET("/boom", func(c *gin.Context) { panic("oops") })

    w := httptest.NewRecorder()
    req := httptest.NewRequest("GET", "/boom", nil)
    r.ServeHTTP(w, req)

    require.Equal(t, http.StatusInternalServerError, w.Code)
    var resp response.Response
    _ = json.Unmarshal(w.Body.Bytes(), &resp)
    require.Equal(t, errs.Internal.Code, resp.Code)
}
```

> 错误码区间分配见 `naming-conventions` skill §错误码编号区间。
> 完整业务错误清单维护在 `pkg/errs/codes.go`。
