import { useState, type FormEvent } from "react"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { useAction } from "@/hooks/use-action"
import { api, setToken } from "@/lib/api"

export function SecurityPanel({ adminListen, refresh }: { adminListen: string; refresh: () => Promise<void> }) {
  const { busy, run } = useAction(refresh)
  const [current, setCurrent] = useState("")
  const [next, setNext] = useState("")
  const [confirm, setConfirm] = useState("")
  const mismatch = confirm.length > 0 && next !== confirm

  const submit = (event: FormEvent) => {
    event.preventDefault()
    void run("password", async () => {
      const res = await api.changePassword(current, next)
      setToken(res.token)
      setCurrent("")
      setNext("")
      setConfirm("")
      toast.success("관리자 비밀번호를 바꿨습니다. 다른 곳에 열린 관리 화면은 로그아웃됩니다.")
    })
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>관리자</CardTitle>
        <CardDescription>
          관리 화면은 <span className="font-mono text-xs">{adminListen}</span>에서만 열립니다. 원격 서버라면 SSH 터널로 접속하세요.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={submit}>
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="current-password">현재 비밀번호</FieldLabel>
              <Input
                id="current-password"
                type="password"
                autoComplete="current-password"
                required
                value={current}
                onChange={(e) => setCurrent(e.target.value)}
              />
            </Field>
            <div className="grid gap-4 sm:grid-cols-2">
              <Field>
                <FieldLabel htmlFor="next-password">새 비밀번호</FieldLabel>
                <Input
                  id="next-password"
                  type="password"
                  autoComplete="new-password"
                  minLength={8}
                  required
                  value={next}
                  onChange={(e) => setNext(e.target.value)}
                />
              </Field>
              <Field data-invalid={mismatch || undefined}>
                <FieldLabel htmlFor="next-password-confirm">한 번 더 입력</FieldLabel>
                <Input
                  id="next-password-confirm"
                  type="password"
                  autoComplete="new-password"
                  required
                  aria-invalid={mismatch || undefined}
                  value={confirm}
                  onChange={(e) => setConfirm(e.target.value)}
                />
                {mismatch && <FieldError>두 비밀번호가 다릅니다.</FieldError>}
              </Field>
            </div>
            <FieldDescription>
              비밀번호를 잊었다면 서버에서 codex-gateway admin reset-password 를 실행해 새 설정 링크를 받으세요.
            </FieldDescription>
            <div>
              <Button
                type="submit"
                variant="outline"
                disabled={busy !== null || mismatch || next.length < 8 || current === ""}
              >
                비밀번호 바꾸기
              </Button>
            </div>
          </FieldGroup>
        </form>
      </CardContent>
    </Card>
  )
}
