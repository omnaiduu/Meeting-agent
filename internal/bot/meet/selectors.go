package meet

// All Google Meet selectors and on-screen texts live here, so when Meet changes
// its UI this is the only file to edit. The probe (inject/probe.js) receives
// these as JSON. Texts are matched case-insensitively after normalizing curly
// apostrophes (Meet's live copy uses ’) and whitespace. The browser is pinned
// to en-US (--lang), so English texts are reliable.
//
// Sources: our own testing plus open-source bots (Vexa's selectors.ts, 2026).

// Patterns is the probe's input.
type Patterns struct {
	NameInput   []string `json:"nameInput"`    // CSS
	JoinTexts   []string `json:"joinTexts"`    // button texts, in priority order
	DismissText []string `json:"dismissTexts"` // pop-ups to close before joining
	LobbyTexts  []string `json:"lobbyTexts"`
	DeniedTexts []string `json:"deniedTexts"`
	BlockedText []string `json:"blockedTexts"`
	EndedTexts  []string `json:"endedTexts"`
	// InCall are CSS selectors that only exist after admission.
	// NOT "Leave call": Meet renders that button in the lobby too.
	InCall      []string `json:"inCall"`
	LeaveCSS    []string `json:"leaveCss"`
	LeaveTexts  []string `json:"leaveTexts"`
	PeopleWords []string `json:"peopleWords"` // label prefix of the People button (has the count)
}

// DefaultPatterns targets the Google Meet web UI as of Nov 2026.
var DefaultPatterns = Patterns{
	NameInput: []string{
		`input[type="text"][aria-label="Your name"]`,
		`input[placeholder*="name" i]`,
		`input[aria-label*="name" i]`,
		`input[jsname][type="text"]`,
	},
	JoinTexts: []string{"Ask to join", "Join now", "Join anyway", "Join"},
	DismissText: []string{
		"Continue without microphone and camera",
		"Continue without microphone",
		"Got it",
		"Dismiss",
		"No thanks",
		"Not now",
	},
	LobbyTexts: []string{
		"Asking to be let in",
		"You'll join the call when someone lets you in",
		"Please wait until a meeting host brings you into the call",
		"Waiting for the host",
		"waiting room",
	},
	DeniedTexts: []string{
		"denied your request",
		"Your request to join was denied",
		"weren't allowed to join",
		"not allowed to join",
		"Ask to join again",
	},
	BlockedText: []string{
		"You can't join this video call",
		"can't join this call",
		"Check your meeting code",
		"Meeting not found",
		"Invalid video call name",
	},
	EndedTexts: []string{
		"You've been removed from the meeting",
		"You were removed",
		"The call has ended",
		"This call has ended",
		"Meeting ended",
		"Call ended",
		"You left the meeting",
		"Return to home screen", // only meaningful after we were in the call; see probe
	},
	InCall: []string{
		`[data-participant-id]`,
		`button[aria-label*="Present now" i]`,
		`button[aria-label*="Share screen" i]`,
		`[data-self-name]`,
	},
	LeaveCSS:    []string{`button[aria-label="Leave call"]`, `button[aria-label*="Leave call" i]`, `button[aria-label*="Leave" i]`},
	LeaveTexts:  []string{"Just leave the call", "Leave call", "Leave meeting", "Leave"},
	PeopleWords: []string{"People", "Show everyone", "Participants"},
}
