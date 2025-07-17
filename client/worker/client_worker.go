package worker

import (
	"fmt"
	"go_fast_copy/fileio"
	"go_fast_copy/networking"
	"go_fast_copy/networking/opcode"
	"sync/atomic"
)

type CompressingReader struct {
	reader           fileio.FileReader
	compressedChunks atomic.Uint32
	chunksTotal      atomic.Uint32
	dataTotal        atomic.Uint64
	compressedData   atomic.Uint64
}

type uncompressedChunk struct {
	seq  uint32
	data []byte
}

// StartFileReader opens new file handle for reading
func (w *CompressingReader) StartFileReader(factory fileio.IOFactory,
	filename string, numworkers, chunksize int) error {
	w.compressedChunks.Store(0)
	w.chunksTotal.Store(0)
	w.dataTotal.Store(0)
	w.compressedData.Store(0)
	w.reader = factory.NewReader()
	return w.reader.New(filename, chunksize*1024, numworkers)
}

// GetChunkStats returns compressed:total chunk count so far and data:compressedData
func (w *CompressingReader) GetChunkStats() (int, int, string) {
	comp := w.compressedChunks.Load()
	total := w.chunksTotal.Load()

	data := w.dataTotal.Load()
	compData := w.compressedData.Load()

	compStats := "Original size: " + humanReadableSize(data) + " Compressed size: " + humanReadableSize(compData)

	return int(comp), int(total), compStats
}

// humanReadableSize converts file size into a human-readable form.
func humanReadableSize(size uint64) string {
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

// StartWorkers starts workers for compressing raw chunks from file
func (w *CompressingReader) StartWorkers(numworkers int, crypto *networking.Crypto) []chan []byte {
	chunkStream := make(chan *uncompressedChunk, numworkers)

	channels := make([]chan []byte, numworkers)

	// Start workers.
	for i := 0; i < numworkers; i++ {
		out := make(chan []byte, 3)
		channels[i] = out

		go func(in chan *uncompressedChunk, out chan []byte) {
			for chunk := range in {
				var isCompressed uint16

				w.dataTotal.Add(uint64(len(chunk.data)))
				// Compress chunk if possible.
				processed, compressed := fileio.CompressChunk(chunk.data)
				w.compressedData.Add(uint64(len(processed)))
				processed = crypto.Encrypt(processed)

				if compressed {
					w.compressedChunks.Add(1)
					isCompressed = 1
				}
				// Prepare full message of chunk header + data for streaming over TCP.
				nextChunk := networking.Packet{
					Header: networking.Header{
						Opcode: opcode.NEXTCHUNK,
						Flags:  0,
					},
				}
				nextChunk.Payload = networking.PayloadToBytes(
					&networking.DataStreamChunk{
						Sequence:    chunk.seq,
						Compression: isCompressed,
						DataLength:  (uint32)(len(processed)),
					}, crypto)
				msg, _ := networking.PacketToBytes(&nextChunk)
				// Pass message header followed with full chunk to be sent.
				out <- append(msg, processed...)
			}
			close(out)
		}(chunkStream, out)
	}

	// Goroutine for passing raw data from file to workers.
	go func() {
		var chunkSeq uint32 = 1

		fileChunks := w.reader.StartReading()

		// Get raw chunks from file reader.
		for raw := range fileChunks {
			// Send to workers for processing.
			chunkStream <- &uncompressedChunk{
				seq:  chunkSeq,
				data: raw,
			}
			// Increment sequence number.
			chunkSeq = chunkSeq + 1
			w.chunksTotal.Add(1)
		}

		close(chunkStream)
	}()

	return channels
}
