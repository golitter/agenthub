/**
 * Centralized Chinese UI text constants.
 *
 * Use these instead of hard-coded strings in components.
 * Organized by semantic group. All objects use `as const`
 * for literal type safety.
 */

// ─── Actions (buttons, links) ────────────────────────────────────────
export const UI_ACTIONS = {
  BACK: '返回',
  SAVE: '保存',
  CANCEL: '取消',
  CLEAR: '清除',
  CONFIRM: '确认',
  DELETE: '删除',
  EXPORT: '导出',
  REFRESH: '刷新',
  RETRY: '重试',
  EDIT: '编辑',
  ADD: '添加',
  PUBLISH: '发布',
  PIN: '置顶',
  UNPIN: '取消置顶',
  CLOSE: '关闭',
  VALIDATE: '校验',
  IMPORT: '确认导入',
  UPLOAD: '上传 Skill',
  EXPAND: '展开',
  COLLAPSE: '收起',
  APPROVE: '批准执行',
  DISCUSS: '继续讨论',
  REQUEST_MODIFY: '请求修改',
  ACCEPT_CHANGE: '接受变更',
  REJECT_CHANGE: '拒绝变更',
  COMMIT: '提交',
  CLEAN_UP: '批量清理',
  VIEW_DETAIL: '查看 Agent 详情',
  EDIT_PROFILE: '编辑资料',
  LOGOUT: '退出登录',
  INIT_GIT: '初始化 Git',
} as const

// ─── Status / loading states ─────────────────────────────────────────
export const UI_STATUS = {
  LOADING: '加载中...',
  SAVING: '保存中...',
  SENDING: '发送中...',
  UPLOADING: '上传中...',
  STREAMING: '正在回复...',
  COMMITTING: '提交中...',
  REVERTING: '撤销中...',
  VALIDATING: '校验中...',
  CREATING: '创建中...',
  DELETING: '清理中...',
  VERIFYING: '验证中...',
  INITIALIZING_GIT: '正在初始化 Git...',
  FAILED: '失败',
  SUCCESS: '成功',
} as const

// ─── Messages / toasts ───────────────────────────────────────────────
export const UI_MESSAGES = {
  COPY_SUCCESS: '✓ 已复制到剪贴板',
  SEND_FAILED: '发送失败',
  LOAD_HISTORY_FAILED: '加载历史消息失败',
  PASSWORD_ERROR: '密码错误',
  DEV_COMING_SOON: '功能开发中，敬请期待',
  PLEASE_AUTH: '请先验证身份',
  GIT_INIT_PROMPT: '该目录不是 Git 仓库。输入目录名以确认初始化',
  GIT_INIT_SUCCESS: 'Git 仓库初始化成功',
  GIT_INIT_MISMATCH: '输入的名称不匹配',
  NO_SKILLS: '暂无技能',
  NO_WORKSPACES: '暂无工作区',
  NO_DATA: '暂无数据',
  NO_ANNOUNCEMENTS: '暂无公告',
  NO_CONVERSATIONS: '暂无会话',
  NO_MATCHING_MESSAGES: '没有找到匹配的消息',
  SELECT_CHAT_TO_START: '选择一个对话开始聊天',
  SEND_MESSAGE_TO_START: '发送消息开始对话',
  DOUBLE_CLICK_TO_COPY: '双击复制',
  RENDER_ERROR: '组件渲染异常',
  CLICK_TO_VIEW_FULL_QUERY: '点击查看完整 query',
  CLICK_TO_WRITE_SOUL: '点击编写 SOUL.md — 描述 Agent 身份和性格',
  IMPORT_EXTERNAL_SKILL_DESC: '从技能库中选择要导入到此 Agent 的外部 Skill。',
  UPLOAD_SKILL_DESC: '上传一个 .zip 压缩包，zip 文件名须与 SKILL.md 中的 name 一致。',
  NO_EXTERNAL_SKILLS: '技能库中暂无外部 Skill',
  IMPORTED: '已导入',
  MOVE_TO_GROUP: '移至分组',
} as const

// ─── Labels / headings ───────────────────────────────────────────────
export const UI_LABELS = {
  AGENT_INFO: 'Agent 信息',
  PATH_INFO: '路径信息',
  REPO_PATH: '仓库路径',
  TASK_PATH: '任务路径',
  GIT_GRAPH: 'Git Graph',
  EXPORT_CHAT: '导出聊天记录',
  PIN_CHAT: '置顶会话',
  UNPIN_CHAT: '取消置顶',
  EXIT_GROUP: '退出群聊',
  DELETE_CHAT: '删除会话',
  EXPAND_SIDEBAR: '展开侧栏',
  MESSAGE_DETAIL: '消息详情',
  MESSAGE_DETAIL_DESC: '查看消息的完整内容',
  ZOOM_IN: '放大',
  CLICK_TO_ZOOM: '点击放大查看完整消息',
  EDIT_AGENT: '编辑 Agent',
  EDIT_AGENT_DESC: '编辑 Agent 名称和头像',
  UPLOAD_AVATAR: '上传头像',
  NAME: '名称',
  CREATED_AT: '创建时间',
  MESSAGE_COUNT: '消息数',
  GROUP_MEMBERS: '群成员',
  ANNOUNCEMENTS: '群公告',
  NEW_ANNOUNCEMENT: '+ 发布新公告',
  DELETE_ANNOUNCEMENT: '删除公告',
  COORD_CHANNEL: '协调通道',
  COORD_CONCLUSION: '协调结论',
  CONTACTS: '通讯录',
  SKILLS_HUB: '技能库',
  NEW_CHAT: '新建对话',
  GROUP_CHAT: '群聊',
  ADMIN_VERIFY: '管理员验证',
  SENSITIVE_CONFIRM: '敏感操作确认',
  ENTER_PASSWORD: '请输入密码',
  WORKSPACE_MANAGE: '工作区管理',
  AGENT_OVERVIEW: 'Agent 概览',
  SERVICE_HEALTH: '服务健康',
  SESSION_CLEANUP: '会话清理',
  STATISTICS: '数据统计',
  DASHBOARD: '总览仪表盘',
  USER_MANAGEMENT: '用户管理',
  METADATA: '元数据',
  CHAT: '聊天',
  SETTINGS: '设置',
  ADMIN: '管理',
  INNER_SKILLS: '内置 Skills',
  EXTERNAL_SKILLS: '外部 Skills',
  METRIC_TOTAL: '总数',
  METRIC_ACTIVE: '活跃',
  METRIC_CLEANED: '已清理',
  METRIC_DISK: '磁盘占用',
  NO_DIFF: '没有检测到 task 分支相对 main 的代码差异。',
} as const

// ─── Placeholders ────────────────────────────────────────────────────
export const UI_PLACEHOLDERS = {
  MESSAGE_INPUT: '输入消息...',
  SEARCH_HISTORY: '搜索历史消息...',
  SEARCH_CONVERSATION: '搜索对话...',
  SEARCH_CONTACTS: '搜索会话...',
  SEARCH_SKILLS: '搜索技能...',
  ANNOUNCEMENT_INPUT: '输入公告内容...',
  GROUP_NAME: '为群聊起个名字',
  GROUP_NAME_INPUT: '输入分组名称...',
  AGENT_NAME_INPUT: '输入 Agent 名称',
  SOUL_DESCRIPTION: '描述这个 Agent 的身份和性格（不超过 300 字，不含空格）',
  FEEDBACK_PLACEHOLDER: '有修改意见或想继续讨论，可以写在这里...',
  PASSWORD: '请输入密码',
  MESSAGE_TO: '发消息给',
} as const

// ─── Confirmations ───────────────────────────────────────────────────
export const UI_CONFIRMS = {
  EXIT_GROUP: '确认退出群聊？退出后将彻底删除所有消息和工作区数据，且不可恢复。',
  DELETE_CHAT: '确认删除会话？删除后将清除所有聊天记录，且不可恢复。',
  DELETE_ANNOUNCEMENT: '确认删除此公告？',
  CLEAN_WORKSPACE: '确认清理该工作区？',
  CLEAN_SESSIONS: '确认清理',
  DELETE_GROUP: '确认删除分组',
  DELETE_GROUP_SUFFIX: '？成员将移至未分组。',
} as const

// ─── Errors / validations ────────────────────────────────────────────
export const UI_ERRORS = {
  GROUP_NAME_REQUIRED: '群聊必须填写名称',
  ORCHESTRATOR_ONLY: 'Orchestrator 不能单独成群，请添加至少一个非 Orchestrator 的 Agent',
  ONE_ORCHESTRATOR: '只能添加一个 Orchestrator',
  DUPLICATE_NAME: '请输入不重复的 Agent 名称',
  REPO_PATH_REQUIRED: '请输入仓库路径',
  VALIDATE_FAILED: '校验失败，请检查 Agent 服务是否可用',
  GIT_INIT_FAILED: 'Git 初始化失败，请检查目录权限或手动执行 git init',
  FEEDBACK_REQUIRED: '请先写下你的反馈。',
  SUBMIT_REVIEW_FAILED: '提交审查失败',
  SOUL_TOO_LONG: '字（不含空格），当前',
  ADD_AGENT: '请添加 Agent',
} as const

// ─── Status labels for cards ─────────────────────────────────────────
export const UI_CARD_STATUS = {
  ANSWERED: '已回答',
  UNANSWERED: '未回答',
  PENDING_ANSWER: '等待回答',
  TASK_TIMEOUT: '任务超时',
  TASK_FAILED: '任务失败',
  COMPLETED: '已完成',
  PARTIAL: '部分完成',
  EXECUTION_FAILED: '执行失败',
  TASK: '任务',
  RUNNING: '执行中',
  DONE: '完成',
  WAITING: '等待',
  IMAGE_LOAD_FAILED: '图片加载失败',
  OPEN_IN_NEW_TAB: '在新标签页打开',
} as const

// ─── Agent Profile page ──────────────────────────────────────────────
export const UI_PROFILE = {
  BACK_TO_CHAT: '返回对话',
  IMPORT_SKILL: '导入外部 Skill',
  SKILL_COUNT: '个技能',
  REMOVE_SKILL: '移除 Skill',
  SKILL_IMPORTED_BY: '已被',
  SKILL_IMPORTED_BY_SUFFIX: '个 Agent 导入',
  UPLOAD_SKILL_FORMAT: '支持 .zip 格式，解压后不超过 10MB，文件数不超过 100',
  UPLOAD_OR_DRAG: '点击或拖拽上传 .zip 文件',
  VALIDATE_FAILED: '校验失败',
  VALIDATE_PASSED: '校验通过',
  SKILL_NAME_LABEL: 'Skill 名称（确认后不可修改）',
  NO_CONFIG: '无配置内容',
  VIEW_CONFIG: '查看配置',
  COLLAPSE_CONFIG: '收起配置',
} as const

// ─── Time formatting ─────────────────────────────────────────────────
export const UI_TIME = {
  JUST_NOW: '刚刚',
  MINUTES_AGO: '分钟前',
  HOURS_AGO: '小时前',
  DAYS_AGO: '天前',
} as const

// ─── Misc formatting ─────────────────────────────────────────────────
export const UI_MISC = {
  ME: '我',
  ONLINE: '在线',
  MORE: '更多',
  CONFIRM_IMPORT: '确认入库',
  SUBMIT: '提交',
  OK: '确定',
  USER: '用户',
  UNGROUPED: '未分组',
  NEW_GROUP: '新建分组',
  MOVE_OUT_GROUP: '移出分组',
  GIT_REF_NOT_EXIST: '（git ref 不存在）',
  COMMITS: 'commits',
  BRANCHES: 'branches',
  WAITING_REPLY: '正在等待',
  REPLYING: '回复中…',
  SUFFIX_ROUND: '轮协调',
  SUFFIX_MESSAGES: '条消息',
  ROUND: '第',
  ROUND_SUFFIX: '轮协调',
  COORD_RESULT: '{rounds} 轮协调，{messages} 条消息',
} as const
