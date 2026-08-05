import type { InvocationArchivePayloadChunk, InvocationArchivePayloadFrame, InvocationArchivePayloadView } from './types'

export type InvocationArchivePayloadViewMode = 'structured' | 'formatted' | 'raw' | 'repaired'
export type InvocationArchivePayloadFormat = 'json' | 'ndjson' | 'sse' | 'form' | 'text' | 'base64'
export type InvocationArchivePayloadWarning = 'replacement_character' | 'invalid_base64' | 'charset_failed' | 'binary' | 'format_limit' | 'transcript_limit' | 'frame_metadata_limit' | 'mojibake_candidate'

export interface InvocationArchiveTranscriptEntry {
  role: string
  title?: string
  content: string
  metadata: string[]
}

export interface InvocationArchiveImagePreview {
  src: string
  mime_type: string
  label: string
}

export interface InvocationArchivePayloadPresentation {
  raw: string
  formatted: string
  format: InvocationArchivePayloadFormat
  charset: string
  canFormat: boolean
  canSelectCharset: boolean
  repaired?: string
  transcript: InvocationArchiveTranscriptEntry[]
  images: InvocationArchiveImagePreview[]
  warnings: InvocationArchivePayloadWarning[]
}

export const invocationArchivePayloadCharsets = ['auto', 'utf-8', 'gb18030', 'big5', 'shift_jis', 'windows-1252', 'utf-16le', 'utf-16be'] as const

const FORMAT_LIMIT_BYTES = 2 * 1024 * 1024
const TRANSCRIPT_ENTRY_LIMIT = 80
const IMAGE_PREVIEW_LIMIT_BYTES = 12 * 1024 * 1024
const IMAGE_PREVIEW_LIMIT = 8

export function presentInvocationArchivePayload(
  payload: InvocationArchivePayloadView,
  charsetChoice = 'auto',
): InvocationArchivePayloadPresentation {
  const raw = payload.data || ''
  const warnings: InvocationArchivePayloadWarning[] = []
  const isBase64 = payload.encoding?.toLowerCase() === 'base64'
  const frameTranscript = buildFrameTranscript(payload, charsetChoice)
  if (frameTranscript.truncated) warnings.push('transcript_limit')
  if (payload.frames_truncated) warnings.push('frame_metadata_limit')
  let text = raw
  let charset = 'utf-8'
  let bytes: Uint8Array | undefined
  let images: InvocationArchiveImagePreview[] = []

  if (isBase64) {
    bytes = decodeBase64(raw)
    if (!bytes) return unavailableTextPresentation(raw, 'invalid_base64', frameTranscript.entries, warnings)
    const decoded = decodeText(bytes, decoderCandidates(payload.content_type, charsetChoice))
    images = extractImagePreviews(payload, bytes, decoded?.value || '')
    if (!decoded) return unavailableTextPresentation(raw, 'charset_failed', frameTranscript.entries, warnings, images)
    if (!isLikelyText(decoded.value)) return unavailableTextPresentation(raw, 'binary', frameTranscript.entries, warnings, images)
    text = decoded.value
    charset = decoded.charset
  } else if (isImageContentType(payload.content_type) || isMultipartContentType(payload.content_type)) {
    bytes = payloadBytes(payload)
  }

  if (images.length === 0) images = extractImagePreviews(payload, bytes, text)

  if (text.includes('\uFFFD')) warnings.push('replacement_character')
  const repaired = text.includes('\uFFFD') ? undefined : recoverSingleByteMojibake(text)
  if (repaired) warnings.push('mojibake_candidate')
  if (byteLength(text) > FORMAT_LIMIT_BYTES) {
    warnings.push('format_limit')
    return {
      raw,
      formatted: text,
      format: isBase64 ? 'base64' : 'text',
      charset,
      canFormat: isBase64,
      canSelectCharset: isBase64,
      repaired,
      transcript: frameTranscript.entries,
      images,
      warnings,
    }
  }

  const formatted = formatText(text, payload.content_type)
  if (formatted.transcriptTruncated && !warnings.includes('transcript_limit')) warnings.push('transcript_limit')
  return {
    raw,
    formatted: formatted.value,
    format: formatted.format,
    charset,
    canFormat: isBase64 || formatted.canFormat,
    canSelectCharset: isBase64,
    repaired,
    transcript: frameTranscript.entries.length > 0 ? frameTranscript.entries : formatted.transcript,
    images,
    warnings,
  }
}

function unavailableTextPresentation(
  raw: string,
  warning: InvocationArchivePayloadWarning,
  transcript: InvocationArchiveTranscriptEntry[] = [],
  warnings: InvocationArchivePayloadWarning[] = [],
  images: InvocationArchiveImagePreview[] = [],
): InvocationArchivePayloadPresentation {
  return {
    raw,
    formatted: raw,
    format: 'base64',
    charset: 'base64',
    canFormat: false,
    canSelectCharset: true,
    transcript,
    images,
    warnings: [...warnings, warning],
  }
}

export function mergeInvocationArchivePayloadChunks(chunks: InvocationArchivePayloadChunk[]): InvocationArchivePayloadChunk {
  if (chunks.length === 0) throw new Error('cannot merge an empty payload')
  const first = chunks[0]
  const payloads = chunks.map((chunk) => chunk.payload)
  const encoding = first.payload.encoding?.toLowerCase()
  const data = encoding === 'base64'
    ? encodeBase64(concatBytes(payloads.map((payload) => decodeBase64(payload.data || '') || new Uint8Array())))
    : payloads.map((payload) => payload.data || '').join('')
  const frames = payloads.flatMap((payload) => payload.frames || [])
    .filter((frame, index, all) => all.findIndex((candidate) => candidate.sequence === frame.sequence && candidate.offset === frame.offset) === index)
    .sort((left, right) => left.offset - right.offset)
  const last = chunks[chunks.length - 1]
  const loadedBytes = payloads.reduce((total, payload) => total + (payload.loaded_bytes ?? 0), 0)
  return {
    ...last,
    next_offset: last.next_offset,
    payload: {
      ...first.payload,
      data,
      offset: 0,
      loaded_bytes: loadedBytes,
      complete: last.payload.complete,
      frames,
      frames_truncated: payloads.some((payload) => payload.frames_truncated),
    },
  }
}

function concatBytes(parts: Uint8Array[]): Uint8Array {
  const total = parts.reduce((size, part) => size + part.byteLength, 0)
  const result = new Uint8Array(total)
  let offset = 0
  for (const part of parts) {
    result.set(part, offset)
    offset += part.byteLength
  }
  return result
}

function extractImagePreviews(payload: InvocationArchivePayloadView, bytes?: Uint8Array, text = ''): InvocationArchiveImagePreview[] {
  if (!payload.complete || payload.truncated) return []
  const result: InvocationArchiveImagePreview[] = []
  const seen = new Set<string>()
  const addBytes = (source: Uint8Array, declaredMime: string, label: string) => {
    if (result.length >= IMAGE_PREVIEW_LIMIT || source.byteLength === 0 || source.byteLength > IMAGE_PREVIEW_LIMIT_BYTES) return
    const mimeType = imageMimeType(declaredMime, source)
    if (!mimeType) return
    const src = `data:${mimeType};base64,${encodeBase64(source)}`
    if (seen.has(src)) return
    seen.add(src)
    result.push({ src, mime_type: mimeType, label: label || `image_${result.length + 1}` })
  }
  const addDataURL = (value: string, label: string) => {
    const match = value.match(/^data:(image\/[a-z0-9.+-]+);base64,([a-z0-9+/=\s]+)$/i)
    if (!match) return
    const decoded = decodeBase64(match[2])
    if (decoded) addBytes(decoded, match[1], label)
  }

  if (bytes && (isImageContentType(payload.content_type) || Boolean(imageMimeType('', bytes)))) addBytes(bytes, mediaType(payload.content_type), 'image')
  if (bytes && isMultipartContentType(payload.content_type)) extractMultipartImages(bytes, payload.content_type, addBytes)
  if (text) {
    addDataURL(text.trim(), 'image')
    const parsed = parseJSON(text)
    if (parsed.ok) visitImageValue(parsed.value, 'image', addBytes, addDataURL)
  }
  return result
}

function visitImageValue(
  value: unknown,
  label: string,
  addBytes: (bytes: Uint8Array, declaredMime: string, label: string) => void,
  addDataURL: (value: string, label: string) => void,
  depth = 0,
): void {
  if (depth > 6) return
  if (typeof value === 'string') {
    addDataURL(value.trim(), label)
    return
  }
  if (Array.isArray(value)) {
    value.forEach((item, index) => visitImageValue(item, `${label}_${index + 1}`, addBytes, addDataURL, depth + 1))
    return
  }
  if (!isRecord(value)) return
  const mimeType = stringValue(firstDefined(value.mime_type, value.mimeType, value.content_type, value.media_type))
  for (const key of ['url', 'src', 'image_url']) {
    const candidate = value[key]
    if (typeof candidate === 'string') addDataURL(candidate.trim(), label)
    else if (isRecord(candidate)) visitImageValue(candidate, label, addBytes, addDataURL, depth + 1)
  }
  for (const key of ['b64_json', 'image_base64', 'base64']) {
    const candidate = value[key]
    if (typeof candidate !== 'string' || candidate.length < 16) continue
    const decoded = decodeBase64(candidate)
    if (decoded) addBytes(decoded, mimeType, label)
  }
  for (const [key, candidate] of Object.entries(value)) {
    if (key === 'b64_json' || key === 'image_base64' || key === 'base64' || key === 'url' || key === 'src' || key === 'image_url') continue
    if (typeof candidate === 'object') visitImageValue(candidate, key, addBytes, addDataURL, depth + 1)
  }
}

function extractMultipartImages(
  bytes: Uint8Array,
  contentType: string,
  addBytes: (bytes: Uint8Array, declaredMime: string, label: string) => void,
): void {
  const boundary = contentType.match(/(?:^|;)\s*boundary\s*=\s*(?:"([^"]+)"|([^;\s]+))/i)?.[1]
    || contentType.match(/(?:^|;)\s*boundary\s*=\s*(?:"([^"]+)"|([^;\s]+))/i)?.[2]
  if (!boundary) return
  const marker = new TextEncoder().encode(`--${boundary}`)
  const headerSeparator = new Uint8Array([13, 10, 13, 10])
  const lineBreak = new Uint8Array([13, 10])
  let cursor = 0
  while (cursor < bytes.length) {
    const markerStart = indexOfBytes(bytes, marker, cursor)
    if (markerStart < 0) return
    let partStart = markerStart + marker.length
    if (bytes[partStart] === 45 && bytes[partStart + 1] === 45) return
    if (matchesBytes(bytes, lineBreak, partStart)) partStart += lineBreak.length
    const headerEnd = indexOfBytes(bytes, headerSeparator, partStart)
    if (headerEnd < 0) return
    const bodyStart = headerEnd + headerSeparator.length
    const nextMarker = indexOfBytes(bytes, marker, bodyStart)
    if (nextMarker < 0) return
    let bodyEnd = nextMarker
    if (bodyEnd >= 2 && bytes[bodyEnd - 2] === 13 && bytes[bodyEnd - 1] === 10) bodyEnd -= 2
    const headers = new TextDecoder('latin1').decode(bytes.slice(partStart, headerEnd))
    const partType = headers.match(/^content-type:\s*([^\r\n]+)/im)?.[1]?.trim() || ''
    if (isImageContentType(partType)) {
      const label = headers.match(/(?:^|;)\s*filename\*?\s*=\s*(?:"([^"]+)"|([^;\r\n]+))/im)?.[1]
        || headers.match(/(?:^|;)\s*filename\*?\s*=\s*(?:"([^"]+)"|([^;\r\n]+))/im)?.[2]
        || 'image'
      addBytes(bytes.slice(bodyStart, bodyEnd), mediaType(partType), label.trim())
    }
    cursor = nextMarker
  }
}

function indexOfBytes(source: Uint8Array, needle: Uint8Array, from: number): number {
  outer: for (let index = from; index <= source.length - needle.length; index++) {
    for (let offset = 0; offset < needle.length; offset++) if (source[index + offset] !== needle[offset]) continue outer
    return index
  }
  return -1
}

function matchesBytes(source: Uint8Array, needle: Uint8Array, offset: number): boolean {
  return offset >= 0 && offset + needle.length <= source.length && needle.every((byte, index) => source[offset + index] === byte)
}

function isImageContentType(contentType: string): boolean { return mediaType(contentType).startsWith('image/') }
function isMultipartContentType(contentType: string): boolean { return ['multipart/form-data', 'multipart/mixed'].includes(mediaType(contentType)) }
function mediaType(contentType: string): string { return contentType.split(';', 1)[0].trim().toLowerCase() }

function imageMimeType(declared: string, bytes: Uint8Array): string | undefined {
  const normalized = mediaType(declared)
  if (normalized.startsWith('image/')) return normalized
  if (bytes.length >= 8 && bytes[0] === 0x89 && bytes[1] === 0x50 && bytes[2] === 0x4e && bytes[3] === 0x47) return 'image/png'
  if (bytes.length >= 3 && bytes[0] === 0xff && bytes[1] === 0xd8 && bytes[2] === 0xff) return 'image/jpeg'
  if (bytes.length >= 6 && new TextDecoder('ascii').decode(bytes.slice(0, 6)) === 'GIF89a') return 'image/gif'
  if (bytes.length >= 12 && new TextDecoder('ascii').decode(bytes.slice(0, 4)) === 'RIFF' && new TextDecoder('ascii').decode(bytes.slice(8, 12)) === 'WEBP') return 'image/webp'
  return undefined
}

function decodeBase64(value: string): Uint8Array | undefined {
  try {
    const binary = atob(value.replace(/\s+/g, ''))
    return Uint8Array.from(binary, (character) => character.charCodeAt(0))
  } catch {
    return undefined
  }
}

function decoderCandidates(contentType: string, choice: string): string[] {
  if (choice !== 'auto') return [normalizeCharset(choice)]
  const declared = declaredCharset(contentType)
  return [...new Set([declared, 'utf-8'].filter(Boolean))]
}

function declaredCharset(contentType: string): string {
  const match = contentType.match(/(?:^|;)\s*charset\s*=\s*(?:"([^"]+)"|([^;\s]+))/i)
  return normalizeCharset(match?.[1] || match?.[2] || '')
}

function normalizeCharset(value: string): string {
  const normalized = value.trim().toLowerCase().replace(/_/g, '-')
  switch (normalized) {
    case 'utf8': return 'utf-8'
    case 'utf-16': return 'utf-16le'
    case 'gbk':
    case 'gb2312':
    case 'cp936': return 'gb18030'
    case 'shift-jis':
    case 'sjis': return 'shift_jis'
    case 'windows1252': return 'windows-1252'
    default: return normalized
  }
}

function decodeText(bytes: Uint8Array, charsets: string[]): { value: string; charset: string } | undefined {
  for (const charset of charsets) {
    try {
      return { value: new TextDecoder(charset, { fatal: true }).decode(bytes), charset }
    } catch {
      // Try the next explicit or safe fallback charset without modifying the source payload.
    }
  }
  return undefined
}

function isLikelyText(value: string): boolean {
  const controls = Array.from(value).filter((character) => {
    const code = character.codePointAt(0) || 0
    return code < 32 && character !== '\n' && character !== '\r' && character !== '\t'
  }).length
  return controls === 0
}

function byteLength(value: string): number {
  return new TextEncoder().encode(value).byteLength
}

function formatText(value: string, contentType: string): {
  value: string
  format: InvocationArchivePayloadFormat
  canFormat: boolean
  transcript: InvocationArchiveTranscriptEntry[]
  transcriptTruncated: boolean
} {
  const json = parseJSON(value)
  if (json.ok && (isJSONContentType(contentType) || looksLikeJSON(value))) {
    const transcript = buildTranscript(json.value)
    return {
      value: JSON.stringify(json.value, null, 2),
      format: 'json',
      canFormat: true,
      transcript: transcript.entries,
      transcriptTruncated: transcript.truncated,
    }
  }
  if (isEventStream(contentType)) {
    const transcript = buildSSETranscript(value)
    return { value: formatSSE(value), format: 'sse', canFormat: true, transcript: transcript.entries, transcriptTruncated: transcript.truncated }
  }
  const lines = formatNDJSON(value)
  if (lines) {
    const transcript = buildNDJSONTranscript(value)
    return { value: lines, format: 'ndjson', canFormat: true, transcript: transcript.entries, transcriptTruncated: transcript.truncated }
  }
  if (isFormEncoded(contentType)) {
    return { value: formatForm(value), format: 'form', canFormat: true, transcript: [], transcriptTruncated: false }
  }
  return { value, format: 'text', canFormat: false, transcript: [], transcriptTruncated: false }
}

function parseJSON(value: string): { ok: true; value: unknown } | { ok: false } {
  try {
    return { ok: true, value: JSON.parse(value) }
  } catch {
    return { ok: false }
  }
}

function isJSONContentType(contentType: string): boolean {
  const type = contentType.split(';', 1)[0].trim().toLowerCase()
  return type === 'application/json' || type.endsWith('+json')
}

function looksLikeJSON(value: string): boolean {
  const trimmed = value.trim()
  return (trimmed.startsWith('{') && trimmed.endsWith('}')) || (trimmed.startsWith('[') && trimmed.endsWith(']'))
}

function isEventStream(contentType: string): boolean {
  return contentType.split(';', 1)[0].trim().toLowerCase() === 'text/event-stream'
}

function formatSSE(value: string): string {
  return value.replace(/\r\n/g, '\n').split('\n\n').map((block) => block.split('\n').map((line) => {
    if (!line.startsWith('data:')) return line
    const data = line.slice(5).trimStart()
    const parsed = parseJSON(data)
    if (!parsed.ok) return line
    return `data: ${JSON.stringify(parsed.value, null, 2).replace(/\n/g, '\n      ')}`
  }).join('\n')).join('\n\n')
}

function buildSSETranscript(value: string): { entries: InvocationArchiveTranscriptEntry[]; truncated: boolean } {
  const entries: InvocationArchiveTranscriptEntry[] = []
  let truncated = false
  const blocks = value.replace(/\r\n/g, '\n').split('\n\n')
  for (const block of blocks) {
    const lines = block.split('\n')
    const event = lines.find((line) => line.startsWith('event:'))?.slice(6).trim() || 'message'
    const data = lines.filter((line) => line.startsWith('data:')).map((line) => line.slice(5).trimStart()).join('\n')
    if (!data || data === '[DONE]') continue
    const parsed = parseJSON(data)
    const semantic = parsed.ok ? buildTranscript(parsed.value) : { entries: [], truncated: false }
    const sourceEntries = semantic.entries.length > 0
      ? semantic
      : { entries: [{ role: 'event', title: event, content: parsed.ok ? printable(parsed.value) : data, metadata: [] }], truncated: semantic.truncated }
    for (const entry of sourceEntries.entries) {
      if (entries.length >= TRANSCRIPT_ENTRY_LIMIT) return { entries, truncated: true }
      entries.push({ ...entry, metadata: [`event: ${event}`, ...entry.metadata] })
    }
    truncated ||= sourceEntries.truncated
    if (truncated) break
  }
  return { entries, truncated }
}

function formatNDJSON(value: string): string | undefined {
  const lines = value.replace(/\r\n/g, '\n').split('\n').filter((line) => line.trim() !== '')
  if (lines.length < 2) return undefined
  const parsed = lines.map((line) => parseJSON(line))
  if (parsed.some((item) => !item.ok)) return undefined
  return parsed.map((item) => JSON.stringify((item as { ok: true; value: unknown }).value, null, 2)).join('\n\n')
}

function buildNDJSONTranscript(value: string): { entries: InvocationArchiveTranscriptEntry[]; truncated: boolean } {
  const entries: InvocationArchiveTranscriptEntry[] = []
  const lines = value.replace(/\r\n/g, '\n').split('\n').filter((line) => line.trim() !== '')
  for (const line of lines) {
    const parsed = parseJSON(line)
    if (!parsed.ok) continue
    const semantic = buildTranscript(parsed.value)
    const sourceEntries = semantic.entries.length > 0
      ? semantic
      : { entries: [{ role: 'event', content: printable(parsed.value), metadata: [] }], truncated: semantic.truncated }
    for (const entry of sourceEntries.entries) {
      if (entries.length >= TRANSCRIPT_ENTRY_LIMIT) return { entries, truncated: true }
      entries.push(entry)
    }
    if (sourceEntries.truncated) return { entries, truncated: true }
  }
  return { entries, truncated: false }
}

function isFormEncoded(contentType: string): boolean {
  return contentType.split(';', 1)[0].trim().toLowerCase() === 'application/x-www-form-urlencoded'
}

function formatForm(value: string): string {
  return Array.from(new URLSearchParams(value).entries()).map(([key, item]) => `${key}: ${item}`).join('\n')
}

function buildTranscript(value: unknown): { entries: InvocationArchiveTranscriptEntry[]; truncated: boolean } {
  const items = transcriptItems(value)
  if (!items) return { entries: [], truncated: false }
  const entries: InvocationArchiveTranscriptEntry[] = []
  let truncated = false
  for (const item of items) {
    if (entries.length >= TRANSCRIPT_ENTRY_LIMIT) {
      truncated = true
      break
    }
    if (!isRecord(item)) {
      entries.push({ role: 'item', content: printable(item), metadata: [] })
      continue
    }
    if (isToolCallItem(item)) {
      entries.push(toolCallEntry(item))
      continue
    }
    if (isToolResultItem(item)) {
      entries.push(toolResultEntry(item))
      continue
    }
    const role = stringValue(item.role) || stringValue(item.type) || 'item'
    const metadata = metadataFor(item)
    const content = messageContent(item)
    const calls = Array.isArray(item.tool_calls) ? item.tool_calls : []
    if (content || calls.length === 0) entries.push({ role, content, metadata })
    for (const call of calls) {
      if (entries.length >= TRANSCRIPT_ENTRY_LIMIT) {
        truncated = true
        break
      }
      entries.push(toolCallEntry(call))
    }
    for (const part of toolParts(item)) {
      if (entries.length >= TRANSCRIPT_ENTRY_LIMIT) {
        truncated = true
        break
      }
      entries.push(isToolCallItem(part) ? toolCallEntry(part) : toolResultEntry(part))
    }
    if (truncated) break
  }
  // ponytail: structural review caps at 80 entries; raw and formatted text remain available for longer transcripts.
  return { entries, truncated }
}

function transcriptItems(value: unknown): unknown[] | undefined {
  if (Array.isArray(value)) return value
  if (!isRecord(value)) return undefined
  for (const key of ['messages', 'input', 'output']) {
    if (Array.isArray(value[key])) return value[key] as unknown[]
  }
  if (Array.isArray(value.choices)) return chatChoiceItems(value.choices)
  if (isRecord(value.item)) return [value.item]
  if (isRecord(value.response)) {
    for (const key of ['messages', 'input', 'output']) {
      if (Array.isArray(value.response[key])) return value.response[key] as unknown[]
    }
  }
  return typeof value.role === 'string' || typeof value.type === 'string' ? [value] : undefined
}

function chatChoiceItems(choices: unknown[]): Record<string, unknown>[] {
  return choices.map((choice, index) => {
    if (!isRecord(choice)) return { role: 'assistant', content: printable(choice), __archive_choice: index }
    const source = isRecord(choice.delta) ? choice.delta : isRecord(choice.message) ? choice.message : choice
    const item: Record<string, unknown> = { ...source, __archive_choice: choice.index ?? index }
    if (!stringValue(item.role) && !stringValue(item.type)) item.role = 'assistant'
    if (typeof choice.finish_reason === 'string' && choice.finish_reason) item.__archive_finish_reason = choice.finish_reason
    return item
  })
}

function toolCallEntry(value: unknown): InvocationArchiveTranscriptEntry {
  const call = isRecord(value) ? value : {}
  const fn = toolFunction(call)
  const title = stringValue(fn.name) || stringValue(call.name) || stringValue(call.type) || 'tool'
  const input = fn.arguments ?? fn.args ?? call.arguments ?? call.input ?? call.parameters ?? call
  const metadata = toolMetadata(call)
  return { role: 'tool_call', title, content: printable(input), metadata }
}

function toolResultEntry(value: unknown): InvocationArchiveTranscriptEntry {
  const result = isRecord(value) ? value : {}
  const fn = isRecord(result.functionResponse) ? result.functionResponse : result
  const title = stringValue(fn.name) || stringValue(result.name) || stringValue(result.type) || 'tool result'
  const output = firstDefined(result.output, result.content, result.result, result.response, result.tool_result, result.tool_output, fn.response, result)
  return { role: 'tool_result', title, content: printable(output), metadata: toolMetadata(result) }
}

function toolFunction(value: Record<string, unknown>): Record<string, unknown> {
  if (isRecord(value.function)) return value.function
  if (isRecord(value.functionCall)) return value.functionCall
  return value
}

function toolMetadata(value: Record<string, unknown>): string[] {
  return [
    stringValue(value.id) ? `id: ${stringValue(value.id)}` : '',
    stringValue(value.call_id) ? `call: ${stringValue(value.call_id)}` : '',
    stringValue(value.tool_call_id) ? `tool call: ${stringValue(value.tool_call_id)}` : '',
    stringValue(value.type) ? `type: ${stringValue(value.type)}` : '',
  ].filter(Boolean)
}

function isToolCallItem(value: Record<string, unknown>): boolean {
  const type = stringValue(value.type).toLowerCase()
  return type === 'tool_use' || type.endsWith('_call') || isRecord(value.functionCall)
}

function isToolResultItem(value: Record<string, unknown>): boolean {
  const type = stringValue(value.type).toLowerCase()
  return stringValue(value.role).toLowerCase() === 'tool' || type === 'tool_result' || type === 'tool_use_result'
    || type.endsWith('_call_output') || type.endsWith('_result') || isRecord(value.functionResponse)
}

function toolParts(value: Record<string, unknown>): Record<string, unknown>[] {
  const parts = [value.content, value.parts].filter(Array.isArray).flat() as unknown[]
  return parts.filter((part): part is Record<string, unknown> => isRecord(part) && (isToolCallItem(part) || isToolResultItem(part)))
}

function messageContent(value: Record<string, unknown>): string {
  for (const candidate of [value.content, value.text, value.reasoning_content, value.output_text, value.delta]) {
    if (candidate === undefined || candidate === null) continue
    if (typeof candidate === 'string') return printable(candidate)
    if (Array.isArray(candidate)) return candidate.map(contentPart).filter(Boolean).join('\n\n')
    return printable(candidate)
  }
  return ''
}

function contentPart(value: unknown): string {
  if (typeof value === 'string') return value
  if (!isRecord(value)) return printable(value)
  for (const key of ['text', 'content', 'value', 'input_text', 'output_text', 'output']) {
    if (typeof value[key] === 'string') return value[key] as string
  }
  return printable(value)
}

function metadataFor(value: Record<string, unknown>): string[] {
  return [
    stringValue(value.id) ? `id: ${stringValue(value.id)}` : '',
    stringValue(value.call_id) ? `call: ${stringValue(value.call_id)}` : '',
    stringValue(value.tool_call_id) ? `tool call: ${stringValue(value.tool_call_id)}` : '',
    stringValue(value.name) ? `name: ${stringValue(value.name)}` : '',
    stringValue(value.type) ? `type: ${stringValue(value.type)}` : '',
    typeof value.__archive_choice === 'number' ? `choice: ${value.__archive_choice}` : '',
    stringValue(value.__archive_finish_reason) ? `finish: ${stringValue(value.__archive_finish_reason)}` : '',
  ].filter(Boolean)
}

function buildFrameTranscript(
  payload: InvocationArchivePayloadView,
  charsetChoice: string,
): { entries: InvocationArchiveTranscriptEntry[]; truncated: boolean } {
  const frames = payload.frames
  if (!Array.isArray(frames) || frames.length === 0) return { entries: [], truncated: false }
  const source = payloadBytes(payload)
  if (!source) return { entries: [], truncated: false }
  const segmentOffset = payload.offset || 0
  const segmentEnd = segmentOffset + (payload.loaded_bytes ?? source.byteLength)
  const entries: InvocationArchiveTranscriptEntry[] = []
  let truncated = false
  for (const frame of frames) {
    if (entries.length >= TRANSCRIPT_ENTRY_LIMIT) return { entries, truncated: true }
    const frameStart = frame.offset
    const frameEnd = frameStart + frame.captured_bytes
    if (frameStart < segmentOffset || frameEnd > segmentEnd || frameEnd < frameStart) continue
    const framePayload = source.slice(frameStart - segmentOffset, frameEnd - segmentOffset)
    const metadata = frameMetadata(frame)
    const decoded = frameText(framePayload, payload.content_type, charsetChoice)
    if (!decoded) {
      entries.push({ role: 'websocket_frame', title: frame.kind || 'frame', content: encodeBase64(framePayload), metadata })
      continue
    }
    const formatted = formatText(decoded, payload.content_type)
    truncated ||= formatted.transcriptTruncated
    const sourceEntries = formatted.transcript
    if (sourceEntries.length === 0) {
      entries.push({ role: 'websocket_frame', title: frame.kind || 'frame', content: decoded, metadata })
      continue
    }
    for (const entry of sourceEntries) {
      if (entries.length >= TRANSCRIPT_ENTRY_LIMIT) return { entries, truncated: true }
      entries.push({ ...entry, metadata: [...metadata, ...entry.metadata] })
    }
  }
  return { entries, truncated }
}

function payloadBytes(payload: InvocationArchivePayloadView): Uint8Array | undefined {
  const raw = payload.data || ''
  if (payload.encoding?.toLowerCase() === 'base64') return decodeBase64(raw)
  return new TextEncoder().encode(raw)
}

function frameText(bytes: Uint8Array, contentType: string, charsetChoice: string): string | undefined {
  const decoded = decodeText(bytes, decoderCandidates(contentType, charsetChoice))
  return decoded && isLikelyText(decoded.value) ? decoded.value : undefined
}

function encodeBase64(bytes: Uint8Array): string {
  let binary = ''
  for (let index = 0; index < bytes.length; index += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(index, index + 0x8000))
  }
  return btoa(binary)
}

function frameMetadata(frame: InvocationArchivePayloadFrame): string[] {
  const bytes = frame.total_bytes > 0 ? `${frame.captured_bytes} / ${frame.total_bytes} bytes` : `${frame.captured_bytes} bytes`
  return [
    `frame: ${frame.sequence}`,
    `kind: ${frame.kind || 'text'}`,
    `offset: ${frame.offset}`,
    frame.occurred_at ? `at: ${frame.occurred_at}` : '',
    frame.truncated ? `truncated · ${bytes}` : bytes,
  ].filter(Boolean)
}

function firstDefined(...values: unknown[]): unknown {
  return values.find((value) => value !== undefined && value !== null)
}

function printable(value: unknown): string {
  if (typeof value === 'string') {
    const parsed = parseJSON(value)
    return parsed.ok ? JSON.stringify(parsed.value, null, 2) : value
  }
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function stringValue(value: unknown): string {
  return typeof value === 'string' ? value.trim() : ''
}

function recoverSingleByteMojibake(value: string): string | undefined {
  if (!/[\u00C2-\u00F0]/.test(value) || Array.from(value).some((character) => (character.codePointAt(0) || 0) > 255)) return undefined
  try {
    const bytes = Uint8Array.from(Array.from(value), (character) => character.charCodeAt(0))
    const repaired = new TextDecoder('utf-8', { fatal: true }).decode(bytes)
    return repaired !== value ? repaired : undefined
  } catch {
    return undefined
  }
}
