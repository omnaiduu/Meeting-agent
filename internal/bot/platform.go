// Package bot runs meeting bots: it launches Chrome, drives a platform adapter
// (Google Meet today) through join → call → leave, and streams captured audio.
package bot

import (
	"context"
	"errors"
	"time"
)

// Phase is what the bot observes about itself while joining.
type Phase string

const (
	PhaseJoining Phase = "joining"  // pre-join screen, filling name / clicking join
	PhaseLobby   Phase = "in_lobby" // asked to join, waiting for the host
	PhaseInCall  Phase = "in_call"  // admitted
)

// Join failures. Each maps to a meeting error_code.
var (
	ErrDenied       = errors.New("host denied the join request")
	ErrJoinBlocked  = errors.New("meeting refused the join before the lobby")
	ErrLobbyTimeout = errors.New("nobody admitted the bot in time")
	ErrJoinTimeout  = errors.New("could not find the join controls")
	ErrMeetingGone  = errors.New("meeting ended or was not found")
)

// EndReason says why a call ended.
type EndReason string

const (
	EndAlone       EndReason = "alone"        // everyone else left
	EndMeetingOver EndReason = "meeting_over" // removed, or the call ended
	EndMaxDuration EndReason = "max_duration"
	EndStopped     EndReason = "stopped" // user asked the bot to leave
)

// JoinOptions configure a single join attempt.
type JoinOptions struct {
	URL          string
	BotName      string
	LobbyTimeout time.Duration
	// OnPhase is called each time the phase changes (may be nil).
	OnPhase func(Phase)
}

// WatchOptions configure end-of-meeting detection.
type WatchOptions struct {
	AloneTimeout time.Duration
	MaxDuration  time.Duration
	PollInterval time.Duration
}

// AudioFunc receives 16 kHz mono little-endian int16 PCM. seq increases by one
// per call in the page; calls can arrive out of order, so receivers must reorder.
type AudioFunc func(seq int, pcm []byte)

// Platform is a meeting provider adapter (Meet now; Teams/Zoom later).
// ctx is always a chromedp tab context.
type Platform interface {
	// Prepare runs before navigation: inject capture scripts, expose Go callbacks.
	Prepare(ctx context.Context, onAudio AudioFunc) error
	// Join navigates to the meeting and returns once the bot is in the call.
	Join(ctx context.Context, opts JoinOptions) error
	// WaitUntilEnded blocks until the call is over for one of the EndReasons.
	// Cancelling ctx returns EndStopped.
	WaitUntilEnded(ctx context.Context, opts WatchOptions) (EndReason, error)
	// Leave clicks "leave" (best effort).
	Leave(ctx context.Context) error
}
