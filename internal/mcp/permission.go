package mcp

// KBPermission 知识库权限（三态，spec F5）：
//   - scope == ""        → 无任何知识库权限
//   - scope == "all"     → 全部知识库
//   - scope == "allowlist" → 仅 IDs 中的知识库
type KBPermission struct {
	All bool     // scope == "all"
	IDs []string // scope == "allowlist" 时的白名单
}

// ParseScope 从 API Key 权限字段解析知识库权限
func ParseScope(scope string, ids []string) KBPermission {
	switch scope {
	case "all":
		return KBPermission{All: true}
	case "allowlist":
		return KBPermission{IDs: ids}
	default:
		return KBPermission{}
	}
}

// CanAccess 当前凭据能否访问指定知识库
func (p KBPermission) CanAccess(kbID string) bool {
	if p.All {
		return true
	}
	for _, id := range p.IDs {
		if id == kbID {
			return true
		}
	}
	return false
}

// Resolve 把请求级 kb_id（可空）解析为检索范围 IDs：
//   - 指定 kb_id：校验在可访问范围内（越权 → NewKBForbidden，不泄露存在性）→ [kbID]
//   - 未指定 + All：返回 nil（不过滤全部）
//   - 未指定 + allowlist：返回白名单 IDs
//   - 未指定 + 无权限：返回 NewKBForbidden
func (p KBPermission) Resolve(reqKBID string) ([]string, error) {
	if reqKBID != "" {
		if !p.CanAccess(reqKBID) {
			return nil, NewKBForbidden()
		}
		return []string{reqKBID}, nil
	}
	if p.All {
		return nil, nil // 不过滤全部
	}
	if len(p.IDs) == 0 {
		return nil, NewKBForbidden() // 无知识库权限
	}
	return p.IDs, nil
}

// ToolAllowed 校验 Key 是否被授予指定 Tool：
// 空白名单 = 无任何 MCP Tool 权限（历史 Key 默认无权限，spec F6）；否则 name 须在名单内。
func ToolAllowed(tools []string, name string) bool {
	for _, t := range tools {
		if t == name {
			return true
		}
	}
	return false
}
