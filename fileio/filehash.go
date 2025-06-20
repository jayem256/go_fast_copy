package fileio

import (
	"crypto/sha256"
	"fmt"
	"hash"
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

// progressiveChecksumSHA256 incrementally calculates SHA256 checksum
func progressiveChecksumSHA256(shaHash hash.Hash, data []byte) hash.Hash {
	if shaHash == nil {
		shaHash = sha256.New()
	}
	if len(data) > 0 {
		shaHash.Write(data)
	}
	return shaHash
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

// progressiveChecksumCRC32 incrementally calculates CRC32 checksum
func progressiveChecksumCRC32(hash uint32, data []byte) uint32 {
	return crc32.Update(hash, crc32.IEEETable, data)
}
