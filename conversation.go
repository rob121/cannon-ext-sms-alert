package main

import (
	"strings"
	"time"

	"gorm.io/gorm"
)

func (app *smsApp) purgeExpiredConversations(db *gorm.DB) {
	_ = db.Table(app.conversationsTable).Where("expires_at < ?", time.Now().UTC()).Delete(&conversationRow{}).Error
}

func (app *smsApp) getConversation(db *gorm.DB, phone, kind string) (conversationRow, bool, error) {
	app.purgeExpiredConversations(db)
	var row conversationRow
	err := db.Table(app.conversationsTable).Where("phone = ? AND kind = ?", phone, kind).First(&row).Error
	if err == gorm.ErrRecordNotFound {
		return conversationRow{}, false, nil
	}
	if err != nil {
		return conversationRow{}, false, err
	}
	if !row.ExpiresAt.IsZero() && row.ExpiresAt.Before(time.Now().UTC()) {
		_ = app.clearConversation(db, phone, kind)
		return conversationRow{}, false, nil
	}
	return row, true, nil
}

func (app *smsApp) setConversation(db *gorm.DB, phone, kind, state, payload string, ttl time.Duration) error {
	app.purgeExpiredConversations(db)
	now := time.Now().UTC()
	row := conversationRow{
		Phone:     phone,
		Kind:      kind,
		State:     state,
		Payload:   payload,
		ExpiresAt: now.Add(ttl),
		UpdatedAt: now,
	}
	var existing conversationRow
	err := db.Table(app.conversationsTable).Where("phone = ? AND kind = ?", phone, kind).First(&existing).Error
	if err == gorm.ErrRecordNotFound {
		return db.Table(app.conversationsTable).Create(&row).Error
	}
	if err != nil {
		return err
	}
	row.ConversationID = existing.ConversationID
	return db.Table(app.conversationsTable).Save(&row).Error
}

func (app *smsApp) clearConversation(db *gorm.DB, phone, kind string) error {
	return db.Table(app.conversationsTable).Where("phone = ? AND kind = ?", phone, kind).Delete(&conversationRow{}).Error
}

func (app *smsApp) hasConversation(db *gorm.DB, phone, kind string) bool {
	_, ok, _ := app.getConversation(db, phone, kind)
	return ok
}

func firstWords(input string, n int) string {
	words := strings.Fields(strings.TrimSpace(input))
	if len(words) <= n {
		return strings.Join(words, " ")
	}
	return strings.Join(words[:n], " ")
}
