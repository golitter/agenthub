# Markdown Rendering — 聊天对话 Markdown 渲染 + 输入栏双栏实时预览

## 实现了什么

用户消息支持 Markdown 渲染（通过 `MarkdownRenderer`），输入栏增加双栏实时预览模式（左编辑 / 右预览），用户可实时看到 Markdown 渲染效果。

## 怎么实现的

### 1. 用户消息 Markdown 渲染

**文件**：`src/components/chat/MessageRenderer.tsx`

用户消息已从纯文本改为 `MarkdownRenderer` 渲染：

```tsx
if (msg.role === MESSAGE_ROLES.USER) {
  return (
    <MessageBubble variant="user">
      <MarkdownRenderer content={msg.content} />
    </MessageBubble>
  )
}
```

### 2. 输入栏双栏实时预览

**文件**：`src/components/chat/MessageInput.tsx`

已实现的双栏预览功能：

- **受控 textarea**：`useState` 管理 `inputValue`，驱动预览渲染
- **预览开关按钮**：工具栏中的 "Markdown" 按钮（`FileText` 图标）切换双栏模式
- **双栏布局**：开启时输入区域变为左右双栏：
  - 左栏：textarea 编辑区（保持 @提及功能）
  - 右栏：`MarkdownRenderer` 实时渲染当前输入内容（150ms 防抖）
  - 空输入时预览栏显示 placeholder 文案
- **滚动同步**：左侧编辑区滚动时按比例同步右侧预览区
- **自动高度**：双栏模式高度根据内容自动调整（最小 120px，最大 60vh）
- **MD 模式下 Enter 插入换行**，不触发发送；单击模式仍为 Enter 发送

### 3. 用户消息气泡样式微调

用户气泡内使用 `MarkdownRenderer` 后，样式已适配：
- `MarkdownRenderer` 外层 `prose dark:prose-invert` 类提供基础排版
- 通过 CSS 变量（`--prose-*`）统一暗色/亮色主题下的配色

## 复用组件

| 组件 | 路径 | 用途 |
|------|------|------|
| `MarkdownRenderer` | `src/components/markdown/MarkdownRenderer.tsx` | 消息渲染 + 输入预览 |
| `lucide-react` | 已有依赖 | `FileText` 预览切换图标 + `Send` 发送按钮 |

## 验证方式

1. `make run-frontend` 启动前端
2. 发送包含 Markdown 语法的用户消息（标题、列表、代码块、粗体等），确认渲染正确
3. 在输入栏点击 "Markdown" 按钮，粘贴一段 Markdown，确认右侧实时渲染
4. 确认预览模式下发送、@提及等功能正常
5. 确认用户气泡内 Markdown 样式可读性（代码块、链接颜色等）
6. 可结合 `frontend/docs/guides/markdown-demo.tsx` 中的测试 Markdown 内容验证渲染效果
