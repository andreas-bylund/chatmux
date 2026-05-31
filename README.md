# stream-chat-terminal

Liten Go-CLI som strömmar live-chatten från **Twitch**, **YouTube** och **Kick** rakt in i terminalen. Stödjer flera källor samtidigt — meddelandena multiplexas och taggas med plattform.

## Installation

Kräver Go (`sudo pacman -S go`).

```bash
git clone <repo> ~/Projects/Stream-chat-terminal
cd ~/Projects/Stream-chat-terminal
go mod tidy
go build -o stream-chat .
```

Eller kör direkt utan att bygga binär: `go run . <source>...`

## Användning

Varje argument är en källa på formen `<platform>:<target>`:

```bash
stream-chat twitch:xqc
stream-chat kick:trainwreckstv
stream-chat youtube:dQw4w9WgXcQ
stream-chat twitch:xqc kick:trainwreckstv youtube:dQw4w9WgXcQ
```

Varje rad prefixas med en färgad plattformstagg (`T` lila / `Y` röd / `K` grön) följt av tid, nick (i användarens färg om tillgänglig) och meddelandet. Ctrl-C avslutar.

## Plattformar

### Twitch
Anonym anslutning via IRC (`irc.chat.twitch.tv:6697`). Ingen token krävs.

```bash
stream-chat twitch:<kanalnamn>
```

### YouTube
Använder YouTube Data API v3. Kräver en gratis API-nyckel:

1. Gå till [Google Cloud Console](https://console.cloud.google.com/)
2. Skapa ett projekt och aktivera **YouTube Data API v3**
3. Skapa en **API key** under Credentials
4. Exportera den:

```bash
export YOUTUBE_API_KEY="din-nyckel-här"
```

Targets kan vara video-ID, `youtu.be/<id>`-URL eller en `watch?v=...`-URL:

```bash
stream-chat youtube:dQw4w9WgXcQ
stream-chat "youtube:https://youtu.be/dQw4w9WgXcQ"
```

**Viktigt:** streamen måste vara live. Quota-default räcker till ungefär 2-3 timmars kontinuerlig polling per dag.

### Kick
Ansluter över Pusher WebSocket (samma som webbklienten använder). Ingen auth.

```bash
stream-chat kick:<kanal-slug>
```

**Inofficiellt** — Kick har ingen publik chat-API ännu, så detta kan sluta fungera om de byter Pusher-konfiguration eller eventformat.

## Struktur

```
chat/        Message + Source-interface
twitch/      IRC-klient
youtube/     Data API v3 polling
kick/        Pusher WebSocket
main.go      CLI + multiplex + utskrift
```

Lägg till en ny plattform genom att implementera `chat.Source` (`Run` + `Label`) och registrera den i `parseSource` i `main.go`.

## Beroenden

- `github.com/coder/websocket` — Kicks WebSocket

Twitch och YouTube använder bara stdlib.
