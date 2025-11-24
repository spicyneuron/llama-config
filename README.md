# Llama Matchmaker

Managing multiple local LLMs can be a pain. Each model has different optimal parameters, and each client needs to be individually reconfigured when new models drop.

Llama Matchmaker is a lightweight proxy that sits between your LLM clients and servers, automatically matching each request with model or situation-specific options. It's the single place to configure all your LLMs.

Designed with amazing projects like [llama-swap](https://github.com/mostlygeek/llama-swap), [llama.cpp server](https://github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md), and [mlx-lm](https://github.com/ml-explore/mlx-lm/) in mind.

## What It Does

- Sits in front of your LLM backend(s) and rewrites requests using declarative YAML rules.
- Can match on methods, paths, headers, and JSON body parameters.
- Can rewrite request paths, apply defaults, update/delete fields, and even render custom JSON via go templates.
- Automatic hot-reloads when configs change.
- Works with SSL and plain HTTP.

## Quickstart

```sh
# Install to $GOPATH/bin
go install github.com/spicyneuron/llama-matchmaker@latest

# Grab and edit the example config
curl -L -o example.config.yml https://raw.githubusercontent.com/spicyneuron/llama-matchmaker/main/examples/example.config.yml

# Start the proxy
llama-matchmaker --config example.config.yml

# Configure your clients to point at http://localhost:8081
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
go run main.go --config examples/example.config.yml

# Test
go test ./...

# Build
go build -o bin/ .
```
