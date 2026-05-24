import { useCallback, useEffect, useReducer, useRef } from 'react'

import type { StreamEvent } from '@/generated/events'
import type { AgentType } from '@/generated/request'
import { getTaskMessages } from '@/lib/api'
import { connectSSE } from '@/lib/sse'

export interface ChatMessage {
  id: string
  role: 'user' | 'agent' | 'system'
  content: string
  agentType?: AgentType
  timestamp: number
}

type ChatStatus = 'idle' | 'loading' | 'streaming' | 'tool_running' | 'done' | 'error'

interface ChatState {
  status: ChatStatus
  messages: ChatMessage[]
  streamingContent: string
  streamingAgentType?: AgentType
  error: Error | null
  toolName?: string
}

type ChatAction =
  | { type: 'LOAD_HISTORY'; messages: ChatMessage[] }
  | { type: 'SEND_MESSAGE'; message: ChatMessage }
  | { type: 'STREAM_START'; agentType: AgentType }
  | { type: 'STREAM_TEXT'; text: string }
  | { type: 'STREAM_TOOL_CALL'; toolName: string }
  | { type: 'STREAM_TOOL_RESULT' }
  | { type: 'STREAM_DONE' }
  | { type: 'STREAM_ERROR'; error: Error }
  | { type: 'RESET' }

const initialState: ChatState = {
  status: 'idle',
  messages: [],
  streamingContent: '',
  streamingAgentType: undefined,
  error: null,
  toolName: undefined,
}

function chatReducer(state: ChatState, action: ChatAction): ChatState {
  switch (action.type) {
    case 'LOAD_HISTORY':
      return {
        ...state,
        status: 'done',
        messages: action.messages,
      }

    case 'SEND_MESSAGE':
      return {
        ...state,
        status: 'loading',
        messages: [...state.messages, action.message],
        streamingContent: '',
        error: null,
      }

    case 'STREAM_START':
      return {
        ...state,
        status: 'streaming',
        streamingAgentType: action.agentType,
      }

    case 'STREAM_TEXT':
      return {
        ...state,
        status: state.status === 'tool_running' ? 'streaming' : state.status,
        streamingContent: state.streamingContent + action.text,
      }

    case 'STREAM_TOOL_CALL':
      return {
        ...state,
        status: 'tool_running',
        toolName: action.toolName,
      }

    case 'STREAM_TOOL_RESULT':
      return {
        ...state,
        status: 'streaming',
        toolName: undefined,
      }

    case 'STREAM_DONE': {
      const agentMessage: ChatMessage = {
        id: `agent-${Date.now()}`,
        role: 'agent',
        content: state.streamingContent,
        agentType: state.streamingAgentType,
        timestamp: Date.now(),
      }
      return {
        ...state,
        status: 'done',
        messages: [...state.messages, agentMessage],
        streamingContent: '',
        streamingAgentType: undefined,
      }
    }

    case 'STREAM_ERROR':
      return {
        ...state,
        status: 'error',
        error: action.error,
        streamingContent: '',
      }

    case 'RESET':
      return initialState

    default:
      return state
  }
}

export function useChatStream(taskId: string) {
  const [state, dispatch] = useReducer(chatReducer, initialState)
  const abortRef = useRef<AbortController | null>(null)

  // Load history on mount
  useEffect(() => {
    let cancelled = false
    getTaskMessages(taskId)
      .then((msgs) => {
        if (cancelled || msgs.length === 0) return
        const chatMessages: ChatMessage[] = msgs.map((m) => ({
          id: `${m.role}-${m.id}`,
          role: m.role,
          content: m.content,
          agentType: m.agent_type as AgentType | undefined,
          timestamp: new Date(m.created_at).getTime(),
        }))
        dispatch({ type: 'LOAD_HISTORY', messages: chatMessages })
      })
      .catch(() => {
        // silently ignore — empty state is fine
      })
    return () => {
      cancelled = true
    }
  }, [taskId])

  const sendMessage = useCallback(
    (message: string, sessionId: string, agentType: AgentType = 'claude-code') => {
      const userMessage: ChatMessage = {
        id: `user-${Date.now()}`,
        role: 'user',
        content: message,
        timestamp: Date.now(),
      }

      dispatch({ type: 'SEND_MESSAGE', message: userMessage })

      const controller = connectSSE({
        url: `/api/tasks/${taskId}/run`,
        body: { message, session_id: sessionId, agent_type: agentType },
        onEvent: (event: StreamEvent) => {
          switch (event.type) {
            case 'init':
              dispatch({ type: 'STREAM_START', agentType })
              break
            case 'text':
              dispatch({ type: 'STREAM_TEXT', text: (event.content?.text as string) ?? '' })
              break
            case 'tool_call':
              dispatch({
                type: 'STREAM_TOOL_CALL',
                toolName: (event.content?.name as string) ?? 'unknown',
              })
              break
            case 'tool_result':
              dispatch({ type: 'STREAM_TOOL_RESULT' })
              break
            case 'done':
              dispatch({ type: 'STREAM_DONE' })
              break
            case 'error':
              dispatch({
                type: 'STREAM_ERROR',
                error: new Error((event.content?.message as string) ?? 'Unknown error'),
              })
              break
          }
        },
        onError: (error) => {
          dispatch({ type: 'STREAM_ERROR', error })
        },
      })

      abortRef.current = controller
    },
    [taskId],
  )

  const abort = useCallback(() => {
    abortRef.current?.abort()
    abortRef.current = null
  }, [])

  return { state, sendMessage, abort }
}
