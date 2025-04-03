package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"golang.org/x/crypto/ssh"
)

// Config holds the SSH connection parameters
type Config struct {
	Host        string
	Port        int
	Username    string
	KeyFile     string
	Passphrase  string
	Password    string
	LocalPort   int // 0 means find an open port
	RemoteHost  string // Host to forward to on the remote side (usually same as Host)
	RemotePort  int    // Port to forward to on the remote side (usually same as Port)
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// Check DEBUG environment variable
	if os.Getenv("DEBUG") != "1" {
		log.SetOutput(io.Discard) // Discard log output if DEBUG is not "1"
		// fmt.Fprintln(os.Stderr, "Debug logging disabled. Set DEBUG=1 to enable.") // Inform user
	} else {
		log.Println("Debug logging enabled.")
	}

	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: %s <env-file> <command> [args...]\n", os.Args[0])
		os.Exit(1)
	}

	envFilePath := os.Args[1]
	command := os.Args[2]
	commandArgs := os.Args[3:]

	// --- Load Config ---
	config, err := loadConfig(envFilePath)
	if err != nil {
		log.Fatalf("Error loading config from %s: %v", envFilePath, err)
	}

	// --- Find Local Port if necessary ---
	localPort := config.LocalPort
	if localPort == 0 {
		p, err := findOpenPort()
		if err != nil {
			log.Fatalf("Failed to find an open local port: %v", err)
		}
		localPort = p
		log.Printf("Using dynamically assigned local port: %d", localPort)
	} else {
		log.Printf("Using specified local port: %d", localPort)
	}

	// --- Main Loop: Manage Tunnel and Command ---
	runTunnelAndCommand(config, localPort, command, commandArgs)
}

// loadConfig reads the .env file and parses SSH configuration
func loadConfig(envFilePath string) (*Config, error) {
	// If the path is relative, godotenv.Load should handle it relative to the CWD
	// where the command was invoked. Pass the path directly.
	err := godotenv.Load(envFilePath)
	if err != nil {
		// Allow missing file if default values are sufficient or set via env directly
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("error loading env file '%s': %w", envFilePath, err)
		}
		// Construct the absolute path it tried for a clearer warning message
		absPath, absErr := filepath.Abs(envFilePath)
		if absErr != nil {
			absPath = envFilePath // fallback to original path if Abs fails
		}
		log.Printf("Warning: Env file '%s' not found (looked for: %s), relying on environment variables or defaults.", envFilePath, absPath)
	}

	config := &Config{
		Port: 22, // Default SSH port
	}

	config.Host = os.Getenv("SSH_HOST")
	if portStr := os.Getenv("SSH_PORT"); portStr != "" {
		var err error // Declare err here
		config.Port, err = strconv.Atoi(portStr)
		if err != nil {
			return nil, fmt.Errorf("invalid SSH_PORT: %w", err)
		}
	}
	config.Username = os.Getenv("SSH_USERNAME")
	config.KeyFile = os.Getenv("SSH_KEYFILE")
	config.Passphrase = os.Getenv("SSH_PASSPHRASE")
	config.Password = os.Getenv("SSH_PASSWORD")
	if localPortStr := os.Getenv("SSH_LOCALPORT"); localPortStr != "" {
		var err error // Declare err here
		config.LocalPort, err = strconv.Atoi(localPortStr)
		if err != nil {
			return nil, fmt.Errorf("invalid SSH_LOCALPORT: %w", err)
		}
	}

	// --- Validation ---
	if config.Host == "" {
		return nil, errors.New("SSH_HOST is required")
	}
	if config.Username == "" {
		return nil, errors.New("SSH_USERNAME is required")
	}
	if config.KeyFile == "" && config.Password == "" {
		return nil, errors.New("either SSH_KEYFILE or SSH_PASSWORD must be provided")
	}

	// Default remote forward target to the SSH server itself
	config.RemoteHost = "localhost"

	// Override remote port if SSH_REMOTEPORT is set
	if remotePortStr := os.Getenv("SSH_REMOTEPORT"); remotePortStr != "" {
		var err error // Declare err here
		config.RemotePort, err = strconv.Atoi(remotePortStr)
		if err != nil {
			return nil, fmt.Errorf("invalid SSH_REMOTEPORT: %w", err)
		}
		log.Printf("Using SSH_REMOTEPORT: %d for remote forwarding", config.RemotePort)
	} else {
		return nil, errors.New("SSH_REMOTEPORT is required")
	}

	// Override remote host if SSH_REMOTEHOST is set
	if remoteHost := os.Getenv("SSH_REMOTEHOST"); remoteHost != "" {
		config.RemoteHost = remoteHost
		log.Printf("Using SSH_REMOTEHOST: %s for remote forwarding", config.RemoteHost)
	}

	return config, nil
}

// findOpenPort finds an available TCP port on localhost
func findOpenPort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}
	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// runTunnelAndCommand manages the lifecycle of the SSH tunnel and the child command
func runTunnelAndCommand(config *Config, localPort int, command string, commandArgs []string) {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			log.Println("Termination signal received, exiting.")
			return
		default:
			// Proceed
		}

		log.Printf("Attempting to establish SSH tunnel to %s:%d...", config.Host, config.Port)
		tunnelCtx, tunnelCancel := context.WithCancel(ctx)
		sshClient, localListener, tunnelErrChan, err := establishTunnel(tunnelCtx, config, localPort)

		if err != nil {
			log.Printf("Failed to establish tunnel: %v. Retrying in 5 seconds...", err)
			tunnelCancel() // Ensure resources are cleaned up if establishTunnel partially succeeded
			select {
			case <-time.After(5 * time.Second):
				continue
			case <-ctx.Done():
				return
			}
		}

		log.Println("SSH tunnel established successfully.")

		// --- Run Command ---
		cmdCtx, cmdCancel := context.WithCancel(ctx)
		cmd := exec.CommandContext(cmdCtx, command, commandArgs...)
		cmd.Env = append(os.Environ(), fmt.Sprintf("SSH_LOCALPORT=%d", localPort))
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin // Allow interaction if needed

		cmdErrChan := make(chan error, 1)
		go func() {
			log.Printf("Starting command: %s %s", command, strings.Join(commandArgs, " "))
			cmdErrChan <- cmd.Run()
		}()

		// --- Wait for Command Exit or Tunnel Failure ---
		select {
		case err := <-cmdErrChan:
			tunnelCancel() // Stop tunnel components
			if err != nil {
				if exitErr, ok := err.(*exec.ExitError); ok {
					log.Printf("Command exited with status %d.", exitErr.ExitCode())
					// Decide if we should exit based on command exit code?
					// For now, assume any exit means we stop.
					return
				}
				log.Printf("Command failed: %v", err)
				return // Exit if command fails unexpectedly
			}
			log.Println("Command completed successfully. Exiting.")
			return // Command finished normally

		case err := <-tunnelErrChan:
			log.Printf("SSH tunnel failed: %v. Restarting command...", err)
			cmdCancel()      // Signal the command to stop
			<-cmdErrChan     // Wait for the command goroutine to finish
			localListener.Close() // Ensure listener is closed
			sshClient.Close()     // Ensure SSH client is closed
			// Loop will continue and try to re-establish tunnel

		case <-ctx.Done():
			log.Println("Termination signal received during run.")
			tunnelCancel()
			cmdCancel()
			<-cmdErrChan // Wait for command goroutine
			return
		}

		log.Println("Waiting 2 seconds before attempting reconnect...")
		select {
		case <-time.After(2 * time.Second):
			// continue loop
		case <-ctx.Done():
			return
		}
	}
}

// establishTunnel connects to SSH and sets up the local port forward listener
func establishTunnel(ctx context.Context, config *Config, localPort int) (*ssh.Client, net.Listener, chan error, error) {
	authMethods, err := getAuthMethods(config)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to prepare auth methods: %w", err)
	}

	sshConfig := &ssh.ClientConfig{
		User:            config.Username,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // Warning: Use a proper host key callback in production!
		Timeout:         10 * time.Second,
	}

	serverAddr := fmt.Sprintf("%s:%d", config.Host, config.Port)
	log.Printf("Dialing SSH server %s...", serverAddr)
	sshClient, err := ssh.Dial("tcp", serverAddr, sshConfig)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to dial SSH server: %w", err)
	}
	log.Println("SSH connection successful.")

	localAddr := fmt.Sprintf("localhost:%d", localPort)
	log.Printf("Starting local listener on %s...", localAddr)
	localListener, err := net.Listen("tcp", localAddr)
	if err != nil {
		sshClient.Close()
		return nil, nil, nil, fmt.Errorf("failed to listen on local port %d: %w", localPort, err)
	}
	log.Printf("Local listener started on %s.", localAddr)

	tunnelErrChan := make(chan error, 1) // Buffered channel

	// Start forwarding connections in a separate goroutine
	go forwardConnections(ctx, sshClient, localListener, config.RemoteHost, config.RemotePort, tunnelErrChan)

	// Monitor context cancellation to close resources
	go func() {
		<-ctx.Done()
		log.Println("Tunnel context cancelled, closing listener and SSH client.")
		localListener.Close()
		sshClient.Close()
	}()


	return sshClient, localListener, tunnelErrChan, nil
}

// getAuthMethods prepares SSH authentication methods based on config
func getAuthMethods(config *Config) ([]ssh.AuthMethod, error) {
	var authMethods []ssh.AuthMethod

	// Password Auth
	if config.Password != "" {
		authMethods = append(authMethods, ssh.Password(config.Password))
	}

	// Public Key Auth
	if config.KeyFile != "" {
		keyPath := config.KeyFile
		// Handle ~ for home directory
		if strings.HasPrefix(keyPath, "~/") {
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, fmt.Errorf("failed to get user home directory: %w", err)
			}
			keyPath = filepath.Join(home, keyPath[2:])
		}

		key, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read private key file %s: %w", keyPath, err)
		}

		var signer ssh.Signer
		if config.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(key, []byte(config.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(key)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to parse private key: %w", err)
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}

	if len(authMethods) == 0 {
		return nil, errors.New("no valid SSH authentication method configured")
	}

	return authMethods, nil
}

// forwardConnections accepts local connections and forwards them over SSH
func forwardConnections(ctx context.Context, sshClient *ssh.Client, localListener net.Listener, remoteHost string, remotePort int, tunnelErrChan chan<- error) {
	defer log.Printf("Local connection forwarding stopped for %s:%d", remoteHost, remotePort)

	remoteAddr := fmt.Sprintf("%s:%d", remoteHost, remotePort)

	for {
		// Accept next connection
		log.Printf("Forwarding connection to %s...", remoteAddr)
		localConn, err := localListener.Accept()
		if err != nil {
			// Check if the error is due to listener being closed
			select {
			case <-ctx.Done(): // Context cancelled, expected closure
				return
			default:
				if errors.Is(err, net.ErrClosed) {
					log.Println("Local listener closed normally.")
					return
				}
				log.Printf("Error accepting local connection: %v", err)
				// Send error only if not already closed/cancelled
				select {
				case tunnelErrChan <- fmt.Errorf("accept error: %w", err):
				default: // Avoid blocking if channel is full or closed
				}
				return // Stop forwarding on accept error
			}
		}

		log.Printf("Accepted local connection from %s", localConn.RemoteAddr())

		// Handle the connection in a new goroutine
		go func(local net.Conn) {
			defer local.Close()
			defer log.Printf("Closed local connection from %s", local.RemoteAddr())

			// Establish connection to the target via SSH tunnel
			log.Printf("Dialing remote target %s via SSH tunnel...", remoteAddr)
			remoteConn, err := sshClient.Dial("tcp", remoteAddr)
			if err != nil {
				log.Printf("Failed to dial remote target %s via SSH: %v", remoteAddr, err)
				// Check if SSH client is closed
				select {
				case <-ctx.Done(): // Context cancelled, expected
				default:
					// Send error only if not already closed/cancelled
					select {
					case tunnelErrChan <- fmt.Errorf("ssh dial error: %w", err):
					default:
					}
				}
				return
			}
			defer remoteConn.Close()
			log.Printf("Successfully dialed remote target %s via SSH.", remoteAddr)

			// Copy data between local and remote connections
			var wg sync.WaitGroup
			wg.Add(2)

			log.Printf("Starting data copy between %s <-> SSH Tunnel <-> %s", local.RemoteAddr(), remoteAddr)

			go func() {
				defer wg.Done()
				defer remoteConn.Close() // Close remote if local closes write
				_, err := io.Copy(remoteConn, local)
				if err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, io.EOF) {
					log.Printf("Error copying local to remote: %v", err)
				}
				log.Printf("Finished copying local -> remote for %s", local.RemoteAddr())
			}()

			go func() {
				defer wg.Done()
				defer local.Close() // Close local if remote closes write
				_, err := io.Copy(local, remoteConn)
				if err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, io.EOF) {
					log.Printf("Error copying remote to local: %v", err)
				}
				log.Printf("Finished copying remote -> local for %s", local.RemoteAddr())
			}()

			wg.Wait()
			log.Printf("Data copy finished for connection from %s", local.RemoteAddr())

		}(localConn)
	}
}
