import { ExternalLinkIcon, Loader2Icon, LogInIcon, RefreshCwIcon, TriangleAlertIcon } from "lucide-react"
import { AnimatePresence, motion } from "motion/react"
import { useEffect, useRef, useState } from "react"
import { toast } from "sonner"

import { ConfirmAction } from "@/components/confirm-action"
import { Facts } from "@/components/facts"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button, buttonVariants } from "@/components/ui/button"
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field"
import { Separator } from "@/components/ui/separator"
import { Textarea } from "@/components/ui/textarea"
import { useAction } from "@/hooks/use-action"
import { api, type Account, type PendingLogin } from "@/lib/api"
import { formatDate } from "@/lib/format"

type AccountPanelProps = {
  account: Account | null
  login: PendingLogin | null
  refresh: () => Promise<void>
}

export function AccountPanel({ account, login, refresh }: AccountPanelProps) {
  const { busy, run } = useAction(refresh)
  const wasPending = useRef(login !== null)

  useEffect(() => {
    if (wasPending.current && login === null && account?.ready) {
      toast.success("Codex 계정을 연결했습니다.")
    }
    wasPending.current = login !== null
  }, [login, account])

  const start = () =>
    run("start", async () => {
      const res = await api.startLogin()
      window.open(res.login.url, "_blank", "noopener,noreferrer")
      await refresh()
    })

  return (
    <Card>
      <CardHeader>
        <CardTitle>Codex 계정</CardTitle>
        <CardDescription>게이트웨이는 이 계정으로 Codex에 요청을 보냅니다. 토큰은 서버 안에 암호화되어 저장됩니다.</CardDescription>
        {account && !login && (
          <CardAction>
            <Button variant="outline" size="sm" disabled={busy !== null} onClick={() => void start()}>
              <LogInIcon />
              다시 로그인
            </Button>
          </CardAction>
        )}
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {account ? (
          <>
            {account.status !== "active" && (
              <Alert variant="destructive">
                <TriangleAlertIcon />
                <AlertTitle>다시 로그인해야 합니다</AlertTitle>
                <AlertDescription>저장된 토큰을 더 쓸 수 없습니다. 다시 로그인할 때까지 Codex 요청이 거절됩니다.</AlertDescription>
              </Alert>
            )}
            {account.status === "active" && !account.account_id && (
              <Alert>
                <TriangleAlertIcon />
                <AlertTitle>ChatGPT 계정 ID가 없습니다</AlertTitle>
                <AlertDescription>로그인은 됐지만 Codex 요청에 필요한 계정 ID를 받지 못했습니다. 다시 로그인해 보세요.</AlertDescription>
              </Alert>
            )}
            <Facts
              items={[
                { term: "이메일", value: account.email || "없음" },
                { term: "플랜", value: account.plan_type || "없음" },
                { term: "계정 ID", value: <span className="font-mono text-xs">{account.account_id || "없음"}</span> },
                { term: "토큰 만료", value: formatDate(account.expires_at) },
              ]}
            />
            <div className="flex flex-wrap gap-2">
              <Button
                variant="outline"
                disabled={busy !== null}
                onClick={() =>
                  void run("refresh", async () => {
                    await api.refreshToken()
                    toast.success("토큰을 새로 받았습니다.")
                    await refresh()
                  })
                }
              >
                <RefreshCwIcon className={busy === "refresh" ? "animate-spin" : undefined} />
                토큰 새로 받기
              </Button>
              <ConfirmAction
                destructive
                label="연결 해제"
                title="Codex 계정 연결을 끊을까요?"
                description="저장된 OAuth 토큰을 지웁니다. API 키는 남지만, 다시 로그인할 때까지 모든 Codex 요청이 거절됩니다."
                confirmLabel="연결 해제"
                disabled={busy !== null}
                onConfirm={() =>
                  run("disconnect", async () => {
                    await api.disconnect()
                    toast.success("Codex 계정 연결을 끊었습니다.")
                    await refresh()
                  })
                }
              />
            </div>
          </>
        ) : (
          !login && (
            <div className="flex flex-col items-start gap-3 rounded-lg border border-dashed p-6">
              <p className="text-sm text-muted-foreground">
                아직 연결된 계정이 없습니다. 로그인하면 발급한 API 키로 Codex를 쓸 수 있습니다.
              </p>
              <Button disabled={busy !== null} onClick={() => void start()}>
                <LogInIcon />
                Codex 로그인 시작
              </Button>
            </div>
          )
        )}
        <AnimatePresence initial={false}>
          {login && (
            <motion.div
              key="login"
              initial={{ opacity: 0, height: 0 }}
              animate={{ opacity: 1, height: "auto" }}
              exit={{ opacity: 0, height: 0 }}
              transition={{ duration: 0.22, ease: "easeOut" }}
              className="overflow-hidden"
            >
              {account && <Separator className="mb-4" />}
              <LoginSteps login={login} refresh={refresh} />
            </motion.div>
          )}
        </AnimatePresence>
      </CardContent>
    </Card>
  )
}

function LoginSteps({ login, refresh }: { login: PendingLogin; refresh: () => Promise<void> }) {
  const { busy, run } = useAction(refresh)
  const [callback, setCallback] = useState("")

  const submit = () =>
    run("callback", async () => {
      await api.submitCallback(callback)
      setCallback("")
      await refresh()
    })

  return (
    <ol className="flex flex-col gap-5 text-sm">
      <li className="flex flex-col gap-2">
        <p className="font-medium">1. OpenAI 로그인 창에서 로그인합니다</p>
        <div className="flex flex-wrap items-center gap-2">
          <a
            className={buttonVariants({ variant: "default" })}
            href={login.url}
            target="_blank"
            rel="noopener noreferrer"
          >
            <ExternalLinkIcon />
            로그인 창 열기
          </a>
          <span className="text-muted-foreground">{formatDate(login.expires_at)}까지 유효합니다.</span>
        </div>
      </li>
      <li className="flex flex-col gap-2">
        <p className="font-medium">2. 끝나면 자동으로 연결됩니다</p>
        {login.callback ? (
          <p className="flex items-start gap-2 text-muted-foreground">
            <Loader2Icon className="mt-0.5 size-4 shrink-0 animate-spin" />
            <span>
              이 서버의 127.0.0.1:1455에서 로그인 완료를 기다리고 있습니다. 원격 서버라면 SSH 터널에
              <code className="mx-1 rounded bg-muted px-1 py-0.5 font-mono text-xs">-L 1455:127.0.0.1:1455</code>
              를 함께 걸어 두세요.
            </span>
          </p>
        ) : (
          <Alert>
            <TriangleAlertIcon />
            <AlertTitle>자동 연결을 쓸 수 없습니다</AlertTitle>
            <AlertDescription>{login.error || "1455 포트를 열지 못했습니다. 아래 방법으로 연결해 주세요."}</AlertDescription>
          </Alert>
        )}
      </li>
      <li className="flex flex-col gap-2">
        <Field>
          <FieldLabel htmlFor="callback-url">3. 자동으로 넘어오지 않으면 주소를 붙여 넣습니다</FieldLabel>
          <Textarea
            id="callback-url"
            className="font-mono text-xs"
            placeholder="http://localhost:1455/auth/callback?code=...&state=..."
            value={callback}
            onChange={(e) => setCallback(e.target.value)}
          />
          <FieldDescription>
            로그인 뒤 브라우저가 연결할 수 없는 페이지를 보여 주면, 그때 주소창에 있는 주소 전체를 복사해 넣으세요.
          </FieldDescription>
        </Field>
        <div className="flex flex-wrap gap-2">
          <Button disabled={busy !== null || callback.trim() === ""} onClick={() => void submit()}>
            {busy === "callback" ? "연결하는 중" : "이 주소로 연결"}
          </Button>
          <Button
            variant="ghost"
            disabled={busy !== null}
            onClick={() =>
              void run("cancel", async () => {
                await api.cancelLogin()
                await refresh()
              })
            }
          >
            로그인 취소
          </Button>
        </div>
      </li>
    </ol>
  )
}
