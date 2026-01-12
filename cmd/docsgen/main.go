package main

import (
	"bytes"
	"flag"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	var outDir string
	flag.StringVar(&outDir, "out", "build/docs", "Output directory")
	flag.Parse()

	version := os.Getenv("VERSION")
	if strings.TrimSpace(version) == "" {
		version = "0.0.0"
	}

	// Use `go doc` to generate documentation from GoDoc comments.
	// This keeps docs generation idiomatic and dependency-light.
	pkgs, err := goListPackages()
	if err != nil {
		fmt.Fprintf(os.Stderr, "docsgen: go list failed: %v\n", err)
		os.Exit(1)
	}

	var docBuf strings.Builder
	for _, pkg := range pkgs {
		out, err := goDocPackage(pkg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "docsgen: go doc failed for %s: %v\n", pkg, err)
			os.Exit(1)
		}
		docBuf.WriteString("\n=== ")
		docBuf.WriteString(pkg)
		docBuf.WriteString(" ===\n\n")
		docBuf.WriteString(out)
		if !strings.HasSuffix(out, "\n") {
			docBuf.WriteString("\n")
		}
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "docsgen: mkdir: %v\n", err)
		os.Exit(1)
	}

	docText := docBuf.String()
	page := fmt.Sprintf(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Seclai Go SDK Docs (v%s)</title>
  <style>
    :root { color-scheme: light dark; }
    body { max-width: 960px; margin: 0 auto; padding: 24px; font-family: system-ui, -apple-system, Segoe UI, Roboto, Helvetica, Arial, sans-serif; }
    h1 { margin: 0 0 12px; }
    pre { padding: 12px; overflow: auto; border: 1px solid rgba(127,127,127,0.35); border-radius: 8px; }
    code { font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace; }
    .meta { opacity: 0.8; margin: 0 0 18px; }
  </style>
</head>
<body>
  <h1>Seclai Go SDK</h1>
  <p class="meta">Version: <code>%s</code></p>
  <p class="meta">Generated from <code>go doc</code>.</p>
  <pre><code>%s</code></pre>
</body>
</html>
`, html.EscapeString(version), html.EscapeString(version), html.EscapeString(docText))

	outPath := filepath.Join(outDir, "index.html")
	if err := os.WriteFile(outPath, []byte(page), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "docsgen: write: %v\n", err)
		os.Exit(1)
	}
}

func goListPackages() ([]string, error) {
	cmd := exec.Command("go", "list", "./...")
	cmd.Env = os.Environ()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}

	lines := strings.Split(stdout.String(), "\n")
	pkgs := make([]string, 0, len(lines))
	for _, line := range lines {
		p := strings.TrimSpace(line)
		if p == "" {
			continue
		}
		pkgs = append(pkgs, p)
	}
	return pkgs, nil
}

func goDocPackage(pkg string) (string, error) {
	cmd := exec.Command("go", "doc", "-all", pkg)
	cmd.Env = os.Environ()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
