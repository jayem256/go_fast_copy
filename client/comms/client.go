package comms

import (
	"archive/tar"
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"go_fast_copy/constants"
	"go_fast_copy/networking"
	"go_fast_copy/networking/opcode"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"golang.org/x/net/ipv4"
)

type Client struct {
	socket net.Conn
	crypto *networking.Crypto
}

// Connect opens TCP connection to target host address
func (c *Client) Connect(address string, dscp int, mptcp bool) error {
	_, err := net.ResolveTCPAddr("tcp", address)
	if err != nil {
		return err
	}
	dial := new(net.Dialer)
	// Set MPTCP.
	dial.SetMultipathTCP(mptcp)
	// Connect to host.
	conn, err := dial.Dial("tcp", address)

	if err != nil {
		return err
	}
	c.socket = conn
	// Set TCP_NODELAY to always immediately send.
	c.socket.(*net.TCPConn).SetNoDelay(true)
	// Set DSCP. NOTE: On Windows by default it will not apply the value.
	ipv4.NewConn(conn).SetTOS(dscp)

	return nil
}

// ServerEhlo reads server greeting and nonce
func (c *Client) ServerEhlo() []byte {
	c.crypto = new(networking.Crypto).WithKeyNonce(nil, nil)
	ehlo := c.readResponse(opcode.EHLO)
	if ehlo != nil {
		// Ehlo from server contains nonce.
		var content networking.EHLO
		networking.DecodePayload(ehlo.Payload, &content, c.crypto)

		nonce := make([]byte, 16)
		copy(nonce, content.Nonce[:])

		return nonce
	}
	return nil
}

// Authenticate sends handshake to server
func (c *Client) Authenticate(key string, nonce []byte) (*networking.Crypto, error) {
	if key != "" {
		// Init AES with key and nonce.
		c.crypto = new(networking.Crypto).WithKeyNonce([]byte(key), nonce)
	}

	auth := networking.Packet{
		Header: networking.Header{
			Opcode: opcode.HANDSHAKE,
			Flags:  0, // 0: no encryption, 1: AES256
		},
	}

	// Encryption is enabled.
	if key != "" {
		auth.Flags = 1
		// PSK is the common denominator.
		secret := []byte(key)
		secret = c.crypto.Encrypt(secret)

		// Additional auth payload is required.
		block := &networking.AuthBlock{
			BlockLen: uint16(len(secret)),
		}
		auth.Payload = networking.PayloadToBytes(block, c.crypto)
		out, _ := networking.PacketToBytes(&auth)
		out = append(out, secret...)

		c.socket.Write(out)
	} else {
		out, _ := networking.PacketToBytes(&auth)
		c.socket.Write(out)
	}

	resp := c.readResponse(opcode.HANDSHAKE)
	if resp != nil {
		if resp.Flags != 1 {
			return nil, errors.New("authentication failed")
		}
	}
	return c.crypto, nil
}

// Initiate tells server to prepare to receive file of given name
func (c *Client) Initiate(file string, hash []byte, hashingMethod uint8) uint8 {
	file = filepath.Base(file)

	fileTransfer := networking.Packet{
		Header: networking.Header{
			Opcode: opcode.BEGINFILETRANSFER,
			Flags:  hashingMethod, // 0: disabled, 1: crc32, 2: sha256
		},
	}

	buffer := new(bytes.Buffer)
	tarra := tar.NewWriter(buffer)
	defer tarra.Close()

	// Write tar header to buffer.
	tarra.WriteHeader(&tar.Header{
		Format:   tar.FormatPAX,
		Typeflag: tar.TypeReg,
		Name:     file,
		PAXRecords: map[string]string{
			constants.PAXAttr: hex.EncodeToString(hash),
		},
	})

	tarHdrBytes := buffer.Bytes()
	if c.crypto != nil {
		tarHdrBytes = c.crypto.Encrypt(tarHdrBytes)
	}
	fileTransfer.Payload = tarHdrBytes

	out, _ := networking.PacketToBytes(&fileTransfer)
	c.socket.Write(out)

	// Get server response.
	resp := c.readResponse(opcode.BEGINFILETRANSFER)

	if resp != nil {
		return resp.Flags
	}

	return 0
}

// EndFileTransfer tells server current session is terminating
func (c *Client) EndFileTransfer(file string, hash []byte, hashingMethod uint8) bool {
	end := networking.Packet{
		Header: networking.Header{
			Opcode: opcode.ENDFILETRANSFER,
			Flags:  hashingMethod, // 0: disabled, 1: crc32, 2: sha256
		},
	}

	eof := &networking.EndFileTransfer{
		Checksum: [32]byte{},
	}

	copy(eof.Checksum[:], hash)

	end.Payload = networking.PayloadToBytes(eof, c.crypto)

	out, _ := networking.PacketToBytes(&end)
	c.socket.Write(out)

	// Wait for server ack.
	resp := c.readResponse(opcode.ENDFILETRANSFER)

	if resp != nil {
		if resp.Flags > 0 {
			var end networking.EndFileTransfer
			err := networking.DecodePayload(resp.Payload, &end, c.crypto)
			if err != nil {
				return false
			}
			return end.Checksum == eof.Checksum
		}
	}

	return false
}

// StartChunkStream streams processed chunk data to server
func (c *Client) StartChunkStream(channels []chan []byte) {
	lastWork := time.Now()
	for {
		closed := 0
		var didWork bool
		for _, inpChan := range channels {
			closeInc, ready := c.processCompletedChunkChannel(inpChan)
			closed += closeInc
			didWork = didWork || ready
		}
		if closed == len(channels) {
			break
		}
		if didWork {
			lastWork = time.Now()
		}
		// If there's no work to do, pause busy looping for a moment.
		if time.Since(lastWork) > time.Millisecond*10 {
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// processCompletedChunkChannel performs non-blocking read on worker channels and sends data if available.
// Return value is increment for # of closed channels and boolean whether channel produced anything.
func (c *Client) processCompletedChunkChannel(chonker chan []byte) (int, bool) {
	select {
	case msg, open := <-chonker:
		if msg == nil {
			return 1, true
		}

		_, err := c.socket.Write(msg)

		if err != nil {
			panic(err)
		}

		var closed int
		if !open {
			closed = 1
		}

		return closed, true
	default:
		return 0, false
	}
}

// Close closes socket
func (c *Client) Close() {
	c.socket.Close()
}

// readResponse reads full message from stream and matches it to opcode
func (c *Client) readResponse(opcode uint8) *networking.Packet {
	msg := make([]byte, 4)

	// Read message header first.
	_, err := io.ReadFull(c.socket, msg)

	if err != nil {
		fmt.Println("Lost connection")
		os.Exit(3)
	}

	// decode 4 bytes as Header.
	header, err := networking.DecodeHeader(msg)

	if err != nil {
		return nil
	}

	packet := &networking.Packet{Header: *header}
	var payload []byte

	// message header is followed by payload.
	if header.Len > 4 {
		payloadLen := header.Len - 4

		payload = make([]byte, payloadLen)
		len, err := io.ReadFull(c.socket, payload)

		if len != int(payloadLen) || err != nil {
			fmt.Println("Recv len mismatch: " + strconv.Itoa(len) +
				" vs " + strconv.Itoa(int(payloadLen)) + " expected")
			return nil
		} else {
			packet.Payload = payload
		}
	}

	if packet.Opcode != opcode {
		return nil
	}

	return packet
}
