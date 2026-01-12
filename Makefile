.PHONY: test fmt vet docs docs-clean

test:
	go test ./...

fmt:
	@gofmt -w .

vet:
	go vet ./...

generate:
	go generate ./...
	@gofmt -w ./generated

docs:
	@echo "Generating SDK documentation into build/docs..."
	@if [ -n "$(VERSION)" ]; then \
		rm -rf build/docs_tmp build/docs/$(VERSION) build/docs/latest; \
		mkdir -p build/docs_tmp build/docs/$(VERSION) build/docs/latest; \
		VERSION=$(VERSION) go run ./cmd/docsgen -out build/docs_tmp; \
		cp -R build/docs_tmp/* build/docs/$(VERSION)/; \
		cp -R build/docs_tmp/* build/docs/latest/; \
			printf '%s\n' \
				'<!doctype html>' \
				'<meta charset="utf-8" />' \
				'<meta http-equiv="refresh" content="0; url=./latest/" />' \
				'<link rel="canonical" href="./latest/" />' \
				'<title>Seclai Go SDK Docs</title>' \
				'<p>Redirecting to <a href="./latest/">latest docs</a>…</p>' \
				> build/docs/index.html; \
	else \
		rm -rf build/docs; \
		VERSION=0.0.0 go run ./cmd/docsgen -out build/docs; \
	fi

docs-clean:
	rm -rf build/docs build/docs_tmp
