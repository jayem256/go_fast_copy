package worker

import "fmt"

// ChunkMuxer takes chunks in any order and reorders them for file writer
type ChunkMuxer struct {
	nextChunkID      uint32
	outOfOrderChunks map[uint32]*decompressedChunk
	maxOOC           int
}

// Start starts new goroutine for processing decompressed chunks in any order
func (c *ChunkMuxer) Start(maxBufferedOOC int, fileout chan []byte, forks int) []chan *decompressedChunk {
	streams := make([]chan *decompressedChunk, forks)
	c.maxOOC = maxBufferedOOC

	for i := 0; i < forks; i++ {
		streams[i] = make(chan *decompressedChunk, 3)
	}

	// Start processing decompressed chunks.
	go func(inStreams []chan *decompressedChunk, out chan []byte) {
		c.nextChunkID = 1
		c.outOfOrderChunks = make(map[uint32]*decompressedChunk)

		for {
			active := 0
			// Reorder incoming chunks and commit them to file writer.
			for _, dc := range inStreams {
				select {
				case chonk, open := <-dc:
					if open {
						active = active + 1
						// Chunk is next expected one in sequence.
						if chonk.seq == c.nextChunkID {
							out <- chonk.raw
							c.nextChunkID = c.nextChunkID + 1
						} else {
							// Received an out-of-order chunk.
							if len(c.outOfOrderChunks) < c.maxOOC {
								c.outOfOrderChunks[chonk.seq] = chonk
							} else {
								fmt.Println("WARNING! Buffer full - dropping out-of-order chunk. " +
									"There WILL BE data corruption!")
							}
						}
						// Check whether buffer contains next chunk before receiving more.
						for {
							next := c.findNext()
							if next == nil {
								break
							}
							out <- next.raw
						}
					}
				default:
					active = active + 1
				}
			}
			if active == 0 {
				break
			}
		}

		// Channel closed - check for remaining chunks.
		if len(c.outOfOrderChunks) > 0 {
			for {
				next := c.findNext()
				if next == nil {
					break
				}
				out <- next.raw
			}
		}

		if len(c.outOfOrderChunks) > 0 {
			fmt.Println("WARNING! Not all chunks received - data corrupted!")
		}

		// Close file I/O channel.
		close(out)
	}(streams, fileout)

	return streams
}

// Check whether next chunk in sequence has been buffered
func (c *ChunkMuxer) findNext() *decompressedChunk {
	chonky := c.outOfOrderChunks[c.nextChunkID]
	if chonky != nil {
		delete(c.outOfOrderChunks, c.nextChunkID)
		c.nextChunkID = c.nextChunkID + 1
		return chonky
	}
	return nil
}
