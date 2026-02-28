# AgentFlow Editor (Wizard Mode)

会话式 AgentFlow 编辑器 —— 在可视化 UI 就绪之前，用 AI 对话替代表单来创建 Flowgent 应用层配置。

一套 `SKILL.md`，三平台通用。

## 定位

Flowgent 最终会有一个可视化的 AgentFlow Editor UI。在 UI 落地之前，这个 skill 充当**临时编辑器**：用户在 Copilot / Claude Code / OpenCode 中加载此 skill，通过结构化多轮对话完成 flow 的创建，效果等同于在 UI 里填表单、连线、保存。

## 安装

### Claude Code

```bash
mkdir -p .claude/skills/agentflow-creator
cp SKILL.md .claude/skills/agentflow-creator/SKILL.md
```

使用：`/agentflow-creator 描述你的 agent 工作流`

### OpenCode

```bash
mkdir -p .opencode/commands
cp SKILL.md .opencode/commands/agentflow-creator.md
```

使用：`/agentflow-creator 描述你的 agent 工作流`

### VS Code Copilot

```bash
mkdir -p .github/prompts
cp SKILL.md .github/prompts/agentflow-creator.prompt.md
```

使用：在 Copilot Chat 输入 `@prompt agentflow-creator 描述你的 agent 工作流`

## 交互流程（5 步向导）

| 步骤 | 内容 |
|------|------|
| Step 1 | 用户自然语言描述 → 编辑器解析为 nodes/edges/agents/vars |
| Step 2 | 展示结构表确认（节点类型、连线关系） |
| Step 3 | 逐个定义新 Agent（model / soul / instruction） |
| Step 4 | 确认触发方式（schedule / webhook / manual）和变量 |
| Step 5 | 展示完整文件清单 + YAML 内容，确认后写入 |

## 生成物

```
<project-name>/
├── Dockerfile              # FROM flowgent:latest, COPY manifests/
├── README.md               # 构建 & 运行说明
└── manifests/
    ├── agents/
    │   ├── 01-xxx.yaml     # Agent 定义
    │   └── 02-yyy.yaml
    └── flows/
        └── 01-my-flow.yaml # AgentFlow 定义
```

生成后直接 `docker build -t <name> . && docker run -p 9999:9999 <name>` 即可运行。
