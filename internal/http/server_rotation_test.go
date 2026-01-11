package http_test
//nolint:wsl_v5 // Whitespace linter disabled for test file readability

import (
	"fmt"
	"io"
	"net"
	nethttp "net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gh.tarampamp.am/error-pages/internal/config"
	"gh.tarampamp.am/error-pages/internal/http"
	"gh.tarampamp.am/error-pages/internal/logger"
)

// TestServer_RotationModeDisabled tests that templates don't rotate when disabled.
func TestServer_RotationModeDisabled(t *testing.T) {
	t.Parallel()

	var (
		log  = logger.NewNop()
		cfg  = config.New()
		port = getFreeTCPPort(t)
	)

	// Setup multiple templates
	_ = cfg.Templates.Add("template1", "<html><body>Template 1: {{.Code}}</body></html>")
	_ = cfg.Templates.Add("template2", "<html><body>Template 2: {{.Code}}</body></html>")
	_ = cfg.Templates.Add("template3", "<html><body>Template 3: {{.Code}}</body></html>")
	cfg.TemplateName = "template1"
	cfg.RotationMode = config.RotationModeDisabled

	var server = http.NewServer(log, 4096)
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

	// Make multiple requests and verify same template is used
	var firstBody string
	for i := 0; i < 10; i++ {
		resp, err := nethttp.Get(fmt.Sprintf("http://127.0.0.1:%d/404", port))
		require.NoError(t, err)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		_ = resp.Body.Close()

		if i == 0 {
			firstBody = string(body)
			assert.Contains(t, firstBody, "Template 1", "should use template1")
		} else {
			assert.Equal(t, firstBody, string(body), "template should not change")
		}

		time.Sleep(10 * time.Millisecond)
	}
}

// TestServer_RotationModeRandomOnEachRequest tests that templates rotate on each request.
func TestServer_RotationModeRandomOnEachRequest(t *testing.T) {
	t.Parallel()

	var (
		log  = logger.NewNop()
		cfg  = config.New()
		port = getFreeTCPPort(t)
	)

	// Setup multiple templates
	_ = cfg.Templates.Add("template1", "<html><body>Template 1: {{.Code}}</body></html>")
	_ = cfg.Templates.Add("template2", "<html><body>Template 2: {{.Code}}</body></html>")
	_ = cfg.Templates.Add("template3", "<html><body>Template 3: {{.Code}}</body></html>")
	cfg.TemplateName = "template1" // Initial template
	cfg.RotationMode = config.RotationModeRandomOnEachRequest

	var server = http.NewServer(log, 4096)
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

	// Make multiple requests and track which templates we see
	var (
		seenTemplates = make(map[string]bool)
		responseCount = 100
		changeCount   = 0
		previousBody  string
	)

	for i := 0; i < responseCount; i++ {
		resp, err := nethttp.Get(fmt.Sprintf("http://127.0.0.1:%d/404", port))
		require.NoError(t, err)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		_ = resp.Body.Close()

		bodyStr := string(body)

		// Track which template was used
		switch {
		case contains(bodyStr, "Template 1"):
			seenTemplates["template1"] = true
		case contains(bodyStr, "Template 2"):
			seenTemplates["template2"] = true
		case contains(bodyStr, "Template 3"):
			seenTemplates["template3"] = true
		}

		// Count changes
		if i > 0 && previousBody != bodyStr {
			changeCount++
		}
		previousBody = bodyStr
	}

	// Verify we saw multiple templates (with random rotation, we should see at least 2 different templates)
	assert.GreaterOrEqual(t, len(seenTemplates), 2, "should see multiple different templates with rotation")

	// Verify templates actually changed between requests
	assert.Greater(t, changeCount, 10, "templates should change frequently with random-on-each-request mode")
}

// TestServer_RotationModeRandomOnStartup tests that a random template is picked on startup.
func TestServer_RotationModeRandomOnStartup(t *testing.T) {
	t.Parallel()

	var (
		log = logger.NewNop()
		cfg = config.New()
	)

	// Setup multiple templates
	_ = cfg.Templates.Add("template1", "<html><body>Template 1: {{.Code}}</body></html>")
	_ = cfg.Templates.Add("template2", "<html><body>Template 2: {{.Code}}</body></html>")
	_ = cfg.Templates.Add("template3", "<html><body>Template 3: {{.Code}}</body></html>")
	cfg.RotationMode = config.RotationModeRandomOnStartup

	// Start multiple servers and track which template each picks
	var seenTemplates = make(map[string]int)

	for i := 0; i < 10; i++ {
		var (
			serverCfg = cfg // Copy config
			port      = getFreeTCPPort(t)
			server    = http.NewServer(log, 4096)
		)

		require.NoError(t, server.Register(&serverCfg))

		go func() { _ = server.Start("127.0.0.1", port) }()

		require.Eventually(t, func() bool {
			conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
			if err == nil {
				_ = conn.Close()
				return true
			}
			return false
		}, 3*time.Second, 50*time.Millisecond)

		// Make a request to see which template was picked
		resp, err := nethttp.Get(fmt.Sprintf("http://127.0.0.1:%d/404", port))
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()

			bodyStr := string(body)
			switch {
			case contains(bodyStr, "Template 1"):
				seenTemplates["template1"]++
			case contains(bodyStr, "Template 2"):
				seenTemplates["template2"]++
			case contains(bodyStr, "Template 3"):
				seenTemplates["template3"]++
			}

			// Make another request to verify template doesn't change
			resp2, err2 := nethttp.Get(fmt.Sprintf("http://127.0.0.1:%d/404", port))
			if err2 == nil {
				body2, _ := io.ReadAll(resp2.Body)
				_ = resp2.Body.Close()
				assert.Equal(t, string(body), string(body2), "template should not change between requests")
			}
		}

		_ = server.Stop(1 * time.Second)
		time.Sleep(50 * time.Millisecond) // Let port be released
	}

	// With 10 servers and 3 templates, we should see at least 2 different templates
	// (unless we're extremely unlucky with randomness)
	assert.GreaterOrEqual(t, len(seenTemplates), 2, "should pick different templates across multiple server starts")
}

// TestServer_TemplateRotationWithCache tests that rotation works correctly with caching.
func TestServer_TemplateRotationWithCache(t *testing.T) {
	t.Parallel()

	var (
		log  = logger.NewNop()
		cfg  = config.New()
		port = getFreeTCPPort(t)
	)

	_ = cfg.Templates.Add("template1", "<html><body>Template 1: {{.Code}}</body></html>")
	_ = cfg.Templates.Add("template2", "<html><body>Template 2: {{.Code}}</body></html>")
	cfg.RotationMode = config.RotationModeRandomOnEachRequest

	var server = http.NewServer(log, 4096)
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

	// Make rapid requests and verify we still get responses (cache should work)
	for i := 0; i < 50; i++ {
		resp, err := nethttp.Get(fmt.Sprintf("http://127.0.0.1:%d/404", port))
		require.NoError(t, err)
		assert.Equal(t, nethttp.StatusOK, resp.StatusCode)
		_ = resp.Body.Close()
	}
}

// TestServer_MultipleTemplateSizes tests rotation with templates of different sizes.
func TestServer_MultipleTemplateSizes(t *testing.T) {
	t.Parallel()

	var (
		log  = logger.NewNop()
		cfg  = config.New()
		port = getFreeTCPPort(t)
	)

	// Create templates of varying sizes
	_ = cfg.Templates.Add("small", "<html><body>{{.Code}}</body></html>")
	_ = cfg.Templates.Add("medium", "<html><head><title>Error</title></head><body><h1>Error {{.Code}}</h1><p>Description: {{.Description}}</p></body></html>")
	_ = cfg.Templates.Add("large", "<html><head><title>Error</title></head><body><h1>Error {{.Code}}</h1>"+createLargeContent(1000)+"</body></html>")
	cfg.RotationMode = config.RotationModeRandomOnEachRequest

	var server = http.NewServer(log, 4096)
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

	// Verify all template sizes work
	for i := 0; i < 30; i++ {
		resp, err := nethttp.Get(fmt.Sprintf("http://127.0.0.1:%d/404", port))
		require.NoError(t, err)
		assert.Equal(t, nethttp.StatusOK, resp.StatusCode)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.NotEmpty(t, body)

		_ = resp.Body.Close()
	}
}

// TestServer_RotationWithSingleTemplate tests that rotation works even with a single template.
func TestServer_RotationWithSingleTemplate(t *testing.T) {
	t.Parallel()

	var (
		log  = logger.NewNop()
		cfg  = config.New()
		port = getFreeTCPPort(t)
	)

	_ = cfg.Templates.Add("only", "<html><body>Only Template: {{.Code}}</body></html>")
	cfg.TemplateName = "only"
	cfg.RotationMode = config.RotationModeRandomOnEachRequest

	var server = http.NewServer(log, 4096)
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

	// Even with one template, rotation shouldn't cause errors
	var firstBody string
	for i := 0; i < 10; i++ {
		resp, err := nethttp.Get(fmt.Sprintf("http://127.0.0.1:%d/404", port))
		require.NoError(t, err)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		_ = resp.Body.Close()

		if i == 0 {
			firstBody = string(body)
		} else {
			// With only one template, should always get the same one
			assert.Equal(t, firstBody, string(body))
		}
	}
}

// TestServer_RotationWithDifferentErrorCodes tests rotation across different HTTP error codes.
func TestServer_RotationWithDifferentErrorCodes(t *testing.T) {
	t.Parallel()

	var (
		log  = logger.NewNop()
		cfg  = config.New()
		port = getFreeTCPPort(t)
	)

	_ = cfg.Templates.Add("template1", "<html><body>T1-{{.Code}}</body></html>")
	_ = cfg.Templates.Add("template2", "<html><body>T2-{{.Code}}</body></html>")
	cfg.RotationMode = config.RotationModeRandomOnEachRequest

	var server = http.NewServer(log, 4096)
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

	// Test rotation works for different error codes
	errorCodes := []int{400, 401, 403, 404, 500, 502, 503}

	for _, code := range errorCodes {
		resp, err := nethttp.Get(fmt.Sprintf("http://127.0.0.1:%d/%d", port, code))
		require.NoError(t, err)
		assert.Equal(t, nethttp.StatusOK, resp.StatusCode)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		_ = resp.Body.Close()

		// Verify the code appears in the response
		assert.Contains(t, string(body), fmt.Sprintf("%d", code))
	}
}

// Helper function to check if string contains substring.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Helper function to create large HTML content for testing.
func createLargeContent(paragraphs int) string {
	var content string
	for i := 0; i < paragraphs; i++ {
		content += fmt.Sprintf("<p>Paragraph %d: This is some filler content for testing purposes. ", i)
		content += "Lorem ipsum dolor sit amet, consectetur adipiscing elit.</p>"
	}
	return content
}
