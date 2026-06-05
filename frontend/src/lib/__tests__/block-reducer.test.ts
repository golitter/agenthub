import { describe, expect, it } from 'vitest'

import { reduceEventToBlocks } from '../block-reducer'

describe('reduceEventToBlocks', () => {
  it('returns text block for plain text', () => {
    const result = reduceEventToBlocks('Hello world')
    expect(result).toHaveLength(1)
    expect(result[0].type).toBe('text')
    if (result[0].type === 'text') expect(result[0].content).toBe('Hello world')
    expect(result[0].id).toMatch(/^blk-/)
  })

  it('parses html-render block', () => {
    const input = 'Before\n```aka_yhy\ntype: html-render\n<div>Hello</div>\n```\nAfter'
    const result = reduceEventToBlocks(input)
    expect(result).toHaveLength(3)
    expect(result[0].type).toBe('text')
    if (result[0].type === 'text') expect(result[0].content).toBe('Before\n')
    expect(result[1].type).toBe('html-render')
    if (result[1].type === 'html-render') expect(result[1].content).toBe('<div>Hello</div>')
    expect(result[2].type).toBe('text')
    if (result[2].type === 'text') expect(result[2].content).toBe('\nAfter')
  })

  it('parses image block', () => {
    const input = '```aka_yhy\ntype: image\npath: chart.png\n```'
    const result = reduceEventToBlocks(input)
    expect(result).toHaveLength(1)
    expect(result[0].type).toBe('image')
    if (result[0].type === 'image') expect(result[0].path).toBe('chart.png')
  })

  it('parses attachment block', () => {
    const input = '```aka_yhy\ntype: attachment\npath: report.pdf\n```'
    const result = reduceEventToBlocks(input)
    expect(result).toHaveLength(1)
    expect(result[0].type).toBe('attachment')
    if (result[0].type === 'attachment') expect(result[0].path).toBe('report.pdf')
  })

  it('parses diff block', () => {
    const input = '```aka_yhy\ntype: diff\nsnapshotId: snap-123\n```'
    const result = reduceEventToBlocks(input)
    expect(result).toHaveLength(1)
    expect(result[0].type).toBe('diff')
    if (result[0].type === 'diff') expect(result[0].snapshotId).toBe('snap-123')
  })

  it('parses preview block', () => {
    const input = '```aka_yhy\ntype: preview\nurl: http://localhost:3928/index.html\n```'
    const result = reduceEventToBlocks(input)
    expect(result).toHaveLength(1)
    expect(result[0].type).toBe('preview')
    if (result[0].type === 'preview') expect(result[0].url).toBe('http://localhost:3928/index.html')
  })

  it('degrades unknown type to text block', () => {
    const input = '```aka_yhy\ntype: unknown\n```'
    const result = reduceEventToBlocks(input)
    expect(result).toHaveLength(1)
    expect(result[0].type).toBe('text')
    if (result[0].type === 'text') expect(result[0].content).toBe('```aka_yhy\ntype: unknown\n```')
  })

  it('handles multiple blocks mixed with text', () => {
    const input =
      'Intro\n```aka_yhy\ntype: diff\nsnapshotId: snap-1\n```\nMiddle\n```aka_yhy\ntype: preview\nurl: http://localhost:3000\n```\nEnd'
    const result = reduceEventToBlocks(input)
    expect(result).toHaveLength(5)
    expect(result.map((b) => b.type)).toEqual(['text', 'diff', 'text', 'preview', 'text'])
  })

  it('assigns unique ids to each block', () => {
    const input =
      'A\n```aka_yhy\ntype: diff\n```\nB\n```aka_yhy\ntype: preview\nurl: http://x\n```\nC'
    const result = reduceEventToBlocks(input)
    const ids = result.map((b) => b.id)
    expect(new Set(ids).size).toBe(ids.length)
  })

  it('folds legacy bare runtime status lines', () => {
    const input =
      'type: runtime_status\n' +
      'json: {"agent":"执行者","status":"running","streamingText":"你好","task_id":"task-001"}\n' +
      'type: runtime_status\n' +
      'json: {"agent":"执行者","status":"running","streamingText":"！","task_id":"task-001"}\n' +
      'type: runtime_status\n' +
      'json: {"agent":"执行者","status":"completed","task_id":"task-001"}'
    const result = reduceEventToBlocks(input)

    expect(result).toHaveLength(1)
    expect(result[0].type).toBe('runtime_status')
    if (result[0].type === 'runtime_status') {
      expect(result[0].status).toBe('completed')
      expect(result[0].streamingText).toBe('你好！')
    }
  })

  it('folds legacy bare plan lines', () => {
    const input =
      '说明\n' +
      'type: plan\n' +
      'json: {"overview":"","tasks":[{"agent":"执行者","status":"pending","task_id":"task-001","title":"介绍自己"}]}'
    const result = reduceEventToBlocks(input)

    expect(result.map((block) => block.type)).toEqual(['text', 'plan'])
    expect(result[1].type).toBe('plan')
    if (result[1].type === 'plan') {
      expect(result[1].tasks[0].title).toBe('介绍自己')
    }
  })

  it('folds legacy plan review lines', () => {
    const input =
      'type: plan_review\n' +
      'json: {"session_id":"s-1","task_id":"t-1","status":"pending","plan":{"overview":"先检查再实现","tasks":[{"task_id":"task-001","session_id":"agent-a","title":"跑 lint","content":"执行 lint"}]},"waves":[]}'
    const result = reduceEventToBlocks(input)

    expect(result).toHaveLength(1)
    expect(result[0].type).toBe('plan_review')
    if (result[0].type === 'plan_review') {
      expect(result[0].overview).toBe('先检查再实现')
      expect(result[0].tasks[0].agent).toBe('agent-a')
      expect(result[0].tasks[0].content).toBe('执行 lint')
    }
  })

  it('keeps separate plan review rounds when review keys differ', () => {
    const input =
      'type: plan_review\n' +
      'json: {"review_key":"r-1","session_id":"s-1","task_id":"t-1","status":"submitted","plan":{"overview":"旧规划","tasks":[{"task_id":"task-001","session_id":"agent-a","title":"旧任务"}]},"waves":[]}\n' +
      'type: plan_review\n' +
      'json: {"review_key":"r-2","session_id":"s-1","task_id":"t-1","status":"pending","plan":{"overview":"新规划","tasks":[{"task_id":"task-001","session_id":"agent-a","title":"新任务"}]},"waves":[]}'
    const result = reduceEventToBlocks(input)

    expect(result).toHaveLength(2)
    expect(result.map((block) => block.type)).toEqual(['plan_review', 'plan_review'])
    if (result[0].type === 'plan_review' && result[1].type === 'plan_review') {
      expect(result[0].overview).toBe('旧规划')
      expect(result[0].status).toBe('submitted')
      expect(result[1].overview).toBe('新规划')
      expect(result[1].status).toBe('pending')
    }
  })

  it('folds legacy fenced runtime status lines', () => {
    const input =
      '```yaml\n' +
      'type: runtime_status\n' +
      'json: {"agent":"执行者","status":"running","streamingText":"Cla","task_id":"task-001"}\n' +
      '```\n' +
      '```yaml\n' +
      'type: runtime_status\n' +
      'json: {"agent":"执行者","status":"running","streamingText":"ude","task_id":"task-001"}\n' +
      '```'
    const result = reduceEventToBlocks(input)

    expect(result).toHaveLength(1)
    expect(result[0].type).toBe('runtime_status')
    if (result[0].type === 'runtime_status') {
      expect(result[0].streamingText).toBe('Claude')
    }
  })

  it('folds legacy ask-agent start and done lines into one card', () => {
    const input =
      'type: ask_agent\n' +
      'json: {"question_id":"q-1","source_agent":"管理者","source_agent_type":"orchestrator","source_session_id":"s-manager","target_agent":"执行者","target_agent_type":"claude-code","target_session_id":"s-worker","question":"请介绍一下你自己","status":"pending","collapsed":false}\n' +
      'type: ask_agent\n' +
      'json: {"question_id":"q-1","target_agent":"执行者","summary":"我是执行者","status":"answered","collapsed":true}'
    const result = reduceEventToBlocks(input)

    expect(result).toHaveLength(1)
    expect(result[0].type).toBe('ask_agent')
    if (result[0].type === 'ask_agent') {
      expect(result[0].source_agent).toBe('管理者')
      expect(result[0].target_agent).toBe('执行者')
      expect(result[0].question).toBe('请介绍一下你自己')
      expect(result[0].status).toBe('answered')
      expect(result[0].collapsed).toBe(true)
      expect(result[0].summary).toBe('我是执行者')
    }
  })

  it('keeps failed ask-agent cards as unanswered', () => {
    const input =
      'type: ask_agent\n' +
      'json: {"question_id":"q-2","target_agent":"执行者","target_session_id":"s-worker","question":"能连接吗","status":"failed","collapsed":false}'
    const result = reduceEventToBlocks(input)

    expect(result).toHaveLength(1)
    expect(result[0].type).toBe('ask_agent')
    if (result[0].type === 'ask_agent') {
      expect(result[0].status).toBe('failed')
      expect(result[0].collapsed).toBe(false)
    }
  })

  it('separates legacy timeout markers from surrounding prose', () => {
    const result = reduceEventToBlocks(
      '前面是正常回答。\n[Timeout] Task task-003 exceeded 300.0s\n后续说明',
    )

    expect(result.map((block) => block.type)).toEqual(['text', 'task_failure', 'text'])
    expect(result[1].type).toBe('task_failure')
    if (result[1].type === 'task_failure') {
      expect(result[1].failureType).toBe('timeout')
      expect(result[1].task_id).toBe('task-003')
      expect(result[1].reason).toBe('exceeded 300.0s')
    }
  })

  it('separates legacy error markers from prose and keeps task metadata', () => {
    const result = reduceEventToBlocks(
      '已完成部分内容。[Error] Task task-004 agent=执行者 failed: command failed',
    )

    expect(result.map((block) => block.type)).toEqual(['text', 'task_failure'])
    expect(result[1].type).toBe('task_failure')
    if (result[1].type === 'task_failure') {
      expect(result[1].failureType).toBe('error')
      expect(result[1].task_id).toBe('task-004')
      expect(result[1].agent).toBe('执行者')
      expect(result[1].reason).toBe('command failed')
    }
  })

  it('leaves unknown timeout marker text untouched', () => {
    const result = reduceEventToBlocks('[Timeout] exceeded without a task id')

    expect(result).toHaveLength(1)
    expect(result[0].type).toBe('text')
    if (result[0].type === 'text') {
      expect(result[0].content).toBe('[Timeout] exceeded without a task id')
    }
  })

  it('normalizes nested legacy error-timeout markers as timeout failures', () => {
    const result = reduceEventToBlocks('[Error] [Timeout] Task task-003 exceeded 300.0s')

    expect(result).toHaveLength(1)
    expect(result[0].type).toBe('task_failure')
    if (result[0].type === 'task_failure') {
      expect(result[0].failureType).toBe('timeout')
      expect(result[0].task_id).toBe('task-003')
      expect(result[0].reason).toBe('exceeded 300.0s')
    }
  })

  it('splits adjacent legacy failure markers without requiring newlines', () => {
    const result = reduceEventToBlocks(
      '[Timeout] Task task-004 exceeded 300.0s[Error] [Timeout] Task task-003 exceeded 300.0s',
    )

    expect(result.map((block) => block.type)).toEqual(['task_failure', 'task_failure'])
    expect(result[0].type).toBe('task_failure')
    expect(result[1].type).toBe('task_failure')
    if (result[0].type === 'task_failure' && result[1].type === 'task_failure') {
      expect(result[0].task_id).toBe('task-004')
      expect(result[0].reason).toBe('exceeded 300.0s')
      expect(result[1].task_id).toBe('task-003')
      expect(result[1].reason).toBe('exceeded 300.0s')
    }
  })

  it('parses final summary blocks', () => {
    const input =
      '```aka_yhy\n' +
      'type: final_summary\n' +
      'json: {"status":"partial","completed":2,"failed":1,"nextAction":"重试失败任务","details":[{"task_id":"task-001","agent":"执行者","status":"completed","summary":"完成页面"},{"task_id":"task-002","agent":"检查者","status":"failed","summary":"超时"}]}\n' +
      '```'
    const result = reduceEventToBlocks(input)

    expect(result).toHaveLength(1)
    expect(result[0].type).toBe('final_summary')
    if (result[0].type === 'final_summary') {
      expect(result[0].status).toBe('partial')
      expect(result[0].completed).toBe(2)
      expect(result[0].failed).toBe(1)
      expect(result[0].details[1].status).toBe('failed')
    }
  })

  it('parses final summary blocks when detail summaries contain fenced code', () => {
    const input =
      '```aka_yhy\n' +
      'type: final_summary\n' +
      'json: {"status":"success","completed":1,"failed":0,"nextAction":"可以继续验收结果。","details":[{"task_id":"task-001","agent":"执行者01","status":"completed","summary":"```markdown\\n# Hello from test-agent1\\n```\\n仅一行占位标题。"}]}\n' +
      '```\n\n' +
      '好的，这是根据执行结果生成的汇总报告。'
    const result = reduceEventToBlocks(input)

    expect(result.map((block) => block.type)).toEqual(['final_summary'])
    expect(result[0].type).toBe('final_summary')
    if (result[0].type === 'final_summary') {
      expect(result[0].status).toBe('success')
      expect(result[0].details[0].summary).toContain('Hello from test-agent1')
    }
  })

  it('drops trailing legacy text after a final summary block', () => {
    const input =
      '```aka_yhy\n' +
      'type: final_summary\n' +
      'json: {"status":"failed","completed":0,"failed":1,"details":[]}\n' +
      '```\n\n' +
      '## 项目执行报告\n\n这一段是重复汇总。'

    const result = reduceEventToBlocks(input)

    expect(result).toHaveLength(1)
    expect(result[0].type).toBe('final_summary')
  })
})
