package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/rob121/cannon/extension"
	"gorm.io/gorm"
)

type numberRow struct {
	NumberID  uint      `gorm:"primaryKey;column:number_id"`
	Label     string    `gorm:"column:label;size:128;not null"`
	Number    string    `gorm:"column:number;size:32;not null"`
	Active    bool      `gorm:"column:active;not null;default:true"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (numberRow) TableName() string { return "" } // set via Table()

type listRow struct {
	ListID        uint      `gorm:"primaryKey;column:list_id"`
	Name          string    `gorm:"column:name;size:160;not null"`
	Label         string    `gorm:"column:label;size:64;not null;uniqueIndex"`
	Description   string    `gorm:"column:description;type:text"`
	NumberID      uint      `gorm:"column:number_id;index"`
	SubscribeMsg  string    `gorm:"column:subscribe_msg;type:text"`
	RemoveMsg     string    `gorm:"column:remove_msg;type:text"`
	SubscribersCt int       `gorm:"column:subscribers_ct;not null;default:0"`
	CreatedAt     time.Time `gorm:"column:created_at"`
	UpdatedAt     time.Time `gorm:"column:updated_at"`
}

type subscriberRow struct {
	SubscriberID uint      `gorm:"primaryKey;column:subscriber_id"`
	Phone        string    `gorm:"column:phone;size:32;not null;uniqueIndex:idx_sms_sub_phone_list,priority:1"`
	ListID       uint      `gorm:"column:list_id;not null;index;uniqueIndex:idx_sms_sub_phone_list,priority:2"`
	CreatedAt    time.Time `gorm:"column:created_at"`
}

type senderRow struct {
	SenderID  uint      `gorm:"primaryKey;column:sender_id"`
	Phone     string    `gorm:"column:phone;size:32;not null;uniqueIndex"`
	Name      string    `gorm:"column:name;size:160"`
	CanSend   bool      `gorm:"column:can_send;not null;default:true"`
	UserID    *uint     `gorm:"column:user_id;index"`
	Status    string    `gorm:"column:status;size:16;not null;default:active"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

type senderListRow struct {
	SenderID uint `gorm:"primaryKey;column:sender_id"`
	ListID   uint `gorm:"primaryKey;column:list_id;index"`
}

type messageRow struct {
	MessageID uint      `gorm:"primaryKey;column:message_id"`
	ListID    uint      `gorm:"column:list_id;index;not null"`
	Title     string    `gorm:"column:title;size:256"`
	Body      string    `gorm:"column:body;type:text;not null"`
	CreatedBy uint      `gorm:"column:created_by"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

type messageLogRow struct {
	LogID        uint      `gorm:"primaryKey;column:log_id"`
	MessageID    uint      `gorm:"column:message_id;index;not null"`
	SubscriberID uint      `gorm:"column:subscriber_id;index;not null"`
	Status       string    `gorm:"column:status;size:32;not null;default:sent"`
	Error        string    `gorm:"column:error;type:text"`
	CreatedAt    time.Time `gorm:"column:created_at"`
}

type conversationRow struct {
	ConversationID uint      `gorm:"primaryKey;column:conversation_id"`
	Phone          string    `gorm:"column:phone;size:32;not null;uniqueIndex:idx_sms_convo_phone_kind,priority:1"`
	Kind           string    `gorm:"column:kind;size:32;not null;uniqueIndex:idx_sms_convo_phone_kind,priority:2"`
	State          string    `gorm:"column:state;size:64;not null"`
	Payload        string    `gorm:"column:payload;type:text"`
	ExpiresAt      time.Time `gorm:"column:expires_at;index"`
	UpdatedAt      time.Time `gorm:"column:updated_at"`
}

func (app *smsApp) ensureSchema(db *gorm.DB) error {
	app.schemaOnce.Do(func() {
		app.schemaErr = extension.MigrateConfigurationStore(db, app.settingsTable)
		if app.schemaErr != nil {
			return
		}
		app.schemaErr = db.Table(app.numbersTable).AutoMigrate(&numberRow{})
		if app.schemaErr != nil {
			return
		}
		app.schemaErr = db.Table(app.listsTable).AutoMigrate(&listRow{})
		if app.schemaErr != nil {
			return
		}
		app.schemaErr = db.Table(app.subscribersTable).AutoMigrate(&subscriberRow{})
		if app.schemaErr != nil {
			return
		}
		app.schemaErr = db.Table(app.sendersTable).AutoMigrate(&senderRow{})
		if app.schemaErr != nil {
			return
		}
		app.schemaErr = db.Table(app.senderListsTable).AutoMigrate(&senderListRow{})
		if app.schemaErr != nil {
			return
		}
		app.schemaErr = db.Table(app.messagesTable).AutoMigrate(&messageRow{})
		if app.schemaErr != nil {
			return
		}
		app.schemaErr = db.Table(app.messageLogsTable).AutoMigrate(&messageLogRow{})
		if app.schemaErr != nil {
			return
		}
		app.schemaErr = db.Table(app.conversationsTable).AutoMigrate(&conversationRow{})
	})
	return app.schemaErr
}

func (app *smsApp) loadSettings() (smsSettings, error) {
	data, err := configStore{table: app.settingsTable, app: app}.Load("general")
	if err != nil {
		return smsSettings{}, err
	}
	// Config UI / JSON may store digit-only from-numbers as numbers; coerce first.
	raw, err := json.Marshal(stringifyConfigValues(data))
	if err != nil {
		return smsSettings{}, err
	}
	var cfg smsSettings
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return smsSettings{}, err
	}
	return cfg, nil
}

// stringConfigKeys must unmarshal as Go strings even when the config UI stores digits as JSON numbers.
var stringConfigKeys = map[string]bool{
	"account_sid_test": true,
	"auth_token_test":  true,
	"from_number_test": true,
	"account_sid_live": true,
	"auth_token_live":  true,
	"from_number_live": true,
}

// stringifyConfigValues coerces selected settings to strings so values like
// from_number_live survive round-trips when stored as numbers. Other keys
// (booleans, integers) are left unchanged.
func stringifyConfigValues(data map[string]any) map[string]any {
	if data == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(data))
	for k, v := range data {
		if !stringConfigKeys[k] {
			out[k] = v
			continue
		}
		switch t := v.(type) {
		case nil:
			out[k] = nil
		case string:
			out[k] = t
		case json.Number:
			out[k] = t.String()
		case float64:
			if t == float64(int64(t)) {
				out[k] = strconv.FormatInt(int64(t), 10)
			} else {
				out[k] = strconv.FormatFloat(t, 'f', -1, 64)
			}
		case float32:
			out[k] = strconv.FormatFloat(float64(t), 'f', -1, 64)
		case int:
			out[k] = strconv.Itoa(t)
		case int64:
			out[k] = strconv.FormatInt(t, 10)
		case uint64:
			out[k] = strconv.FormatUint(t, 10)
		default:
			out[k] = fmt.Sprint(t)
		}
	}
	return out
}

func (app *smsApp) refreshSubscriberCount(db *gorm.DB, listID uint) error {
	var count int64
	if err := db.Table(app.subscribersTable).Where("list_id = ?", listID).Count(&count).Error; err != nil {
		return err
	}
	return db.Table(app.listsTable).Where("list_id = ?", listID).Update("subscribers_ct", count).Error
}

func (app *smsApp) findListByLabel(db *gorm.DB, label string) (listRow, error) {
	label = strings.ToLower(strings.TrimSpace(label))
	var row listRow
	err := db.Table(app.listsTable).Where("LOWER(label) = ?", label).First(&row).Error
	return row, err
}

func (app *smsApp) listsForPhone(db *gorm.DB, phone string) ([]listRow, error) {
	var listIDs []uint
	if err := db.Table(app.subscribersTable).Where("phone = ?", phone).Pluck("list_id", &listIDs).Error; err != nil {
		return nil, err
	}
	if len(listIDs) == 0 {
		return nil, nil
	}
	var rows []listRow
	err := db.Table(app.listsTable).Where("list_id IN ?", listIDs).Order("name asc").Find(&rows).Error
	return rows, err
}

func (app *smsApp) findSenderByPhone(db *gorm.DB, phone string) (senderRow, error) {
	var row senderRow
	err := db.Table(app.sendersTable).
		Where("phone = ? AND status = ? AND can_send = ?", phone, "active", true).
		First(&row).Error
	return row, err
}

func (app *smsApp) senderListLabels(db *gorm.DB, senderID uint) ([]listRow, error) {
	var listIDs []uint
	if err := db.Table(app.senderListsTable).Where("sender_id = ?", senderID).Pluck("list_id", &listIDs).Error; err != nil {
		return nil, err
	}
	if len(listIDs) == 0 {
		return nil, nil
	}
	var rows []listRow
	err := db.Table(app.listsTable).Where("list_id IN ?", listIDs).Order("label asc").Find(&rows).Error
	return rows, err
}

func (app *smsApp) subscribePhone(db *gorm.DB, list listRow, phone string) (bool, error) {
	var count int64
	if err := db.Table(app.subscribersTable).Where("phone = ? AND list_id = ?", phone, list.ListID).Count(&count).Error; err != nil {
		return false, err
	}
	if count > 0 {
		return false, nil
	}
	row := subscriberRow{Phone: phone, ListID: list.ListID, CreatedAt: time.Now().UTC()}
	if err := db.Table(app.subscribersTable).Create(&row).Error; err != nil {
		return false, err
	}
	_ = app.refreshSubscriberCount(db, list.ListID)
	return true, nil
}

func (app *smsApp) removeSubscriber(db *gorm.DB, listID uint, phone string) error {
	if err := db.Table(app.subscribersTable).Where("list_id = ? AND phone = ?", listID, phone).Delete(&subscriberRow{}).Error; err != nil {
		return err
	}
	return app.refreshSubscriberCount(db, listID)
}

func (app *smsApp) removeFromAllLists(db *gorm.DB, phone string) ([]string, error) {
	lists, err := app.listsForPhone(db, phone)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(lists))
	for _, l := range lists {
		if err := app.removeSubscriber(db, l.ListID, phone); err != nil {
			return names, err
		}
		names = append(names, l.Name)
	}
	return names, nil
}

func (app *smsApp) loadNumber(db *gorm.DB, id uint) (numberRow, error) {
	var row numberRow
	err := db.Table(app.numbersTable).Where("number_id = ?", id).First(&row).Error
	return row, err
}

func (app *smsApp) replaceSenderLists(db *gorm.DB, senderID uint, listIDs []uint) error {
	if err := db.Table(app.senderListsTable).Where("sender_id = ?", senderID).Delete(&senderListRow{}).Error; err != nil {
		return err
	}
	for _, id := range listIDs {
		if id == 0 {
			continue
		}
		if err := db.Table(app.senderListsTable).Create(&senderListRow{SenderID: senderID, ListID: id}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (app *smsApp) senderAllowedListIDs(db *gorm.DB, senderID uint) ([]uint, error) {
	var ids []uint
	err := db.Table(app.senderListsTable).Where("sender_id = ?", senderID).Pluck("list_id", &ids).Error
	return ids, err
}

func defaultSubscribeMsg(listName string) string {
	return fmt.Sprintf("You are subscribed to %s.", listName)
}

func defaultRemoveMsg(listName string) string {
	return fmt.Sprintf("You have been removed from %s.", listName)
}
