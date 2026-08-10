package mcp

import "testing"

// ParseScope 三态解析
func TestParseScope(t *testing.T) {
	cases := []struct {
		name  string
		scope string
		ids   []string
		all   bool
		len   int
	}{
		{"无权限", "", nil, false, 0},
		{"全部", "all", nil, true, 0},
		{"白名单", "allowlist", []string{"kb-a", "kb-b"}, false, 2},
		{"未知 scope 视为无权限", "weird", nil, false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := ParseScope(tc.scope, tc.ids)
			if p.All != tc.all || len(p.IDs) != tc.len {
				t.Errorf("ParseScope(%q) = %+v，期望 all=%v len=%d", tc.scope, p, tc.all, tc.len)
			}
		})
	}
}

// CanAccess：all 全部可访问；allowlist 仅白名单内
func TestKBPermissionCanAccess(t *testing.T) {
	all := KBPermission{All: true}
	if !all.CanAccess("任意库") {
		t.Error("all 应可访问任意知识库")
	}

	wl := KBPermission{IDs: []string{"kb-a"}}
	if !wl.CanAccess("kb-a") {
		t.Error("白名单内应可访问")
	}
	if wl.CanAccess("kb-b") {
		t.Error("白名单外不应可访问")
	}

	none := KBPermission{}
	if none.CanAccess("kb-a") {
		t.Error("无权限不应可访问任何知识库")
	}
}

// Resolve 四分支
func TestKBPermissionResolve(t *testing.T) {
	// 指定 kb_id 且在范围内 → 单值
	ids, err := (KBPermission{IDs: []string{"kb-a", "kb-b"}}).Resolve("kb-b")
	if err != nil || len(ids) != 1 || ids[0] != "kb-b" {
		t.Errorf("白名单内指定 kb_id 应返回 [kb-b]，实际 %v err=%v", ids, err)
	}
	// 指定 kb_id 越权 → KBForbidden（不泄露存在性）
	if _, err := (KBPermission{IDs: []string{"kb-a"}}).Resolve("kb-x"); err == nil {
		t.Error("越权 kb_id 应返回错误")
	} else if err.Error() != msgKBForbidden {
		t.Errorf("越权消息应统一为 %q，实际 %q", msgKBForbidden, err.Error())
	}
	// 未指定 + all → nil（不过滤）
	if ids, err := (KBPermission{All: true}).Resolve(""); err != nil || ids != nil {
		t.Errorf("all 未指定 kb_id 应返回 nil，实际 %v err=%v", ids, err)
	}
	// 未指定 + allowlist → 白名单
	if ids, err := (KBPermission{IDs: []string{"kb-a"}}).Resolve(""); err != nil || len(ids) != 1 {
		t.Errorf("allowlist 未指定 kb_id 应返回白名单，实际 %v err=%v", ids, err)
	}
	// 未指定 + 无权限 → KBForbidden
	if _, err := (KBPermission{}).Resolve(""); err == nil || err.Error() != msgKBForbidden {
		t.Errorf("无权限未指定 kb_id 应返回 KBForbidden，实际 %v", err)
	}
}

// ToolAllowed：空名单无任何权限；白名单按名匹配
func TestToolAllowed(t *testing.T) {
	if ToolAllowed(nil, "ask") {
		t.Error("空白名单（历史 Key）不应允许任何 Tool")
	}
	if ToolAllowed([]string{}, "ask") {
		t.Error("空名单不应允许任何 Tool")
	}
	if !ToolAllowed([]string{"retrieve", "ask"}, "ask") {
		t.Error("白名单内应允许")
	}
	if ToolAllowed([]string{"retrieve"}, "ask") {
		t.Error("白名单外不应允许")
	}
}
