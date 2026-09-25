import type { ReactNode } from "react"

export type Fact = { term: string; value: ReactNode }

export function Facts({ items }: { items: Fact[] }) {
  return (
    <dl className="grid grid-cols-[7rem_1fr] gap-x-4 gap-y-2.5 text-sm">
      {items.map(({ term, value }) => (
        <div key={term} className="contents">
          <dt className="text-muted-foreground">{term}</dt>
          <dd className="min-w-0 break-all">{value}</dd>
        </div>
      ))}
    </dl>
  )
}
