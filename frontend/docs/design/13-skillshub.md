# SkillsHub — 技能库页面与技能导入

## 实现了什么

技能库管理页面（`/skills` 路由，`SkillsHubPage`）：浏览内置/外部技能、搜索、管理员上传 .zip 技能包（两步式上传-校验-确认流程）、删除外部技能。配套在 `AgentProfilePage` 中提供技能导入/移除弹窗，将技能库中的外部技能导入到指定 Session worktree。技能数据走 Backend 的 `/api/skills*` 接口族（底层为 MinIO 技能存储），类型由契约生成（`src/generated/skill-storage.ts`）。

## 怎么实现的

### 页面结构 (`src/pages/SkillsHubPage.tsx`)

懒加载路由组件（`ImPage` 中 `lazy(() => import('@/pages/SkillsHubPage'))`）。容器使用 `.chat-canvas` 点阵背景避免大面积空黑，主容器 `max-w-[88rem]`，`xl` 下双栏 `xl:grid-cols-[minmax(0,1fr)_18rem]`（左侧技能网格 + 右侧粘性状态摘要栏）：

```tsx
export function SkillsHubPage() {
  const [search, setSearch] = useState('')
  const isAdmin = useAdminStore((state) => state.isAuthenticated)

  const { data: skills = [], isError, isLoading, refetch } = useQuery({
    queryKey: ['skills'],
    queryFn: fetchSkills,
  })

  const deleteMutation = useMutation({
    mutationFn: deleteSkill,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['skills'] })
      setDeleteTarget(null)
    },
  })

  const query = search.trim().toLowerCase()
  const filtered = skills.filter((s) => {
    if (!query) return true
    return [s.name, s.description].some((value) => value.toLowerCase().includes(query))
  })
  const builtins = filtered.filter((s) => s.builtin)
  const externals = filtered.filter((s) => !s.builtin)
  // ...
}
```

要点：

- **管理员门控**：Header 的"上传"按钮 `disabled={!isAdmin}`（`useAdminStore.isAuthenticated`），外部技能卡片的删除按钮同样仅管理员可见；内置技能不可删除（`onDelete={undefined}`）。
- **分区渲染**：内置技能（`Shield` 图标 + success 色 Badge）与外部技能（`Package` 图标 + primary 色 Badge）分成两区，`md:grid-cols-2` 网格；非 ready 状态的外部技能显示状态 Badge（`storage_error` → 存储异常、`deleting` → 删除中、`migrating` → 迁移中）。
- **技能卡 `HubSkillCard`**：`min-h-[8rem] rounded-[14px] border-border/70 bg-card/80`，展示 name、description、`import_count`（"已被 N 个 Agent 导入"）及可选元信息（`uploaded_by` / `sha256` / `files` / `contains_executable` / `contains_binary` 内容提示）。
- **右侧摘要栏**（`hidden xl:block`）：`StatPill` 统计内置/外部数量 + "上传前检查"提示（zip 文件名需与 SKILL.md 的 name 一致）。
- **加载/错误/空态**：加载态为 4 个骨架卡片（`skeleton-sheen`）；错误态含"重试"按钮（`refetch()`）；搜索无结果与技能库为空分别显示不同文案。

### 两步式上传 (`UploadDialog`)

上传弹窗为受控 `role="dialog"` + `useDialogFocusTrap`，分 `upload` / `validate` 两步：

```tsx
function UploadDialog({ onClose, onSuccess }: { onClose: () => void; onSuccess: () => void }) {
  const [step, setStep] = useState<'upload' | 'validate'>('upload')
  const [validation, setValidation] = useState<ValidationResponse | null>(null)
  const [confirmName, setConfirmName] = useState('')

  const handleFile = useCallback(async (file: File) => {
    if (!file.name.toLowerCase().endsWith('.zip')) { /* 前端先行拒绝非 zip */ }
    const result = await uploadSkill(file)      // POST /api/skills/upload
    setValidation(result)
    if (result.valid && result.name) {
      setConfirmName(result.name)
      setStep('validate')
    }
  }, [])

  const handleConfirm = async () => {
    const confirmPayload = validation.upload_id
      ? { upload_id: validation.upload_id }
      : { name: confirmName, description: ..., file_count: ..., total_size: ..., tmp_dir: ... }
    await confirmSkill(confirmPayload)          // POST /api/skills/confirm
    onSuccess()                                  // invalidate ['skills'] 并关闭
  }
}
```

- 第一步：点击或拖拽上传 `.zip`（限制：≤10MB、解压后 ≤50MB、≤200 个文件），`uploadSkill` 仅做暂存与结构校验（SKILL.md 存在、frontmatter name 与 zip 名一致、SHA-256、可执行/二进制内容警告），返回 `SkillUploadResponse`。
- 第二步：展示校验清单，用户确认 Skill 名称（`upload_id` 存在时只读）后调用 `confirmSkill` 落库。优先走 `upload_id`（服务端暂存），降级传完整字段（兼容旧暂存路径）。
- 校验失败时 `validation.errors` 逐条列出，不进入第二步。

### 删除确认 (`DeleteConfirmDialog`)

外部技能删除前弹确认框，文案明确"仅删除技能库中的源文件，不影响已导入到 Agent 的副本"，确认后 `deleteMutation.mutate(name)`（`DELETE /api/skills/:name`）。

### Agent 详情页的导入/移除 (`src/pages/AgentProfilePage.tsx`)

- **导入弹窗**：复用 `useQuery({ queryKey: ['skills'], queryFn: fetchSkills })` 拉取技能库，过滤出外部技能并用 `alreadyImported`（当前 Session 已有技能名的 Set）标记已导入项；多选后 `Promise.all` 并行调用 `importSkill(name, sessionId)`（`POST /api/skills/:name/import`，将技能复制进该 Session 的 worktree）。
- **移除**：Agent 技能列表的移除按钮调用 `removeSkill(s.name, sessionId)`（`DELETE /api/skills/:name/sessions/:sessionId`）。

### 契约类型 (`src/generated/skill-storage.ts`)

由 `contracts/schemas/skill-storage.yaml` 经 `make generate` 生成，勿手改：

```typescript
export interface SkillUploadResponse {
  valid: boolean
  name?: string
  description?: string
  file_count?: number
  files?: string[]
  contains_executable?: boolean
  contains_binary?: boolean
  total_size?: number
  upload_id?: string
  uploaded_by?: string
  tmp_dir?: string
  sha256?: string
  package_size?: number
  storage_type?: string
  errors?: string[]
}

export interface SkillConfirmRequest {
  upload_id?: string
  name?: string
  description?: string
  file_count?: number
  total_size?: number
  tmp_dir?: string
}

export interface SkillConfirmResponse {
  success: boolean
  name?: string
}

export interface SkillHubItem {
  name: string
  builtin: boolean
  description: string
  file_count: number
  total_size: number
  import_count: number
  created_at: string
  uploaded_by?: string
  sha256?: string
  storage_type?: string
  status?: string
  files?: string[]
  contains_executable?: boolean
  contains_binary?: boolean
}
```

### API 汇总 (`src/lib/api.ts`)

| 函数 | 方法 | 路径 | 说明 |
|------|------|------|------|
| `fetchSkills` | GET | `/api/skills` | 技能库列表（`SkillHubItem[]`） |
| `uploadSkill` | POST | `/api/skills/upload` | 上传 .zip 暂存并校验（admin token） |
| `confirmSkill` | POST | `/api/skills/confirm` | 确认入库（优先 `upload_id`） |
| `deleteSkill` | DELETE | `/api/skills/:name` | 删除技能库源文件（admin token） |
| `importSkill` | POST | `/api/skills/:name/import` | 导入技能到指定 Session worktree |
| `removeSkill` | DELETE | `/api/skills/:name/sessions/:sessionId` | 从指定 Session 移除技能 |
