package fileio

import (
	"go_fast_copy/constants"

	"github.com/pierrec/lz4/v4"
)

// CompressChunk attempts to compress a chunk in LZ4 and either returns original or compressed chunk
func CompressChunk(chunk []byte) ([]byte, bool) {
	// Attempt to compress.
	compressedSize, compressed := compress(chunk)

	if compressedSize == 0 || compressedSize >= len(chunk) {
		// Chunk was not compressible.
		return chunk, false
	} else {
		// Chunk was compressed.
		return compressed[:compressedSize], true
	}
}

// DecompressChunk returns uncompressed data of given chunk
func DecompressChunk(chunk []byte) []byte {
	return uncompress(chunk)
}

// uncompress uncompresses chunk and returns resulting slice of uncompressed bytes
func uncompress(block []byte) []byte {
	buffer := make([]byte, constants.MAX_CLIENT_CHUNK_SIZE*1024)
	actual, err := lz4.UncompressBlock(block, buffer)
	if err != nil {
		panic("protocol error: client sent data which uncompressed exceeds the maximum allowed size")
	}
	return buffer[:actual]
}

// compress compresses chunk and returns resulting chunk and # of bytes compressed if any
func compress(block []byte) (int, []byte) {
	buffer := make([]byte, lz4.CompressBlockBound(len(block)))
	var c lz4.Compressor
	compressed, err := c.CompressBlock(block, buffer)
	if err != nil {
		return 0, block
	}
	return compressed, buffer
}
