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

### Get agent run details (optionally include step outputs)

By default, agent run details may omit per-step outputs. To request step details, set `IncludeStepOutputs`.

```go
run, err := client.GetAgentRunByIDWithOptions(context.Background(), "run_id", &seclai.GetAgentRunOptions{
	IncludeStepOutputs: true,
})
if err != nil {
	log.Fatal(err)
}

fmt.Println("run status:", run.Status)
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

### Upload a file to a source

**Max file size:** 200 MiB.

**Supported MIME types:**
- `application/epub+zip`
- `application/json`
- `application/msword`
- `application/pdf`
- `application/vnd.ms-excel`
- `application/vnd.ms-outlook`
- `application/vnd.ms-powerpoint`
- `application/vnd.openxmlformats-officedocument.presentationml.presentation`
- `application/vnd.openxmlformats-officedocument.spreadsheetml.sheet`
- `application/vnd.openxmlformats-officedocument.wordprocessingml.document`
- `application/xml`
- `application/zip`
- `audio/flac`, `audio/mp4`, `audio/mpeg`, `audio/ogg`, `audio/wav`
- `image/bmp`, `image/gif`, `image/jpeg`, `image/png`, `image/tiff`, `image/webp`
- `text/csv`, `text/html`, `text/markdown`, `text/x-markdown`, `text/plain`, `text/xml`
- `video/mp4`, `video/quicktime`, `video/x-msvideo`

If the upload is sent as `application/octet-stream`, the server attempts to infer the type from the file extension.

```go
upload, err := client.UploadFileToSource(context.Background(), "source_connection_id", seclai.UploadFileRequest{
	File:     []byte("hello"),
	FileName: "hello.txt",
	MimeType: "text/plain",
	Title:    "Hello",
	Metadata: map[string]any{"category": "docs", "author": "Ada"},
})
if err != nil {
	log.Fatal(err)
}
fmt.Println("upload:", upload.Filename, upload.Status)
```

### Replace an existing content version (upload a new file)

To replace the file backing an existing content version, upload a new file to `/contents/{source_connection_content_version}/upload`.

```go
upload, err := client.UploadFileToContent(context.Background(), "content_version_id", seclai.UploadFileRequest{
	File:     []byte("%PDF-1.4 ..."),
	FileName: "updated.pdf",
	MimeType: "application/pdf",
	Metadata: map[string]any{"revision": 2},
})
if err != nil {
	log.Fatal(err)
}
fmt.Println("upload:", upload.Filename, upload.Status)
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

