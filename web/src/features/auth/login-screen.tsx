import { useState, type FormEvent } from "react"

import { Button } from "@/components/ui/button"
import { Field, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { api, errorMessage, setToken } from "@/lib/api"

import { AuthLayout } from "./auth-layout"

export function LoginScreen({ onDone }: { onDone: () => Promise<void> }) {
  const [password, setPassword] = useState("")
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true)
    setError(null)
    try {
      const res = await api.login(password)
      setToken(res.token)
      await onDone()
    } catch (err) {
      setError(errorMessage(err))
      setPassword("")
    } finally {
      setBusy(false)
    }
  }

  return (
    <AuthLayout title="관리자 로그인" description="설치할 때 정한 관리자 비밀번호를 입력해 주세요.">
      <form onSubmit={(e) => void submit(e)}>
        <FieldGroup>
          <Field data-invalid={error ? true : undefined}>
            <FieldLabel htmlFor="password">비밀번호</FieldLabel>
            <Input
              id="password"
              type="password"
              autoComplete="current-password"
              required
              autoFocus
              aria-invalid={error ? true : undefined}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
            {error && <FieldError>{error}</FieldError>}
          </Field>
          <Button type="submit" size="lg" disabled={busy || password.length === 0}>
            {busy ? "확인하는 중" : "들어가기"}
          </Button>
        </FieldGroup>
      </form>
    </AuthLayout>
  )
}
