/*****************************************************************************
 * server.go
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

const RECV_BUFFER_SIZE = 2048

/* server()
 * Open socket and wait for client to connect
 * Print received message to stdout
 */
func server(server_port string) {
	// listen on the specified port 
	ln, err := net.Listen("tcp", ":"+server_port)
	if err != nil {
		log.Fatal("Failed to listen on port: ", err)
	}
	defer ln.Close()

	
	for { // infinite loop to accept clients 
		conn, err := ln.Accept() // accept a client connection
		if err != nil { 
			// accept errors are client-connection related; log and continue (not fatal to the server) to next iteration to accept next client
			log.Print("Failed to accept connection: ", err)
			continue
		}

		// read all data from this client and print it to stdout; symmetric to client 
		buf := make([]byte, RECV_BUFFER_SIZE)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				_, werr := os.Stdout.Write(buf[:n])
				if werr != nil {
					log.Print("Failed to write to stdout: ", werr)
				}
			}
			if err == io.EOF {
				// client closed the connection so we go to next iteration 
				break
			}
			if err != nil {
				// client connection error
				log.Print("Error reading from connection: ", err)
				break
			}
		}
		conn.Close()
	}
}

// Main parses command-line arguments and calls server function
func main() {
	if len(os.Args) != 2 {
		log.Fatal("Usage: ./server [server port]")
	}
	server_port := os.Args[1]
	server(server_port)
}
