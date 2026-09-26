export async function copyText(text: string): Promise<boolean> {
  if (navigator.clipboard && window.isSecureContext) {
    try {
      await navigator.clipboard.writeText(text)
      return true
    } catch {
      return legacyCopy(text)
    }
  }
  return legacyCopy(text)
}

function legacyCopy(text: string): boolean {
  const area = document.createElement("textarea")
  area.value = text
  area.setAttribute("readonly", "")
  area.style.position = "fixed"
  area.style.top = "0"
  area.style.left = "0"
  area.style.width = "1px"
  area.style.height = "1px"
  area.style.opacity = "0"
  area.style.pointerEvents = "none"
  const active = document.activeElement as HTMLElement | null
  document.body.appendChild(area)
  area.focus({ preventScroll: true })
  area.select()
  area.setSelectionRange(0, text.length)
  let ok = false
  try {
    ok = document.execCommand("copy")
  } catch {
    ok = false
  }
  area.remove()
  active?.focus({ preventScroll: true })
  return ok
}

export function selectContents(element: HTMLElement | null) {
  if (!element) return
  const range = document.createRange()
  range.selectNodeContents(element)
  const selection = window.getSelection()
  selection?.removeAllRanges()
  selection?.addRange(range)
}
