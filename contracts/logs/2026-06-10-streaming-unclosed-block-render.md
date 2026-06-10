# 流式未闭合 aka_yhy 围栏卡片渲染修复

## 变更原因

前端 `block-reducer.ts` 的 `findAkaBlocks` 在流式输出中，当找到 `` ```aka_yhy `` 围栏开头但闭合 `` ``` `` 尚未随流到达时，将整个块 `continue` 跳过，导致围栏标记、`type: html-render` 及已到达的 HTML 内容全部被当作纯文本渲染。大 HTML 卡片（如 agent 输出的富媒体名片）闭合晚到时尤为明显——用户看到原始 `` ```aka_yhy `` 标记而非渲染后的卡片。

## 变更文件

- `frontend/src/lib/block-reducer.ts`：核心解析修复（非 schema 变更）
- `frontend/src/lib/block-types.ts`：html-render 增加可选 `streaming` 前端渲染字段
- `frontend/src/lib/ui-text.ts`：新增 `UI_CARD_STATUS.HTML_RENDERING` 文案常量
- `frontend/src/components/cards/HtmlCard.tsx`：streaming 占位 UI
- `frontend/src/components/chat/BlockRenderer.tsx`：透传 streaming
- `frontend/src/stores/message-store.ts`：`buildAgentMessage` 终态化时清除 streaming 标记
- `frontend/src/lib/__tests__/block-reducer.test.ts`：新增 4 个流式未闭合测试用例

## 对比结果

| 项目 | 修复前 | 修复后 |
|------|--------|--------|
| 流式中 aka_yhy 围栏未闭合 | 整块跳过，`` ```aka_yhy / type: xxx `` 当纯文本 | open 块解析已到内容，html-render 显示生成中占位 |
| 流式中 html-render 卡片 | 不渲染（显示原始围栏标记） | 显示「正在生成 HTML 卡片…」占位，闭合后渲染 iframe |
| 闭合后 html-render 卡片 | 正常渲染（不受影响） | 正常渲染，`streaming` 字段为 `undefined` |
| `block-types.ts` | `html-render` 无 `streaming` 字段 | 新增可选 `streaming?: boolean`（前端派生状态） |
| 终态化消息中的 html-render | `streaming` 可能残留为 `true`（SSE done 早于闭合 ``` 到达） | `buildAgentMessage` 清除 `streaming` 标记 |

## 跨端影响

- **Frontend**：`block-reducer` 解析逻辑 + `HtmlCard` 渲染行为变更，接口签名不变。
- **Backend**：无需改动。`streaming` 字段不由后端产生，是前端从围栏闭合状态派生的渲染提示。
- **AgentEnd**：无需改动。Agent 端输出格式（`` ```aka_yhy `` 围栏约定）不变。

## 契约变更

无。`block-types.ts` 中的 `MessageBlock` 联合类型是**前端手写内部类型**，位于 `frontend/src/lib/`（非 `frontend/src/generated/`），不在 `contracts/schemas/` → `make generate` 的契约映射表中。新增的 `streaming` 字段是前端渲染层派生状态，不涉及跨端协议变更。

## 测试覆盖

新增 4 个测试用例覆盖此前未测试的流式未闭合路径（原有 24 个闭合用例保持通过，总计 28/28）：

- 未闭合 html-render → streaming 卡片
- 仅围栏开头（type 行没到）→ 不泄露围栏标记
- type 行到但字段没到（image/path）→ 吞掉围栏
- 闭合 html-render → streaming 不设置（回归）
