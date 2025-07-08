package server

import (
	"archive/tar"
	"bytes"
	"encoding/hex"
	"fmt"
	"go_fast_copy/constants"
	"go_fast_copy/fileio"
	"go_fast_copy/networking"
	"go_fast_copy/networking/opcode"
	"go_fast_copy/server/worker"
	"io"
	"net"
	"os"
	"time"
)

var writer *worker.ChunkProcessor
var crypto *networking.Crypto
var requireAuth bool

// initCrypto initializes encryption with given key and nonce
func initCrypto(passphrase string, nonce []byte) {
	requireAuth = !(passphrase == "")

	if requireAuth {
		crypto = new(networking.Crypto).WithKeyNonce([]byte(passphrase), nonce)
	} else {
		crypto = new(networking.Crypto).WithKeyNonce(nil, nil)
	}
}

// handleHandshake handles response to handshake request
func handleHandshake(conn net.Conn, packet *networking.Packet) bool {
	resp := networking.Packet{
		Header: networking.Header{
			Opcode: opcode.HANDSHAKE,
			Flags:  1,
		},
	}

	// Encryption is in use and authentication is required.
	if packet.Flags == 1 {
		var auth networking.AuthBlock
		if networking.DecodePayload(packet.Payload, &auth, crypto) != nil {
			return false
		}

		block := make([]byte, auth.BlockLen)

		// Read authentication block containing secret. Apply time constraints.
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, err := io.ReadFull(conn, block)
		if err != nil {
			return false
		}
		conn.SetReadDeadline(time.Time{})

		// Check if we could decrypt contents of the block with our key.
		if !crypto.MatchSecret(crypto.Decrypt(block)) {
			resp.Flags = 0
		}
	} else if requireAuth {
		resp.Flags = 0
	}

	out, _ := networking.PacketToBytes(&resp)
	conn.Write(out)

	return resp.Flags > 0
}

// startFileTransfer handles response to file transfer request
func startFileTransfer(conn net.Conn, packet *networking.Packet, path string, blocksize, forks, wqlen int) {
	tarHdrBytes := packet.Payload
	if crypto != nil {
		tarHdrBytes = crypto.Decrypt(tarHdrBytes)
	}

	hdrb := bytes.NewBuffer(tarHdrBytes)
	tarra := tar.NewReader(hdrb)
	// Read tar header.
	header, err := tarra.Next()

	if err == nil {
		filename := path + header.Name
		fmt.Println("Received client request to start transfer for:", filename)

		resp := networking.Packet{
			Header: networking.Header{
				Opcode: packet.Opcode,
				Flags:  1,
			},
		}

		// File checksum enabled.
		if packet.Flags > 0 {
			// File with same name already exists.
			if _, err = os.Stat(filename); err == nil {
				var hash []byte
				if packet.Flags == 1 {
					// Use CRC32 to check if file is identical.
					hash = fileio.GetFileChecksumCRC32(filename)
				} else if packet.Flags == 2 {
					// Use SHA256 to check if file is identical.
					hash = fileio.GetFileChecksumSHA256(filename)
				}
				// File with same name and content exists. No need to transfer it.
				if header.PAXRecords[constants.PAXAttr] == hex.EncodeToString(hash) {
					resp.Flags = 2
				}
			}
		}

		out, _ := networking.PacketToBytes(&resp)
		conn.Write(out)

		if resp.Flags == 2 {
			fmt.Println("Identical file already exists locally. Omitting transfer!")
			conn.Close()
			return
		}

		// Start writer and workers.
		writer = new(worker.ChunkProcessor)
		writer.NewFile(filename, blocksize, wqlen, packet.Flags == 2)
		writer.StartForks(forks, crypto)
	} else {
		fmt.Println(err)
		conn.Close()
	}
}

// endFileTransfer handles response to end file transfer request
func endFileTransfer(conn net.Conn, packet *networking.Packet) {
	var end networking.EndFileTransfer
	err := networking.DecodePayload(packet.Payload, &end, crypto)

	// Wait for file writer to complete.
	hash := writer.Stop()

	if err != nil {
		conn.Close()
		fmt.Println("Malformed teardown message from client. Ending file transfer without checksum.")
		return
	}

	resp := networking.Packet{
		Header: networking.Header{
			Opcode: packet.Opcode,
			Flags:  1,
		},
	}
	eft := &networking.EndFileTransfer{
		Checksum: [32]byte{},
	}
	copy(eft.Checksum[:], hash)
	resp.Payload = networking.PayloadToBytes(eft, crypto)

	if packet.Flags > 0 {
		if end.Checksum != eft.Checksum {
			fmt.Println("Checksum mismatch!")
			resp.Flags = 0
		} else {
			fmt.Println("Checksum match. File transfer completed!")
		}
	} else {
		fmt.Println("No checksum verification requested. File transfer completed!")
	}

	out, _ := networking.PacketToBytes(&resp)

	conn.Write(out)
	conn.Close()
}

// nextFileDataChunk handles processing of data chunks
func nextFileDataChunk(conn net.Conn, packet *networking.Packet) {
	var chonk networking.DataStreamChunk
	err := networking.DecodePayload(packet.Payload, &chonk, crypto)

	if err != nil {
		conn.Close()
		writer.Stop()
		fmt.Println("Malformed chunk message from client. Ending file transfer.")
		return
	}

	if chonk.Sequence == 0 {
		return
	}

	chunkData := make([]byte, chonk.DataLength)

	// Read full chunk.
	_, err = io.ReadFull(conn, chunkData)

	if err != nil {
		conn.Close()
		writer.Stop()
		fmt.Println("Incomplete chunk from client. Ending file transfer.")
		return
	}

	// Have workers process the chunk.
	writer.ProcessNextChunk(&worker.UnprocessedChunk{
		Seq:        chonk.Sequence,
		Compressed: chonk.Compression > 0,
		Data:       chunkData,
	})
}
