package opcode

const (
	EHLO              = iota // 0: Server greeting
	HANDSHAKE                // 1: New session
	BEGINFILETRANSFER        // 2: Request file transfer
	NEXTCHUNK                // 3: Next chunk of file data
	ENDFILETRANSFER          // 4: EOF
)
