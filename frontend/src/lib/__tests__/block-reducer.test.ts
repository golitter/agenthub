import { describe, expect, it } from 'vitest'

import { reduceEventToBlocks } from '../block-reducer'

describe('reduceEventToBlocks', () => {
  it('returns text block for plain text', () => {
    const result = reduceEventToBlocks('Hello world')
    expect(result).toEqual([{ type: 'text', content: 'Hello world' }])
  })

  it('parses html-render block', () => {
    const input = 'Before\n```aka_yhy\ntype: html-render\n<div>Hello</div>\n```\nAfter'
    const result = reduceEventToBlocks(input)
    expect(result).toEqual([
      { type: 'text', content: 'Before\n' },
      { type: 'html-render', content: '<div>Hello</div>' },
      { type: 'text', content: '\nAfter' },
    ])
  })

  it('parses image block', () => {
    const input = '```aka_yhy\ntype: image\npath: chart.png\n```'
    const result = reduceEventToBlocks(input)
    expect(result).toEqual([{ type: 'image', path: 'chart.png' }])
  })

  it('parses attachment block', () => {
    const input = '```aka_yhy\ntype: attachment\npath: report.pdf\n```'
    const result = reduceEventToBlocks(input)
    expect(result).toEqual([{ type: 'attachment', path: 'report.pdf' }])
  })

  it('parses diff block', () => {
    const input = '```aka_yhy\ntype: diff\n```'
    const result = reduceEventToBlocks(input)
    expect(result).toEqual([{ type: 'diff' }])
  })

  it('parses preview block', () => {
    const input = '```aka_yhy\ntype: preview\nurl: http://localhost:3928/index.html\n```'
    const result = reduceEventToBlocks(input)
    expect(result).toEqual([{ type: 'preview', url: 'http://localhost:3928/index.html' }])
  })

  it('degrades unknown type to text block', () => {
    const input = '```aka_yhy\ntype: unknown\n```'
    const result = reduceEventToBlocks(input)
    expect(result).toEqual([{ type: 'text', content: '```aka_yhy\ntype: unknown\n```' }])
  })

  it('handles multiple blocks mixed with text', () => {
    const input =
      'Intro\n```aka_yhy\ntype: diff\n```\nMiddle\n```aka_yhy\ntype: preview\nurl: http://localhost:3000\n```\nEnd'
    const result = reduceEventToBlocks(input)
    expect(result).toEqual([
      { type: 'text', content: 'Intro\n' },
      { type: 'diff' },
      { type: 'text', content: '\nMiddle\n' },
      { type: 'preview', url: 'http://localhost:3000' },
      { type: 'text', content: '\nEnd' },
    ])
  })
})
