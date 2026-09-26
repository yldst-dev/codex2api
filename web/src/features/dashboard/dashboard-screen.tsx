import {
  ArrowUpCircleIcon,
  KeyRoundIcon,
  LogOutIcon,
  ServerIcon,
  ShieldIcon,
  UserRoundIcon,
  WifiOffIcon,
  type LucideIcon,
} from "lucide-react"
import { motion } from "motion/react"
import { useState, type ReactNode } from "react"

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useAction } from "@/hooks/use-action"
import { useMediaQuery } from "@/hooks/use-media-query"
import { api, setToken, type Dashboard } from "@/lib/api"
import { cn } from "@/lib/utils"

import { AccountPanel } from "./account-panel"
import { GatewayPanel } from "./gateway-panel"
import { KeysPanel } from "./keys-panel"
import { SecurityPanel } from "./security-panel"
import { UpdatePanel } from "./update-panel"

type TabId = "account" | "keys" | "gateway" | "update" | "admin"

const tabKey = "codex-gateway-tab"
const tabIds: TabId[] = ["account", "keys", "gateway", "update", "admin"]

function readTab(): TabId {
  try {
    const saved = sessionStorage.getItem(tabKey)
    return tabIds.includes(saved as TabId) ? (saved as TabId) : "account"
  } catch {
    return "account"
  }
}

function saveTab(tab: TabId) {
  try {
    sessionStorage.setItem(tabKey, tab)
  } catch {
    return
  }
}

type DashboardScreenProps = {
  data: Dashboard
  error: string | null
  refresh: () => Promise<void>
}

export function DashboardScreen({ data, error, refresh }: DashboardScreenProps) {
  const { busy, run } = useAction(refresh)
  const [tab, setTab] = useState<TabId>(readTab)
  const wide = useMediaQuery("(min-width: 768px)")
  const account = data.account
  const activeKeys = data.keys.filter((k) => k.status === "active").length

  const select = (value: TabId) => {
    setTab(value)
    saveTab(value)
  }

  const logout = () =>
    run("logout", async () => {
      try {
        await api.logout()
      } finally {
        setToken(null)
        await refresh()
      }
    })

  const accountAlert = !account || account.status !== "active"
  const tabs: { id: TabId; label: string; icon: LucideIcon; mark?: ReactNode }[] = [
    { id: "account", label: "Codex 계정", icon: UserRoundIcon, mark: accountAlert && <Dot tone="danger" label="확인 필요" /> },
    { id: "keys", label: "API 키", icon: KeyRoundIcon, mark: <Count value={activeKeys} /> },
    { id: "gateway", label: "게이트웨이", icon: ServerIcon, mark: !data.gateway.running && <Dot tone="danger" label="멈춤" /> },
    { id: "update", label: "업데이트", icon: ArrowUpCircleIcon, mark: data.update.available && <Dot tone="accent" label="새 버전 있음" /> },
    { id: "admin", label: "관리자", icon: ShieldIcon },
  ]

  return (
    <div className="mx-auto flex w-full max-w-5xl flex-col gap-6 px-4 py-8 md:py-10">
      <header className="flex items-start justify-between gap-4">
        <div className="flex flex-col gap-2">
          <h1 className="font-heading text-2xl font-semibold tracking-tight">Codex Gateway</h1>
          <div className="flex flex-wrap gap-1.5">
            {!account ? (
              <Badge variant="outline">Codex 연결 안 됨</Badge>
            ) : account.status === "active" ? (
              <Badge variant="secondary">{account.email || "Codex 연결됨"}</Badge>
            ) : (
              <Badge variant="destructive">다시 로그인 필요</Badge>
            )}
            {data.gateway.running ? (
              <Badge variant="secondary">
                실행 중 <span className="font-mono">{data.gateway.listen}</span>
              </Badge>
            ) : (
              <Badge variant="destructive">게이트웨이 멈춤</Badge>
            )}
            <Badge variant="outline" className="font-mono">
              {data.update.current}
            </Badge>
          </div>
        </div>
        <Button variant="ghost" size="sm" disabled={busy !== null} onClick={() => void logout()}>
          <LogOutIcon />
          로그아웃
        </Button>
      </header>

      {error && (
        <Alert variant="destructive">
          <WifiOffIcon />
          <AlertTitle>최신 상태를 받지 못했습니다</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      <Tabs
        value={tab}
        onValueChange={(value) => select(value as TabId)}
        orientation={wide ? "vertical" : "horizontal"}
        className="gap-6 md:flex-row md:items-start"
      >
        <TabsList
          variant="line"
          aria-label="관리 메뉴"
          className={cn(
            "shrink-0",
            wide
              ? "sticky top-6 w-52 items-stretch gap-0.5"
              : "-mx-4 w-[calc(100%+2rem)] justify-start overflow-x-auto px-4 [scrollbar-width:none]",
          )}
        >
          {tabs.map(({ id, label, icon: Icon, mark }) => (
            <TabsTrigger key={id} value={id} className={cn("gap-2", wide ? "h-9 px-3" : "flex-none px-2.5")}>
              <Icon />
              <span>{label}</span>
              {mark && <span className={wide ? "ml-auto" : undefined}>{mark}</span>}
            </TabsTrigger>
          ))}
        </TabsList>

        <div className="min-w-0 flex-1">
          <Panel value="account">
            <AccountPanel account={account} login={data.login} refresh={refresh} />
          </Panel>
          <Panel value="keys">
            <KeysPanel keys={data.keys} listen={data.gateway.listen} hasAccount={account !== null} refresh={refresh} />
          </Panel>
          <Panel value="gateway">
            <GatewayPanel gateway={data.gateway} dataDir={data.data_dir} refresh={refresh} />
          </Panel>
          <Panel value="update">
            <UpdatePanel state={data.update} refresh={refresh} />
          </Panel>
          <Panel value="admin">
            <SecurityPanel adminListen={data.admin_listen} adminAllow={data.admin_allow} refresh={refresh} />
          </Panel>
        </div>
      </Tabs>
    </div>
  )
}

function Panel({ value, children }: { value: TabId; children: ReactNode }) {
  return (
    <TabsContent value={value}>
      <motion.div
        initial={{ opacity: 0, y: 8 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.2, ease: "easeOut" }}
      >
        {children}
      </motion.div>
    </TabsContent>
  )
}

function Dot({ tone, label }: { tone: "danger" | "accent"; label: string }) {
  return (
    <>
      <span
        aria-hidden
        className={cn("block size-1.5 rounded-full", tone === "danger" ? "bg-destructive" : "bg-primary")}
      />
      <span className="sr-only">{label}</span>
    </>
  )
}

function Count({ value }: { value: number }) {
  return <span className="text-xs text-muted-foreground tabular-nums">{value}</span>
}
