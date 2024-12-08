package main

import (
	"encoding/hex"
	"fmt"
	"go_fast_copy/client/comms"
	"go_fast_copy/client/worker"
	"go_fast_copy/constants"
	"go_fast_copy/fileio"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/akamensky/argparse"
)

func main() {
	args := argparse.NewParser("client", constants.Title)

	bind := args.String("a", "address", &argparse.Options{Required: true, Help: "Target host address"})
	chunk := args.Int("c", "chunksize", &argparse.Options{Required: false, Help: "File I/O chunk size in KB (1-8192)",
		Default: constants.DEFAULT_FILE_CHUNK_SIZE})
	dscp := args.Int("d", "dscp", &argparse.Options{Required: false, Help: "DSCP field for QoS",
		Default: constants.DEFAULT_DSCP})
	file := args.String("f", "file", &argparse.Options{Required: true, Help: "File path"})
	frame := args.Flag("j", "jumbo", &argparse.Options{Help: "Enable jumbo frames"})
	pass := args.String("k", "key", &argparse.Options{Required: false, Help: "Encryption key (16 or 32 characters). Enables AES 128 or 256 encryption"})
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

	// Get file info.
	finfo, err := os.Stat(*file)
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

	// Connect to host.
	err = comms.Connect(addr, *dscp)

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

		fileName := strings.TrimSpace(*file)

		// 8MB chunks the limit.
		if *chunk > 8192 {
			*chunk = 8192
		} else if *chunk < 1 {
			*chunk = 1
		}

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

			if status == 0 {
				fmt.Println("Server not ready to receive the file")
				os.Exit(1)
			} else if status == 2 {
				fmt.Println("Server already has identical file. Omitting!")
				os.Exit(0)
			}

			begin := time.Now()

			// Start sending chunks.
			channels := worker.StartWorkers(*workers, crypto)
			comms.StartChunkStream(*frame, channels)

			comp, total, origSize, compSize := worker.GetChunkStats()
			fmt.Println("Sent all data in",
				time.Since(begin), "with", comp, "/", total, "chunks compressed")
			fmt.Println("Original size:", humanReadableSize(origSize), "Compressed size:", humanReadableSize(compSize))

			fmt.Println("Waiting for server to confirm")
			// EOF negotiation with server.
			ack := comms.EndFileTransfer(fileName, hash, method)

			if ack {
				fmt.Println("Server confirmed file has been synced")
			} else {
				fmt.Println("File transfer may not have completed or data may be corrupted")
				os.Exit(2)
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

// humanReadableSize converts file size into a human-readable from.
// FIXME: This probably should be moved somewhere else
func humanReadableSize(size uint32) string {
	const (
		_  = iota
		KB = 1 << (10 * iota) // 1024
		MB                    // 1048576
		GB                    // 1073741824
	)

	switch {
	case size > GB:
		return fmt.Sprintf("%.2f GB", float32(size)/GB)
	case size > MB:
		return fmt.Sprintf("%.2f MB", float32(size)/MB)
	case size > KB:
		return fmt.Sprintf("%.2f kB", float32(size)/KB)
	default:
		return fmt.Sprintf("%d B", size)
	}
}
