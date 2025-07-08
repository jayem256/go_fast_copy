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

var socket net.Conn
var crypto *networking.Crypto

// Connect opens TCP connection to target host address
func Connect(address string, dscp int, mptcp bool) error {
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
	socket = conn
	// Set TCP_NODELAY to always immediately send.
	socket.(*net.TCPConn).SetNoDelay(true)
	// Set DSCP. NOTE: On Windows by default it will not apply the value.
	ipv4.NewConn(conn).SetTOS(dscp)

	return nil
}

// ServerEhlo reads server greeting and nonce
func ServerEhlo() []byte {
	crypto = new(networking.Crypto).WithKeyNonce(nil, nil)
	ehlo := readResponse(opcode.EHLO)
	if ehlo != nil {
		// Ehlo from server contains nonce.
		var content networking.EHLO
		networking.DecodePayload(ehlo.Payload, &content, crypto)

		nonce := make([]byte, 16)
		copy(nonce, content.Nonce[:])

		return nonce
	}
	return nil
}

// Authenticate sends handshake to server
func Authenticate(key string, nonce []byte) (*networking.Crypto, error) {
	if key != "" {
		// Init AES with key and nonce.
		crypto = new(networking.Crypto).WithKeyNonce([]byte(key), nonce)
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
		secret = crypto.Encrypt(secret)

		// Additional auth payload is required.
		block := &networking.AuthBlock{
			BlockLen: uint16(len(secret)),
		}
		auth.Payload = networking.PayloadToBytes(block, crypto)
		out, _ := networking.PacketToBytes(&auth)
		out = append(out, secret...)

		socket.Write(out)
	} else {
		out, _ := networking.PacketToBytes(&auth)
		socket.Write(out)
	}

	resp := readResponse(opcode.HANDSHAKE)
	if resp != nil {
		if resp.Flags != 1 {
			return nil, errors.New("authentication failed")
		}
	}
	return crypto, nil
}

// Initiate tells server to prepare to receive file of given name
func Initiate(file string, hash []byte, hashingMethod uint8) uint8 {
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
	if crypto != nil {
		tarHdrBytes = crypto.Encrypt(tarHdrBytes)
	}
	fileTransfer.Payload = tarHdrBytes

	out, _ := networking.PacketToBytes(&fileTransfer)
	socket.Write(out)

	// Get server response.
	resp := readResponse(opcode.BEGINFILETRANSFER)

	if resp != nil {
		return resp.Flags
	}

	return 0
}

// EndFileTransfer tells server current session is terminating
func EndFileTransfer(file string, hash []byte, hashingMethod uint8) bool {
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

	end.Payload = networking.PayloadToBytes(eof, crypto)

	out, _ := networking.PacketToBytes(&end)
	socket.Write(out)

	// Wait for server ack.
	resp := readResponse(opcode.ENDFILETRANSFER)

	if resp != nil {
		if resp.Flags > 0 {
			var end networking.EndFileTransfer
			err := networking.DecodePayload(resp.Payload, &end, crypto)
			if err != nil {
				return false
			}
			return end.Checksum == eof.Checksum
		}
	}

	return false
}

// StartChunkStream streams processed chunk data to server
func StartChunkStream(jumbo bool, channels []chan []byte) {
	frameSize := constants.DEFAULT_TCP_FRAME_SIZE
	if jumbo {
		// Attempt to send larger TCP frames.
		frameSize = constants.JUMBO_TCP_FRAME_SIZE
	}
	frame := make([]byte, 0, frameSize)
	lastWork := time.Now()
	for {
		closed := 0
		var didWork bool
		for _, inpChan := range channels {
			concatenated, closeInc, ready := processCompletedChunkChannel(frame, frameSize, inpChan)
			frame = concatenated
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
	if len(frame) > 0 {
		socket.Write(frame)
	}
}

// processCompletedChunkChannel performs non-blocking read on worker channels and sends data if available.
// Return value is current buffer including concatenated bytes, increment for # of closed channels and
// boolean for whether the channel had anything to consume.
func processCompletedChunkChannel(frame []byte, frameSize int, chonker chan []byte) ([]byte, int, bool) {
	select {
	case msg, open := <-chonker:
		if msg == nil {
			return frame, 1, true
		}
		if len(frame)+len(msg) <= frameSize {
			frame = append(frame, msg...)
		} else {
			olen := len(msg)
			ptr := 0
			for {
				remaining, full := appendPartial(frameSize, frame, msg[ptr:])
				ptr = olen - remaining

				// Frame is full or all of chunk data has been consumed.
				_, err := socket.Write(full)

				if err != nil {
					panic(err)
				}

				// Prepare next frame.
				frame = make([]byte, 0, frameSize)

				if remaining == 0 {
					break
				}
			}
		}
		if len(frame) == frameSize {
			_, err := socket.Write(frame)
			if err != nil {
				panic(err)
			}
			frame = make([]byte, 0, frameSize)
		}
		var closed int
		if !open {
			closed = 1
		}
		return frame, closed, true
	default:
		return frame, 0, false
	}
}

// appendPartial appends as much as it can and return number of bytes remaining
func appendPartial(max int, frame, bytes []byte) (int, []byte) {
	space := max - len(frame)
	if len(bytes) > space {
		frame = append(frame, bytes[0:space]...)
		return len(bytes) - space, frame
	}
	frame = append(frame, bytes...)
	return 0, frame
}

// Close closes socket
func Close() {
	socket.Close()
}

// readResponse reads full message from stream and matches it to opcode
func readResponse(opcode uint8) *networking.Packet {
	msg := make([]byte, 4)

	// Read message header first.
	_, err := io.ReadFull(socket, msg)

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
		len, err := io.ReadFull(socket, payload)

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
