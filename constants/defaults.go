package constants

const (
	DEFAULT_FILE_CHUNK_SIZE = 256  // 256K reads and writes
	MIN_CLIENT_CHUNK_SIZE   = 64   // Client minimum chunk size
	MAX_CLIENT_CHUNK_SIZE   = 8192 // Client max chunk size
	DEFAULT_PORT            = 6969 // Nice
	DEFAULT_NUM_WORKERS     = 2    // LZ4 worker threads
	FILE_WRITE_QUEUE        = 10   // Queued chunks before blocking on file writes
	DEFAULT_DSCP            = 0x0A // QoS for high throughput
	MAX_OOC                 = 256  // Maximum number of buffered out-of-order chunks
)
