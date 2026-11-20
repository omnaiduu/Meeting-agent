-- name: InsertSegment :one
INSERT INTO transcript_segments (meeting_id, start_ms, end_ms, text, created_at)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: ListSegments :many
SELECT * FROM transcript_segments WHERE meeting_id = ? ORDER BY start_ms, id;

-- name: ListSegmentsAfter :many
SELECT * FROM transcript_segments WHERE meeting_id = ? AND id > ? ORDER BY start_ms, id;

-- name: UpsertSummary :exec
INSERT INTO summaries (meeting_id, model, content, created_at) VALUES (?, ?, ?, ?)
ON CONFLICT (meeting_id) DO UPDATE SET model = excluded.model, content = excluded.content, created_at = excluded.created_at;

-- name: GetSummary :one
SELECT * FROM summaries WHERE meeting_id = ?;
