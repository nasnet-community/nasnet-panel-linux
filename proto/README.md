# Proto Generation

Generate Go code from protobuf definitions.

## Prerequisites

Install protoc and Go plugins:

```bash
# Install protoc (using brew on macOS)
brew install protobuf

# Install Go protoc plugins
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
```

## Generate

```bash
# From project root
protoc --go_out=. --go_opt=paths=source_relative \
       --go-grpc_out=. --go-grpc_opt=paths=source_relative \
       proto/node_agent.proto
```

Or use the Makefile:

```bash
make proto
```
