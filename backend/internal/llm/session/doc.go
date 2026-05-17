// Package session 定义 AI 会话存储与缓存的抽象接口。
//
// SessionStore 接口（store.go）由 §3.3 定义、§4 实现 SQLite 版本；
// Cache 接口（cache.go）由 §3.4 定义并提供 NoopCache 默认实现。
package session
