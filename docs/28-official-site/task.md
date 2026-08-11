# BinRag 官网与文档站 Tasks

> 依据：已批准的 spec.md + plan.md。在 `official/` 目录用 VitePress 搭建，严格按 plan 范围。

## 文件清单

| 操作 | 文件 | 职责 |
|------|------|------|
| 新建 | `official/package.json` | vitepress 依赖 + dev/build/preview scripts |
| 新建 | `official/.gitignore` | node_modules / dist / cache |
| 新建 | `official/.vitepress/config.mts` | title/description/og、nav、sidebar、暗色、search、lastUpdated |
| 新建 | `official/.vitepress/theme/index.ts` | 主题入口（import custom.css + 注册组件） |
| 新建 | `official/.vitepress/theme/custom.css` | 品牌 CSS 变量（靛蓝系）、明暗适配、组件样式 |
| 新建 | `official/.vitepress/theme/components/FeatureCard.vue` | 特性墙卡片 |
| 新建 | `official/.vitepress/theme/components/ArchitectureDiagram.vue` | 架构图（CSS/SVG） |
| 新建 | `official/.vitepress/theme/components/HighlightBlock.vue` | MCP/OIDC 亮点双卡 |
| 新建 | `official/.vitepress/theme/components/Steps.vue` | 编号步骤 |
| 新建 | `official/index.md` | 官网首页 Landing |
| 新建 | `official/guide/getting-started.md` | 快速开始 |
| 新建 | `official/guide/concepts.md` | 核心概念 |
| 新建 | `official/guide/deployment.md` | 部署 |
| 新建 | `official/guide/api.md` | API 参考 |
| 新建 | `official/guide/mcp.md` | MCP Server |
| 新建 | `official/guide/auth.md` | 登录认证 |
| 新建 | `official/guide/eval.md` | RAG 评估 |

## T1: 脚手架初始化

**文件：** `official/package.json`、`official/.gitignore`
**依赖：** 无
**步骤：**
1. `package.json`：依赖 `vitepress@^1.6`、`vue`；scripts：`dev: vitepress dev`、`build: vitepress build`、`preview: vitepress preview`
2. `.gitignore`：`node_modules/`、`.vitepress/dist/`、`.vitepress/cache/`
3. 建最小骨架（空 index.md + 最小 config）后 `npm install`（网络慢，用后台执行并等待完成）
4. 验证脚手架可用：`npm run build` 空站成功

**验证：** `cd official && npm install` 完成；`npm run build` 成功产出 `official/.vitepress/dist`

## T2: 站点配置 config.mts

**文件：** `official/.vitepress/config.mts`
**依赖：** T1
**步骤：**
1. `title: 'BinRag'`、`description`（官网一句话）、`lang: 'zh-CN'`、`lastUpdated: true`
2. `head`：og:title / og:description / og:type=website（SEO，N3）
3. `themeConfig.nav`：快速开始 / 核心概念 / 部署 / API 参考 / MCP Server / 登录认证 / RAG 评估（前 4 个短 nav，完整入口靠侧边栏）
4. `themeConfig.sidebar`：`/guide/` 按 7 章节分组（简介组：getting-started/concepts；能力组：mcp/auth/eval；运行组：deployment/api）
5. `search: { provider: 'local' }`、`darkModeSwitchLabel: '外观'`、`outline: { level: [2,3] }`

**验证：** `npm run build` 通过（config 无类型错误）

## T3: 主题入口与品牌样式

**文件：** `official/.vitepress/theme/index.ts`、`official/.vitepress/theme/custom.css`
**依赖：** T2
**步骤：**
1. `theme/index.ts`：`import DefaultTheme from 'vitepress/theme'` + `import './custom.css'` + 注册 4 个组件到 `theme.components`（全局可用）
2. `custom.css`（品牌视觉，D2/D4）：
   - `:root`：`--vp-c-brand-1:#4f5bd5`、`-2:#5f6adc`、`-3:#3d47c0`、`-soft:rgba(79,91,213,.14)`
   - `.dark`：`--vp-c-brand-1:#7d85ee` 等暗色变体
   - Hero 区：名称渐变文字（靛蓝→紫）、tagline 行高
   - 组件样式占位类名（feature-card / arch-diagram / highlight / steps 的公共变量）
3. 字体栈：系统字体 + `font-feature-settings`（中文友好）

**验证：** `npm run build` 通过；dev 预览品牌色生效（`--vp-c-brand-1` 映射）

## T4: 自定义组件

**文件：** `official/.vitepress/theme/components/{FeatureCard,ArchitectureDiagram,HighlightBlock,Steps}.vue`
**依赖：** T3
**步骤：**
1. **FeatureCard.vue**：props `{ icon, title, desc }`；卡片渐变描边（brand-soft 背景 + 1px 渐变边框）+ hover 抬升与光晕；icon 用内联 SVG（D6）
2. **ArchitectureDiagram.vue**：三段式（① 前端 Web/桌面 + API 层 ② MCP 层 → RAG 引擎 ③ 入库链路 Load→Chunk→Embed→Store）；CSS grid 布局 + 箭头（`→`/SVG 箭头），各块品牌色描边；响应式（窄屏纵向堆叠）
3. **HighlightBlock.vue**：props `{ title, points, badge }` 双卡并排；brand 渐变左上角光斑 + 列表点
4. **Steps.vue**：props `{ steps: [{title, desc}] }`；编号圆形徽标（brand 渐变）+ 竖线连接

**验证：** `npm run build` 通过（组件编译无错）；dev 页面渲染组件

## T5: 官网首页 index.md

**文件：** `official/index.md`
**依赖：** T4
**步骤：**
1. frontmatter：`layout: home` + hero（name=BinRag / text 标语 / tagline + actions 快速开始→guide/getting-started、MCP Server→guide/mcp）+ features（8 个：多格式解析 / 混合检索 / 重排序精排 / 流式问答 / 异步入库 / 双通道认证 / MCP Server / Web+桌面）
2. 页面主体：
   - `# 架构一览` + `<ArchitectureDiagram />`
   - `# 核心能力` + `<HighlightBlock />`（MCP Server：streamable HTTP、401/-32001、用户凭据；OIDC 登录：多 Provider、会话 JWT、用户知识库）
   - `# 快速上手` + `<Steps />`（启动基础设施 → 配置模型与密钥 → 构建启动）
   - 页脚区：技术栈徽标（Go/Qdrant/PostgreSQL/Vue）+ 指向 guide 的链接
3. 每 section 带锚点

**验证：** `npm run build` 通过；`npm run preview` 浏览器抽查（或静态检查 dist/index.html 含各组件渲染内容）

## T6: 文档页 guide/*（7 篇，分 3 批）

**文件：** `official/guide/{getting-started,concepts,deployment,api,mcp,auth,eval}.md`
**依赖：** T2
**步骤：**
- 批 1：`getting-started.md`（前置条件 / docker compose / config.local 说明与示例 / 构建启动 / 第一个请求 curl 全套）、`concepts.md`（知识库 / 异步入库 / 混合检索+RRF / 重排序 / RAG 问答编排 / 流式）
- 批 2：`mcp.md`（配置 / 认证授权 401/-32001/双层开关 / 6 Tool 表 / 客户端 mcpServers 示例 / 用户凭据「我的 MCP」/ REST 管理接口）、`auth.md`（API Key / OIDC 配置 providers+回调 / GitHub / 会话 JWT / 用户体系）
- 批 3：`deployment.md`（Web 交叉编译 / 桌面 Wails / Docker / GitHub Actions）、`api.md`（REST 接口表 + /mcp 端点 + 统一响应 + 认证说明）、`eval.md`（CLI / 数据集格式 / 模式）
- 每篇 frontmatter：`title` + `description`（N3）；内容基于 README/docs 提炼（D5），代码/命令保持原文

**验证：** 每批后 `npm run build` 通过；检查 dist 内各页面 HTML 生成

## T7: 构建与质量检查

**文件：** 无新增
**依赖：** T5、T6
**步骤：**
1. `npm run build` 全量成功；`npm run preview` 起本地服务
2. 链接检查：提取 dist 内所有页面 HTML，遍历内部链接（`/guide/*.html`、`/index.html`）确认无 404（脚本或 curl 逐个 HEAD）
3. SEO 抽查：dist/index.html 含 `<title>BinRag` 与 `og:` meta；每篇 guide 页含对应 title
4. 移动端抽查（无浏览器则静态检查样式 media query / 组件 flex 布局）

**验证：** build 通过；内部链接全部 200；首页与各页 title/description 齐全

## 执行顺序与依赖

```
T1（脚手架）→ T2（config）→ T3（主题样式）→ T4（组件）→ T5（首页）
                                                    ↘
T2 ─────────────────────────────────→ T6（文档 3 批）→ T7（质量检查）
```

关键路径：`T1 → T2 → T3 → T4 → T5 → T7`；T6 文档与 T3–T5 并行（依赖 T2 即可）。
