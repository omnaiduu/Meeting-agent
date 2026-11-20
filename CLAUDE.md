# Meeting Agent

Go + React bot that joins Google Meet as a guest, captures call audio, transcribes it live (Groq Whisper), and summarizes it (Groq LLM).

**Read `docs/PLAN.md` first.** It holds every design decision, the architecture, the API, the UI layout, and the milestone checklist (M0 → M7). Work through milestones in order, tick the checkboxes as you finish them, and update the plan if a decision changes.

Key decisions (details in the plan): Go 1.24+, chromedp, headed Chrome on Xvfb, injected JS audio capture via `ExposeFunc`, stdlib `net/http`, SSE, SQLite (`modernc.org/sqlite`) + sqlc + goose, one goroutine per meeting behind a `Runner` interface, Vite + React + TS + TanStack Router/Query + Tailwind v4 + shadcn/ui embedded in the Go binary. Speaker identification is out of scope for now.

## Git

No AI attribution: do not add `Co-Authored-By`, `Claude-Session`, or "Generated with Claude Code" lines to commits or PRs. Commit as Om Naidu.
