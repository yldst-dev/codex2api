import { KeyRoundIcon, PlusIcon } from "lucide-react"
import { AnimatePresence, motion } from "motion/react"
import { useState, type FormEvent } from "react"
import { toast } from "sonner"

import { ConfirmAction } from "@/components/confirm-action"
import { CopyButton } from "@/components/copy-button"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { useAction } from "@/hooks/use-action"
import { api, type ApiKey, type CreatedKey } from "@/lib/api"
import { apiBaseURL, formatDate } from "@/lib/format"

const MotionRow = motion.create(TableRow)

type KeysPanelProps = {
  keys: ApiKey[]
  listen: string
  hasAccount: boolean
  refresh: () => Promise<void>
}

export function KeysPanel({ keys, listen, hasAccount, refresh }: KeysPanelProps) {
  const { busy, run } = useAction(refresh)
  const [name, setName] = useState("")
  const [revealed, setRevealed] = useState<CreatedKey | null>(null)

  const sorted = [...keys].sort((a, b) => {
    if (a.status !== b.status) return a.status === "active" ? -1 : 1
    return b.created_at.localeCompare(a.created_at)
  })

  const create = (event: FormEvent) => {
    event.preventDefault()
    void run("create", async () => {
      const created = await api.createKey(name.trim() || "default")
      setRevealed(created)
      setName("")
      await refresh()
    })
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>API 키</CardTitle>
        <CardDescription>
          클라이언트에는 OpenAI 토큰 대신 이 키를 줍니다. 요청 주소는{" "}
          <code className="rounded bg-muted px-1 py-0.5 font-mono text-xs">{apiBaseURL(listen)}</code> 입니다.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {!hasAccount && (
          <p className="text-sm text-muted-foreground">
            키는 지금 만들 수 있지만, Codex 계정을 연결하기 전까지 요청은 503으로 거절됩니다.
          </p>
        )}
        <form className="flex flex-col gap-2 sm:flex-row" onSubmit={create}>
          <Input
            aria-label="키 이름"
            placeholder="키 이름 (비우면 default)"
            maxLength={64}
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
          <Button type="submit" disabled={busy !== null}>
            <PlusIcon />
            키 만들기
          </Button>
        </form>

        <AnimatePresence initial={false}>
          {revealed && (
            <motion.div
              key={revealed.id + revealed.prefix}
              initial={{ opacity: 0, height: 0 }}
              animate={{ opacity: 1, height: "auto" }}
              exit={{ opacity: 0, height: 0 }}
              transition={{ duration: 0.22, ease: "easeOut" }}
              className="overflow-hidden"
            >
              <Alert>
                <KeyRoundIcon />
                <AlertTitle>{revealed.name} 키가 준비되었습니다</AlertTitle>
                <AlertDescription className="flex flex-col gap-3">
                  <span>이 값은 지금 한 번만 보입니다. 바로 안전한 곳에 저장해 주세요.</span>
                  <code className="block rounded-md bg-muted px-3 py-2 font-mono text-xs break-all text-foreground select-all">
                    {revealed.key}
                  </code>
                  <span className="flex flex-wrap gap-2">
                    <CopyButton value={revealed.key} label="키 복사" />
                    <Button variant="ghost" size="sm" onClick={() => setRevealed(null)}>
                      저장했습니다
                    </Button>
                  </span>
                </AlertDescription>
              </Alert>
            </motion.div>
          )}
        </AnimatePresence>

        {sorted.length === 0 ? (
          <p className="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">
            아직 만든 키가 없습니다.
          </p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>이름</TableHead>
                <TableHead>앞자리</TableHead>
                <TableHead>상태</TableHead>
                <TableHead>만든 날</TableHead>
                <TableHead>마지막 사용</TableHead>
                <TableHead className="text-right">관리</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <AnimatePresence initial={false}>
                {sorted.map((key) => (
                  <MotionRow
                    key={key.id}
                    layout
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.2 }}
                    className={key.status === "revoked" ? "text-muted-foreground" : undefined}
                  >
                    <TableCell className="font-medium">{key.name}</TableCell>
                    <TableCell className="font-mono text-xs">{key.prefix}</TableCell>
                    <TableCell>
                      {key.status === "active" ? (
                        <Badge variant="secondary">사용 중</Badge>
                      ) : (
                        <Badge variant="outline">폐기됨</Badge>
                      )}
                    </TableCell>
                    <TableCell>{formatDate(key.created_at)}</TableCell>
                    <TableCell>{formatDate(key.last_used_at)}</TableCell>
                    <TableCell className="text-right">
                      {key.status === "active" && (
                        <span className="inline-flex gap-1.5">
                          <ConfirmAction
                            size="sm"
                            label="재발급"
                            title={`${key.name} 키를 재발급할까요?`}
                            description="지금 쓰는 키는 바로 막히고 새 키가 한 번만 표시됩니다. 이 키를 쓰는 클라이언트 설정을 모두 바꿔야 합니다."
                            confirmLabel="재발급"
                            disabled={busy !== null}
                            onConfirm={() =>
                              run("rotate", async () => {
                                setRevealed(await api.rotateKey(key.id))
                                await refresh()
                              })
                            }
                          />
                          <ConfirmAction
                            size="sm"
                            destructive
                            label="폐기"
                            title={`${key.name} 키를 폐기할까요?`}
                            description="폐기한 키로 들어오는 요청은 바로 거절됩니다. 되돌릴 수 없습니다."
                            confirmLabel="폐기"
                            disabled={busy !== null}
                            onConfirm={() =>
                              run("revoke", async () => {
                                await api.revokeKey(key.id)
                                if (revealed?.id === key.id) setRevealed(null)
                                toast.success(`${key.name} 키를 폐기했습니다.`)
                                await refresh()
                              })
                            }
                          />
                        </span>
                      )}
                    </TableCell>
                  </MotionRow>
                ))}
              </AnimatePresence>
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
}
