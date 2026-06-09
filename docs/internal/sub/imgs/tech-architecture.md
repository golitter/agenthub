# 技术架构总览图 — GPT-Image-2 提示词

> 来源：[技术文档.md §2.1 总体架构](../技术文档.md)（行 73–101）。
> 图旨：**一张图说清「三端分层 + 数据流向 + 各端技术栈选型」**，呈现 Event-Driven Runtime 全景；全览概括视角，不绑定具体实现端口。
> 供图工具：GPT-Image-2（gpt-image-2）。提示词以中文给出，遵循 [aiimage.md](../aiimage.md) 的 Dark Utilitarian 风格。
> 命名约定：与同目录 `flow-design.md` / `information-architecture.md` 一致；产物建议 `tech-architecture.png`。

---

## 一、构图蓝图（先看结构，再看提示词）

横向画幅，左主区三层垂直堆叠、右辅区存储栈、底部 contracts 贯穿带。编号箭头 ①②③④⑤ 是全图骨架。

```
┌─────────────────────────────────────────────────────────────────────┐
│  顶部标题区：AgentHub · Event-Driven Runtime 技术架构总览             │
├──────────────────────────────────────────────────────────────┬──────┤
│ ① 前端层 FRONTEND LAYER              [React19][TS][Vite8]…   │ DATA │
│   React SPA · reducer over events    EventSource→…→Cards     │┌────┐│
│ ── ①SSE实时回流(绿↑) ─── ③REST触发(灰↓) ───────────────────── ││Redis││
├──────────────────────────────────────────────────────────────┤ └────┘│
│ ② 后端层 BACKEND LAYER              [Go1.26][Gin][GORM]      │┌────┐│
│   Platform State Layer(只观察)      Controller→Service→DAO   ││MySQL││
│ ── ②SSE调用(青↓) ───────── ④双写(琥珀→)──────────────────── │└────┘│
├──────────────────────────────────────────────────────────────┴──────┤
│ ③ Agent 端 AGENTEND LAYER           [Python3.10][FastAPI]…          │
│   Execution Layer(唯一执行权威)     FastAPI→AdapterRegistry→…       │
│   LangGraph 编排 · 波次并行 · git worktree 隔离                     │
├─────────────────────────────────────────────────────────────────────┤
│ ⑤ contracts/schemas/*.yaml ── make generate ──▶ 三端 generated/     │
└─────────────────────────────────────────────────────────────────────┘
```

---

## 二、★ 主提示词（直接整段复制给 GPT-Image-2）

> 以下为中文提示词，已写尽构图、文字、配色与排版纪律，可直接使用。

```text
生成一张给工程师看的「技术架构总览信息图」，主题为「AgentHub — Event-Driven Runtime 技术架构全景」。整体气质对标 Linear 的深色面板、Cursor 的 IDE 氛围、Slack 的结构化节奏：深色、扁平、克制、精密、信息密集。这是一张全览概括图，不出现任何具体端口号。禁止渐变、毛玻璃、发光、霓虹、赛博朋克、拟物、卡通、营销海报感。

【画布与基调】
- 横向画幅，比例约 16:10。整体深色：最底层画布 #0A0B0E（近黑），主面板 #121419，三张架构卡片表面 #1A1D24，卡片内嵌子面板 #22252E。深度仅靠这 5 级灰阶的背景色差 + 1px 极细边框 #2A2D36 表达，禁用厚重阴影。
- 全图圆角统一 10px。所有间距基于 4px 网格，紧凑而有序。
- 字体：标题与 UI 文案用现代几何无衬线（思源黑体 / Inter），所有命令、路径、技术标签用等宽字体（思源等宽 / JetBrains Mono）。文字小而锐利。中文必须精确清晰，不得乱码、不得臆造笔画、不得漏字。

【顶部标题区】
- 主标题：AgentHub · Event-Driven Runtime 技术架构总览
- 副标题：从 Request-Driven → Event-Driven：HTTP 仅触发，事件流（EventEnvelope）贯穿三端，前端以 reducer 模式从事件流派生 UI
- 右上角一枚等宽角标：v1.0 · 架构总览

【左主区：三层垂直堆叠】
自上而下三张等宽、圆角 10px 的架构卡片，每张卡片左侧带一条 3px 宽的身份色 accent bar。

第一层 — 前端层 FRONTEND LAYER（accent bar #6366F1 靛蓝）
- 标题行：① 前端层 FRONTEND LAYER
- 角色小字：React SPA · Event-Driven UI（reducer over events）
- 一排技术标签芯片（等宽细描边）：React 19 | TypeScript | Vite 8 | Tailwind CSS | shadcn/ui(Radix) | Zustand | TanStack Query
- 横向数据通路：EventSource → useChatStream → messageStore(rAF) → Cards
- 注脚：block-reducer 按事件增量派生 UI

第二层 — 后端层 BACKEND LAYER（accent bar #06B6D4 青）
- 标题行：② 后端层 BACKEND LAYER
- 角色小字：Platform State Layer — 只 record / observe / request，不执行 Agent、不持执行态权威
- 技术标签芯片：Go 1.26 | Gin | GORM | Air(热重载)
- 卡片内一条竖向细分隔线分两栏：
  左栏「业务三层」：Controller → Service → DAO(GORM)
  右栏「流式中转」：stream/RuntimeHub（内存 Pub/Sub，实时推前端）+ writer（Redis Stream → MySQL 批量刷写）
- 左下注脚：middleware: auth / cors / rate_limit

第三层 — Agent 端 AGENTEND LAYER（accent bar #10B981 绿）
- 标题行：③ Agent 端 AGENTEND LAYER
- 角色小字：Execution Layer — 唯一执行态权威（task.running / completed / failed）
- 技术标签芯片：Python 3.10 | FastAPI | uv | LangGraph | ruff | pytest
- 横向数据通路：FastAPI → AdapterRegistry → { Claude / OpenCode / Codex / Orchestrator }
- 能力注脚：LangGraph 编排引擎 · ExecutionEngine 波次并行 · git worktree 隔离

【右辅区：数据存储栈】
右侧两张窄卡片纵向排列，顶部小标题 DATA STORAGE。
- 上卡 Redis（accent bar #F59E0B 琥珀）：Redis Stream；注：短期缓存 / 回放 / 断线重连
- 下卡 MySQL（accent bar #3B82F6 蓝）：MySQL · GORM；注：长期持久化

【数据流箭头：带编号圆圈，是全图骨架】
箭头一律干净的细直线 + 小箭头头；编号统一用圆形徽章包裹阿拉伯数字。
- ① 后端 → 前端（SSE 实时回流）：绿色 #10B981，从后端层左上引箭头向上至前端层，标注「① SSE GET /api/tasks/:id/stream　实时事件流」
- ③ 前端 → 后端（REST 触发）：中性亮灰 #A1A1AA，从前端层右下引箭头向下至后端层，标注「③ REST POST /api/tasks/:id/run · /review · /messages　触发任务」
- ② 后端 → Agent 端（SSE 调用）：青色 #06B6D4，从后端层底边引箭头向下至 Agent 端层，标注「② SSE POST /v1/agent/stream　调用执行」
- ④ 后端 → 存储（双写）：琥珀色 #F59E0B，从后端层右侧引箭头分别指向 Redis 与 MySQL，标注「④ 双写：Hub 实时推前端 + Redis/MySQL 持久化」
- ⑤ contracts（贯穿底带）：画面最底部一条贯穿左右的虚线带，靛蓝 #6366F1，标注「⑤ contracts/schemas/*.yaml ── make generate ──▶ 三端 generated/（契约优先）」

【核心数据流总览条】
在三层卡片底部、⑤虚线带上方，留一条等宽字体的水平注释，文字逐字为：
前端 REST 触发 → 后端建消息+建 SSE 通道+调 Agent 端 → Agent 端按事件流式回传 → 后端双写(Hub 实时推 + Redis/MySQL 持久化) → 前端按事件增量渲染

【配色与文字纪律（必须严格遵守）】
- 全图只用功能/状态/身份色：品牌靛蓝 #6366F1、青 #06B6D4、绿 #10B981、琥珀 #F59E0B、蓝 #3B82F6，加若干中性灰。不得出现大面积彩色色块、渐变、发光、晕染。
- 颜色只落在：卡片左侧 accent bar、数据流编号圆圈与箭头、技术标签芯片描边、状态点。
- 中英混排：标题中英双语，技术名词保持英文原文，中文描述精确无错字。
- 保持高密度但不混乱：细分隔线、小标签、芯片、序号构成清晰的信息层级。
```

---

## 三、视觉风格指导（Dark Utilitarian / Precision Runtime Console）

> 与 [aiimage.md](../aiimage.md) 同一套风格基线。如工具效果不理想，可将下述风格段**追加粘贴在主提示词之后**做强化约束。

```text
风格方向：Dark Utilitarian / Precision Runtime Console（深色实用主义 · 精密运行时控制台）。

以深色中性层级为基底：近黑画布约 #0A0B0E，分层表面以 5 级灰阶逐步抬升到约 #1A1D24 的卡片；通过背景色差、细边框、留白与层级表达深度，而非厚重阴影。

保持扁平与实用：避免渐变、毛玻璃、模糊、高光、拟物、装饰性辉光、过度阴影、彩色装饰、卡通插画、拟真、赛博朋克。

颜色仅用于功能、状态与身份：克制品牌强调色约 #6366F1，稀疏的语义色（成功/警告/错误），以及不同层/模块的身份色。除表示状态、归属或类别外，不使用大块彩色面。

采用紧凑、信息密集的布局：精确对齐、细分隔线、小标签、元数据行、状态芯片、序号徽章，工程感而非装饰感。图节点用干净的矩形/圆角矩形、细边框、极简连线、清晰层级、网格化间距。

字体现代、技术、锐利：UI 文案用几何无衬线，命令/时间戳/代码/标签/运行时元数据用等宽字体；文字小而清晰。

圆角适中（约 10px），间距基于 4px 网格，密度高但不混乱。

最终观感应贴近 Linear 的克制深色面板、Cursor 的 IDE 氛围、Slack 的结构化节奏：冷静、扁平、深色、精密、密集、实用。
```

---

## 四、精确文字清单（逐字核对，防乱码）

> GPT-Image-2 渲染中文偶有错字，生成后请按下表逐项核对、必要时局部重抽或后修。

| 区域 | 精确文字 |
|---|---|
| 主标题 | `AgentHub · Event-Driven Runtime 技术架构总览` |
| 副标题 | `从 Request-Driven → Event-Driven：HTTP 仅触发，事件流（EventEnvelope）贯穿三端，前端以 reducer 模式从事件流派生 UI` |
| 角标 | `v1.0 · 架构总览` |
| 前端 · 标题 | `① 前端层 FRONTEND LAYER` |
| 前端 · 角色 | `React SPA · Event-Driven UI（reducer over events）` |
| 前端 · 栈 | `React 19` `TypeScript` `Vite 8` `Tailwind CSS` `shadcn/ui(Radix)` `Zustand` `TanStack Query` |
| 前端 · 通路 | `EventSource → useChatStream → messageStore(rAF) → Cards` |
| 前端 · 注脚 | `block-reducer 按事件增量派生 UI` |
| 后端 · 标题 | `② 后端层 BACKEND LAYER` |
| 后端 · 角色 | `Platform State Layer — 只 record / observe / request，不执行 Agent、不持执行态权威` |
| 后端 · 栈 | `Go 1.26` `Gin` `GORM` `Air(热重载)` |
| 后端 · 左栏 | `Controller → Service → DAO(GORM)` |
| 后端 · 右栏 | `stream/RuntimeHub（内存 Pub/Sub，实时推前端）+ writer（Redis Stream → MySQL 批量刷写）` |
| 后端 · 注脚 | `middleware: auth / cors / rate_limit` |
| Agent · 标题 | `③ Agent 端 AGENTEND LAYER` |
| Agent · 角色 | `Execution Layer — 唯一执行态权威（task.running / completed / failed）` |
| Agent · 栈 | `Python 3.10` `FastAPI` `uv` `LangGraph` `ruff` `pytest` |
| Agent · 通路 | `FastAPI → AdapterRegistry → { Claude / OpenCode / Codex / Orchestrator }` |
| Agent · 注脚 | `LangGraph 编排引擎 · ExecutionEngine 波次并行 · git worktree 隔离` |
| 存储 · 标题 | `DATA STORAGE` |
| Redis | `Redis Stream` · `短期缓存 / 回放 / 断线重连` |
| MySQL | `MySQL · GORM` · `长期持久化` |
| ① | `① SSE GET /api/tasks/:id/stream　实时事件流`（绿 #10B981） |
| ② | `② SSE POST /v1/agent/stream　调用执行`（青 #06B6D4） |
| ③ | `③ REST POST /api/tasks/:id/run · /review · /messages　触发任务`（灰 #A1A1AA） |
| ④ | `④ 双写：Hub 实时推前端 + Redis/MySQL 持久化`（琥珀 #F59E0B） |
| ⑤ | `⑤ contracts/schemas/*.yaml ── make generate ──▶ 三端 generated/（契约优先）`（靛蓝 #6366F1，虚线带） |
| 总览条 | `前端 REST 触发 → 后端建消息+建 SSE 通道+调 Agent 端 → Agent 端按事件流式回传 → 后端双写(Hub 实时推 + Redis/MySQL 持久化) → 前端按事件增量渲染` |

---

## 五、数据流与配色映射

| 编号 | 方向 | 含义 | 颜色 | 形态 |
|---|---|---|---|---|
| ① | 后端 → 前端 | SSE 实时事件回流 | 绿 `#10B981` | 上行实线箭头 |
| ② | 后端 → Agent 端 | SSE 调用执行 | 青 `#06B6D4` | 下行实线箭头 |
| ③ | 前端 → 后端 | REST 触发任务 | 灰 `#A1A1AA` | 下行实线箭头 |
| ④ | 后端 → 存储 | 双写持久化 | 琥珀 `#F59E0B` | 右行实线箭头（分叉到 Redis/MySQL） |
| ⑤ | 全局贯穿 | 契约 → 代码生成 | 靛蓝 `#6366F1` | 底部虚线带 |

三端身份色（仅落在卡片左 accent bar）：前端 `#6366F1` · 后端 `#06B6D4` · Agent 端 `#10B981`。
存储身份色：Redis `#F59E0B` · MySQL `#3B82F6`。

---

## 六、渲染注意事项

- **中文渲染**：gpt-image-2 对中文仍可能出错。务必按「四、精确文字清单」逐字核对；高频出错项（如「派生」「权威」「回放」「隔离」「贯穿」）若糊化，建议局部重抽或后修。
- **等宽 vs 无衬线**：命令/路径/标签一律等宽（思源等宽 / JetBrains Mono），标题与角色描述用无衬线。两类字体不要混用风格。
- **克制纪律**：绝不加渐变、发光、阴影晕染。颜色只在 accent bar / 编号圆 / 芯片描边 / 状态点出现。
- **对齐**：三张主卡片等宽、左对齐；右辅存储卡与主区顶部对齐；箭头编号圆圈在同一垂直/水平轴上对齐。
- **密度**：保留细分隔线、小注脚、状态点，体现"工程感"，但留白要呼吸，不得堆砌。
- **版本角标**：右上 `v1.0 · 架构总览` 用等宽小字，弱对比，不抢主标题。

---

## 七、可选变体

按用途切换提示词的开头一句即可，其余照旧：

1. **横版 Banner（宽屏演示/封面）**：把画幅改为 21:9，标题区放大，三层卡片横向压缩为三列并排，适合 README 顶图。
2. **纵版海报（竖屏/打印）**：改为 9:16，三层自上而下加高，右辅存储栈移到底部，适合展板。
3. **极简骨架版（弱化技术栈）**：删除所有技术标签芯片，只保留三层角色 + 五条编号数据流，适合面向非技术读者的总览。
4. **纯英文版（渲染最稳）**：将所有中文描述替换为英文（Frontend Layer / Backend Layer / AgentEnd Layer / Real-time SSE / Dual-write 等），规避中文乱码风险，作为保底。

---

## 附：Mermaid 对照版（保底，信息无损）

> 若 gpt-image-2 渲染不理想，可用此 mermaid 源码直接生成对照图，确保架构信息完整。源数据与 [§2.1](../技术文档.md) 一致。

```mermaid
flowchart TB
    subgraph FE["① 前端层 FRONTEND LAYER<br/>React 19 · TS · Vite 8 · Tailwind · shadcn/ui · Zustand · TanStack Query"]
        direction LR
        FE1["EventSource"] --> FE2["useChatStream"] --> FE3["messageStore(rAF)"] --> FE4["Cards<br/>(reducer over events)"]
    end

    subgraph BE["② 后端层 BACKEND LAYER<br/>Go 1.26 · Gin · GORM · Air　Platform State Layer(只观察)"]
        direction LR
        BE1["Controller → Service → DAO(GORM)"]
        BE2["stream/RuntimeHub 内存 Pub/Sub(实时)"] ::: hub
        BE3["writer: Redis Stream → MySQL 批量刷写"] ::: hub
    end

    subgraph AG["③ Agent 端 AGENTEND LAYER<br/>Python 3.10 · FastAPI · uv · LangGraph · ruff · pytest　Execution Layer(唯一执行权威)"]
        direction LR
        AG1["FastAPI"] --> AG2["AdapterRegistry"] --> AG3["{Claude / OpenCode / Codex / Orchestrator}"]
        AG4["LangGraph 编排 · ExecutionEngine 波次并行 · git worktree 隔离"]
    end

    subgraph STORE["DATA STORAGE"]
        RD["Redis Stream<br/>短期缓存 / 回放 / 断线重连"] ::: redis
        MY["MySQL · GORM<br/>长期持久化"] ::: mysql
    end

    CT["⑤ contracts/schemas/*.yaml ── make generate ──▶ 三端 generated/"] ::: contract

    FE -- "③ REST POST /run · /review · /messages 触发任务" --> BE
    BE -- "① SSE GET /stream 实时事件流" --> FE
    BE -- "② SSE POST /v1/agent/stream 调用执行" --> AG
    BE -- "④ 双写: Hub 实时推 + 持久化" --> STORE
    CT -.-> FE
    CT -.-> BE
    CT -.-> AG

    classDef hub fill:#1A1D24,stroke:#06B6D4,color:#E5E7EB;
    classDef redis fill:#1A1D24,stroke:#F59E0B,color:#E5E7EB;
    classDef mysql fill:#1A1D24,stroke:#3B82F6,color:#E5E7EB;
    classDef contract fill:#1A1D24,stroke:#6366F1,color:#E5E7EB;
```

## 设计要点

- **三层即三权**：前端「派生 UI」、后端「只观察不执行」、Agent 端「唯一执行态权威」——三层身份色 accent bar 一眼区分，避免把后端误当成执行者（这是 Event-Driven 演进的核心心智）。
- **颜色即功能**：①绿=实时回流、②青=调用执行、③灰=REST 触发（非事件流）、④琥珀=持久化写入、⑤靛蓝虚线=契约生成。编号圆圈 + 颜色双重编码，编号是无色彩依赖的保底通道。
- **契约贯穿**：⑤ 用虚线带横贯底部，强调「契约优先」是跨三端的横切关注点，而非某一层的附属。
- **技术栈芯片化**：每端技术栈用等宽细描边芯片平铺，与角色描述分层，让「选型」和「职责」互不干扰，信息可扫读。
- **全览概括**：刻意不绑端口等部署实现细节，图只承载架构职责与数据语义，便于跨场景复用（文档封面 / 演示 / 入职介绍）。
