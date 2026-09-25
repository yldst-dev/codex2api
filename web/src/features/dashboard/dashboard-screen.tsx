import { LogOutIcon, WifiOffIcon } from "lucide-react"
import { motion, type Variants } from "motion/react"

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { useAction } from "@/hooks/use-action"
import { api, setToken, type Dashboard } from "@/lib/api"

import { AccountPanel } from "./account-panel"
import { GatewayPanel } from "./gateway-panel"
import { KeysPanel } from "./keys-panel"
import { SecurityPanel } from "./security-panel"

const list: Variants = {
  show: { transition: { staggerChildren: 0.05 } },
}

const item: Variants = {
  hidden: { opacity: 0, y: 10 },
  show: { opacity: 1, y: 0, transition: { duration: 0.25, ease: "easeOut" } },
}

type DashboardScreenProps = {
  data: Dashboard
  error: string | null
  refresh: () => Promise<void>
}

export function DashboardScreen({ data, error, refresh }: DashboardScreenProps) {
  const { busy, run } = useAction(refresh)
  const account = data.account

  const logout = () =>
    run("logout", async () => {
      try {
        await api.logout()
      } finally {
        setToken(null)
        await refresh()
      }
    })

  return (
    <div className="mx-auto flex w-full max-w-3xl flex-col gap-6 px-4 py-10">
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
            <Badge variant="outline">키 {data.keys.filter((k) => k.status === "active").length}개</Badge>
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

      <motion.div className="flex flex-col gap-6" variants={list} initial="hidden" animate="show">
        <motion.div variants={item}>
          <AccountPanel account={account} login={data.login} refresh={refresh} />
        </motion.div>
        <motion.div variants={item}>
          <KeysPanel keys={data.keys} listen={data.gateway.listen} hasAccount={account !== null} refresh={refresh} />
        </motion.div>
        <motion.div variants={item}>
          <GatewayPanel gateway={data.gateway} dataDir={data.data_dir} refresh={refresh} />
        </motion.div>
        <motion.div variants={item}>
          <SecurityPanel adminListen={data.admin_listen} refresh={refresh} />
        </motion.div>
      </motion.div>
    </div>
  )
}
