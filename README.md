# Llama Config Proxy

Managing multiple local LLMs can be a pain. Each model has different optimal parameters, but configuring every client individually is tedious.

This lightweight proxy sits between your LLM clients and servers, automatically applying your preferred settings to each request.

## Usage

```sh
# Install to $GOPATH/bin
go install github.com/spicyneuron/llama-config-proxy@latest

# Configure your server (llama.cpp, mlx_lm.server, etc) and model settings
curl -o config.yml https://raw.githubusercontent.com/spicyneuron/llama-config-proxy/main/example.config.yml

# Start the proxy
llama-config-proxy --config config.yml
```

**Notes:**

- Each request's model will be compared against your rules, and if matching, receive updated `params`.
- First match wins, so order your rules from specific to general.
- Use `all: true` to match every request, regardless of model.
- Supports SSL termination (works great with [mkcert](https://github.com/FiloSottile/mkcert)).
- All `proxy:` settings can also be set as CLI flags.

## Development

```sh
# Run
go run main.go --config config.yml

# Build
go build -o bin/ .
```