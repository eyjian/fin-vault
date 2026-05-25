## ADDED Requirements

### Requirement: Asset API SHALL support optional include_holdings parameter
The Asset List API (`GET /assets`) SHALL support an optional query parameter `include_holdings` (boolean). When set to `true`, the API SHALL return asset data with associated holding and PnL (profit and loss) information.

#### Scenario: Request asset list without include_holdings parameter
- **WHEN** client sends `GET /assets?asset_type=stock`
- **THEN** API returns asset list with only basic asset fields (code, name, market, etc.)
- **AND** response time SHALL be under 100ms for typical workloads

#### Scenario: Request asset list with include_holdings=true
- **WHEN** client sends `GET /assets?asset_type=stock&include_holdings=true`
- **THEN** API returns asset list with basic asset fields plus holding data
- **AND** each asset object SHALL contain: `holding_quantity`, `holding_avg_cost`, `holding_total_cost`, `holding_realized_pnl`, `holding_total_dividend`

#### Scenario: Request asset list with include_holdings=true includes calculated fields
- **WHEN** client sends `GET /assets?asset_type=stock&include_holdings=true`
- **THEN** API returns asset list with calculated fields: `holding_latest_price`, `holding_market_value`, `holding_unrealized_pnl`, `holding_total_pnl`, `holding_pnl_ratio`

### Requirement: Asset list frontend SHALL display holding and PnL data
The frontend asset list pages (`/stock`, `/wealth`, `/cash`) SHALL display holding and PnL data when the API returns this information.

#### Scenario: Stock page displays holding data
- **WHEN** user visits `/stock` page
- **THEN** page SHALL display columns: 持有数量, 平均成本, 最新价, 市值, 未实现盈亏, 总盈亏, 盈亏比率, 已实现盈亏, 累计分红

#### Scenario: Wealth page displays holding data
- **WHEN** user visits `/wealth` page
- **THEN** page SHALL display columns: 持有份额, 平均成本, 最新净值, 市值, 未实现盈亏, 总盈亏, 盈亏比率, 已实现盈亏, 累计利息

#### Scenario: Cash page displays holding data
- **WHEN** user visits `/cash` page
- **THEN** page SHALL display columns: 持有金额, 关联账户, 收益率, 总收益

### Requirement: Holding data SHALL handle assets without holdings
The system SHALL gracefully handle assets that have no associated holding records.

#### Scenario: Asset without holding record
- **WHEN** an asset has no holding record in database
- **THEN** API SHALL return holding fields as `null` or zero values
- **AND** frontend SHALL display "-" or "0" for holding-related columns

#### Scenario: Mixed assets with and without holdings
- **WHEN** asset list contains both assets with holdings and without holdings
- **THEN** API SHALL return correct data for each asset
- **AND** frontend SHALL display data correctly for each row
