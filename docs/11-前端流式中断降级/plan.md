# 前端流式中断降级 Plan

## 架构概览

本次工作分三块，覆盖 spec 的 F1-F7：

1. **前端单元测试基建**：引入 vitest + @vue/test-utils，为 `chatStream`（SSE 解析）与 `chatStore.send/stop`（降级状态机）编写单元测试，用 mock fetch / mock 全局 fetch 构造各降级场景。这是「实测验证」的自动化主体——前端目前完全无测试框架。
2. **修复发现的问题**：运行单测与浏览器实测后，修正降级逻辑中不符合 spec 行为的地方（预期集中在：中断后内容保留语义、非 2xx 错误信息优先级、空流占位等）。
3. **后端场景构造辅助**：复用 `internal/api/api_test.go` 的 fake engine，为 SSE error 事件、空流、非 2xx 补充/确认测试，作为浏览器实测的对照（浏览器实测由用户本机完成）。

## 核心数据结构

### chatStream（frontend/src/api/chat.ts）
不改签名。测试时 mock 全局 `fetch` 返回可控制的 `ReadableStream`。

### chatStore（frontend/src/stores/chat.ts）
不改公开 API。测试覆盖 `send()` 的完整状态机：`streaming` 置位/复位、`messages` 追加、error 标记、abort 分支、`stop()` 调用 `abortController.abort()`。

### fake engine（internal/api/api_test.go 已有）
`StreamAsk` 已支持注入 `streamChunks` 与错误；补充 error 事件与空流注入点。

## 模块设计

### 前端测试基建（新增）
**职责：** 提供 vitest 运行环境与首个测试套件。
- `frontend/vitest.config.ts` — 基于 vite config，environment: jsdom（或 node + 手动 mock），resolve alias 与 src 一致
- `frontend/src/api/chat.test.ts` — mock fetch 构造：正常流（sources→chunk→done）、error 事件、非 JSON data 行、HTTP 非 2xx、无 body
- `frontend/src/stores/chat.test.ts` — 用 `createPinia`/`setActivePinia` 建 store，mock `chatStream` 或全局 fetch 构造：error 事件、abort 中断、网络失败、空流、done 后按钮状态
- `package.json` — 新增 `"test": "vitest run"` 脚本与 vitest 依赖

### 前端降级逻辑（可能修改）
**职责：** 修复实测暴露的问题（不确定项，测试先行）。
- `frontend/src/stores/chat.ts` send()：abort 中断时保留已输出内容且不标 error（F2）；error 事件/非 2xx 优先级（F4）；空流「（无回答）」占位（F5）
- `frontend/src/views/ChatView.vue`：若停止按钮状态流转有问题则修（F6）

### 后端 SSE 测试对照（补充）
**职责：** 为浏览器实测提供可信的后端场景来源。
- `internal/api/api_test.go`：补充 SSE error 事件用例、空流（无 chunk 直接 done）用例断言

## 模块交互

```
chat.test.ts ──mock fetch──> chatStream 解析（正常/error/非2xx）
chat.test.ts ──mock fetch──> chatStore.send 状态机（error/abort/空流/done）
        │
        └── 断言：messages 内容、error 标记、streaming 状态、按钮可用性
```

## 文件组织

```
frontend/
├── vitest.config.ts        — 测试配置
├── package.json            — test 脚本 + vitest 依赖
└── src/
    ├── api/chat.test.ts    — SSE 解析各场景
    └── stores/chat.test.ts — store 降级状态机各场景
internal/api/api_test.go    — 补充 SSE error / 空流用例
```

## 技术决策

| 决策点 | 选择 | 理由 |
|--------|------|------|
| 测试框架 | vitest | 与 vite 同生态、零配置接入、内置 fetch/stream mock 友好；项目已有 vite |
| 测试环境 | jsdom | 需要 DOM 行为（按钮状态断言可用真实组件或 store 状态）；store 测试也可用 node，视需要 |
| 覆盖主体 | chatStream 解析 + chatStore 状态机 | 降级逻辑全在这两层；视图层只做薄胶水，浏览器实测兜底 |
| 后端配合 | 仅补充 api_test 用例 | SSE 协议后端已实现，测试对照为主，不改后端行为 |
| 浏览器实测 | 用户本机 | 真实 fetch/流式/网络中断无法在单测完整模拟，AC 项保留人工确认 |
| 空流/error 注入 | mock fetch 返回预制流 | 无需真实后端即可精确控制事件序列 |
