package daemonhttp

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestServerWithRealPort starts an httptest server first (so the ephemeral port is
// known) and only then installs a Protect-wrapped handler configured with that exact
// host:port as the sole allowed host.
func newTestServerWithRealPort(t *testing.T, logs *bytes.Buffer) *httptest.Server {
	t.Helper()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	server := httptest.NewUnstartedServer(nil)
	server.Start()
	t.Cleanup(server.Close)
	host := strings.TrimPrefix(server.URL, "http://")
	var logger *slog.Logger
	if logs != nil {
		logger = slog.New(slog.NewJSONHandler(logs, nil))
	}
	server.Config.Handler = Protect(Config{
		Token:           "the-token",
		AllowedHosts:    []string{host},
		MaxRequestBytes: 1024,
		Logger:          logger,
	}, next)
	return server
}

func TestProtectRejectsMissingOrWrongCredential(t *testing.T) {
	server := newTestServerWithRealPort(t, nil)

	response, err := http.Get(server.URL + "/mcp")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing credential status = %d, want 401", response.StatusCode)
	}

	request, _ := http.NewRequest(http.MethodGet, server.URL+"/mcp", nil)
	request.Header.Set("Authorization", "Bearer wrong-token")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong credential status = %d, want 401", response.StatusCode)
	}
}

func TestProtectAcceptsTheCorrectCredential(t *testing.T) {
	server := newTestServerWithRealPort(t, nil)
	request, _ := http.NewRequest(http.MethodGet, server.URL+"/mcp", nil)
	request.Header.Set("Authorization", "Bearer the-token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("correct credential status = %d, want 200", response.StatusCode)
	}
}

func TestProtectRejectsAForeignOriginEvenWithACorrectCredential(t *testing.T) {
	server := newTestServerWithRealPort(t, nil)
	request, _ := http.NewRequest(http.MethodGet, server.URL+"/mcp", nil)
	request.Header.Set("Authorization", "Bearer the-token")
	request.Header.Set("Origin", "https://evil.example.com")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign origin status = %d, want 403", response.StatusCode)
	}
}

func TestProtectAllowsAnAbsentOriginForNativeClients(t *testing.T) {
	server := newTestServerWithRealPort(t, nil)
	request, _ := http.NewRequest(http.MethodGet, server.URL+"/mcp", nil)
	request.Header.Set("Authorization", "Bearer the-token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("absent origin status = %d, want 200", response.StatusCode)
	}
}

func TestProtectRejectsAForeignHostHeader(t *testing.T) {
	server := newTestServerWithRealPort(t, nil)
	request, _ := http.NewRequest(http.MethodGet, server.URL+"/mcp", nil)
	request.Header.Set("Authorization", "Bearer the-token")
	request.Host = "attacker.example.com"
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign host status = %d, want 403", response.StatusCode)
	}
}

func TestProtectNeverEmitsCORSHeaders(t *testing.T) {
	server := newTestServerWithRealPort(t, nil)
	request, _ := http.NewRequest(http.MethodGet, server.URL+"/mcp", nil)
	request.Header.Set("Authorization", "Bearer the-token")
	request.Header.Set("Origin", server.URL)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	for header := range response.Header {
		if strings.HasPrefix(strings.ToLower(header), "access-control-") {
			t.Fatalf("unexpected CORS header %q", header)
		}
	}
}

func TestProtectEnforcesTheRequestBodyLimit(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			http.Error(w, "too large", http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	server := httptest.NewUnstartedServer(nil)
	server.Start()
	t.Cleanup(server.Close)
	host := strings.TrimPrefix(server.URL, "http://")
	server.Config.Handler = Protect(Config{Token: "the-token", AllowedHosts: []string{host}, MaxRequestBytes: 8}, next)

	request, _ := http.NewRequest(http.MethodPost, server.URL+"/mcp", strings.NewReader("this body is far longer than eight bytes"))
	request.Header.Set("Authorization", "Bearer the-token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize body status = %d, want 413", response.StatusCode)
	}
}

func TestProtectLogsRedactedAccessLinesOnly(t *testing.T) {
	var logs bytes.Buffer
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	server := httptest.NewUnstartedServer(nil)
	server.Start()
	t.Cleanup(server.Close)
	host := strings.TrimPrefix(server.URL, "http://")
	server.Config.Handler = Protect(Config{
		Token: "super-secret-token", AllowedHosts: []string{host},
		Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
	}, next)

	request, _ := http.NewRequest(http.MethodGet, server.URL+"/mcp?leak=me", nil)
	request.Header.Set("Authorization", "Bearer super-secret-token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()

	output := logs.String()
	if strings.Contains(output, "super-secret-token") {
		t.Fatalf("access log leaked the bearer token: %s", output)
	}
	if strings.Contains(output, "Authorization") {
		t.Fatalf("access log leaked the Authorization header name: %s", output)
	}
	if !strings.Contains(output, `"status":200`) {
		t.Fatalf("access log missing status: %s", output)
	}
	if !strings.Contains(output, "request_id") {
		t.Fatalf("access log missing request_id: %s", output)
	}
}
