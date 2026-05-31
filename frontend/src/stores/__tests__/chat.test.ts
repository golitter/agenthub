import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useChatStore } from '../chat'

const sessionId = 'session-live-ask-card'

describe('chat store ask-agent cards', () => {
  beforeEach(() => {
    vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => {
      callback(0)
      return 1
    })
    vi.stubGlobal('cancelAnimationFrame', () => undefined)
    useChatStore.getState().resetSession(sessionId)
  })

  it('updates an ask-agent card after agent switching has moved it into messages', () => {
    const store = useChatStore.getState()

    store.streamStart(sessionId, 'orchestrator')
    store.streamAskCardStart(sessionId, {
      question_id: 'q-1',
      source_agent: '管理者',
      source_agent_type: 'orchestrator',
      source_session_id: sessionId,
      target_agent: '执行者',
      target_agent_type: 'codex',
      target_session_id: 'session-worker',
      question: '请检查权限系统',
    })

    store.streamAgentUpdate(sessionId, 'codex', '执行者', 'worker-message-1')
    store.streamAskCardDone(sessionId, {
      question_id: 'q-1',
      target_agent: '执行者',
      target_agent_type: 'codex',
      target_session_id: 'session-worker',
      summary: '检查完成',
      status: 'completed',
    })

    const state = useChatStore.getState().getSession(sessionId)
    expect(state.runtimeBlocks).toHaveLength(0)
    expect(state.messages).toHaveLength(1)
    const block = state.messages[0].blocks?.[0]
    expect(block?.type).toBe('ask_agent')
    if (block?.type === 'ask_agent') {
      expect(block.status).toBe('answered')
      expect(block.collapsed).toBe(true)
      expect(block.summary).toBe('检查完成')
    }
  })

  it('does not duplicate ask-agent cards when text also contains persisted card markers', () => {
    const store = useChatStore.getState()

    store.streamStart(sessionId, 'orchestrator')
    store.streamAskCardStart(sessionId, {
      question_id: 'q-dup',
      source_agent: '管理者',
      source_agent_type: 'orchestrator',
      source_session_id: sessionId,
      target_agent: '执行者',
      target_agent_type: 'codex',
      target_session_id: 'session-worker',
      question: '请检查 god.html',
    })
    store.streamText(
      sessionId,
      '\ntype: ask_agent\n' +
        'json: {"question_id":"q-dup","target_agent":"执行者","target_session_id":"session-worker","question":"请检查 god.html","summary":"已检查","status":"answered","collapsed":true}\n' +
        '正文回答',
    )
    store.streamDone(sessionId)

    const state = useChatStore.getState().getSession(sessionId)
    const askCards = state.messages[0].blocks?.filter((block) => block.type === 'ask_agent') ?? []
    expect(askCards).toHaveLength(1)
  })

  it('does not freeze runtime text into the orchestrator card when agent text arrives', () => {
    const store = useChatStore.getState()

    store.streamStart(sessionId, 'orchestrator')
    store.streamRuntimeEvent(sessionId, {
      task_id: 'task-001',
      agent: '执行者',
      title: '改造 god.html',
      status: 'running',
    })
    store.streamRuntimeText(sessionId, {
      task_id: 'task-001',
      agent: '执行者',
      text: '我正在修改文件。',
    })
    store.streamAgentUpdate(sessionId, 'codex', '执行者', 'worker-message-2')
    store.streamText(sessionId, '我正在修改文件。')

    const state = useChatStore.getState().getSession(sessionId)
    expect(state.messages).toHaveLength(1)
    const block = state.messages[0].blocks?.find((item) => item.type === 'runtime_status')
    expect(block?.type).toBe('runtime_status')
    if (block?.type === 'runtime_status') {
      expect(block.streamingText).toBeUndefined()
    }
    expect(state.streamingContent).toBe('我正在修改文件。')
  })

  it('clears live runtime transcript when normal text arrives for the same agent', () => {
    const store = useChatStore.getState()

    store.streamStart(sessionId, 'codex')
    store.streamAgentUpdate(sessionId, 'codex', '执行者', 'worker-message-3')
    store.streamRuntimeEvent(sessionId, {
      task_id: 'task-002',
      agent: '执行者',
      title: '强化 god.html',
      status: 'running',
    })
    store.streamRuntimeText(sessionId, {
      task_id: 'task-002',
      agent: '执行者',
      text: '临时运行日志',
    })
    store.streamText(sessionId, '正式执行结果')

    const state = useChatStore.getState().getSession(sessionId)
    const block = state.runtimeBlocks.find((item) => item.type === 'runtime_status')
    expect(block?.type).toBe('runtime_status')
    if (block?.type === 'runtime_status') {
      expect(block.streamingText).toBeUndefined()
    }
    expect(state.streamingContent).toBe('正式执行结果')
  })
})
