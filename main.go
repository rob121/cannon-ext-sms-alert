package main

import (
	"embed"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/rob121/cannon/extension"
	"gorm.io/gorm"
)

//go:embed help/*.md
var helpFiles embed.FS

//go:embed all:templates
var templateFiles embed.FS

const (
	extensionName        = "cannon-ext-sms-alert"
	extensionTitle       = "SMS Alerts"
	extensionDescription = "Keyword SMS list subscribe/unsubscribe and broadcasts via Twilio, with Cannon admin and authorized-sender conversations."
	extensionVersion     = "0.1.0"
	updateURLBase        = "https://github.com/rob121/cannon-ext-sms-alert/releases/download"
	defaultAdminMenu     = "SMS Alerts"
	pageSize             = 25
	conversationTTLMins  = 30
)

var smsConfigSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "dry_run": {
      "type": "boolean",
      "title": "Dry run (log only)",
      "description": "When enabled, outbound SMS are logged and not sent. Defaults to on while Development Mode is enabled if left unset.",
      "default": true
    },
    "log_retention_messages": {
      "type": "integer",
      "title": "Keep recent messages",
      "description": "Keep only the newest N broadcast messages and their delivery logs. Older messages are deleted. Set 0 to disable cleanup.",
      "default": 100,
      "minimum": 0
    },
    "log_cleanup_hours": {
      "type": "integer",
      "title": "Cleanup interval (hours)",
      "description": "How often automatic delivery-log cleanup runs in the background. Default 24. Manual cleanup is always available from the extension home.",
      "default": 24,
      "minimum": 1
    },
    "account_sid_test": {
      "type": "string",
      "title": "Account SID (Test)",
      "description": "Used when Cannon Development Mode is on."
    },
    "auth_token_test": {
      "type": "string",
      "title": "Auth Token (Test)",
      "description": "Twilio auth token for test credentials."
    },
    "from_number_test": {
      "type": "string",
      "title": "Default From Number (Test)",
      "description": "E.164 fallback when a list has no number assigned."
    },
    "account_sid_live": {
      "type": "string",
      "title": "Account SID (Live)",
      "description": "Used when Cannon Development Mode is off."
    },
    "auth_token_live": {
      "type": "string",
      "title": "Auth Token (Live)"
    },
    "from_number_live": {
      "type": "string",
      "title": "Default From Number (Live)",
      "description": "E.164 fallback when a list has no number assigned."
    }
  }
}`)

var smsConfigUI = json.RawMessage(`{
  "type": "VerticalLayout",
  "elements": [
    {"type": "Control", "scope": "#/properties/dry_run"},
    {"type": "Group", "label": "Delivery log retention", "elements": [
      {"type": "Control", "scope": "#/properties/log_retention_messages"},
      {"type": "Control", "scope": "#/properties/log_cleanup_hours"}
    ]},
    {"type": "Group", "label": "Test Credentials (Development Mode)", "elements": [
      {"type": "Control", "scope": "#/properties/account_sid_test"},
      {"type": "Control", "scope": "#/properties/auth_token_test"},
      {"type": "Control", "scope": "#/properties/from_number_test"}
    ]},
    {"type": "Group", "label": "Live Credentials", "elements": [
      {"type": "Control", "scope": "#/properties/account_sid_live"},
      {"type": "Control", "scope": "#/properties/auth_token_live"},
      {"type": "Control", "scope": "#/properties/from_number_live"}
    ]}
  ]
}`)

type smsApp struct {
	info               extension.Info
	templates          *extension.Templates
	settingsTable      string
	numbersTable       string
	listsTable         string
	subscribersTable   string
	sendersTable       string
	senderListsTable   string
	messagesTable      string
	messageLogsTable   string
	conversationsTable string

	schemaOnce sync.Once
	schemaErr  error

	cleanupMu   sync.Mutex
	lastCleanup time.Time
}

type smsSettings struct {
	DryRun               *bool  `json:"dry_run"`
	LogRetentionMessages *int   `json:"log_retention_messages"`
	LogCleanupHours      *int   `json:"log_cleanup_hours"`
	AccountSIDTest       string `json:"account_sid_test"`
	AuthTokenTest        string `json:"auth_token_test"`
	FromNumberTest       string `json:"from_number_test"`
	AccountSIDLive       string `json:"account_sid_live"`
	AuthTokenLive        string `json:"auth_token_live"`
	FromNumberLive       string `json:"from_number_live"`
}

func main() {
	if err := newServer().Run(); err != nil {
		log.Fatal(err)
	}
}

func newServer() *extension.Server {
	app, s := newAppServer()
	app.startCleanupLoop()
	return s
}

func newAppServer() (*smsApp, *extension.Server) {
	info := extension.Info{
		Name:          extensionName,
		Title:         extensionTitle,
		Description:   extensionDescription,
		Version:       extensionVersion,
		UpdateURLBase: updateURLBase,
		AdminMenuName: defaultAdminMenu,
	}
	prefix := info.TablePrefix()
	app := &smsApp{
		info:               info,
		templates:          extension.NewTemplates(templateFiles, "templates"),
		settingsTable:      extension.TableName(prefix, "settings"),
		numbersTable:       extension.TableName(prefix, "numbers"),
		listsTable:         extension.TableName(prefix, "lists"),
		subscribersTable:   extension.TableName(prefix, "subscribers"),
		sendersTable:       extension.TableName(prefix, "senders"),
		senderListsTable:   extension.TableName(prefix, "sender_lists"),
		messagesTable:      extension.TableName(prefix, "messages"),
		messageLogsTable:   extension.TableName(prefix, "message_logs"),
		conversationsTable: extension.TableName(prefix, "conversations"),
	}

	s := extension.New(info)
	s.EmbedHelp(helpFiles, "help")
	s.EmbedTemplates(templateFiles, "templates")
	s.OnInstall(app.install)
	s.RegisterPermissions([]extension.PermissionDef{
		{ID: "sms-alert.manage", DisplayName: "Manage SMS alerts", Description: "Manage lists, numbers, senders, and subscribers.", Category: "sms-alert"},
		{ID: "sms-alert.send", DisplayName: "Send SMS alerts", Description: "Compose and send SMS broadcasts.", Category: "sms-alert"},
	})
	s.OnConfiguration(extension.MapConfiguration([]extension.ConfigurationDefinition{
		{ID: "general", Title: "Twilio", Schema: smsConfigSchema, UISchema: smsConfigUI},
	}, configStore{table: app.settingsTable, app: app}))
	s.RegisterEndpoint(extension.EndpointDefinition{
		ID:    "twilio-inbound",
		Title: "Twilio Inbound Webhook",
	}, app.twilioWebhook)
	s.HandleAdmin("/admin/", app.admin)
	return app, s
}

func (app *smsApp) install(_ extension.WireRequest) error {
	db, _, err := extension.OpenDB()
	if err != nil {
		return err
	}
	return app.ensureSchema(db)
}

type configStore struct {
	table string
	app   *smsApp
}

func (c configStore) Load(section string) (map[string]any, error) {
	db, err := c.app.db()
	if err != nil {
		return nil, err
	}
	if err := c.app.ensureSchema(db); err != nil {
		return nil, err
	}
	return extension.DBConfigurationStore(db, c.table).Load(section)
}

func (c configStore) Save(section string, data map[string]any) error {
	db, err := c.app.db()
	if err != nil {
		return err
	}
	if err := c.app.ensureSchema(db); err != nil {
		return err
	}
	return extension.DBConfigurationStore(db, c.table).Save(section, data)
}

func (app *smsApp) db() (*gorm.DB, error) {
	db, _, err := extension.OpenDB()
	return db, err
}
