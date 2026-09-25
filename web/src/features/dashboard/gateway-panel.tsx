import { LockIcon, TriangleAlertIcon } from "lucide-react"
import { useState, type FormEvent } from "react"
import { toast } from "sonner"

import { Facts } from "@/components/facts"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { useAction } from "@/hooks/use-action"
import { api, type GatewayState } from "@/lib/api"
import { apiBaseURL, isLoopbackListen } from "@/lib/format"

type GatewayPanelProps = {
  gateway: GatewayState
  dataDir: string
  refresh: () => Promise<void>
}

export function GatewayPanel({ gateway, dataDir, refresh }: GatewayPanelProps) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>게이트웨이</CardTitle>
        <CardDescription>API 요청을 받는 주소입니다. 바꾸면 재시작 없이 바로 새 주소로 옮겨 갑니다.</CardDescription>
        <CardAction>
          {gateway.running ? <Badge variant="secondary">실행 중</Badge> : <Badge variant="destructive">멈춤</Badge>}
        </CardAction>
      </CardHeader>
      <CardContent className="flex flex-col gap-5">
        <Facts
          items={[
            { term: "수신 주소", value: <span className="font-mono text-xs">{gateway.listen}</span> },
            { term: "요청 주소", value: <span className="font-mono text-xs">{apiBaseURL(gateway.listen)}</span> },
            { term: "데이터 폴더", value: <span className="font-mono text-xs">{dataDir}</span> },
          ]}
        />
        {gateway.locked ? (
          <Alert>
            <LockIcon />
            <AlertTitle>환경 변수로 고정되어 있습니다</AlertTitle>
            <AlertDescription>
              CODEX_GATEWAY_LISTEN 값이 있어서 여기서는 바꿀 수 없습니다. 서비스 설정에서 그 값을 지우고 한 번 재시작하면 이 화면에서 바꿀 수 있습니다.
            </AlertDescription>
          </Alert>
        ) : (
          <ListenForm key={gateway.listen} current={gateway.listen} running={gateway.running} refresh={refresh} />
        )}
      </CardContent>
    </Card>
  )
}

function ListenForm({
  current,
  running,
  refresh,
}: {
  current: string
  running: boolean
  refresh: () => Promise<void>
}) {
  const { busy, run } = useAction(refresh)
  const [value, setValue] = useState(current)
  const next = value.trim()
  const exposed = next !== "" && !isLoopbackListen(next)

  const submit = (event: FormEvent) => {
    event.preventDefault()
    void run("listen", async () => {
      const res = await api.setListen(next)
      toast.success(`${res.listen}에서 요청을 받습니다.`)
      await refresh()
    })
  }

  return (
    <form className="flex flex-col gap-3" onSubmit={submit}>
      <Field>
        <FieldLabel htmlFor="listen">수신 주소</FieldLabel>
        <div className="flex flex-col gap-2 sm:flex-row">
          <Input
            id="listen"
            className="font-mono"
            placeholder="127.0.0.1:8080"
            value={value}
            onChange={(e) => setValue(e.target.value)}
          />
          <Button type="submit" disabled={busy !== null || next === "" || (next === current && running)}>
            {running ? "적용" : "이 주소로 시작"}
          </Button>
        </div>
        <FieldDescription>
          이 서버 안에서만 쓰려면 127.0.0.1, 다른 기기에서도 쓰려면 0.0.0.0을 적습니다.
        </FieldDescription>
      </Field>
      {exposed && (
        <Alert>
          <TriangleAlertIcon />
          <AlertTitle>외부에서 접속할 수 있게 됩니다</AlertTitle>
          <AlertDescription>
            API 키가 있어야 요청이 통과하지만 통신은 암호화되지 않습니다. 인터넷에 내놓을 때는 HTTPS 프록시와 방화벽을 함께 두세요.
          </AlertDescription>
        </Alert>
      )}
    </form>
  )
}
