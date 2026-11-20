# SeeYouHere

**Live: [seeyouhere.today](https://seeyouhere.today)**

A very boring social network, on purpose — a small place for lonely people
to be alone, together.

There is one list per day: everyone who is here today. Sign in with Google,
pick a name (once, forever), press "I'm here", and tap other people's
names to say — without words — *I see you*. They might tap you back.
Being tapped is never announced; your lifetime points counter just ticks
up, live, and a small number appears beside the name of whoever tapped
you.

No feed. No posts. No comments. No messages. No profiles.

The whole site — this code, one stylesheet, one tiny animated SVG — weighs
about 15 KB. It runs as a single static Go binary with SQLite. It is
published so that it can outlive its server: if seeyouhere.today ever goes
away, anyone can bring it back.

## Run

```sh
CGO_ENABLED=0 go build -o seeyouhere .
SYH_SESSION_SECRET=$(openssl rand -hex 32) \
GOOGLE_CLIENT_ID=... GOOGLE_CLIENT_SECRET=... \
./seeyouhere
```

## Configuration (env)

| Var | Default | Meaning |
|---|---|---|
| `SYH_ADDR` | `127.0.0.1:18081` | listen address |
| `SYH_DB` | `./seeyouhere.db` | SQLite file |
| `SYH_BASE_URL` | `https://seeyouhere.today` | public base URL (OAuth redirect = `<base>/auth/google/callback`) |
| `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` | — | Google OAuth credentials; sign-in is disabled without them |
| `SYH_SESSION_SECRET` | random per boot | HMAC key for session cookies (set it in production) |
| `SYH_ADMIN_SUB` | — | Google `sub` of the admin; enables `/admin` (ban/unban) for that account only |

## Rules implemented

- One global Space day for everyone; the day flips at **08:00 UTC**.
- Google login only. First login → set a name: letters/digits/Han only,
  width 2–16 (Han = 2), unique case-insensitively, **never changeable**
  (two-step setup with an explicit warning).
- Not checked in = window view: a deterministic per-viewer sample of the
  roster (15%, min 3, max 20), no tapping. Check-in is per-day, irreversible.
- Taps cost today's quota first, then referral tickets. Quota by level:
  level = 1 + floor(attendance days / 100); 24 / 48 / 60 / 66 / 69 / 71 / 72,
  then +1 per level. Quota expires at the flip; points and tickets never do.
- Points: +1 per tap received, lifetime, unspendable. Referral tickets: +1
  when someone you invited (via `/?ref=<your code>`) checks in for the
  first time ever. No caps.
- No "who tapped you" notifications: the recipient gets a WebSocket event
  (new points total + tapper id + pair count for today) and nothing else.
  Level is shown only to its owner.
- `/share`: daily card with only your own numbers + copyable text version.
- Per-IP rate limits on auth/name-setup; admin can ban (banned users can't
  check in or tap and disappear from the roster).

## Test

```sh
go test ./...   # day-boundary, level/quota table, name rules, window sampling
go vet ./...
```

## Notes

- `assets/og.png` (the social-card screenshot) is not in this repo; the
  code references it, and any screenshot of the live site will do.
- The repository is a publication archive, not an actively maintained
  open-source project: it is not accepting issues or pull requests.

## License

AGPL-3.0 — see [LICENSE](LICENSE). Run a modified version as a network
service and you must offer its users the modified source. That is the
point: this little place, and anything built from it, stays open.
