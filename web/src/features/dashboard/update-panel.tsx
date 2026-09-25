import { ArrowUpCircleIcon, ExternalLinkIcon, Loader2Icon, RefreshCwIcon, TriangleAlertIcon } from "lucide-react"
import { AnimatePresence, motion } from "motion/react"
import type { ReactNode } from "react"
import { toast } from "sonner"

import { ConfirmAction } from "@/components/confirm-action"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Field, FieldContent, FieldDescription, FieldLabel } from "@/components/ui/field"
import { Switch } from "@/components/ui/switch"
import { useAction } from "@/hooks/use-action"
import { api, type UpdateState } from "@/lib/api"
import { formatDate } from "@/lib/format"

const busyStatuses = new Set(["installing", "restarting", "requested"])

export function UpdatePanel({ state, refresh }: { state: UpdateState; refresh: () => Promise<void> }) {
  const { busy, run } = useAction(refresh)
  const working = busyStatuses.has(state.status)
  const latest = state.latest

  return (
    <Card>
      <CardHeader>
        <CardTitle>업데이트</CardTitle>
        <CardDescription>GitHub 릴리스에서 새 버전을 받아 체크섬을 확인한 뒤 교체합니다.</CardDescription>
        <CardAction>
          <Badge variant="outline" className="font-mono">
            {state.current}
          </Badge>
        </CardAction>
      </CardHeader>
      <CardContent className="flex flex-col gap-5">
        <AnimatePresence mode="wait" initial={false}>
          <motion.div
            key={working ? state.status : state.available ? "available" : "current"}
            initial={{ opacity: 0, y: 6 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -6 }}
            transition={{ duration: 0.18, ease: "easeOut" }}
          >
            {working ? (
              <Progress status={state.status} tag={latest?.tag} />
            ) : state.available && latest ? (
              <Alert>
                <ArrowUpCircleIcon />
                <AlertTitle>{latest.tag} 버전이 나왔습니다</AlertTitle>
                <AlertDescription className="flex flex-col gap-3">
                  <span>
                    {formatDate(latest.published_at)}에 배포되었습니다.{" "}
                    <a className="underline underline-offset-4" href={latest.url} target="_blank" rel="noopener noreferrer">
                      바뀐 점 보기
                      <ExternalLinkIcon className="ml-1 inline size-3" />
                    </a>
                  </span>
                  {state.supported && (
                    <span>
                      <ConfirmAction
                        label="지금 업데이트"
                        title={`${latest.tag}로 업데이트할까요?`}
                        description="새 실행 파일로 바꾼 뒤 게이트웨이를 다시 시작합니다. 진행 중인 스트림은 끊길 수 있고, 끝나면 관리 화면에 다시 로그인해야 합니다."
                        confirmLabel="업데이트"
                        disabled={busy !== null}
                        onConfirm={() =>
                          run("apply", async () => {
                            await api.applyUpdate()
                            await refresh()
                          })
                        }
                      />
                    </span>
                  )}
                </AlertDescription>
              </Alert>
            ) : (
              <p className="text-sm text-muted-foreground">
                {latest ? "최신 버전을 쓰고 있습니다." : "아직 새 버전을 확인하지 않았습니다."}
                {state.checked_at && ` 마지막 확인 ${formatDate(state.checked_at)}.`}
              </p>
            )}
          </motion.div>
        </AnimatePresence>

        {state.status === "error" && state.error && (
          <Alert variant="destructive">
            <TriangleAlertIcon />
            <AlertTitle>업데이트하지 못했습니다</AlertTitle>
            <AlertDescription>{state.error}</AlertDescription>
          </Alert>
        )}
        {state.check_error && (
          <Alert variant="destructive">
            <TriangleAlertIcon />
            <AlertTitle>새 버전을 확인하지 못했습니다</AlertTitle>
            <AlertDescription>{state.check_error}</AlertDescription>
          </Alert>
        )}
        {!state.supported && <Unsupported state={state} />}

        <Field orientation="horizontal">
          <FieldContent>
            <FieldLabel htmlFor="auto-update">자동 업데이트</FieldLabel>
            <FieldDescription>6시간마다 확인해서 새 버전이 있으면 바로 설치합니다.</FieldDescription>
          </FieldContent>
          <Switch
            id="auto-update"
            checked={state.auto}
            disabled={busy !== null || !state.supported}
            onCheckedChange={(checked) =>
              void run("auto", async () => {
                await api.setAutoUpdate(checked)
                toast.success(checked ? "자동 업데이트를 켰습니다." : "자동 업데이트를 껐습니다.")
                await refresh()
              })
            }
          />
        </Field>

        <div>
          <Button
            variant="outline"
            disabled={busy !== null || working}
            onClick={() =>
              void run("check", async () => {
                const res = await api.checkUpdate()
                if (!res.update.available && res.update.latest) toast.success("최신 버전입니다.")
                await refresh()
              })
            }
          >
            <RefreshCwIcon className={busy === "check" ? "animate-spin" : undefined} />
            업데이트 확인
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}

function Progress({ status, tag }: { status: string; tag?: string }) {
  const text: Record<string, ReactNode> = {
    installing: `${tag ?? "새 버전"}을 받아 확인하고 있습니다.`,
    restarting: "새 버전으로 다시 시작하고 있습니다. 잠시 뒤 다시 로그인해 주세요.",
    requested: "서버의 업데이트 도우미에 요청했습니다. 새 버전으로 다시 시작되면 다시 로그인해 주세요.",
  }
  return (
    <p className="flex items-start gap-2 text-sm text-muted-foreground" role="status">
      <Loader2Icon className="mt-0.5 size-4 shrink-0 animate-spin" />
      <span>{text[status]}</span>
    </p>
  )
}

function Unsupported({ state }: { state: UpdateState }) {
  const dev = !/^v\d+\.\d+\.\d+$/.test(state.current)
  return (
    <Alert>
      <TriangleAlertIcon />
      <AlertTitle>{dev ? "개발 빌드입니다" : "웹에서 업데이트할 수 없습니다"}</AlertTitle>
      <AlertDescription>
        {dev
          ? "직접 빌드한 실행 파일이라 업데이트하지 않습니다. 새 버전 확인만 합니다."
          : "실행 파일 폴더에 쓸 권한이 없습니다. 설치 스크립트를 --service로 다시 실행하면 웹 업데이트가 켜집니다."}
      </AlertDescription>
    </Alert>
  )
}
