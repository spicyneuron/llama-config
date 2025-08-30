# LLM Config Proxy

Managing local LLMs is a pain. Each model's recommended parameters differ, yet each client need to be configured separately.

This simple, lightweight proxy sits in between your LLM client(s) and server(s),dutifully applying your preferred configuration to each model.

## Usage

```sh
# Check example.config.yml for format
llm-config-proxy -c config.yml
```

## Development

```sh
# Run
go run main.go -c config.yml

# Build
go build -o bin/ .
```