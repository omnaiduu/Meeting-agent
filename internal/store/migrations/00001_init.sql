-- +goose Up
CREATE TABLE meetings (
  id            TEXT PRIMARY KEY,               -- ULID
  platform      TEXT NOT NULL DEFAULT 'google_meet',
  meeting_url   TEXT NOT NULL,
  meeting_code  TEXT NOT NULL,                  -- e.g. abc-defg-hij
  bot_name      TEXT NOT NULL,
  title         TEXT,                           -- from the summary, when known
  status        TEXT NOT NULL,
  error_code    TEXT,
  error_message TEXT,
  end_reason    TEXT,
  created_at    DATETIME NOT NULL,
  joined_at     DATETIME,
  ended_at      DATETIME,
  audio_path    TEXT
);
CREATE INDEX idx_meetings_created ON meetings(created_at DESC);

CREATE TABLE transcript_segments (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  meeting_id  TEXT NOT NULL REFERENCES meetings(id) ON DELETE CASCADE,
  start_ms    INTEGER NOT NULL,                 -- relative to the start of the recording
  end_ms      INTEGER NOT NULL,
  text        TEXT NOT NULL,
  created_at  DATETIME NOT NULL
);
CREATE INDEX idx_segments_meeting ON transcript_segments(meeting_id, start_ms);

CREATE TABLE summaries (
  meeting_id  TEXT PRIMARY KEY REFERENCES meetings(id) ON DELETE CASCADE,
  model       TEXT NOT NULL,
  content     TEXT NOT NULL,                    -- JSON, shape in docs/PLAN.md §6
  created_at  DATETIME NOT NULL
);

-- +goose Down
DROP TABLE summaries;
DROP TABLE transcript_segments;
DROP TABLE meetings;
