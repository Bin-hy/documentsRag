package rag

import (
	"testing"

	"github.com/Bin-hy/bin-rag/internal/config"
)

func TestResolveStrategyPriority(t *testing.T) {
	global := config.StrategyConfig{Query: "multi", Fusion: "rrf", Decomposition: "off", StepBack: "off", HyDE: "off", Routing: "off"}
	kb := config.StrategyConfig{Query: "single", Fusion: "none"}
	req := config.StrategyConfig{Query: "multi"}

	eff, err := ResolveStrategy(global, kb, req)
	if err != nil {
		t.Fatalf("ResolveStrategy 失败: %v", err)
	}
	// 请求覆盖 kb
	if eff.Query != "multi" {
		t.Errorf("req 应覆盖 kb：query=%q", eff.Query)
	}
	// kb 覆盖全局（req 未设置 fusion）
	if eff.Fusion != "none" {
		t.Errorf("kb 应覆盖全局：fusion=%q", eff.Fusion)
	}
	// 全局兜底（kb/req 未设置）
	if eff.Decomposition != "off" || eff.Routing != "off" {
		t.Errorf("全局应兜底：decomposition=%q routing=%q", eff.Decomposition, eff.Routing)
	}
}

func TestResolveStrategyDefaults(t *testing.T) {
	// 全空 → 默认值
	eff, err := ResolveStrategy(config.StrategyConfig{}, config.StrategyConfig{}, config.StrategyConfig{})
	if err != nil {
		t.Fatalf("ResolveStrategy 失败: %v", err)
	}
	if eff.Query != "multi" || eff.Fusion != "rrf" || eff.Decomposition != "off" ||
		eff.StepBack != "off" || eff.HyDE != "off" || eff.Routing != "off" {
		t.Errorf("默认值错误: %+v", eff)
	}
}

func TestValidateStrategyIllegal(t *testing.T) {
	cases := []struct {
		name string
		s    EffectiveStrategy
	}{
		{"single+rrf", EffectiveStrategy{Query: "single", Fusion: "rrf"}},
		{"routing+decomposition", EffectiveStrategy{Query: "multi", Fusion: "rrf", Routing: "auto", Decomposition: "parallel"}},
		{"routing+stepback", EffectiveStrategy{Query: "multi", Fusion: "rrf", Routing: "auto", StepBack: "on"}},
		{"未知枚举", EffectiveStrategy{Query: "weird", Fusion: "rrf"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := ValidateStrategy(c.s); err == nil {
				t.Errorf("%s 应被拒绝", c.name)
			}
		})
	}
}

func TestValidateStrategyValid(t *testing.T) {
	valid := []EffectiveStrategy{
		{Query: "single", Fusion: "none", Decomposition: "off", StepBack: "off", HyDE: "off", Routing: "off"},
		{Query: "multi", Fusion: "rrf", Decomposition: "parallel", StepBack: "off", HyDE: "on", Routing: "off"},
	}
	for _, s := range valid {
		if err := ValidateStrategy(s); err != nil {
			t.Errorf("合法组合被拒绝: %+v (%v)", s, err)
		}
	}
}
