package fileio

import (
	"crypto/sha256"
	"fmt"
	"hash/crc32"
	"io"
	"os"
)

// GetFileChecksumSHA256 returns SHA256 checksum of given file
func GetFileChecksumSHA256(file string) []byte {
	handle, err := os.Open(file)
	if err != nil {
		fmt.Println(err.Error())
		return make([]byte, 32)
	}
	defer handle.Close()

	hash := sha256.New()
	if _, err := io.CopyBuffer(hash, handle, make([]byte, 64*1024)); err != nil {
		fmt.Println(err.Error())
		return make([]byte, 32)
	}

	return hash.Sum(nil)
}

// GetFileChecksumCRC32 returns CRC32 checksum of given file
func GetFileChecksumCRC32(file string) []byte {
	handle, err := os.Open(file)
	if err != nil {
		fmt.Println(err.Error())
		return make([]byte, 4)
	}
	defer handle.Close()

	hash := crc32.New(crc32.IEEETable)
	if _, err := io.CopyBuffer(hash, handle, make([]byte, 64*1024)); err != nil {
		fmt.Println(err.Error())
		return make([]byte, 4)
	}

	return hash.Sum(nil)
}
