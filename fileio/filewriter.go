package fileio

type FileWriter interface {
	New(filename string, bufferSize, qlen int, sha bool) error
	StartWriting() (chan []byte, chan []byte)
}
