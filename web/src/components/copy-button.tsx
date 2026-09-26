import { CheckIcon, CopyIcon } from "lucide-react"
import { AnimatePresence, motion } from "motion/react"
import { useEffect, useState, type RefObject } from "react"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import { copyText, selectContents } from "@/lib/clipboard"

type CopyButtonProps = {
  value: string
  label?: string
  selectTarget?: RefObject<HTMLElement | null>
}

const shortcut = /Mac|iPhone|iPad/.test(navigator.platform) ? "⌘C" : "Ctrl+C"

export function CopyButton({ value, label = "복사", selectTarget }: CopyButtonProps) {
  const [copied, setCopied] = useState(false)

  useEffect(() => {
    if (!copied) return
    const id = window.setTimeout(() => setCopied(false), 1600)
    return () => window.clearTimeout(id)
  }, [copied])

  const copy = async () => {
    if (await copyText(value)) {
      setCopied(true)
      return
    }
    selectContents(selectTarget?.current ?? null)
    toast.error(`자동 복사가 막혀 있어 값을 선택해 두었습니다. ${shortcut}로 복사해 주세요.`)
  }

  return (
    <Button variant="outline" size="sm" onClick={() => void copy()} aria-label={label}>
      <span className="relative size-3.5">
        <AnimatePresence initial={false} mode="popLayout">
          <motion.span
            key={copied ? "done" : "copy"}
            className="absolute inset-0"
            initial={{ opacity: 0, scale: 0.6 }}
            animate={{ opacity: 1, scale: 1 }}
            exit={{ opacity: 0, scale: 0.6 }}
            transition={{ duration: 0.15 }}
          >
            {copied ? <CheckIcon className="size-3.5" /> : <CopyIcon className="size-3.5" />}
          </motion.span>
        </AnimatePresence>
      </span>
      {copied ? "복사됨" : label}
    </Button>
  )
}
