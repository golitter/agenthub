# 前端审查修复计划（fix/frontend-review-issues）

> 基于 2026-08-05 对 `frontend/` 的全量代码审查。所有问题均经源码 + 生产构建 CSS 产物交叉验证。
> 静态基线：`eslint` / `tsc -b` / `vitest 45/45` / `vite build` 全绿。
>
> 修复原则：**先低风险高收益（token / 常量 / key），后高风险重构（流式性能）**。每批改完独立验证。
>
> **当前状态（2026-08-17 复核）**：阶段一（Tailwind token）、阶段二全部（2.1 Git Graph 车道宽度 `LANE_WIDTH=120`、2.2 CodeMirror Tab 重建、2.3 无 hunk 文件保护）、阶段三 3.1（activeStream 设置）、3.4（loadMoreMessages 依赖收敛）、阶段四全部（4.1 getSafeHttpUrl、4.2 use-resize 方向、4.3 ConversationList 宽度、4.4 导航竞态守卫、4.5 Admin token 过期、4.6 WorkspacePage 兜底、4.7 Dialog 遮罩）已落地；3.2（memo 化）、3.3（estimateSize / 滚动协调）部分落地，明细见对应小节的"落地结果"。下文保留原始计划描述，每节标题后以 ✅ 标注已完成项。

---

## 阶段一：Tailwind v4 token 断链（严重 · 静默失效 · 低风险）✅ 已完成

Tailwind v4 规则：只有在 `@theme inline` 块里显式声明 `--color-xxx`，才会生成 `bg-xxx` / `text-xxx` 工具类。构建产物验证：以下 6 类样式规则数为 0。

### 1.1 补 `@theme inline` 缺失映射（3 行）
**文件**：`frontend/src/index.css` `@theme inline { ... }` 块内（约 64-67 行附近）

补三行：
```css
--color-agent-codex: var(--agent-codex);   /* 当前只有 claude/opencode/orchestrator */
--color-bg-subtle: var(--bg-subtle);        /* 当前完全未映射 */
--color-warning-soft: var(--warning-soft); /* 当前未映射 */
```

### 1.2 补 `--bg-subtle` 变量定义（agent-codex / warning-soft 已有，仅 bg-subtle 缺）
**文件**：`frontend/src/index.css`

`:root`（约 109 行后）与 `.dark`（约 229 行后）各补：
```css
/* light */  --bg-subtle: #f1f5f3;
/* dark  */  --bg-subtle: #121917;
```
> 取值对齐现有 `--bg-hover`(#ecf3f0) / `--bg-card` 之间的一档更浅背景。

### 1.3 全局修正拼写错误的类名
- `text-color-warning` → `text-warning`（2 处）
  - `frontend/src/components/chat/AnnouncementsSection.tsx:160`
  - `frontend/src/components/chat/HistorySearch.tsx:66`
- `text-destructive-foreground` → `text-destructive`（token 从未定义）
  - `frontend/src/components/cards/TaskFailureCard.tsx:17`

### 1.4 全局修正裸 `hover:bg-hover` → `hover:bg-bg-hover`（48 处）
项目定义的 token 是 `--color-bg-hover`（正确类名 `bg-bg-hover`，已有 13 处正确写法）。
**命令**：
```bash
cd frontend
grep -rl 'hover:bg-hover' src --include=*.tsx \
  | xargs sed -i 's/hover:bg-hover\b/hover:bg-bg-hover/g'
```
> 必须用 `\b` 边界，避免误伤已经是 `hover:bg-bg-hover` 的写法。

**验证**：改完 `pnpm build`，用 Python 解析 `dist/assets/index-*.css`，确认：
- `bg-agent-codex` / `bg-bg-subtle` / `bg-warning-soft` 选择器规则数 ≥ 1
- `hover:bg-bg-hover` 的 `:hover` 规则数 = 原 13 + 48
- `text-color-warning` / `text-destructive-foreground` 规则数 = 0（已消失）

---

## 阶段二：功能性 Critical（一两行修复 · 低风险）

### 2.1 Git Graph 车道宽度常量统一 ✅ 已完成
**问题**：`GitGraphPanel.getLaneX` 硬编码 `LANE_WIDTH=220`，而 `git-graph-types.ts` 导出 `LANE_WIDTH=64`，`GraphRenderer` 用 64 设 `<svg width>` → x>64 的车道全被裁剪。

**修复**：`frontend/src/components/chat/GitGraphPanel.tsx:14-22`
- 删除局部 `const LANE_WIDTH = 220`
- 从 `./git-graph-types` 导入 `LANE_WIDTH`
- `getLaneX` 的 step 计算改用导入的常量

> 注意：统一为 64 后车道会很窄。需评估：若要多分支可读，应把 `LANE_WIDTH` 提到更大值（如 120）并让 SVG width = `LANE_WIDTH`（types 和 renderer 自然跟随）。**推荐方案**：把 `git-graph-types.ts` 的 `LANE_WIDTH` 改为 `120`，删除 panel 内的局部常量，统一单一来源。

**落地结果**：采用了上述推荐方案 —— `git-graph-types.ts:101` 的 `LANE_WIDTH = 120`，`GitGraphPanel.tsx` 已从 types 导入该常量并用于 `getLaneX`，不再硬编码。

**验证**：构造 ≥3 分支的 git graph 数据，肉眼确认所有分支竖线/节点可见。

### 2.2 CodeMirror 编辑器 Tab 切换强制重建 ✅ 已完成
**问题**：`DiffFileEditorInner` 的 `useState(newContent)` 只在首次挂载生效，切 Tab 时不卸载编辑器 → 草稿串到新文件，保存跨文件错写。

**修复**（最小改动）：`frontend/src/components/cards/DiffCard.tsx:299`
给 `<DiffFileEditor>` 加 `key={activeFile?.newPath}`（或 `key={activeFileIndex}`），强制按文件重建实例，state 自然重置。

**验证**：多文件 diff，编辑 A 不保存 → 切 B → 确认 B 显示 B 内容；保存 B 写入正确内容。

### 2.3 diff 重建对无 hunk 文件的保护 ✅ 已完成
**问题**：`diff-parser.reconstructContent` 对 add/delete/rename/二进制（无 hunk）返回空串 → 编辑器空白 → 保存清空文件。

**修复**：`frontend/src/lib/diff-parser.ts`
- `reconstructContent` 返回 `null`（而非空串）表示"无内容可重建"
- `DiffFileEditor` / `DiffFileView`：当 `newContent == null` 时禁用编辑按钮、不渲染编辑器，UI 提示"该文件无 hunk，不可编辑"
- CRLF：暂记为已知限制（注释），完整修复需后端返回原始行尾信息，超出本次范围

**验证**：新增文件/二进制 diff 不再出现可编辑空白区。

**落地结果**：采用"调用方守卫"而非返回 null —— `reconstructContent` 仍返回字符串，但 `frontend/src/lib/diff-parser.ts` 注释明确约定：无 hunk 文件返回空串，调用方必须以 `hunks.length > 0` 判断后才能提供编辑/保存；编辑入口在 `DiffCard.tsx` 以 `canEdit={!!activeFile && activeFile.hunks.length > 0}` 关闭。CRLF 规范化已作为已知限制写入注释。

---

## 阶段三：流式性能与正确性（高风险 · 需回归测试）

### 3.1 `streamStart` 补设 `activeStream` ✅ 已完成
**文件**：`frontend/src/stores/message-store.ts:439-456`（streamStart）
**修复**：在 `streamStart` 里一并设置 `activeStream: { messageId, sessionId }`（connectToStream 已有 messageId 参数），使组件卸载重挂载后能正确识别正在进行的工作并重连。

**落地结果**：未改 `streamStart`，而是改为随 `sendMessage(sessionId, message, activeStream)` 传入并由 store 写入 `session.activeStream`（`use-chat-stream.ts` 调用）；另新增 `clearActiveStream` 清理中断路径残留，重挂载重连以 `currentSession.activeStream !== null` 判断。

### 3.2 流式组件 memo 化（性能）
**文件**：`frontend/src/components/chat/MessageRenderer.tsx`、`MessageBubble.tsx`、`BlockRenderer.tsx`、`markdown/MarkdownRenderer.tsx`、`markdown/CodeBlock.tsx`
**修复**：对上述组件 `export default React.memo(Component)`，并审视父组件传入的内联对象/函数 props（避免每次新引用击穿 memo）。

**落地结果（部分）**：`MessageRenderer` / `MarkdownRenderer` / `CodeBlock` 已改为 `memo()` 导出；`MessageBubble` / `BlockRenderer` 仍为普通命名导出，依赖已 memo 的 `MessageRenderer` 边界间接隔离，全量 memo 未做。

### 3.3 虚拟列表 estimateSize 改进 + 滚动协调
**文件**：`frontend/src/components/chat/MessageList.tsx:122`、`frontend/src/hooks/use-message-scroll.ts`
**修复**：
- `estimateSize` 按常见 block 类型给基线估算（plan≈400、diff≈400、final_summary≈300、html≈256、默认 80）
- `useMessageScroll` 在虚拟化模式下把滚动控制权完全交给 virtualizer，不直接操作 `scrollTop`
- 流式消息 `timestamp` 用 store 层记录的固定 `streamingStartedAt`，不在 `displayItems` 里每帧 `Date.now()`

**落地结果（部分）**：`estimateSize` 已按 block 类型基线估算（`MessageList.tsx` 的 `estimateBlockHeight` 逐 block 求和 + 文本长度兜底）；流式时间戳以 `MessageList.tsx` 的 `streamingStartedAtRef` 固定并刻意排除出 memo deps；`use-message-scroll.ts` 仍直接操作 `scrollTop`（滚动到底 / 加载恢复），虚拟化滚动控制权移交未做。

### 3.4 `loadMoreMessages` 依赖收敛 ✅ 已完成
**文件**：`frontend/src/components/chat/ChatArea.tsx:64-95`
**修复**：依赖数组从 `state.messages` 收敛为 `state.messages[0]?.dbId`（单独 memo 出来），避免每帧重建回调。

> 阶段三每项改完单独跑 `pnpm test` + 手动流式回归（长会话、切会话、历史加载）。

---

## 阶段四：安全与交互（中风险）

### 4.1 `getSafeHttpUrl` 收紧相对 URL ✅ 已完成
**文件**：`frontend/src/lib/utils.ts:22-30`
**修复**：解析后增加 `parsed.origin === base` 且输入非绝对 http(s) 时返回 null；或要求输入必须匹配 `/^https?:\/\//i`。仅放行显式 http(s) 绝对地址。

**落地结果**：`getSafeHttpUrl`（`frontend/src/lib/utils.ts:26`）已采用后一方案：先以 `/^https?:\/\//i` 预检、再校验解析后协议，相对 URL 一律返回 null。

### 4.2 `useResize` 键盘方向修正 ✅ 已完成
**文件**：`frontend/src/hooks/use-resize.ts:121`
**修复**：`e.key === 'ArrowLeft' ? -16 : 16`（当前反了），与鼠标拖拽方向、WAI-ARIA 一致。

### 4.3 `ConversationList` 移动端宽度 ✅ 已完成
**文件**：`frontend/src/components/im/ConversationList.tsx:40`
**修复**：`w-[calc(100vw-3.5rem)]` → `w-full md:w-[280px]`，由父 flex 容器约束。

### 4.4 导航竞态守卫 ✅ 已完成
**文件**：`frontend/src/pages/ImPage.tsx:243-260`
**修复**：会话校验 effect 在 `isLoading`/`isFetching` 期间不清空导航；仅当 `!isLoading && !isError` 且列表不含目标 id 时才 clear。

**落地结果**：ImPage 会话校验 effect 已以 `if (!conversations || !currentSessionId || conversationsLoading) return` 守卫，仅当列表稳定加载完成且不含目标会话时才清空导航。

### 4.5 Admin token 过期处理 ✅ 已完成
**文件**：`frontend/src/lib/api.ts`、`frontend/src/stores/admin.ts`
**修复**：保存 `expires_in`，用 `setTimeout` 在到期前 N 秒触发登出；二次验证 token 不覆盖主 token（用一次性 header 或独立存储）。

**落地结果**：`setAdminToken(token, expiresInSeconds)` + `scheduleExpiry`（到期前 min(30s, 10%) 触发登出监听）+ sessionStorage 缓存（刷新可恢复）已实现（`frontend/src/lib/api.ts`）；当前代码无独立二次验证 token 流程，该子项不再适用。

### 4.6 `WorkspacePage` 数值兜底 ✅ 已完成
**文件**：`frontend/src/pages/admin/WorkspacePage.tsx:69`
**修复**：`(stats.totalDisk ?? 0).toFixed(1)`。

**落地结果**：`WorkspacePage.tsx` 磁盘占用展示已为 `${(stats.totalDisk ?? 0).toFixed(1)} MB`。

### 4.7 Dialog 遮罩去纯黑 ✅ 已完成
**文件**：`frontend/src/components/ui/dialog.tsx:23`、相关 admin 页面
**修复**：`bg-black/50` → `bg-background/80 backdrop-blur-sm`。

**落地结果**：`frontend/src/components/ui/dialog.tsx` 的 Overlay 已使用 `bg-background/80 backdrop-blur-sm`。

---

## 不在本次范围（记录待办）

- 终端 `git log` 改可达性模型（需重设计 lane 语义，影响面大）
- rAF buffer 移入 store / WeakMap 隔离（架构级，见 message-store.ts 顶部 P2 计划）
- 流式状态迁出 Zustand 至 useReducer + TanStack Query（长期重构）
- `MembersSection` 嵌套 button/Link 重构（需改组件结构）
- 移动端响应式断点全量审查

---

## 验收清单

每个阶段完成后：
1. `make stop-frontend && make run-frontend`（或在 frontend 目录 `pnpm dev`）
2. `pnpm lint && pnpm exec tsc -b && pnpm test`
3. 阶段一额外：`pnpm build` 后解析 CSS 确认规则生成
4. 手动回归对应功能点

全部完成后更新 `contracts/logs/` 变更记录并提交。
