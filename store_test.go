package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func testAppDB(t *testing.T) (*smsApp, *gorm.DB) {
	t.Helper()
	app, _ := newAppServer()
	db, err := gorm.Open(sqlite.Open("file:smsalert_"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.ensureSchema(db); err != nil {
		t.Fatal(err)
	}
	return app, db
}

func TestSubscribeAndRemove(t *testing.T) {
	app, db := testAppDB(t)
	list := listRow{
		Name:         "Alerts",
		Label:        "alerts",
		SubscribeMsg: "Welcome",
		RemoveMsg:    "Gone",
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	if err := db.Table(app.listsTable).Create(&list).Error; err != nil {
		t.Fatal(err)
	}
	phone := "+15551234567"
	added, err := app.subscribePhone(db, list, phone)
	if err != nil || !added {
		t.Fatalf("subscribe: added=%v err=%v", added, err)
	}
	added, err = app.subscribePhone(db, list, phone)
	if err != nil || added {
		t.Fatalf("duplicate should not add: added=%v err=%v", added, err)
	}
	lists, err := app.listsForPhone(db, phone)
	if err != nil || len(lists) != 1 {
		t.Fatalf("listsForPhone: %#v err=%v", lists, err)
	}
	if err := app.removeSubscriber(db, list.ListID, phone); err != nil {
		t.Fatal(err)
	}
	lists, err = app.listsForPhone(db, phone)
	if err != nil || len(lists) != 0 {
		t.Fatalf("expected empty after remove: %#v", lists)
	}
}

func TestSenderACL(t *testing.T) {
	app, db := testAppDB(t)
	listA := listRow{Name: "A", Label: "lista", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	listB := listRow{Name: "B", Label: "listb", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := db.Table(app.listsTable).Create(&listA).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table(app.listsTable).Create(&listB).Error; err != nil {
		t.Fatal(err)
	}
	sender := senderRow{
		Phone: "+15550001111", Name: "Ops", CanSend: true, Status: "active",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := db.Table(app.sendersTable).Create(&sender).Error; err != nil {
		t.Fatal(err)
	}
	if err := app.replaceSenderLists(db, sender.SenderID, []uint{listA.ListID}); err != nil {
		t.Fatal(err)
	}
	allowed, err := app.senderAllowedListIDs(db, sender.SenderID)
	if err != nil {
		t.Fatal(err)
	}
	if !containsUint(allowed, listA.ListID) || containsUint(allowed, listB.ListID) {
		t.Fatalf("ACL wrong: %v", allowed)
	}
	labels, err := app.senderListLabels(db, sender.SenderID)
	if err != nil || len(labels) != 1 || labels[0].Label != "lista" {
		t.Fatalf("labels: %#v err=%v", labels, err)
	}
	found, err := app.findSenderByPhone(db, "+15550001111")
	if err != nil || found.SenderID != sender.SenderID {
		t.Fatalf("findSender: %#v err=%v", found, err)
	}
}

func TestConversationTTL(t *testing.T) {
	app, db := testAppDB(t)
	phone := "+15551230000"
	if err := app.setConversation(db, phone, convoKindRemove, convoStateStart, "", time.Minute); err != nil {
		t.Fatal(err)
	}
	row, ok, err := app.getConversation(db, phone, convoKindRemove)
	if err != nil || !ok || row.State != convoStateStart {
		t.Fatalf("get: ok=%v row=%#v err=%v", ok, row, err)
	}
	if err := app.clearConversation(db, phone, convoKindRemove); err != nil {
		t.Fatal(err)
	}
	_, ok, err = app.getConversation(db, phone, convoKindRemove)
	if err != nil || ok {
		t.Fatal("expected cleared")
	}
}

func TestFindListByLabel(t *testing.T) {
	app, db := testAppDB(t)
	list := listRow{Name: "News", Label: "news", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := db.Table(app.listsTable).Create(&list).Error; err != nil {
		t.Fatal(err)
	}
	got, err := app.findListByLabel(db, "NEWS")
	if err != nil || got.ListID != list.ListID {
		t.Fatalf("got %#v err=%v", got, err)
	}
}

func TestStringifyConfigValuesCoercesFromNumbers(t *testing.T) {
	t.Parallel()
	got := stringifyConfigValues(map[string]any{
		"dry_run":                true,
		"from_number_live":       float64(15551234567),
		"account_sid_live":       "ACabc",
		"auth_token_live":        json.Number("12345"),
		"log_retention_messages": float64(50),
	})
	if got["from_number_live"] != "15551234567" {
		t.Fatalf("from_number_live: %#v", got["from_number_live"])
	}
	if got["auth_token_live"] != "12345" {
		t.Fatalf("auth_token_live: %#v", got["auth_token_live"])
	}
	if got["dry_run"] != true {
		t.Fatalf("dry_run should stay bool: %#v", got["dry_run"])
	}
	if got["log_retention_messages"] != float64(50) {
		t.Fatalf("retention should stay numeric: %#v", got["log_retention_messages"])
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var cfg smsSettings
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.FromNumberLive != "15551234567" || cfg.AccountSIDLive != "ACabc" {
		t.Fatalf("%#v", cfg)
	}
	if cfg.LogRetentionMessages == nil || *cfg.LogRetentionMessages != 50 {
		t.Fatalf("retention: %#v", cfg.LogRetentionMessages)
	}
}

func TestCredentialsFromSettings(t *testing.T) {
	t.Parallel()
	cfg := smsSettings{
		AccountSIDTest: "ACtest",
		AuthTokenTest:  "tt",
		FromNumberTest: "+15551111111",
		AccountSIDLive: "AClive",
		AuthTokenLive:  "tl",
		FromNumberLive: "+15552222222",
	}
	testCreds, err := credentialsFromSettings(cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	if testCreds.Mode != "test" || testCreds.AccountSID != "ACtest" || !testCreds.DryRun {
		t.Fatalf("%#v", testCreds)
	}
	liveCreds, err := credentialsFromSettings(cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	if liveCreds.Mode != "live" || liveCreds.AccountSID != "AClive" || liveCreds.DryRun {
		t.Fatalf("%#v", liveCreds)
	}
	_, err = credentialsFromSettings(smsSettings{}, true)
	if err == nil || !strings.Contains(err.Error(), "test credentials") {
		t.Fatalf("expected test credentials error, got %v", err)
	}
	_, err = credentialsFromSettings(smsSettings{}, false)
	if err == nil || !strings.Contains(err.Error(), "live credentials") {
		t.Fatalf("expected live credentials error, got %v", err)
	}
}
