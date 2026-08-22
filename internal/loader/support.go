package loader

import "sort"

// SupportResult 单个文件的支持判断结果。
type SupportResult struct {
	Supported bool   `json:"supported"`
	Reason    string `json:"reason,omitempty"` // 不支持原因；Supported 时为空
}

// SupportedType 单个扩展名类型的支持状态（枚举给查询入口/前端使用）。
type SupportedType struct {
	Ext       string `json:"ext"`      // 如 ".mp3"（小写）
	Category  string `json:"category"` // text / image / audio / video
	Supported bool   `json:"supported"`
	Reason    string `json:"reason,omitempty"` // 不支持原因
}

// Support 判断单个文件当前是否可处理：
// 格式无法识别 → 不支持（原因取 ErrUnsupportedFormat）；多媒体 parser 能力未配置 → 不支持（原因取 CheckCapabilities）。
func (r *defaultRegistry) Support(info FileInfo) SupportResult {
	parser, err := r.Resolve(info)
	if err != nil {
		return SupportResult{Supported: false, Reason: err.Error()}
	}
	if checker, ok := parser.(MediaCapabilityChecker); ok {
		if err := checker.CheckCapabilities(); err != nil {
			return SupportResult{Supported: false, Reason: err.Error()}
		}
	}
	return SupportResult{Supported: true}
}

// SupportedTypes 枚举当前注册表全部扩展名的支持状态，按 ext 升序返回。
// 类别取 parser 的 MediaCategory（未实现视为 "text"）；能力检查与 Support 同源。
func (r *defaultRegistry) SupportedTypes() []SupportedType {
	types := make([]SupportedType, 0, len(r.extMap))
	for ext, parser := range r.extMap {
		st := SupportedType{Ext: ext, Category: "text", Supported: true}
		if c, ok := parser.(MediaCategory); ok {
			st.Category = c.MediaCategory()
		}
		if checker, ok := parser.(MediaCapabilityChecker); ok {
			if err := checker.CheckCapabilities(); err != nil {
				st.Supported = false
				st.Reason = err.Error()
			}
		}
		types = append(types, st)
	}
	sort.Slice(types, func(i, j int) bool { return types[i].Ext < types[j].Ext })
	return types
}
