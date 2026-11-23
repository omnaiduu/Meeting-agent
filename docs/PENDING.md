# Pending work

Everything in the plan (M0–M7) is built and tested against the local fake Meet. What is left needs a laptop on a home network and real people in a call.

## 1. Real Google Meet test (from the laptop)

```bash
go run ./cmd/joinmeet -url https://meet.google.com/xxx-xxxx-xxx
```

- Start a meeting from your Google account, run the command, click **Admit** when "Meeting Agent (Notetaker)" asks to join, and talk for 30–60 seconds.
- Check `data/debug/joinmeet.wav` (your voice should be in it) and the screenshots in `data/debug/`.
- If joining fails, the log says which step failed and a screenshot is saved. The fix usually goes in `internal/bot/meet/selectors.go`.

### What happened when we tried from the cloud dev container (2026-11-23)

- Meet loaded fine in headed Chrome on Xvfb once the Meet hosts were allowed on the network.
- Google refused the guest **before the lobby**: the bot saw "You can't join this video call", and Meet's own join check returned HTTP 403. The proxy was not involved.
- Most likely cause: Google blocks anonymous guests from cloud/datacenter IPs. (A meeting whose access setting doesn't let guests ask to join shows the same screen, so check Host controls → Meeting access if it also happens on the laptop.)
- Good sign: the bot recognized Meet's real "can't join" screen, saved a screenshot and exited with a clear error, so the failure handling works on real Meet.
- Consequence for hosting: a server in a datacenter may need a residential IP or a signed-in Google account for the bot. Decide after the laptop test.

## 2. Docker test on the laptop

```bash
docker compose up --build
```

Send the bot to a real meeting from the dashboard and confirm the transcript and summary appear.

## 3. Real demo clip

Record a short real-meeting clip for the portfolio and put it next to `docs/screenshots/demo.mp4`.

## 4. After that

- Publish on GitHub and LinkedIn; write the blog post from `docs/blog-notes.md`.
- Optional, later: the self-healing selectors idea in [`LIMITATIONS.md`](LIMITATIONS.md), speaker names, Teams support.
