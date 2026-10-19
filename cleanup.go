package main

import (
	"log"
	"time"

	"gorm.io/gorm"
)

const (
	defaultLogRetentionMessages = 100
	defaultLogCleanupHours      = 24
)

func (cfg smsSettings) retentionMessages() int {
	if cfg.LogRetentionMessages == nil {
		return defaultLogRetentionMessages
	}
	if *cfg.LogRetentionMessages < 0 {
		return 0
	}
	return *cfg.LogRetentionMessages
}

func (cfg smsSettings) cleanupInterval() time.Duration {
	hours := defaultLogCleanupHours
	if cfg.LogCleanupHours != nil && *cfg.LogCleanupHours > 0 {
		hours = *cfg.LogCleanupHours
	}
	return time.Duration(hours) * time.Hour
}

func (app *smsApp) startCleanupLoop() {
	go func() {
		// Initial delay so install/config can settle.
		timer := time.NewTimer(2 * time.Minute)
		defer timer.Stop()
		<-timer.C
		app.maybeCleanupLogs(false)

		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			app.maybeCleanupLogs(false)
		}
	}()
}

// maybeCleanupLogs runs retention cleanup when due (or always when force is true).
func (app *smsApp) maybeCleanupLogs(force bool) (deleted int, ran bool, err error) {
	app.cleanupMu.Lock()
	defer app.cleanupMu.Unlock()

	cfg, err := app.loadSettings()
	if err != nil {
		return 0, false, err
	}
	keep := cfg.retentionMessages()
	if keep == 0 {
		return 0, false, nil
	}
	if !force {
		interval := cfg.cleanupInterval()
		if !app.lastCleanup.IsZero() && time.Since(app.lastCleanup) < interval {
			return 0, false, nil
		}
	}

	db, err := app.db()
	if err != nil {
		return 0, false, err
	}
	if err := app.ensureSchema(db); err != nil {
		return 0, false, err
	}
	deleted, err = app.pruneMessageLogs(db, keep)
	if err != nil {
		return 0, true, err
	}
	app.lastCleanup = time.Now().UTC()
	if deleted > 0 {
		log.Printf("sms-alert: pruned %d old message(s); keeping newest %d", deleted, keep)
	}
	return deleted, true, nil
}

// pruneMessageLogs deletes messages (and their delivery rows) beyond the newest keep count.
func (app *smsApp) pruneMessageLogs(db *gorm.DB, keep int) (int, error) {
	if keep <= 0 {
		return 0, nil
	}
	var total int64
	if err := db.Table(app.messagesTable).Count(&total).Error; err != nil {
		return 0, err
	}
	if total <= int64(keep) {
		return 0, nil
	}

	var keepIDs []uint
	err := db.Table(app.messagesTable).
		Select("message_id").
		Order("created_at desc, message_id desc").
		Limit(keep).
		Pluck("message_id", &keepIDs).Error
	if err != nil {
		return 0, err
	}
	if len(keepIDs) == 0 {
		return 0, nil
	}

	var oldIDs []uint
	err = db.Table(app.messagesTable).
		Where("message_id NOT IN ?", keepIDs).
		Pluck("message_id", &oldIDs).Error
	if err != nil {
		return 0, err
	}
	if len(oldIDs) == 0 {
		return 0, nil
	}

	if err := db.Table(app.messageLogsTable).Where("message_id IN ?", oldIDs).Delete(&messageLogRow{}).Error; err != nil {
		return 0, err
	}
	res := db.Table(app.messagesTable).Where("message_id IN ?", oldIDs).Delete(&messageRow{})
	if res.Error != nil {
		return 0, res.Error
	}
	return int(res.RowsAffected), nil
}
