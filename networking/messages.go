package networking

import (
	"bytes"
	"encoding/binary"
	"errors"
)

// Header contains static message parts
type Header struct {
	Opcode uint8
	Flags  uint8
	Len    uint16
	// Followed by Len * bytes payload.
}

// Packet contains Header + payload
type Packet struct {
	Header
	Payload []byte
}

// DecodeHeader decodes slice of bytes to Header
func DecodeHeader(message []byte) (*Header, error) {
	if len(message) != 4 {
		return nil, errors.New("header length should always be 4 bytes")
	}

	header := new(Header)
	buffer := bytes.NewBuffer(message[:4])
	err := binary.Read(buffer, binary.LittleEndian, header)

	return header, err
}

// PacketToBytes encodes packet to slice of bytes
func PacketToBytes(packet *Packet) ([]byte, error) {
	if len(packet.Payload) > 65503 {
		return nil, errors.New("payload size greater than 65503 not allowed")
	}

	if packet.Payload == nil {
		packet.Payload = make([]byte, 0)
	}

	packet.Len = uint16(len(packet.Payload) + 4)

	buffer := bytes.NewBuffer(make([]byte, 0, 4))
	err := binary.Write(buffer, binary.LittleEndian, packet.Header)

	return append(buffer.Bytes(), packet.Payload...), err
}

// PayloadToBytes encodes data intented as payload to slice of bytes
func PayloadToBytes(payload interface{}, encryption *Crypto) []byte {
	buffer := bytes.NewBuffer(make([]byte, 0, binary.Size(payload)))
	binary.Write(buffer, binary.LittleEndian, payload)
	bytes := buffer.Bytes()
	if encryption != nil {
		bytes = encryption.Encrypt(bytes)
	}
	return bytes
}

// DecodePayload decodes slice of bytes to given structure
func DecodePayload(payload []byte, dst interface{}, encryption *Crypto) error {
	if encryption != nil {
		payload = encryption.Decrypt(payload)
	}
	buffer := bytes.NewBuffer(payload)
	err := binary.Read(buffer, binary.LittleEndian, dst)
	return err
}
