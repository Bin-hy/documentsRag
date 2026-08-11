# BinRag 官网（official/）

BinRag 项目官网与文档站，基于 [VitePress](https://vitepress.dev)（Vue 3 生态，与项目前端技术栈一致）。

`official/` 是 **pnpm workspace（monorepo）** 的子包之一（与 `frontend/` 并列，见根目录 `pnpm-workspace.yaml`），统一在仓库根安装与管理依赖。

## 本地开发

```bash
# 仓库根：一次安装 frontend + official 全部依赖
pnpm install

# 官网（本包）
pnpm --filter binrag-official dev        # 本地开发（http://localhost:5173）
pnpm --filter binrag-official build      # 构建静态产物 → official/dist/
pnpm --filter binrag-official preview    # 预览构建产物

# 前端（兄弟子包）
pnpm --filter binrag-frontend dev
pnpm --filter binrag-frontend build

# 全部子包构建
pnpm -r build
```

- 构建产物输出：`official/dist/`（在 `config.mts` 中 `outDir: 'dist'` 指定）
- 依赖管理器：pnpm（根 `packageManager: pnpm@10.33.0`，锁定根 `pnpm-lock.yaml`）

## Cloudflare Pages 部署

在 Cloudflare Dashboard → Workers & Pages → Create → Pages 连接 Git 仓库（或直接上传目录），填写：

| 配置项 | 填写值 |
|--------|--------|
| Framework preset | **None**（手动填写） |
| Build command | `pnpm install --filter binrag-official... && pnpm --filter binrag-official build`（或仅 `pnpm --filter binrag-official build`） |
| Build output directory | `dist` |
| Node.js version | **20+**（建议 22；根 `package.json` 已声明 `engines.node >= 20`） |
| 环境变量 | 无需 |

> 提示：Cloudflare Pages 需要仓库内存在 `pnpm-lock.yaml` 才会使用 pnpm（本仓库根目录已包含）。若部署时 workspace 安装报错，可改用构建命令：`cd official && pnpm install && pnpm build`。

### 本地用 wrangler 部署（可选）

```bash
npx wrangler pages deploy dist --project-name binrag
```

## 目录结构

```
official/
├── package.json          # 子包声明（vitepress 依赖与 scripts）
├── .gitignore            # node_modules / dist / 缓存
├── README.md             # 本文件（srcExclude，不作为站点页面）
├── .vitepress/
│   ├── config.mts        # 站点配置：导航/侧边栏/SEO/outDir:dist
│   └── theme/            # 品牌主题（靛蓝 #4f5bd5）与自定义组件
├── index.md              # 官网首页（Landing）
└── guide/                # 文档站（7 篇）
```

