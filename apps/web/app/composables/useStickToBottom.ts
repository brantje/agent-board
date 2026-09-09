import { toValue, type MaybeRefOrGetter } from 'vue'

export type StickToBottomElement = Pick<HTMLElement, 'scrollTop' | 'clientHeight' | 'scrollHeight'>

export type StickToBottomTouchEvent = {
  touches: ArrayLike<{ clientY: number }>
  cancelable?: boolean
  preventDefault(): void
  stopPropagation(): void
}

export function isStickToBottom(el: StickToBottomElement, threshold = 32) {
  return el.scrollHeight - el.scrollTop - el.clientHeight <= threshold
}

export function useStickToBottom(element: MaybeRefOrGetter<StickToBottomElement | null | undefined>, threshold = 32) {
  let stick = true
  let touching = false
  let startY = 0

  function onScroll() {
    const el = toValue(element)
    if (!el) return
    stick = isStickToBottom(el, threshold)
  }

  function onTouchStart(event: Pick<StickToBottomTouchEvent, 'touches'>) {
    touching = true
    startY = event.touches[0]?.clientY ?? 0
    const el = toValue(element)
    if (!el) return
    const max = el.scrollHeight - el.clientHeight
    if (max <= 0) return
    if (el.scrollTop <= 0) el.scrollTop = 1
    else if (el.scrollTop >= max) el.scrollTop = max - 1
  }

  function onTouchMove(event: StickToBottomTouchEvent) {
    const el = toValue(element)
    if (!el) return
    const y = event.touches[0]?.clientY
    if (y == null) return
    const deltaY = y - startY
    startY = y
    if (el.scrollHeight <= el.clientHeight) return
    event.stopPropagation()
    const atTop = el.scrollTop <= 0
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight <= 0
    if ((atTop && deltaY > 0) || (atBottom && deltaY < 0)) {
      if (event.cancelable !== false) event.preventDefault()
    }
  }

  function onTouchEnd() {
    touching = false
    follow()
  }

  function follow() {
    const el = toValue(element)
    if (!el || !stick || touching) return
    if (el.scrollHeight - el.scrollTop - el.clientHeight <= 0) return
    el.scrollTop = el.scrollHeight
  }

  return { onScroll, onTouchStart, onTouchMove, onTouchEnd, follow }
}
