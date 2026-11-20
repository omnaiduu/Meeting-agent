import { Check, X } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { statusLabel } from '@/lib/format'
import { cn } from '@/lib/utils'
import type { Status } from '@/types'

type Tone = 'neutral' | 'blue' | 'green' | 'violet' | 'red'

const tone: Record<Status, Tone> = {
  queued: 'neutral', launching: 'blue', joining: 'blue', in_lobby: 'blue', in_call: 'green',
  leaving: 'violet', transcribing: 'violet', summarizing: 'violet', done: 'green', failed: 'red',
}

const dot: Record<Tone, string> = {
  neutral: 'bg-muted-foreground', blue: 'bg-sky-500', green: 'bg-emerald-500', violet: 'bg-violet-500', red: 'bg-red-500',
}

export function StatusBadge({ status, className }: { status: Status; className?: string }) {
  const t = tone[status]
  const live = !['queued', 'done', 'failed'].includes(status)
  return (
    <Badge tone={t} className={className}>
      {status === 'done' ? (
        <Check />
      ) : status === 'failed' ? (
        <X />
      ) : (
        <span className="relative flex size-2">
          {live && <span className={cn('absolute inline-flex size-full animate-ping rounded-full opacity-60', dot[t])} />}
          <span className={cn('relative inline-flex size-2 rounded-full', dot[t])} />
        </span>
      )}
      {statusLabel[status]}
    </Badge>
  )
}
