package fileio

type IOFactory interface {
	NewReader() FileReader
	NewWriter() FileWriter
}

// BufferedFactory is the default factory returning buffered reader/writer instances
type BufferedFactory struct{}

func (b *BufferedFactory) NewReader() FileReader {
	return new(BufferedReader)
}

func (b *BufferedFactory) NewWriter() FileWriter {
	return new(BufferedWriter)
}
