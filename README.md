# live2gether

a small watch-room server for public live streams and videos.

paste a youtube url, start a room, share the link, chat, see who is watching, and keep everyone near the live edge.

## features

- go and gin server
- video.js html player
- websocket room updates
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
```

`RESTREAM_TRANSCODE=0` uses stream copy instead of decode and re-encode.

## deploy on vercel

the repo includes `Dockerfile.vercel`, which builds the go server and installs `ffmpeg` and `yt-dlp` in the image.

```bash
vercel deploy --prod
```

vercel container images still run with vercel function constraints. this can work for short rooms, but a long-running restream server is better suited to a dedicated container host such as fly.io, railway, render, or a vps.

## test

```bash
go test ./...
npm install
npx playwright install chromium
BASE_URL=http://127.0.0.1:8080 npm run test:e2e
```

## notes

only restream public, non-drm media that you have the right to share.
