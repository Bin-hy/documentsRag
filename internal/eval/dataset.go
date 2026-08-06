// Package eval 提供 RAG 评估框架：数据集加载、Recall@K 检索评估、LLM-as-Judge 准确性/忠实度评估与报告输出。
package eval

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EvalSample 数据集单条样本
type EvalSample struct {
	Question    string   `json:"question"`         // 问题
	Answer      string   `json:"answer,omitempty"` // 标准答案（可选，仅问答/全量模式用）
	ExpectedIDs []string `json:"expected_ids"`     // 期望检索片段 ID（Recall@K 判定依据）
	KBID        string   `json:"kb_id,omitempty"`  // 知识库范围（可选，空=不限定）
}

// Dataset 评估数据集
type Dataset struct {
	Name    string       `json:"name"`
	Samples []EvalSample `json:"samples"`
}

// LoadDataset 加载数据集：.json 走 JSON 解析，.jsonl 走逐行解析
func LoadDataset(path string) (*Dataset, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取数据集失败: %w", err)
	}

	var ds *Dataset
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jsonl":
		ds, err = parseJSONL(data)
	case ".json":
		ds = &Dataset{}
		err = json.Unmarshal(data, ds)
	default:
		return nil, fmt.Errorf("不支持的数据集格式: %s（支持 .json / .jsonl）", filepath.Ext(path))
	}
	if err != nil {
		return nil, fmt.Errorf("解析数据集失败: %w", err)
	}
	if ds.Name == "" {
		ds.Name = filepath.Base(path)
	}
	if err := Validate(ds); err != nil {
		return nil, err
	}
	return ds, nil
}

// parseJSONL 解析 JSONL：每行一条 sample
func parseJSONL(data []byte) (*Dataset, error) {
	ds := &Dataset{}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 允许大行
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var s EvalSample
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			return nil, fmt.Errorf("JSONL 第 %d 行解析失败: %w", len(ds.Samples)+1, err)
		}
		ds.Samples = append(ds.Samples, s)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return ds, nil
}

// Validate 校验数据集结构完整性
func Validate(d *Dataset) error {
	if d == nil {
		return fmt.Errorf("数据集为空")
	}
	if len(d.Samples) == 0 {
		return fmt.Errorf("数据集没有样本")
	}
	for i, s := range d.Samples {
		if strings.TrimSpace(s.Question) == "" {
			return fmt.Errorf("第 %d 条样本 question 为空", i+1)
		}
		if s.ExpectedIDs == nil {
			return fmt.Errorf("第 %d 条样本 expected_ids 为 nil（应使用空数组）", i+1)
		}
	}
	return nil
}
