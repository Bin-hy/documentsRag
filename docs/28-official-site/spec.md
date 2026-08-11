# BinRag 官网与文档站 Spec

## 背景

BinRag 项目功能已完备：多格式文档解析、混合检索、RAG 问答编排、流式输出、异步入库、API Key + OIDC 双通道认证、MCP Server（streamable HTTP 只读 RAG 能力 + 用户维度凭据）、Web/桌面双形态。现有 `README.md` 与 `docs/` 目录承载设计文档，但缺乏面向用户的**官网与结构化文档站**。

本迭代在 `official/` 目录下用 **VitePress** 脚手架搭建官网（高级视觉 Landing）与项目文档站，静态构建、独立部署。

## 目标

- 官网首页：品牌化高级视觉（Hero / 特性墙 / 架构 / 能力亮点 / 快速上手入口 / 页脚），让访客一眼理解「BinRag 是什么、能做什么、怎么开始」
- 文档站：结构化中文文档（快速开始 / 核心概念 / 部署 / API 参考 / MCP Server / 登录认证 / RAG 评估），基于现有 README 与 docs 素材整理提炼（非简单复制）
- 静态站点：`npm run build` 产出可独立部署的静态文件

## 功能需求

- F1: 官网首页（Landing）：
  - Hero 区：产品名、一句标语、能力简介、双 CTA（快速开始 / 查看文档）
  - 特性墙：6-8 个核心能力卡片（多格式解析 / 混合检索 / 重排序 / 流式问答 / 异步入库 / 双通道认证 / MCP / 桌面应用）
  - 架构图区：整体架构可视化（前端 / API / MCP / 入库链路 / 问答链路）
  - 能力亮点：MCP Server 与 OIDC 登录两个亮点块（本轮交付重点）
  - 快速上手：三步引导（启动基础设施 / 配置 / 启动）
  - 页脚：技术栈徽标、GitHub、License 说明
- F2: 文档站：
  - 顶部导航 + 侧边栏：快速开始 / 核心概念 / 部署 / API 参考 / MCP Server / 登录认证 / RAG 评估
  - 文档页基于现有 `README.md` 与 `docs/` 素材整理，覆盖：
    - 快速开始（前置条件 / 配置 / 构建启动 / 第一个请求）
    - 核心概念（知识库 / 异步入库 / 检索与重排序 / RAG 问答）
    - 部署（Web 版 / 桌面版 / Docker / 发布）
    - API 参考（REST 接口表 + MCP 端点）
    - MCP Server（协议 / 配置 / 认证授权 / 6 Tool / 客户端接入 / 用户凭据）
    - 登录认证（API Key / OIDC / GitHub）
    - RAG 评估（CLI 用法 / 数据集格式）
- F3: 高级视觉与交互：
  - 自定义主题：品牌色（沿用项目主色系）、字体栈、暗色模式
  - 首页自定义组件（Hero / FeatureCard / ArchitectureDiagram / HighlightBlock / Steps）
  - 平滑过渡与 hover 反馈
- F4: 静态构建：`npm run build` 成功产出 `official/.vitepress/dist`，可被任意静态服务器托管

## 非功能需求

- N1: 全中文界面与文档（代码/命令保持原文）
- N2: 响应式（桌面 / 平板 / 移动端可用）
- N3: SEO 基础：每页 title / description、`og:` meta
- N4: 构建产物轻量、构建时间可接受（秒级）
- N5: 文档内容与项目现状一致（MCP / OIDC / 用户凭据等已实现功能如实呈现）

## 不做的事

- 不做多语言站点（本版仅中文）
- 不做博客 / 新闻 / 版本更新日志站
- 不做在线 API 试玩（Swagger 交互留在运行时 `/swagger/`）
- 不做官网与运行时服务的集成部署（静态站独立部署）

## 验收标准

- AC1: `npm run build` 成功，`official/.vitepress/dist` 产出完整静态站
- AC2: 官网首页渲染完整（Hero / 特性墙 / 架构 / 亮点 / 快速上手 / 页脚），暗色模式可用
- AC3: 文档站导航与侧边栏完整，7 大章节全部有内容页，无死链
- AC4: MCP Server 与登录认证章节内容与项目实际能力一致（认证 401 / 授权 -32001 / 用户凭据 / OIDC 配置）
- AC5: 首页与文档在移动端布局正常（响应式）
- AC6: 每页含 title 与 description（SEO 基础）
