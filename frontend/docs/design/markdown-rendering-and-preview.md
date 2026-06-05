# 聊天对话 Markdown 渲染 + 输入栏双栏实时预览

## 背景

当前聊天区域中，只有 Agent 消息使用 `MarkdownRenderer` 渲染 Markdown，**用户消息是纯文本**。输入栏是普通 textarea，无法预览 Markdown 渲染效果。

目标：
1. 用户消息也支持 Markdown 渲染
2. 输入栏增加双栏实时预览模式（左编辑 / 右预览），用户可以实时看到 Markdown 渲染效果

## 修改方案

### 1. 用户消息 Markdown 渲染

**文件**：`src/components/chat/MessageRenderer.tsx`

**改动**：将用户消息从纯文本改为 `MarkdownRenderer` 渲染。

```diff
  if (msg.role === MESSAGE_ROLES.USER) {
-   return <MessageBubble variant="user">{msg.content}</MessageBubble>
+   return (
+     <MessageBubble variant="user">
+       <MarkdownRenderer content={msg.content} />
+     </MessageBubble>
+   )
  }
```

### 2. 输入栏双栏实时预览

**文件**：`src/components/chat/MessageInput.tsx`

**改动要点**：

- **受控 textarea**：引入 `useState` 管理 textarea 值（当前直接操作 `textareaRef.current.value`，需改为受控组件以驱动预览）
- **预览开关按钮**：在发送按钮左侧添加 `Eye`/`EyeOff` 图标按钮（来自 lucide-react）
- **双栏布局**：当预览模式开启时，输入区域变为左右双栏：
  - 左栏：textarea 编辑区（保持现有功能：@提及、Enter 发送等）
  - 右栏：复用 `MarkdownRenderer` 实时渲染当前输入内容
  - 空输入时预览栏显示 placeholder 文案
- **数据流调整**：`handleSend`、`insertMention` 从 state 读取值而非 `textareaRef.current.value`

### 3. 用户消息气泡样式微调

**文件**：`src/components/chat/MessageBubble.tsx`

用户气泡内使用 `MarkdownRenderer` 后，需确保样式可读：
- 代码块背景在 `bg-primary-soft` 上有足够对比
- 链接颜色、引用块等元素在用户气泡内适配
- 必要时通过 `[&_a]:`、`[&_pre]:` 等 CSS 覆盖调整

## 复用组件

| 组件 | 路径 | 用途 |
|------|------|------|
| `MarkdownRenderer` | `src/components/markdown/MarkdownRenderer.tsx` | 消息渲染 + 输入预览 |
| `lucide-react` | 已有依赖 | `Eye` / `EyeOff` 预览切换图标 |

## 验证方式

1. `make run-frontend` 启动前端
2. 发送包含 Markdown 语法的用户消息（标题、列表、代码块、粗体等），确认渲染正确
3. 在输入栏粘贴一段 Markdown，点击预览按钮，确认右侧实时渲染
4. 确认预览模式下发送、@提及等功能正常
5. 确认用户气泡内 Markdown 样式可读性（代码块、链接颜色等）
6. 可结合 `frontend/docs/guides/markdown-demo.tsx` 中的测试 Markdown 内容验证渲染效果
