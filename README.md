# Llama Config

Managing multiple local LLMs can be a pain. Each model has different optimal parameters, but configuring every client individually is tedious.

This lightweight proxy sits between your LLM clients and servers, automatically applying your preferred settings to each request.

## Usage

Configure your clients to point to the proxy, then configure the proxy to point to your LLM servers. The proxy applies the first matching model's configuration to each request.

```sh
# Check example.config.yml for format
llama-config -c config.yml
```

## Development

```sh
# Run
go run main.go -c config.yml

# Build
go build -o bin/ .
```