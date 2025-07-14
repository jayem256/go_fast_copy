package server

import (
	"context"
	"fmt"
	"go_fast_copy/networking"
	"go_fast_copy/networking/opcode"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"math/rand"
)

var authenticated bool
var chunksize int
var workers int
var wqlen int
var folder string

// StartListening binds new listening socket
func StartListening(key, path, addr string, blocksize, numworkers, queue int, mptcp bool) {
	var err error
	chunksize = blocksize * 1024
	workers = numworkers
	wqlen = queue
	folder = filepath.Clean(path) + string(os.PathSeparator)

	// Check path validity.
	info, err := os.Stat(folder)

	if err != nil || !info.IsDir() {
		fmt.Println("Invalid root folder -", err.Error())
		os.Exit(1)
	}

	_, err = net.ResolveTCPAddr("tcp4", addr)

	if err != nil {
		panic(err)
	}

	lc := new(net.ListenConfig)
	// Set MPTCP.
	lc.SetMultipathTCP(mptcp)
	// Listen for incoming connections.
	l, err := lc.Listen(context.Background(), "tcp", addr)

	if err != nil {
		fmt.Println("Could not bind listening socket on " + addr)
		os.Exit(1)
	}

	// Close the listener when the application closes.
	defer l.Close()

	fmt.Println("Listening on " + addr)

	for {
		// Handle incoming connection.
		conn, err := l.Accept()
		// Set TCP_NODELAY to always immediately send.
		conn.(*net.TCPConn).SetNoDelay(true)

		if err != nil {
			fmt.Println("Failed to establish incoming connection")
			continue
		}

		fmt.Println("New connection from: " + conn.RemoteAddr().String())

		authenticated = false
		// Generate new nonce for session.
		nonce := generateNonce()
		// Send greeting with nonce.
		sendEhlo(conn, nonce)
		// Enable encryption if in use.
		initCrypto(key, nonce)
		// Start handling client requests.
		handleRequest(conn)
		// Reset crypto.
		initCrypto("", nil)
		// Reset authentication state.
		authenticated = false

		fmt.Println("Client disconnected")
	}
}

// sendEhlo sends greeting message to client with optional nonce
func sendEhlo(conn net.Conn, nonce []byte) {
	ehlo := networking.Packet{
		Header: networking.Header{
			Opcode: opcode.EHLO,
			Flags:  1,
		},
	}
	nonceBlock := &networking.EHLO{
		Nonce: [16]byte{},
	}
	copy(nonceBlock.Nonce[:], nonce)
	ehlo.Payload = networking.PayloadToBytes(nonceBlock, crypto)
	out, _ := networking.PacketToBytes(&ehlo)
	conn.Write(out)
}

// generateNonce generates new nonce for session
func generateNonce() []byte {
	nonce := make([]byte, 16)
	// Generate nonce for this session.
	randr := rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := range nonce {
		nonce[i] = byte(randr.Intn(255))
	}
	return nonce
}

// handleRequest handles whole session
func handleRequest(conn net.Conn) {
	defer conn.Close()

	for {
		msg := make([]byte, 4)

		// Read message header first.
		len, err := io.ReadFull(conn, msg)

		if err != nil {
			// Connection closed.
			return
		}

		if len == 4 {
			// decode 4 bytes as Header.
			header, err := networking.DecodeHeader(msg)

			if err != nil {
				fmt.Println(conn.RemoteAddr().String() + " " + err.Error())
			} else {
				packet := &networking.Packet{Header: *header}
				var payload []byte

				// message header is followed by payload.
				if header.Len > 4 {
					payloadLen := header.Len - 4

					payload = make([]byte, payloadLen)
					len, err := io.ReadFull(conn, payload)

					if len != int(payloadLen) || err != nil {
						fmt.Println("Recv len mismatch: " + strconv.Itoa(len) +
							" vs " + strconv.Itoa(int(payloadLen)) + " expected")
						return
					} else {
						packet.Payload = payload
						// handle decoded message with payload.
						dispatcher(conn, packet)
					}
				} else {
					// empty payload.
					payload = make([]byte, 0)
					packet.Payload = payload
					// handle decoded message.
					dispatcher(conn, packet)
				}
			}
		} else {
			fmt.Println(conn.RemoteAddr().String() + " sent malformed header")
		}
	}
}

// dispatcher determines what to do with incoming messages
func dispatcher(conn net.Conn, packet *networking.Packet) {
	if packet.Opcode == opcode.HANDSHAKE {
		authenticated = handleHandshake(conn, packet)
		if !authenticated {
			fmt.Println("Authentication failed for client", conn.RemoteAddr().String())
		}
	} else {
		// For messages other than authentication itself the connection must be authenticated.
		if authenticated {
			switch packet.Opcode {
			case opcode.BEGINFILETRANSFER:
				startFileTransfer(conn, packet, folder, chunksize, workers, wqlen)
			case opcode.NEXTCHUNK:
				nextFileDataChunk(conn, packet)
			case opcode.ENDFILETRANSFER:
				endFileTransfer(conn, packet)
			default:
				fmt.Println("Don't know what to do with message opcode " + strconv.Itoa(int(packet.Opcode)))
			}
		} else {
			fmt.Println("Dropping unauthorized connection", conn.RemoteAddr().String())
			// Not authorized to perform any operations.
			conn.Close()
		}
	}
}
