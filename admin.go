package main

import (
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rob121/cannon/extension"
	"gorm.io/gorm"
)

func (app *smsApp) admin(req extension.WireRequest) extension.WireResponse {
	if !extension.UserAuthenticated(req) {
		return extension.Error(http.StatusForbidden, "sign in required")
	}
	canManage := adminCanManage(req)
	canSend := adminCanSend(req)
	if !canManage && !canSend {
		return extension.Error(http.StatusForbidden, "permission denied")
	}

	db, err := app.db()
	if err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	if err := app.ensureSchema(db); err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}

	adminURL, err := parseWireURL(req.URL)
	if err != nil {
		return extension.Error(http.StatusBadRequest, err.Error())
	}
	basePath, parts := adminRoute(adminURL.Path)
	if basePath == "" {
		basePath = adminHomeURL()
	}
	csrf := csrfField(req)
	notice := adminURL.Query().Get("notice")

	switch {
	case len(parts) == 0:
		return app.adminHome(db, adminURL, basePath, csrf, canManage, canSend)

	case len(parts) == 1 && parts[0] == "compose":
		if !canSend {
			return extension.Error(http.StatusForbidden, "permission denied")
		}
		if !strings.EqualFold(req.Method, http.MethodPost) {
			return extension.Error(http.StatusMethodNotAllowed, "method not allowed")
		}
		return app.startCompose(db, req.Body, basePath, csrf)

	case len(parts) == 1 && parts[0] == "cleanup-logs":
		if !canManage {
			return extension.Error(http.StatusForbidden, "permission denied")
		}
		if !strings.EqualFold(req.Method, http.MethodPost) {
			return extension.Error(http.StatusMethodNotAllowed, "method not allowed")
		}
		deleted, _, err := app.maybeCleanupLogs(true)
		if err != nil {
			return extension.Redirect(http.StatusSeeOther, basePath+"?error="+url.QueryEscape("Cleanup failed: "+err.Error()))
		}
		notice = fmt.Sprintf("Delivery log cleanup finished. Removed %d old message(s).", deleted)
		return extension.Redirect(http.StatusSeeOther, basePath+"?notice="+url.QueryEscape(notice))

	// Numbers
	case len(parts) == 1 && parts[0] == "numbers":
		return app.listNumbers(db, basePath, notice, csrf, canManage)
	case len(parts) == 2 && parts[0] == "numbers" && parts[1] == "new":
		if !canManage {
			return extension.Error(http.StatusForbidden, "permission denied")
		}
		if strings.EqualFold(req.Method, http.MethodPost) {
			return app.saveNumber(db, req.Body, basePath, 0, csrf)
		}
		return app.renderNumberForm(basePath, numberRow{Active: true}, true, "", csrf)
	case len(parts) == 2 && parts[0] == "numbers":
		id, ok := parseID(parts[1])
		if !ok {
			return extension.Error(http.StatusNotFound, "number not found")
		}
		if strings.EqualFold(req.Method, http.MethodPost) {
			if !canManage {
				return extension.Error(http.StatusForbidden, "permission denied")
			}
			return app.saveNumber(db, req.Body, basePath, id, csrf)
		}
		return app.editNumber(db, basePath, id, "", csrf)
	case len(parts) == 3 && parts[0] == "numbers" && parts[2] == "delete":
		if !canManage {
			return extension.Error(http.StatusForbidden, "permission denied")
		}
		id, ok := parseID(parts[1])
		if !ok {
			return extension.Error(http.StatusNotFound, "number not found")
		}
		if !strings.EqualFold(req.Method, http.MethodPost) {
			return extension.Error(http.StatusMethodNotAllowed, "method not allowed")
		}
		if err := db.Table(app.numbersTable).Where("number_id = ?", id).Delete(&numberRow{}).Error; err != nil {
			return extension.Error(http.StatusInternalServerError, err.Error())
		}
		return app.listNumbers(db, basePath, "Number deleted.", csrf, canManage)

	// Lists
	case len(parts) == 1 && parts[0] == "lists":
		return app.listLists(db, basePath, notice, csrf, canManage, canSend)
	case len(parts) == 2 && parts[0] == "lists" && parts[1] == "new":
		if !canManage {
			return extension.Error(http.StatusForbidden, "permission denied")
		}
		if strings.EqualFold(req.Method, http.MethodPost) {
			return app.saveList(db, req.Body, basePath, 0, csrf)
		}
		return app.renderListForm(db, basePath, listRow{}, true, "", csrf)
	case len(parts) == 2 && parts[0] == "lists":
		id, ok := parseID(parts[1])
		if !ok {
			return extension.Error(http.StatusNotFound, "list not found")
		}
		if strings.EqualFold(req.Method, http.MethodPost) {
			if !canManage {
				return extension.Error(http.StatusForbidden, "permission denied")
			}
			return app.saveList(db, req.Body, basePath, id, csrf)
		}
		return app.editList(db, basePath, id, "", csrf)
	case len(parts) == 3 && parts[0] == "lists" && parts[2] == "delete":
		if !canManage {
			return extension.Error(http.StatusForbidden, "permission denied")
		}
		id, ok := parseID(parts[1])
		if !ok {
			return extension.Error(http.StatusNotFound, "list not found")
		}
		if !strings.EqualFold(req.Method, http.MethodPost) {
			return extension.Error(http.StatusMethodNotAllowed, "method not allowed")
		}
		if err := app.deleteList(db, id); err != nil {
			return extension.Error(http.StatusInternalServerError, err.Error())
		}
		return app.listLists(db, basePath, "List deleted.", csrf, canManage, canSend)

	// Subscribers for a list
	case len(parts) == 3 && parts[0] == "lists" && parts[2] == "subscribers":
		listID, ok := parseID(parts[1])
		if !ok {
			return extension.Error(http.StatusNotFound, "list not found")
		}
		return app.listSubscribers(db, basePath, listID, notice, csrf, canManage)
	case len(parts) == 4 && parts[0] == "lists" && parts[2] == "subscribers" && parts[3] == "new":
		if !canManage {
			return extension.Error(http.StatusForbidden, "permission denied")
		}
		listID, ok := parseID(parts[1])
		if !ok {
			return extension.Error(http.StatusNotFound, "list not found")
		}
		if strings.EqualFold(req.Method, http.MethodPost) {
			return app.saveSubscriber(db, req.Body, basePath, listID, csrf)
		}
		return app.renderSubscriberForm(db, basePath, listID, "", csrf)
	case len(parts) == 5 && parts[0] == "lists" && parts[2] == "subscribers" && parts[4] == "delete":
		if !canManage {
			return extension.Error(http.StatusForbidden, "permission denied")
		}
		listID, ok := parseID(parts[1])
		if !ok {
			return extension.Error(http.StatusNotFound, "list not found")
		}
		subID, ok := parseID(parts[3])
		if !ok {
			return extension.Error(http.StatusNotFound, "subscriber not found")
		}
		if !strings.EqualFold(req.Method, http.MethodPost) {
			return extension.Error(http.StatusMethodNotAllowed, "method not allowed")
		}
		var sub subscriberRow
		if err := db.Table(app.subscribersTable).Where("subscriber_id = ? AND list_id = ?", subID, listID).First(&sub).Error; err != nil {
			return extension.Error(http.StatusNotFound, "subscriber not found")
		}
		_ = app.removeSubscriber(db, listID, sub.Phone)
		return app.listSubscribers(db, basePath, listID, "Subscriber removed.", csrf, canManage)

	// Messages
	case len(parts) == 3 && parts[0] == "lists" && parts[2] == "messages":
		listID, ok := parseID(parts[1])
		if !ok {
			return extension.Error(http.StatusNotFound, "list not found")
		}
		return app.listMessages(db, basePath, listID, notice, csrf, canSend)
	case len(parts) == 4 && parts[0] == "lists" && parts[2] == "messages" && parts[3] == "new":
		if !canSend {
			return extension.Error(http.StatusForbidden, "permission denied")
		}
		listID, ok := parseID(parts[1])
		if !ok {
			return extension.Error(http.StatusNotFound, "list not found")
		}
		if strings.EqualFold(req.Method, http.MethodPost) {
			return app.composeAndSend(db, req, basePath, listID, csrf)
		}
		return app.renderComposeForm(db, basePath, listID, "", "", "", csrf)
	case len(parts) == 4 && parts[0] == "lists" && parts[2] == "messages":
		listID, ok := parseID(parts[1])
		if !ok {
			return extension.Error(http.StatusNotFound, "list not found")
		}
		msgID, ok := parseID(parts[3])
		if !ok {
			return extension.Error(http.StatusNotFound, "message not found")
		}
		return app.messageDetail(db, basePath, listID, msgID, notice, csrf)

	// Senders
	case len(parts) == 1 && parts[0] == "senders":
		return app.listSenders(db, basePath, notice, csrf, canManage)
	case len(parts) == 2 && parts[0] == "senders" && parts[1] == "new":
		if !canManage {
			return extension.Error(http.StatusForbidden, "permission denied")
		}
		if strings.EqualFold(req.Method, http.MethodPost) {
			return app.saveSender(db, req.Body, basePath, 0, csrf)
		}
		return app.renderSenderForm(db, basePath, senderRow{CanSend: true, Status: "active"}, nil, true, "", csrf)
	case len(parts) == 2 && parts[0] == "senders":
		id, ok := parseID(parts[1])
		if !ok {
			return extension.Error(http.StatusNotFound, "sender not found")
		}
		if strings.EqualFold(req.Method, http.MethodPost) {
			if !canManage {
				return extension.Error(http.StatusForbidden, "permission denied")
			}
			return app.saveSender(db, req.Body, basePath, id, csrf)
		}
		return app.editSender(db, basePath, id, "", csrf)
	case len(parts) == 3 && parts[0] == "senders" && parts[2] == "delete":
		if !canManage {
			return extension.Error(http.StatusForbidden, "permission denied")
		}
		id, ok := parseID(parts[1])
		if !ok {
			return extension.Error(http.StatusNotFound, "sender not found")
		}
		if !strings.EqualFold(req.Method, http.MethodPost) {
			return extension.Error(http.StatusMethodNotAllowed, "method not allowed")
		}
		_ = db.Table(app.senderListsTable).Where("sender_id = ?", id).Delete(&senderListRow{}).Error
		if err := db.Table(app.sendersTable).Where("sender_id = ?", id).Delete(&senderRow{}).Error; err != nil {
			return extension.Error(http.StatusInternalServerError, err.Error())
		}
		return app.listSenders(db, basePath, "Sender deleted.", csrf, canManage)

	default:
		return extension.Error(http.StatusNotFound, "admin route not found")
	}
}

func (app *smsApp) adminHome(db *gorm.DB, adminURL *url.URL, basePath string, csrf template.HTML, canManage, canSend bool) extension.WireResponse {
	var listTotal, numberTotal, senderTotal, subscriberTotal, messageTotal int64
	_ = db.Table(app.listsTable).Count(&listTotal).Error
	_ = db.Table(app.numbersTable).Count(&numberTotal).Error
	_ = db.Table(app.sendersTable).Count(&senderTotal).Error
	_ = db.Table(app.subscribersTable).Count(&subscriberTotal).Error
	_ = db.Table(app.messagesTable).Count(&messageTotal).Error
	var lists []listRow
	_ = db.Table(app.listsTable).Order("name asc").Find(&lists).Error
	cfg, _ := app.loadSettings()
	keep := cfg.retentionMessages()
	go app.maybeCleanupLogs(false)
	return app.render("admin/home.html", map[string]any{
		"Title":           "SMS Alerts",
		"Subtitle":        "Keyword subscribe lists and Twilio broadcasts.",
		"BasePath":        basePath,
		"SettingsURL":     "/admin/configuration/extensions/" + url.PathEscape(extensionName),
		"RoutesURL":       "/admin/routes",
		"NewRouteURL":     "/admin/routes/new",
		"WebhookPath":     "/webhooks/sms-alert",
		"Lists":           lists,
		"ListTotal":       listTotal,
		"NumberTotal":     numberTotal,
		"SenderTotal":     senderTotal,
		"SubscriberTotal": subscriberTotal,
		"MessageTotal":    messageTotal,
		"LogRetention":    keep,
		"CanManage":       canManage,
		"CanSend":         canSend,
		"Notice":          adminURL.Query().Get("notice"),
		"Error":           adminURL.Query().Get("error"),
		"CSRFField":       csrf,
	})
}

func (app *smsApp) startCompose(db *gorm.DB, body, basePath string, csrf template.HTML) extension.WireResponse {
	values, err := formValues(body)
	if err != nil {
		return extension.Redirect(http.StatusSeeOther, basePath+"?error="+url.QueryEscape("invalid form data"))
	}
	listID, ok := parseID(values.Get("list_id"))
	if !ok {
		return extension.Redirect(http.StatusSeeOther, basePath+"?error="+url.QueryEscape("Choose a list to compose."))
	}
	if _, err := app.loadList(db, listID); err != nil {
		return extension.Redirect(http.StatusSeeOther, basePath+"?error="+url.QueryEscape("list not found"))
	}
	_ = csrf
	return extension.Redirect(http.StatusSeeOther, fmt.Sprintf("%s/lists/%d/messages/new", basePath, listID))
}

func (app *smsApp) listNumbers(db *gorm.DB, basePath, notice string, csrf template.HTML, canManage bool) extension.WireResponse {
	var rows []numberRow
	if err := db.Table(app.numbersTable).Order("label asc").Find(&rows).Error; err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	return app.render("admin/numbers.html", map[string]any{
		"Title":     "From Numbers",
		"BasePath":  basePath,
		"Rows":      rows,
		"Notice":    notice,
		"CanManage": canManage,
		"CSRFField": csrf,
	})
}

func (app *smsApp) editNumber(db *gorm.DB, basePath string, id uint, errMsg string, csrf template.HTML) extension.WireResponse {
	var row numberRow
	if err := db.Table(app.numbersTable).Where("number_id = ?", id).First(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return extension.Error(http.StatusNotFound, "number not found")
		}
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	return app.renderNumberForm(basePath, row, false, errMsg, csrf)
}

func (app *smsApp) renderNumberForm(basePath string, row numberRow, isNew bool, errMsg string, csrf template.HTML) extension.WireResponse {
	title := "Edit Number"
	action := fmt.Sprintf("%s/numbers/%d", basePath, row.NumberID)
	if isNew {
		title = "Add Number"
		action = basePath + "/numbers/new"
	}
	return app.render("admin/number_form.html", map[string]any{
		"Title":     title,
		"BasePath":  basePath,
		"ActionURL": action,
		"CancelURL": basePath + "/numbers",
		"Row":       row,
		"IsNew":     isNew,
		"Error":     errMsg,
		"CSRFField": csrf,
	})
}

func (app *smsApp) saveNumber(db *gorm.DB, body, basePath string, id uint, csrf template.HTML) extension.WireResponse {
	values, err := formValues(body)
	if err != nil {
		return app.renderNumberForm(basePath, numberRow{NumberID: id}, id == 0, "invalid form data", csrf)
	}
	label := strings.TrimSpace(values.Get("label"))
	raw := strings.TrimSpace(values.Get("number"))
	active := values.Get("active") == "1" || strings.EqualFold(values.Get("active"), "on")
	phone, err := phoneE164(raw, "US")
	row := numberRow{NumberID: id, Label: label, Number: phone, Active: active}
	if label == "" {
		return app.renderNumberForm(basePath, row, id == 0, "label is required", csrf)
	}
	if err != nil {
		row.Number = raw
		return app.renderNumberForm(basePath, row, id == 0, "invalid phone number (use E.164)", csrf)
	}
	now := time.Now().UTC()
	if id == 0 {
		row.CreatedAt = now
		row.UpdatedAt = now
		if err := db.Table(app.numbersTable).Create(&row).Error; err != nil {
			return app.renderNumberForm(basePath, row, true, err.Error(), csrf)
		}
	} else {
		var existing numberRow
		if err := db.Table(app.numbersTable).Where("number_id = ?", id).First(&existing).Error; err != nil {
			return extension.Error(http.StatusNotFound, "number not found")
		}
		row.CreatedAt = existing.CreatedAt
		row.UpdatedAt = now
		if err := db.Table(app.numbersTable).Save(&row).Error; err != nil {
			return app.renderNumberForm(basePath, row, false, err.Error(), csrf)
		}
	}
	return app.listNumbers(db, basePath, "Number saved.", csrf, true)
}

func (app *smsApp) listLists(db *gorm.DB, basePath, notice string, csrf template.HTML, canManage, canSend bool) extension.WireResponse {
	var rows []listRow
	if err := db.Table(app.listsTable).Order("name asc").Find(&rows).Error; err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	return app.render("admin/lists.html", map[string]any{
		"Title":     "Lists",
		"BasePath":  basePath,
		"Rows":      rows,
		"Notice":    notice,
		"CanManage": canManage,
		"CanSend":   canSend,
		"CSRFField": csrf,
	})
}

func (app *smsApp) editList(db *gorm.DB, basePath string, id uint, errMsg string, csrf template.HTML) extension.WireResponse {
	var row listRow
	if err := db.Table(app.listsTable).Where("list_id = ?", id).First(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return extension.Error(http.StatusNotFound, "list not found")
		}
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	return app.renderListForm(db, basePath, row, false, errMsg, csrf)
}

func (app *smsApp) renderListForm(db *gorm.DB, basePath string, row listRow, isNew bool, errMsg string, csrf template.HTML) extension.WireResponse {
	var numbers []numberRow
	_ = db.Table(app.numbersTable).Where("active = ?", true).Order("label asc").Find(&numbers).Error
	title := "Edit List"
	action := fmt.Sprintf("%s/lists/%d", basePath, row.ListID)
	if isNew {
		title = "Add List"
		action = basePath + "/lists/new"
	}
	return app.render("admin/list_form.html", map[string]any{
		"Title":     title,
		"BasePath":  basePath,
		"ActionURL": action,
		"CancelURL": basePath + "/lists",
		"Row":       row,
		"Numbers":   numbers,
		"IsNew":     isNew,
		"Error":     errMsg,
		"CSRFField": csrf,
	})
}

func (app *smsApp) saveList(db *gorm.DB, body, basePath string, id uint, csrf template.HTML) extension.WireResponse {
	values, err := formValues(body)
	if err != nil {
		return app.renderListForm(db, basePath, listRow{ListID: id}, id == 0, "invalid form data", csrf)
	}
	name := strings.TrimSpace(values.Get("name"))
	label := strings.ToLower(strings.TrimSpace(values.Get("label")))
	label = strings.ReplaceAll(label, " ", "")
	numberID, _ := strconv.ParseUint(strings.TrimSpace(values.Get("number_id")), 10, 64)
	row := listRow{
		ListID:       id,
		Name:         name,
		Label:        label,
		Description:  strings.TrimSpace(values.Get("description")),
		NumberID:     uint(numberID),
		SubscribeMsg: strings.TrimSpace(values.Get("subscribe_msg")),
		RemoveMsg:    strings.TrimSpace(values.Get("remove_msg")),
	}
	if name == "" {
		return app.renderListForm(db, basePath, row, id == 0, "name is required", csrf)
	}
	if label == "" {
		return app.renderListForm(db, basePath, row, id == 0, "keyword/label is required", csrf)
	}
	var dup listRow
	q := db.Table(app.listsTable).Where("LOWER(label) = ?", label)
	if id > 0 {
		q = q.Where("list_id <> ?", id)
	}
	if err := q.First(&dup).Error; err == nil {
		return app.renderListForm(db, basePath, row, id == 0, "keyword/label already in use", csrf)
	}
	now := time.Now().UTC()
	if id == 0 {
		row.CreatedAt = now
		row.UpdatedAt = now
		if row.SubscribeMsg == "" {
			row.SubscribeMsg = defaultSubscribeMsg(name)
		}
		if row.RemoveMsg == "" {
			row.RemoveMsg = defaultRemoveMsg(name)
		}
		if err := db.Table(app.listsTable).Create(&row).Error; err != nil {
			return app.renderListForm(db, basePath, row, true, err.Error(), csrf)
		}
	} else {
		var existing listRow
		if err := db.Table(app.listsTable).Where("list_id = ?", id).First(&existing).Error; err != nil {
			return extension.Error(http.StatusNotFound, "list not found")
		}
		row.SubscribersCt = existing.SubscribersCt
		row.CreatedAt = existing.CreatedAt
		row.UpdatedAt = now
		if err := db.Table(app.listsTable).Save(&row).Error; err != nil {
			return app.renderListForm(db, basePath, row, false, err.Error(), csrf)
		}
	}
	return app.listLists(db, basePath, "List saved.", csrf, true, true)
}

func (app *smsApp) deleteList(db *gorm.DB, listID uint) error {
	_ = db.Table(app.subscribersTable).Where("list_id = ?", listID).Delete(&subscriberRow{}).Error
	_ = db.Table(app.senderListsTable).Where("list_id = ?", listID).Delete(&senderListRow{}).Error
	var msgIDs []uint
	_ = db.Table(app.messagesTable).Where("list_id = ?", listID).Pluck("message_id", &msgIDs).Error
	if len(msgIDs) > 0 {
		_ = db.Table(app.messageLogsTable).Where("message_id IN ?", msgIDs).Delete(&messageLogRow{}).Error
	}
	_ = db.Table(app.messagesTable).Where("list_id = ?", listID).Delete(&messageRow{}).Error
	return db.Table(app.listsTable).Where("list_id = ?", listID).Delete(&listRow{}).Error
}

func (app *smsApp) listSubscribers(db *gorm.DB, basePath string, listID uint, notice string, csrf template.HTML, canManage bool) extension.WireResponse {
	list, err := app.loadList(db, listID)
	if err != nil {
		return extension.Error(http.StatusNotFound, "list not found")
	}
	var rows []subscriberRow
	if err := db.Table(app.subscribersTable).Where("list_id = ?", listID).Order("created_at desc").Find(&rows).Error; err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	return app.render("admin/subscribers.html", map[string]any{
		"Title":     "Subscribers — " + list.Name,
		"BasePath":  basePath,
		"List":      list,
		"Rows":      rows,
		"Notice":    notice,
		"CanManage": canManage,
		"CSRFField": csrf,
	})
}

func (app *smsApp) renderSubscriberForm(db *gorm.DB, basePath string, listID uint, errMsg string, csrf template.HTML) extension.WireResponse {
	list, err := app.loadList(db, listID)
	if err != nil {
		return extension.Error(http.StatusNotFound, "list not found")
	}
	return app.render("admin/subscriber_form.html", map[string]any{
		"Title":     "Add Subscriber",
		"BasePath":  basePath,
		"List":      list,
		"ActionURL": fmt.Sprintf("%s/lists/%d/subscribers/new", basePath, listID),
		"CancelURL": fmt.Sprintf("%s/lists/%d/subscribers", basePath, listID),
		"Phone":     "",
		"Error":     errMsg,
		"CSRFField": csrf,
	})
}

func (app *smsApp) saveSubscriber(db *gorm.DB, body, basePath string, listID uint, csrf template.HTML) extension.WireResponse {
	list, err := app.loadList(db, listID)
	if err != nil {
		return extension.Error(http.StatusNotFound, "list not found")
	}
	values, err := formValues(body)
	if err != nil {
		return app.renderSubscriberForm(db, basePath, listID, "invalid form data", csrf)
	}
	phone, err := phoneE164(values.Get("phone"), "US")
	if err != nil {
		return app.render("admin/subscriber_form.html", map[string]any{
			"Title":     "Add Subscriber",
			"BasePath":  basePath,
			"List":      list,
			"ActionURL": fmt.Sprintf("%s/lists/%d/subscribers/new", basePath, listID),
			"CancelURL": fmt.Sprintf("%s/lists/%d/subscribers", basePath, listID),
			"Phone":     strings.TrimSpace(values.Get("phone")),
			"Error":     "invalid phone number",
			"CSRFField": csrf,
		})
	}
	added, err := app.subscribePhone(db, list, phone)
	if err != nil {
		return app.renderSubscriberForm(db, basePath, listID, err.Error(), csrf)
	}
	notice := "Subscriber added."
	if !added {
		notice = "Already subscribed."
	}
	return app.listSubscribers(db, basePath, listID, notice, csrf, true)
}

func (app *smsApp) listMessages(db *gorm.DB, basePath string, listID uint, notice string, csrf template.HTML, canSend bool) extension.WireResponse {
	list, err := app.loadList(db, listID)
	if err != nil {
		return extension.Error(http.StatusNotFound, "list not found")
	}
	var rows []messageRow
	if err := db.Table(app.messagesTable).Where("list_id = ?", listID).Order("created_at desc").Limit(100).Find(&rows).Error; err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	return app.render("admin/messages.html", map[string]any{
		"Title":     "Messages — " + list.Name,
		"BasePath":  basePath,
		"List":      list,
		"Rows":      rows,
		"Notice":    notice,
		"CanSend":   canSend,
		"CSRFField": csrf,
	})
}

func (app *smsApp) renderComposeForm(db *gorm.DB, basePath string, listID uint, title, body, errMsg string, csrf template.HTML) extension.WireResponse {
	list, err := app.loadList(db, listID)
	if err != nil {
		return extension.Error(http.StatusNotFound, "list not found")
	}
	return app.render("admin/compose.html", map[string]any{
		"Title":     "Compose — " + list.Name,
		"BasePath":  basePath,
		"List":      list,
		"ActionURL": fmt.Sprintf("%s/lists/%d/messages/new", basePath, listID),
		"CancelURL": fmt.Sprintf("%s/lists/%d/messages", basePath, listID),
		"MsgTitle":  title,
		"Body":      body,
		"Error":     errMsg,
		"CSRFField": csrf,
	})
}

func (app *smsApp) composeAndSend(db *gorm.DB, req extension.WireRequest, basePath string, listID uint, csrf template.HTML) extension.WireResponse {
	values, err := formValues(req.Body)
	if err != nil {
		return app.renderComposeForm(db, basePath, listID, "", "", "invalid form data", csrf)
	}
	title := strings.TrimSpace(values.Get("title"))
	body := strings.TrimSpace(values.Get("body"))
	if body == "" {
		return app.renderComposeForm(db, basePath, listID, title, body, "message body is required", csrf)
	}
	if title == "" {
		title = firstWords(body, 5)
	}
	var createdBy uint
	if uid, ok := extension.UserID(req); ok {
		createdBy = uint(uid)
	}
	msg := messageRow{
		ListID:    listID,
		Title:     title,
		Body:      body,
		CreatedBy: createdBy,
		CreatedAt: time.Now().UTC(),
	}
	if err := db.Table(app.messagesTable).Create(&msg).Error; err != nil {
		return app.renderComposeForm(db, basePath, listID, title, body, err.Error(), csrf)
	}
	if err := app.sendMessage(db, req, msg.MessageID); err != nil {
		return app.listMessages(db, basePath, listID, "Message saved but send failed: "+err.Error(), csrf, true)
	}
	return extension.Redirect(http.StatusSeeOther, fmt.Sprintf("%s/lists/%d/messages/%d?notice=%s", basePath, listID, msg.MessageID, url.QueryEscape("Message sent.")))
}

func (app *smsApp) messageDetail(db *gorm.DB, basePath string, listID, msgID uint, notice string, csrf template.HTML) extension.WireResponse {
	list, err := app.loadList(db, listID)
	if err != nil {
		return extension.Error(http.StatusNotFound, "list not found")
	}
	var msg messageRow
	if err := db.Table(app.messagesTable).Where("message_id = ? AND list_id = ?", msgID, listID).First(&msg).Error; err != nil {
		return extension.Error(http.StatusNotFound, "message not found")
	}
	type logView struct {
		messageLogRow
		Phone string
	}
	var logs []messageLogRow
	_ = db.Table(app.messageLogsTable).Where("message_id = ?", msgID).Order("created_at asc").Find(&logs).Error
	views := make([]logView, 0, len(logs))
	for _, l := range logs {
		v := logView{messageLogRow: l}
		var sub subscriberRow
		if err := db.Table(app.subscribersTable).Where("subscriber_id = ?", l.SubscriberID).First(&sub).Error; err == nil {
			v.Phone = sub.Phone
		}
		views = append(views, v)
	}
	return app.render("admin/message_detail.html", map[string]any{
		"Title":     msg.Title,
		"BasePath":  basePath,
		"List":      list,
		"Message":   msg,
		"Logs":      views,
		"Notice":    notice,
		"CSRFField": csrf,
	})
}

func (app *smsApp) listSenders(db *gorm.DB, basePath, notice string, csrf template.HTML, canManage bool) extension.WireResponse {
	var rows []senderRow
	if err := db.Table(app.sendersTable).Order("name asc, phone asc").Find(&rows).Error; err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	return app.render("admin/senders.html", map[string]any{
		"Title":     "Authorized Senders",
		"BasePath":  basePath,
		"Rows":      rows,
		"Notice":    notice,
		"CanManage": canManage,
		"CSRFField": csrf,
	})
}

func (app *smsApp) editSender(db *gorm.DB, basePath string, id uint, errMsg string, csrf template.HTML) extension.WireResponse {
	var row senderRow
	if err := db.Table(app.sendersTable).Where("sender_id = ?", id).First(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return extension.Error(http.StatusNotFound, "sender not found")
		}
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	allowed, _ := app.senderAllowedListIDs(db, id)
	return app.renderSenderForm(db, basePath, row, allowed, false, errMsg, csrf)
}

func (app *smsApp) renderSenderForm(db *gorm.DB, basePath string, row senderRow, selected []uint, isNew bool, errMsg string, csrf template.HTML) extension.WireResponse {
	var lists []listRow
	_ = db.Table(app.listsTable).Order("name asc").Find(&lists).Error
	sel := map[uint]bool{}
	for _, id := range selected {
		sel[id] = true
	}
	title := "Edit Sender"
	action := fmt.Sprintf("%s/senders/%d", basePath, row.SenderID)
	if isNew {
		title = "Add Sender"
		action = basePath + "/senders/new"
	}
	return app.render("admin/sender_form.html", map[string]any{
		"Title":     title,
		"BasePath":  basePath,
		"ActionURL": action,
		"CancelURL": basePath + "/senders",
		"Row":       row,
		"Lists":     lists,
		"Selected":  sel,
		"IsNew":     isNew,
		"Error":     errMsg,
		"CSRFField": csrf,
	})
}

func (app *smsApp) saveSender(db *gorm.DB, body, basePath string, id uint, csrf template.HTML) extension.WireResponse {
	values, err := formValues(body)
	if err != nil {
		return app.renderSenderForm(db, basePath, senderRow{SenderID: id}, nil, id == 0, "invalid form data", csrf)
	}
	phone, err := phoneE164(values.Get("phone"), "US")
	name := strings.TrimSpace(values.Get("name"))
	canSend := values.Get("can_send") == "1" || strings.EqualFold(values.Get("can_send"), "on")
	status := strings.TrimSpace(values.Get("status"))
	if status == "" {
		status = "active"
	}
	var userID *uint
	if raw := strings.TrimSpace(values.Get("user_id")); raw != "" {
		if n, err := strconv.ParseUint(raw, 10, 64); err == nil && n > 0 {
			u := uint(n)
			userID = &u
		}
	}
	listIDs := parseUintMulti(values["list_id"])
	row := senderRow{
		SenderID: id,
		Phone:    phone,
		Name:     name,
		CanSend:  canSend,
		UserID:   userID,
		Status:   status,
	}
	if err != nil {
		row.Phone = strings.TrimSpace(values.Get("phone"))
		return app.renderSenderForm(db, basePath, row, listIDs, id == 0, "invalid phone number", csrf)
	}
	now := time.Now().UTC()
	if id == 0 {
		row.CreatedAt = now
		row.UpdatedAt = now
		if err := db.Table(app.sendersTable).Create(&row).Error; err != nil {
			return app.renderSenderForm(db, basePath, row, listIDs, true, err.Error(), csrf)
		}
		id = row.SenderID
	} else {
		var existing senderRow
		if err := db.Table(app.sendersTable).Where("sender_id = ?", id).First(&existing).Error; err != nil {
			return extension.Error(http.StatusNotFound, "sender not found")
		}
		row.CreatedAt = existing.CreatedAt
		row.UpdatedAt = now
		if err := db.Table(app.sendersTable).Save(&row).Error; err != nil {
			return app.renderSenderForm(db, basePath, row, listIDs, false, err.Error(), csrf)
		}
	}
	if err := app.replaceSenderLists(db, id, listIDs); err != nil {
		return app.renderSenderForm(db, basePath, row, listIDs, false, err.Error(), csrf)
	}
	return app.listSenders(db, basePath, "Sender saved.", csrf, true)
}

func (app *smsApp) loadList(db *gorm.DB, id uint) (listRow, error) {
	var row listRow
	err := db.Table(app.listsTable).Where("list_id = ?", id).First(&row).Error
	return row, err
}

func (app *smsApp) render(name string, data map[string]any) extension.WireResponse {
	body, err := app.templates.WithFuncs(template.FuncMap{
		"selected": func(m map[uint]bool, id uint) bool {
			return m[id]
		},
	}).Execute(name, data)
	if err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	return extension.HTML(http.StatusOK, body)
}

func adminCanManage(req extension.WireRequest) bool {
	if !userHasPermissions(req) {
		return true
	}
	return extension.UserCan(req, permSMSManage)
}

func adminCanSend(req extension.WireRequest) bool {
	if !userHasPermissions(req) {
		return true
	}
	return extension.UserCan(req, permSMSSend) || extension.UserCan(req, permSMSManage)
}

func userHasPermissions(req extension.WireRequest) bool {
	if req.User == nil {
		return false
	}
	perms, ok := req.User["permissions"].([]any)
	return ok && len(perms) > 0
}

func csrfField(req extension.WireRequest) template.HTML {
	return template.HTML(extension.CSRFHiddenField(req))
}

func parseWireURL(raw string) (*url.URL, error) {
	return url.Parse(raw)
}

func adminHomeURL() string {
	return "/admin/extension-apps/" + url.PathEscape(extensionName)
}

func adminRoute(path string) (base string, parts []string) {
	path = strings.TrimSpace(path)
	marker := "/admin/extension-apps/" + extensionName
	idx := strings.Index(path, marker)
	if idx < 0 {
		return "", nil
	}
	base = path[:idx+len(marker)]
	rest := strings.Trim(path[idx+len(marker):], "/")
	if rest == "" {
		return base, nil
	}
	return base, strings.Split(rest, "/")
}

func parseID(s string) (uint, bool) {
	n, err := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	if err != nil || n == 0 {
		return 0, false
	}
	return uint(n), true
}

func formValues(body string) (url.Values, error) {
	return url.ParseQuery(body)
}

func parseUintMulti(vals []string) []uint {
	out := make([]uint, 0, len(vals))
	seen := map[uint]bool{}
	for _, v := range vals {
		n, err := strconv.ParseUint(strings.TrimSpace(v), 10, 64)
		if err != nil || n == 0 || seen[uint(n)] {
			continue
		}
		seen[uint(n)] = true
		out = append(out, uint(n))
	}
	return out
}
