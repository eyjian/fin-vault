# FinVault 前端

Vue 3 + Vite + TypeScript + Element Plus + Pinia + Vue Router + decimal.js + ECharts。

## 启动

```bash
npm install
npm run dev   # http://localhost:5173 ，axios 代理 /api -> http://localhost:8080
```

## 必交付页面

- `/dashboard` 持仓总览（按类型/平台/币种聚合 + 总盈亏卡片）
- `/fund` 基金管理（CRUD + 净值刷新 + 申购/赎回/分红流水）★必交付
- `/stock` 股票管理（CRUD + 行情刷新 + 5 种流水）★必交付
- `/wealth` 理财产品管理（CRUD + 即将到期/已到期标记 + 流水）★必交付
- `/cash` 现金账户（CASH-{platform}-{currency}）
- `/holding` 持仓视图（含 market_value/pnl 实时计算 + raw/CNY 切换）
- `/transaction` 交易流水（13 种类型筛选 + 录入）
- `/quote` 行情管理（批量刷新 + 手动写入）
- `/rate` 汇率维护
- `/ai-chat` AI 对话（4 场景 + Provider 切换 + SSE 流式）
- `/export` 数据导出（Excel / Markdown）

## 关键约定

- 金额/数量：JSON 字符串，前端用 `decimal.js`（`@/utils/decimal`）处理，禁用 number 算金额
- 用户认证：Header `X-User-Id`，axios 拦截器自动注入 `1`
- 错误处理：全局 axios 拦截器统一弹 `ElMessage.error`
- AI SSE：`@/api/ai.ts` 的 `aiStream` 用 fetch + ReadableStream 解析

## 依赖图

```
src/
├── api/            后端接口封装（与 docs/design/data-interfaces.yaml 一对一）
├── components/     通用组件（MoneyInput / TxnDialog）
├── stores/         Pinia（platform / ai）
├── utils/decimal.ts decimal.js 包装
├── views/
│   ├── Dashboard.vue
│   ├── asset/{Fund,Stock,Wealth,Cash}Manage.vue
│   ├── transaction/TransactionList.vue
│   ├── HoldingView.vue
│   ├── QuoteManage.vue / RateManage.vue
│   ├── AIChat.vue
│   └── ExportPage.vue
├── router/index.ts
├── App.vue
└── main.ts
```
