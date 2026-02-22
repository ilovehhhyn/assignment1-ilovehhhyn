 package main

 import (
	 "fmt"
	 "io"
	 "net"
	 "os/exec"
	 "path/filepath"
	 "sort"
	 "strings"
	 "sync"
	 "testing"
	 "time"
 )
 
 /******************************************************************************/
 /*                          Server Type                                       */
 /******************************************************************************/
 
 type Server struct {
	 alive  bool
	 cmd    *exec.Cmd
	 port   string
	 stdout io.ReadCloser
 }
 
 // NewServer() creates a new server (but does not attempt to start it)
 func NewServer(port string) *Server {
	 return &Server{alive: false, port: port}
 }
 
 // Start() starts the server so it is open for accepting connections.
 // If an error occurs while starting the server, fail the provided test,
 // and return the error.
 func (srv *Server) Start(t *testing.T) error {
	 // Create a command to run the student server executable
	 server := filepath.Join(solutionDir, "server")
	 debug.Println("Executable:", server)
	 cmd := exec.Command(server, srv.port)
	 srv.cmd = cmd
 
	 // Get stdout
	 stdout, err := cmd.StdoutPipe()
	 if err != nil {
		 e := fmt.Sprintf("Failed to get server stdout: %s", err)
		 t.Errorf(e)
		 return fmt.Errorf(e)
	 }
	 srv.stdout = stdout
 
	 // Print messages asynchronously on stderr.
	 // WARNING: This might interleave messages from different servers if several are up
	 stderr, err := cmd.StderrPipe()
	 if err != nil {
		 e := fmt.Sprintf("Failed to get server stderr: %s", err)
		 t.Errorf(e)
		 return fmt.Errorf(e)
	 }
	 go printErrors(stderr)
 
	 // Execute the student server executable
	 debug.Println("Starting server on port", srv.port)
	 err = cmd.Start()
	 if err != nil {
		 e := fmt.Sprintf("Failed to start server: %s", err)
		 debug.Println(e)
		 t.Errorf(e)
		 return fmt.Errorf(e)
	 }
 
	 // Wait for a moment to ensure the server process has had time to start up,
	 // bind to its port, and be ready to accept connections.
	 time.Sleep(StartupDelay)
 
	 srv.alive = true
	 return nil
 }
 
 // Connect() attempts to connect to the server. If successful, it returns
 // the net.Conn representing the active connection. If unsuccessful,
 // fails the provided test and returns an error
 func (srv *Server) Connect(t *testing.T) (net.Conn, error) {
	 addr := fmt.Sprintf("127.0.0.1:%s", srv.port)
	 debug.Printf("Dialing server at %s...\n", addr)
	 connection, err := net.Dial("tcp", addr)
	 if err != nil {
		 e := fmt.Sprintf("Failed to connect to server: %s", err)
		 t.Errorf(e)
		 return nil, fmt.Errorf(e)
	 }
 
	 return connection, nil
 }
 
 // TestMessage() attempts to send the specified message `msg` over the specified
 // connection `conn` to this server `srv`. If the message fails to arrive intact,
 // fail the provided test with an informative message.
 // WARNING: Only call this with a `conn` connected to the receiving server.
 // Bad things will (probably) happen otherwise.
 // TODO: Add a timeout to catch infinite loops
 func (srv *Server) TestMessage(t *testing.T, msg string, conn net.Conn) {
	 testMessage(t, msg, conn, srv.stdout)
 }
 
 // Stop() stops the server so it is no longer open for accepting connections,
 // and its port becomes freed. It is not possible to re-start a server
 // after it has been stopped - just make a new one.
 // If an error occurs while stopping the server fail the provided test and
 // return the error. (TODO: Is it a good idea to fail tests for this?)
 func (srv *Server) Stop(t *testing.T) error {
	 debug.Println("Stopping server... ")
 
	 if !srv.alive {
		 e := "Attempted to kill server that is not running."
		 debug.Println(e)
		 t.Errorf(e)
		 return fmt.Errorf(e)
	 }
 
	 err := srv.cmd.Process.Kill()
	 if err != nil {
		 e := fmt.Sprintf("Failed to kill server: %s", err.Error())
		 debug.Println(e)
		 t.Errorf(e)
		 return fmt.Errorf(e)
	 }
 
	 srv.cmd.Wait()
 
	 debug.Println("Server stopped.")
	 srv.alive = false
	 return nil
 }
 
 /******************************************************************************/
 /*                          Test Helpers                                      */
 /******************************************************************************/
 
 // Try to run server.go, capturing its output. Then connect to it, and
 // call f on its connection
 func testServer(t *testing.T, port string, f func(conn net.Conn, reader io.Reader)) {
	 server := NewServer(port)
	 err := server.Start(t)
	 if err != nil {
		 debug.Println(err)
		 return
	 }
	 defer server.Stop(t)
 
	 connection, err := server.Connect(t)
	 if err != nil {
		 debug.Println(err)
		 return
	 }
 
	 if f != nil {
		 f(connection, server.stdout)
	 }
 }
 
 func testServerMessage(t *testing.T, msg string) {
	 // If we already know client to be unconnectable, fail tests without running
	 if skipServerMessageTests {
		 t.Logf("Cannot establish connection to server. Aborting test...")
		 t.FailNow()
		 return
	 }
 
	 srv := NewServer(DefaultPort)
	 err := srv.Start(t)
	 if err != nil {
		 debug.Println(err)
		 return
	 }
	 defer srv.Stop(t)
 
	 conn, err := srv.Connect(t)
	 if err != nil {
		 debug.Println(err)
		 return
	 }
 
	 srv.TestMessage(t, msg, conn)
 }
 
 /******************************************************************************/
 /*                            Server Tests                                    */
 /******************************************************************************/
 
 var skipServerMessageTests = false
 
 // ------------------------- Connection Tests -------------------------------
 func TestServerBasicConnect(t *testing.T) {
	 // desc := "Check that student server can accept connections at all"
	 // note := "Reference Client ⇌ Student Server"
	 testServer(t, DefaultPort, nil)
 
	 // If this test fails, all the others certainly will too. Don't bother with them
	 if t.Failed() {
		 skipServerMessageTests = true
	 }
 }
 
 func TestServerPortConnect(t *testing.T) {
	 // desc := "Server: Listen for connections on multiple different ports"
	 // note := "Reference Client ⇌ Student Server"
	 for i := 10; i <= 20; i++ {
		 port := fmt.Sprintf("%d316", i)
		 name := fmt.Sprintf("Port&%s", port)
		 t.Run(name, func(t *testing.T) { testServer(t, port, nil) })
	 }
 }
 
 func TestServerSequentialConnect(t *testing.T) {
	 // desc := "Server: Accept multiple sequential connections"
	 // note := "Reference Client ⇌ Student Server"
 
	 srv := NewServer(DefaultPort)
	 srv.Start(t)
	 defer srv.Stop(t)
 
	 var expected strings.Builder
 
	 for i := 1; i < 10; i++ {
		 conn, err := srv.Connect(t)
		 if err != nil {
			 continue
		 }
 
		 // Write message to conn
		 msg := fmt.Sprintf("Testing connection %d\n", i)
		 expected.WriteString(msg)
		 writeMessage(t, msg, conn, WriteTimeout)
 
		 err = conn.Close()
		 if err != nil {
			 e := fmt.Sprintf("Failed to close conn: %s", err)
			 t.Errorf(e)
		 }
	 }
 
	 // Read results from all 10 clients
	 rawResponse := readMessage(t, srv.stdout, ReadTimeout)
	 // Responses could arrive in any order; so we need to sort them.
	 responses := strings.SplitAfter(rawResponse, "\n")
	 sort.Strings(responses)
	 response := strings.Join(responses, "")
 
	 // Compare results
	 compareMessages(t, expected.String(), response)
 }
 
 func TestServerConcurrentConnect(t *testing.T) {
	 // desc := "Server: Accept multiple concurrent connections"
	 // note := "Reference Client ⇌ Student Server"
	 // t.SkipNow() // TODO: This test is buggy - seems to crash and/or hang sporadically
	 // Check how ref handles concurrency and revise these tests to do it that way
	 srv := NewServer(DefaultPort)
	 srv.Start(t)
	 defer srv.Stop(t)
 
	 var wg sync.WaitGroup
	 var expected strings.Builder
 
	 // Pre-define the messages to avoid dealing with concurrency issues
	 N := 10
	 messages := make([]string, N)
	 for i := range messages {
		 s := fmt.Sprintf("Testing connection %d\n", i)
		 messages[i] = s
		 expected.WriteString(s)
	 }
 
	 for i := 1; i <= N; i++ {
		 wg.Add(1)
		 iter := i // check if necessary - (because i might have incremented)
		 go func() {
			 defer wg.Done()
			 conn, err := srv.Connect(t)
			 if err != nil {
				 e := fmt.Sprintf("Failed to connect: %s", err)
				 t.Errorf(e)
				 return
			 }
 
			 msg := messages[iter-1]
			 writeMessage(t, msg, conn, WriteTimeout)
 
			 err = conn.Close()
			 if err != nil {
				 e := fmt.Sprintf("Failed to close conn: %s", err)
				 t.Errorf(e)
			 }
		 }()
	 }
 
	 debug.Printf("Waiting for all closes.")
	 // Wait for all goroutines to return their results before proceeding.
	 wg.Wait()
 
	 // Responses could arrive in any order; so we need to sort them.
	 rawResponse := readMessage(t, srv.stdout, ReadTimeout)
	 responses := strings.SplitAfter(rawResponse, "\n")
	 sort.Strings(responses)
	 response := strings.Join(responses, "")
 
	 // compare results
	 compareMessages(t, expected.String(), response)
 }
 
 // --------------------- Printable ASCII Tests -------------------------------
 func TestServerShortNewline(t *testing.T) {
	 // desc := "Server: Receive a short printable ASCII message terminated by a newline"
	 // note := "Reference Client ⇌ Student Server"
	 msg := ShortMessage + "\n"
	 testServerMessage(t, msg)
 }
 
 func TestServerShortNoNewline(t *testing.T) {
	 // desc := "Server: Receive a short printable ASCII message not terminated by a newline"
	 // note := "Reference Client ⇌ Student Server"
	 msg := ShortMessage
	 testServerMessage(t, msg)
 }
 
 func TestServerShortPrintf(t *testing.T) {
	 // desc := "Server: Receive a short message containing fmt.Printf command characters"
	 // note := "Reference Client ⇌ Student Server"
	 msg := PrintfMessage
	 testServerMessage(t, msg)
 }
 
 func TestServerMultiline(t *testing.T) {
	 // desc := "Server: Receive a multi-line printable ASCII message"
	 // note := "Reference Client ⇌ Student Server"
	 msg := MultilineMessage
	 testServerMessage(t, msg)
 }
 
 // TODO: Update assignment cfg
 // NOTE: This test could be rewritten using testServer() and passing a func
 func TestServerManyShort(t *testing.T) {
	 // desc := "Server: Receive several short one-line messages sent in sequence"
	 // note := "Reference Client ⇌ Student Server"
	 srv := NewServer(DefaultPort)
	 err := srv.Start(t)
	 if err != nil {
		 debug.Println(err)
		 return
	 }
	 defer srv.Stop(t)
 
	 conn, err := srv.Connect(t)
	 if err != nil {
		 debug.Println(err)
		 return
	 }
 
	 var sentMsg strings.Builder
	 for i := 0; i < NumShort; i++ {
		 msg := fmt.Sprintf("Hello World %d\n", i)
		 debug.Print()
		 debug.Printf("==== Starting Many %d ====", i)
 
		 sentMsg.WriteString(msg)
		 writeMessage(t, msg, conn, WriteTimeout)
		 time.Sleep(2 * ReadTimeout)
	 }
 
	 recdMsg := readMessage(t, srv.stdout, ReadTimeout)
 
	 compareMessages(t, sentMsg.String(), recdMsg)
 }
 
 func TestServerMobyDick(t *testing.T) {
	 // desc := "Server: Receive the entire text of Moby Dick"
	 // note := "Reference Client ⇌ Student Server"
	 msg := MobyDick
	 if len(msg) == 0 {
		 debug.Printf("Unable to locate mobydick.txt")
		 t.Skip("Unable to locate mobydick.txt")
		 return
	 }
	 testServerMessage(t, msg)
 }
 
 func TestServerShortRandomPrintable(t *testing.T) {
	 // desc := "Server: Receive random short printable ASCII messages"
	 // note := "Reference Client ⇌ Student Server"
	 for i := 1; i <= NumShortRandomPrintable; i++ {
		 msg := randString(1, 63, Printable)
		 name := fmt.Sprintf("Message&%d", i)
		 t.Run(name, func(t *testing.T) { testServerMessage(t, msg) })
	 }
 }
 
 func TestServerLongRandomPrintable(t *testing.T) {
	 // desc := "Server: Receive random long printable ASCII messages"
	 // note := "Reference Client ⇌ Student Server"
	 for i := 1; i <= NumLongRandomPrintable; i++ {
		 msg := randString(64, 512, Printable)
		 name := fmt.Sprintf("Message&%d", i)
		 t.Run(name, func(t *testing.T) { testServerMessage(t, msg) })
	 }
 }
 
 // ------------------------ Binary Tests ------------------------------------
 func TestServerBinary(t *testing.T) {
	 // desc := "Server: Receive a short binary message"
	 // note := "Reference Client ⇌ Student Server"
	 msg := string([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20})
	 testServerMessage(t, msg)
 }
 
 func TestServerShortRandomBinary(t *testing.T) {
	 // desc := "Server: Receive random short binary messages"
	 // note := "Reference Client ⇌ Student Server"
	 for i := 1; i <= NumShortRandomBinary; i++ {
		 msg := randString(1, 63, Binary)
		 name := fmt.Sprintf("Message&%d", i)
		 t.Run(name, func(t *testing.T) { testServerMessage(t, msg) })
	 }
 }
 
 func TestServerLongRandomBinary(t *testing.T) {
	fmt.Println("I AM HEREEEEEE");
	 // desc := "Server: Receive random long binary messages"
	 // note := "Reference Client ⇌ Student Server"
	 for i := 1; i <= NumLongRandomBinary; i++ {
		 msg := randString(64, 512, Binary)
		 name := fmt.Sprintf("Message&%d", i)
		 t.Run(name, func(t *testing.T) { testServerMessage(t, msg) })
	 }
 }
 
 // func TestServer(t *testing.T) {
 // // desc :=
 // 	msg := ""
 // 	testServerMessage(t, msg)
 // }
 
 /******************************************************************************/
 /*                            ...                                             */
 /******************************************************************************/