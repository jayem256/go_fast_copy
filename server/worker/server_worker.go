package worker

import (
	"go_fast_copy/constants"
	"go_fast_copy/fileio"
	"go_fast_copy/networking"
)

// ChunkProcessor is responsible for starting workers and passing work
type ChunkProcessor struct {
	file        *fileio.FileBuffer
	forks       []chan *UnprocessedChunk
	next        int
	mux         *ChunkMuxer
	fioComplete chan string
}

// NewFile prepares file writer
func (s *ChunkProcessor) NewFile(filename string, bufferSize, qlen int) {
	s.file = new(fileio.FileBuffer)
	err := s.file.NewWriter(filename, bufferSize, qlen)
	s.mux = new(ChunkMuxer)
	if err != nil {
		panic(err)
	}
}

// StartForks starts workers for processing chunks
func (s *ChunkProcessor) StartForks(forkCount int, crypto *networking.Crypto) {
	chunkProcessingQueues := make([]chan *UnprocessedChunk, 0, forkCount)
	// Start file writing.
	outChan, fioc := s.file.StartWriting()
	s.fioComplete = fioc
	// Start chunk muxer.
	dcStreams := s.mux.Start(constants.MAX_OOC, outChan, forkCount)

	// Start all workers.
	for i := 0; i < forkCount; i++ {
		workerChunkProcQ := make(chan *UnprocessedChunk, 2)
		chunkProcessingQueues = append(chunkProcessingQueues, workerChunkProcQ)
		decompChannel := dcStreams[i]

		go func(in chan *UnprocessedChunk, out chan *decompressedChunk, crypto *networking.Crypto) {
			for {
				com, open := <-in
				if com == nil || !open {
					break
				}

				// Decrypt the chunk first if encrypted.
				com.Data = crypto.Decrypt(com.Data)

				// Decompress if compressed.
				if com.Compressed {
					out <- &decompressedChunk{
						seq: com.Seq,
						raw: fileio.DecompressChunk(com.Data),
					}
				} else {
					// Chunk was not compressed so no action required.
					out <- &decompressedChunk{
						seq: com.Seq,
						raw: com.Data,
					}
				}
			}
			close(decompChannel)
		}(workerChunkProcQ, decompChannel, crypto)
	}

	s.forks = chunkProcessingQueues
}

// ProcessNextChunk passes chunk to next worker
func (s *ChunkProcessor) ProcessNextChunk(chunk *UnprocessedChunk) {
	s.forks[s.next] <- chunk
	s.next = (s.next + 1) % len(s.forks)
}

// Stop ends all forks
func (s *ChunkProcessor) Stop() string {
	for _, fork := range s.forks {
		close(fork)
	}
	// Wait for all data to be persisted.
	return <-s.fioComplete
}