# Antidubl

A Telegram bot that keeps group chats clean of duplicates, floods and spam.
It detects repeated content in real time — text, links, photos, videos,
documents, forwards — and reacts per configurable rules: ignore, comment or
delete.

One static binary, one SQLite file, no web interface. Settings live in the
chat itself: `/settings` opens an inline menu for chat administrators.

> The bot's own chat-facing text (menus, notifications, `/help`) is in Russian,
> matching the bot's name «Антидубль». This README is in English.

## What counts as a duplicate

The first occurrence of any content is **never** removed — only a repeat, and
only inside the tracking window (10 days by default, configurable 1..30).

| Content | Compared by | Catches |
|---|---|---|
| Text | normalized: case, repeated spaces, `ё`/`е`, trailing punctuation | «Привет» vs «привет!», «текст» vs «текст)» |
| Links | URL canonicalization inside the text: `utm_*`, `?start=` and other tracking stripped | same link with different tracking |
| Photos | 64-bit dHash, Hamming distance ≤ 10 | copy-paste, recompression, resize |
| Videos | dHash of the thumbnail (first frame), threshold ≤ 6 | recompression, renaming, "send as file" |
| Documents | SHA-256 of the file bytes; thumbnail dHash for visually similar ones | renamed PDF copies, re-saved documents |
| Forwards | configurable policy (see below) | re-forwarding the same channel post |

Media is classified by MIME type: a video sent with "Send as file" arrives as
`m.Document` with `video/*`, and its hash is taken from the thumbnail instead
of being skipped.

Documents and videos larger than 20 MB are not downloaded — that is the Bot API
`getFile` ceiling; they fall back to `file_unique_id` and thumbnail checks.

## Reactions

The reaction is configured per *type × category*:

- **type** — link (the message contains a URL) or message;
- **category** — duplicate from the same participant, or from different ones.

Values: `ignore` (do nothing), `comment` (reply to the duplicate naming the
author), `delete` (remove the duplicate and post a notice).

Sensible defaults: link × anyone → delete; message × same participant →
delete; message × different participants → **ignore** (two people saying
«Привет» is a conversation, not a flood).

## Warnings and bans

Duplicate N → a warning; the N-th warning → a ban. The counter is **one per
user**, not split by duplicate type, and resets after the ban. If the ban is
impossible (chat owner, missing rights), the bot reports the reason and resets
the counter, so warnings never pile up against someone who cannot be banned.

Ban type: `readonly` (restrict — the member can still read) or `kick` (ban with
automatic return at `until_date`). Duration 1..366 days. A restricted user's
duplicates are still removed, but they receive no further warnings.

## Commands

| Command | Who | What it does |
|---|---|---|
| `/settings` | chat admins | inline menu with every setting for this chat |
| `/status` | everyone | chat, tracking window, auto-delete, message count in the database |
| `/help` | everyone | detailed reference for all policies |
| `/chats` | bot owner | list of allowed chats |
| `/allow <id>` | bot owner | allow a chat |
| `/deny <id>` | bot owner | deny a chat |
| `/unban <id> [chat_id]` | bot owner | emergency lift of readonly/kick |

`/settings` is declared as an ephemeral command (Bot API 10.3): the command
message itself is invisible to other members, and the menu is sent as an
ephemeral message visible only to the calling admin and the bot. If ephemeral
messages are unavailable, there is a fallback to a regular public menu.

## Policies

- **Forwards.** `all` — a forward is matched like any message (text, each
  media item, source). `source_only` (recommended for chats with forwards from
  many channels) — a duplicate only when the *same* post is re-forwarded
  (channel + post id). `ignore` — forwards are never duplicates. The mode does
  not affect direct messages.
- **Deleted original.** `strict` — if the first message was deleted manually,
  the repeat is still removed. `allow` — the repeat passes and becomes the new
  original.
- **Freshness threshold.** Messages older than N minutes (5 by default) are
  only remembered, not acted on: after downtime the accumulated backlog does not
  trigger a wave of deletions. `0` — react to everything.
- **Notices.** `full` — public, naming the participant; `short` — public,
  without the name; `ephemeral` — nothing is posted publicly, the offender
  receives an ephemeral message.
- **Auto-delete.** Bot messages are removed after N hours; the maximum equals
  the tracking window in hours. `0` — off.

## Installation

Requires Go 1.25+ (no CGO — SQLite via `modernc.org/sqlite`).

```sh
git clone https://github.com/ReyterAK/antidoubl
cd antidoubl
go build -o antidubl .
cp config.example.json config.json
$EDITOR config.json          # set bot_token
./antidubl
```

Environment variables:

| Variable | Default | Meaning |
|---|---|---|
| `ANTIDUBL_CONFIG` | `config.json` | configuration path |
| `ANTIDUBL_DB` | `antidubl.db` next to the config | database path |
| `ANTIDUBL_HEARTBEAT` | `heartbeat` next to the config | heartbeat file for the watchdog |

### Adding the bot to a group

The bot **must be an administrator** with the delete-messages right
(`can_delete_messages`), otherwise the `delete` reaction cannot work. The
`restrict` and `ban` rights are only available to a chat admin.

### Running as a systemd service

```sh
sudo install -d -o antidubl -g antidubl /opt/antidubl
sudo install -m 0755 antidubl            /opt/antidubl/antidubl
sudo install -m 0640 config.json        /opt/antidubl/config.json   # chown antidubl
sudo cp deploy/antidubl.service         /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now antidubl
journalctl -u antidubl -f
```

The service writes a heartbeat once a minute. Crashes are handled by
`Restart=on-failure`; a hung (but alive) process is covered by the watchdog:

```sh
sudo cp deploy/antidubl-watchdog.{sh,service,timer} /opt/antidubl/
sudo cp deploy/antidubl-watchdog.{service,timer} /etc/systemd/system/
sudo systemctl enable --now antidubl-watchdog.timer
```

The watchdog restarts the bot when the heartbeat is older than 5 minutes, at
most once per 10 minutes, and never touches a service that was stopped manually.

### Backups

Back up **both** the database and the config — the config contains the bot
token, so keep backups in private storage:

```sh
sqlite3 antidubl.db '.backup /backup/antidubl.db'
cp config.json /backup/
```

## Configuration

Global defaults live in `config.json`; on a chat's first contact they are
copied into the `chat_settings` table, after which each chat keeps its own
settings. `/settings` writes changes there and applies them instantly, without
a restart.

Global keys: `allowed_chats` (empty — any chat where the bot is an admin;
non-empty — only those chats, others are ignored and the owner is notified)
and `owner_user_id` (`0` — owner commands disabled). Each chat is served
independently; there is no single-chat key.

Keys and bounds are documented in comments in [`config.go`](config.go) and shown
in [`config.example.json`](config.example.json).

## Limitations

- The bot cannot see user-initiated message deletions — Telegram sends no such
  events. Hence the "deleted original" policy.
- A real ban only ever catches offenders: unique spam is not a duplicate by
  definition. Other rules are needed to stop "different text every time".
- The warning counter is per user, not split by duplicate type.
- Degenerate hashes are deliberately excluded from perceptual comparison —
  solid-colour images and blank frames — otherwise any two black frames would
  match.

## Development

```sh
go build ./...   # build
go vet ./...     # static analysis
go test ./...    # unit tests
```

Tests cover text and URL normalization, the SQLite schema and retention, the
detector (text, media, forwards, type × category classification), warnings,
auto-delete, command argument parsing and notice modes. Everything runs
offline: no network needed.

## License

[MIT](LICENSE).