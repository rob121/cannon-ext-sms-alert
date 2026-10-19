# SMS Alerts

Keyword SMS subscribe/unsubscribe and list broadcasts via Twilio.

## What It Provides

- Admin UI for lists, from-numbers, authorized senders, subscribers, and compose/send
- Twilio inbound webhook for keyword subscribe, `remove`, and authorized-sender conversations
- Live and test Twilio credentials selected by Cannon Development Mode
- Optional dry-run (default on in Development Mode) that logs outbound SMS without calling Twilio

## Permissions

- `sms-alert.manage` — lists, numbers, senders, subscribers, delivery-log cleanup
- `sms-alert.send` — compose and send broadcasts

## Delivery log retention

Under Twilio Settings:

- **Keep recent messages** — keep only the newest N broadcasts (and their delivery rows); older ones are deleted. `0` disables cleanup. Default `100`.
- **Cleanup interval (hours)** — background cleanup cadence. Default `24`.

Cleanup also runs after each send, when the admin home loads (if due), and via **Cleanup now** on the extension home.

## Webhook

Cannon does **not** auto-mount the path. You must create a route first:

1. **System → Routes** → add **Extension Endpoint**
2. Path: `/webhooks/sms-alert` (prefix `/webhooks/` is required so CSRF is skipped)
3. Extension: SMS Alerts → Endpoint: **Twilio Inbound Webhook**
4. In Twilio Console, set the number’s messaging webhook to `https://yoursite/webhooks/sms-alert` (HTTP POST)

Until that route exists, the URL returns 404. After it exists, a browser GET returns “POST required”; Twilio must POST.

Signatures are verified with the active auth token (test or live depending on Development Mode).

## SMS flows

| Inbound body | Behavior |
|---|---|
| list keyword | Subscribe + subscribe message |
| `remove` | Leave list (or pick among multiple) |
| authorized sender text | Start compose conversation; reply with list label to send |
| `endconvo` | Cancel sender conversation |

## Tables

On install:

- `sms_alert_settings`
- `sms_alert_numbers`
- `sms_alert_lists`
- `sms_alert_subscribers`
- `sms_alert_senders`
- `sms_alert_sender_lists`
- `sms_alert_messages`
- `sms_alert_message_logs`
- `sms_alert_conversations`

## Non-goals (v1)

- MySQL Notify import
- Public web subscribe pages
- Profile CRM sync
