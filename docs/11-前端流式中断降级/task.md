# 前端流式中断降级 Tasks

## 文件清单

| 操作 | 文件 | 职责 |
|------|------|------|
| 新建 | `frontend/vitest.config.ts` | vitest 测试配置 |
| 修改 | `frontend/package.json` | 新增 test 脚本 + vitest 依赖 |
| 新建 | `frontend/src/api/chat.test.ts` | SSE 解析各场景单测 |
| 新建 | `frontend/src/stores/chat.test.ts` | store 降级状态机单测 |
| 修改 | `frontend/src/stores/chat.ts` | 修复实测暴露的降级问题（若需） |
| 修改 | `frontend/src/views/ChatView.vue` | 停止按钮状态流转修复（若需） |
| 修改 | `internal/api/api_test.go` | 补充 SSE error / 空流用例 |

## T1: vitest 测试基建

**文件：** `frontend/vitest.config.ts`、`frontend/package.json`
**依赖：** 无
**步骤：**
1. `npm install -D vitest @vue/test-utils jsdom`（frontend 目录）
2. 新建 `vitest.config.ts`：基于 vite 配置，`test: { environment: 'jsdom', globals: true }`
3. `package.json` scripts 新增 `"test": "vitest run"`
4. 确认 `tsconfig.json` 的 types 兼容（必要时加 `"types": ["vitest/globals"]`）

**验证：** `npm test` 能运行（无测试文件时报 "No test files found" 属正常，exit code 允许非 0 时以 `|| true` 确认基建就绪）

## T2: chatStream SSE 解析单测

**文件：** `frontend/src/api/chat.test.ts`
**依赖：** T1
**步骤：**
1. 编写 mock fetch 工具：返回 `{ ok, status, body: ReadableStream }`，body 由给定的事件字符串数组构造
2. 用例 1（正常流）：`event:sources` → `event:chunk`×2 → `event:done`，断言回调收到 sources/chunk/done 且顺序正确
3. 用例 2（error 事件）：流中收到 `event:error`，断言 `{ type: 'error', message }`
4. 用例 3（非 JSON data 行）：混入非法行，断言被跳过不抛错、后续事件仍解析
5. 用例 4（HTTP 非 2xx）：fetch 返回 500 + JSON body，断言抛出 Error 且 message 取自后端 message
6. 用例 5（无 body / ok=false）：断言抛出 Error
7. 用例 6（中止）：传入已 abort 的 signal，断言抛 AbortError（或按实现约定）

**验证：** `npm test -- chat` 该文件用例全绿

## T3: chatStore 降级状态机单测

**文件：** `frontend/src/stores/chat.test.ts`
**依赖：** T2（mock fetch 模式可复用）
**步骤：**
1. `setActivePinia(createPinia())` 建 store；mock 全局 fetch 或 mock `chatStream`
2. 用例 1（error 事件）：流中 error → 断言 `messages[last].error === true`、content 为错误信息、`streaming === false`
3. 用例 2（abort 中断）：流中调用 `stop()` → 断言已收到的 chunk 内容保留、无 error 标记、`streaming === false`
4. 用例 3（网络失败）：fetch reject → 断言消息标记 error、content 为「请求失败…」或已有内容保留
5. 用例 4（空流）：直接 done 无 chunk → 断言 content 为「（无回答）」、无 error
6. 用例 5（正常 done）：sources+chunk+done → 断言 content 完整、无 error、`streaming === false`
7. 用例 6（按钮状态）：`streaming` 在发送期间 true、结束后 false（驱动 ChatView 停止/发送按钮显隐）

**验证：** `npm test -- stores/chat` 该文件用例全绿

## T4: 修复降级逻辑问题

**文件：** `frontend/src/stores/chat.ts`、`frontend/src/views/ChatView.vue`
**依赖：** T3
**步骤：**
1. 运行 T2/T3 全部用例，找出失败用例对应的问题
2. 按失败原因修复：abort 中断保留内容且不标 error、非 2xx 信息优先级、空流占位、按钮状态流转等（按 spec F1-F7）
3. 修复后重跑 T2/T3 直至全绿
4. 若 ChatView 停止按钮状态需改（如 streaming 与按钮绑定），同步修改

**验证：** `npm test` 全部用例通过；`npm run build` 通过

## T5: 后端 SSE 测试对照

**文件：** `internal/api/api_test.go`
**依赖：** 无（独立于前端）
**步骤：**
1. 阅读现有 SSE 用例（`streamChunks` 注入方式，约 604-622 行）
2. 新增用例 A：fake engine `StreamAsk` 发送 error 事件，断言响应含 `event:error` 与 message
3. 新增用例 B：空流（无 chunk 直接 done），断言响应含 `event:sources`（空）+ `event:done`
4. 保持既有用例不回归

**验证：** `go test ./internal/api/ -run SSE -v` 通过（或按用例名匹配）

## T6: 全量验证

**文件：** 无新增
**依赖：** T1-T5
**步骤：**
1. `cd frontend && npm test` 全部单测通过
2. `cd frontend && npm run build` 构建通过
3. `go build ./...` + `go test ./...` + `go vet ./...` 后端全绿
4. 汇总「浏览器实测待确认」清单（error/中断/断网/非2xx/空流 + 控制台无异常），交用户本机验证

**验证：** 上述命令全部通过；清单交付

## 执行顺序

```
T1 → T2 → T3 → T4
        T5（可并行）
T4/T5 → T6
```
