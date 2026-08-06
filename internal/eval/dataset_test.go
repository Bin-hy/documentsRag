package eval

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadDatasetJSON(t *testing.T) {
	path := writeTemp(t, "ds.json", `{
	  "name": "测试集",
	  "samples": [
	    {"question": "什么是 RAG？", "answer": "检索增强生成", "expected_ids": ["c1"]},
	    {"question": "如何分块？", "expected_ids": ["c2", "c3"], "kb_id": "kb1"}
	  ]
	}`)

	ds, err := LoadDataset(path)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if ds.Name != "测试集" {
		t.Errorf("名称错误: %q", ds.Name)
	}
	if len(ds.Samples) != 2 {
		t.Fatalf("样本数错误: %d", len(ds.Samples))
	}
	if ds.Samples[0].ExpectedIDs[0] != "c1" || ds.Samples[1].KBID != "kb1" {
		t.Errorf("字段解析错误: %+v", ds.Samples)
	}
}

func TestLoadDatasetJSONL(t *testing.T) {
	path := writeTemp(t, "ds.jsonl", `{"question":"Q1","expected_ids":["a"]}
{"question":"Q2","expected_ids":[],"kb_id":"kb2"}
`)

	ds, err := LoadDataset(path)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if len(ds.Samples) != 2 {
		t.Fatalf("样本数错误: %d", len(ds.Samples))
	}
	if ds.Samples[1].KBID != "kb2" {
		t.Errorf("kb_id 解析错误: %+v", ds.Samples[1])
	}
}

func TestLoadDatasetInvalidJSON(t *testing.T) {
	path := writeTemp(t, "ds.json", `{"name": "x", "samples": [`)
	if _, err := LoadDataset(path); err == nil {
		t.Fatal("非法 JSON 应报错")
	}
}

func TestLoadDatasetUnsupportedExt(t *testing.T) {
	path := writeTemp(t, "ds.txt", `hello`)
	if _, err := LoadDataset(path); err == nil {
		t.Fatal("不支持扩展名应报错")
	}
}

func TestValidateEmptySamples(t *testing.T) {
	ds := &Dataset{Name: "x", Samples: nil}
	if err := Validate(ds); err == nil {
		t.Fatal("空样本应报错")
	}
}

func TestValidateEmptyQuestion(t *testing.T) {
	ds := &Dataset{Samples: []EvalSample{{Question: "  ", ExpectedIDs: []string{}}}}
	if err := Validate(ds); err == nil {
		t.Fatal("空 question 应报错")
	}
}

func TestValidateNilExpectedIDs(t *testing.T) {
	ds := &Dataset{Samples: []EvalSample{{Question: "q"}}} // ExpectedIDs 为 nil
	if err := Validate(ds); err == nil {
		t.Fatal("nil expected_ids 应报错")
	}
}

func TestDatasetNameFromFilename(t *testing.T) {
	path := writeTemp(t, "myds.json", `{"samples":[{"question":"q","expected_ids":[]}]}`)
	ds, err := LoadDataset(path)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if ds.Name != "myds.json" {
		t.Errorf("名称应从文件名推导，实际 %q", ds.Name)
	}
}
