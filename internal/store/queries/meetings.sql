-- name: CreateMeeting :one
INSERT INTO meetings (id, platform, meeting_url, meeting_code, bot_name, status, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetMeeting :one
SELECT * FROM meetings WHERE id = ?;

-- name: ListMeetings :many
SELECT * FROM meetings ORDER BY created_at DESC LIMIT ?;

-- name: UpdateStatus :execrows
-- Never moves a meeting out of a terminal state.
UPDATE meetings SET status = ? WHERE id = ? AND status NOT IN ('done', 'failed');

-- name: SetFailed :execrows
UPDATE meetings SET status = 'failed', error_code = ?, error_message = ?, ended_at = COALESCE(ended_at, ?) WHERE id = ? AND status NOT IN ('done', 'failed');

-- name: SetJoined :exec
UPDATE meetings SET joined_at = ? WHERE id = ?;

-- name: SetEnded :exec
UPDATE meetings SET ended_at = ?, end_reason = ? WHERE id = ?;

-- name: SetAudioPath :exec
UPDATE meetings SET audio_path = ? WHERE id = ?;

-- name: SetTitle :exec
UPDATE meetings SET title = ? WHERE id = ?;

-- name: DeleteMeeting :exec
DELETE FROM meetings WHERE id = ?;

-- name: FailUnfinished :many
-- On startup: meetings left mid-flight by a crash can't resume.
UPDATE meetings SET status = 'failed', error_code = 'interrupted', error_message = 'The server restarted while this meeting was in progress.'
WHERE status NOT IN ('done', 'failed')
RETURNING id;
