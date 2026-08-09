# 增强模式与 websearch（function calling）Checklist

> 验收结果：全部通过（2026-08-09，基于单元测试与全量回归，真实博查/联网链路待配置 api_key 后人工验证）

## 实现完整性

- [x] LLM 层 tools 请求体正确（验证：`TestGenerateTool_ToolCallsParsed` 断言请求含 tools/function 定义）
- [x] tool_calls 响应解析正确（验证：`TestGenerateTool_ToolCallsParsed` 解析 ID/name/arguments）
- [x] 流式 delta.tool_calls 聚合（验证：`TestStreamGenerate_ToolCallsAggregated` 分片拼接）
- [x] 博查 Provider 请求/响应（验证：`TestBochaSearch` 断言 Authorization/请求体/结果解析）
- [x] 未配置 api_key 时不可用（验证：`TestBochaSearchNoAPIKey`）
- [x] 增强模式 tool loop 多轮（验证：`TestAsk_EnhancedToolLoop`——LLM 请求工具 → 执行 → 结果回传 → 最终回答）
- [x] 未知工具错误回传（验证：`TestAsk_EnhancedUnknownTool` + 思考链路 Error 记录）
- [x] 无工具回退普通生成（验证：`TestAsk_EnhancedNoToolsFallback`）

## 集成

- [x] API enhanced 透传（验证：`TestChatEnhancedPassThrough` / `TestChatEnhancedDefaultOff`）
- [x] 增强能力列表接口（验证：`TestChatEnhancements` 返回 web_search 能力）
- [x] 前端增强开关请求体携带 enhanced（验证：vitest 新增 2 用例，9/9 通过）
- [x] 前端 StepTool 渲染（验证：`vue-tsc --noEmit` 通过 + ThinkingPanel tool 分支）
- [x] app 装配注入搜索 Provider（验证：`go build ./...`）

## 编译与测试

- [x] gofmt 无未格式化文件（`gofmt -l internal/` 为空）
- [x] 项目编译无错误（`go build ./...`）
- [x] go vet 无告警（`go vet ./...`）
- [x] 全量单元测试通过（`go test ./...`）
- [x] race 检测通过（`go test -race` store/task/api/eval）
- [x] 前端构建 + 测试通过（`npm run build` + `npm test` 15/15）

## 端到端场景

- [x] 场景 1（增强开启）：提问 → thinking 含工具调用（StepTool）→ 回答基于搜索结果（验证：`TestAsk_EnhancedToolLoop` 断言 Answer + StepTool Data）
- [x] 场景 2（增强关闭/无 key）：普通提问 → 无工具 → 行为与现状一致（验证：`TestAsk_EnhancedNoToolsFallback` + `TestChatEnhancedDefaultOff`）
- [x] 场景 3（前端）：勾选「增强：联网搜索」→ 请求体 enhanced=true → SSE 收到 thinking(tool) → 渲染（验证：vitest 断言请求体 + ThinkingPanel tool 分支渲染逻辑）
- [ ] 场景 4（真实链路，待人工）：配置博查 api_key → 启动服务 → 勾选增强提问实时问题 → 回答引用网络来源（需真实 key 与联网，未在自动化中执行）
