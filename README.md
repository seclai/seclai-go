# Seclai Go SDK

This is the official Seclai Go SDK.

## Install

```bash
go get github.com/seclai/seclai-go@latest
```

## API documentation

Online API documentation (latest):

https://seclai.github.io/seclai-go/latest/

Generate HTML docs into `build/docs/`:

```bash
make docs VERSION=0.0.0
```

## OpenAPI spec & regenerating the client

Put the OpenAPI JSON file at:

- openapi/seclai.openapi.json

This should match the spec used by the other SDK repos.

Regenerate the typed client and models:

```bash
make generate
```

## Usage

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	seclai "github.com/seclai/seclai-go"
)

func main() {
	client, err := seclai.NewClient(seclai.Options{APIKey: os.Getenv("SECLAI_API_KEY")})
	if err != nil {
		log.Fatal(err)
	}

	sources, err := client.ListSources(context.Background(), 1, 20, "", "", "")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("sources:", len(sources.Data))
}
```

### Run an agent with SSE streaming (wait for final result)

This helper returns when the stream emits the final `done` event; it returns an error if the stream ends early or the context deadline is reached.

```go
// If ctx has no deadline, the SDK applies a default timeout.
// To control the timeout yourself, pass a context with a deadline.
run, err := client.RunStreamingAgentAndWait(context.Background(), "agent_id", seclai.AgentRunStreamRequest{
	Input:    "Hello from streaming",
	Metadata: map[string]any{"app": "My App"},
})
if err != nil {
	log.Fatal(err)
}

fmt.Println("run:", run.Id, "status=", run.Status)
```

## Development

### Base URL

Set `SECLAI_API_URL` to point at a different API host (e.g., staging):

```bash
export SECLAI_API_URL="https://example.invalid"
```

### Test

```bash
make test
```

