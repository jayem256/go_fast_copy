package networking

// EHLO is server greeting message of opcode 0 with (optional) nonce
type EHLO struct {
	Nonce [16]byte // Nonce for AES-128/256
}

// AuthBlock is optional payload of opcode 1 request
type AuthBlock struct {
	BlockLen uint16 // Auth block length
	// Followed by len * byte payload.
}

// StartFileTransfer is payload of opcode 2 request
type StartFileTransfer struct {
	FileName [128]byte // File name
	FileHash [32]byte  // CRC32/SHA256 checksum
}

// DataStreamChunk opcode 3 describes an individual chunk in TCP stream
type DataStreamChunk struct {
	Sequence    uint32 // Sequence number of the chunk (starts from 1)
	Compression uint16 // Is the chunk compressed
	DataLength  uint32 // Chunk len
	// Followed by len * byte payload.
}

// EndFileTransfer opcode 4 contains file checksum for comparison
type EndFileTransfer struct {
	FileName [128]byte // File name
	Checksum [32]byte  // CRC32/SHA256 checksum
}
