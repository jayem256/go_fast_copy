package main

import (
	"fmt"
	"go_fast_copy/constants"
	server "go_fast_copy/server/controller"
	"os"
	"runtime/debug"
	"strconv"

	"github.com/akamensky/argparse"
)

func main() {
	args := argparse.NewParser("server", constants.Title)

	chunk := args.Int("c", "chunksize", &argparse.Options{Required: false, Help: "File write chunk size in KB",
		Default: constants.DEFAULT_FILE_CHUNK_SIZE})
	pass := args.String("k", "key", &argparse.Options{Required: false, Help: "Encryption key (16 or 32 characters). Enables AES 128 or 256 encryption"})
	bind := args.String("l", "listen", &argparse.Options{Required: false, Help: "Listen on address",
		Default: "0.0.0.0"})
	port := args.Int("p", "port", &argparse.Options{Required: false, Help: "Listening port",
		Default: constants.DEFAULT_PORT})
	queue := args.Int("q", "queue", &argparse.Options{Required: false, Help: "Write queue length",
		Default: constants.FILE_WRITE_QUEUE})
	path := args.String("r", "root", &argparse.Options{Required: true, Help: "Root path for storing files"})
	workers := args.Int("t", "threads", &argparse.Options{Required: false, Help: "Number of decompression (and decryption) threads",
		Default: constants.DEFAULT_NUM_WORKERS})

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

	debug.SetGCPercent(666)

	bindTo := *bind + ":" + strconv.Itoa(*port)

	server.StartListening(*pass, *path, bindTo, *chunk, *workers, *queue)
}
