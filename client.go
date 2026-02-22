/*****************************************************************************
 * client.go
 * Name: Yuanning Hui
 * NetId: hh9077
 *****************************************************************************/

package main

import (
	"io"
	"log"
	"net"
	"os"
)

const SEND_BUFFER_SIZE = 2048

/* client()
 * Open socket and send message from stdin.
 */
func client(server_ip string, server_port string) {
	// connect to the server via TCP
	conn, err := net.Dial("tcp", server_ip+":"+server_port)
	if err != nil {
		log.Fatal("Failed to connect to server: ", err)
	}
	defer conn.Close()

	// read from stdin in chunks and send to server
	buf := make([]byte, SEND_BUFFER_SIZE) // make buffer for reading from stdin with size SEND_BUFFER_SIZE 
	// such that we can handle messages larger than the buffer size by sending in multiple chunks
	for { // infinite for loop until error  or EOF
		n, err := os.Stdin.Read(buf)
		if n > 0 { // if we read some bytes, send them to the server
			// handle partial sends: keep sending until all n bytes are sent
			sent := 0 // number of bytes sent so far
			for sent < n {
				written, werr := conn.Write(buf[sent:n]) // conn.Write may write fewer than n bytes, so we need to check how many bytes were actually written
				if werr != nil {
					log.Fatal("Failed to send data: ", werr)
				}
				sent += written
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal("Failed to read from stdin: ", err)
		}
	}
}

// Main parses command-line arguments and calls client function
func main() {
	if len(os.Args) != 3 {
		log.Fatal("Usage: ./client [server IP] [server port] < [message file]")
	}
	server_ip := os.Args[1]
	server_port := os.Args[2]
	client(server_ip, server_port)
}
