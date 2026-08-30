# 前端后续优化实施计划

> 基线日期：2026-08-27  
> 范围：仅 `frontend/`  
> 状态：代码实现与纯逻辑自动化验证完成；浏览器实测及组件/交互回归待执行  
> 关联历史计划：[`docs/common/frontend-review-fix.md`](../../../docs/common/frontend-review-fix.md)

## 一、背景

当前前端已经具备较完整的工程与视觉基础：React 19、Vite 8、Tailwind CSS 4、shadcn/Radix、TanStack Query、Zustand、路由懒加载、消息虚拟列表、浅深色 token、骨架屏、空/错状态、键盘焦点样式和减少动画适配。

下一轮不重做视觉、不替换技术栈，重点处理仍会影响 Runtime Workspace 体验的六类问题：

1. 会话列表服务端缓存可能与活动 SSE 状态不同步；
2. 主工作台和首个代码块加载的 JavaScript 偏多；
3. 少量暗色专用样式泄漏到浅色主题；
4. 部分管理视图的无障碍语义和弹窗行为不统一；
5. 重复控件样式与超大文件增加维护成本；
6. 页面和组件级关键交互缺少自动化覆盖。

## 二、当前基线

2026-08-27 仓库验证结果：

| 检查项                     | 结果                                                     |
| -------------------------- | -------------------------------------------------------- |
| ESLint                     | 0 error，1 条 React Compiler/TanStack Virtual 兼容性警告 |
| Vitest                     | 7 个文件、64 个测试，全部通过                            |
| TypeScript + Vite 生产构建 | 通过                                                     |
| 基础入口 chunk             | 260.99 kB raw / 83.02 kB gzip                            |
| `ImPage` chunk             | 451.74 kB raw / 132.38 kB gzip                           |
| 组件测试                   | 0                                                        |
| 页面测试                   | 0                                                        |
| 原生 `<button>` 使用量     | 约 131 处                                                |

审计时三端服务未运行，因此浏览器 Performance、Lighthouse、真实网络瀑布、接口数据下的响应式表现，需要在阶段 0 补齐。

### 2.1 实现后自动化与构建记录

2026-08-28 继续审查后的最新验证结果：

| 检查项                       | 结果                                                                                      |
| ---------------------------- | ----------------------------------------------------------------------------------------- |
| ESLint                       | 0 error，0 warning；TanStack Virtual 仅在集成边界使用局部 compiler opt-out                |
| TypeScript                   | `tsc -b` 通过                                                                             |
| Vitest                       | 9 个文件、81 个测试，全部通过                                                             |
| 基础入口 chunk               | 262.68 kB raw / 83.60 kB gzip                                                             |
| `ImPage` chunk               | 347.80 kB raw / 100.48 kB gzip                                                            |
| 基础入口 + `ImPage`          | 184.08 kB gzip（两个命名入口 chunk 之和，低于 190 kB 目标）                               |
| `/chat` 初始 JS 静态依赖闭包 | 约 237.07 kB gzip（含 `page-title` 共享 chunk 与 Vite preload 支持 chunk，仍高于 190 kB） |
| 结构化消息与编辑器           | `DiffCard`、`HtmlCard`、`PreviewCard`、`PlanReviewCard`、CodeMirror 均已拆为按需 chunk    |
| Shiki                        | core/engine/theme 单次初始化；语言 grammar 按当前代码块动态加载并共享 in-flight 请求      |

构建报告另生成了 `page-title` 共享 chunk（100.79 kB raw / 34.74 kB gzip），其中包含多个路由共同使用的 Radix/common 依赖。上表的 184.08 kB 仅用于与原始报告保持“两个命名入口 chunk”同口径；把 `/chat` 的 preload 依赖一并计算后约为 237.07 kB gzip，说明真实首屏仍未达到 190 kB 预算，实际网络请求和 Brotli 传输量仍需浏览器 Network 面板确认。

本轮未修改后端、Agent 端或 contracts。由于当前执行环境没有浏览器服务和真实接口 fixture，阶段 0 要求的 Performance/Lighthouse、真实网络瀑布、截图矩阵，以及阶段 6 的浏览器交互回归仍待在本地服务启动后执行。

## 三、目标与非目标

### 3.1 目标

- 发送消息及 SSE 结束后，会话状态、排序、标题和最后活动时间保持一致。
- 降低工作台首次加载体积，结构化消息能力只在真正使用时加载。
- Shiki 只加载当前代码块需要的语言，而不是一次加载全部语言。
- Markdown、终端和 Preview 在浅色、深色主题下均使用正确语义色。
- 在不改变视觉定位的前提下改善键盘、读屏和触控体验。
- 为高风险流程建立针对性的交互测试。
- 每批修改都能独立验证、独立提交和独立回滚。

### 3.2 非目标

- 不迁移 React、Router、状态库、CSS 框架或组件库。
- 不做全量视觉改版，不引入新的品牌色体系。
- 除非现有接口与事件确实无法提供所需状态，否则不修改后端和契约。
- 不把所有原生控件一次性包装成公共组件。
- 不顺带清理 `backend/`、`agentend/` 或生成契约文件。

## 四、交付顺序

按六批实施，每批完成自动化检查和对应手工验收后再进入下一批。阶段 1～3 不合并成一个大改动，它们分别涉及状态正确性、加载路径和主题渲染，需要保持容易定位与回滚。

推荐顺序：

1. 建立浏览器与 bundle 可比较基线；
2. 修复会话列表与 SSE 的状态一致性；
3. 拆分重型运行时 UI，并让 Shiki 按语言加载；
4. 修复浅深色主题语义；
5. 完善无障碍、弹窗和重复交互组件；
6. 补关键交互测试并处理编译器边界警告。

## 五、阶段 0：建立可度量基线

### 5.1 修改内容

- 使用本地后端真实数据或稳定 fixture 启动前端，记录：
  - `/chat` 冷启动请求数与 JavaScript 传输体积；
  - 会话列表首次可见时间；
  - 打开会话所需时间；
  - 首个代码块出现时的新增请求与传输体积；
  - 超过 100 条消息时的滚动和历史加载表现；
  - 360×800、390×844 两档移动端布局；
  - 1280×800、1440×900、1920×1080 三档桌面布局；
  - Chat、Markdown、SkillsHub、Admin Statistics 的浅/深色截图。
- 保存当前生产构建尺寸作为后续对比基线。
- 优先使用浏览器 Performance 和 Network 面板，不预先增加新的性能依赖。

### 5.2 产物

在本文增加带日期的实测结果小节，记录数值与截图路径；不提交生成的 `dist/`。

### 5.3 验收

- 优化前后使用同一会话、同一代码样例。
- 同时记录冷缓存与第二次暖缓存导航。
- 浅/深主题和移动端/桌面端均有可比较截图。

## 六、阶段 1：会话列表与 SSE 缓存一致性

### 6.1 问题

`useConversations()` 使用 `['conversations']` 保存服务端列表，`useChatStream()` 则把活动运行态写入 Zustand。消息提交和 SSE `done`/`error` 当前不会同步或失效会话 Query；同时全局关闭了 `refetchOnWindowFocus`，因此左侧列表可能持续保留旧状态、旧排序和旧 `lastActiveAt`。

涉及文件：

- `src/hooks/use-conversations.ts`
- `src/hooks/use-chat-stream.ts`
- `src/components/im/ConversationItem.tsx`
- `src/lib/query-keys.ts`

### 6.2 实施方式

1. 建立会话 Query Key 单一来源，同时保留现有 `isAdminQueryKey()`：

   ```ts
   export const queryKeys = {
     conversations: ['conversations'] as const,
   }
   ```

2. 增加一个不可变、带类型的缓存更新函数：

   ```ts
   function patchConversation(
     current: Conversation[] | undefined,
     sessionId: string,
     patch: Partial<Conversation>,
   ): Conversation[] | undefined
   ```

   未找到目标时返回原引用，禁止直接修改缓存对象。

3. 在 `useChatStream()` 中获取 `queryClient`，按生命周期同步：

   - 提交成功或流开始时：
     - 立即把目标会话设为活动状态；
     - 把 `lastActiveAt` 更新为当前 ISO 时间；
     - 如果接口约定列表按活跃时间排序，同步调整目标项位置；
   - SSE `done`、`error` 或取消请求后收到的终止事件：
     - 先更新当前可见终态；
     - 仅失效一次 `queryKeys.conversations`，以服务端为最终权威；取消请求成功后保持 SSE 消费，避免在 AgentEnd 持久化终态前抢先读取旧状态；
   - `text`、`heartbeat` 等高频事件不触发失效。
   - 会话组件卸载或重新进入时清理本地临时流状态，并在历史加载完成后做一次低频列表对账，避免离开期间错过终态事件。

4. 群聊不能只按 `taskId` 批量更新。应使用流 session、主会话和现有群聊元数据定位可见行，避免同 Task 下其他 Agent 被误标为运行中。

5. 相对时间使用一个列表级分钟 ticker 更新，不给每一行创建定时器；同时在 `<time title>` 中提供完整本地时间。

### 6.3 测试

- 为 `patchConversation()` 覆盖：命中、未命中、不可变性、群聊定位。
- Hook/集成测试覆盖：
  - 发送成功后立即显示运行中；
  - `done` 只失效一次；
  - `text`/`heartbeat` 不失效；
  - `error`/取消恢复终态；
  - 同 Task 的其他会话不被错误更新。

### 6.4 验收

- 发送后左侧列表无需完整 refetch 即显示运行中。
- 结束或失败后能取得服务端最终状态。
- 静置超过一分钟后，相对时间仍会更新。
- 不会因每个流式 token 额外请求会话接口。

## 七、阶段 2：工作台 bundle 与代码高亮优化

### 7.1 问题

原实现的 `BlockRenderer` 静态导入全部结构化卡片，使仅查看文本聊天的用户也会通过主聊天依赖树获得 Diff、Preview、Plan Review、Runtime 等代码；原实现的 `CodeBlock.getHighlighter()` 还会在第一个高亮代码块出现时，通过一个 `Promise.all` 同时加载 15 种语言。

涉及文件：

- `src/components/chat/BlockRenderer.tsx`
- `src/components/cards/index.ts`
- `src/components/markdown/CodeBlock.tsx`
- `src/pages/ImPage.tsx`
- `vite.config.ts`

### 7.2 结构化卡片按需加载

1. 文本渲染和很小的状态组件继续同步加载。
2. 从以下高成本或低频组件开始建立懒加载边界：
   - `DiffCard` 及编辑器/查看器依赖树；
   - `HtmlCard` 和 Preview；
   - `PlanReviewCard`；
   - 构建分析确认有收益时再拆 `RuntimeStatus`；
   - 初始折叠的 Git Graph 和 Terminal 内容。
3. 使用与目标 block 高度接近的 `Suspense` fallback，不能使用页面级 spinner，以免消息滚动跳动。
4. 每个异步 block 使用现有 ErrorBoundary 或新增 block 级边界，动态导入失败不能让整条消息或列表白屏。
5. 不为小图标、小状态标签制造碎片 chunk。每增加一个边界都重新构建，只保留实际降低传输量或交互等待的拆分。

### 7.3 Shiki 按语言加载

1. 用带类型的 loader map 替换全部语言 `Promise.all`：

   ```ts
   const LANGUAGE_LOADERS = {
     javascript: () => import('@shikijs/langs/javascript'),
     typescript: () => import('@shikijs/langs/typescript'),
     // ...
   } satisfies Record<SupportedLanguage, LanguageLoader>
   ```

2. Shiki core、engine、theme 只初始化一次；语言规范化后仅对当前语言调用 `loadLanguage()`。
3. 缓存 core promise 和每种语言的 in-flight promise，同语言多个代码块共享请求。
4. 加载过程中保持纯文本 fallback；动态导入失败时清理失败缓存，保留再次尝试能力。
5. 已 memo 且内容未改变的代码块不能因父级流式更新重新高亮。

### 7.4 初始预算

阶段 0 实测后可微调预算，首轮目标如下：

| 资源                |                  当前 gzip |                      目标 |
| ------------------- | -------------------------: | ------------------------: |
| `ImPage` chunk      |                  132.38 kB |               低于 105 kB |
| 基础入口 + `ImPage` |                  215.40 kB |               低于 190 kB |
| 首个高亮代码块      | engine + theme + 15 种语言 | engine + theme + 当前语言 |

如果必须使用脆弱的手工分包才能达标，应记录原因和稳定的最优结果，不为数字强行拆包。

### 7.5 测试与验收

- 生产构建通过并记录新尺寸。
- 文本聊天不请求 Diff/CodeMirror chunk。
- Python 代码块不会加载 TypeScript、Go、Rust、SQL grammar。
- 切换会话不会重复下载或初始化已加载语言。
- block fallback 不造成明显滚动位移。

## 八、阶段 3：浅深色主题正确性

### 8.1 问题

部分组件绕过语义 token，使用暗色专用的半透明白色。浅色模式下 Markdown 边框几乎不可见，Shiki 也始终使用 `tokyo-night`。

涉及文件：

- `src/components/markdown/MarkdownRenderer.tsx`
- `src/components/markdown/CodeBlock.tsx`
- `src/components/chat/TerminalPanel.tsx`
- `src/index.css`
- `src/hooks/use-theme.ts`

### 8.2 实施方式

1. 修复 Markdown 硬编码颜色：
   - `border-white/5`、`border-white/8` 改为 `border-border` 或专用 prose border token；
   - 图片、表格、标题分隔线使用同一语义边框族；
   - 保证浅色可见，同时避免暗色边框过重。
2. 终端的 `bg-white/[0.02-0.03]` 改用 `bg-muted/30`、`bg-bg-subtle` 或专用 terminal surface token。
3. 配置 Shiki 双主题：
   - 深色保持 `tokyo-night`；
   - 浅色使用 `github-light` 等配套主题；
   - 一次生成两套主题 CSS 变量，通过 `.dark` 切换，避免每次切主题重新高亮。
4. 增加 `system` 主题选项：
   - `Theme` 改为 `'system' | 'dark' | 'light'`；
   - 通过 `matchMedia('(prefers-color-scheme: dark)')` 解析系统主题；
   - 只在 system 模式订阅系统变化；
   - localStorage 保存用户偏好，而不是解析后的主题；
   - 同步更新 HTML 启动脚本，避免 React 挂载前闪烁错误主题。
   - 在应用根部保留轻量主题同步组件，避免设置面板按需卸载后丢失 system 监听。
5. 全量检查残留的 `border-white`、`bg-white`、固定黑色遮罩和主题相关内联阴影，仅保留在两种主题下都满足对比度的有意覆盖。

### 8.3 测试与验收

- 当前自动化测试覆盖 `light/dark/system` 解析；localStorage 初始化、`matchMedia` 事件和 DOM 主题切换需要浏览器回归确认。
- Markdown 标题、表格、图片、行内代码、围栏代码及终端在两种主题下均层级清晰。
- 主题切换不重复获取 grammar，代码块不会闪回纯文本。
- 首屏在 React 挂载前就使用正确的存储/系统主题。

## 九、阶段 4：无障碍与交互一致性

### 9.1 弹窗统一

在行为匹配的前提下，把自定义弹窗迁移到现有 Radix/shadcn Dialog：

- SkillsHub 上传和删除确认；
- Agent Profile 导入 Skill；
- Admin 二次认证；
- 响应式会话详情使用基于 Dialog 的 Sheet 形态，而不是居中 Modal。

保留现有布局，统一获得焦点捕获、Escape、焦点恢复、ARIA、Portal 和滚动锁。每迁移一个弹窗同时添加对应回归测试，不做纯标记替换。

### 9.2 数据图表语义

- 资源条增加 `role="progressbar"`、`aria-valuemin`、`aria-valuemax`、`aria-valuenow` 和明确 label。
- 日/周切换控件提供命名组和一致的选中状态。
- 会话与存储趋势图增加读屏专用表格或简洁文本摘要。
- 保留可见数值，信息不能只依赖颜色或柱高。

### 9.3 禁用操作说明

不要只依赖禁用按钮上的 `title`：禁用控件不一定能获得 hover/focus，触屏也不会展示 title。SkillsHub 上传等管理员操作应选择一种方式：

- 用可聚焦 wrapper 触发 Tooltip；或
- 显示附近的内联说明，通过 `aria-describedby` 关联。

说明文案需要明确告诉用户“请先登录管理员账户”。

### 9.4 页面身份

- 根据路由与当前会话更新 `document.title`，例如 `Agent 名称 · AgentHub`。
- token 流式更新期间不修改标题，避免浏览器标签持续重绘。
- 使用一个路由标题 helper，不在每个叶子组件重复设置。

### 9.5 验收

- 所有迁移弹窗关闭后焦点返回触发器。
- 弹窗不会遗留 body 滚动锁。
- 纯键盘用户能理解并操作图表切换与管理员操作。
- 读屏能获取资源百分比和趋势摘要。
- 浏览器标签能识别当前页面或会话。

## 十、阶段 5：有边界的组件收敛

### 10.1 原则

遵循 `frontend/docs/guides/development-strategy.md`：只抽象已确认的重复模式，直接扩展 shadcn，不增加 `BaseXXX` 层。

### 10.2 实施方式

1. 只增加重复行为明确的基础组件：
   - `Button`：primary、secondary、destructive、quiet 变体；
   - `IconButton` 作为 Button 的 size/variant，而不是另一层基类；
   - 阶段 1～4 完成后仍有三处一致实现时，再抽搜索框组合。
2. 统一 focus、disabled、loading、hover、pressed 状态；业务组件继续负责布局和标签。
3. 按职责拆文件，而不是单纯按行数拆分：
   - 从 `SkillsHubPage.tsx` 提取弹窗逻辑和卡片展示；
   - 从 `AgentProfilePage.tsx` 提取表单与导入弹窗；
   - 如果普通输入和 Markdown 编辑仍各自复杂，从 `MessageInput.tsx` 分离两种编辑器；
   - 保持 `MessageList` 虚拟化逻辑与普通消息渲染隔离。
4. 本批不直接拆 1,000 行以上的 message store。必须先用测试描述其公开 action，再单独提出架构变更。

### 10.3 验收

- 重复控件 class 明显减少，但专用布局不受影响。
- 新基础组件完整保留焦点、加载、禁用和按压反馈。
- Page 文件以路由、数据编排为主，不再承载完整弹窗实现。
- class 重构不造成大范围视觉变化。

## 十一、阶段 6：交互覆盖与 React Compiler 边界

### 11.1 测试设施

按需要增加最小浏览器测试依赖，预计包括 React Testing Library、user-event、jest-dom 和 jsdom。现有 store/lib 纯逻辑测试尽量继续使用快速环境。

### 11.2 优先测试矩阵

| 区域       | 必测场景                                                           |
| ---------- | ------------------------------------------------------------------ |
| 输入框     | Enter 发送、Markdown/换行行为、空输入禁用、停止任务、@提及键盘导航 |
| 会话列表   | loading/error/empty/search、当前项、乐观运行态、相对时间更新       |
| SSE        | start/text/done/error/cancel、不按 token 失效 Query、重连清理      |
| 虚拟列表   | 阈值切换、回到底部、历史 prepend、搜索定位消息                     |
| 弹窗       | 初始焦点、Escape、遮罩关闭策略、焦点返回、loading 锁定             |
| 主题       | light/dark/system、Markdown 语义边框、Shiki 双主题输出             |
| 异步 block | fallback、加载成功、动态导入失败边界                               |

### 11.3 React Compiler 警告

TanStack Virtual 当前在 `MessageList` 触发 `react-hooks/incompatible-library`，应把它视为明确集成边界：

- 确认应用不依赖编译器对 virtualizer 的 memo；
- 把虚拟列表实现隔离到独立组件/模块；
- 如有需要，仅对这个边界使用官方支持的 compiler opt-out；
- 如果上游暂无兼容方案，只在最窄位置抑制并写明原因。

禁止全局关闭该规则。

### 11.4 验收

- 核心纯逻辑与 store 状态边界已有稳定、可重复测试；组件和浏览器交互覆盖仍待测试设施可用后补齐。
- 现有测试及新增覆盖共 81 个测试继续通过。
- ESLint 不存在未解释警告。
- 生产构建通过；长聊天、真实 SSE 和浏览器焦点回归仍待在本地服务启动后执行。

## 十二、统一验证方式

在 `frontend/` 执行：

```bash
pnpm lint
pnpm exec tsc -b
pnpm test
pnpm build
```

在仓库根目录执行服务级验证：

```bash
make run-frontend
make status
```

每个阶段都需要手工检查：两种主题、键盘导航、移动端与桌面端、单 Agent 会话、群聊、长会话，以及至少一条含 Markdown 代码和结构化 Diff block 的消息。

## 十三、提交与回滚边界

| 批次 | 建议提交范围                                                     | 主要回滚边界                    |
| ---- | ---------------------------------------------------------------- | ------------------------------- |
| 0    | `docs(frontend): record optimization baseline`                   | 仅文档                          |
| 1    | `fix(frontend): synchronize conversations with stream lifecycle` | Query 缓存 helper + stream 接入 |
| 2A   | `perf(frontend): lazy load structured message blocks`            | 异步 block registry             |
| 2B   | `perf(frontend): load shiki languages on demand`                 | highlighter loader              |
| 3    | `fix(frontend): align light and dark runtime surfaces`           | 语义 token + theme resolver     |
| 4    | `a11y(frontend): standardize dialogs and data semantics`         | 每个弹窗/图表独立迁移           |
| 5    | `refactor(frontend): consolidate repeated controls`              | 按基础组件逐个采用              |
| 6    | `test(frontend): cover critical workspace interactions`          | 测试设施和用例                  |

不要把行为修改与全量格式化混在一起。阶段 2 如果出现 chunk 加载失败或滚动不稳定，只回滚对应懒加载边界；阶段 1 如果乐观投影与服务端状态不一致，保留终态失效逻辑，先撤掉错误的乐观 patch。

## 十四、风险与缓解

| 风险                           | 缓解措施                                             |
| ------------------------------ | ---------------------------------------------------- |
| 乐观状态与后端状态名不一致     | 集中状态映射，终态事件始终向服务端对账               |
| 群聊更新到错误会话             | 按 session/group identity 匹配，补同 Task 多会话测试 |
| 异步 block 导致滚动跳动        | 使用等高 fallback，并测试 virtualizer 重新测量       |
| 小 chunk 过多增加请求开销      | 只保留实测有收益的高价值边界                         |
| Shiki 双主题导致重新高亮或闪烁 | 一次输出两套变量，通过 CSS 切换                      |
| 弹窗迁移改变关闭策略           | 每次只迁移一个并保留 loading guard                   |
| 控件收敛演变为视觉改版         | 保留现有 token 和布局，只抽象已证实重复              |

## 十五、完成定义

全部满足以下条件才视为本轮优化完成：

- 会话行无需手工刷新即可反映流开始和终态；
- 终态事件只向后端对账一次；
- 文本聊天不加载 Diff/编辑器代码；
- 首个代码块只加载需要的 Shiki 语言；
- 达到约定 bundle 预算，或用实测记录偏差原因；
- Markdown 和终端通过浅/深色视觉检查；
- 资源条、趋势图、管理员操作及迁移后的弹窗具备完整交互语义；
- 输入框、SSE、虚拟列表、弹窗、主题和异步加载关键流程已有自动化覆盖；
- lint、类型检查、测试、生产构建和手工回归矩阵全部通过；
- 根据最终实现更新相关 frontend design/reference 文档，而不是让提案替代实现说明；
- 只有实际发生跨端 schema 变化时才新增 `contracts/logs/` 记录。
