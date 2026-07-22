/**
 * A small, shared scheduler for account-usage requests.
 *
 * The account table can mount dozens of usage cells after a bulk import. Each
 * cell may need a local usage snapshot, and firing all of those requests at
 * once creates a browser-side request burst as well as a database/upstream
 * burst. Keep the work bounded and let an explicit row action overtake passive
 * background work.
 */

import type { Account } from '@/types'

export type UsageRequestPriority = 'background' | 'interactive'

export interface UsageRequestOptions {
  /** Explicit user actions run before passive row hydration work. */
  priority?: UsageRequestPriority
  /**
   * Lets a caller discard queued work when its table row was unmounted or
   * superseded by a newer request. It is checked immediately before dispatch.
   */
  shouldRun?: () => boolean
}

interface QueuedUsageRequest {
  run: () => Promise<unknown>
  resolve: (value: unknown) => void
  reject: (reason?: unknown) => void
  shouldRun?: () => boolean
}

export interface UsageRequestQueue {
  enqueue<T>(run: () => Promise<T>, options?: UsageRequestOptions): Promise<T>
}

const DEFAULT_USAGE_REQUEST_CONCURRENCY = 4

const cancelledUsageRequestError = () => {
  const error = new Error('Usage request was cancelled before dispatch')
  error.name = 'AbortError'
  return error
}

/** True for the intentional cancellation produced when stale queued work is skipped. */
export const isUsageRequestCancelled = (error: unknown): boolean => {
  if (!error || typeof error !== 'object') return false
  return (error as { name?: unknown }).name === 'AbortError'
}

const canRun = (request: QueuedUsageRequest): boolean => {
  if (!request.shouldRun) return true
  try {
    return request.shouldRun()
  } catch {
    return false
  }
}

/**
 * Creates a bounded two-lane request queue.
 *
 * Exported for focused tests; the application uses the shared queue below so
 * every account table page participates in the same concurrency budget.
 */
export function createUsageRequestQueue(maxConcurrent = DEFAULT_USAGE_REQUEST_CONCURRENCY): UsageRequestQueue {
  const concurrency = Math.max(1, Math.floor(maxConcurrent) || DEFAULT_USAGE_REQUEST_CONCURRENCY)
  const interactiveQueue: QueuedUsageRequest[] = []
  const backgroundQueue: QueuedUsageRequest[] = []
  let activeCount = 0

  const nextRequest = () => interactiveQueue.shift() ?? backgroundQueue.shift()

  const drain = () => {
    while (activeCount < concurrency) {
      const request = nextRequest()
      if (!request) return

      if (!canRun(request)) {
        request.reject(cancelledUsageRequestError())
        continue
      }

      activeCount += 1
      Promise.resolve()
        .then(() => {
          if (!canRun(request)) {
            throw cancelledUsageRequestError()
          }
          return request.run()
        })
        .then(request.resolve, request.reject)
        .finally(() => {
          activeCount -= 1
          drain()
        })
    }
  }

  return {
    enqueue<T>(run: () => Promise<T>, options: UsageRequestOptions = {}): Promise<T> {
      return new Promise<T>((resolve, reject) => {
        const request: QueuedUsageRequest = {
          run: () => run(),
          resolve: (value) => resolve(value as T),
          reject,
          shouldRun: options.shouldRun
        }

        if (options.priority === 'interactive') {
          interactiveQueue.push(request)
        } else {
          backgroundQueue.push(request)
        }
        drain()
      })
    }
  }
}

const sharedUsageRequestQueue = createUsageRequestQueue()

/**
 * Schedule a usage fetch in the shared bounded queue. The account parameter
 * remains part of the public call shape so existing cells retain their stable
 * invocation API and future per-account telemetry can be added without churn.
 */
export function enqueueUsageRequest<T>(
  _account: Account,
  fn: () => Promise<T>,
  options?: UsageRequestOptions
): Promise<T> {
  return sharedUsageRequestQueue.enqueue(fn, options)
}
