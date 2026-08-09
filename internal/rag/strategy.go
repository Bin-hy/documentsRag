package rag

import (
	"fmt"

	"github.com/Bin-hy/bin-rag/internal/config"
)

// EffectiveStrategy 合并后的生效策略（所有字段已填充非空值）
type EffectiveStrategy struct {
	Query         string // single / multi
	Fusion        string // rrf / none
	Decomposition string // off / parallel / sequential
	StepBack      string // off / on
	HyDE          string // off / on
	Routing       string // off / auto
	Thinking      string // off / on
	// DataSources 允许的数据源（vector_store / web_search 等）；空=nil 表示默认仅 vector_store（私有性默认）
	DataSources []string
}

// DefaultEffectiveStrategy 全局默认生效策略（与阶段一~三默认一致）
func DefaultEffectiveStrategy() EffectiveStrategy {
	return EffectiveStrategy{
		Query:         "multi",
		Fusion:        "rrf",
		Decomposition: "off",
		StepBack:      "off",
		HyDE:          "off",
		Routing:       "off",
		Thinking:      "off",
	}
}

// ResolveStrategy 合并三级策略（请求 > 知识库 > 全局），空字段继承低层级，最终默认值兜底。
// 校验非法组合，返回错误时调用方应降级（用全局默认）。
func ResolveStrategy(global, kb, req config.StrategyConfig) (EffectiveStrategy, error) {
	eff := EffectiveStrategy{}

	// 字段级合并：req 非空覆盖 kb，kb 非空覆盖 global
	pick := func(globalV, kbV, reqV, def string) string {
		v := globalV
		if kbV != "" {
			v = kbV
		}
		if reqV != "" {
			v = reqV
		}
		if v == "" {
			v = def
		}
		return v
	}

	eff.Query = pick(global.Query, kb.Query, req.Query, "multi")
	eff.Fusion = pick(global.Fusion, kb.Fusion, req.Fusion, "rrf")
	eff.Decomposition = pick(global.Decomposition, kb.Decomposition, req.Decomposition, "off")
	eff.StepBack = pick(global.StepBack, kb.StepBack, req.StepBack, "off")
	eff.HyDE = pick(global.HyDE, kb.HyDE, req.HyDE, "off")
	eff.Routing = pick(global.Routing, kb.Routing, req.Routing, "off")
	eff.Thinking = pick(global.Thinking, kb.Thinking, req.Thinking, "off")

	// DataSources 合并：请求 > 知识库 > 全局，高优先级非空覆盖；全空 → nil（默认仅 vector_store）
	eff.DataSources = global.DataSources
	if len(kb.DataSources) > 0 {
		eff.DataSources = kb.DataSources
	}
	if len(req.DataSources) > 0 {
		eff.DataSources = req.DataSources
	}

	if err := ValidateStrategy(eff); err != nil {
		return eff, err
	}
	return eff, nil
}

// ValidateStrategy 校验策略组合合法性
func ValidateStrategy(s EffectiveStrategy) error {
	switch s.Query {
	case "single", "multi":
	default:
		return fmt.Errorf("非法策略 query=%q（应为 single/multi）", s.Query)
	}
	switch s.Fusion {
	case "rrf", "none":
	default:
		return fmt.Errorf("非法策略 fusion=%q（应为 rrf/none）", s.Fusion)
	}
	// query=single + fusion=rrf：无多路可融合
	if s.Query == "single" && s.Fusion == "rrf" {
		return fmt.Errorf("非法策略组合：query=single 时无多路可融合，fusion 应为 none")
	}
	switch s.Decomposition {
	case "off", "parallel", "sequential":
	default:
		return fmt.Errorf("非法策略 decomposition=%q（应为 off/parallel/sequential）", s.Decomposition)
	}
	switch s.StepBack {
	case "off", "on":
	default:
		return fmt.Errorf("非法策略 step_back=%q（应为 off/on）", s.StepBack)
	}
	switch s.HyDE {
	case "off", "on":
	default:
		return fmt.Errorf("非法策略 hyde=%q（应为 off/on）", s.HyDE)
	}
	switch s.Routing {
	case "off", "auto":
	default:
		return fmt.Errorf("非法策略 routing=%q（应为 off/auto）", s.Routing)
	}
	switch s.Thinking {
	case "off", "on":
	default:
		return fmt.Errorf("非法策略 thinking=%q（应为 off/on）", s.Thinking)
	}
	// routing=auto 已含分流，与 decomposition/step_back 冲突
	if s.Routing == "auto" {
		if s.Decomposition != "off" {
			return fmt.Errorf("非法策略组合：routing=auto 时 decomposition 应为 off（路由已含分流）")
		}
		if s.StepBack == "on" {
			return fmt.Errorf("非法策略组合：routing=auto 时 step_back 应为 off（路由已含分流）")
		}
	}
	return nil
}
