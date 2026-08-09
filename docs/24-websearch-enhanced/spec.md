# 增强模式与 websearch（function calling）Spec

## 背景

项目已具备多数据源框架（LLM 路由判定数据源，见 docs/23-multi-datasource），但 web_search 仅为占位。本次需求：

1. 前端 UI 提问中提供「增强」可选项（增强面板，多能力预留）
2. 支持 websearch，作为 **tool-use（真 function calling）** 封装
3. 搜索后端：博查 Bocha（国内，第一版），设计为可插拔 Search Provider

## 目标

- LLM 层支持 OpenAI 风格 function calling（tools 参数 + tool_calls 响应 + 流式聚合）
- 可插拔搜索 Provider 抽象，博查为第一版实现
- RAG 增强模式：LLM 在生成时可调用 web_search 工具，工具结果回传后继续生成（tool loop）
- 前端增强面板开关，请求透传增强标记；思考链路展示工具调用

## 功能需求

- F1: LLM function calling——请求支持 tools 参数，响应解析 tool_calls，流式 delta.tool_calls 聚合
- F2: 可插拔搜索 Provider——统一接口（名称/可用性/搜索），博查实现，工厂按配置选择
- F3: web_search 工具——参数（query/count），执行搜索并格式化为文本回传 LLM
- F4: 增强模式 tool loop——LLM 可多次请求工具（上限 3 轮防死循环），工具结果以 role=tool 消息回传
- F5: 无工具/工具不可用时回退普通生成（行为不变，回归兼容）
- F6: 前端增强面板——「联网搜索」开关（多能力预留），请求携带 enhanced 标记
- F7: 增强能力列表接口（GET /api/v1/chat/enhancements）——前端动态渲染能力及可用性
- F8: 思考链路 StepTool——工具名/参数/结果摘要/错误随 thinking 事件输出，前端渲染

## 非功能需求

- N1: 未开启增强时，现有生成路径完全不变（回归兼容）
- N2: 流式增强模式：先静默完成 tool loop，最终回答一次性输出（SSE 事件序列保持 thinking → sources → chunk → done）
- N3: 新增搜索后端 = 实现 Provider + 工厂注册，不改 RAG 核心
- N4: 配置向后兼容，新增字段均可选

## 不做的事

- 不实现多工具并行调用（串行 tool loop）
- 不实现流式阶段的 tool_calls 动态展示（增强模式先非流式完成工具循环）
- 不做搜索结果持久化/缓存
- 不做网页正文抓取（依赖博查返回 content/summary）

## 验收标准

- AC1: LLM 层 tools 请求体正确、tool_calls 解析正确、流式聚合正确（单测）
- AC2: 博查 Provider 请求/响应正确，未配置 key 时不可用（单测）
- AC3: 增强模式 tool loop 多轮执行、工具结果进入生成上下文（单测）
- AC4: 无工具时回退普通生成，行为不变（单测）
- AC5: API enhanced 字段透传、enhancements 接口返回能力列表（单测）
- AC6: 前端增强开关请求体带 enhanced、StepTool 渲染（vitest + 构建）
- AC7: 全量回归通过（gofmt/build/vet/test/race/前端 build+test）
