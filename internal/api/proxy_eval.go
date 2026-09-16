package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/gin-gonic/gin"
)

// evalInternalTokenHeader Go→Python 共享内部令牌头（Python 侧中间件校验，缺失/错误 401）
const evalInternalTokenHeader = "X-Eval-Internal-Token"

// evalHealthPath 评测健康检查路径（豁免用户鉴权，见 router.go 挂载说明）
const evalHealthPath = "/api/v1/eval/health"

// evalTaskCreatePath 评测任务提交路径（代理层唯一业务逻辑：kb_id 越权校验）
const evalTaskCreatePath = "/tasks"

// evalTaskCreateBody 任务提交 body 中需要解析的字段（仅取 kb_id 做越权校验，其余字段原样透传）
type evalTaskCreateBody struct {
	KBID string `json:"kb_id"`
}

// EvalProxy 评测服务（ragas-eval）反向代理。
// 纯透传 /api/v1/eval/*（路径/查询串/Body 不改写），注入 X-Eval-Internal-Token 头；
// 唯一业务逻辑：POST /api/v1/eval/tasks 解析 body 中 kb_id 做越权校验（越权 404，与 GetKB 同款语义）。
// 配置（eval.service_url / eval.internal_token）为空时整个代理组 503「评测服务未配置」。
//
//	@Summary		评测服务代理
//	@Description	将 /api/v1/eval/* 原样反向代理到 ragas-eval 评测微服务；未配置评测服务时返回 503
//	@Tags			评测
//	@Success		200	{object}	Response
//	@Failure		401	{object}	Response
//	@Failure		404	{object}	Response
//	@Failure		503	{object}	Response
//	@Security		ApiKeyAuth
//	@Router			/api/v1/eval/{path} [get]
//	@Router			/api/v1/eval/{path} [post]
//	@Router			/api/v1/eval/{path} [delete]
func (h *handler) EvalProxy(c *gin.Context) {
	// 请求级配置快照（热重载一致性）；未配置评测服务 → 503
	snap := h.cfgSnapshot()
	if snap == nil || !snap.Eval.Available() {
		Fail(c, CodeServiceUnavailable, "评测服务未配置")
		return
	}
	target, err := url.Parse(snap.Eval.ServiceURL)
	if err != nil {
		slog.Error("评测服务地址解析失败", "url", snap.Eval.ServiceURL, "err", err)
		Fail(c, CodeInternal, "评测服务地址配置非法")
		return
	}

	// POST /api/v1/eval/tasks 特判：解析 body 中 kb_id 做越权校验（提交时收口，Python 信任已校验请求）。
	// body 读取后必须复原，供反向代理原样透传。
	if c.Request.Method == http.MethodPost && c.Param("path") == evalTaskCreatePath {
		if !h.checkEvalTaskKB(c) {
			return
		}
	}

	token := snap.Eval.InternalToken
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			// 纯透传：仅改写目标 scheme/host，路径与查询串保持原样；Body 不动
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host
			req.Header.Set(evalInternalTokenHeader, token)
		},
		ErrorHandler: func(rw http.ResponseWriter, _ *http.Request, err error) {
			slog.Warn("评测服务代理失败", "path", c.Request.URL.Path, "err", err)
			rw.Header().Set("Content-Type", "application/json; charset=utf-8")
			rw.WriteHeader(CodeBadGateway)
			_ = json.NewEncoder(rw).Encode(Response{Code: CodeBadGateway, Message: "评测服务不可用: " + err.Error()})
		},
	}
	proxy.ServeHTTP(c.Writer, c.Request)
}

// checkEvalTaskKB 校验任务提交 body 中的 kb_id 访问权（越权/不存在 → 404，与 ensureKBAccess 语义一致）。
// 无论校验结果如何，body 都会复原供透传；返回 false 表示已响应错误，调用方应终止。
func (h *handler) checkEvalTaskKB(c *gin.Context) bool {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		Fail(c, CodeBadRequest, "读取请求体失败")
		return false
	}
	// 复原 body，供反向代理原样透传
	c.Request.Body = io.NopCloser(bytes.NewReader(body))

	var payload evalTaskCreateBody
	if err := json.Unmarshal(body, &payload); err != nil {
		// body 非合法 JSON：不在代理层拦截，交由 Python 侧返回 400/422（纯透传原则）
		return true
	}
	if payload.KBID == "" {
		return true // 未指定知识库：由 Python 侧校验必填性
	}
	if !h.ensureKBAccess(c, payload.KBID) {
		Fail(c, CodeNotFound, "知识库不存在")
		return false
	}
	return true
}
