import { createRootRouteWithContext, Link, Outlet } from '@tanstack/react-router'
import type { QueryClient } from '@tanstack/react-query'
import { Toaster } from 'sonner'
import { Logo } from '@/components/Logo'
import { ThemeToggle } from '@/components/ThemeToggle'
import { TooltipProvider } from '@/components/ui/tooltip'
import { buttonVariants } from '@/components/ui/button'

export const Route = createRootRouteWithContext<{ queryClient: QueryClient }>()({
  component: Root,
  notFoundComponent: () => (
    <div className="mx-auto max-w-md px-4 py-24 text-center">
      <h1 className="text-xl font-semibold">Page not found</h1>
      <Link to="/" className={buttonVariants({ variant: 'outline', className: 'mt-6' })}>Back to dashboard</Link>
    </div>
  ),
})

function Root() {
  return (
    <TooltipProvider>
      <div className="flex min-h-dvh flex-col">
        <header className="sticky top-0 z-40 border-b bg-background/80 backdrop-blur-md">
          <div className="mx-auto flex h-14 max-w-6xl items-center justify-between px-4 sm:px-6">
            <Link to="/" className="flex items-center gap-2.5 font-semibold tracking-tight">
              <Logo />
              Meeting Agent
            </Link>
            <ThemeToggle />
          </div>
        </header>
        <main className="flex-1">
          <Outlet />
        </main>
      </div>
      <Toaster richColors closeButton position="bottom-right" />
    </TooltipProvider>
  )
}
