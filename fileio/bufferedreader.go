package fileio

import (
	"bufio"
	"os"
)

// BufferedReader does buffered file reads
type BufferedReader struct {
	file      *os.File
	reader    *bufio.Reader
	chunkSize int
	rqLen     int
}

// New opens file for reading or returns error upon failing to do so
func (b *BufferedReader) New(filename string, chunkSize, numchunks int) error {
	file, err := os.Open(filename)
	if err == nil {
		b.file = file
		b.chunkSize = chunkSize
		b.rqLen = numchunks
		b.reader = bufio.NewReaderSize(b.file, chunkSize)
		return nil
	}
	return err
}

// StartReading starts a goroutine to read file contents in chunks
func (b *BufferedReader) StartReading() chan []byte {
	if b.file == nil {
		panic("cannot start reading without file handle")
	}
	outChan := make(chan []byte, b.rqLen)
	go func(channel chan []byte) {
		for {
			buf := make([]byte, b.chunkSize)
			// Read from file.
			read, _ := b.reader.Read(buf)
			if read > 0 {
				channel <- buf[:read]
			} else {
				// File has been fully consumed.
				break
			}
		}
		close(outChan)
		b.file.Close()
	}(outChan)
	return outChan
}
