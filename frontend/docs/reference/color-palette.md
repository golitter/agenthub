# Color Palette — 前端配色速查

> 暗色模式（Dark Utilitarian）为唯一生产主题，色值均取自 `.dark` 块。
> 所有颜色通过 CSS 变量 + Tailwind `@theme inline` 映射为工具类，组件中不硬编码色值。

---

## 一、设计风格关键词

**Dark · Flat · Restrained · Functional**

- 层级靠 **色阶** 区分，不靠阴影/毛玻璃
- 色彩为 **功能服务**（标识身份、表达状态），不作为装饰
- 品牌色 Indigo `#6366F1` 极度克制——仅用于选中态、发送按钮、焦点环
- **无渐变**、**无 backdrop-blur**、**无纯白 `#FFFFFF`**

参考气质：Linear 的克制暗色 + Cursor 的 IDE 氛围 + Slack 的对话节奏。

---

## 二、背景色阶（5 级灰）

层级从深到浅，相邻层级 RGB 差值约 10-15 单位，靠色差建立深度。

| 层级 | 用途 | 色值 | 变量 | Tailwind |
|:----:|------|------|------|----------|
| 0 | 主画布 / 聊天区 | `#0A0B0E` | `--bg-canvas` / `--background` | `bg-background` |
| 1 | 侧边栏 | `#111318` | `--bg-sidebar` / `--sidebar` | `bg-sidebar` |
| 2 | 卡片 / Agent 气泡 | `#1A1D24` | `--bg-card` / `--card` | `bg-card` |
| 3 | 悬停 / 提升面 | `#22262F` | `--bg-hover` / `--accent` / `--muted` | `bg-bg-hover` / `bg-accent` / `bg-muted` |
| 4 | 按下 / 最浅提升 | `#2C313B` | `--bg-active` | `bg-active` |

特殊背景：

| 用途 | 色值 | 变量 |
|------|------|------|
| 代码块 | `#0D0F14` | `--code-bg` |
| Popover / 下拉 | `#22262F` | `--popover` |

---

## 三、文字色阶（3 级灰）

| 级别 | 用途 | 色值 | 变量 | Tailwind |
|:----:|------|------|------|----------|
| 主 | 标题、正文 | `#E8EBF0` | `--text-primary` / `--foreground` | `text-foreground` / `text-text-primary` |
| 次 | 时间戳、描述、标签 | `#8B91A0` | `--text-secondary` / `--muted-foreground` | `text-muted-foreground` / `text-text-secondary` |
| 辅 | 占位符、禁用态、辅助 | `#5A6070` | `--text-tertiary` | `text-text-tertiary` / `text-tertiary` |

---

## 四、品牌色

| 用途 | 色值 | 变量 | 使用场景 |
|------|------|------|----------|
| 品牌 / 主色 | `#6366F1` | `--color-brand` / `--primary` | 发送按钮、选中态、焦点环、当前会话标记 |
| 品牌浅底 | `rgba(99,102,241,0.08)` | `--primary-soft` | 用户消息气泡背景 |
| 品牌边框 | `rgba(99,102,241,0.15)` | `--primary-border` | 用户消息气泡边框 |

> 品牌色即 Indigo（靛蓝），等同于 Codex Agent 标识色。

---

## 五、语义色（Status Colors）

| 状态 | 色值 | 变量 | 使用场景 |
|------|------|------|----------|
| 成功 / 就绪 | `#22C55E` | `--color-success` | Agent ready 状态灯、任务完成、Diff 新增 |
| 警告 / 运行中 | `#F59E0B` | `--color-warning` | Agent running 状态灯、Plan 卡片 |
| 错误 | `#EF4444` | `--color-error` / `--destructive` | Agent error 状态灯、任务失败、Diff 删除 |

语义色常以低透明度作底色（如 `bg-success/10`、`bg-destructive/8`），高透明度或纯色仅用于图标/文字。

---

## 六、Agent 标识色

用于区分 Agent 身份，仅在头像、消息色条、Agent 专属卡片中出现，**不在 UI 框架上使用**。

| Agent | 色值 | 变量 | 色相描述 |
|-------|------|------|----------|
| Claude Code | `#DA7756` | `--agent-claude` | 暖珊瑚 / Terracotta |
| OpenCode | `#10B981` | `--agent-opencode` | 翡翠绿 / Emerald |
| Orchestrator | `#EAB308` | `--agent-orchestrator` | 金黄 / Amber |
| Codex | `#6366F1` | `--agent-codex` | 靛蓝（同品牌色） |

在组件中的典型用法：

```
头像背景:    ${agentColor}20     → 标识色 + 12% 透明度
头像光晕:    boxShadow: 0 0 8px ${agentColor}
消息色条:    左侧 3px border，标识色
Agent 卡片:  border-agent-orchestrator/15 bg-agent-orchestrator/5（如 PlanCard）
```

---

## 七、边框色

| 用途 | 色值 | 变量 | 说明 |
|------|------|------|------|
| 通用边框 | `rgba(255,255,255,0.06)` | `--border` | 几乎不可见的微妙分割 |
| 焦点环 | `#6366F1` | `--ring` | 品牌色 2px ring + 2px offset |
| Sidebar 边框 | `rgba(255,255,255,0.06)` | `--sidebar-border` | 与通用边框相同 |

> 组件中常以透明度变体使用：`border-border/60`、`border-border/80` 等。

---

## 八、Diff / 代码对比色

| 用途 | 色值 | 变量 |
|------|------|------|
| 新增行文字 | `#22C55E` | `--diff-insert-color`（即 success） |
| 新增行背景 | `rgba(34,197,94,0.08)` | `--diff-insert-bg` |
| 新增行强背景 | `rgba(34,197,94,0.1)` | `--diff-insert-bg-strong` |
| 删除行文字 | `#EF4444` | `--diff-delete-color`（即 error） |
| 删除行背景 | `rgba(239,68,68,0.08)` | `--diff-delete-bg` |
| 删除行强背景 | `rgba(239,68,68,0.1)` | `--diff-delete-bg-strong` |

---

## 九、Markdown / Prose 增强色

用于聊天区 Markdown 渲染，与主题色体系衔接。

| 用途 | 色值 | 变量 |
|------|------|------|
| 标题 / 加粗 | `#F0F2F7` | `--prose-heading` / `--prose-bold` |
| 链接 | `#818CF8` | `--prose-link` |
| 链接悬停 | `#A5B4FC` | `--prose-link-hover` |
| 行内代码文字 | `#C7D2FE` | `--prose-code-text` |
| 行内代码背景 | `rgba(99,102,241,0.12)` | `--prose-code-bg` |
| 引用块边框 | `#6366F1` | `--prose-bq-border` |
| 引用块背景 | `rgba(99,102,241,0.06)` | `--prose-bq-bg` |
| 列表标记 | `#6366F1` | `--prose-li-marker` |
| 分割线 | `rgba(255,255,255,0.08)` | `--prose-hr` |

---

## 十、图表色（Chart Colors）

| 序号 | 色值 | 变量 |
|:----:|------|------|
| 1 | `#6366F1` | `--chart-1`（Indigo） |
| 2 | `#22C55E` | `--chart-2`（Green） |
| 3 | `#F59E0B` | `--chart-3`（Amber） |
| 4 | `#EF4444` | `--chart-4`（Red） |
| 5 | `#8B91A0` | `--chart-5`（Gray） |

---

## 十一、Git 分支 / 作者色

| 对象 | 色值 | 变量 |
|------|------|------|
| main 分支 | `#8B91A0` | `--text-secondary` |
| task/* 分支 | `#F59E0B` | `--color-warning` |
| agent/* 分支 | `#6366F1` | `--primary` |
| Orchestrator 作者 | `#EAB308` | `--agent-orchestrator` |
| Claude Code 作者 | `#DA7756` | `--agent-claude` |
| OpenCode 作者 | `#10B981` | `--agent-opencode` |

---

## 十二、透明度速查

组件中大量使用 Tailwind 透明度后缀生成浅底色 / 淡色文字，常见档位：

```
/5  /8  /10  /15  /20  /25  /30  /40  /50  /60  /70  /80  /85  /95
```

典型模式：

| 模式 | 写法 | 效果 |
|------|------|------|
| 状态底色 | `bg-success/10` | 成功色 10% 透明度底 |
| 状态边框 | `border-destructive/25` | 错误色 25% 透明度边框 |
| 淡文字 | `text-destructive/75` | 错误色 75% 透明度文字 |
| 卡片半透明 | `bg-card/95` | 卡片背景 95% 不透明 |

---

## 十三、配色总览图

```
┌──────────────────────────────────────────────────┐
│  背景色阶 (5 级)                                   │
│  ██████ #0A0B0E  ██████ #111318  ██████ #1A1D24  │
│  ██████ #22262F  ██████ #2C313B                    │
├──────────────────────────────────────────────────┤
│  文字色阶 (3 级)                                   │
│  ██████ #E8EBF0  ██████ #8B91A0  ██████ #5A6070  │
├──────────────────────────────────────────────────┤
│  品牌色                                           │
│  ██████ #6366F1  Indigo                           │
├──────────────────────────────────────────────────┤
│  语义色                                           │
│  ██████ #22C55E  ██████ #F59E0B  ██████ #EF4444  │
│  Success          Warning          Error          │
├──────────────────────────────────────────────────┤
│  Agent 标识色                                      │
│  ██████ #DA7756  ██████ #10B981  ██████ #EAB308  │
│  Claude           OpenCode        Orchestrator    │
│  ██████ #6366F1                                    │
│  Codex (同品牌色)                                   │
├──────────────────────────────────────────────────┤
│  Prose 增强                                        │
│  ██████ #818CF8  ██████ #A5B4FC  ██████ #C7D2FE  │
│  Link             Link Hover       Inline Code    │
│  ██████ #F0F2F7                                    │
│  Heading/Bold                                      │
└──────────────────────────────────────────────────┘
```

---

# Light 主题配色

> Light 主题已在 `:root` 中完整定义，包含 shadcn 基础 token、自定义语义色阶、Agent 标识色、Diff 对比色、Markdown prose 增强色等。与 Dark 主题共享相同的 Agent 标识色和语义色，仅背景/文字色阶反转。

---

## L-一、背景色

| 变量 | 色值 | 用途 |
|------|------|------|
| `--background` | `#FFFFFF` | 主画布背景 |
| `--card` | `#FFFFFF` | 卡片背景 |
| `--popover` | `#FFFFFF` | 弹出层背景 |
| `--sidebar` | `#F8F9FA` | 侧边栏背景 |
| `--secondary` | `#F5F5F5` | 次要面 / 输入框背景 |
| `--muted` | `#F5F5F5` | 淡化面 |
| `--accent` | `#F5F5F5` | 悬停 / 提升面 |
| `--bg-canvas` | `#FFFFFF` | 画布背景 |
| `--bg-sidebar` | `#F8F9FA` | 侧边栏背景 |
| `--bg-card` | `#FFFFFF` | 卡片背景 |
| `--bg-hover` | `#F1F3F5` | 悬停背景 |
| `--bg-active` | `#E9ECEF` | 按下 / 活跃背景 |
| `--code-bg` | `#F6F8FA` | 代码块背景 |

---

## L-二、文字色

| 变量 | 色值 | 用途 |
|------|------|------|
| `--foreground` | `#1A1A1A` | 主文本 |
| `--card-foreground` | `#1A1A1A` | 卡片内文字 |
| `--popover-foreground` | `#1A1A1A` | 弹出层文字 |
| `--primary` | `#6366F1` | 品牌色（Indigo，与 Dark 一致） |
| `--primary-foreground` | `#FAFAFA` | 品牌色上文字 |
| `--secondary-foreground` | `#262626` | 次要面文字 |
| `--muted-foreground` | `#737373` | 次要 / 弱化文字 |
| `--accent-foreground` | `#262626` | 提升面文字 |
| `--text-primary` | `#1A1A1A` | 自定义主文字 |
| `--text-secondary` | `#6B7280` | 自定义次要文字 |
| `--text-tertiary` | `#9CA3AF` | 自定义辅助文字 |

---

## L-三、品牌色与语义色

| 变量 | 色值 | 说明 |
|------|------|------|
| `--primary` | `#6366F1` | 品牌色（Indigo，与 Dark 一致） |
| `--destructive` | `#DC2626` | 错误红 |
| `--ring` | `#6366F1` | 焦点环（与品牌色一致） |
| `--color-brand` | `#6366F1` | 自定义品牌色 |
| `--color-success` | `#22C55E` | 成功色（与 Dark 一致） |
| `--color-warning` | `#F59E0B` | 警告色（与 Dark 一致） |
| `--color-error` | `#EF4444` | 错误色（与 Dark 一致） |
| `--primary-soft` | `rgba(99, 102, 241, 0.08)` | 品牌浅底 |
| `--primary-border` | `rgba(99, 102, 241, 0.15)` | 品牌边框 |
| `--color-danger-bg` | `rgba(239, 68, 68, 0.06)` | 错误底色 |

Agent 标识色与 Dark 模式完全一致：`--agent-claude: #DA7756`、`--agent-opencode: #10B981`、`--agent-orchestrator: #EAB308`、`--agent-codex: #6366F1`。

---

## L-四、边框色

| 变量 | 色值 | 说明 |
|------|------|------|
| `--border` | `#E5E5E5` | 通用边框 |
| `--input` | `#E5E5E5` | 输入框边框 |
| `--sidebar-border` | `#E5E5E5` | 侧边栏边框 |

---

## L-五、图表色

| 变量 | 色值 | 说明 |
|------|------|------|
| `--chart-1` | `#6366F1` | Indigo（与 Dark 一致） |
| `--chart-2` | `#22C55E` | Green |
| `--chart-3` | `#F59E0B` | Amber |
| `--chart-4` | `#EF4444` | Red |
| `--chart-5` | `#8B91A0` | Gray |

---

## L-六、滚动条

| 变量 | 值 | 说明 |
|------|-----|------|
| `--scrollbar-track` | `rgba(0, 0, 0, 0.04)` | 轨道背景 |
| `--scrollbar-thumb` | `rgba(0, 0, 0, 0.22)` | 滑块颜色 |
| `--scrollbar-thumb-hover` | `rgba(0, 0, 0, 0.34)` | 滑块悬停颜色 |

---

## L-七、Dark vs Light 对比速查

| 语义 | Dark 色值 | Light 色值 | 差异 |
|------|-----------|------------|------|
| 画布背景 | `#0A0B0E` | `#FFFFFF` | 黑 ↔ 白 |
| 侧边栏 | `#111318` | `#F8F9FA` | 深 ↔ 浅灰 |
| 卡片 | `#1A1D24` | `#FFFFFF` | 深 ↔ 白 |
| 主文字 | `#E8EBF0` | `#1A1A1A` | 亮 ↔ 暗 |
| 次文字 | `#8B91A0` | `#6B7280` | 冷灰 ↔ 暖灰 |
| 品牌色 | `#6366F1` | `#6366F1` | 相同 Indigo |
| 边框 | `rgba(255,255,255,0.06)` | `#E5E5E5` | 微光 ↔ 实色 |
| 焦点环 | `#6366F1` | `#6366F1` | 相同 |
| 错误色 | `#EF4444` | `#DC2626` | 相似红色 |

---

## L-八、Markdown / Prose 增强色（Light）

| 用途 | 色值 | 变量 |
|------|------|------|
| 标题 / 加粗 | `#1A1A1A` | `--prose-heading` / `--prose-bold` |
| 链接 | `#4F46E5` | `--prose-link` |
| 链接悬停 | `#6366F1` | `--prose-link-hover` |
| 行内代码文字 | `#4338CA` | `--prose-code-text` |
| 行内代码背景 | `rgba(99,102,241,0.10)` | `--prose-code-bg` |
| 引用块边框 | `#6366F1` | `--prose-bq-border` |
| 引用块背景 | `rgba(99,102,241,0.06)` | `--prose-bq-bg` |
| 列表标记 | `#6366F1` | `--prose-li-marker` |
| 分割线 | `rgba(0,0,0,0.08)` | `--prose-hr` |

---

> 完整设计规范参见 [visual-style-guide.md](visual-style-guide.md)，主题实现细节参见 [design/05-theme.md](../design/05-theme.md)。
