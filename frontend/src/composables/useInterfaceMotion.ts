import { animate, stagger } from 'animejs'
import { nextTick, onBeforeUnmount, onMounted } from 'vue'

type AnimeInstance = ReturnType<typeof animate>

const PAGE_TARGET_SELECTOR = [
  '[data-motion-page]',
  'main > section',
  'main > article',
  'main > div:not(.fixed)',
].join(',')

export function prefersReducedMotion(): boolean {
  return typeof window !== 'undefined'
    && typeof window.matchMedia === 'function'
    && window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

export function collectMotionTargets(root: ParentNode = document): HTMLElement[] {
  const seen = new Set<HTMLElement>()
  for (const element of root.querySelectorAll<HTMLElement>(PAGE_TARGET_SELECTOR)) {
    if (element.closest('[aria-hidden="true"]')) continue
    seen.add(element)
    if (seen.size >= 24) break
  }
  return [...seen]
}

export function animatePageEntrance(root: ParentNode = document): AnimeInstance | null {
  if (prefersReducedMotion()) return null
  const targets = collectMotionTargets(root)
  if (targets.length === 0) return null

  targets.forEach((target) => {
    target.style.willChange = 'opacity, transform'
  })

  return animate(targets, {
    opacity: [0, 1],
    y: [10, 0],
    duration: 420,
    delay: stagger(26, { start: 20 }),
    ease: 'out(4)',
    onComplete: () => {
      targets.forEach((target) => {
        target.style.removeProperty('will-change')
      })
    },
  })
}

export function animateListEntrance(root: ParentNode | null): AnimeInstance | null {
  if (!root || prefersReducedMotion()) return null
  const targets = [...root.querySelectorAll<HTMLElement>('[data-motion-item]')].slice(0, 30)
  if (targets.length === 0) return null
  return animate(targets, {
    opacity: [0, 1],
    x: [-8, 0],
    duration: 320,
    delay: stagger(22),
    ease: 'out(3)',
  })
}

export function useInterfaceMotion() {
  let animation: AnimeInstance | null = null

  const run = async () => {
    await nextTick()
    animation?.revert()
    animation = animatePageEntrance()
  }

  onMounted(run)
  onBeforeUnmount(() => animation?.revert())
}

export function useAnimeDialogTransition() {
  const enter = (element: Element, done: () => void) => {
    if (prefersReducedMotion()) {
      done()
      return
    }
    const overlay = element as HTMLElement
    const panel = overlay.firstElementChild as HTMLElement | null
    animate(overlay, { opacity: [0, 1], duration: 180, ease: 'outQuad' })
    if (!panel) {
      done()
      return
    }
    animate(panel, {
      opacity: [0, 1],
      y: [14, 0],
      duration: 360,
      ease: 'out(4)',
      onComplete: done,
    })
  }

  const leave = (element: Element, done: () => void) => {
    if (prefersReducedMotion()) {
      done()
      return
    }
    const overlay = element as HTMLElement
    const panel = overlay.firstElementChild as HTMLElement | null
    animate(overlay, { opacity: [1, 0], duration: 180, ease: 'inQuad' })
    if (!panel) {
      done()
      return
    }
    animate(panel, {
      opacity: [1, 0],
      y: [0, 8],
      duration: 180,
      ease: 'inQuad',
      onComplete: done,
    })
  }

  return { enter, leave }
}
