package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/joho/godotenv"
)

func TestTunnelWithTestEnv(t *testing.T) {
	// Get the absolute path to the test.env file
	testEnvPath := filepath.Join(".", "test.env")
	_, err := filepath.Abs(testEnvPath)
	if err != nil {
		t.Fatalf("Failed to get absolute path to test.env: %v", err)
	}

	// Load the test.env file
	err = godotenv.Load(testEnvPath)
	if err != nil {
		t.Fatalf("Error loading test.env file: %v", err)
	}

	// Load the configuration
	config, err := loadConfig(testEnvPath)
	if err != nil {
		t.Fatalf("Failed to load config from test.env: %v", err)
	}

	// Find an open port if not specified
	localPort := config.LocalPort
	if localPort == 0 {
		localPort, err = findOpenPort()
		if err != nil {
			t.Fatalf("Failed to find an open port: %v", err)
		}
		t.Logf("Using dynamically assigned local port: %d", localPort)
	} else {
		t.Logf("Using specified local port: %d", localPort)
	}

	// Create a context with timeout for the test
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Establish the tunnel
	sshClient, listener, errChan, err := establishTunnel(ctx, config, localPort)
	if err != nil {
		t.Fatalf("Failed to establish tunnel: %v", err)
	}
	defer sshClient.Close()
	defer listener.Close()

	// Start forwarding connections
	go forwardConnections(ctx, sshClient, listener, config.RemoteHost, config.RemotePort, errChan)

	// Give the tunnel a moment to initialize
	time.Sleep(2 * time.Second)

	// Test the connection
	t.Run("TestConnection", func(t *testing.T) {
		// Try to connect to the local port
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", localPort), 5*time.Second)
		if err != nil {
			t.Fatalf("Failed to connect to local port %d: %v", localPort, err)
		}
		defer conn.Close()
		t.Logf("Successfully connected to local port %d", localPort)
	})

	// If the remote service is HTTP-based, we can also test HTTP connectivity
	if config.RemotePort == 80 || config.RemotePort == 443 || config.RemotePort == 8080 {
		t.Run("TestHTTPConnection", func(t *testing.T) {
			url := fmt.Sprintf("http://localhost:%d", localPort)
			resp, err := http.Get(url)
			if err != nil {
				t.Logf("HTTP connection test skipped or failed: %v", err)
				return
			}
			defer resp.Body.Close()
			t.Logf("HTTP connection successful, status code: %d", resp.StatusCode)
		})
	}

	// Check for any errors from the tunnel
	select {
	case err := <-errChan:
		t.Fatalf("Tunnel error: %v", err)
	case <-ctx.Done():
		// Test timeout reached, which is expected
		t.Log("Test completed successfully")
	default:
		// No errors, which is good
	}
}

// TestConfigLoading tests that the config can be loaded from test.env
func TestConfigLoading(t *testing.T) {
	testEnvPath := filepath.Join(".", "test.env")
	config, err := loadConfig(testEnvPath)
	if err != nil {
		t.Fatalf("Failed to load config from test.env: %v", err)
	}

	// Verify required fields are present
	if config.Host == "" {
		t.Error("SSH_HOST is empty")
	}
	if config.Username == "" {
		t.Error("SSH_USERNAME is empty")
	}
	if config.RemotePort == 0 {
		t.Error("SSH_REMOTEPORT is empty or invalid")
	}
	if config.KeyFile == "" && config.Password == "" {
		t.Error("Both SSH_KEYFILE and SSH_PASSWORD are empty")
	}

	t.Logf("Config loaded successfully: %+v", config)
}

// TestFindOpenPort tests that we can find an open port
func TestFindOpenPort(t *testing.T) {
	port, err := findOpenPort()
	if err != nil {
		t.Fatalf("Failed to find an open port: %v", err)
	}
	// t.Logf("Found open port: %d", port)
	if port <= 0 || port > 65535 {
		t.Fatalf("Invalid port number: %d", port)
	}
	t.Logf("Found open port: %d", port)
}
