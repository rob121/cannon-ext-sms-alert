package main

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/nyaruka/phonenumbers"
	"github.com/rob121/cannon/extension"
)

type twilioCreds struct {
	AccountSID string
	AuthToken  string
	FromNumber string
	DryRun     bool
	Mode       string // "test" or "live"
}

func (app *smsApp) activeCredentials(req extension.WireRequest) (twilioCreds, error) {
	cfg, err := app.loadSettings()
	if err != nil {
		return twilioCreds{}, err
	}
	return credentialsFromSettings(cfg, extension.DevelopmentMode(req))
}

func credentialsFromSettings(cfg smsSettings, developmentMode bool) (twilioCreds, error) {
	out := twilioCreds{Mode: "live"}
	if developmentMode {
		out.Mode = "test"
		out.AccountSID = strings.TrimSpace(cfg.AccountSIDTest)
		out.AuthToken = strings.TrimSpace(cfg.AuthTokenTest)
		out.FromNumber = strings.TrimSpace(cfg.FromNumberTest)
		if out.AccountSID == "" || out.AuthToken == "" {
			return twilioCreds{}, fmt.Errorf("Twilio test credentials are not configured (Development Mode is on)")
		}
	} else {
		out.AccountSID = strings.TrimSpace(cfg.AccountSIDLive)
		out.AuthToken = strings.TrimSpace(cfg.AuthTokenLive)
		out.FromNumber = strings.TrimSpace(cfg.FromNumberLive)
		if out.AccountSID == "" || out.AuthToken == "" {
			return twilioCreds{}, fmt.Errorf("Twilio live credentials are not configured")
		}
	}
	out.DryRun = resolveDryRun(cfg, developmentMode)
	return out, nil
}

func resolveDryRun(cfg smsSettings, developmentMode bool) bool {
	if cfg.DryRun != nil {
		return *cfg.DryRun
	}
	// Default on while Development Mode is enabled.
	return developmentMode
}

func phoneE164(raw, region string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("phone is required")
	}
	if region == "" {
		region = "US"
	}
	num, err := phonenumbers.Parse(raw, region)
	if err != nil {
		return "", err
	}
	if !phonenumbers.IsValidNumber(num) {
		return "", fmt.Errorf("invalid phone number")
	}
	return phonenumbers.Format(num, phonenumbers.E164), nil
}

func twimlMessage(body string) string {
	return `<?xml version="1.0" encoding="UTF-8"?><Response><Message>` + xmlEscape(body) + `</Message></Response>`
}

func twimlResponse(body string) extension.WireResponse {
	return extension.WireResponse{
		StatusCode: http.StatusOK,
		Header: map[string][]string{
			"Content-Type": {"application/xml; charset=utf-8"},
		},
		Body: twimlMessage(body),
	}
}

func xmlEscape(s string) string {
	replacer := strings.NewReplacer(
		`&`, "&amp;",
		`<`, "&lt;",
		`>`, "&gt;",
		`"`, "&quot;",
		`'`, "&apos;",
	)
	return replacer.Replace(s)
}

func verifyTwilioSignature(authToken, fullURL, signature string, params url.Values) bool {
	authToken = strings.TrimSpace(authToken)
	signature = strings.TrimSpace(signature)
	if authToken == "" || signature == "" || fullURL == "" {
		return false
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(fullURL)
	for _, k := range keys {
		b.WriteString(k)
		b.WriteString(params.Get(k))
	}
	mac := hmac.New(sha1.New, []byte(authToken))
	_, _ = mac.Write([]byte(b.String()))
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

func (c twilioCreds) Send(from, to, message string) error {
	fromPhone, err := phoneE164(from, "US")
	if err != nil {
		return fmt.Errorf("from: %w", err)
	}
	toPhone, err := phoneE164(to, "US")
	if err != nil {
		return fmt.Errorf("to: %w", err)
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return fmt.Errorf("message is required")
	}
	if c.DryRun {
		log.Printf("sms-alert dry-run [%s]: from=%s to=%s body=%q", c.Mode, fromPhone, toPhone, message)
		return nil
	}
	form := url.Values{}
	form.Set("To", toPhone)
	form.Set("From", fromPhone)
	form.Set("Body", message)
	endpoint := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Messages.json", c.AccountSID)
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.AccountSID, c.AuthToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("twilio status %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var payload map[string]any
	_ = json.Unmarshal(body, &payload)
	return nil
}

func webhookPublicURL(req extension.WireRequest) string {
	// Prefer X-Forwarded proto/host when present; fall back to request URL host.
	proto := headerGet(req, "X-Forwarded-Proto")
	host := headerGet(req, "X-Forwarded-Host")
	if host == "" {
		host = headerGet(req, "Host")
	}
	path := ""
	if u, err := url.Parse(req.URL); err == nil {
		if host == "" {
			host = u.Host
		}
		if proto == "" {
			proto = u.Scheme
		}
		path = u.Path
		if u.RawQuery != "" {
			path += "?" + u.RawQuery
		}
	}
	if proto == "" {
		proto = "https"
	}
	if host == "" || path == "" {
		return req.URL
	}
	return proto + "://" + host + path
}

func headerGet(req extension.WireRequest, key string) string {
	if req.Header == nil {
		return ""
	}
	for k, vals := range req.Header {
		if strings.EqualFold(k, key) && len(vals) > 0 {
			return strings.TrimSpace(vals[0])
		}
	}
	return ""
}

func parseFormBody(raw string) (url.Values, error) {
	return url.ParseQuery(raw)
}
