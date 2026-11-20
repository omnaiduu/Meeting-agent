import { useSyncExternalStore } from 'react'

/** True while the CSS media query matches (e.g. '(min-width: 1024px)'). */
export function useMediaQuery(query: string) {
  return useSyncExternalStore(
    (cb) => {
      const m = matchMedia(query)
      m.addEventListener('change', cb)
      return () => m.removeEventListener('change', cb)
    },
    () => matchMedia(query).matches,
  )
}
