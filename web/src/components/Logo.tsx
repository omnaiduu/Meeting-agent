export function Logo({ className = 'size-7' }: { className?: string }) {
  return (
    <svg viewBox="0 0 32 32" className={className} aria-hidden="true">
      <rect width="32" height="32" rx="8" className="fill-primary" />
      <g stroke="currentColor" strokeWidth="2.6" strokeLinecap="round" className="text-primary-foreground">
        <path d="M9 13v6" />
        <path d="M13.7 9v14" />
        <path d="M18.3 11.5v9" />
        <path d="M23 14v4" />
      </g>
    </svg>
  )
}
