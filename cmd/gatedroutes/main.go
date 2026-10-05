// Command gatedroutes lists the API routes whose list response is
// version-gated, read from the backend's router sources.
//
// A route is gated when its handler returns through versioned_list_response,
// versioned_offset_list_response or versioned_complete_list_response. The
// output is the checked-in testdata/gated_routes.txt, which the tests hold the
// client's list methods against:
//
//	go run ./cmd/gatedroutes -routers ../seclai/backend/api/src/api/routers/api > testdata/gated_routes.txt
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	prefixRe    = regexp.MustCompile(`APIRouter\(\s*prefix="([^"]*)"`)
	decoratorRe = regexp.MustCompile(`^@router\.(get|post|put|patch|delete)\(\s*(?:"([^"]*)")?`)
	pathArgRe   = regexp.MustCompile(`^\s*"([^"]*)"`)
	handlerRe   = regexp.MustCompile(`^(?:async )?def `)
	gatedCallRe = regexp.MustCompile(`\bversioned_(?:offset_|complete_)?list_response\(`)
)

// gatedRoutes returns the "METHOD /path" of every gated handler in one router source.
func gatedRoutes(source string) []string {
	prefix := ""
	if m := prefixRe.FindStringSubmatch(source); m != nil {
		prefix = strings.TrimPrefix(m[1], "/api")
	}

	var routes, pending, current []string
	lines := strings.Split(source, "\n")
	for i, line := range lines {
		if m := decoratorRe.FindStringSubmatch(line); m != nil {
			path, found := m[2], strings.Contains(m[0], `"`)
			if !found && i+1 < len(lines) {
				if arg := pathArgRe.FindStringSubmatch(lines[i+1]); arg != nil {
					path, found = arg[1], true
				}
			}
			if found {
				route := strings.ToUpper(m[1]) + " " + strings.TrimSuffix(prefix+path, "/")
				pending = append(pending, route)
			}
			continue
		}
		if handlerRe.MatchString(line) {
			current, pending = pending, nil
			continue
		}
		if gatedCallRe.MatchString(line) && !strings.Contains(line, "import") {
			routes = append(routes, current...)
		}
	}
	return routes
}

func main() {
	routers := flag.String("routers", "", "directory holding the backend's /api router sources")
	flag.Parse()
	if *routers == "" {
		fmt.Fprintln(os.Stderr, "usage: gatedroutes -routers <dir>")
		os.Exit(2)
	}
	files, err := filepath.Glob(filepath.Join(*routers, "*.py"))
	if err != nil || len(files) == 0 {
		fmt.Fprintf(os.Stderr, "no router sources under %s\n", *routers)
		os.Exit(1)
	}

	seen := map[string]bool{}
	for _, file := range files {
		source, err := os.ReadFile(file)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		for _, route := range gatedRoutes(string(source)) {
			seen[route] = true
		}
	}
	routes := make([]string, 0, len(seen))
	for route := range seen {
		routes = append(routes, route)
	}
	sort.Strings(routes)

	fmt.Println("# Version-gated list routes, generated from the backend's router sources.")
	fmt.Println("# Do not edit by hand. Regenerate from the repository root with:")
	fmt.Println("#   go run ./cmd/gatedroutes -routers ../seclai/backend/api/src/api/routers/api > testdata/gated_routes.txt")
	for _, route := range routes {
		fmt.Println(route)
	}
}
