# 前端开发策略

> 基于 React 19 + Vite + TypeScript + Tailwind CSS 4 + shadcn/ui + Zustand 5 + TanStack Query 5 + React Router 7 技术栈，结合工程经验提炼的开发指导。

---

## 一、核心原则

1. **抽象是负债，不是奖励** — 第三次重复再抽象，不提前建 BaseXXX
2. **状态离使用它最近** — 能 local 就不 global，能 computed 就不 stored
3. **组件按职责和变化频率拆** — 不按 DOM 结构、不按代码行数拆
4. **先让它 work，再让它 beautiful** — 不过度架构，让架构顺着业务长出来

---

## 二、技术栈速查

| 层面 | 选型 | 版本 | 说明 |
|------|------|------|------|
| 构建 | Vite | ^8.0 | 开发服务器 + 生产构建，热重载极快 |
| 语言 | TypeScript | ~6.0 | bundler 模式，严格类型检查 |
| 包管理 | pnpm | workspace | 严格依赖隔离，磁盘占用小 |
| 框架 | React | ^19.2 | StrictMode，并发特性全量可用 |
| 路由 | React Router | ^7.15 | BrowserRouter 客户端路由 |
| 样式 | Tailwind CSS | ^4.3 | 原子化 CSS，oklch 色彩空间，CSS 变量双主题 |
| 组件库 | shadcn/ui (radix-nova) | ^4.8 | 非运行时依赖，代码拷入项目可自由修改 |
| 无障碍 | Radix UI | ^1.4 | shadcn/ui 底层原语，Dialog/Slot 等 |
| 全局状态 | Zustand | ^5.0 | 轻量，只有真正跨模块的数据才放这里 |
| 服务端状态 | TanStack React Query | ^5.100 | 缓存、失效、乐观更新、SSE 集成 |
| 图标 | Lucide React | ^1.16 | 线条风格图标库 |
| 字体 | Geist Variable | ^5.2 | UI 用 Geist Sans，代码用 Geist Mono |
| 代码规范 | ESLint | flat config | typescript-eslint + react-hooks + react-refresh |
| 视觉规范 | — | — | 详见 [visual-style-guide.md](visual-style-guide.md) |

---

## 三、组件拆分策略

### 3.1 拆分时机

| 信号 | 动作 |
|------|------|
| 同一段 UI/逻辑出现第 3 次 | 抽成公共组件或自定义 Hook |
| 一个组件承担多个职责 | 按单一职责拆分（列表、表单、分页各自独立） |
| 某区域有独立状态和高频变化 | 必须隔离（如 MessageList 的 streaming + 虚拟列表） |
| 文件超过 200~300 行且还在增长 | 按视觉区块或功能块切分 |
| 需要异步加载的重组件 | 拆出后用 React.lazy + Suspense |
| 多人并行开发同一页面 | 按区域拆分，减少 Git 冲突 |

### 3.2 三层组件模型

- **Page（页面层）**：负责路由、布局编排、数据组合。对应 React Router 的页面组件，如 ChatPage、TaskDetailPage。通过 useQuery/useLoaderData 获取数据，向下传递。
- **Smart（容器层）**：负责状态管理、数据请求、业务编排。如 ChatArea 管理 streaming 状态，SessionManager 管理会话切换。内部使用 useReducer 或 TanStack Query。
- **Dumb（展示层）**：只接收 props 渲染 UI，无内部请求或全局状态。如 MessageBubble、AgentAvatar、CodeBlock。优先从 shadcn/ui 扩展，保持 Radix 无障碍特性。

### 3.3 本项目组件规划（Phase 2 Chat UI）

ChatPage 作为页面层编排三栏布局。ChatSidebar 独立管理会话列表和搜索。ChatArea 作为 Smart 组件管理 streaming 状态，内部的 MessageList 负责虚拟列表和 streaming diff，MessageInput 处理防抖和快捷键。DetailPanel 独立展示 Diff/Logs/Preview。

其中 MessageList 是必须单独隔离的典型——它会处理滚动、虚拟列表、streaming token diff、loading、retry，这些高频逻辑不能污染整个页面。

---

## 四、样式开发规范

### 4.1 Tailwind CSS 4 使用原则

**优先用工具类，不提取自定义 CSS 类。** 只在以下情况提取：同一组工具类重复 5 次以上，或需要响应式/状态变体组合过长的场景。提取时用 Tailwind 的 @apply 或 cva（class-variance-authority），不写独立 .css 文件。

**用 CSS 变量实现主题。** 项目通过 oklch 色彩空间在 index.css 的 :root / .dark 中定义双主题变量。切换主题只改 CSS 变量值，Tailwind 直接引用 var(--color-xxx)。具体色值参见 [visual-style-guide.md](visual-style-guide.md)。

**cn() 合并类名。** 组件内需要条件拼接 className 时，统一用 lib/utils.ts 中的 cn()（clsx + tailwind-merge），处理冲突和条件类名。

### 4.2 shadcn/ui 使用规范

**优先从 shadcn/ui 扩展，不自己造轮子。** 已安装 Button、Card、Input、Dialog。需要新组件时用 pnpm dlx shadcn@latest add <component> 安装，代码拷入 components/ui/ 可自由修改。

**保持 Radix 无障碍特性。** shadcn/ui 底层用 Radix UI 原语，自带键盘导航、ARIA 属性、焦点管理。修改 shadcn 组件时不要破坏这些无障碍特性。

**不建 BaseXXX 层。** 不要在 shadcn/ui 之上再包一层 BaseButton、BaseCard。如果需要变体，用 cva 定义变体，直接扩展 shadcn 组件。

### 4.3 图标与字体

图标统一用 Lucide React，不混入其他图标库。字体用 Geist Sans（UI 文字）和 Geist Mono（代码块），已通过 @fontsource-variable/geist 引入。

---

## 五、状态管理策略

### 5.1 三类状态的归属

- **Server State**（来自后端、需要缓存、可能 stale）：tasks、sessions、messages 等。用 **TanStack React Query 5** 管理，利用其缓存、自动刷新、窗口聚焦重验证、乐观更新能力。不放进 Zustand。
- **Global Client State**（跨页面/跨模块共享、与后端无关）：用户信息、主题、auth、WebSocket 连接状态等。用 **Zustand 5** 管理。Store 位于 src/stores/，每个独立领域一个 store 文件，不合并成一个大 store。
- **Local Client State**（只在某个组件或小范围使用）：表单输入值、弹窗开关、折叠状态等。用组件内 **useState / useReducer**。

### 5.2 关键规则

**派生状态坚决不存。** 能通过现有数据计算得出的值，绝对不要用 useState 存。多存一个状态，就要多维护一次同步，早晚会出 bug。用 useMemo 直接计算即可。

**能局部就别全局。** 弹窗开关、表单临时值这类状态，放在使用它的组件内部。只有真正跨页面、跨模块的数据才放进 Zustand。如果只有两个兄弟组件共享状态，提升到它们的最近父组件就行。

**URL 是最廉价的全局状态。** 搜索词、分页页码、Tab 选项、弹窗开关——这些刷新后仍需保留的状态，优先放 URL query 参数。React Router 7 的 useSearchParams 可以直接读写，免费获得前进/后退 + 可分享链接。

**临时状态用 ref。** 定时器 ID、防抖函数实例、上一次渲染的值——这些变化不需要触发 re-render 的数据，用 useRef，不用 useState。

**不要把 Server State 塞进 Zustand。** messages、tasks 这类来自后端的数据属于 Server State，应该用 TanStack Query 管理（缓存、刷新、失效），不是 Zustand 的职责。

### 5.3 TanStack Query 5 使用要点

API 调用集中在 src/lib/api.ts，组件通过 useQuery / useMutation 调用。利用 queryKey 管理缓存粒度，如 ['tasks'] 列表级、['task', id] 单条级。mutation 成功后通过 queryClient.invalidateQueries 刷新相关缓存。SSE streaming 场景用 useQuery 的初始加载 + useReducer 接收增量更新。

### 5.4 Zustand 5 使用要点

Store 按领域拆分，不合并成一个大 store。用 create 创建，组件通过 selector 订阅具体字段，避免不必要的 re-render。当前只有 demo counter，后续按需增加 user store、ui store 等。

### 5.5 状态机建模 Chat 流转

AI Chat 天然是状态机，状态在 idle → loading → streaming → tool_running → done 之间流转，中间可能 error → retrying。

用 TypeScript 的 discriminated unions + useReducer 建模：每个状态用 status 字段区分，streaming 状态携带 abortController，error 状态携带 Error 对象。TypeScript 会保证进入某个状态时对应的字段一定存在，不需要到处判空。

---

## 六、路由组织

使用 React Router 7 的 BrowserRouter 客户端路由。路由定义集中在 main.tsx 或独立的 router 配置文件。

当前路由结构：`/` 首页、`/tasks` 任务列表、`/tasks/:taskId` 任务详情。

Phase 2 新增 Chat 相关路由：`/chat` 或复用 `/tasks/:taskId` 进入 Chat 模式。路由参数（taskId、sessionId）作为数据获取的入口，对应组件内 useQuery 的 queryKey。

---

## 七、Hook 使用规范

### 7.1 什么时候抽 Hook

Hook 是**状态逻辑复用**，不是"把代码挪出去"。

抽 Hook 的条件：涉及状态 + side effect + 生命周期。如 useChatStream（SSE 连接）、useDebounce（定时器）、useInfiniteScroll（DOM + IntersectionObserver）。

纯计算函数不要写成 Hook。formatTime、formatFileSize、generateId 这类纯转换，写在 src/lib/ 下作为普通工具函数即可。Hook 有 React 生命周期语义，纯函数没有。

### 7.2 Hook 体积红线

Hook 超过 150~200 行开始危险。如果一个 Hook 里塞了请求、toast、loading、modal、analytics、routing、cache——没人敢改。拆成多个小 Hook，各自独立。

---

## 八、性能策略

### 8.1 真正的瓶颈

性能问题通常不是 React render 慢，而是以下五种：

1. **请求瀑布**：page → fetch A → fetch B → fetch C 串行，直接炸。多个独立请求应并行，TanStack Query 天然支持并行 useQuery。
2. **巨型状态更新**：streaming 时每收到一个 token 都重建整个消息数组。应该只追加最后一条消息的内容。
3. **Context 导致全树 rerender**：Chat state 不能用 Context，每个 token 更新都会重渲染整个树。用 Zustand selector 或 useReducer 精准订阅。
4. **useEffect 无限循环**：setState 触发自己作为依赖的 useEffect。检查依赖数组，避免 state → effect → setState 的循环。
5. **大列表不虚拟化**：超过 100 条就要考虑虚拟列表，只渲染视口内的几十个 DOM 节点。

### 8.2 本项目的性能要点

| 场景 | 策略 |
|------|------|
| 消息列表 >100 条 | 虚拟列表（react-virtuoso 或 @tanstack/virtual） |
| Streaming 消息更新 | 只更新最后一条消息的 DOM，不重建整个列表 |
| 搜索输入 | 防抖 300-500ms，对请求函数防抖而非对 setState 防抖 |
| 条件渲染的重组件 | React.lazy + Suspense 按需加载（如 Diff 面板） |
| 页面加载 | 骨架屏替代 Loading spinner，用户心理等待时间更短 |
| 列表 key | 永远用稳定唯一 ID，不用 index（避免 React diff 复用错节点） |

### 8.3 memo 使用原则

不要到处加 React.memo。只在子组件纯（仅依赖 props）且父组件频繁渲染时使用，同时配合 useCallback/useMemo 稳定传递给子组件的回调和对象引用，否则 memo 无效。

---

## 九、代码健壮性

**解构后端数据给默认值。** 后端挂了返回 null 时，前端不白屏。给解构赋默认空数组/空对象，至少能展示空状态。

**消灭魔法字符串。** 不要直接写 if (role === 'admin')，后端改了字段名全项目找不回来。抽成常量集中管理，改一处全局生效。

**错误边界兜底。** 给可能出错的模块（第三方组件、动态数据区块）外层包裹 Error Boundary，显示降级 UI，防止单点故障导致整个页面白屏。

**条件渲染用提前返回，不用三元嵌套。** 三元的嵌套地狱可读性极差，用 guard clause 逐个判断提前返回，逻辑一目了然。

**利用 TypeScript 严格模式。** 项目启用 TypeScript 严格检查，用 discriminated unions 代替到处可选链，让类型系统在编译期捕获错误。

**利用契约生成类型。** contracts/schemas/ 为单一来源，通过 make generate 生成 src/generated/ 下的 TypeScript 类型。修改协议先改 schema，再生成，保证三端类型一致。

---

## 十、AI 时代的前端模式

### 10.1 核心转变

传统前端是 API → Render，一次请求一次渲染。

AI 时代是 Event Stream → Runtime State → UI Projection：SSE/WebSocket 推送事件流，状态机/reducer 维护运行时状态，React 负责最终渲染。

本项目（AgentHub）本质是 AI Runtime Workspace，不是传统网页。前端越来越像游戏引擎 / runtime / event system。

### 10.2 Streaming 数据流

后端通过 SSE 推送事件流，前端用 useReducer 统一接收。初始消息用 TanStack Query 的 useQuery 加载，streaming 部分通过 SSE 事件 dispatch 到同一个 reducer 中，最终由 React 渲染到 MessageList。

关键点：不要把 streaming 状态和初始消息状态割裂成两套系统，应该统一到一个状态机里。TanStack Query 负责初始加载和历史消息，reducer 负责实时流式更新。

### 10.3 乐观更新（Optimistic UI）

TanStack Query 的 useMutation 自带 onMutate + onError 回调，天然支持乐观更新。用户发送消息时，在 onMutate 中先向缓存插入乐观消息，接口成功后 invalidate 刷新为真实数据，失败时在 onError 中回滚。这让用户觉得系统"快如闪电"。

---

## 十一、危险信号清单

代码出现以下信号时，说明需要重构：

| 信号 | 含义 |
|------|------|
| if/else 嵌套 3 层以上 | 状态模型错了，重新设计状态结构 |
| useEffect 超过 5 个 | 数据流失控，检查数据流方向 |
| props drilling 超过 3 层 | 状态边界错了，就近放置或提升 |
| 大量 any 类型 | 类型系统失效，补充类型定义 |
| 出现 shared/common/base 目录 | 抽象开始腐烂，重新审视是否真正复用 |

---