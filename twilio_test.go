package main

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
)

func TestResolveDryRun(t *testing.T) {
	t.Parallel()
	on := true
	off := false
	cases := []struct {
		name string
		cfg  smsSettings
		dev  bool
		want bool
	}{
		{"dev default on", smsSettings{}, true, true},
		{"live default off", smsSettings{}, false, false},
		{"explicit off in dev", smsSettings{DryRun: &off}, true, false},
		{"explicit on in live", smsSettings{DryRun: &on}, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveDryRun(tc.cfg, tc.dev); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestPhoneE164(t *testing.T) {
	t.Parallel()
	got, err := phoneE164("4155552671", "US")
	if err != nil {
		t.Fatal(err)
	}
	if got != "+14155552671" {
		t.Fatalf("got %q", got)
	}
	if _, err := phoneE164("not-a-phone", "US"); err == nil {
		t.Fatal("expected error")
	}
}

func TestVerifyTwilioSignature(t *testing.T) {
	t.Parallel()
	token := "auth-token"
	fullURL := "https://example.com/webhooks/sms-alert"
	params := url.Values{}
	params.Set("From", "+15551234567")
	params.Set("Body", "hello")

	var b strings.Builder
	b.WriteString(fullURL)
	// Signature algorithm sorts keys alphabetically: Body, From
	b.WriteString("Body")
	b.WriteString(params.Get("Body"))
	b.WriteString("From")
	b.WriteString(params.Get("From"))
	mac := hmac.New(sha1.New, []byte(token))
	_, _ = mac.Write([]byte(b.String()))
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	if !verifyTwilioSignature(token, fullURL, sig, params) {
		t.Fatal("expected valid signature")
	}
	if verifyTwilioSignature(token, fullURL, "bad", params) {
		t.Fatal("expected reject")
	}
	if verifyTwilioSignature("", fullURL, sig, params) {
		t.Fatal("expected reject empty token")
	}
}

func TestTwimlEscapesXML(t *testing.T) {
	t.Parallel()
	out := twimlMessage(`Hi <script> & "x"`)
	for _, part := range []string{"&lt;", "&amp;", "&quot;"} {
		if !strings.Contains(out, part) {
			t.Fatalf("missing %q in %s", part, out)
		}
	}
}

func TestDryRunSendSkipsHTTP(t *testing.T) {
	t.Parallel()
	c := twilioCreds{
		AccountSID: "AC",
		AuthToken:  "tok",
		FromNumber: "+14155552671",
		DryRun:     true,
		Mode:       "test",
	}
	if err := c.Send("+14155552671", "+14155552672", "hello"); err != nil {
		t.Fatal(err)
	}
}

func TestContainsUint(t *testing.T) {
	t.Parallel()
	if !containsUint([]uint{1, 2, 3}, 2) {
		t.Fatal("expected true")
	}
	if containsUint([]uint{1, 2}, 9) {
		t.Fatal("expected false")
	}
}

func TestFirstWords(t *testing.T) {
	t.Parallel()
	if got := firstWords("one two three four five six", 5); got != "one two three four five" {
		t.Fatalf("got %q", got)
	}
}

func TestDefaultMessages(t *testing.T) {
	t.Parallel()
	if !strings.Contains(defaultSubscribeMsg("Alerts"), "Alerts") {
		t.Fatal("subscribe msg")
	}
	if !strings.Contains(defaultRemoveMsg("Alerts"), "Alerts") {
		t.Fatal("remove msg")
	}
}
