package fileio

type FileReader interface {
	New(filename string, chunkSize, numchunks int) error
	StartReading() chan []byte
}
