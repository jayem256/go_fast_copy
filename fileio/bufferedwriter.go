package fileio

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"hash"
	"os"
)

// BufferedWriter does buffered write to file
type BufferedWriter struct {
	file       *os.File
	writer     *bufio.Writer
	wqLen      int
	crc32Hash  uint32
	sha256Hash hash.Hash
}

// New creates new file for writing or returns error upon failing to do so
func (b *BufferedWriter) New(filename string, bufferSize, qlen int, sha bool) error {
	file, err := os.Create(filename)
	if err == nil {
		if sha {
			b.sha256Hash = sha256.New()
		}
		b.file = file
		// New buffered writer.
		b.writer = bufio.NewWriterSize(b.file, bufferSize)
		b.wqLen = qlen
		return nil
	}
	return err
}

// StartWriting starts goroutine for writing chunks of data to file
func (b *BufferedWriter) StartWriting() (chan []byte, chan []byte) {
	if b.file == nil {
		panic("cannot start writing without file handle")
	}
	hash := make(chan []byte)
	// Make write queue.
	stream := make(chan []byte, b.wqLen)
	// Start consuming queue in goroutine.
	go func(chunkStream chan []byte, result chan []byte) {
		for chunk := range chunkStream {
			// Write to file.
			b.writer.Write(chunk)

			// Update hash.
			if b.sha256Hash != nil {
				progressiveChecksumSHA256(b.sha256Hash, chunk)
			} else {
				b.crc32Hash = progressiveChecksumCRC32(b.crc32Hash, chunk)
			}
		}

		// Write any remaining bytes.
		b.writer.Flush()
		b.file.Close()

		var bytes []byte

		// Get SHA256 or CRC32 checksum for all data written so far.
		if b.sha256Hash != nil {
			bytes = b.sha256Hash.Sum(nil)
		} else {
			bytes = binary.BigEndian.AppendUint32(make([]byte, 0, 4), b.crc32Hash)
		}

		// Signal that all data has been written.
		hash <- bytes
		close(hash)
	}(stream, hash)
	return stream, hash
}
