import * as React from 'react'
import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '@/lib/utils'

const badgeVariants = cva(
  'inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-xs font-medium whitespace-nowrap [&_svg]:size-3.5',
  {
    variants: {
      tone: {
        neutral: 'border-border bg-muted text-muted-foreground',
        blue: 'border-sky-500/25 bg-sky-500/10 text-sky-700 dark:text-sky-300',
        green: 'border-emerald-500/25 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300',
        violet: 'border-violet-500/25 bg-violet-500/10 text-violet-700 dark:text-violet-300',
        red: 'border-red-500/25 bg-red-500/10 text-red-700 dark:text-red-300',
        amber: 'border-amber-500/25 bg-amber-500/10 text-amber-700 dark:text-amber-300',
      },
    },
    defaultVariants: { tone: 'neutral' },
  },
)

function Badge({ className, tone, ...props }: React.ComponentProps<'span'> & VariantProps<typeof badgeVariants>) {
  return <span className={cn(badgeVariants({ tone }), className)} {...props} />
}

export { Badge, badgeVariants }
