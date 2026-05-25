package agent

import (
	"context"
	"errors"
	"sync"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/llm/session"
)

// fakeStore 是测试用的 in-memory SessionStore（仅实现 §5 测试涉及的方法），
// 与 §4 sqliteStore 共享相同的 SessionStore 接口契约。
//
// 设计：
//   - 内置互斥锁保证并发安全（虽然单测通常不并发，但 SDK channel 消费天然并发友好）
//   - AppendStep / AppendMessage 失败可通过设 stepErr / msgErr 注入，覆盖软失败路径
type fakeStore struct {
	mu       sync.Mutex
	sessions map[string]*domain.Session
	messages []domain.Message
	steps    []domain.AgentStep

	// 注入错误，用于覆盖 D11 / spec 软失败路径
	stepErr error
	msgErr  error
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		sessions: make(map[string]*domain.Session),
		messages: []domain.Message{},
		steps:    []domain.AgentStep{},
	}
}

func (f *fakeStore) CreateSession(_ context.Context, s *domain.Session) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions[s.ID] = s
	return nil
}

func (f *fakeStore) GetSession(_ context.Context, sessionID string) (*domain.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sessions[sessionID]
	if !ok {
		return nil, errors.New("not found")
	}
	return s, nil
}

func (f *fakeStore) UpdateSession(_ context.Context, s *domain.Session) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions[s.ID] = s
	return nil
}

func (f *fakeStore) DeleteSession(_ context.Context, sessionID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.sessions, sessionID)
	return nil
}

func (f *fakeStore) ListSessions(_ context.Context, _ session.ListSessionsOptions) ([]domain.Session, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.Session, 0, len(f.sessions))
	for _, s := range f.sessions {
		out = append(out, *s)
	}
	return out, int64(len(out)), nil
}

func (f *fakeStore) ListMessages(_ context.Context, sessionID string, _ int) ([]domain.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []domain.Message{}
	for _, m := range f.messages {
		if m.SessionID == sessionID {
			out = append(out, m)
		}
	}
	return out, nil
}

func (f *fakeStore) AppendMessage(_ context.Context, m *domain.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.msgErr != nil {
		return f.msgErr
	}
	f.messages = append(f.messages, *m)
	return nil
}

func (f *fakeStore) AppendStep(_ context.Context, step *domain.AgentStep) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stepErr != nil {
		return f.stepErr
	}
	f.steps = append(f.steps, *step)
	return nil
}

func (f *fakeStore) EstimateStepsSize(_ context.Context) (int64, error) {
	return 0, nil
}

// snapshotSteps 返回 step 列表副本（避免测试代码持有共享引用）
func (f *fakeStore) snapshotSteps() []domain.AgentStep {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.AgentStep, len(f.steps))
	copy(out, f.steps)
	return out
}

// snapshotMessages 返回 message 列表副本
func (f *fakeStore) snapshotMessages() []domain.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.Message, len(f.messages))
	copy(out, f.messages)
	return out
}

// 编译期断言：fakeStore 满足 SessionStore 接口
var _ session.SessionStore = (*fakeStore)(nil)
