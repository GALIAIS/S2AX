import { describe, expect, it } from 'vitest'
import { createUsageRequestQueue } from '../usageLoadQueue'

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

async function flushQueue() {
  for (let index = 0; index < 6; index += 1) {
    await Promise.resolve()
  }
}

describe('usageLoadQueue', () => {
  it('bounds passive account usage requests', async () => {
    const queue = createUsageRequestQueue(2)
    const gates = Array.from({ length: 5 }, () => deferred<number>())
    const started: number[] = []
    let active = 0
    let maxActive = 0

    const requests = gates.map((gate, index) =>
      queue.enqueue(async () => {
        started.push(index)
        active += 1
        maxActive = Math.max(maxActive, active)
        try {
          return await gate.promise
        } finally {
          active -= 1
        }
      })
    )

    await flushQueue()
    expect(started).toEqual([0, 1])
    expect(maxActive).toBe(2)

    gates[0].resolve(0)
    await flushQueue()
    expect(started).toEqual([0, 1, 2])
    expect(maxActive).toBe(2)

    gates[1].resolve(1)
    gates[2].resolve(2)
    await flushQueue()
    gates[3].resolve(3)
    gates[4].resolve(4)
    await expect(Promise.all(requests)).resolves.toEqual([0, 1, 2, 3, 4])
  })

  it('lets an explicit refresh overtake queued passive work', async () => {
    const queue = createUsageRequestQueue(1)
    const first = deferred<string>()
    const passive = deferred<string>()
    const interactive = deferred<string>()
    const started: string[] = []

    const firstRequest = queue.enqueue(async () => {
      started.push('first')
      return first.promise
    })
    const passiveRequest = queue.enqueue(async () => {
      started.push('passive')
      return passive.promise
    })
    const interactiveRequest = queue.enqueue(
      async () => {
        started.push('interactive')
        return interactive.promise
      },
      { priority: 'interactive' }
    )

    await flushQueue()
    expect(started).toEqual(['first'])

    first.resolve('first')
    await flushQueue()
    expect(started).toEqual(['first', 'interactive'])

    interactive.resolve('interactive')
    await flushQueue()
    expect(started).toEqual(['first', 'interactive', 'passive'])

    passive.resolve('passive')
    await expect(Promise.all([firstRequest, passiveRequest, interactiveRequest])).resolves.toEqual([
      'first',
      'passive',
      'interactive'
    ])
  })
})
