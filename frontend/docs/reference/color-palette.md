# Color Palette — 前端配色速查

> 色值以 `src/index.css` 为准。组件应通过 CSS 变量和 Tailwind `@theme inline` 映射使用颜色，避免在 JSX 中硬编码旧主题色。

---

## 一、设计风格关键词

**Dark Runtime Workspace · 墨青 · 克制 · 可扫视 · 工具感**

- 主体使用墨青灰阶，不使用纯黑背景。
- 全局品牌色为 cyan/teal，不再使用紫蓝 Indigo 作为主品牌色。
- 大面积聊天区和 SkillsHub 可使用 `.chat-canvas` 微点阵纹理，避免空黑。
- 阴影只服务层级：消息块、技能卡、头像、弹层允许低透明阴影。
- Agent 色是身份标识，不作为全局 UI 装饰色。

---

## 二、Dark 背景色阶

| 层级 | 用途                        | 色值      | 变量                           | Tailwind                    |
| :--: | --------------------------- | --------- | ------------------------------ | --------------------------- |
|  0   | 主画布 / 聊天区 / SkillsHub | `#0B1110` | `--background` / `--bg-canvas` | `bg-background`             |
|  1   | 侧边栏                      | `#101715` | `--sidebar` / `--bg-sidebar`   | `bg-sidebar`                |
|  2   | 卡片 / 消息块               | `#141C1A` | `--card` / `--bg-card`         | `bg-card`                   |
|  3   | 悬停 / 弱面                 | `#1E2A27` | `--accent` / `--bg-hover`      | `bg-accent` / `bg-bg-hover` |
|  4   | 按下 / 活跃                 | `#273531` | `--bg-active`                  | `bg-bg-active`              |

特殊背景：

| 用途           | 色值      | 变量        |
| -------------- | --------- | ----------- |
| 代码块         | `#0E1715` | `--code-bg` |
| Popover / 下拉 | `#192321` | `--popover` |

---

## 三、Dark 文字色阶

| 级别 | 用途                     | 色值      | 变量                                      | Tailwind              |
| :--: | ------------------------ | --------- | ----------------------------------------- | --------------------- |
|  主  | 标题、正文               | `#E6EFEC` | `--foreground` / `--text-primary`         | `text-foreground`     |
|  次  | 时间戳、描述、标签       | `#92A09C` | `--muted-foreground` / `--text-secondary` | `text-text-secondary` |
|  辅  | 占位符、禁用态、辅助信息 | `#697773` | `--text-tertiary`                         | `text-tertiary`       |

---

## 四、品牌色

| 用途        | 色值                    | 变量                          | 使用场景                 |
| ----------- | ----------------------- | ----------------------------- | ------------------------ |
| 品牌 / 主色 | `#5EEAD4`               | `--primary` / `--color-brand` | 发送按钮、选中态、焦点环 |
| 品牌前景    | `#05201C`               | `--primary-foreground`        | 主按钮文字/图标          |
| 品牌浅底    | `rgba(94,234,212,0.10)` | `--primary-soft`              | 侧栏选中行、用户气泡内嵌 blockquote |
| 品牌边框    | `rgba(94,234,212,0.20)` | `--primary-border`            | 侧栏选中边框、用户气泡内嵌 pre 边框 |

---

## 五、语义色

| 状态          | 色值                     | 变量                              | 使用场景                    |
| ------------- | ------------------------ | --------------------------------- | --------------------------- |
| 成功 / 就绪   | `#4ADE80`                | `--color-success`                 | 状态点、任务完成、Diff 新增 |
| 警告 / 运行中 | `#FBBF24`                | `--color-warning`                 | 运行状态、提醒              |
| 警告浅底      | `rgba(251,191,36,0.12)`  | `--warning-soft`                  | 警告提示底色                |
| 错误          | `#F97066`                | `--color-error` / `--destructive` | 错误状态、Diff 删除         |
| 错误浅底      | `rgba(249,112,102,0.10)` | `--color-danger-bg`               | 错误提示底色                |

语义色常以 `/5`、`/10`、`/20` 透明度作底色或边框，纯色主要用于文字、图标和状态点。

---

## 六、Agent 标识色

| Agent        | 色值      | 变量                   | 色相描述              |
| ------------ | --------- | ---------------------- | --------------------- |
| Claude Code  | `#DA7756` | `--agent-claude`       | 暖珊瑚 / Terracotta   |
| OpenCode     | `#10B981` | `--agent-opencode`     | 翡翠绿 / Emerald      |
| Orchestrator | `#EAB308` | `--agent-orchestrator` | 金黄 / Amber          |
| Codex        | `#5EEAD4` | `--agent-codex`        | Cyan/Teal（同品牌色） |

典型用法：

```tsx
// Agent 类型标签
style={{
  color: agentColor,
  backgroundColor: `${agentColor}1A`,
}}

// 无图片头像底色
style={{ backgroundColor: imgSrc ? 'transparent' : color }}
```

禁止用法：

- 不给头像加 `0 0 8px ${agentColor}` 一类外发光。
- 不把 Agent 色用于普通按钮、页面背景或导航选中态。

---

## 七、边框与阴影

| 用途     | 色值 / 写法                    | 变量             |
| -------- | ------------------------------ | ---------------- |
| 通用边框 | `rgba(255,255,255,0.06)`       | `--border`       |
| 输入边框 | `rgba(255,255,255,0.06)`       | `--input`        |
| 焦点环   | `#5EEAD4`                      | `--ring`         |
| 弹层阴影 | `0 18px 52px rgba(0,0,0,0.42)` | `--shadow-popup` |

常用组件阴影：

```tsx
// 消息块 / 技能卡
shadow-[0_12px_32px_rgba(0,0,0,0.08)]

// Agent 头像
shadow-[0_8px_20px_rgba(0,0,0,0.16)]
```

---

## 八、Diff / 代码对比色

| 用途         | 色值                     | 变量                      |
| ------------ | ------------------------ | ------------------------- |
| 新增行文字   | `#4ADE80`                | `--diff-insert-color`     |
| 新增行背景   | `rgba(74,222,128,0.08)`  | `--diff-insert-bg`        |
| 新增行强背景 | `rgba(74,222,128,0.10)`  | `--diff-insert-bg-strong` |
| 删除行文字   | `#F97066`                | `--diff-delete-color`     |
| 删除行背景   | `rgba(249,112,102,0.08)` | `--diff-delete-bg`        |
| 删除行强背景 | `rgba(249,112,102,0.10)` | `--diff-delete-bg-strong` |

---

## 九、Markdown / Prose 增强色

| 用途         | 色值                     | 变量                               |
| ------------ | ------------------------ | ---------------------------------- |
| 标题 / 加粗  | `#F0F6F4`                | `--prose-heading` / `--prose-bold` |
| 链接         | `#5EEAD4`                | `--prose-link`                     |
| 链接悬停     | `#99F6E4`                | `--prose-link-hover`               |
| 行内代码文字 | `#99F6E4`                | `--prose-code-text`                |
| 行内代码背景 | `rgba(94,234,212,0.12)`  | `--prose-code-bg`                  |
| 引用块边框   | `#5EEAD4`                | `--prose-bq-border`                |
| 引用块背景   | `rgba(94,234,212,0.07)`  | `--prose-bq-bg`                    |
| 列表标记     | `#5EEAD4`                | `--prose-li-marker`                |
| 分割线       | `rgba(255,255,255,0.08)` | `--prose-hr`                       |

---

## 十、图表色

| 序号 | 色值      | 变量        |
| :--: | --------- | ----------- |
|  1   | `#5EEAD4` | `--chart-1` |
|  2   | `#4ADE80` | `--chart-2` |
|  3   | `#FBBF24` | `--chart-3` |
|  4   | `#F97066` | `--chart-4` |
|  5   | `#92A09C` | `--chart-5` |

---

## 十一、Light 主题速查

| 用途     | 色值      | 变量                              |
| -------- | --------- | --------------------------------- |
| 主画布   | `#FBFCFB` | `--background` / `--bg-canvas`    |
| 侧边栏   | `#F3F7F5` | `--sidebar` / `--bg-sidebar`      |
| 卡片     | `#FFFFFF` | `--card` / `--bg-card`            |
| Hover    | `#E9F0ED` | `--bg-hover`                      |
| Active   | `#DDE8E4` | `--bg-active`                     |
| 主文字   | `#17211F` | `--foreground` / `--text-primary` |
| 次文字   | `#64716E` | `--text-secondary`                |
| 辅助文字 | `#8A9994` | `--text-tertiary`                 |
| 品牌色   | `#0F766E` | `--primary` / `--color-brand`     |
| 成功     | `#218358` | `--color-success`                 |
| 警告     | `#B7791F` | `--color-warning`                 |
| 错误     | `#B42318` | `--color-error` / `--destructive` |

---

## 十二、使用规则

1. 新增视觉样式先看 `src/index.css` 是否已有 token。
2. 组件中优先使用 `bg-card`、`bg-muted`、`text-text-secondary`、`border-border` 等语义类。
3. 只在动态 Agent 色、尺寸计算、进度条宽度等无法用静态 class 表达时使用 `style`。
4. 不新增紫蓝主色，不新增霓虹发光，不用纯黑/纯白作为大面积 UI 色。
