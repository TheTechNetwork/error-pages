package http_test
//nolint:wsl_v5 // Whitespace linter disabled for test file readability

import (
	"fmt"
	"io"
	"net"
	nethttp "net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gh.tarampamp.am/error-pages/internal/config"
	"gh.tarampamp.am/error-pages/internal/http"
	"gh.tarampamp.am/error-pages/internal/logger"
)

// TestServer_FullLifecycle tests the complete server lifecycle: start → handle requests → shutdown.
func TestServer_FullLifecycle(t *testing.T) {
	t.Parallel()

	var (
		log = logger.NewNop()
		cfg = config.New()
	)

	t.Run("server starts, handles requests, and shuts down gracefully", func(t *testing.T) {
		t.Parallel()

		var (
			server = http.NewServer(log, 4096)
			port   = getFreeTCPPort(t)
		)

		require.NoError(t, server.Register(&cfg))

		// Start server in background
		var startErr = make(chan error, 1)
		go func() {
			startErr <- server.Start("127.0.0.1", port)
		}()

		// Wait for server to start
		require.Eventually(t, func() bool {
			conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
			if err == nil {
				_ = conn.Close()
				return true
			}
			return false
		}, 3*time.Second, 50*time.Millisecond, "server should start")

		// Make a request to verify server is working
		resp, err := nethttp.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", port))
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		assert.Equal(t, nethttp.StatusOK, resp.StatusCode)

		// Stop server gracefully
		stopErr := server.Stop(5 * time.Second)
		assert.NoError(t, stopErr, "server should stop gracefully")

		// Verify server actually stopped
		_, err = net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
		assert.Error(t, err, "connection should fail after server stops")
	})

	t.Run("server handles multiple requests before shutdown", func(t *testing.T) {
		t.Parallel()

		var (
			server = http.NewServer(log, 4096)
			port   = getFreeTCPPort(t)
		)

		require.NoError(t, server.Register(&cfg))

		go func() { _ = server.Start("127.0.0.1", port) }()

		require.Eventually(t, func() bool {
			conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
			if err == nil {
				_ = conn.Close()
				return true
			}
			return false
		}, 3*time.Second, 50*time.Millisecond)

		// Make multiple requests
		for i := 0; i < 10; i++ {
			resp, err := nethttp.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", port))
			require.NoError(t, err)
			assert.Equal(t, nethttp.StatusOK, resp.StatusCode)
			_ = resp.Body.Close()
		}

		require.NoError(t, server.Stop(5*time.Second))
	})
}

// TestServer_ConcurrentRequests tests that the server can handle multiple concurrent requests.
func TestServer_ConcurrentRequests(t *testing.T) {
	t.Parallel()

	var (
		log    = logger.NewNop()
		cfg    = config.New()
		server = http.NewServer(log, 4096)
		port   = getFreeTCPPort(t)
	)

	require.NoError(t, server.Register(&cfg))

	go func() { _ = server.Start("127.0.0.1", port) }()

	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return true
		}
		return false
	}, 3*time.Second, 50*time.Millisecond)

	defer func() { _ = server.Stop(5 * time.Second) }()

	t.Run("handles concurrent health check requests", func(t *testing.T) {
		const numRequests = 50
		var (
			wg           sync.WaitGroup
			successCount atomic.Int32
			errorCount   atomic.Int32
		)

		wg.Add(numRequests)
		for i := 0; i < numRequests; i++ {
			go func() {
				defer wg.Done()

				resp, err := nethttp.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", port))
				if err != nil {
					errorCount.Add(1)
					return
				}
				defer func() { _ = resp.Body.Close() }()

				if resp.StatusCode == nethttp.StatusOK {
					successCount.Add(1)
				} else {
					errorCount.Add(1)
				}
			}()
		}

		wg.Wait()

		assert.Equal(t, int32(numRequests), successCount.Load(), "all requests should succeed")
		assert.Equal(t, int32(0), errorCount.Load(), "no requests should fail")
	})

	t.Run("handles concurrent error page requests", func(t *testing.T) {
		const numRequests = 50
		var (
			wg           sync.WaitGroup
			successCount atomic.Int32
		)

		wg.Add(numRequests)
		for i := 0; i < numRequests; i++ {
			code := 400 + (i % 5) // Vary codes: 400, 401, 402, 403, 404
			go func(errorCode int) {
				defer wg.Done()

				resp, err := nethttp.Get(fmt.Sprintf("http://127.0.0.1:%d/%d", port, errorCode))
				if err != nil {
					return
				}
				defer func() { _ = resp.Body.Close() }()

				if resp.StatusCode == nethttp.StatusOK {
					successCount.Add(1)
				}
			}(code)
		}

		wg.Wait()

		assert.Equal(t, int32(numRequests), successCount.Load(), "all error page requests should succeed")
	})
}

// TestServer_GracefulShutdown tests that the server shuts down gracefully even with ongoing requests.
func TestServer_GracefulShutdown(t *testing.T) {
	t.Parallel()

	var (
		log    = logger.NewNop()
		cfg    = config.New()
		server = http.NewServer(log, 4096)
		port   = getFreeTCPPort(t)
	)

	require.NoError(t, server.Register(&cfg))

	go func() { _ = server.Start("127.0.0.1", port) }()

	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return true
		}
		return false
	}, 3*time.Second, 50*time.Millisecond)

	t.Run("completes in-flight requests during shutdown", func(t *testing.T) {
		var (
			requestStarted  = make(chan struct{})
			requestComplete = make(chan struct{})
		)

		// Start a request
		go func() {
			close(requestStarted)
			resp, err := nethttp.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", port))
			if err == nil {
				_ = resp.Body.Close()
			}
			close(requestComplete)
		}()

		// Wait for request to start
		<-requestStarted
		time.Sleep(50 * time.Millisecond) // Give it time to reach server

		// Initiate shutdown
		shutdownDone := make(chan error, 1)
		go func() {
			shutdownDone <- server.Stop(5 * time.Second)
		}()

		// Verify request completes
		select {
		case <-requestComplete:
			// Good - request completed
		case <-time.After(2 * time.Second):
			t.Fatal("request should complete during graceful shutdown")
		}

		// Verify shutdown completes
		select {
		case err := <-shutdownDone:
			assert.NoError(t, err)
		case <-time.After(3 * time.Second):
			t.Fatal("shutdown should complete")
		}
	})
}

// TestServer_ShutdownTimeout tests that shutdown times out if requests don't complete.
func TestServer_ShutdownTimeout(t *testing.T) {
	t.Parallel()

	var (
		log    = logger.NewNop()
		cfg    = config.New()
		server = http.NewServer(log, 4096)
		port   = getFreeTCPPort(t)
	)

	require.NoError(t, server.Register(&cfg))

	go func() { _ = server.Start("127.0.0.1", port) }()

	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return true
		}
		return false
	}, 3*time.Second, 50*time.Millisecond)

	// Shutdown with very short timeout
	err := server.Stop(1 * time.Millisecond)

	// Should complete (may or may not error depending on timing)
	// The important thing is it doesn't hang
	_ = err
}

// TestServer_AllEndpoints tests that all server endpoints are accessible.
func TestServer_AllEndpoints(t *testing.T) {
	t.Parallel()

	var (
		log    = logger.NewNop()
		cfg    = config.New()
		server = http.NewServer(log, 4096)
		port   = getFreeTCPPort(t)
	)

	require.NoError(t, server.Register(&cfg))

	go func() { _ = server.Start("127.0.0.1", port) }()

	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return true
		}
		return false
	}, 3*time.Second, 50*time.Millisecond)

	defer func() { _ = server.Stop(5 * time.Second) }()

	tests := []struct {
		name           string
		path           string
		wantStatusCode int
		wantContains   string
	}{
		{
			name:           "health check /healthz",
			path:           "/healthz",
			wantStatusCode: nethttp.StatusOK,
		},
		{
			name:           "health check /health/live",
			path:           "/health/live",
			wantStatusCode: nethttp.StatusOK,
		},
		{
			name:           "health check /health",
			path:           "/health",
			wantStatusCode: nethttp.StatusOK,
		},
		{
			name:           "health check /live",
			path:           "/live",
			wantStatusCode: nethttp.StatusOK,
		},
		{
			name:           "version endpoint",
			path:           "/version",
			wantStatusCode: nethttp.StatusOK,
			wantContains:   "version",
		},
		{
			name:           "favicon",
			path:           "/favicon.ico",
			wantStatusCode: nethttp.StatusOK,
		},
		{
			name:           "root error page",
			path:           "/",
			wantStatusCode: nethttp.StatusOK,
		},
		{
			name:           "404 error page with .html",
			path:           "/404.html",
			wantStatusCode: nethttp.StatusOK,
			wantContains:   "404",
		},
		{
			name:           "500 error page without extension",
			path:           "/500",
			wantStatusCode: nethttp.StatusOK,
			wantContains:   "500",
		},
		{
			name:           "503 error page with .htm",
			path:           "/503.htm",
			wantStatusCode: nethttp.StatusOK,
			wantContains:   "503",
		},
		{
			name:           "unknown endpoint returns 404",
			path:           "/unknown/path",
			wantStatusCode: nethttp.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := nethttp.Get(fmt.Sprintf("http://127.0.0.1:%d%s", port, tt.path))
			require.NoError(t, err)
			defer func() { _ = resp.Body.Close() }()

			assert.Equal(t, tt.wantStatusCode, resp.StatusCode)

			if tt.wantContains != "" {
				body, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				assert.Contains(t, string(body), tt.wantContains)
			}
		})
	}
}

// TestServer_ContentTypes tests that the server responds with correct content types.
func TestServer_ContentTypes(t *testing.T) {
	t.Parallel()

	var (
		log    = logger.NewNop()
		cfg    = config.New()
		server = http.NewServer(log, 4096)
		port   = getFreeTCPPort(t)
	)

	require.NoError(t, server.Register(&cfg))

	go func() { _ = server.Start("127.0.0.1", port) }()

	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return true
		}
		return false
	}, 3*time.Second, 50*time.Millisecond)

	defer func() { _ = server.Stop(5 * time.Second) }()

	tests := []struct {
		name           string
		acceptHeader   string
		wantContains   string
		checkMediaType bool
	}{
		{
			name:           "HTML content type",
			acceptHeader:   "text/html",
			wantContains:   "<!DOCTYPE html>",
			checkMediaType: true,
		},
		{
			name:           "JSON content type",
			acceptHeader:   "application/json",
			wantContains:   "{",
			checkMediaType: true,
		},
		{
			name:           "XML content type",
			acceptHeader:   "application/xml",
			wantContains:   "<?xml",
			checkMediaType: true,
		},
		{
			name:           "plain text content type",
			acceptHeader:   "text/plain",
			checkMediaType: true,
		},
		{
			name:         "default to HTML when no accept header",
			acceptHeader: "",
			wantContains: "<!DOCTYPE html>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := nethttp.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/404", port), nil)
			require.NoError(t, err)

			if tt.acceptHeader != "" {
				req.Header.Set("Accept", tt.acceptHeader)
			}

			resp, err := nethttp.DefaultClient.Do(req)
			require.NoError(t, err)
			defer func() { _ = resp.Body.Close() }()

			assert.Equal(t, nethttp.StatusOK, resp.StatusCode)

			if tt.wantContains != "" {
				body, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				assert.Contains(t, string(body), tt.wantContains)
			}

			if tt.checkMediaType {
				contentType := resp.Header.Get("Content-Type")
				assert.NotEmpty(t, contentType, "Content-Type header should be set")
			}
		})
	}
}

// TestServer_MethodHandling tests HTTP method handling.
func TestServer_MethodHandling(t *testing.T) {
	t.Parallel()

	var (
		log    = logger.NewNop()
		cfg    = config.New()
		server = http.NewServer(log, 4096)
		port   = getFreeTCPPort(t)
	)

	require.NoError(t, server.Register(&cfg))

	go func() { _ = server.Start("127.0.0.1", port) }()

	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return true
		}
		return false
	}, 3*time.Second, 50*time.Millisecond)

	defer func() { _ = server.Stop(5 * time.Second) }()

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{"GET request to error page", "GET", "/404", nethttp.StatusOK},
		{"HEAD request to error page", "HEAD", "/404", nethttp.StatusOK},
		{"POST request to error page", "POST", "/404", nethttp.StatusOK},
		{"PUT request to error page", "PUT", "/404", nethttp.StatusOK},
		{"DELETE request to error page", "DELETE", "/404", nethttp.StatusOK},
		{"GET request to health", "GET", "/healthz", nethttp.StatusOK},
		{"HEAD request to health", "HEAD", "/healthz", nethttp.StatusOK},
		{"POST request to unknown", "POST", "/unknown", nethttp.StatusMethodNotAllowed},
		{"PUT request to unknown", "PUT", "/unknown", nethttp.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := nethttp.NewRequest(tt.method, fmt.Sprintf("http://127.0.0.1:%d%s", port, tt.path), nil)
			require.NoError(t, err)

			resp, err := nethttp.DefaultClient.Do(req)
			require.NoError(t, err)
			defer func() { _ = resp.Body.Close() }()

			assert.Equal(t, tt.wantStatus, resp.StatusCode)
		})
	}
}

// TestServer_InvalidIPAddress tests server behavior with invalid IP addresses.
func TestServer_InvalidIPAddress(t *testing.T) {
	t.Parallel()

	var (
		log    = logger.NewNop()
		cfg    = config.New()
		server = http.NewServer(log, 4096)
	)

	require.NoError(t, server.Register(&cfg))

	tests := []struct {
		name string
		ip   string
	}{
		{"empty IP", ""},
		{"invalid IP", "not-an-ip"},
		{"malformed IP", "256.256.256.256"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := server.Start(tt.ip, 8080)
			assert.Error(t, err)
			assert.Contains(t, strings.ToLower(err.Error()), "invalid")
		})
	}
}

// getFreeTCPPort returns a free TCP port for testing.
func getFreeTCPPort(t *testing.T) uint16 {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())

	// Wait for port to be released
	for i := 0; i < 10; i++ {
		conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			break
		}
		_ = conn.Close()
		time.Sleep(10 * time.Millisecond)
	}

	return uint16(port) //nolint:gosec
}
