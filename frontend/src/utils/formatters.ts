/**
 * 格式化缓存 token 数量（1K/1M 缩写）
 */
export function formatCacheTokens(tokens: number): string {
  if (tokens >= 1000000) return `${(tokens / 1000000).toFixed(1)}M`
  if (tokens >= 1000) return `${(tokens / 1000).toFixed(1)}K`
  return tokens.toLocaleString()
}

/**
 * 倍率精度约定：非零值最小为 0.000001，服务端以八位小数保存。
 */
export const MIN_RATE_MULTIPLIER = 0.000001
export const RATE_MULTIPLIER_STEP = '0.000001'
export const CURRENCY_STEP = '0.000001'

export function formatMultiplier(val: number | null | undefined): string {
  if (val === null || val === undefined || !Number.isFinite(val)) return '-'
  const absolute = Math.abs(val)
  const minimumFractionDigits = absolute >= 0.01
    ? 2
    : absolute >= 0.001
      ? 3
      : absolute >= 0.0001
        ? 4
        : 6
  const formatted = val.toFixed(8).replace(/\.?0+$/, '')
  if (!formatted.includes('.')) return `${formatted}.${'0'.repeat(minimumFractionDigits)}`
  const [integer, fraction = ''] = formatted.split('.')
  return `${integer}.${fraction.padEnd(minimumFractionDigits, '0')}`
}

export function parseRateMultiplier(
  value: string | number | null | undefined,
  allowZero = false
): number | null {
  if (value === null || value === undefined || String(value).trim() === '') return null
  const parsed = typeof value === 'number' ? value : Number(value)
  if (!Number.isFinite(parsed)) return null
  if (allowZero && parsed === 0) return 0
  return parsed >= MIN_RATE_MULTIPLIER ? parsed : null
}
