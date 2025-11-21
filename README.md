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

- `proxy:` is now a list, so you can run multiple listeners (HTTP/HTTPS, or multiple backends) in one file.
- Keep rules next to each proxy, or reuse them with `include:`—works for both proxy entries and rule lists.
- CLI overrides for listen/target/timeout/SSL only work when exactly one proxy is defined in the file.
- First matching rule wins; order rules from specific to general. Use `all: true` to match every request.
- Supports SSL termination (works great with [mkcert](https://github.com/FiloSottile/mkcert)).

## Development

```sh
# Run
go run main.go --config config.yml

# Build
go build -o bin/ .
```
