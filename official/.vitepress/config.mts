import { defineConfig } from 'vitepress'

// BinRag 官网与文档站配置
export default defineConfig({
  title: 'BinRag',
  description: '企业级多类型文档知识库问答系统 — 文档 RAG + MCP Server + OIDC 登录',
  lang: 'zh-CN',
  lastUpdated: true,
  cleanUrls: true,
  // 构建产物输出到站点根目录 dist/（Cloudflare Pages 输出目录填 dist）
  outDir: 'dist',
  // README.md 是仓库部署说明，不作为站点页面
  srcExclude: ['README.md'],

  head: [
    ['meta', { property: 'og:type', content: 'website' }],
    ['meta', { property: 'og:title', content: 'BinRag — 文档 RAG 知识库问答系统' }],
    [
      'meta',
      {
        property: 'og:description',
        content: '多格式文档解析、混合检索、带引用来源的智能问答；开箱即用的 MCP Server 与 OIDC 登录。',
      },
    ],
  ],

  themeConfig: {
    logo: '⚡',
    siteTitle: 'BinRag',

    // 右上角 GitHub 仓库跳转
    socialLinks: [{ icon: 'github', link: 'https://github.com/Bin-hy/documentsRag', ariaLabel: 'GitHub 仓库' }],

    nav: [
      { text: '快速开始', link: '/guide/getting-started' },
      { text: '核心概念', link: '/guide/concepts' },
      { text: 'MCP Server', link: '/guide/mcp' },
      { text: '登录认证', link: '/guide/auth' },
      { text: 'API 参考', link: '/guide/api' },
    ],

    sidebar: {
      '/guide/': [
        {
          text: '开始使用',
          items: [
            { text: '快速开始', link: '/guide/getting-started' },
            { text: '核心概念', link: '/guide/concepts' },
          ],
        },
        {
          text: '能力',
          items: [
            { text: 'MCP Server', link: '/guide/mcp' },
            { text: '登录认证', link: '/guide/auth' },
            { text: 'RAG 评估', link: '/guide/eval' },
          ],
        },
        {
          text: '运行与参考',
          items: [
            { text: '部署', link: '/guide/deployment' },
            { text: 'API 参考', link: '/guide/api' },
          ],
        },
      ],
    },

    outline: { level: [2, 3], label: '本页目录' },
    search: { provider: 'local' },
    darkModeSwitchLabel: '外观',
    returnToTopLabel: '返回顶部',
    sidebarMenuLabel: '菜单',
    lastUpdated: { text: '最后更新于', formatOptions: { dateStyle: 'short', timeStyle: 'short' } },
  },
})
