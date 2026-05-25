package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

func main() {
	// 测试不带参数
	resp, err := http.Get("http://localhost:2026/api/v1/assets?asset_type=stock")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("=== Without include_holdings ===\n%s\n\n", formatJSON(body))

	// 测试带参数
	resp2, err := http.Get("http://localhost:2026/api/v1/assets?asset_type=stock&include_holdings=true")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	defer resp2.Body.Close()
	body2, _ := io.ReadAll(resp2.Body)
	fmt.Printf("=== With include_holdings=true ===\n%s\n", formatJSON(body2))
}

func formatJSON(data []byte) string {
	var obj interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		return string(data)
	}
	formatted, _ := json.MarshalIndent(obj, "", "  ")
	return string(formatted)
}
