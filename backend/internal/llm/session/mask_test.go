package session

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMaskSensitiveJSON_FlatMap(t *testing.T) {
	in := []byte(`{"api_key":"sk-abc","model":"deepseek-chat","temperature":0.7}`)
	out := MaskSensitiveJSON(in)

	var got map[string]interface{}
	require.NoError(t, json.Unmarshal(out, &got))
	assert.Equal(t, "***", got["api_key"], "api_key 应被掩码")
	assert.Equal(t, "deepseek-chat", got["model"], "非敏感字段应原样保留")
	// JSON 解码后数字均为 float64
	assert.InDelta(t, 0.7, got["temperature"].(float64), 0.0001)
}

func TestMaskSensitiveJSON_NestedMap(t *testing.T) {
	in := []byte(`{
		"tool":"openai_call",
		"arguments":{
			"prompt":"hello",
			"headers":{"Authorization":"Bearer xxx","Content-Type":"application/json"},
			"options":{"password":"p@ss","api_key":"sk-abc"}
		}
	}`)
	out := MaskSensitiveJSON(in)

	var got map[string]interface{}
	require.NoError(t, json.Unmarshal(out, &got))

	args := got["arguments"].(map[string]interface{})
	assert.Equal(t, "hello", args["prompt"])

	headers := args["headers"].(map[string]interface{})
	assert.Equal(t, "***", headers["Authorization"], "Authorization 应被掩码（case-insensitive）")
	assert.Equal(t, "application/json", headers["Content-Type"], "非敏感字段应保留")

	opts := args["options"].(map[string]interface{})
	assert.Equal(t, "***", opts["password"])
	assert.Equal(t, "***", opts["api_key"])
}

func TestMaskSensitiveJSON_CaseInsensitiveAndVariants(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"upper API_KEY", `{"API_KEY":"x"}`},
		{"mixed Api_Key", `{"Api_Key":"x"}`},
		{"no underscore apikey", `{"apikey":"x"}`},
		{"upper APIKEY", `{"APIKEY":"x"}`},
		{"upper Authorization", `{"Authorization":"x"}`},
		{"contains substr access_token", `{"access_token":"x"}`},
		{"contains substr id_token", `{"id_token":"x"}`},
		{"upper Password", `{"Password":"x"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := MaskSensitiveJSON([]byte(c.in))
			var got map[string]interface{}
			require.NoError(t, json.Unmarshal(out, &got))
			for _, v := range got {
				assert.Equal(t, "***", v, "敏感 key 必须被掩码")
			}
		})
	}
}

func TestMaskSensitiveJSON_NonSensitiveKeysUntouched(t *testing.T) {
	in := []byte(`{"name":"x","count":3,"enabled":true,"items":["a","b"]}`)
	out := MaskSensitiveJSON(in)
	// 直接对比 marshal 出来的等价 JSON（结构应一致）
	var a, b map[string]interface{}
	require.NoError(t, json.Unmarshal(in, &a))
	require.NoError(t, json.Unmarshal(out, &b))
	assert.Equal(t, a, b, "非敏感字段集合应原样")
}

func TestMaskSensitiveJSON_SliceOfMaps(t *testing.T) {
	in := []byte(`{"steps":[{"api_key":"k1","name":"step1"},{"token":"t2","name":"step2"}]}`)
	out := MaskSensitiveJSON(in)

	var got map[string]interface{}
	require.NoError(t, json.Unmarshal(out, &got))
	steps := got["steps"].([]interface{})
	require.Len(t, steps, 2)
	s0 := steps[0].(map[string]interface{})
	s1 := steps[1].(map[string]interface{})
	assert.Equal(t, "***", s0["api_key"])
	assert.Equal(t, "step1", s0["name"])
	assert.Equal(t, "***", s1["token"])
	assert.Equal(t, "step2", s1["name"])
}

func TestMaskSensitiveJSON_EmptyAndNil(t *testing.T) {
	assert.Nil(t, MaskSensitiveJSON(nil), "nil 输入返回 nil")
	assert.Equal(t, []byte(""), MaskSensitiveJSON([]byte("")), "空字节原样")
	assert.Equal(t, []byte("   "), MaskSensitiveJSON([]byte("   ")), "纯空白原样")
}

func TestMaskSensitiveJSON_InvalidJSONReturnedAsIs(t *testing.T) {
	in := []byte(`not a json {api_key:"x"`)
	out := MaskSensitiveJSON(in)
	assert.Equal(t, in, out, "非合法 JSON 应原样返回，不阻断业务")
}

func TestMaskSensitiveJSON_TopLevelArray(t *testing.T) {
	// 顶层是数组的合法 JSON，递归仍应处理
	in := []byte(`[{"api_key":"k1"},{"name":"keep"}]`)
	out := MaskSensitiveJSON(in)
	var got []map[string]interface{}
	require.NoError(t, json.Unmarshal(out, &got))
	require.Len(t, got, 2)
	assert.Equal(t, "***", got[0]["api_key"])
	assert.Equal(t, "keep", got[1]["name"])
}

func TestIsSensitiveKey(t *testing.T) {
	sensitive := []string{
		"api_key", "API_KEY", "ApiKey", "apikey", "APIKEY",
		"password", "Password", "user_password",
		"token", "access_token", "refresh_token",
		"Authorization", "AUTHORIZATION",
	}
	for _, k := range sensitive {
		assert.True(t, isSensitiveKey(k), "%q 应被识别为敏感", k)
	}

	nonSensitive := []string{"name", "model", "count", "id", "user_id", "session_id"}
	for _, k := range nonSensitive {
		assert.False(t, isSensitiveKey(k), "%q 不应被识别为敏感", k)
	}
}
