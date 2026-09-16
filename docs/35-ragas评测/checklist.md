# RAGAS 评测系统 Checklist

> 依据 spec.md 验收标准（AC1–AC11）与 plan.md 架构设计生成。每项通过运行代码或观察行为验证。

## 实现完整性

- [ ] Python 评测服务可启动（验证：`cd services/ragas-eval && uv sync && uv run uvicorn ragas_eval.main:app`，进程正常监听）
- [ ] 依赖锁定可复现（验证：删除 .venv 后 `uv sync --frozen` 成功）
- [ ] 14 个 `/api/v1/eval/*` 端点全部可访问且响应为 `{code,message,data}` 包装（验证：逐端点 curl 或 pytest 接口测试）
- [ ] 任务状态机合法流转（验证：提交任务观察 pending→collecting→evaluating→completed；非法迁移接口返回 409）
- [ ] 四套中文评审提示词生效且输出版本号（验证：报告 config_snapshot.prompt_version = "zh-v1"，样本判定理由为中文）
- [ ] Go chat 接口 include_contexts 出口生效（验证：`POST /api/v1/chat?include_contexts=true` 响应 sources 含 content；不带参数时不含）
- [ ] Go 代理纯透传并注入内部令牌（验证：Python 侧日志可见 X-Eval-Internal-Token 头）
- [ ] 前端四个页面与路由可用（验证：访问 /eval、/eval/new、/eval/:id、/eval/compare?ids= 均正常渲染）

## 集成

- [ ] 前端 → Go 代理 → Python 全链路打通（验证：前端发起评测，Python 日志出现对应 task_id）
- [ ] Python → Go 采集回调打通（验证：评测运行中 Go 访问日志出现评测专用 Key 的 /api/v1/chat 调用）
- [ ] 前端经 Go 代理自动获得鉴权保护（验证：无凭据请求 /api/v1/eval/tasks 返回 401）
- [ ] kb 越权校验在代理层生效（验证：用户 A 提交指向用户 B 知识库的评测任务返回 404）
- [ ] 直连 Python 服务不可达或无令牌 401（验证：绕过 Go 直接请求 Python 端口被拒/401）——AC9
- [ ] 前端轮询与后端进度字段对齐（验证：运行中任务详情页进度条随 collected/evaluated 增长）

## 功能行为（对齐验收标准）

- [ ] AC1：服务启动 + 健康检查返回 status ok（验证：curl /api/v1/eval/health）
- [ ] AC2：合法数据集上传成功可预览；错误数据集 422 且 message 带行号（验证：各传一份正确/错误 JSONL）
- [ ] AC3：任务状态流转可见、进度可查（验证：≥5 条样本数据集发起评测并轮询任务详情）
- [ ] AC4：报告含四指标汇总分数与逐样本明细（分数+判定理由）（验证：查看完成任务报告与单样本下钻）
- [ ] AC5：无标准答案数据集自动降级——context_recall 记 N/A 并在 summary 标注覆盖率，其余指标正常（验证：无 answer 字段数据集评测后查看报告）
- [ ] AC6：单样本失败不中断任务，报告标记该样本错误（验证：数据集含指向不存在知识库的样本，任务 completed 且 progress.failed=1）
- [ ] AC7：取消运行中任务生效（验证：评测中途 POST cancel，状态变 canceled，后续样本不再执行）
- [ ] AC8：前端评测中心完整流程（验证：上传数据集→发起→进度→报告可视化→单样本下钻→勾选两个已完成任务对比）
- [ ] AC10：解析失败率统计上报（验证：报告含 parse_failure_rate 字段；>5% 时结论标记不可靠）
- [ ] 幂等提交：相同 Idempotency-Key 重复 POST /tasks 返回同一任务（验证：带 key 连发两次，task_id 相同）
- [ ] 重启恢复：kill 运行中的 Python 服务并重启，任务重排队且已完成样本不重复评测（验证：重启后任务续跑完成，样本数不重复计数）
- [ ] 多次评测对比：批量接口返回各任务 summary+config_snapshot，口径不一致时前端显示警告条（验证：用不同评审模型跑两次后对比）

## 编译与测试

- [ ] Go：`go build ./...`、`go test ./...`、`go vet ./...` 全部通过——AC11
- [ ] Python：`uv run pytest` 全部通过、`uv run ruff check` 无错误——AC11
- [ ] 前端：`pnpm build` 通过；echarts 不进主包（验证：构建产物中 echarts 在评测路由分包内）

## 端到端场景

- [ ] 场景 1（完整闭环）：docker compose up 起全栈 → 前端登录 → 评测中心上传含 8 条样本的数据集（含 1 条无 reference、1 条坏 kb_id）→ 选择四指标发起评测 → 观察进度面板至完成 → 报告页四指标分数、6 条有效样本、1 条 N/A、1 条错误标记 → 下钻低分样本查看上下文与判定理由（预期：全流程无报错，报告数据自洽）
- [ ] 场景 2（容错与恢复）：评测进行中取消任务（预期：状态 canceled，部分结果可查并标注「已取消，结果为部分数据」）；再次发起并中途重启 ragas-eval 容器（预期：任务自动续跑至完成）
- [ ] 场景 3（对比调优）：修改 Go 侧检索策略配置后对同一数据集再跑一次评测 → 对比视图查看两次四指标差异（预期：并排表与雷达图正确渲染，config_snapshot 一致无警告）
