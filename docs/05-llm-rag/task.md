# LLM 集成与 RAG 编排 Tasks

## 文件清单

| 操作 | 文件 | 职责 |
|------|------|------|
| 修改 | `internal/config/config.go` | 新增 LLMConfig、RAGConfig 结构体 + 默认值 |
| 修改 | `configs/config.yaml` | 新增 llm、rag 配置段示例 |
| 新建 | `internal/llm/llm.go` | Message、ChatOptions、LLM 接口、openaiLLM 实现 |
| 新建 | `internal/llm/stream.go` | SSE 流式解析 + StreamGenerate |
| 新建 | `internal/llm/llm_test.go` | LLM 客户端测试（httptest mock） |
| 新建 | `internal/rag/history.go` | HistoryStore 接口 + memoryHistoryStore |
| 新建 | `internal/rag/context.go` | Source 类型、buildContext、token 估算 |
| 新建 | `internal/rag/prompt.go` | 默认模板常量 + 渲染 |
| 新建 | `internal/rag/engine.go` | Engine 接口、RAGEngine、Ask / StreamAsk |
| 新建 | `internal/rag/history_test.go` | 历史容量/并发测试 |
| 新建 | `internal/rag/context_test.go` | 上下文截断测试 |
| 新建 | `internal/rag/prompt_test.go` | 模板渲染测试 |
| 新建 | `internal/rag/engine_test.go` | 编排链路测试（mock retriever + mock llm） |

## T1: 配置扩展

**文件：** `internal/config/config.go`、`configs/config.yaml`
**依赖：** 无
**步骤：**
1. `Config` 结构体新增 `LLM LLMConfig \`yaml:"llm"\`` 和 `RAG RAGConfig \`yaml:"rag"\`` 字段
2. 定义 `LLMConfig`（BaseURL/APIKey/Model/Temperature/MaxTokens/MaxRetries/QPS/Timeout，字段名与 plan.md 一致）
3. 定义 `RAGConfig`（TopK/MaxContextTokens/MaxChunks/EnableRewrite/HistoryCapacity/HistoryLimit/SystemPromptPath/ContextTemplatePath/RewriteTemplatePath）
4. `applyDefaults` 补齐：LLM.MaxRetries=3、QPS=10、Timeout=60、Temperature=0.7、MaxTokens=2048；RAG.TopK=5、MaxContextTokens=2048、MaxChunks=5、EnableRewrite=true、HistoryCapacity=50、HistoryLimit=10
5. `configs/config.yaml` 追加 `llm:` 与 `rag:` 两个配置段示例（带注释，风格与现有段落一致）

**验证：** `go build ./internal/config/...` 编译通过；`go vet ./internal/config/...` 无告警

## T2: llm 包基础（接口 + Generate）

**文件：** `internal/llm/llm.go`
**依赖：** T1
**步骤：**
1. 定义 `Message{Role, Content}`、角色常量 `RoleSystem/RoleUser/RoleAssistant`
2. 定义 `ChatOptions`（Model/Temperature/MaxTokens）与 `ChatOption` 函数式选项（`WithModel`、`WithTemperature`、`WithMaxTokens`）
3. 定义 `LLM` 接口：`Generate(ctx, messages, opts...) (string, error)` 和 `StreamGenerate(ctx, messages, opts...) (<-chan StreamChunk, error)`
4. 定义 `StreamChunk{Content string, Done bool}`
5. 实现 `openaiLLM`（client + rate.Limiter + config.LLMConfig）：`NewLLM(cfg) LLM` 构造函数
6. 实现 `Generate`：POST `{base_url}/v1/chat/completions`，请求体含 model/messages/temperature/max_tokens/stream:false；无 APIKey 时省略 Authorization 头
7. 实现重试：网络错误/429/5xx 指数退避（复用 embedding 的 RetryableError 模式），4xx 直接失败；限流用 `limiter.Wait`
8. 响应解析：取 `choices[0].message.content`；空 choices 视为错误

**验证：** `go build ./internal/llm/...` 编译通过

## T3: llm 流式（SSE 解析 + StreamGenerate）

**文件：** `internal/llm/stream.go`
**依赖：** T2
**步骤：**
1. 实现 `parseSSE(reader, out chan<- StreamChunk)`：按行读，解析 `data: {...}`，取 `choices[0].delta.content` 发增量；`data: [DONE]` 发 `Done` 片段
2. 实现 `StreamGenerate`：请求体 `stream:true`，返回 `<-chan StreamChunk`；**首个 delta 前**失败走重试（限流+重建连接），收到 delta 后失败直接发错误并关闭通道，不重试
3. 上下文取消（ctx.Done）时关闭通道

**验证：** `go build ./internal/llm/...` 编译通过

## T4: llm 客户端测试

**文件：** `internal/llm/llm_test.go`
**依赖：** T2、T3
**步骤：**
1. 用 `httptest.NewServer` 搭 mock：校验请求体（model/messages/stream 标志/温度），按场景返回响应
2. 覆盖：Generate 成功返回完整文本（AC1）；流式增量拼合与普通生成一致（AC2）；config 的 model/base_url/temperature 生效（AC3，断言请求体）
3. 覆盖重试：先 500 后成功、先 429 后成功（AC4）；400 直接失败不重试（断言只收到一次请求）
4. 无 APIKey 时请求不含 Authorization 头；有 APIKey 时包含

**验证：** `go test ./internal/llm/ -v` 全部通过

## T5: rag 对话历史

**文件：** `internal/rag/history.go`
**依赖：** T2（使用 llm.Message）
**步骤：**
1. 定义 `HistoryStore` 接口：`Append(sessionID, role, content) error`、`Get(sessionID, limit) ([]llm.Message, error)`、`Clear(sessionID) error`
2. 实现 `memoryHistoryStore`：`map[string][]llm.Message` + RWMutex；`NewMemoryHistoryStore(capacity int) HistoryStore`
3. `Append` 超容量丢弃最旧消息；`Get` 返回最近 limit 条（不足返回全部，limit<=0 返回全部）

**验证：** `go build ./internal/rag/...` 编译通过

## T6: rag 上下文组装

**文件：** `internal/rag/context.go`
**依赖：** 无
**步骤：**
1. 定义 `Source{ID, Filename, Heading string, Score float32}`
2. 实现 token 估算函数 `estimateTokens(text string) int`（中文字符 2、英文按词 1、标点 1，参考 chunker 思路）
3. 实现 `buildContext(chunks []retriever.RetrieveResult, maxTokens, maxChunks int) (string, []Source)`：按序累加，超预算或达上限停止；从 Metadata 提取 `filename`/`heading_context` 组装 Source

**验证：** `go build ./internal/rag/...` 编译通过

## T7: rag Prompt 模板

**文件：** `internal/rag/prompt.go`
**依赖：** T1（模板路径配置）、T2（llm.Message）
**步骤：**
1. 定义内置默认模板常量：系统提示词（严格基于资料回答、不足答「未找到相关资料」、按 [编号] 标注引用）、上下文注入（`[1]（来源：{filename} / {heading}）` + 内容）、改写模板（结合历史消解指代、仅输出改写结果）
2. 实现渲染函数：`renderSystem(...)`、`renderContext(items, 模板) (string, error)`、`renderRewrite(历史, 问题, 模板) (string, error)`，用 text/template
3. 模板来源：配置路径文件存在则读文件，否则用内置默认；读文件/渲染失败降级默认模板（返回错误由调用方决定，engine 中告警降级）

**验证：** `go build ./internal/rag/...` 编译通过

## T8: RAGEngine 编排

**文件：** `internal/rag/engine.go`
**依赖：** T1、T2、T5、T6、T7（retriever 为现有包）
**步骤：**
1. 定义 `RAGResult{Answer string, Sources []Source}`、`StreamEvent{Type, Content, Sources, Err}`、`EventType` 常量（EventSources/EventChunk/EventDone/EventError）
2. 定义 `Engine` 接口：`Ask(ctx, sessionID, question) (*RAGResult, error)`、`StreamAsk(ctx, sessionID, question) (<-chan StreamEvent, error)`
3. 实现 `RAGEngine` 与 `NewEngine(cfg, llm, rt, hs) Engine`
4. 实现 `Ask` 七步：历史 Get → 改写（EnableRewrite 时，失败降级原问题+日志）→ retriever.Search（TopK）→ buildContext → 渲染 `[system]+历史+[user: 上下文+原问题]` → llm.Generate → 成功后 Append 问题与回答
5. 检索结果为空：不调用 LLM 生成，直接返回「未找到相关资料」类回答（Sources 为空）
6. 实现 `StreamAsk`：事件序列 EventSources → EventChunk×N → EventDone；任一步失败发 EventError 并关闭通道
7. 各阶段耗时与关键数据（改写查询、上下文 token 数、引用数量）打日志

**验证：** `go build ./...` 编译通过

## T9: rag 单元测试

**文件：** `internal/rag/history_test.go`、`context_test.go`、`prompt_test.go`、`engine_test.go`
**依赖：** T5、T6、T7、T8
**步骤：**
1. history_test：Append 超容量丢最旧；Get 限条数；并发 Append+Get 无竞争（AC9、N2）
2. context_test：超预算截断；maxChunks 生效；Source 元数据提取正确（AC8）
3. prompt_test：默认模板渲染输出正确；自定义模板渲染生效（AC11）
4. engine_test：mock retriever（自建 fake 实现 Retriever 接口）+ mock llm（fake 实现 LLM 接口）
   - Ask 返回回答与引用来源对应检索结果（AC6）
   - 改写调用携带历史上下文、改写查询用于检索（AC5）
   - 检索空返回「未找到相关资料」不 panic（AC10）
   - StreamAsk 事件序列正确且引用与 Ask 一致（AC7）
   - 模板替换后系统提示词变化（AC11）
   - 改写失败降级原问题继续检索（N3）

**验证：** `go test ./internal/rag/... -race` 全部通过

## T10: 全量验证

**文件：** 无新增
**依赖：** T1-T9
**步骤：**
1. `go build ./...` 全项目编译通过
2. `go test ./...` 全部测试通过（含既有 loader/chunker/retriever 等测试不回归）
3. `go test -race ./internal/llm/... ./internal/rag/...` 无数据竞争
4. `go vet ./...` 无告警

**验证：** 上述命令全部通过

## 执行顺序

```
T1（配置）
 ├→ T2（llm.go）→ T3（stream.go）→ T4（llm_test）
 ├→ T5（history.go）
 ├→ T6（context.go）
 └→ T7（prompt.go）
        ↓（T2-T7 齐）
T8（engine.go）→ T9（rag 测试）→ T10（全量验证）
```

T5、T6、T7 在 T2 完成后可并行。
