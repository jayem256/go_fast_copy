package fileio

import (
	"bufio"
	"os"
)

// FileBuffer provides tools for buffered I/O
type FileBuffer struct {
	file      *os.File
	writer    *bufio.Writer
	chunkSize int
	rqLen     int
	wqLen     int
}

// NewReader initializes new file reader
func (l *FileBuffer) NewReader(filename string, chunkSize, numchunks int) error {
	file, err := os.Open(filename)
	if err == nil {
		l.file = file
		l.chunkSize = chunkSize
		l.rqLen = numchunks
		return nil
	}
	return err
}

// NewWriter initializes new file writer
func (l *FileBuffer) NewWriter(filename string, bufferSize, qlen int) error {
	file, err := os.Create(filename)
	if err == nil {
		l.file = file
		// New buffered writer.
		l.writer = bufio.NewWriterSize(l.file, bufferSize)
		l.wqLen = qlen
		return nil
	}
	return err
}

// StartWriting starts new goroutine for buffered writes to file
func (l *FileBuffer) StartWriting() (chan []byte, chan string) {
	if l.file == nil {
		panic("cannot start writing without file handle")
	}
	completed := make(chan string)
	// Make write queue.
	stream := make(chan []byte, l.wqLen)
	// Start consuming queue in goroutine.
	go func(chunkStream chan []byte, complete chan string) {
		name := l.file.Name()
		for chunk := range chunkStream {
			// Write to file.
			l.writer.Write(chunk)
		}
		// Write any remaining bytes.
		l.writer.Flush()
		l.file.Close()
		// Signal that all data has been written.
		complete <- name
		close(completed)
	}(stream, completed)
	return stream, completed
}

// StartReading starts new goroutine for buffered reads from file
func (l *FileBuffer) StartReading() chan []byte {
	if l.file == nil {
		panic("cannot start reading without file handle")
	}
	outChan := make(chan []byte, l.rqLen)
	go func(channel chan []byte) {
		for {
			plain := make([]byte, l.chunkSize)
			// Read from file.
			read, _ := l.file.Read(plain)
			if read > 0 {
				plain = plain[:read]
				channel <- plain
			} else {
				// File has been fully consumed.
				break
			}
		}
		close(outChan)
		l.file.Close()
	}(outChan)
	return outChan
}
