# Cannon SMS Alerts

Keyword SMS list subscribe/unsubscribe and broadcasts via Twilio for Cannon sites.

## Features

- SMS keyword subscribe and `remove` flows (Twilio inbound webhook)
- Authorized-sender SMS compose conversation
- Cannon admin for lists, from-numbers, senders, subscribers, compose/send, delivery logs
- Live + test Twilio credentials selected by Cannon Development Mode
- Dry-run outbound (default on in Development Mode): log and skip HTTP

## Requirements

- Cannon with `/webhooks/` mounted without CSRF (core support)
- Twilio account with a messaging-capable number

## Quick start

1. Install and activate `cannon-ext-sms-alert`
2. Open **Twilio Settings** (extension configuration) and enter test and/or live SID, auth token, and default from number
3. Add a from-number under **Numbers**, then create a **List** with a unique keyword
4. **System → Routes**: create an **Extension Endpoint** at `/webhooks/sms-alert` → SMS Alerts → **Twilio Inbound Webhook** (404 until this exists)
5. In Twilio Console, set the number’s messaging webhook to that URL (HTTP POST)
6. Text the list keyword to subscribe; use admin Compose or an authorized sender phone to broadcast

## Development Mode

| Cannon Development Mode | Credentials used | Dry-run default |
|---|---|---|
| On | `*_test` | On (unless `dry_run` is explicitly false) |
| Off | `*_live` | Off (unless `dry_run` is explicitly true) |

Missing test keys while Development Mode is on returns a clear error (no silent live fallback).

## Permissions

- `sms-alert.manage` — manage lists, numbers, senders, subscribers
- `sms-alert.send` — compose and send broadcasts

## Capabilities

- `/admin` — lists, numbers, senders, subscribers, messages
- `/endpoint` — Twilio inbound webhook
- `/configuration` — Twilio credentials
- `/help`, `/templates`, `/install`, `/permissions`

## Version

`0.1.0`
