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

  it('starts a new orchestrator message when an ask card arrives after sub-agent text', () => {
    const store = useChatStore.getState()

    store.streamStart(sessionId, 'orchestrator')
    store.streamAskCardStart(sessionId, {
      question_id: 'q-god',
      source_agent: '规划者',
      source_agent_type: 'orchestrator',
      source_session_id: sessionId,
      target_agent: 'god',
      target_agent_type: 'claude-code',
      target_session_id: 'session-god',
      question: '你愿意参加 battle 吗？',
    })
    store.streamAgentUpdate(sessionId, 'claude-code', 'god', 'god-message-1')
    store.streamText(sessionId, '本座不仅愿意，而且迫不及待。')

    store.streamAskCardStart(sessionId, {
      question_id: 'q-aa',
      source_agent: '规划者',
      source_agent_type: 'orchestrator',
      source_session_id: sessionId,
      target_agent: 'aa',
      target_agent_type: 'claude-code',
      target_session_id: 'session-aa',
      question: '你愿意参加 battle 吗？',
    })

    const state = useChatStore.getState().getSession(sessionId)
    expect(state.messages).toHaveLength(2)
    expect(state.messages[1].agentName).toBe('god')
    expect(state.messages[1].content).toBe('本座不仅愿意，而且迫不及待。')
    expect(state.streamingAgentName).toBe('规划者')
    expect(state.runtimeBlocks).toHaveLength(1)
    expect(state.runtimeBlocks[0]?.type).toBe('ask_agent')
    if (state.runtimeBlocks[0]?.type === 'ask_agent') {
      expect(state.runtimeBlocks[0].target_agent).toBe('aa')
    }
  })

  it('keeps grouped ask-card and sub-agent reply in the same message bubble', () => {
    const store = useChatStore.getState()

    store.streamStart(sessionId, 'orchestrator')
    store.streamAskCardStart(sessionId, {
      question_id: 'q-group-1',
      source_agent: '管理者',
      source_agent_type: 'orchestrator',
      source_session_id: sessionId,
      target_agent: '实现者',
      target_agent_type: 'claude-code',
      target_session_id: 'session-impl',
      question: '请介绍一下你自己',
      group_id: 'orch-group-1',
    })
    store.streamAskCardDone(sessionId, {
      question_id: 'q-group-1',
      target_agent: '实现者',
      target_agent_type: 'claude-code',
      target_session_id: 'session-impl',
      summary: '已经介绍完成',
      status: 'completed',
      group_id: 'orch-group-1',
    })
    store.streamAgentUpdate(
      sessionId,
      'claude-code',
      '实现者',
      'worker-message-group-1',
      'orch-group-1',
    )
    store.streamText(sessionId, '你好，我是实现者。', 'worker-message-group-1')
    store.streamDone(sessionId)

    const state = useChatStore.getState().getSession(sessionId)
    expect(state.messages).toHaveLength(1)
    expect(state.messages[0].agentName).toBe('实现者')
    expect(state.messages[0].groupId).toBe('orch-group-1')
    expect(state.messages[0].content).toBe('你好，我是实现者。')
    const askCard = state.messages[0].blocks?.find((block) => block.type === 'ask_agent')
    expect(askCard?.type).toBe('ask_agent')
    if (askCard?.type === 'ask_agent') {
      expect(askCard.target_agent).toBe('实现者')
      expect(askCard.status).toBe('answered')
      expect(askCard.summary).toBe('已经介绍完成')
    }
  })

  it('keeps interleaved grouped agent text in independent message bubbles', () => {
    const store = useChatStore.getState()

    store.streamStart(sessionId, 'orchestrator')
    store.streamGroupedText(sessionId, {
      text: 'A1',
      messageId: 'alice-message',
      groupId: 'orch-group-parallel',
      agentType: 'pi',
      agentName: 'Alice',
    })
    store.streamGroupedText(sessionId, {
      text: 'B1',
      messageId: 'aa-message',
      groupId: 'orch-group-parallel',
      agentType: 'codex',
      agentName: '阿a',
    })
    store.streamGroupedText(sessionId, {
      text: 'A2',
      messageId: 'alice-message',
      groupId: 'orch-group-parallel',
      agentType: 'pi',
      agentName: 'Alice',
    })
    store.streamGroupedText(sessionId, {
      text: 'B2',
      messageId: 'aa-message',
      groupId: 'orch-group-parallel',
      agentType: 'codex',
      agentName: '阿a',
    })
    store.streamGroupedMessageStatus(sessionId, 'alice-message', 'completed')
    store.streamGroupedMessageStatus(sessionId, 'aa-message', 'failed')
    store.streamDone(sessionId)

    const state = useChatStore.getState().getSession(sessionId)
    expect(state.messages).toHaveLength(2)
    expect(state.messages.find((message) => message.messageId === 'alice-message')).toMatchObject({
      content: 'A1A2',
      agentName: 'Alice',
      status: 'completed',
    })
    expect(state.messages.find((message) => message.messageId === 'aa-message')).toMatchObject({
      content: 'B1B2',
      agentName: '阿a',
      status: 'failed',
    })
  })

  it('keeps repeated live chunks while skipping persisted grouped replay', () => {
    const store = useChatStore.getState()

    store.streamStart(sessionId, 'orchestrator')
    store.streamGroupedText(sessionId, {
      text: 'ha',
      messageId: 'new-live-message',
      groupId: 'orch-group-replay',
      agentType: 'pi',
      agentName: 'Alice',
    })
    store.streamGroupedText(sessionId, {
      text: 'ha',
      messageId: 'new-live-message',
      groupId: 'orch-group-replay',
      agentType: 'pi',
      agentName: 'Alice',
    })

    store.loadHistory(sessionId, [
      {
        id: 'persisted-message',
        role: 'agent',
        content: 'hello',
        messageId: 'persisted-message',
        groupId: 'orch-group-replay',
        agentType: 'codex',
        agentName: '阿a',
        timestamp: 1,
        status: 'streaming',
      },
    ])
    store.streamGroupedText(sessionId, {
      text: 'hel',
      messageId: 'persisted-message',
      groupId: 'orch-group-replay',
      agentType: 'codex',
      agentName: '阿a',
    })
    store.streamGroupedText(sessionId, {
      text: 'lo',
      messageId: 'persisted-message',
      groupId: 'orch-group-replay',
      agentType: 'codex',
      agentName: '阿a',
    })
    store.streamGroupedText(sessionId, {
      text: '!',
      messageId: 'persisted-message',
      groupId: 'orch-group-replay',
      agentType: 'codex',
      agentName: '阿a',
    })

    const state = useChatStore.getState().getSession(sessionId)
    expect(
      state.messages.find((message) => message.messageId === 'new-live-message')?.content,
    ).toBe('haha')
    expect(
      state.messages.find((message) => message.messageId === 'persisted-message')?.content,
    ).toBe('hello!')
  })

  it('keeps streamed content as a failed message when the stream errors', () => {
    const store = useChatStore.getState()

    store.streamStart(sessionId, 'codex')
    store.streamAgentUpdate(sessionId, 'codex', '执行者', 'worker-message-error')
    store.streamText(sessionId, '已经输出的内容', 'worker-message-error')
    store.streamError(sessionId, new Error('stream failed'))

    const state = useChatStore.getState().getSession(sessionId)
    expect(state.status).toBe('error')
    expect(state.streamingContent).toBe('')
    expect(state.messages).toHaveLength(1)
    expect(state.messages[0].content).toBe('已经输出的内容')
    expect(state.messages[0].status).toBe('failed')
  })

  it('shows a failed message when the stream errors before any text arrives', () => {
    const store = useChatStore.getState()

    store.streamStart(sessionId, 'orchestrator')
    store.streamAgentUpdate(sessionId, 'orchestrator', '管理者', 'orchestrator-error-message')
    store.streamError(
      sessionId,
      new Error('Orchestrator 推理失败：APIConnectionError: Connection error.'),
    )

    const state = useChatStore.getState().getSession(sessionId)
    expect(state.status).toBe('error')
    expect(state.messages).toHaveLength(1)
    expect(state.messages[0].content).toBe(
      'Orchestrator 推理失败：APIConnectionError: Connection error.',
    )
    expect(state.messages[0].status).toBe('failed')
    expect(state.messages[0].agentName).toBe('管理者')
  })

  it('keeps a pending plan review actionable after the stream ends', () => {
    const store = useChatStore.getState()

    store.streamStart(sessionId, 'orchestrator')
    store.streamPlanReviewEvent(sessionId, {
      review_key: 'review-current',
      session_id: sessionId,
      task_id: 'task-review',
      overview: '请确认执行计划',
      tasks: [],
      waves: [],
      status: 'pending',
    })
    store.streamDone(sessionId)

    const state = useChatStore.getState().getSession(sessionId)
    expect(state.activePlanReviewKey).toBe('review-current')
    expect(state.messages[0]?.blocks?.some((block) => block.type === 'plan_review')).toBe(true)
  })

  it('deduplicates replayed text chunks by message id', () => {
    const store = useChatStore.getState()

    store.streamStart(sessionId, 'codex')
    store.streamAgentUpdate(sessionId, 'codex', '执行者', 'worker-message-replay')
    store.streamText(sessionId, 'hello world', 'worker-message-replay')
    store.streamText(sessionId, 'hello', 'worker-message-replay')
    store.streamText(sessionId, ' world', 'worker-message-replay')
    store.streamText(sessionId, '!', 'worker-message-replay')
    store.streamDone(sessionId)

    const state = useChatStore.getState().getSession(sessionId)
    expect(state.messages).toHaveLength(1)
    expect(state.messages[0].content).toBe('hello world!')
  })
})
