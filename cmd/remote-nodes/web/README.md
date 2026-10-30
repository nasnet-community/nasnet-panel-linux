# Remote Nodes web page

The page of the Remote Nodes plugin. It is a Vite + Preact (through React's API) + Tailwind 3 +
TypeScript project that builds into one `dist/index.html`, which the Go hub embeds.

The API it talks to is served under `BASE_PATH` + `/api/`. The page calls relative `api/...` URLs
and sends `X-RemoteNodes-UI: 1` with every request except the job event stream, which is an
`EventSource` and can't carry headers.

## Commands

| Command | What it does |
|---|---|
| `npm run dev` | Dev server. It proxies `/api` to a hub on `localhost:8080`. |
| `npm run dev:mock` | Dev server with a fake API (`mock-api.ts`): sample nodes, subscriptions and jobs, no hub needed. |
| `npm run dev:mock:fresh` | The same, starting from a first run: no nodes, entry off. |
| `npm run build` | Typechecks, then writes the single-file `dist/index.html`. |
| `npm run pack` | Writes `dist/index.html.br` (brotli, highest quality) from the built page, for the Go side to embed. |
| `npm run typecheck` | `tsc -b`. |
| `npm test` | Unit tests (vitest). |
| `npm run e2e` | Browser tests (Playwright) against the mock API, in a desktop window and at phone width (Pixel 7). Needs Chromium: `npx playwright install chromium`, or set `CHROMIUM_PATH`. |

## The mock API

`mock-api.ts` is a Vite plugin that answers the plugin's HTTP API from memory while `npm run dev:mock`
runs. Its header comment lists the sample data and the scenarios (an address containing `fail`,
`changed`, `offline` and so on makes the add-node flow go that way). `POST /api/__reset` puts the data
back and takes `{ fresh, nodes, maxNodes, entry, pace }` to set up other states (the browser tests use
it). The calls that change subscriptions (add, rename, delete, the switch per node, the links) are in
`mock-subscriptions.ts`. It is not part of the build.

## In the image

`Dockerfile.remote-nodes`, at the repository root, builds the page here with `npm ci`, `npm run build`
and `npm run pack`, copies `dist/index.html.br` to `internal/remotenodes/ui/index.html.br`, and builds
the hub with it. Without that file the hub embeds a placeholder page.

To run the hub with the real page and no Docker, from the repository root (git ignores the copy):

```
(cd cmd/remote-nodes/web && npm run build && npm run pack)
cp cmd/remote-nodes/web/dist/index.html.br internal/remotenodes/ui/
DATA_DIR=/tmp/remote-nodes go run ./cmd/remote-nodes
```

To build the image for this machine, or only the two packed programs, with their unpacked sizes and
the licence files, into `bin/remote-nodes`:

```
docker build -f Dockerfile.remote-nodes -t remote-nodes .
docker build -f Dockerfile.remote-nodes --target binary --output type=local,dest=bin/remote-nodes .
```

## Where it comes from

The tokens, the components in `src/components` and the job tray follow the web page of the Remote
MTProto plugin (https://github.com/nasnet-community/mtproto-remote-plugin, `web/`), which is MIT
licensed. Its notice:

```
MIT License

Copyright (c) 2026 NASNET Community

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```
