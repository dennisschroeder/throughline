// Command releasesmoke is a release-pipeline-only check: it connects to a running throughline
// daemon over Streamable HTTP and verifies get_semantic_model round-trips, so a packaged
// release archive is proven to serve the embedded model before it is published. It is not
// part of the throughline binary or any documented CLI surface.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type bearerRoundTripper struct {
	token string
	base  http.RoundTripper
}

func (rt bearerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+rt.token)
	return rt.base.RoundTrip(req)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "releasesmoke:", err)
		os.Exit(1)
	}
}

func run() error {
	endpoint := os.Getenv("THROUGHLINE_SMOKE_ENDPOINT")
	token := os.Getenv("THROUGHLINE_SMOKE_TOKEN")
	if endpoint == "" || token == "" {
		return fmt.Errorf("THROUGHLINE_SMOKE_ENDPOINT and THROUGHLINE_SMOKE_TOKEN are required")
	}

	httpClient := &http.Client{Transport: bearerRoundTripper{token: token, base: http.DefaultTransport}}
	transport := &mcp.StreamableClientTransport{Endpoint: endpoint, HTTPClient: httpClient, DisableStandaloneSSE: true}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "release-smoke", Version: "v1"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer session.Close()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_semantic_model", Arguments: map[string]any{}})
	if err != nil {
		return fmt.Errorf("call get_semantic_model: %w", err)
	}
	if result.IsError {
		return fmt.Errorf("get_semantic_model returned an error result: %+v", result.Content)
	}
	for _, block := range result.Content {
		if text, ok := block.(*mcp.TextContent); ok && text.Text != "" {
			if !contains(text.Text, `"model_version"`) {
				return fmt.Errorf("response missing model_version: %s", text.Text)
			}
			fmt.Println("releasesmoke: get_semantic_model ok")
			return nil
		}
	}
	return fmt.Errorf("get_semantic_model returned no text content")
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
