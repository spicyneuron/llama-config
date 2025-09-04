# Llama Config

Managing multiple local LLMs can be a pain. Each model has different optimal parameters, but configuring every client individually is tedious.

This lightweight proxy sits between your LLM clients and servers, automatically applying your preferred settings to each request.

## Usage

```sh
# Install to $GOPATH/bin
go install github.com/spicyneuron/llama-config@latest

# Configure your server (llama.cpp, mlx_lm.server, etc) and model settings
curl -o config.yml https://raw.githubusercontent.com/spicyneuron/llama-config/main/example.config.yml

# Start the proxy
llama-config --config config.yml
```

**Notes:**

- The proxy will automatically apply the correct config to each request based on the model name in the request body.
- First match wins, so order your rules from specific to general.
- Supports SSL termination (works great with [mkcert](https://github.com/FiloSottile/mkcert)).
- All `proxy:` settings can also be set as CLI flags.

## Development

```sh
# Run
go run main.go --config config.yml

# Build
go build -o bin/ .
```