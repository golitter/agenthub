# 用户旅程流程图（Mermaid）

> 来源：[产品设计文档.md §3.3 典型用户旅程（端到端）](../产品设计文档.md)（行 112–121）。
> 同主题可交互版本见 [user-journey-flow.html](../user-journey-flow.html)。

## 端到端用户旅程

```mermaid
flowchart TD
    Start([登录]) --> List["会话列表（左侧）<br/>置顶优先 + 最近活跃"]
    List --> New{"新建会话"}

    %% ===== 单聊分支 =====
    New -->|单聊| Pick1["选择单个 Agent<br/>Claude Code / OpenCode / Codex"]
    Pick1 --> Send1["发消息"]
    Send1 --> Stream1["流式输出（SSE 逐字）"]
    Stream1 --> Art1["产物内联预览<br/>Diff 卡 / HTML 卡 / 图片卡"]
    Art1 --> Apply1["一键应用产物（写入工作区）"]

    %% ===== 群聊分支 =====
    New -->|群聊| Pick2["选择多个 Agent（含 Orchestrator）"]
    Pick2 --> Send2["发消息"]
    Send2 --> Plan["Orchestrator 自动规划<br/>拆解 + 分派"]
    Plan --> Review{{"规划审查卡<br/>（人工介入点）"}}
    Review -->|驳回 / 修正| Plan
    Review -->|批准| Exec["子 Agent 依次执行<br/>运行时状态卡实时更新"]
    Exec --> Art2["产物内联预览<br/>Diff / HTML / 图片"]
    Art2 --> Summary["Orchestrator 聚合总结"]
    Summary --> Done["应用 / 部署 / 二次迭代"]

    %% ===== 二次迭代回路 =====
    Done -. "继续迭代（上下文连续）" .-> Send2
    Apply1 -. "继续迭代（上下文连续）" .-> Send1

    classDef agent fill:#e8f0fe,stroke:#4285f4,color:#1a1a1a;
    classDef review fill:#fef7e0,stroke:#f9ab00,color:#1a1a1a;
    classDef artifact fill:#e6f4ea,stroke:#34a853,color:#1a1a1a;
    class Pick1,Pick2,Exec,Summary agent;
    class Review review;
    class Art1,Art2 artifact;
```

## 设计要点

- **分支结构**：菱形判断节点「新建会话」拆出单聊 / 群聊两条主线（对应 FR-IM-02，群聊必须可包含 Orchestrator）。
- **人工介入**：`规划审查卡` 用菱形 + 黄色高亮，强调「批准 → 继续 / 驳回 → 重规划」的决策点（对应 FR-OR-04，每次执行前必审、不可绕过）。
- **运行时状态卡**：子 Agent 节点标注「实时更新」，体现群聊中各 Agent 像群成员依次发言的过程可见性（对应 FR-OR-02 波次可视化）。
- **产物卡**：两类分支都收敛到 Diff / HTML / 图片三类内联产物，绿色高亮（对应 FR-AR-01/02/03）。
- **二次迭代回路**：虚线箭头连回「发消息」，表示上下文连续、可多轮迭代修改（对应 FR-IM-04）。
