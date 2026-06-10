export interface PlanTask {
  task_id: string
  agent: string
  title: string
  status: 'pending' | 'running' | 'completed' | 'failed'
  content?: string
  session_id?: string
}

export interface PlanReviewPayload {
  review_key?: string
  session_id?: string
  task_id?: string
  review_type?: 'plan' | 'merge_to_main'
  source_branch?: string
  target_branch?: string
  diff_snapshot_id?: string
  overview: string
  tasks: PlanTask[]
  waves: PlanTask[][]
  status: 'pending' | 'submitted' | 'approved'
}

export interface CoordMessage {
  from: string
  to: string
  text: string
  round: number
}

export interface FinalSummaryDetail {
  task_id: string
  agent: string
  status: 'completed' | 'failed'
  summary?: string
}

export type MessageBlock =
  | { type: 'text'; id: string; content: string }
  | { type: 'html-render'; id: string; content: string; streaming?: boolean }
  | { type: 'image'; id: string; path: string }
  | { type: 'attachment'; id: string; path: string }
  | { type: 'diff'; id: string; snapshotId: string }
  | { type: 'preview'; id: string; url: string }
  | { type: 'plan'; id: string; overview: string; tasks: PlanTask[] }
  | ({ type: 'plan_review'; id: string } & PlanReviewPayload)
  | {
      type: 'runtime_status'
      id: string
      task_id: string
      agent: string
      status: string
      title?: string
      streamingText?: string
    }
  | {
      type: 'coordination'
      id: string
      messages: CoordMessage[]
      closed: boolean
      summary?: string
    }
  | {
      type: 'ask_agent'
      id: string
      question_id: string
      source_agent?: string
      source_agent_type?: string
      source_session_id?: string
      target_agent: string
      target_agent_type?: string
      target_session_id: string
      question: string
      status: 'pending' | 'answered' | 'failed'
      collapsed: boolean
      summary?: string
    }
  | {
      type: 'task_failure'
      id: string
      task_id?: string
      agent?: string
      reason: string
      failureType: 'timeout' | 'error'
    }
  | {
      type: 'final_summary'
      id: string
      status: 'success' | 'partial' | 'failed'
      completed: number
      failed: number
      nextAction?: string
      details: FinalSummaryDetail[]
    }
  | { type: 'tool_call'; id: string; name: string; input?: string }
  | { type: 'tool_result'; id: string; output?: string }
