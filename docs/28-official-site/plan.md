# BinRag 官网与文档站 Plan

## 架构概览

VitePress 静态站（`official/` 目录，独立于运行时服务）：

```
official/
├── package.json            — vitepress + vue 依赖；scripts: dev / build / preview
├── .vitepress/
│   ├── config.mts          — 站点元信息（title/description/og）、导航、侧边栏、暗色主题、LastUpdated
│   └── theme/
│       ├── index.ts        — 主题入口（扩展默认主题：import custom.css + 注册组件）
│       ├── custom.css      — 品牌 CSS 变量（靛蓝 #4f5bd5 系）、明暗适配、字体、组件样式
│       └── components/
│           ├── FeatureCard.vue        — 特性墙卡片（icon/标题/描述/hover）
│           ├── ArchitectureDiagram.vue— 架构图（纯 CSS/SVG，三链路）
│           ├── HighlightBlock.vue     — 亮点块（MCP / OIDC）
│           └── Steps.vue              — 编号步骤（三步上手）
├── index.md                — 官网首页（Landing，引用上述组件）
└── guide/                  — 文档站
    ├── getting-started.md  — 快速开始
    ├── concepts.md         — 核心概念
    ├── deployment.md       — 部署（Web/桌面/容器/发布）
    ├── api.md              — API 参考
    ├── mcp.md              — MCP Server
    ├── auth.md             — 登录认证（API Key / OIDC / GitHub）
    └── eval.md             — RAG 评估
```

## 模块设计

### .vitepress/config.mts

**职责：** 站点配置。
```ts
export default defineConfig({
  title: 'BinRag',
  description: '企业级多类型文档知识库问答系统 — 文档 RAG + MCP Server',
  lang: 'zh-CN',
  lastUpdated: true,
  head: [['meta', { property: 'og:title', content: 'BinRag' }], ...],
  themeConfig: {
    logo: '...'（可选，用 emoji/SVG 徽标）,
    nav: [快速开始 / MCP Server / 登录认证 / API / GitHub],
    sidebar: { '/guide/': [7 章节分组] },
    outline: { level: [2, 3] },
    socialLinks: [{ icon: 'github', link: '仓库地址' }],
    search: { provider: 'local' },
    darkModeSwitchLabel: '外观',
  },
})
```

### index.md（官网首页）

**职责：** Landing，布局 + 引用自定义组件（F1）。
```markdown
---
layout: home
hero:
  name: "BinRag"
  text: "文档 RAG 知识库问答系统"
  tagline: "多格式文档 → 混合检索 → 带引用来源的智能问答；开箱即用的 MCP Server"
  actions:
    - { theme: brand, text: 快速开始, link: /guide/getting-started }
    - { theme: alt, text: MCP Server, link: /guide/mcp }
features:
  - 多格式解析 / 混合检索 / 重排序 / 流式问答 / 异步入库 / 双通道认证 / MCP / 桌面
---
<!-- 自定义区：架构图 / 亮点 / 三步上手 -->
<ArchitectureDiagram />
<HighlightBlock />
<Steps />
```

### 自定义组件（高级视觉，F3）

- **FeatureCard.vue**：品牌色渐变描边 + hover 抬升/光晕；icon 用内联 SVG（无图标库依赖）
- **ArchitectureDiagram.vue**：三段式横向图（前端/API+MCP → 入库链路 → 问答链路），纯 CSS 网格 + 箭头（不引入 mermaid 插件，视觉可控，D3）
- **HighlightBlock.vue**：双卡并排（MCP Server：streamable HTTP + 401/-32001 + 用户凭据；OIDC 登录：多 Provider + 会话 JWT + 用户知识库归属），品牌色圆角卡片
- **Steps.vue**：编号步骤（启动基础设施 → 配置 → 启动），序号徽标 + 描述

### custom.css

**职责：** 品牌视觉（D2/D4）。
```css
:root {
  --vp-c-brand-1: #4f5bd5;   /* 品牌主色（明色） */
  --vp-c-brand-2: #5f6adc;
  --vp-c-brand-3: #3d47c0;
  --vp-c-brand-soft: rgba(79, 91, 213, 0.14);
}
.dark { --vp-c-brand-1: #7d85ee; ... }
/* Hero 渐变文字、卡片 hover、步骤徽标、架构图配色等组件样式 */
```

## 文档页（guide/*，F2）

| 页面 | 内容来源 | 覆盖 |
|------|----------|------|
| `getting-started.md` | README 快速开始 | 前置条件 / docker compose / config.local / 构建启动 / 第一个请求 curl |
| `concepts.md` | README + docs | 知识库 / 异步入库链路 / 混合检索+RRF / 重排序 / RAG 问答编排 / 流式 |
| `deployment.md` | README 构建打包 | Web 交叉编译 / 桌面 Wails / Docker / GitHub Actions 发布 |
| `api.md` | README API 概览 + swagger | REST 接口表 + `/mcp` 端点 + 统一响应格式 + 认证说明 |
| `mcp.md` | README MCP 章节 + docs/26 | 协议与配置 / 认证授权模型（401/-32001/双层开关） / 6 Tool 表 / 客户端接入示例 / 用户维度凭据 / REST 管理接口 |
| `auth.md` | README + config 注释 | API Key / OIDC 配置（providers/回调地址） / GitHub / 会话 JWT / 用户体系 |
| `eval.md` | README RAG 评估 | 评估 CLI / 数据集格式 / 模式 |

每页含 `frontmatter: title + description`（N3 SEO）。

## 文件组织

```
docs/28-official-site/{spec,plan,task,checklist}.md   — 本迭代设计文档
official/
├── package.json
├── .gitignore            — node_modules / .vitepress/dist / .vitepress/cache
├── .vitepress/
│   ├── config.mts
│   └── theme/
│       ├── index.ts
│       ├── custom.css
│       └── components/{FeatureCard,ArchitectureDiagram,HighlightBlock,Steps}.vue
├── index.md
└── guide/{getting-started,concepts,deployment,api,mcp,auth,eval}.md
```

## 技术决策

| # | 决策点 | 选择 | 理由 |
|---|--------|------|------|
| D1 | 脚手架 | VitePress 1.6.x | 与项目 Vue 3 一致；Markdown 优先；内置暗色/搜索/导航；中文生态成熟 |
| D2 | 品牌色 | 沿用前端 `--br-primary` 靛蓝 `#4f5bd5`（暗色 `#7d85ee`） | 官网与产品视觉统一 |
| D3 | 架构图 | 自定义 CSS/SVG 组件 | 视觉可控、无 mermaid 插件依赖、避免构建复杂度 |
| D4 | 暗色模式 | VitePress 内置 + 自定义品牌 CSS 变量适配 | 开箱即用 + 品牌一致性 |
| D5 | 文档内容 | 基于 README/docs 提炼，与实现一致 | 避免重复造轮子，MCP/OIDC 如实呈现 |
| D6 | 图标 | 内联 SVG（组件内置） | 无图标库依赖，构建轻量 |
| D7 | 语言 | 全中文（代码/命令原文） | 与项目文档一致 |

## Spec 覆盖

| Spec | 落点 |
|------|------|
| F1 官网首页 | index.md + 4 个自定义组件 |
| F2 文档站 | guide/* 7 页 + config 导航/侧边栏 |
| F3 高级视觉 | custom.css 品牌变量 + 组件样式（hover/渐变/过渡） |
| F4 静态构建 | package.json scripts + .gitignore |
| N1 中文 | 全部页面 zh-CN |
| N2 响应式 | VitePress 默认 + 组件 flex/grid 自适应 |
| N3 SEO | config head + 每页 frontmatter |
| N5 与实现一致 | mcp.md/auth.md 按实现整理 |
