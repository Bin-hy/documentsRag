# BinRag 官网与文档站 Checklist

> 每一项通过运行构建或观察产物验证，聚焦站点行为与交付质量。

## 实现完整性（官网首页）

- [ ] 首页渲染完整：Hero（产品名 / 标语 / tagline / 双 CTA 链接正确）（验证：`npm run preview` 访问 `/`，检查 hero 区与 CTA href）
- [ ] 特性墙 8 个卡片可见（图标 / 标题 / 描述）（验证：首页 features 区）
- [ ] 架构图区展示（前端/API+MCP/入库/问答链路）（验证：`<ArchitectureDiagram />` 渲染）
- [ ] MCP / OIDC 亮点双卡（验证：`<HighlightBlock />` 内容正确）
- [ ] 三步上手（启动基础设施 / 配置 / 构建启动）（验证：`<Steps />` 序号与描述）
- [ ] 页脚 / 技术栈徽标区（验证：底部区域）

## 实现完整性（文档站）

- [ ] 侧边栏 7 章节完整：快速开始 / 核心概念 / 部署 / API 参考 / MCP Server / 登录认证 / RAG 评估（验证：任意 guide 页左侧 sidebar）
- [ ] 每篇文档页有内容（非空页 / 无 TODO 占位）（验证：逐页打开）
- [ ] `getting-started.md`：前置条件 / docker compose / config.local / 构建启动 / 第一个请求 curl（验证：对照 README 检查完整性）
- [ ] `mcp.md`：配置 / 认证授权（401 / -32001 / 双层开关）/ 6 Tool / 客户端示例 / 用户凭据（验证：与 docs/26 实现一致）
- [ ] `auth.md`：API Key / OIDC 配置（providers/回调地址）/ GitHub / 会话 JWT（验证：与 config.yaml 注释一致）
- [ ] `api.md`：REST 接口表 + `/mcp` 端点 + 统一响应格式（验证：与 router.go 路由一致）
- [ ] `concepts.md` / `deployment.md` / `eval.md` 内容完整（验证：逐页对照 README）

## 主题与视觉

- [ ] 品牌色生效：`--vp-c-brand-1` 映射靛蓝 `#4f5bd5`（验证：dev 检查 computed 样式）
- [ ] 暗色模式可用且品牌色切换为 `#7d85ee`（验证：切换外观按钮，检查按钮/链接/组件配色）
- [ ] Hero 名称渐变文字、卡片 hover 效果（验证：观察样式）
- [ ] 组件在暗色/明色下均可读（对比度正常）（验证：双模式浏览）

## 构建与质量

- [ ] `cd official && npm run build` 成功产出 `.vitepress/dist`（验证：构建命令 exit 0）
- [ ] `npm run preview` 本地服务可访问首页与各文档页（验证：curl 各路径）
- [ ] 内部链接无死链：dist 内所有页面内部链接可访问（验证：遍历链接 curl 200）
- [ ] SEO：dist/index.html 含 `<title>BinRag` 与 `og:` meta；每篇 guide 页含自身 title/description（验证：grep dist）
- [ ] 移动端布局：组件在窄屏不溢出（验证：静态检查 media query / 组件 flex 布局；无浏览器则标注）

## 端到端场景

- [ ] **场景 1（访客完整路径）**：`npm run preview` → 访问首页 → 看到 Hero/特性/架构/亮点/三步上手 → 点击「快速开始」→ 进入 getting-started → 按文档执行 docker compose + 配置 + 构建启动 → 侧边栏跳转 mcp.md → 按文档配置 MCP 客户端接入
- [ ] **场景 2（暗色与移动端）**：切换暗色模式 → 全站配色正常 → 窄窗口浏览首页与文档 → 布局无横向滚动
- [ ] **场景 3（内容一致性抽查）**：mcp.md 中的 Tool 列表与实现（6 个）、认证错误码（401 / -32001）、用户凭据说明与 docs/26、docs/27 一致；auth.md 的 OIDC 配置字段与 config.yaml 一致
