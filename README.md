# ai-chat-go

A stateful AI chat assistant written in **Go** and running on **Cloudflare Workers**. It uses **Llama 3.3 (70B) on Workers AI** for responses, keeps per-session conversation history in **Workers KV**, and serves a small chat UI from the same Worker.

**Live demo:** `<your-worker>.<your-subdomain>.workers.dev`

## What it does

- Chat with an LLM in the browser.
- Conversations persist across page reloads and Worker invocations, keyed by a per-browser session ID.
- The model sees recent history, so follow-up questions work ("make it shorter", "what about the second one?").

## Architecture

```
Browser (chat UI, sessionId in localStorage)
   │  POST /api/chat { sessionId, message }
   ▼
Cloudflare Worker (Go -> Wasm, workers-go)
   │  1. load history          ┌──────────────┐
   ├──────────────────────────▶│  Workers KV  │  session:<id> -> []Msg
   │  4. save history          └──────────────┘
   │
   │  2. build prompt (system + last 20 messages)
   │  3. call Llama 3.3
   ▼
Workers AI  (@cf/meta/llama-3.3-70b-instruct-fp8-fast)
```

| Requirement | Implementation |
|---|---|
| LLM | Llama 3.3 70B on Workers AI, called through the Workers AI REST API |
| Workflow / coordination | The Go Worker orchestrates each turn: load state, build prompt, call the model, persist the result |
| User input | Browser chat UI (static HTML/JS) served from the same Worker |
| Memory / state | Workers KV stores each session's message history |

## Tech stack

- **Language:** Go, compiled to WebAssembly
- **Runtime:** Cloudflare Workers via [`syumai/workers-go`](https://github.com/syumai/workers-go)
- **Model:** `@cf/meta/llama-3.3-70b-instruct-fp8-fast` on Workers AI
- **State:** Workers KV
- **Frontend:** Plain HTML and JavaScript (no build step)

## Project layout

```
.
├── main.go            # HTTP handler: state + LLM orchestration
├── public/
│   └── index.html     # Chat UI, served as a static asset
├── wrangler.jsonc     # Worker config and bindings
├── Makefile           # Builds the Go code to Wasm; wraps dev/deploy
└── README.md
```

`make build` generates the JavaScript loader (`build/worker.mjs`) with the `workers-assets-gen` tool from `workers-go`, then compiles the Go code to `build/app.wasm`. The `build/` directory is generated output, so add it to `.gitignore`.

## Running it

### Prerequisites

- Node.js and npm
- Go 1.24.0 or later
- TinyGo 0.42.0 or later (only if you use the TinyGo template for a smaller binary)
- A Cloudflare account, plus an API token with **Workers AI** permission

### Setup

```bash
# 1. Initialize the Go module and fetch dependencies
go mod init <your-module-name>
go get github.com/syumai/workers-go
go mod tidy

# 2. Log in and look up your Cloudflare account ID (shown in the accounts table)
npx wrangler login
npx wrangler whoami

# 3. Create the KV namespace and note the returned id
npx wrangler kv namespace create CHAT_KV
```

Put the account ID and KV namespace id into `wrangler.jsonc`:

`wrangler.jsonc`:

```jsonc
{
  "name": "ai-chat-go",
  "main": "./build/worker.mjs",
  "compatibility_date": "2026-10-01",
  "build": { "command": "make build" },
  "assets": { "directory": "./public" },
  "kv_namespaces": [{ "binding": "CHAT_KV", "id": "<your-kv-id>" }],
  "vars": { "CF_ACCOUNT_ID": "<your-account-id>" }
}
```

With `assets` set, Cloudflare serves `public/index.html` at `/` directly, and any request that doesn't match a static file (such as `/api/chat`) goes to the Go Worker. To build with TinyGo for a smaller binary, change the build command to `make build TINYGO=1`.

### Credentials

The Worker calls the Workers AI REST API, so it needs two values:

- **`CF_ACCOUNT_ID`**: your Cloudflare account ID. It identifies the account but can't authenticate anything, so it's fine in `wrangler.jsonc` and in version control.
- **`CF_API_TOKEN`**: an API token with **Workers AI** permission, scoped to your account. This is a secret. Create it at [dash.cloudflare.com/profile/api-tokens](https://dash.cloudflare.com/profile/api-tokens) and never commit it.

For local development, create a `.dev.vars` file in the project root. Wrangler reads it at startup, so restart `make dev` after changing it:

```
CF_API_TOKEN=<your-token>
```

For the deployed Worker, store the token as a secret (`.dev.vars` is not uploaded):

```bash
npx wrangler secret put CF_API_TOKEN
```

Keep secrets and generated files out of git:

```
# .gitignore
.dev.vars
build/
```

### Run and deploy

```bash
make dev       # serves http://localhost:8787
make deploy
```

## API

### `POST /api/chat`

Request:

```json
{ "sessionId": "b6f1c2e0-...", "message": "Explain Durable Objects in one paragraph." }
```

Response:

```json
{ "reply": "Durable Objects are ..." }
```

Errors return a plain-text body with status `400` (bad request), `500` (storage failure) or `502` (model call failed; the body includes Workers AI's error message).

## Design decisions and trade-offs

**Why Go?** I wanted to build on Cloudflare in Go. `workers-go` lets a standard `http.Handler` run on Workers, so the request-handling code is ordinary Go.

**Why KV for state, and not Durable Objects?** `workers-go` can call Durable Object stubs but can't define Durable Object classes, so a Go-only design can't own one. KV is the simplest persistent store available to Go Workers. The trade-off is that KV is eventually consistent, and concurrent requests on the same session can overwrite each other (last write wins). That is acceptable for a single-user chat session. D1 would give stronger consistency, and a Durable Object would serialize requests per session.

**Why the REST API for Workers AI?** The `workers-go` feature list doesn't include a Workers AI binding, so the Worker calls the Workers AI REST endpoint with an API token instead. A native binding would remove the token and a network hop.

**Bounded context.** Only the most recent 20 messages are sent to the model. This keeps latency and token usage predictable, at the cost of forgetting older turns.

**Experimental dependency.** `workers-go` describes itself as experimental. I pinned a version in `go.mod` and kept the Go-specific surface small (HTTP, KV, fetch) to limit risk.

## Known limitations

- No streaming yet; the full reply appears when generation finishes.
- No authentication. Anyone who knows a session ID can read that history.
- No rate limiting or abuse protection on `/api/chat`. Every request spends the Workers AI quota tied to the API token, so anyone who finds a public deployment can run it up.
- Long conversations are truncated, not summarized.

## What I'd build next

1. **Streaming responses** (`stream: true`, server-sent events) so text appears as it is generated.
2. **Summarization with a Cloudflare Workflow or Queue**: when a session grows past a threshold, summarize older turns in the background and store the summary alongside recent messages.
3. **Hybrid architecture**: a small TypeScript Worker owning a Durable Object per session (serialized writes, SQLite-backed storage) and the native AI binding, with the Go Worker calling it through stubs.
4. **Rate limiting and basic auth** to protect the model endpoint.
5. **Voice input** using the Web Speech API or Cloudflare Realtime.

