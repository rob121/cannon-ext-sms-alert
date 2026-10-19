package main

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/rob121/cannon/extension"
)

func TestWebhookRejectsBadSignature(t *testing.T) {
	t.Parallel()
	params := url.Values{"From": {"+15551234567"}, "Body": {"alerts"}}
	if verifyTwilioSignature("tok", "https://x/webhooks/sms-alert", "nope", params) {
		t.Fatal("expected reject")
	}
}

func TestHandleRemoveAndSubscribeFlows(t *testing.T) {
	app, db := testAppDB(t)
	list := listRow{
		Name: "Alerts", Label: "alerts", SubscribeMsg: "Joined alerts", RemoveMsg: "Left alerts",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := db.Table(app.listsTable).Create(&list).Error; err != nil {
		t.Fatal(err)
	}
	phone := "+15557654321"
	_, err := app.subscribePhone(db, list, phone)
	if err != nil {
		t.Fatal(err)
	}

	resp := app.handleRemoveStart(db, phone, time.Minute)
	if resp.StatusCode != http.StatusOK || !strings.Contains(resp.Body, "Left alerts") {
		t.Fatalf("single-list remove: %#v", resp)
	}

	_, err = app.subscribePhone(db, list, phone)
	if err != nil {
		t.Fatal(err)
	}
	list2 := listRow{
		Name: "News", Label: "news", RemoveMsg: "Left news",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := db.Table(app.listsTable).Create(&list2).Error; err != nil {
		t.Fatal(err)
	}
	_, _ = app.subscribePhone(db, list2, phone)

	resp = app.handleRemoveStart(db, phone, time.Minute)
	if !strings.Contains(resp.Body, "Which list") || !strings.Contains(resp.Body, "all") {
		t.Fatalf("multi remove prompt: %s", resp.Body)
	}
	resp = app.handleRemovePick(db, phone, "all", time.Minute)
	if !strings.Contains(resp.Body, "removed from") {
		t.Fatalf("remove all: %s", resp.Body)
	}
}

func TestSenderConversationACL(t *testing.T) {
	app, db := testAppDB(t)
	list := listRow{Name: "A", Label: "lista", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := db.Table(app.listsTable).Create(&list).Error; err != nil {
		t.Fatal(err)
	}
	other := listRow{Name: "B", Label: "listb", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := db.Table(app.listsTable).Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	sender := senderRow{
		Phone: "+15550009999", Name: "Ops", CanSend: true, Status: "active",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := db.Table(app.sendersTable).Create(&sender).Error; err != nil {
		t.Fatal(err)
	}
	_ = app.replaceSenderLists(db, sender.SenderID, []uint{list.ListID})

	req := extension.WireRequest{Method: http.MethodPost, URL: "https://example.com/webhooks/sms-alert", Body: "Body=hello+world"}
	resp, handled := app.handleSenderConversation(db, req, sender.Phone, "hello world", sender, time.Minute)
	if !handled || !strings.Contains(resp.Body, "what list") {
		t.Fatalf("start: handled=%v body=%s", handled, resp.Body)
	}
	resp, handled = app.handleSenderConversation(db, req, sender.Phone, "listb", sender, time.Minute)
	if !handled || !strings.Contains(resp.Body, "not allowed") {
		t.Fatalf("ACL: %s", resp.Body)
	}
}

func TestAdminRouteParsing(t *testing.T) {
	t.Parallel()
	base, parts := adminRoute("/admin/extension-apps/cannon-ext-sms-alert/lists/3/messages")
	if base == "" || len(parts) != 3 || parts[0] != "lists" || parts[2] != "messages" {
		t.Fatalf("base=%q parts=%v", base, parts)
	}
}
