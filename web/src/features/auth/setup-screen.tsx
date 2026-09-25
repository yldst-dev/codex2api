import { useState, type FormEvent } from "react"
import { toast } from "sonner"

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { api, errorMessage, setToken } from "@/lib/api"

import { AuthLayout } from "./auth-layout"

function readSetupToken(): string {
  return new URLSearchParams(window.location.hash.slice(1)).get("setup") ?? ""
}

export function SetupScreen({ onDone }: { onDone: () => Promise<void> }) {
  const [token] = useState(readSetupToken)
  const [password, setPassword] = useState("")
  const [confirm, setConfirm] = useState("")
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  if (!token) {
    return (
      <AuthLayout title="설정 링크로 들어와 주세요" description="처음 한 번은 설치할 때 받은 링크가 있어야 관리자 비밀번호를 정할 수 있습니다.">
        <Alert>
          <AlertTitle>링크를 잃어버렸다면</AlertTitle>
          <AlertDescription>
            서버 데이터 폴더의 setup.token 파일 값을 이 주소 뒤에 #setup=값 형태로 붙여서 열면 됩니다.
          </AlertDescription>
        </Alert>
      </AuthLayout>
    )
  }

  const mismatch = confirm.length > 0 && password !== confirm

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (password !== confirm) return
    setBusy(true)
    setError(null)
    try {
      const res = await api.setup(token, password)
      setToken(res.token)
      window.history.replaceState(null, "", window.location.pathname)
      toast.success("관리자 비밀번호를 정했습니다.")
      await onDone()
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <AuthLayout title="관리자 비밀번호 정하기" description="이 비밀번호로 앞으로 관리 화면에 들어옵니다. 설정 링크는 한 번만 쓸 수 있습니다.">
      <form onSubmit={(e) => void submit(e)}>
        <FieldGroup>
          <Field>
            <FieldLabel htmlFor="new-password">새 비밀번호</FieldLabel>
            <Input
              id="new-password"
              type="password"
              autoComplete="new-password"
              minLength={8}
              required
              autoFocus
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
            <FieldDescription>8자 이상으로 정해 주세요.</FieldDescription>
          </Field>
          <Field data-invalid={mismatch || undefined}>
            <FieldLabel htmlFor="confirm-password">한 번 더 입력</FieldLabel>
            <Input
              id="confirm-password"
              type="password"
              autoComplete="new-password"
              required
              aria-invalid={mismatch || undefined}
              value={confirm}
              onChange={(e) => setConfirm(e.target.value)}
            />
            {mismatch && <FieldError>두 비밀번호가 다릅니다.</FieldError>}
          </Field>
          {error && <FieldError>{error}</FieldError>}
          <Button type="submit" size="lg" disabled={busy || mismatch || password.length < 8}>
            {busy ? "저장하는 중" : "비밀번호 저장"}
          </Button>
        </FieldGroup>
      </form>
    </AuthLayout>
  )
}
