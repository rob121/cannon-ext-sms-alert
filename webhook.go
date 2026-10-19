package main

import (
	"net/http"
	"strings"
	"time"

	"github.com/rob121/cannon/extension"
	"gorm.io/gorm"
)

const (
	convoKindRemove  = "remove"
	convoKindMsgSend = "msgsend"
	convoStateStart  = "started"
	convoStateWait   = "waitingformsg"
)

func (app *smsApp) twilioWebhook(_ string, req extension.WireRequest) extension.WireResponse {
	if !strings.EqualFold(req.Method, http.MethodPost) {
		return extension.Error(http.StatusMethodNotAllowed, "POST required")
	}
	db, err := app.db()
	if err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	if err := app.ensureSchema(db); err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	creds, err := app.activeCredentials(req)
	if err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	form, err := parseFormBody(req.Body)
	if err != nil {
		return extension.Error(http.StatusBadRequest, "invalid form body")
	}
	sig := headerGet(req, "X-Twilio-Signature")
	if !verifyTwilioSignature(creds.AuthToken, webhookPublicURL(req), sig, form) {
		return extension.Error(http.StatusForbidden, "invalid Twilio signature")
	}

	from, err := phoneE164(form.Get("From"), "US")
	if err != nil {
		return twimlResponse("Invalid phone number.")
	}
	msg := strings.ToLower(strings.TrimSpace(form.Get("Body")))
	if msg == "" {
		return twimlResponse("Empty message.")
	}
	ttl := time.Duration(conversationTTLMins) * time.Minute

	if msg == "remove" {
		return app.handleRemoveStart(db, from, ttl)
	}
	if row, ok, _ := app.getConversation(db, from, convoKindRemove); ok && row.State == convoStateStart {
		return app.handleRemovePick(db, from, msg, ttl)
	}

	sender, senderErr := app.findSenderByPhone(db, from)
	isSender := senderErr == nil && sender.SenderID > 0
	if isSender {
		if resp, handled := app.handleSenderConversation(db, req, from, msg, sender, ttl); handled {
			return resp
		}
	}

	list, err := app.findListByLabel(db, msg)
	if err == nil {
		_, _ = app.subscribePhone(db, list, from)
		text := strings.TrimSpace(list.SubscribeMsg)
		if text == "" {
			text = defaultSubscribeMsg(list.Name)
		}
		return twimlResponse(text + "\nText 'remove' to remove yourself from the list")
	}
	if !isSender {
		return twimlResponse("We were unable to find your list (" + msg + "), please try again")
	}
	return twimlResponse("OK")
}

func (app *smsApp) handleRemoveStart(db *gorm.DB, from string, ttl time.Duration) extension.WireResponse {
	lists, err := app.listsForPhone(db, from)
	if err != nil {
		return twimlResponse("Sorry, something went wrong.")
	}
	if len(lists) == 0 {
		return twimlResponse("You are not subscribed to any lists.")
	}
	if len(lists) == 1 {
		_ = app.removeSubscriber(db, lists[0].ListID, from)
		text := strings.TrimSpace(lists[0].RemoveMsg)
		if text == "" {
			text = defaultRemoveMsg(lists[0].Name)
		}
		return twimlResponse(text)
	}
	labels := make([]string, 0, len(lists)+1)
	for _, l := range lists {
		labels = append(labels, l.Label)
	}
	labels = append(labels, "all")
	_ = app.setConversation(db, from, convoKindRemove, convoStateStart, "", ttl)
	return twimlResponse("Which list do you want to be removed from: " + strings.Join(labels, ", "))
}

func (app *smsApp) handleRemovePick(db *gorm.DB, from, msg string, ttl time.Duration) extension.WireResponse {
	_ = ttl
	lists, err := app.listsForPhone(db, from)
	if err != nil {
		return twimlResponse("Sorry, something went wrong.")
	}
	if msg == "all" {
		names, err := app.removeFromAllLists(db, from)
		_ = app.clearConversation(db, from, convoKindRemove)
		if err != nil {
			return twimlResponse("Sorry, something went wrong.")
		}
		return twimlResponse("You have been removed from " + strings.Join(names, ", "))
	}
	for _, l := range lists {
		if strings.EqualFold(l.Label, msg) {
			_ = app.removeSubscriber(db, l.ListID, from)
			_ = app.clearConversation(db, from, convoKindRemove)
			text := strings.TrimSpace(l.RemoveMsg)
			if text == "" {
				text = defaultRemoveMsg(l.Name)
			}
			return twimlResponse(text)
		}
	}
	return twimlResponse("We don't recognize that list. Reply with a label or 'all'.")
}

func (app *smsApp) handleSenderConversation(db *gorm.DB, req extension.WireRequest, from, msg string, sender senderRow, ttl time.Duration) (extension.WireResponse, bool) {
	if msg == "endconvo" {
		_ = app.clearConversation(db, from, convoKindMsgSend)
		return twimlResponse("Conversation Ended"), true
	}

	convo, ok, err := app.getConversation(db, from, convoKindMsgSend)
	if err != nil {
		return twimlResponse("Sorry, something went wrong."), true
	}

	if ok && convo.State == convoStateStart {
		list, err := app.findListByLabel(db, msg)
		if err != nil {
			return twimlResponse("We don't seem to have that list, please try again"), true
		}
		allowed, _ := app.senderAllowedListIDs(db, sender.SenderID)
		if !containsUint(allowed, list.ListID) {
			return twimlResponse("You are not allowed to send to that list."), true
		}
		payload := strings.TrimSpace(convo.Payload)
		if len(payload) < 2 {
			_ = app.setConversation(db, from, convoKindMsgSend, convoStateWait, msg, ttl)
			return twimlResponse("Give me your message:"), true
		}
		return app.finishSenderSend(db, req, from, list, payload), true
	}

	if ok && convo.State == convoStateWait {
		listLabel := strings.TrimSpace(convo.Payload)
		list, err := app.findListByLabel(db, listLabel)
		if err != nil {
			_ = app.clearConversation(db, from, convoKindMsgSend)
			return twimlResponse("We lost the list selection. Start over by texting your message."), true
		}
		// Body is the original message text (preserve case from form).
		body := strings.TrimSpace(req.Body)
		if vals, err := parseFormBody(req.Body); err == nil {
			body = strings.TrimSpace(vals.Get("Body"))
		}
		return app.finishSenderSend(db, req, from, list, body), true
	}

	// Start conversation: inbound body is the broadcast message draft.
	allowedLists, err := app.senderListLabels(db, sender.SenderID)
	if err != nil {
		return twimlResponse("Sorry, something went wrong."), true
	}
	if len(allowedLists) == 0 {
		return twimlResponse("Hi! You are authorized to send a message, however, we found no assigned lists"), true
	}
	labels := make([]string, 0, len(allowedLists))
	for _, l := range allowedLists {
		labels = append(labels, l.Label)
	}
	body := strings.TrimSpace(msg)
	if vals, err := parseFormBody(req.Body); err == nil {
		if v := strings.TrimSpace(vals.Get("Body")); v != "" {
			body = v
		}
	}
	_ = app.setConversation(db, from, convoKindMsgSend, convoStateStart, body, ttl)
	return twimlResponse("Hi! You are authorized to send a message, what list would you like to send to?\n\n" +
		strings.Join(labels, "\n") + "\n\nIf this was sent by mistake, send 'endconvo'"), true
}

func (app *smsApp) finishSenderSend(db *gorm.DB, req extension.WireRequest, from string, list listRow, body string) extension.WireResponse {
	body = strings.TrimSpace(body)
	if body == "" {
		return twimlResponse("Message was empty.")
	}
	msg := messageRow{
		ListID:    list.ListID,
		Title:     firstWords(body, 5),
		Body:      body,
		CreatedAt: time.Now().UTC(),
	}
	if err := db.Table(app.messagesTable).Create(&msg).Error; err != nil || msg.MessageID == 0 {
		_ = app.clearConversation(db, from, convoKindMsgSend)
		return twimlResponse("We were unable to send that message")
	}
	app.sendMessageAsync(db, req, msg.MessageID)
	_ = app.clearConversation(db, from, convoKindMsgSend)
	return twimlResponse("Sent!")
}

func containsUint(ids []uint, want uint) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
