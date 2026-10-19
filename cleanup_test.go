package main

import (
	"testing"
	"time"
)

func TestPruneMessageLogsKeepsNewest(t *testing.T) {
	app, db := testAppDB(t)
	list := listRow{Name: "A", Label: "a", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := db.Table(app.listsTable).Create(&list).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		msg := messageRow{
			ListID:    list.ListID,
			Title:     "m",
			Body:      "body",
			CreatedAt: now.Add(time.Duration(i) * time.Minute),
		}
		if err := db.Table(app.messagesTable).Create(&msg).Error; err != nil {
			t.Fatal(err)
		}
		logRow := messageLogRow{
			MessageID:    msg.MessageID,
			SubscriberID: 1,
			Status:       "sent",
			CreatedAt:    msg.CreatedAt,
		}
		if err := db.Table(app.messageLogsTable).Create(&logRow).Error; err != nil {
			t.Fatal(err)
		}
	}
	deleted, err := app.pruneMessageLogs(db, 2)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 3 {
		t.Fatalf("deleted=%d want 3", deleted)
	}
	var msgCount, logCount int64
	_ = db.Table(app.messagesTable).Count(&msgCount).Error
	_ = db.Table(app.messageLogsTable).Count(&logCount).Error
	if msgCount != 2 || logCount != 2 {
		t.Fatalf("msg=%d log=%d", msgCount, logCount)
	}
}

func TestRetentionDisabledSkipsPrune(t *testing.T) {
	t.Parallel()
	app, _ := newAppServer()
	deleted, err := app.pruneMessageLogs(nil, 0)
	if err != nil || deleted != 0 {
		t.Fatalf("deleted=%d err=%v", deleted, err)
	}
}

func TestCleanupIntervalHelpers(t *testing.T) {
	t.Parallel()
	cfg := smsSettings{}
	if cfg.retentionMessages() != defaultLogRetentionMessages {
		t.Fatal("default retention")
	}
	if cfg.cleanupInterval() != time.Duration(defaultLogCleanupHours)*time.Hour {
		t.Fatal("default interval")
	}
	keep := 10
	hours := 2
	cfg.LogRetentionMessages = &keep
	cfg.LogCleanupHours = &hours
	if cfg.retentionMessages() != 10 || cfg.cleanupInterval() != 2*time.Hour {
		t.Fatal("custom values")
	}
}
