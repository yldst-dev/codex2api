import { useCallback, useState } from "react"
import { toast } from "sonner"

import { ApiError, errorMessage } from "@/lib/api"

export function useAction(refresh: () => Promise<void>) {
  const [busy, setBusy] = useState<string | null>(null)

  const run = useCallback(
    async (name: string, fn: () => Promise<void>) => {
      setBusy(name)
      try {
        await fn()
      } catch (err) {
        toast.error(errorMessage(err))
        if (err instanceof ApiError && err.status === 401) await refresh()
      } finally {
        setBusy(null)
      }
    },
    [refresh],
  )

  return { busy, run }
}
