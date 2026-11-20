package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/omnaiduu/meeting-agent/internal/store/db"
)

func TestMeetingLifecycle(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Now().UTC().Truncate(time.Millisecond)
	m, err := s.CreateMeeting(ctx, db.CreateMeetingParams{ID: "m1", Platform: "google_meet", MeetingUrl: "u", MeetingCode: "abc", BotName: "Bot", Status: "queued", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if !m.CreatedAt.Equal(now) {
		t.Errorf("created_at %v != %v", m.CreatedAt, now)
	}
	if err := s.SetJoined(ctx, db.SetJoinedParams{JoinedAt: sql.NullTime{Time: now, Valid: true}, ID: "m1"}); err != nil {
		t.Fatal(err)
	}
	seg, err := s.InsertSegment(ctx, db.InsertSegmentParams{MeetingID: "m1", StartMs: 1000, EndMs: 2000, Text: "hi", CreatedAt: now})
	if err != nil || seg.ID == 0 {
		t.Fatal(seg, err)
	}
	after, _ := s.ListSegmentsAfter(ctx, db.ListSegmentsAfterParams{MeetingID: "m1", ID: seg.ID})
	if len(after) != 0 {
		t.Errorf("after: %v", after)
	}
	ids, err := s.FailUnfinished(ctx)
	if err != nil || len(ids) != 1 {
		t.Fatalf("fail unfinished: %v %v", ids, err)
	}
	got, _ := s.GetMeeting(ctx, "m1")
	if got.Status != "failed" || got.ErrorCode.String != "interrupted" || !got.JoinedAt.Time.Equal(now) {
		t.Errorf("%+v", got)
	}
	if err := s.DeleteMeeting(ctx, "m1"); err != nil {
		t.Fatal(err)
	}
	segs, _ := s.ListSegments(ctx, "m1")
	if len(segs) != 0 {
		t.Error("segments should cascade-delete")
	}
	if _, err := s.GetMeeting(ctx, "m1"); !errors.Is(err, sql.ErrNoRows) {
		t.Error(err)
	}
}
