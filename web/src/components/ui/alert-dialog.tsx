import * as React from 'react'
import { AlertDialog as A } from 'radix-ui'
import { buttonVariants } from './button'
import { cn } from '@/lib/utils'

// A confirm dialog: <Confirm title=... onConfirm=...><Button/></Confirm>
function Confirm({
  children, title, description, confirmLabel = 'Confirm', destructive, onConfirm,
}: {
  children: React.ReactNode
  title: string
  description: string
  confirmLabel?: string
  destructive?: boolean
  onConfirm: () => void
}) {
  return (
    <A.Root>
      <A.Trigger asChild>{children}</A.Trigger>
      <A.Portal>
        <A.Overlay className="fixed inset-0 z-50 bg-black/40 backdrop-blur-[2px] animate-fade-in" />
        <A.Content className="fixed top-1/2 left-1/2 z-50 w-[calc(100%-2rem)] max-w-md -translate-x-1/2 -translate-y-1/2 rounded-xl border bg-card p-6 shadow-xl animate-fade-in">
          <A.Title className="text-base font-semibold">{title}</A.Title>
          <A.Description className="mt-2 text-sm text-muted-foreground">{description}</A.Description>
          <div className="mt-6 flex justify-end gap-2">
            <A.Cancel className={buttonVariants({ variant: 'outline' })}>Cancel</A.Cancel>
            <A.Action className={cn(buttonVariants({ variant: destructive ? 'destructive' : 'default' }))} onClick={onConfirm}>
              {confirmLabel}
            </A.Action>
          </div>
        </A.Content>
      </A.Portal>
    </A.Root>
  )
}

export { Confirm }
