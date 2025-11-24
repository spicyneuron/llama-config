# Llama Config Proxy

Managing multiple local LLMs can be a pain. Each model has different optimal parameters, and each client needs to be individually reconfigured when new models drop.

This lightweight proxy sits between your LLM clients and servers, automatically transforming each request with model-specific logic.

## What It Does

- Sits in front of your LLM backends and rewrites requests/responses using declarative YAML rules.
- Can match on methods, paths, headers, and JSON request body parameters.
- Can rewrite request paths, apply defaults, update/delete fields, and render custom JSON via templates.
- Hot-reloads when configs change.
- Works with SSL or plain HTTP.

## Quickstart

```sh
# Install to $GOPATH/bin
go install github.com/spicyneuron/llama-config-proxy@latest

# Grab the latest examples
curl -L https://github.com/spicyneuron/llama-config-proxy/archive/refs/heads/main.tar.gz \
  | tar -xz --strip-components=1 llama-config-proxy-main/examples

# Start with the combined example (point it at your backend target)
llama-config-proxy --config examples/combined.yml
```

## Configuration & Behavior

Start from `examples/example.config.yml` for an annotated, OpenAI-compatible chat setup. At a glance:

- Hierarchy: a `proxy` has ordered `rules`; each rule has ordered `operations`. All matching rules run; only the last matched rule’s `on_response` runs.
- Proxies live under `proxy:` (single map or list). Each has `listen` and `target`; optional `timeout` and `ssl_cert`/`ssl_key`.
- Rules match with case-insensitive regex on method/path. `target_path` rewrites outbound paths. `on_request` processes JSON bodies; non-JSON bodies pass through untouched.
- Reuse proxies, rules, or operations with `include:`; paths resolve relative to the file that references them.
- Operations:
  - `merge` (override fields)
  - `default` (set if missing)
  - `delete` (remove keys)
  - `template` (emit JSON with helpers like `toJson`, `default`, `uuid`, `now`, `add`, `mul`, `dict`, `index`, `kindIs`)
  - `stop` (end remaining ops in the same rule)
- Passing multiple `--config` files appends proxies and rules. CLI overrides for `listen/target/timeout/ssl-*` only work when exactly one proxy is defined.

## Development

```sh
# Run
go run main.go --config examples/combined.yml

# Test
go test ./...

# Build
go build -o bin/ .
```
