package worker

import (
	"go_fast_copy/fileio"
	"go_fast_copy/networking"
	"go_fast_copy/networking/opcode"
	"sync/atomic"
)

var file *fileio.FileBuffer
var compressedChunks atomic.Uint32
var chunksTotal atomic.Uint32

type uncompressedChunk struct {
	seq  uint32
	data []byte
}

// StartFileReader opens new file handle for reading
func StartFileReader(filename string, numworkers, chunksize int) error {
	compressedChunks.Store(0)
	chunksTotal.Store(0)
	file = new(fileio.FileBuffer)
	return file.NewReader(filename, chunksize*1024, numworkers)
}

// GetChunkStats returns compressed:total chunk count so far
func GetChunkStats() (int, int) {
	comp := compressedChunks.Load()
	total := chunksTotal.Load()

	return int(comp), int(total)
}

// StartWorkers starts workers for compressing raw chunks from file
func StartWorkers(numworkers int, crypto *networking.Crypto) []chan []byte {
	chunkStream := make(chan *uncompressedChunk, numworkers)

	channels := make([]chan []byte, numworkers)

	// Start workers.
	for i := 0; i < numworkers; i++ {
		out := make(chan []byte)
		channels[i] = out

		go func(in chan *uncompressedChunk, out chan []byte) {
			for chunk := range in {
				var isCompressed uint16

				// Compress chunk if possible.
				processed, compressed := fileio.CompressChunk(chunk.data)
				processed = crypto.Encrypt(processed)

				if compressed {
					compressedChunks.Add(1)
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

		fileChunks := file.StartReading()

		// Get raw chunks from file reader.
		for raw := range fileChunks {
			// Send to workers for processing.
			chunkStream <- &uncompressedChunk{
				seq:  chunkSeq,
				data: raw,
			}
			// Increment sequence number.
			chunkSeq = chunkSeq + 1
			chunksTotal.Add(1)
		}

		close(chunkStream)
	}()

	return channels
}
