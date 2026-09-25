const dateTime = new Intl.DateTimeFormat("ko-KR", {
  dateStyle: "medium",
  timeStyle: "short",
})

export function formatDate(value?: string): string {
  if (!value) return "없음"
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : dateTime.format(date)
}

function splitHostPort(listen: string): { host: string; port: string } {
  const idx = listen.lastIndexOf(":")
  if (idx < 0) return { host: listen, port: "" }
  return { host: listen.slice(0, idx).replace(/^\[|\]$/g, ""), port: listen.slice(idx + 1) }
}

export function isWildcardHost(listen: string): boolean {
  const { host } = splitHostPort(listen)
  return host === "0.0.0.0" || host === "::"
}

export function isLoopbackListen(listen: string): boolean {
  const { host } = splitHostPort(listen)
  return host === "localhost" || host === "::1" || host.startsWith("127.")
}

export function apiBaseURL(listen: string): string {
  const { host, port } = splitHostPort(listen)
  const shown = isWildcardHost(listen) ? "서버-주소" : host.includes(":") ? `[${host}]` : host
  return `http://${shown}:${port}/v1`
}
