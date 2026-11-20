import { Check, X } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { Status } from '@/types'

const steps: { label: string; statuses: Status[] }[] = [
  { label: 'Queued', statuses: ['queued', 'launching'] },
  { label: 'Joining', statuses: ['joining'] },
  { label: 'Lobby', statuses: ['in_lobby'] },
  { label: 'In call', statuses: ['in_call', 'leaving'] },
  { label: 'Summarizing', statuses: ['transcribing', 'summarizing'] },
  { label: 'Done', statuses: ['done'] },
]

/** Horizontal progress through the bot's life. On failure, the step it failed at turns red. */
export function StatusStepper({ status, failedAt }: { status: Status; failedAt?: Status }) {
  const current = status === 'failed' ? steps.findIndex((s) => s.statuses.includes(failedAt ?? 'queued')) : steps.findIndex((s) => s.statuses.includes(status))
  const label = status === 'failed' ? `Failed at: ${steps[current]?.label ?? 'start'}` : steps[Math.max(current, 0)]?.label
  return (
    <>
    {/* Phones: one line of text plus a segmented bar. */}
    <div className="sm:hidden" aria-label="Progress">
      <div className="flex items-center justify-between text-xs">
        <span className={cn('font-medium', status === 'failed' && 'text-red-600 dark:text-red-400')}>{label}</span>
        <span className="text-muted-foreground">Step {Math.min(current + 1, steps.length)} of {steps.length}</span>
      </div>
      <div className="mt-2 flex gap-1">
        {steps.map((s, i) => (
          <span
            key={s.label}
            className={cn(
              'h-1.5 flex-1 rounded-full bg-border',
              (i < current || status === 'done') && 'bg-primary',
              i === current && status !== 'done' && (status === 'failed' ? 'bg-red-500' : 'animate-pulse bg-primary/60'),
            )}
          />
        ))}
      </div>
    </div>
    <ol className="hidden w-full items-center sm:flex" aria-label="Progress">
      {steps.map((s, i) => {
        const done = i < current || status === 'done'
        const active = i === current && status !== 'done'
        const failed = status === 'failed' && i === current
        return (
          <li key={s.label} className={cn('flex items-center', i < steps.length - 1 && 'flex-1')}>
            <div className="flex items-center gap-2">
              <span
                className={cn(
                  'flex size-6 shrink-0 items-center justify-center rounded-full border text-[11px] font-semibold transition-colors',
                  done && 'border-primary bg-primary text-primary-foreground',
                  active && !failed && 'border-primary text-primary ring-4 ring-primary/15',
                  failed && 'border-red-500 bg-red-500 text-white',
                  !done && !active && 'text-muted-foreground',
                )}
                aria-current={active ? 'step' : undefined}
              >
                {done ? <Check className="size-3.5" /> : failed ? <X className="size-3.5" /> : i + 1}
              </span>
              <span className={cn('text-xs whitespace-nowrap', active || done ? 'font-medium text-foreground' : 'text-muted-foreground', failed && 'text-red-600 dark:text-red-400')}>
                {s.label}
              </span>
            </div>
            {i < steps.length - 1 && (
              <span className={cn('mx-2 h-px flex-1', i < current || status === 'done' ? 'bg-primary' : 'bg-border')} />
            )}
          </li>
        )
      })}
    </ol>
    </>
  )
}
