// Package tools_test —— pulse_diagnosis 工具单元测试。
package tools_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/eyjian/fin-vault/backend/internal/llm/tools"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
)

// fakePulseDiagnoser 工具单测的 mock 实现，记录调用并返回预设结果/错误。
type fakePulseDiagnoser struct {
	available bool
	results   map[uint]*tools.PulseDiagnoseResult // assetID → result
	errs      map[uint]error                      // assetID → error
	calls     []tools.PulseDiagnoseRequest
}

func (f *fakePulseDiagnoser) IsAvailable() bool { return f.available }

func (f *fakePulseDiagnoser) Diagnose(_ context.Context, req tools.PulseDiagnoseRequest) (*tools.PulseDiagnoseResult, error) {
	f.calls = append(f.calls, req)
	if e, ok := f.errs[req.AssetID]; ok {
		return nil, e
	}
	if r, ok := f.results[req.AssetID]; ok {
		return r, nil
	}
	return &tools.PulseDiagnoseResult{
		AssetID:        req.AssetID,
		Recommendation: "hold",
		Confidence:     "medium",
		Summary:        "default summary",
		Detail:         "default detail",
		TriggerSource:  req.TriggerSource,
	}, nil
}

func TestPulseDiagnosisTool_NoUserIDInContext(t *testing.T) {
	tool := tools.NewPulseDiagnosisTool(tools.PulseDiagnosisDeps{
		Pulse: &fakePulseDiagnoser{available: true},
	})
	ctx := context.Background()

	_, err := tool.Call(ctx, []byte(`{"asset_id": 100}`))
	require.Error(t, err)
	assert.True(t, errors.Is(err, errs.ErrAIToolCallFailed))
}

func TestPulseDiagnosisTool_Unavailable(t *testing.T) {
	tool := tools.NewPulseDiagnosisTool(tools.PulseDiagnosisDeps{
		Pulse: &fakePulseDiagnoser{available: false},
	})
	ctx := tools.WithUserID(context.Background(), 1)

	_, err := tool.Call(ctx, []byte(`{"asset_id": 100}`))
	require.Error(t, err)
	assert.True(t, errors.Is(err, errs.ErrAIPulseUnavailable))
}

func TestPulseDiagnosisTool_NoAssetID(t *testing.T) {
	tool := tools.NewPulseDiagnosisTool(tools.PulseDiagnosisDeps{
		Pulse: &fakePulseDiagnoser{available: true},
	})
	ctx := tools.WithUserID(context.Background(), 1)

	_, err := tool.Call(ctx, []byte(`{}`))
	require.Error(t, err)
	assert.True(t, errors.Is(err, errs.ErrInvalidParam))
}

func TestPulseDiagnosisTool_SingleAsset_Success(t *testing.T) {
	pulse := &fakePulseDiagnoser{
		available: true,
		results: map[uint]*tools.PulseDiagnoseResult{
			100: {
				AssetID:        100,
				Recommendation: "reduce",
				Confidence:     "medium",
				Summary:        "盈利已达 25%，估值偏高",
				Detail:         "详细分析...",
				TriggerSource:  "chat",
			},
		},
	}
	tool := tools.NewPulseDiagnosisTool(tools.PulseDiagnosisDeps{Pulse: pulse})
	ctx := tools.WithUserID(context.Background(), 1)

	out, err := tool.Call(ctx, []byte(`{"asset_id": 100}`))
	require.NoError(t, err)

	output, ok := out.(tools.PulseDiagnosisOutput)
	require.True(t, ok, "返回类型必须是 PulseDiagnosisOutput, got %T", out)
	assert.Equal(t, 1, output.Count)
	require.Len(t, output.Items, 1)
	assert.Equal(t, uint(100), output.Items[0].AssetID)
	assert.Equal(t, "reduce", output.Items[0].Recommendation)
	assert.Equal(t, "medium", output.Items[0].Confidence)
	assert.Equal(t, "success", output.Items[0].Status)

	// 调用层应注入 user_id + triggerSource=chat
	require.Len(t, pulse.calls, 1)
	assert.Equal(t, uint(1), pulse.calls[0].UserID)
	assert.Equal(t, "chat", pulse.calls[0].TriggerSource)
}

func TestPulseDiagnosisTool_BatchAssets_PartialFailure(t *testing.T) {
	pulse := &fakePulseDiagnoser{
		available: true,
		results: map[uint]*tools.PulseDiagnoseResult{
			100: {AssetID: 100, Recommendation: "hold", Confidence: "high", Summary: "ok", Detail: "d"},
			102: {AssetID: 102, Recommendation: "add", Confidence: "medium", Summary: "ok2", Detail: "d2"},
		},
		errs: map[uint]error{
			101: errs.ErrAIRequestFailed,
		},
	}
	tool := tools.NewPulseDiagnosisTool(tools.PulseDiagnosisDeps{Pulse: pulse})
	ctx := tools.WithUserID(context.Background(), 1)

	out, err := tool.Call(ctx, []byte(`{"asset_ids": [100, 101, 102]}`))
	require.NoError(t, err, "批量场景单个失败不应阻塞整体")

	output, ok := out.(tools.PulseDiagnosisOutput)
	require.True(t, ok)
	assert.Equal(t, 3, output.Count)
	require.Len(t, output.Items, 3)

	assert.Equal(t, "success", output.Items[0].Status)
	assert.Equal(t, "failed", output.Items[1].Status)
	assert.NotEmpty(t, output.Items[1].ErrorMessage)
	assert.Equal(t, "success", output.Items[2].Status)
}

func TestPulseDiagnosisTool_AssetIDMergedWithAssetIDs(t *testing.T) {
	pulse := &fakePulseDiagnoser{available: true}
	tool := tools.NewPulseDiagnosisTool(tools.PulseDiagnosisDeps{Pulse: pulse})
	ctx := tools.WithUserID(context.Background(), 1)

	_, err := tool.Call(ctx, []byte(`{"asset_id": 100, "asset_ids": [101, 102]}`))
	require.NoError(t, err)

	require.Len(t, pulse.calls, 3, "asset_id 与 asset_ids 应合并去调")
	assert.Equal(t, uint(100), pulse.calls[0].AssetID)
	assert.Equal(t, uint(101), pulse.calls[1].AssetID)
	assert.Equal(t, uint(102), pulse.calls[2].AssetID)
}
