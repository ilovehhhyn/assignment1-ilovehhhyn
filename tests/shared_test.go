package main

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

/******************************************************************************/
/*                           Init & Constants                                 */
/******************************************************************************/

// Port to run server on if not otherwise specified
const DefaultPort = "31600"

// How long to wait for network IO before timing out
const (
	EpsilonTimeout = 3 * time.Millisecond   // Avoid race conditions
	AcceptTimeout  = 3 * time.Second        // just a guess - could probably be shorter
	ReadTimeout    = 35 * time.Millisecond  // 35 experimentally seems OK
	WriteTimeout   = 35 * time.Millisecond  // 35 experimentally seems OK
	StartupDelay   = 100 * time.Millisecond // Wait this long after starting a process to be sure it's ready
)

// Parameters for randString()
const (
	Printable = true
	Binary    = false
)

// Number of random tests to run
const (
	NumShort                = 10
	NumRandom               = 10
	NumShortRandomPrintable = NumRandom
	NumShortRandomBinary    = NumRandom
	NumLongRandomPrintable  = NumRandom
	NumLongRandomBinary     = NumRandom
)

// Where are student executables located?
var solutionDir = os.Getenv("SOLUTION_DIR")

// Debugging output to make sure the tests work OK (not for students)
var debugWriter = ioutil.Discard // [os.Stderr | ioutil.Discard]
// var debugWriter = os.Stderr // [os.Stderr | ioutil.Discard]

var debug = log.New(debugWriter, "", log.LstdFlags)

func init() {
	// Initialize MobyDick
	var err error
	MobyDick, err = ReadMobyDick()
	if err != nil {
		log.Println("Failed to read moby dick:", err)
	}

	// Seed the RNG so tests are repeatably random
	rand.Seed(316316316)
}

func TestMain(m *testing.M) {
	// Do nothing if --short flag provided (e.g. testing from IDE)
	flag.Parse()
	if testing.Short() {
		os.Exit(0)
	}

	m.Run()
}

/******************************************************************************/
/*                           Error Messages                                   */
/******************************************************************************/
var testFailMessage = `

*********** Test Failed ************
Message sent does not match message received!

-----------    Sent     ------------
%s

-----------  Received   ------------
%s

`

/******************************************************************************/
/*                          Test Messages                                     */
/******************************************************************************/
var (
	ShortMessage     = "Hello World"
	PrintfMessage    = "Hello Printf! %s %s %% %d %f %2.f"
	MultilineMessage = `
And when Ruby went over the hill,
Go came in for the kill.
It seemed so fast,
But oh at long last,
We all got tired of err != nil.

By Ryan McDermott

Source: https://www.freecodecamp.org/news/
programming-language-limericks-a8fb3416e0e4/
	`
	MobyDick = ""
)

// Read mobydick from file
func ReadMobyDick() (string, error) {
	filename := "mobydick.txt"
	file, err := os.Open(filename)
	if err != nil {
		return "", err
	}

	b, err := ioutil.ReadAll(file)
	if err != nil {
		return "", err
	}

	return string(b), nil
}

/******************************************************************************/
/*                           Message Type                                     */
/******************************************************************************/
const (
	MaxHashLength      = 4  // Max number of chars to include in hash
	MaxLinesPerMessage = 10 // Max number of output lines to be included in an error
	MaxCharsPerLine    = 55 // Max number of charts per output line included in an error
)

type Message struct {
	Message string
}

// Escaped() returns an escaped copy of the string, enclosed in quotation marks.
// Non-Ascii or nonprintable characters are escaped, excluding \n and \t
func (msg Message) Quoted() string {
	quoted := fmt.Sprintf("%q", msg.Message)
	escaped := quoted

	// Replace escaped `\n` with literal "\n"
	// Technically buggy `\\n` --> "\\\n" but "good enough" for debugging output
	re := regexp.MustCompile(`\\n`)
	escaped = re.ReplaceAllString(escaped, "\n")

	return escaped
}

// Hash returns a string containing the first 8 chars of msg's SHA1 hash
// Intended as a sanity check for long or non-human-readable
// messages to be convinced that two messages differ (or don't)
func (msg Message) Hash() string {
	h := sha1.New()
	h.Write([]byte(msg.Message))
	hash := h.Sum(nil)
	return fmt.Sprintf("%x", hash[:MaxHashLength])
}

// Diff returns a string reporting the first difference between two messages.
func (msg Message) Diff(other Message) string {
	a := msg.Message
	b := other.Message

	if strings.HasPrefix(a, b) {
		return "The first message is a prefix of the second."
	} else if strings.HasPrefix(b, a) {
		return "The second message is a prefix of the first."
	} else {
		return "The messages differ for the first time at position %d:\n%s\n\n%s"
	}
}

// String() returns a string representation of the message, preceded by a brief summary
func (msg Message) String() string {
	s := msg.Message
	fmtStr := "(%d lines, %d chars, hash: %s)\n\n%s"
	lines := strings.Split(msg.Message, "\n")
	return fmt.Sprintf(fmtStr, len(lines), len(s), msg.Hash(), msg.Shortened())
}

// trim a potentially long message down to a reasonable length and width
// Also apply quoting as described in Quoted()
func (msg Message) Shortened() string {
	var b strings.Builder

	lines := strings.Split(msg.Quoted(), "\n")
	for i, line := range lines {

		// Omit lines past the limit
		if i > MaxLinesPerMessage {
			fmt.Fprintf(&b, "(... %d more lines)", len(lines)-i)
			break
		}

		// Print the line itself, up to max number of chars
		if len(line) > MaxCharsPerLine {
			line = line[:MaxCharsPerLine] + "  (...)"
		}
		fmt.Fprintf(&b, "%s", line)

		// Print newline
		fmt.Fprintln(&b)
	}

	return b.String()
}

// Return a quoted form of the last k characters of the string
func (msg Message) Tail(k int) string {
	n := len(msg.Message)
	if n < k {
		return fmt.Sprintf("%q", msg.Message)
	}
	end := msg.Message[n-k : n]
	return fmt.Sprintf("%q", end)
}

/******************************************************************************/
/*                      TimeoutReader & TimeoutWriter                         */
/******************************************************************************/

/* Note on why TimeoutReader & TimeoutWriter are necessary:
 * Sometimes we want to test whatever output the client or server has produced,
 * without shutting down the entire server printing the output (e.g. if we would
 * like to send additional messages to that server later on.)
 *
 * Therefore we can't rely on EOF designating the end of a message, as the
 * server won't generate an EOF until it is shut down. Without a timeout,
 * reads and writes could hang indefinitely, even if the program generated the
 * right output.
 *
 * Note that a read or write timing out does not necessarily mean a test has
 * failed. It could indicate something benign, like a server correctly awaiting
 * further input, or something broken, like a client hanging forever
 *
 * TODO: Some broken implementations (e.g. double) are passing tests (MobyDick)
 * they shouldn't necessarily be passing - in these cases timeouts should count
 * as test failures, but there is no way to distinguish benign and broken timeouts
 */

var TimeoutError = errors.New("timed out")

// TimeoutReader is a wrapper for a normal reader, but its Reads will only block
// for a maximum of timeout before giving an error
type TimeoutReader struct {
	r       io.Reader
	timeout time.Duration
}

type RWRet struct {
	n   int
	err error
}

// NewTimeoutReader returns a new TimeoutReader with specified timeout and
// underlying reader r
func NewTimeoutReader(r io.Reader, timeout time.Duration) TimeoutReader {
	return TimeoutReader{r, timeout}
}

// Read bytes from underlying reader r into b, returning the number of bytes and
// any errors that occurred. err is io.EOF if the Read() call would block
// for more than the timeout allows
func (r TimeoutReader) Read(b []byte) (n int, err error) {
	// If testing client: r is a conn to server; use its deadline
	// WARNING: The deadline (may?) persist after this fn call, which is sometimes undesirable
	if conn, ok := r.r.(net.Conn); ok {
		debug.Println("setting reader conn deadline")
		conn.SetReadDeadline(time.Now().Add(r.timeout - EpsilonTimeout))
		return r.r.Read(b)
	} else {

		// Testing server: Deadlines not supported - just use old fashioned timeouts
		ch := make(chan RWRet)
		go func() {
			// debug.Println("launching async read")
			n, err := r.r.Read(b)
			// debug.Println("async read complete")
			ch <- RWRet{n, err}
		}()

		select {
		case ret := <-ch:
			return ret.n, ret.err
		case <-time.After(r.timeout):
			return 0, TimeoutError
		}
	}
}

// TimeoutWriter is a wrapper for a normal writer, but its Writes will only block
// for a maximum of timeout before giving an error.
type TimeoutWriter struct {
	w       io.Writer
	timeout time.Duration
}

// NewTimeoutWriter returns a new TimeoutWriter with specified timeout and
// underlying writer w
func NewTimeoutWriter(w io.Writer, timeout time.Duration) TimeoutWriter {
	return TimeoutWriter{w, timeout}
}

// Write bytes b into underlying writer w, returning the number of bytes and
// any errors that occurred. err is non-nil if the Write() call would block
// for more than the timeout allows
func (w TimeoutWriter) Write(b []byte) (n int, err error) {
	// WARNING: Deadline (may?) persist after this call, which is sometimes undesirable.
	// If testing server: w is a conn to server; use its deadline
	if conn, ok := w.w.(net.Conn); ok {
		debug.Println("setting write conn deadline")
		conn.SetWriteDeadline(time.Now().Add(w.timeout - EpsilonTimeout))
		return w.w.Write(b)
	} else {
		// Testing client: Deadlines not supported - just use old fashioned timeouts
		ch := make(chan RWRet)
		go func() {
			n, err := w.w.Write(b)
			ch <- RWRet{n, err}
		}()

		select {
		case ret := <-ch:
			return ret.n, ret.err
		case <-time.After(w.timeout):
			return 0, TimeoutError
		}
	}
}

/******************************************************************************/
/*                              Helper Functions                              */
/******************************************************************************/

// writeMessage writes msg to w, failing test t if any unexpected errors occur
func writeMessage(t *testing.T, msg string, w io.Writer, timeout time.Duration) {
	debug.Printf("Writing message (%d bytes) ...", len(msg))
	tw := NewTimeoutWriter(w, timeout)
	bmsg := []byte(msg)
	bytesToWrite := len(bmsg)
	bytesWritten := 0
	for bytesWritten < bytesToWrite {
		n, err := tw.Write(bmsg[bytesWritten:])
		debug.Printf("wrote %d bytes", n)

		if neterr, ok := err.(net.Error); ok && neterr.Timeout() {
			// Conn write timeout; stop writing. (a test may fail later)
			debug.Println("write hit conn deadline (timed out)")
			break
		} else if err == TimeoutError {
			// Write timeout; stop writing. (a test may fail later)
			debug.Println("write hit timeout (timed out)")
			break
		} else if err != nil {
			// Unexpected error - fail the test and report it
			e := fmt.Sprintf("Failed to write: %s", err)
			debug.Println(e)
			t.Errorf(e)
			break
		}
		// debug.Printf("wrote %d bytes", n)

		bytesWritten += n
	}
}

// readMessage reads a message from r, failing test t if any unexpected errors occur.
// Return the string representing the read message
func readMessage(t *testing.T, r io.Reader, timeout time.Duration) string {
	debug.Println("Reading message...")
	N := 2048
	b := make([]byte, N)
	var response bytes.Buffer
	tr := NewTimeoutReader(r, timeout)

	for {
		// Read in up to N byte chunk into b.
		n, err := tr.Read(b)

		// Add the chunk to the response
		response.Write(b[:n])
		debug.Println("read", n, "bytes")

		if err == io.EOF {
			// End of file - stop reading (this is probably normal behavior)
			debug.Println("read got EOF")
			break
		} else if err == TimeoutError {
			// Timeout - stop reading (a test may fail later)
			debug.Println("read hit timeout (timed out)")
			break
		} else if neterr, ok := err.(net.Error); ok && neterr.Timeout() {
			// Connection read timeout - stop reading (a test may fail later)
			debug.Println("read hit conn deadline (timed out)")
			break
		} else if err != nil {
			// Unexpected error - fail the test and report it
			e := fmt.Sprintf("Failed to read output: %s", err)
			t.Errorf(e)
			break
		}
		// debug.Println("read", n, "bytes")
	}
	return response.String()
}

func compareMessages(t *testing.T, sentStr, recdStr string) {
	debug.Println("Comparing messages...")
	sent := Message{sentStr}
	recd := Message{recdStr}

	if sentStr != recdStr {
		debug.Printf("expected: %s", sent)
		debug.Printf("found:    %s", recd)
		t.Errorf(testFailMessage, sent, recd)
	}
}

func testMessage(t *testing.T, msg string, w io.Writer, r io.Reader) {
	// Send the message
	writeMessage(t, msg, w, WriteTimeout)

	// Read results, timing out after ReadTimeout ms (default 50)
	response := readMessage(t, r, ReadTimeout)

	// Report results
	compareMessages(t, msg, response)
}

// randString generates a random string of at most cols columns wide and at least
// rows rows tall, which will consist of entirely printable ASCII characters if
// printable is true.
// The output will have exactly the specified rows and columns unless
// by chance a "\n" character is added to the output.
func randString(rows, cols int, printable bool) string {
	if rows <= 0 || cols <= 0 {
		return ""
	}

	var b strings.Builder
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {

			var c byte
			if printable {
				c = byte(rand.Intn(95) + 32) // Random printable ASCII char
			} else {
				c = byte(rand.Intn(256)) // Random byte
			}

			b.WriteByte(c)
		}
		b.WriteString("\n")
	}

	return b.String()
}

// Copied from /grader/runner/runner.go
// printErrors reads in messages from errPipe, printing them to stderr as they arrive
func printErrors(errPipe io.ReadCloser) {
	buferr := bufio.NewReader(errPipe)
	for {
		line, err := buferr.ReadString('\n')
		if err == io.EOF {
			fmt.Fprintf(os.Stderr, line) // print last message
			break
		} else if err != nil {
			log.Println("buferr: ", err)
			return
		}
		fmt.Fprintf(os.Stderr, line)
	}
	errPipe.Close()
}

// Returns true if the port is open
func isOpen(port string) bool {
	addr := "127.0.0.1:" + port
	debug.Println("Checking port", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	ln.Close()
	return true
}