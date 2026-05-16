// Package service 提供 FinVault 的业务编排层。
//
// 严格依赖原则：
//   - 仅 import internal/{repository,cache,llm,llm/tools,platformapi,domain} 与 pkg/{errs,utils/*}
//   - 禁止 import gorm.io / redis/go-redis / sashabaranov/go-openai / xuri/excelize 等第三方实现
//   - 事务一律走 repository.UnitOfWork，不直接拿 *gorm.DB
package service
