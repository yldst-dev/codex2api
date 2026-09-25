import { useCallback, useEffect, useState } from "react"

import { api, errorMessage, type AdminState } from "@/lib/api"

export function useAdminState() {
  const [state, setState] = useState<AdminState | null>(null)
  const [error, setError] = useState<string | null>(null)

  const apply = useCallback((next: AdminState) => {
    setState(next)
    setError(null)
  }, [])

  const fail = useCallback((err: unknown) => setError(errorMessage(err)), [])

  const refresh = useCallback(() => api.state().then(apply, fail), [apply, fail])

  useEffect(() => {
    let active = true
    api.state().then(
      (next) => active && apply(next),
      (err) => active && fail(err),
    )
    return () => {
      active = false
    }
  }, [apply, fail])

  useEffect(() => {
    const onHash = () => void refresh()
    window.addEventListener("hashchange", onHash)
    return () => window.removeEventListener("hashchange", onHash)
  }, [refresh])

  const authenticated = state?.authenticated === true
  const pending =
    state?.authenticated === true &&
    (state.login !== null || ["installing", "restarting", "requested"].includes(state.update.status))

  useEffect(() => {
    if (!authenticated) return
    const id = window.setInterval(() => void refresh(), pending ? 2000 : 15000)
    return () => window.clearInterval(id)
  }, [authenticated, pending, refresh])

  return { state, error, refresh }
}
