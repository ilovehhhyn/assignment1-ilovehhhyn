package main

import (
	"fmt"
	"io"
	"net"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

/******************************************************************************/
/*                            Client Type                                     */
/******************************************************************************/

type RefServer struct {
	listener net.Listener
	conn     net.Conn
}

func (srv *RefServer) Accept(connChan chan<- net.Conn, errChan chan<- error) {
	conn, err := srv.listener.Accept()
	if err != nil {
		errChan <- err
	}
	connChan <- conn
}

type Client struct {
	alive  bool
	cmd    *exec.Cmd
	ip     string
	port   string
	stdin  io.WriteCloser
	server *RefServer
}

// NewClient() creates a new client (but does not attempt to connect)
func NewClient(ip, port string) *Client {
	return &Client{alive: false, ip: ip, port: port, server: new(RefServer)}
}

// StartRefServer() starts a localhost server for the client to connect to,
// returning an error if unsuccessful.
func (client *Client) StartRefServer() error {
	port := ":" + client.port
	ln, err := net.Listen("tcp", port)
	if err != nil {
		e := fmt.Sprintf("Failed to start refserver: %s", err)
		debug.Println(e)
		return fmt.Errorf(e)
	}
	client.server.listener = ln

	return nil
}

func (client *Client) StopRefServer() error {
	errs := make([]string, 0)

	if client.server.listener != nil {
		err := client.server.listener.Close()
		if err != nil {
			e := fmt.Sprintf("Failed to close refserver listener: %s", err)
			errs = append(errs, e)
		}
	}

	if client.server.conn != nil {
		err := client.server.conn.Close()
		if err != nil {
			e := fmt.Sprintf("Failed to close refserver conn: %s", err)
			errs = append(errs, e)
		}
	}

	if len(errs) != 0 {
		err := strings.Join(errs, "\n")
		debug.Println(err)
		return fmt.Errorf(err)
	}

	return nil
}

// Connect() connects the client to its intended server, returning the connection.
// If this attempt fails, the provided test fails and an error is returned.
// Note that this must be called after StartRefServer() and before Stop()
func (client *Client) Connect(t *testing.T) (net.Conn, error) {
	// Create a command to run the student server executable
	client_exe := filepath.Join(solutionDir, "client")
	debug.Println("Executable:", client_exe)
	cmd := exec.Command(client_exe, client.ip, client.port)
	client.cmd = cmd

	// Get stdin
	stdin, err := cmd.StdinPipe()
	if err != nil {
		e := fmt.Sprintf("Failed to get client stdin: %s", err)
		t.Errorf(e)
		return nil, fmt.Errorf(e)
	}
	client.stdin = stdin

	// Redirect client Stdout to client stderr, Print messages asynchronously on stderr.
	// WARNING: This might interleave messages from different clients if several are up
	cmd.Stdout = cmd.Stderr
	stderr, err := cmd.StderrPipe()
	if err != nil {
		e := fmt.Sprintf("Failed to get server stderr: %s", err)
		t.Errorf(e)
		return nil, fmt.Errorf(e)
	}
	go printErrors(stderr)

	// (Prepare to) Accept a connection serverside.
	errChan := make(chan error)
	connChan := make(chan net.Conn)
	go client.server.Accept(connChan, errChan)

	// Wait to make sure the server is ready for connections
	time.Sleep(StartupDelay)

	// Execute the student client executable (Attempt to connect)
	debug.Printf("Connecting client to %s:%s\n", client.ip, client.port)
	err = cmd.Start()
	if err != nil {
		e := fmt.Sprintf("Failed to start server: %s", err)
		debug.Println(e)
		t.Errorf(e)
		return nil, fmt.Errorf(e)
	}

	// Wait for the server to accept the connection
	select {
	case err := <-errChan:
		// Handle errors from Accept()
		e := fmt.Sprintf("Failed to accept client connection: %s", err)
		debug.Printf(e)
		t.Errorf(e)
		return nil, fmt.Errorf(e)
	case <-time.After(AcceptTimeout):
		// Took too long to establish connection - timeout
		e := fmt.Sprintf("Timed out waiting for client connection.")
		debug.Printf(e)
		t.Errorf(e)
		return nil, fmt.Errorf(e)
	case conn := <-connChan:
		// Success!
		client.alive = true
		client.server.conn = conn
		return conn, nil
	}
}

// TestMessage() sends a message from the client to its connected server.
// If the message is not received intact, the provided test fails.
// TODO: Refactor this and Server.TestMessage to share code
func (client *Client) TestMessage(t *testing.T, msg string) {
	testMessage(t, msg, client.stdin, client.server.conn)
}

// Stop() stops a connected client, severing its connection. Once Stop() has
// been called on a client, it cannot re- Connect() to its server.
// Do not call Stop() on clients that are not connected. If Stop() fails for
// any reason, the provided test fails, and an error is returned.
// This should be called after StopRefServer()
func (client *Client) Stop(t *testing.T) error {
	debug.Println("Stopping client... ")

	// Stop ref server
	// client.StopRefServer() // Note - call this separately

	if !client.alive {
		e := "Attempted to kill client that is not running."
		debug.Println(e)
		t.Errorf(e)
		return fmt.Errorf(e)
	}

	err := client.cmd.Process.Kill()
	if err != nil {
		e := fmt.Sprintf("Failed to kill client: %s", err.Error())
		debug.Println(e)
		t.Errorf(e)
		return fmt.Errorf(e)
	}

	client.cmd.Wait()

	debug.Println("Client stopped.")
	client.alive = false
	return nil
}

/******************************************************************************/
/*                            Test Helpers                                    */
/******************************************************************************/

// Try to connect client to reference server. Call f on its input and output
func testClient(t *testing.T, ip, port string, f func(w io.Writer, r io.Reader)) {
	client := NewClient(ip, port)

	// Start reference server
	err := client.StartRefServer()
	if err != nil {
		debug.Println(err)
		t.SkipNow() // Don't fail student tests if reference code is buggy
	}
	defer client.StopRefServer()

	// Connect to reference server
	conn, err := client.Connect(t)
	if err != nil {
		debug.Println(err)
		return
	}
	defer client.Stop(t)

	if f != nil {
		f(client.stdin, conn)
	}
}

// Test the client with the given message, ensuring the message makes it intact
func testClientMessage(t *testing.T, msg string) {
	// If we already know client to be unconnectable, fail tests without running
	if skipClientMessageTests {
		t.Logf("Cannot establish connection to client. Aborting test...")
		t.FailNow()
		return
	}

	client := NewClient("127.0.0.1", DefaultPort)

	// Start reference server
	debug.Println("Starting refserver for client...")
	err := client.StartRefServer()
	if err != nil {
		debug.Println(err)
		t.SkipNow() // Don't fail student tests if reference code is buggy
	}
	defer client.StopRefServer()

	// Connect to reference server
	debug.Println("Connecting client to refserver...")
	_, err = client.Connect(t) // conn not needed explicitly b/c client stored a ref to it
	if err != nil {
		debug.Println(err)
		return
	}
	defer client.Stop(t)

	debug.Printf("Testing message (%d bytes)...", len(msg))
	client.TestMessage(t, msg)
}

/******************************************************************************/
/*                            Client Tests                                    */
/******************************************************************************/

var skipClientMessageTests = false

// ------------------------- Connection Tests -------------------------------
func TestClientBasicConnect(t *testing.T) {
	// desc := "Check that student client can establish connections at all"
	// note := "Student Client ⇌ Reference Server"
	testClient(t, "127.0.0.1", DefaultPort, nil)

	// If this test fails, all the others certainly will too. Don't bother with them
	if t.Failed() {
		skipClientMessageTests = true
	}
}

// -------------------- Printable ASCII Tests ----------------------------
func TestClientShortNewline(t *testing.T) {
	// desc := "Client: Send a short printable ASCII message terminated by a newline"
	// note := "Student Client ⇌ Reference Server"
	msg := ShortMessage + "\n"
	testClientMessage(t, msg)
}

func TestClientShortNoNewline(t *testing.T) {
	// desc := "Client: Send a short printable ASCII message not terminated by a newline"
	// note := "Student Client ⇌ Reference Server"
	msg := ShortMessage
	testClientMessage(t, msg)
}

func TestClientShortPrintf(t *testing.T) {
	// desc := "Client: Send a short message containing fmt.Printf command characters"
	// note := "Student Client ⇌ Reference Server"
	msg := PrintfMessage
	testClientMessage(t, msg)
}

func TestClientMultiline(t *testing.T) {
	// desc := "Client: Send a multi-line printable ASCII message"
	// note := "Student Client ⇌ Reference Server"
	msg := MultilineMessage
	testClientMessage(t, msg)
}

// TODO: update assignment cfg
// NOTE: This could be rewritten by passing testClient() a function
func TestClientManyShort(t *testing.T) {
	client := NewClient("127.0.0.1", DefaultPort)

	debug.Println("Starting refserver for client...")
	err := client.StartRefServer()
	if err != nil {
		debug.Println(err)
		t.SkipNow() // Don't fail student tests if reference code is buggy
	}
	defer client.StopRefServer()

	// Connect to reference server
	debug.Println("Connecting client to refserver...")
	_, err = client.Connect(t) // conn not needed explicitly b/c client stored a ref to it
	if err != nil {
		debug.Println(err)
		return
	}
	defer client.Stop(t)

	for i := 0; i < NumShort; i++ {
		msg := fmt.Sprintf("Hello World %d\n\n", i)
		client.TestMessage(t, msg)
	}
}

func TestClientMobyDick(t *testing.T) {
	// desc := "Client: Send the entire text of Moby Dick"
	// note := "Student Client ⇌ Reference Server"
	msg := MobyDick
	if len(msg) == 0 {
		debug.Printf("Unable to locate mobydick.txt")
		t.Skip("Unable to locate mobydick.txt")
		return
	}
	testClientMessage(t, msg)
}

func TestClientShortRandomPrintable(t *testing.T) {
	// desc := "Client: Send random short printable ASCII messages"
	// note := "Student Client ⇌ Reference Server"
	for i := 1; i <= NumShortRandomPrintable; i++ {
		msg := randString(1, 63, Printable)
		name := fmt.Sprintf("Message&%d", i)
		t.Run(name, func(t *testing.T) { testClientMessage(t, msg) })
	}
}

func TestClientLongRandomPrintable(t *testing.T) {
	// desc := "Client: Send random long printable ASCII messages"
	// note := "Student Client ⇌ Reference Server"
	for i := 1; i <= NumLongRandomPrintable; i++ {
		msg := randString(64, 512, Printable)
		name := fmt.Sprintf("Message&%d", i)
		t.Run(name, func(t *testing.T) { testClientMessage(t, msg) })
	}
}

// ------------------------ Binary Tests ------------------------------------
func TestClientBinary(t *testing.T) {
	// desc := "Client: Send a short binary message"
	// note := "Student Client ⇌ Reference Server"
	msg := string([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20})
	testClientMessage(t, msg)
}

func TestClientShortRandomBinary(t *testing.T) {
	// desc := "Client: Send random short binary messages"
	// note := "Student Client ⇌ Reference Server"
	for i := 1; i <= NumShortRandomBinary; i++ {
		msg := randString(1, 63, Binary)
		name := fmt.Sprintf("Message&%d", i)
		t.Run(name, func(t *testing.T) { testClientMessage(t, msg) })
	}
}

func TestClientLongRandomBinary(t *testing.T) {
	// desc := "Client: Send random long binary messages"
	// note := "Student Client ⇌ Reference Server"
	for i := 1; i <= NumLongRandomBinary; i++ {
		msg := randString(64, 512, Binary)
		name := fmt.Sprintf("Message&%d", i)
		t.Run(name, func(t *testing.T) { testClientMessage(t, msg) })
	}
}

/******************************************************************************/
/*                                                                            */
/******************************************************************************/