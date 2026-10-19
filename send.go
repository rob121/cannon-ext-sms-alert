package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/rob121/cannon/extension"
	"gorm.io/gorm"
)

func (app *smsApp) sendMessage(db *gorm.DB, req extension.WireRequest, messageID uint) error {
	var msg messageRow
	if err := db.Table(app.messagesTable).Where("message_id = ?", messageID).First(&msg).Error; err != nil {
		return err
	}
	var list listRow
	if err := db.Table(app.listsTable).Where("list_id = ?", msg.ListID).First(&list).Error; err != nil {
		return err
	}
	creds, err := app.activeCredentials(req)
	if err != nil {
		return err
	}
	from := strings.TrimSpace(creds.FromNumber)
	if list.NumberID > 0 {
		if num, err := app.loadNumber(db, list.NumberID); err == nil && strings.TrimSpace(num.Number) != "" {
			from = strings.TrimSpace(num.Number)
		}
	}
	if from == "" {
		return fmt.Errorf("no from number configured for list %q", list.Name)
	}
	var subs []subscriberRow
	if err := db.Table(app.subscribersTable).Where("list_id = ?", list.ListID).Find(&subs).Error; err != nil {
		return err
	}
	for _, sub := range subs {
		sendErr := creds.Send(from, sub.Phone, msg.Body)
		logRow := messageLogRow{
			MessageID:    msg.MessageID,
			SubscriberID: sub.SubscriberID,
			Status:       "sent",
			CreatedAt:    time.Now().UTC(),
		}
		if sendErr != nil {
			logRow.Status = "error"
			logRow.Error = sendErr.Error()
			log.Printf("sms-alert: send to %s failed: %v", sub.Phone, sendErr)
		}
		if err := db.Table(app.messageLogsTable).Create(&logRow).Error; err != nil {
			log.Printf("sms-alert: log write failed: %v", err)
		}
	}
	go app.maybeCleanupLogs(false)
	return nil
}

func (app *smsApp) sendMessageAsync(db *gorm.DB, req extension.WireRequest, messageID uint) {
	go func() {
		if err := app.sendMessage(db, req, messageID); err != nil {
			log.Printf("sms-alert: async send %d: %v", messageID, err)
		}
	}()
}
