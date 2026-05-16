// Package tools 提供 AI 对话场景的可调用工具集合。
//
// 工具实现只依赖 repository 抽象接口，不直接 import gorm/redis。
// 业务 Service 层通过 Registry 注入需要的 Tool 子集到 LLM 对话中。
package tools

import (
	"sort"
	"sync"

	"github.com/eyjian/fin-vault/backend/internal/llm"
)

// Registry 工具注册中心。线程安全：注册一次，运行时只读。
type Registry struct {
	mu    sync.RWMutex
	tools map[string]llm.Tool
}

// NewRegistry 返回空 Registry。
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]llm.Tool)}
}

// Register 注册一个工具。重名会覆盖。
func (r *Registry) Register(t llm.Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.Name] = t
}

// Get 按名取工具。
func (r *Registry) Get(name string) (llm.Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// All 返回全部工具的拷贝（用于注入对话）。
func (r *Registry) All() []llm.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]llm.Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Names 已注册的工具名列表。
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for n := range r.tools {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Pick 按名称挑出工具子集。未注册的名字会被忽略。
func (r *Registry) Pick(names ...string) []llm.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]llm.Tool, 0, len(names))
	for _, n := range names {
		if t, ok := r.tools[n]; ok {
			out = append(out, t)
		}
	}
	return out
}
