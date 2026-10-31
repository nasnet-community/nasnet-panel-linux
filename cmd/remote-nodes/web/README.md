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
| `npm run pack` | Writes `dist/index.html.br` (brotli, highest quality) from the built page, for the Go side to embed, and then runs `npm run notices`. |
| `npm run notices` | Writes `dist/NOTICES.txt`, the licences of the libraries in the page and of its font (see Licence notices). |
| `npm run typecheck` | `tsc -b`. |
| `npm test` | Unit tests (vitest). |
| `npm run e2e` | Browser tests (Playwright) against the mock API, in a desktop window and at phone width (Pixel 7). Needs Chromium: `npx playwright install chromium`, or set `CHROMIUM_PATH`. |

## The mock API

`mock-api.ts` is a Vite plugin that answers the plugin's HTTP API from memory while `npm run dev:mock`
runs. Its header comment lists the sample data and the scenarios (an address containing `fail`,
`changed`, `offline` and so on makes the add-node flow go that way). `mock-node.ts` answers the node
page and the router's Settings tab (traffic, renaming, settings, updates, removal, backup); its header
lists what `POST /api/__node` can do to a node, such as putting it in the Xray mismatch state. `POST /api/__reset` puts the data
back and takes `{ fresh, nodes, maxNodes, entry, pace }` to set up other states (the browser tests use
it). The calls that change subscriptions (add, rename, delete, the switch per node, the links) are in
`mock-subscriptions.ts`. It is not part of the build.

## In the image

`Dockerfile.remote-nodes`, at the repository root, builds the page here with `npm ci`, `npm run build`
and `npm run pack`, copies `dist/index.html.br` to `internal/remotenodes/ui/index.html.br`, and builds
the hub with it. Without that file the hub embeds a placeholder page. `pack` also writes
`dist/NOTICES.txt`, which the image keeps as `/licenses/web-notices.txt` next to the Go side's `NOTICES`.
The hub serves the files of that directory as one plain text at `GET /licenses` (under `BASE_PATH` too, with
no header needed). The Notices link in the page's About card opens it: a relative `licenses` link, so the
base tag decides where it goes, and the mock API answers it with a short sample.

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

## Licence notices

MIT, ISC and the OFL want their notice to travel with copies, so what the page carries is written into
`dist/NOTICES.txt`: for each library its name, version, SPDX licence, homepage or repository and the text
of its licence file; what its CSS carries from Tailwind; and for Vazirmatn the notice in `src/fonts/OFL.txt`
with its copyright line. The libraries are the `dependencies` of `package.json` and what those depend on in
turn, read from `node_modules`. Peer dependencies are not followed: the page aliases `react` to Preact, so
the `react` that npm installs for lucide-react's peer dependency is not in the bundle. The file has no
dates and is sorted, so the same install gives the same bytes.

A library that ships no licence file (qrcode-generator) gets the comment at the top of its main source file,
and, since that only names the licence, the standard MIT text under the copyright line the comment gives,
labelled as the standard text and not a file of the package. Only MIT has its standard text in the script;
a library with only a header and another licence is refused until its text is found or added.

Tailwind CSS is a build tool, not a dependency, but it writes its Preflight base styles into the page's CSS,
and Preflight is derived from modern-normalize. Both are listed, labelled "included in the page's CSS", with
the licence texts `node_modules` has: Tailwind's own `LICENSE`, and for modern-normalize, which isn't
installed on its own, the licence file Tailwind ships next to Preflight (`tailwindcss/src/css/LICENSE`) or,
if a Tailwind has none, Tailwind's own licence, and the notice says which it is. No copyright line is
written by the script: each one comes from a file. If the page's CSS stops having Preflight (no
`@tailwind base`, or `preflight: false`), a test says to take those two out of `scripts/notices.mjs`.

`npm run notices` stops when a package has no usable notice or a licence outside `ALLOWED_LICENSES` in
`scripts/notices.mjs` (MIT, ISC, OFL-1.1, Apache-2.0, BSD-2-Clause, BSD-3-Clause), and `npm test` makes the
same checks, for what the CSS carries too. `npm test` also fails when the page imports a package that isn't
in `dependencies`, which would be bundled with no notice. A new dependency needs its licence agreed and
added to that list first.

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

## The Persian font

Persian is set in Vazirmatn, embedded so it looks the same on every device and the page asks for no font.
`src/fonts/` holds it cut down to what the page's Persian text uses, in two weights (400 and 600; the
browser maps 500 to the first and 700 to the second): the Persian letters, marks and digits in
`vazirmatn-persian-*.woff2`, and the digits and punctuation of Latin text in `vazirmatn-symbols-*.woff2`.
Latin letters keep the system font. `src/fonts.css` declares the four files; the build inlines them as
base64, which costs about 33 KB packed. Vazirmatn is under the SIL Open Font License 1.1 (`src/fonts/OFL.txt`).

The files were cut from `@fontsource/vazirmatn` 5.3.0 (`vazirmatn-arabic-{400,600}-normal.woff2` and
`vazirmatn-latin-{400,600}-normal.woff2`) with fontTools. If the Persian copy needs a letter that is
missing, cut them again with the letter added to the first list, and add it to the `unicode-range`:

```
pyftsubset vazirmatn-arabic-400-normal.woff2 --flavor=woff2 --layout-features='*' --no-hinting \
  --name-IDs=0,1,2,3,4,5,6,13,14 --drop-tables+=STAT,gasp --output-file=vazirmatn-persian-400.woff2 \
  --unicodes=U+0020,U+00A0,U+0621-063A,U+0640-0655,U+0660-066D,U+067E,U+0686,U+0698,U+06A9,U+06AF,U+06CC,U+06C0,U+06F0-06F9,U+060C,U+061B,U+061F,U+200C-200F,U+FDFC
pyftsubset vazirmatn-latin-400-normal.woff2 --flavor=woff2 --layout-features= --no-hinting \
  --name-IDs=0,1,2,3,4,5,6,13,14 --drop-tables+=STAT,gasp --output-file=vazirmatn-symbols-400.woff2 \
  --unicodes=U+0021-0040,U+005B-0060,U+007B-007E,U+00AB,U+00B7,U+00BB,U+2013-2014,U+2018-2019,U+201C-201D,U+2026
```

The same two commands, with 600 in the names, make the heavier weight.
