# live2gether

a small watch-room server for public live streams and videos.

paste a youtube url, start a room, share the link, chat, see who is watching, and keep everyone near the live edge.

## features

- go and gin server
- video.js html player
- websocket room updates
- persisted browser session id
- encrypted youtube cookie profiles
- generated viewer names
- simple room chat
- viewer count and player list
- host-only quality picker
- live sync button
- ffmpeg and yt-dlp based hls restreaming

## requirements

- go 1.26 or newer
- ffmpeg
- yt-dlp
- node.js for browser tests

## run locally

```bash
go run .
```

open `http://localhost:8080`.

use another port:

```bash
ADDR=:8090 go run .
```

## configuration

```bash
ADDR=:8080
DATA_DIR=/tmp/live2gether
RESTREAM_TRANSCODE=0
GIN_MODE=release
DATABASE_URL=postgres://...
COOKIE_ENCRYPTION_KEY=base64-encoded-32-byte-key
```

`RESTREAM_TRANSCODE=0` uses stream copy instead of decode and re-encode.

if `DATABASE_URL` is set, live2gether stores browser sessions and youtube cookie profiles in postgres. cookie values are encrypted with aes-gcm before writing to the database and are never returned by the api.

without `DATABASE_URL`, local dev uses an encrypted in-memory store. that is fine for testing, but it will not survive a restart or vercel cold start.

## youtube login

the browser stores only a generated session id in localstorage. when youtube requires login or region/account cookies, use the `youtube login` button and paste a `cookies.txt` export. the server stores the encrypted cookie profile for that browser session and passes a temporary `0600` cookies file to `yt-dlp`.

do not paste account passwords. remove old cookie profiles from the database if a device is lost or a session should be revoked.

## deploy on vercel

the repo includes `Dockerfile.vercel`, which builds the go server and installs `ffmpeg` and `yt-dlp` in the image.

`vercel.json` enables fluid compute and pins the runtime region to `gru1` so youtube requests originate from sao paulo, brazil.

create a postgres database from the vercel marketplace, for example neon, and connect it to the `live2gether` project so vercel injects `DATABASE_URL`.

```bash
vercel integration add neon --name live2gether-db --plan free_v3 -m region=gru1 -m auth=false -e production
```

then add the encryption key as a sensitive production variable:

```bash
openssl rand -base64 32
vercel env add COOKIE_ENCRYPTION_KEY production --sensitive --value "<key>" --yes
```

```bash
vercel deploy --prod
```

vercel container images still run with function constraints. containers can scale down after idle time, websocket connections can close when a function reaches its maximum duration, and in-memory rooms are not shared across multiple instances.

for a production watch-party service, move room state and chat to redis or another external store. for long-running public restreams, a dedicated container host such as fly.io, railway, render, or a vps is still a better fit.

## test

```bash
go test ./...
npm install
npx playwright install chromium
BASE_URL=http://127.0.0.1:8080 npm run test:e2e
```

## notes

only restream public, non-drm media that you have the right to share.
