# Bin-rag
这是一个使用 go 语言实现的 文档 RAG 系统，项目名称为BinRag。

目的是实现对多类型文档内容的存储和知识问答助手。满足企业级的知识库构建问答助手。

## 语言
中文回答，中文注释。

使用 codegraph 进行检索
## Skill 存放约定

- `.agents/skills/` — 规范存放位置（跨 agent 共享，所有 agent 都能读取）
- `.claude/skills/` — 软链接到 `.agents/skills/` 下的同名目录

创建或修改 skill 时：
1. 实际文件写入 `.agents/skills/<skill-name>/SKILL.md`
2. 确保 `.claude/skills/<skill-name>` 是指向它的软链接
3. 软链接创建命令：`ln -s ../../.agents/skills/<skill-name> .claude/skills/<skill-name>`