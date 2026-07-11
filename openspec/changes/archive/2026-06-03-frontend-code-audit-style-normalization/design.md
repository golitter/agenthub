## Context

前端代码库经过多轮功能迭代后，存在三类合规性问题：

1. **Tailwind CSS 4 类名误用**：`text-secondary` 在 Tailwind 4 中映射到 shadcn 的 `secondary` 前景色（暗色背景下 `hsl(210 40% 20%)` ≈ 深灰，近乎不可见），而项目自定义的次要文字色应使用 `text-text-secondary`（对应 CSS 变量 `--text-secondary: #8B91A0`，可见度高）。当前 4 个文件使用了错误的 `text-secondary`，另有多个文件使用 `var(--text-secondary)` inline style（功能正确但风格不统一）。

2. **硬编码元信息散落**：ContactsPage.tsx 中内联了 GitHub 仓库链接 `https://github.com/golitter/agenthub` 和项目描述文案。已有 `lib/constants.ts` 集中管理 Agent 常量，但项目级元信息尚未纳入。

3. **视觉规范偏差**：少量组件存在圆角超标（`rounded-2xl` = 16px > 规范最大 12px）、不必要阴影（`shadow-lg`/`shadow-md` 用于非弹出层）、硬编码语义色值（`bg-[#EF4444]` 等）等问题。

## Goals / Non-Goals

**Goals:**

- 修正所有 `text-secondary` → `text-text-secondary`，确保暗色背景下次要文字可见
- 统一 `var(--text-secondary)` inline style → Tailwind 类 `text-text-secondary`
- 将项目元信息（GitHub URL、项目名称、项目描述）提取到 `lib/constants.ts`，建立单一来源
- 替换 TerminalPanel 中硬编码色值为 CSS 变量或语义 Tailwind 类
- 修正圆角/阴影/边框的视觉规范违规

**Non-Goals:**

- 不重构组件架构或状态管理方案
- 不修改 shadcn/ui 内置组件（`components/ui/`）的阴影实现，这些是库的默认行为
- 不引入新的 CSS 工具类或 @apply 规则
- 不处理亮色主题（当前仅使用暗色主题）
- 不修改后端 API 或契约层

## Decisions

### Decision 1: 项目元信息放入 `lib/constants.ts` 而非新建文件

**选择**：扩展现有 `lib/constants.ts`，新增 `PROJECT_META` 常量对象。

**理由**：`constants.ts` 已包含 Agent 常量、消息角色常量、聊天状态常量等，是项目既有的常量集中点。项目元信息量小（URL + 名称 + 描述），不值得新建文件增加模块复杂度。遵循 development-strategy.md 的"第三次重复再抽象"原则。

**备选**：新建 `lib/settings.ts` — 过度设计，当前只有 3-5 个值。

### Decision 2: 硬编码语义色替换策略

**选择**：TerminalPanel 中的 `bg-[#EF4444]`、`bg-[#F59E0B]`、`bg-[#22C55E]` 替换为 Tailwind 语义类 `bg-destructive`、`bg-[var(--color-warning)]`、`bg-[var(--color-success)]`。

**理由**：CSS 变量 `--color-success`、`--color-warning`、`--destructive` 已在 index.css 中定义（通过 shadcn 的 `--destructive` 和自定义变量）。使用语义名比硬编码色值更易维护、更符合 design token 理念。

**备选**：直接用 Tailwind 默认的 `bg-red-500`/`bg-yellow-500`/`bg-green-500` — 虽然视觉接近，但不走 design token 体系，不利于主题一致性。

### Decision 3: 阴影处理策略 — 仅修自定义组件

**选择**：仅修正非 shadcn/ui 组件中的 `shadow-lg`/`shadow-md`（GitGraphPanel tooltip、IconSidebar tooltip、HistorySearch），将其替换为更深的背景色差（`bg-popover` + border）来表达层级。shadcn/ui 内置组件（popover.tsx、dialog.tsx）的阴影保持不变。

**理由**：shadcn/ui 组件的阴影是其默认样式的一部分，修改可能破坏组件视觉一致性。自定义组件则应遵循 style guide 的"深度靠色阶不靠阴影"原则。

### Decision 4: `text-secondary` 全面替换为 `text-text-secondary`

**选择**：将所有 Tailwind 类 `text-secondary` 替换为 `text-text-secondary`，将所有 `style={{ color: 'var(--text-secondary)' }}` inline style 替换为 Tailwind 类 `text-text-secondary`。

**理由**：`text-secondary` 在 Tailwind 4 + shadcn 配置中映射到 `hsl(var(--secondary-foreground))`，这是 shadcn 的按钮/徽章次要色，在暗色背景下 `#1e1e2e` 级别，几乎不可见。`text-text-secondary` 映射到 `var(--text-secondary): #8B91A0`，是 style guide 定义的正确次要文字色。统一使用 Tailwind 类而非 inline style，保持代码风格一致性。

## Risks / Trade-offs

- **[低风险] 视觉微调差异**：移除 tooltip shadow 后可能感觉"不够浮起" → 用 `bg-popover`（最浅背景级）+ 边框补偿视觉层级，确保弹出内容与背景有足够对比
- **[低风险] 常量文件膨胀**：每次新增常量都往 constants.ts 加 → 当前文件 56 行，新增 5-10 行后仍在合理范围。设置 150 行红线，超过时再按领域拆分
- **[无风险] text-secondary 替换**：纯等价替换，`text-text-secondary` 色值 `#8B91A0` 比原来 shadcn secondary 前景色亮很多，是正确的改进
