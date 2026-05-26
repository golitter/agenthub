# 2026-05-26: Diff 查看器升级（split view + 文件信息 + 未跟踪文件）

## 变更类型: MODIFY

## 影响范围
- Frontend: DiffFileView、DiffFileTabs、DiffCard 组件升级
- AgentEnd: workspace diff API 新增未跟踪文件支持
- Backend: 无变更

## 说明
- DiffFileView 新增 `viewType` prop，支持 split（默认）/unified 切换
- DiffCard Header 新增视图模式切换按钮
- DiffFileTabs 显示完整相对路径 + 变更类型标签（A/D/M/R/C）+ 增删统计
- DiffCard 新增文件信息栏（Tab 下方、diff 上方）
- AgentEnd `GET /v1/workspace/{id}/diff` 现在包含未跟踪的新文件（`git ls-files --others`）

## 契约变更
- 无 schema 文件修改（API 路径和响应格式不变，diff 内容更丰富但仍是 unified diff 文本）
