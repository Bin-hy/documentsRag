# 扫描版 PDF 与无效内容识别拒绝 Checklist

> 每一项通过运行代码或观察行为来验证，聚焦系统行为。

## 实现完整性
- [ ] 可读字符统计与判定已实现（验证：`go test ./internal/loader/ -run TestValidate -v` 通过）
- [ ] ErrNoReadableContent 错误类型存在且文案明确（验证：错误信息含「无可读文本」与可读字符/阈值）
- [ ] LoaderConfig 配置项生效（验证：`go build ./internal/config/...` 通过，config.yaml 含 loader 段）
- [ ] pipeline Ingest 调用判定（验证：`go test ./internal/pipeline/` 通过）
- [ ] API 上传预检已接入（验证：`go test ./internal/api/` 通过）

## 判定行为
- [ ] 纯乱码/图像指令内容判定为无可读文本（验证：validate_test 用例）
- [ ] 空 Blocks 判定为无可读文本（验证：validate_test 用例）
- [ ] 正常中文/英文文本通过判定（验证：validate_test 用例）
- [ ] min_readable_chars=0 时禁用判定（验证：validate_test 用例）

## 拒绝链路
- [ ] 上传扫描件/空文件返回 400 且不创建任务（验证：api_test 用例；curl 实测响应）
- [ ] 无可读文本文档不写入向量库与 BM25（验证：拒绝后 Qdrant 点数不增、检索不到）
- [ ] worker 将无可读文本任务置为 failed 且 error_message 含原因（验证：worker_test 或集成观察）
- [ ] 空文档不再静默成功（原返回 nil,nil 改报错）（验证：pipeline_test 断言）

## 集成
- [ ] app 装配传递 LoaderConfig（验证：`go build ./...` 通过）
- [ ] 正常文本 PDF/TXT 入库不受影响，任务 completed（验证：上传正常文件观察）

## 编译与测试
- [ ] `go build ./...` 通过
- [ ] `go test ./...` 全部通过（含新增用例）
- [ ] `go vet ./...` 无告警

## 端到端场景
- [ ] 场景 1（扫描件拒绝）：上传地下空间扫描 PDF → 返回 400「无可读文本」，任务列表无记录（验证：curl/浏览器）
- [ ] 场景 2（正常入库）：上传文本 TXT/MD → 任务 completed，检索可命中（验证：浏览器 + curl 检索）
- [ ] 场景 3（前端可见）：任务失败原因在文档列表/任务详情展示（验证：用户本机浏览器观察）

## Spec 验收标准映射
- [ ] AC1（上传扫描 PDF 400）→ 拒绝链路第 1 项 + 端到端场景 1
- [ ] AC2（任务 failed + 原因）→ 拒绝链路第 3 项 + 端到端场景 3
- [ ] AC3（正常 PDF 正常入库）→ 集成第 2 项 + 端到端场景 2
- [ ] AC4（空/乱码拒绝）→ 判定行为第 1/2 项 + 拒绝链路第 1 项
- [ ] AC5（不写存储）→ 拒绝链路第 2 项
- [ ] AC6（前端可见原因）→ 端到端场景 3
- [ ] AC7（阈值可配置）→ 实现完整性第 3 项 + 判定行为第 4 项
- [ ] AC8（build/test/vet）→ 编译与测试
