// BinRag RAG 评估 CLI：加载数据集，运行检索/问答/LLM 指标评估，输出报告。
// 用法：binrag-eval -c configs/config.yaml -d dataset.json -m full
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Bin-hy/bin-rag/internal/app"
	"github.com/Bin-hy/bin-rag/internal/config"
	"github.com/Bin-hy/bin-rag/internal/eval"
)

func main() {
	var (
		cfgPath  = flag.String("c", "", "配置文件路径（默认 configs/config.yaml）")
		dataset  = flag.String("d", "", "数据集文件路径（必填，.json 或 .jsonl）")
		mode     = flag.String("m", "full", "评估模式：retrieve / qa / full")
		kList    = flag.String("k", "1,3,5", "Recall@K 的 K 值，逗号分隔")
		judgeMod = flag.String("j", "", "LLM-as-Judge 模型（空=用配置默认）")
		concurr  = flag.Int("n", 2, "样本并发数")
		outPath  = flag.String("o", "", "报告输出路径（空=stdout；.json 后缀输出 JSON）")
		verbose  = flag.Bool("v", false, "显示详细日志")
	)
	flag.Parse()

	if !*verbose {
		slog.SetLogLoggerLevel(slog.LevelError)
	}

	if *dataset == "" {
		fmt.Fprintln(os.Stderr, "错误: 必须指定数据集路径 -d <file.json|file.jsonl>")
		flag.Usage()
		os.Exit(1)
	}

	// 配置文件：-c 或环境变量或默认路径
	path := *cfgPath
	if path == "" {
		path = os.Getenv("BINRAG_CONFIG")
	}
	if path == "" {
		path = "configs/config.yaml"
	}
	cfg, err := config.LoadConfig(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "加载配置失败: %v\n", err)
		os.Exit(1)
	}

	deps, err := app.AssembleEvalDeps(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "装配失败: %v\n", err)
		os.Exit(1)
	}
	defer deps.Closer()

	kValues, err := parseKList(*kList)
	if err != nil {
		fmt.Fprintf(os.Stderr, "解析 K 值失败: %v\n", err)
		os.Exit(1)
	}

	evalCfg := eval.EvalConfig{
		DatasetPath:  *dataset,
		KValues:      kValues,
		Mode:         *mode,
		JudgeModel:   *judgeMod,
		Concurrency:  *concurr,
		OutputPath:   *outPath,
		OutputFormat: outputFormat(*outPath),
	}

	rep, err := eval.Run(context.Background(), evalCfg, deps.Retriever, deps.Engine, deps.LLM)
	if err != nil {
		fmt.Fprintf(os.Stderr, "评估失败: %v\n", err)
		os.Exit(1)
	}

	// 输出
	w := os.Stdout
	if *outPath != "" {
		f, err := os.Create(*outPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "创建报告文件失败: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()
		w = f
	}
	if err := eval.WriteReport(*rep, w, evalCfg.OutputFormat); err != nil {
		fmt.Fprintf(os.Stderr, "写报告失败: %v\n", err)
		os.Exit(1)
	}
}

// parseKList 解析逗号分隔的 K 值列表
func parseKList(s string) ([]int, error) {
	parts := strings.Split(s, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.Atoi(p)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("无效 K 值: %q", p)
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("K 值列表为空")
	}
	return out, nil
}

// outputFormat 由输出路径推断格式：.json → json，否则 text
func outputFormat(path string) string {
	if path != "" && strings.EqualFold(filepath.Ext(path), ".json") {
		return "json"
	}
	return "text"
}
