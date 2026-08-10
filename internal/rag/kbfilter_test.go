package rag

import (
	"reflect"
	"testing"
)

// kbFilter：空 → 不过滤；单值 → {"kb_id": id}；多值 → {"kb_id": [ids]}
func TestKBFilterMultiScope(t *testing.T) {
	if f := kbFilter(""); f != nil {
		t.Errorf("空范围应返回 nil（不过滤），got %v", f)
	}
	if f := kbFilter("", nil...); f != nil {
		t.Errorf("空 KBIDs 应返回 nil，got %v", f)
	}

	single := kbFilter("kb-1")
	if single["kb_id"] != "kb-1" {
		t.Errorf("单值范围应返回 {\"kb_id\": \"kb-1\"}，got %v", single)
	}

	multi := kbFilter("", "kb-a", "kb-b")
	want := map[string]any{"kb_id": []string{"kb-a", "kb-b"}}
	if !reflect.DeepEqual(multi, want) {
		t.Errorf("多值范围错误，got %v want %v", multi, want)
	}

	// kbID 与 KBIDs 合并去空
	merged := kbFilter("kb-x", "", "kb-y")
	if !reflect.DeepEqual(merged, map[string]any{"kb_id": []string{"kb-x", "kb-y"}}) {
		t.Errorf("合并范围错误，got %v", merged)
	}
}
