package server

import (
	"archive/tar"
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"go_fast_copy/constants"
	"go_fast_copy/fileio"
	"go_fast_copy/networking"
	"go_fast_copy/networking/opcode"
	"go_fast_copy/server/worker"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Handler struct {
	writer      *worker.ChunkProcessor
	crypto      *networking.Crypto
	requireAuth bool
}

// initCrypto initializes encryption with given key and nonce
func (h *Handler) initCrypto(passphrase string, nonce []byte) {
	h.requireAuth = !(passphrase == "")

	if h.requireAuth {
		h.crypto = new(networking.Crypto).WithKeyNonce([]byte(passphrase), nonce)
	} else {
		h.crypto = new(networking.Crypto).WithKeyNonce(nil, nil)
	}
}

// handleHandshake handles response to handshake request
func (h *Handler) handleHandshake(conn net.Conn, packet *networking.Packet) bool {
	resp := networking.Packet{
		Header: networking.Header{
			Opcode: opcode.HANDSHAKE,
			Flags:  1,
		},
	}

	// Encryption is in use and authentication is required.
	if packet.Flags == 1 {
		var auth networking.AuthBlock
		if networking.DecodePayload(packet.Payload, &auth, h.crypto) != nil {
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
		if !h.crypto.MatchSecret(h.crypto.Decrypt(block)) {
			resp.Flags = 0
		}
	} else if h.requireAuth {
		resp.Flags = 0
	}

	out, _ := networking.PacketToBytes(&resp)
	conn.Write(out)

	return resp.Flags > 0
}

// startFileTransfer handles response to file transfer request
func (h *Handler) startFileTransfer(conn net.Conn, packet *networking.Packet, rootPath string, blocksize, forks, wqlen int) {
	if h.writer != nil {
		// Previous transfer has not completed.
		out, _ := networking.PacketToBytes(&networking.Packet{
			Header: networking.Header{
				Opcode: packet.Opcode,
				Flags:  0,
			},
		})
		conn.Write(out)
		return
	}

	tarHdrBytes := packet.Payload
	if h.crypto != nil {
		tarHdrBytes = h.crypto.Decrypt(tarHdrBytes)
	}

	hdrb := bytes.NewBuffer(tarHdrBytes)
	tarra := tar.NewReader(hdrb)
	// Read tar header.
	header, err := tarra.Next()

	if err == nil {
		localizedPath, err := filepath.Localize(header.Name)
		// Neither localize nor To/FromSlash seem to convert paths between OS formats.
		localizedPath = strings.ReplaceAll(localizedPath, "\\", string(os.PathSeparator))
		localizedPath = strings.ReplaceAll(localizedPath, "/", string(os.PathSeparator))
		filename := rootPath + localizedPath

		resp := networking.Packet{
			Header: networking.Header{
				Opcode: packet.Opcode,
				Flags:  1,
			},
		}

		if err != nil {
			resp.Flags = 3
			fmt.Println("Invalid path requested:", filename)
		} else {
			fmt.Println("Received client request to start transfer for:", filename)

			// Walk the path of light.
			err = filepath.Walk(filepath.Dir(filename), func(path string, info fs.FileInfo, err error) error {
				if !strings.HasPrefix(path, filepath.Clean(rootPath)) {
					// We have strayed from the path of light.
					return errors.New("invalid path " + path + ":" + rootPath)
				}
				if err != nil {
					if errors.Is(err, os.ErrNotExist) {
						// Create the directory if it doesn't already exist.
						return os.MkdirAll(path, os.ModePerm)
					} else {
						return err
					}
				}
				return nil
			})

			if err != nil {
				resp.Flags = 3
				fmt.Println(err.Error())
			} else {
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
			}
		}

		out, _ := networking.PacketToBytes(&resp)
		conn.Write(out)

		if resp.Flags == 2 {
			fmt.Println("Identical file already exists locally. Omitting transfer!")
			return
		} else if resp.Flags == 3 {
			fmt.Println("Could not start transfer for requested file")
			conn.Close()
			return
		}

		// Start writer and workers.
		h.writer = new(worker.ChunkProcessor)
		h.writer.NewFile(new(fileio.BufferedFactory), filename, blocksize, wqlen, packet.Flags == 2)
		h.writer.StartForks(forks, h.crypto)
	} else {
		fmt.Println(err)
		conn.Close()
	}
}

// endFileTransfer handles response to end file transfer request
func (h *Handler) endFileTransfer(conn net.Conn, packet *networking.Packet) {
	var end networking.EndFileTransfer
	err := networking.DecodePayload(packet.Payload, &end, h.crypto)

	// Wait for file writer to complete.
	hash := h.writer.Stop()

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
	resp.Payload = networking.PayloadToBytes(eft, h.crypto)

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
	h.writer = nil
}

// nextFileDataChunk handles processing of data chunks
func (h *Handler) nextFileDataChunk(conn net.Conn, packet *networking.Packet) {
	var chonk networking.DataStreamChunk
	err := networking.DecodePayload(packet.Payload, &chonk, h.crypto)

	if err != nil {
		conn.Close()
		h.writer.Stop()
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
		h.writer.Stop()
		fmt.Println("Incomplete chunk from client. Ending file transfer.")
		return
	}

	// Have workers process the chunk.
	h.writer.ProcessNextChunk(&worker.UnprocessedChunk{
		Seq:        chonk.Sequence,
		Compressed: chonk.Compression > 0,
		Data:       chunkData,
	})
}
