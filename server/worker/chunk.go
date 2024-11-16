package worker

// decompressedChunk contains chunk sequence number and raw decompressed data
type decompressedChunk struct {
	seq uint32
	raw []byte
}

// UnprocessedChunk could be either compressed or not
type UnprocessedChunk struct {
	Seq        uint32
	Compressed bool
	Data       []byte
}
