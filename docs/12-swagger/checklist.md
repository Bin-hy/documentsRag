# API Swagger 文档 Checklist

> 每一项通过运行代码或观察行为来验证，聚焦系统行为。

## 实现完整性
- [ ] 16 个 handler 全部有 swag 注解（验证：`swag init` 生成无告警；抽查各 handler 文件注解块存在）
- [ ] `/swagger/*` 路由已挂载（验证：`go build ./...` 通过，启动后 `GET /swagger/index.html` 200）
- [ ] docs 生成产物存在（验证：`internal/api/docs/` 含 docs.go / swagger.json / swagger.yaml）
- [ ] swag 依赖与 CLI 可用（验证：`swag --version` 输出版本）

## 文档内容
- [ ] OpenAPI 文档 paths 含全部 16 条接口（验证：`GET /swagger/doc.json` 后统计 paths 数量与 router.go 比对）
- [ ] 知识库 5 条接口文档完整（路径/方法/参数/响应，验证：抽查 doc.json 中 knowledge-bases 段）
- [ ] 文档 3 条接口文档完整（含 multipart 上传参数，验证：抽查 documents 段）
- [ ] 任务 2 条接口文档完整（验证：抽查 tasks 段）
- [ ] 问答 2 条接口文档完整（普通 JSON + SSE 分别描述，验证：抽查 chat 段）
- [ ] API Key 4 条接口文档完整（验证：抽查 api-keys 段）
- [ ] 统一响应格式 `{code,message,data}` 在文档中体现（验证：文档 Response 类型引用）
- [ ] 认证说明：安全定义与接口 `security` 标记存在（验证：doc.json 含 securityDefinitions / security）

## 集成
- [ ] 文档可交互：Swagger UI 带 API Key 可调用接口（验证：浏览器 `/swagger/index.html` 填 Key 调列表接口成功）
- [ ] 文档页本身无需认证即可查看（验证：无 Key 直接访问 `/swagger/index.html` 200）

## 编译与测试
- [ ] `go build ./...` 通过
- [ ] `go test ./...` 全部通过（注解不影响行为）
- [ ] `go vet ./...` 无告警
- [ ] `go mod tidy` 后依赖干净

## 端到端场景
- [ ] 场景 1（文档可访问）：启动服务 → `GET /swagger/index.html` 200 渲染 UI → `GET /swagger/doc.json` 返回 OpenAPI JSON（验证：curl + 浏览器）
- [ ] 场景 2（接口一致性）：doc.json 中随机抽 3 条接口，路径/方法/参数与代码一致，并用 curl 实测响应结构吻合（验证：比对 + 实测）
- [ ] 场景 3（认证实测）：带正确 Key 调用接口成功、无 Key 返回 401，与文档描述的 security 一致（验证：curl 两种请求）

## Spec 验收标准映射
- [ ] AC1（文档 URL 200）→ 端到端场景 1
- [ ] AC2（16 条接口齐全）→ 文档内容第 1 项
- [ ] AC3（注解无占位）→ 文档内容第 2-6 项
- [ ] AC4（认证说明 + 实操）→ 文档内容第 8 项 + 端到端场景 3
- [ ] AC5（统一响应说明）→ 文档内容第 7 项
- [ ] AC6（build/test/vet 通过）→ 编译与测试
- [ ] AC7（产物与代码同步）→ 实现完整性第 3 项 + 重新生成后 git diff 反映变更
