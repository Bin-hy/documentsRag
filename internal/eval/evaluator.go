package eval

import (
	"context"
	"sync"

	"github.com/Bin-hy/bin-rag/internal/llm"
	"github.com/Bin-hy/bin-rag/internal/rag"
	"github.com/Bin-hy/bin-rag/internal/retriever"
)

// EvalConfig 评测配置
type EvalConfig struct {
	DatasetPath  string // 数据集文件路径
	KValues      []int  // Recall@K 的 K 列表，默认 [1,3,5]
	Mode         string // retrieve / qa / full
	JudgeModel   string // LLM-as-Judge 模型（空=用 llm 配置默认）
	Concurrency  int    // 样本并发数，默认 2
	OutputPath   string // 报告输出路径（空=stdout）
	OutputFormat string // text / json（空=按扩展名推断）
}

// 默认值
const (
	defaultMode        = "full"
	defaultConcurrency = 2
)

// Run 执行评估：加载数据集 → 按 Mode 分派 → 汇总报告
func Run(ctx context.Context, cfg EvalConfig, rt retriever.Retriever, engine rag.Engine, judgeLLM llm.LLM) (*Report, error) {
	if cfg.Mode == "" {
		cfg.Mode = defaultMode
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = defaultConcurrency
	}
	if len(cfg.KValues) == 0 {
		cfg.KValues = []int{1, 3, 5}
	}

	ds, err := LoadDataset(cfg.DatasetPath)
	if err != nil {
		return nil, err
	}

	switch cfg.Mode {
	case "retrieve":
		results := runRetrieve(ctx, ds, cfg, rt)
		rep := ComputeMetrics(results, cfg.KValues)
		rep.DatasetName = ds.Name
		rep.Mode = cfg.Mode
		return &rep, nil
	case "qa":
		results := runQA(ctx, ds, cfg, rt, engine)
		rep := ComputeMetrics(results, cfg.KValues)
		rep.DatasetName = ds.Name
		rep.Mode = cfg.Mode
		return &rep, nil
	case "full":
		results := runFull(ctx, ds, cfg, rt, engine, judgeLLM)
		rep := ComputeMetrics(results, cfg.KValues)
		rep.DatasetName = ds.Name
		rep.Mode = cfg.Mode
		return &rep, nil
	default:
		return nil, errUnknownMode(cfg.Mode)
	}
}

// runRetrieve 仅检索：逐样本跑检索计算 Recall@K，不调 LLM
func runRetrieve(ctx context.Context, ds *Dataset, cfg EvalConfig, rt retriever.Retriever) []EvalResult {
	results := make([]EvalResult, len(ds.Samples))
	runConcurrent(ctx, cfg.Concurrency, len(ds.Samples), func(i int) {
		res := evalRetrieve(ctx, rt, ds.Samples[i], cfg.KValues)
		results[i] = res
	})
	return results
}

// runQA 问答：逐样本检索+问答，收集回答与来源（不评 LLM 指标）
func runQA(ctx context.Context, ds *Dataset, cfg EvalConfig, rt retriever.Retriever, engine rag.Engine) []EvalResult {
	results := make([]EvalResult, len(ds.Samples))
	runConcurrent(ctx, cfg.Concurrency, len(ds.Samples), func(i int) {
		res := evalRetrieve(ctx, rt, ds.Samples[i], cfg.KValues)
		if res.Error != "" {
			results[i] = res
			return
		}
		res = evalAsk(ctx, engine, ds.Samples[i], res)
		results[i] = res
	})
	return results
}

// runFull 全量：检索+问答+LLM 指标（准确性/忠实度）
func runFull(ctx context.Context, ds *Dataset, cfg EvalConfig, rt retriever.Retriever, engine rag.Engine, judgeLLM llm.LLM) []EvalResult {
	results := make([]EvalResult, len(ds.Samples))
	runConcurrent(ctx, cfg.Concurrency, len(ds.Samples), func(i int) {
		res := evalRetrieve(ctx, rt, ds.Samples[i], cfg.KValues)
		if res.Error != "" {
			results[i] = res
			return
		}
		res = evalAsk(ctx, engine, ds.Samples[i], res)
		if res.Error != "" {
			results[i] = res
			return
		}
		res = evalJudge(ctx, judgeLLM, ds.Samples[i], res, cfg.JudgeModel)
		results[i] = res
	})
	return results
}

// evalRetrieve 单样本检索与 Recall 判定
func evalRetrieve(ctx context.Context, rt retriever.Retriever, s EvalSample, kValues []int) EvalResult {
	res := EvalResult{Sample: s, Recall: make(map[int]bool)}

	filter := map[string]any{}
	if s.KBID != "" {
		filter["kb_id"] = s.KBID
	}
	maxK := 1
	for _, k := range kValues {
		if k > maxK {
			maxK = k
		}
	}

	out, err := rt.Search(ctx, retriever.RetrieveRequest{Query: s.Question, TopK: maxK, Filter: filter})
	if err != nil {
		res.Error = "检索失败: " + err.Error()
		return res
	}
	for _, r := range out {
		res.Retrieved = append(res.Retrieved, r.ID)
	}
	hitSet := make(map[string]bool)
	for _, id := range res.Retrieved {
		hitSet[id] = true
	}
	// 判定：期望片段任一出现在前 K 内即命中该 K
	for _, k := range kValues {
		top := res.Retrieved
		if len(top) > k {
			top = top[:k]
		}
		topSet := make(map[string]bool, len(top))
		for _, id := range top {
			topSet[id] = true
		}
		for _, eid := range s.ExpectedIDs {
			if topSet[eid] {
				res.Recall[k] = true
				break
			}
		}
	}
	return res
}

// evalAsk 单样本问答
func evalAsk(ctx context.Context, engine rag.Engine, s EvalSample, res EvalResult) EvalResult {
	sessionID := "eval-" + truncate(s.Question, 20)
	opts := []rag.AskOption{}
	if s.KBID != "" {
		opts = append(opts, rag.WithKBID(s.KBID))
	}
	out, err := engine.Ask(ctx, sessionID, s.Question, opts...)
	if err != nil {
		res.Error = "问答失败: " + err.Error()
		return res
	}
	res.Answer = out.Answer
	for _, src := range out.Sources {
		res.Sources = append(res.Sources, src.Filename)
	}
	return res
}

// evalJudge 单样本 LLM 指标
func evalJudge(ctx context.Context, judgeLLM llm.LLM, s EvalSample, res EvalResult, model string) EvalResult {
	if s.Answer != "" {
		score, err := JudgeAccuracy(ctx, judgeLLM, s.Question, res.Answer, s.Answer, model)
		if err != nil {
			res.Error = "准确性评分失败: " + err.Error()
			return res
		}
		res.Accuracy = &score
	}
	faithful, err := JudgeFaithfulness(ctx, judgeLLM, s.Question, res.Answer, nil, model)
	if err != nil {
		res.Error = "忠实度判定失败: " + err.Error()
		return res
	}
	res.Faithful = &faithful
	return res
}

// runConcurrent 样本级并发执行（上限 concurrency），单样本 panic 捕获
func runConcurrent(ctx context.Context, concurrency, n int, fn func(i int)) {
	if n <= 0 {
		return
	}
	if concurrency <= 0 {
		concurrency = 1
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)
	for i := 0; i < n; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			select {
			case <-ctx.Done():
				return
			default:
			}
			fn(i)
		}()
	}
	wg.Wait()
}

// errUnknownMode 未知模式错误
func errUnknownMode(m string) error {
	return &modeError{mode: m}
}

type modeError struct{ mode string }

func (e *modeError) Error() string {
	return "未知模式: " + e.mode + "（支持 retrieve / qa / full）"
}
