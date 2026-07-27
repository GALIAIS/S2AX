import { describe, expect, it } from 'vitest'
import { presentInvocationArchivePayload } from '../payloadPresentation'
import type { InvocationArchivePayloadView } from '../types'

function payload(overrides: Partial<InvocationArchivePayloadView> = {}): InvocationArchivePayloadView {
  return {
    available: true,
    status: 'captured',
    content_type: 'application/json; charset=utf-8',
    encoding: 'utf8',
    data: '',
    total_bytes: 0,
    captured_bytes: 0,
    truncated: false,
    ...overrides,
  }
}

describe('Invocation Archive payload presentation', () => {
  it('formats chat messages into a readable transcript without changing the raw payload', () => {
    const raw = JSON.stringify({
      messages: [
        { role: 'user', content: 'hello' },
        { role: 'tool', tool_call_id: 'call_1', content: '{"ok":true}' },
      ],
    })
    const presentation = presentInvocationArchivePayload(payload({ data: raw }))

    expect(presentation.raw).toBe(raw)
    expect(presentation.format).toBe('json')
    expect(presentation.formatted).toContain('"messages"')
    expect(presentation.transcript).toMatchObject([
      { role: 'user', content: 'hello' },
      { role: 'tool_result', content: '{\n  "ok": true\n}' },
    ])
  })

  it('uses the preserved declared charset when decoding Base64 payload bytes', () => {
    const presentation = presentInvocationArchivePayload(payload({
      content_type: 'text/plain; charset=gb18030',
      encoding: 'base64',
      data: 'xOO6ww==',
    }))

    expect(presentation.charset).toBe('gb18030')
    expect(presentation.formatted).toBe('你好')
    expect(presentation.canSelectCharset).toBe(true)
  })

  it('reports replacement characters as an irreversible upstream loss', () => {
    const presentation = presentInvocationArchivePayload(payload({ data: `{"content":"\uFFFD"}` }))

    expect(presentation.warnings).toContain('replacement_character')
    expect(presentation.repaired).toBeUndefined()
  })

  it('caps only the structured transcript and leaves the complete payload available', () => {
    const messages = Array.from({ length: 81 }, (_, index) => ({ role: 'user', content: `message ${index}` }))
    const presentation = presentInvocationArchivePayload(payload({ data: JSON.stringify({ messages }) }))

    expect(presentation.transcript).toHaveLength(80)
    expect(presentation.warnings).toContain('transcript_limit')
    expect(presentation.formatted).toContain('message 80')
  })

  it('renders Responses tool calls and tool outputs without dropping their payloads', () => {
    const raw = JSON.stringify({
      input: [
        { type: 'function_call', call_id: 'call_1', name: 'bash', arguments: '{"command":"Get-ChildItem"}' },
        { type: 'function_call_output', call_id: 'call_1', output: '{"stdout":"hello"}' },
      ],
    })
    const presentation = presentInvocationArchivePayload(payload({ data: raw }))

    expect(presentation.transcript).toMatchObject([
      { role: 'tool_call', title: 'bash', content: '{\n  "command": "Get-ChildItem"\n}' },
      { role: 'tool_result', content: '{\n  "stdout": "hello"\n}' },
    ])
    expect(presentation.raw).toBe(raw)
  })

  it('keeps WebSocket frame order and binary frame data while structuring tool output', () => {
    const firstFrame = JSON.stringify({
      type: 'response.output_item.done',
      item: { type: 'function_call_output', call_id: 'call_1', output: '{"ok":true}' },
    })
    const presentation = presentInvocationArchivePayload(payload({
      data: firstFrame,
      frames: [
        {
          sequence: 1,
          kind: 'text',
          occurred_at: '2026-07-27T00:00:00Z',
          encoding: 'utf8',
          data: firstFrame,
          total_bytes: firstFrame.length,
          captured_bytes: firstFrame.length,
          truncated: false,
        },
        {
          sequence: 2,
          kind: 'binary',
          occurred_at: '2026-07-27T00:00:01Z',
          encoding: 'base64',
          data: 'AP8B',
          total_bytes: 3,
          captured_bytes: 3,
          truncated: false,
        },
      ],
    }))

    expect(presentation.transcript).toEqual(expect.arrayContaining([
      expect.objectContaining({ role: 'tool_result', content: '{\n  "ok": true\n}' }),
      expect.objectContaining({ role: 'websocket_frame', title: 'binary', content: 'AP8B' }),
    ]))
  })

  it('marks a frame transcript limit without discarding the archived frame bytes', () => {
    const frame = JSON.stringify({
      messages: Array.from({ length: 81 }, (_, index) => ({ role: 'user', content: `frame message ${index}` })),
    })
    const presentation = presentInvocationArchivePayload(payload({
      data: frame,
      frames: [{
        sequence: 1,
        kind: 'text',
        encoding: 'utf8',
        data: frame,
        total_bytes: frame.length,
        captured_bytes: frame.length,
        truncated: false,
      }],
    }))

    expect(presentation.transcript).toHaveLength(80)
    expect(presentation.warnings).toContain('transcript_limit')
    expect(presentation.raw).toContain('frame message 80')
  })

  it('extracts tool calls from streamed Responses events', () => {
    const presentation = presentInvocationArchivePayload(payload({
      content_type: 'text/event-stream',
      data: [
        'event: response.output_item.done',
        'data: {"type":"response.output_item.done","item":{"type":"function_call","call_id":"call_2","name":"search","arguments":"{\\"query\\":\\"archive\\"}"}}',
        '',
      ].join('\n'),
    }))

    expect(presentation.format).toBe('sse')
    expect(presentation.transcript).toEqual(expect.arrayContaining([
      expect.objectContaining({ role: 'tool_call', title: 'search', content: '{\n  "query": "archive"\n}' }),
    ]))
  })
})
