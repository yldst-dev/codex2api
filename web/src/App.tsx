import { RefreshCwIcon } from "lucide-react"
import { AnimatePresence, MotionConfig, motion } from "motion/react"
import type { ReactNode } from "react"

import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { Toaster } from "@/components/ui/sonner"
import { LoginScreen } from "@/features/auth/login-screen"
import { SetupScreen } from "@/features/auth/setup-screen"
import { DashboardScreen } from "@/features/dashboard/dashboard-screen"
import { useAdminState } from "@/hooks/use-admin-state"
import { useColorScheme } from "@/hooks/use-color-scheme"

export default function App() {
  useColorScheme()
  const { state, error, refresh } = useAdminState()

  let key: string
  let screen: ReactNode
  if (!state) {
    key = error ? "offline" : "loading"
    screen = error ? <Offline message={error} onRetry={refresh} /> : <Loading />
  } else if (!state.authenticated) {
    key = state.setup_required ? "setup" : "login"
    screen = state.setup_required ? <SetupScreen onDone={refresh} /> : <LoginScreen onDone={refresh} />
  } else {
    key = "dashboard"
    screen = <DashboardScreen data={state} error={error} refresh={refresh} />
  }

  return (
    <MotionConfig reducedMotion="user">
      <AnimatePresence mode="wait">
        <motion.main
          key={key}
          initial={{ opacity: 0, y: 8 }}
          animate={{ opacity: 1, y: 0 }}
          exit={{ opacity: 0, y: -8 }}
          transition={{ duration: 0.2, ease: "easeOut" }}
        >
          {screen}
        </motion.main>
      </AnimatePresence>
      <Toaster position="bottom-right" />
    </MotionConfig>
  )
}

function Loading() {
  return (
    <div className="mx-auto flex w-full max-w-3xl flex-col gap-6 px-4 py-10" aria-busy="true">
      <Skeleton className="h-8 w-48" />
      <Skeleton className="h-48 w-full" />
      <Skeleton className="h-64 w-full" />
    </div>
  )
}

function Offline({ message, onRetry }: { message: string; onRetry: () => Promise<void> }) {
  return (
    <div className="flex min-h-svh flex-col items-center justify-center gap-4 px-4 text-center">
      <p className="max-w-sm text-sm text-muted-foreground">{message}</p>
      <Button variant="outline" onClick={() => void onRetry()}>
        <RefreshCwIcon />
        다시 시도
      </Button>
    </div>
  )
}
