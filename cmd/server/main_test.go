package main

import (
	"testing"

	"github.com/Bin-hy/bin-rag/internal/app"
)

func TestParseConfigFlag(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"短横线 -c", []string{"-c", "configs/config.local.yaml"}, "configs/config.local.yaml"},
		{"双横线 --config", []string{"--config", "configs/config.yaml"}, "configs/config.yaml"},
		{"单横线 -config 等价", []string{"-config", "a.yaml"}, "a.yaml"},
		{"无参数", nil, ""},
		{"空参数列表", []string{}, ""},
		{"缺值", []string{"-c"}, ""},
		{"未知参数忽略", []string{"-x", "1"}, ""},
		{"混合顺序", []string{"-c", "x.yaml", "extra"}, "x.yaml"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := app.ParseConfigFlag(tc.args)
			if got != tc.want {
				t.Errorf("app.ParseConfigFlag(%v) = %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}
