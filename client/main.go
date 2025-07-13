package main

import (
	"encoding/hex"
	"fmt"
	"go_fast_copy/client/comms"
	"go_fast_copy/client/worker"
	"go_fast_copy/constants"
	"go_fast_copy/fileio"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/akamensky/argparse"
)

func main() {
	args := argparse.NewParser("client", constants.Title)

	bind := args.String("a", "address", &argparse.Options{Required: true, Help: "Target host address"})
	chunk := args.Int("c", "chunksize", &argparse.Options{Required: false, Help: "File I/O chunk size in KB " +
		"(" + strconv.Itoa(constants.MIN_CLIENT_CHUNK_SIZE) + "-" +
		strconv.Itoa(constants.MAX_CLIENT_CHUNK_SIZE) + ")", Default: constants.DEFAULT_FILE_CHUNK_SIZE})
	dscp := args.Int("d", "dscp", &argparse.Options{Required: false, Help: "DSCP field for QoS",
		Default: constants.DEFAULT_DSCP})
	file := args.String("f", "file", &argparse.Options{Required: true, Help: "File path"})
	pass := args.String("k", "key", &argparse.Options{Required: false, Help: "Encryption key (16 or 32 characters). Enables AES 128 or 256 encryption"})
	mptcp := args.Flag("m", "mptcp", &argparse.Options{Help: "Enable Multipath TCP"})
	omit := args.Flag("o", "omit", &argparse.Options{Help: "Omit checksum calculation"})
	port := args.Int("p", "port", &argparse.Options{Required: false, Help: "Target port",
		Default: constants.DEFAULT_PORT})
	sha := args.Flag("s", "sha", &argparse.Options{Help: "Use SHA256 checksum instead of CRC32"})
	workers := args.Int("t", "threads", &argparse.Options{Required: false, Help: "Number of compression (and encryption) threads",
		Default: constants.DEFAULT_NUM_WORKERS * 2})

	err := args.Parse(os.Args)

	if err != nil {
		fmt.Print(args.Usage(err))
		os.Exit(1)
	}

	if *pass != "" {
		if !(len(*pass) == 32) && !(len(*pass) == 16) {
			fmt.Println("Key length must be 16 or 32 bytes")
			os.Exit(1)
		}
	}

	fileName := filepath.Clean(*file)

	// Get file info.
	finfo, err := os.Stat(fileName)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	// Do nothing if it's a folder.
	if finfo.IsDir() {
		fmt.Println("Provided path is directory. Skipping.")
		os.Exit(0)
	}

	debug.SetGCPercent(666)

	addr := *bind + ":" + strconv.Itoa(*port)

	comms := new(comms.Client)

	// Connect to host.
	err = comms.Connect(addr, *dscp, *mptcp)

	if err == nil {
		fmt.Println("Connected to", addr)

		// Get server greeting and nonce.
		nonce := comms.ServerEhlo()

		// Perform handshake with server.
		crypto, err := comms.Authenticate(*pass, nonce)
		if err != nil {
			fmt.Println(err.Error())
			os.Exit(1)
		}
		fmt.Println("Handshake ok")

		// 8MB chunks the limit.
		if *chunk > constants.MAX_CLIENT_CHUNK_SIZE {
			*chunk = constants.MAX_CLIENT_CHUNK_SIZE
			fmt.Println("Chunk size above maximum. Using " + strconv.Itoa(*chunk))
		} else if *chunk < constants.MIN_CLIENT_CHUNK_SIZE {
			// 64KB chunks minimum.
			*chunk = constants.MIN_CLIENT_CHUNK_SIZE
			fmt.Println("Chunk size below minimum. Using " + strconv.Itoa(*chunk))
		}

		worker := new(worker.CompressingReader)
		err = worker.StartFileReader(fileName, *workers, *chunk)

		if err == nil {
			fmt.Println("Starting file transfer for", fileName)

			var hash []byte
			var method uint8

			if !*omit {
				if *sha {
					method = 2
					hash = fileio.GetFileChecksumSHA256(fileName)
				} else {
					method = 1
					hash = fileio.GetFileChecksumCRC32(fileName)
				}
				fmt.Println("Checksum", hex.EncodeToString(hash))
			}

			// Request file transfer.
			status := comms.Initiate(fileName, hash, method)

			switch status {
			case 0:
				fmt.Println("Server not ready to receive the file")
				os.Exit(1)
			case 1:
				fmt.Println("Server is ready to accept the file")
			case 2:
				fmt.Println("Server already has identical file. Omitting!")
				os.Exit(0)
			default:
				fmt.Println("Server did not accept the file")
				os.Exit(1)
			}

			begin := time.Now()

			// Start sending chunks.
			channels := worker.StartWorkers(*workers, crypto)
			comms.StartChunkStream(channels)

			comp, total, compStats := worker.GetChunkStats()
			fmt.Println("Sent all data in",
				time.Since(begin), "with", comp, "/", total, "chunks compressed")
			fmt.Println(compStats)

			fmt.Println("Waiting for server to confirm")
			// EOF negotiation with server.
			ack := comms.EndFileTransfer(fileName, hash, method)

			if ack {
				fmt.Println("Server confirmed file has been synced")
			} else {
				if *omit {
					fmt.Println("Omitting checksum verification. File integrity unknown.")
				} else {
					fmt.Println("File transfer may not have completed or data may be corrupted")
					os.Exit(2)
				}
			}
		} else {
			fmt.Println(err.Error())
			os.Exit(1)
		}
		// Close connection.
		comms.Close()
		fmt.Println("Disconnected")
	} else {
		fmt.Println(err.Error())
		os.Exit(1)
	}
}
