package networking

import (
	"crypto/aes"
	"crypto/cipher"
)

// Crypto handles AES encryption and decryption
type Crypto struct {
	aes    cipher.Block
	nonce  []byte
	secret []byte
}

// WithKeyNonce takes encryption key and nonce
func (c *Crypto) WithKeyNonce(key, nonce []byte) *Crypto {
	if key == nil || nonce == nil {
		return c
	}
	if len(nonce) < aes.BlockSize {
		return c
	}
	cipha, err := aes.NewCipher(key)
	if err == nil {
		c.aes = cipha
		c.nonce = nonce
		c.secret = key
	}
	return c
}

// MatchSecret returns true if contents of given secret matches the predefined one
func (c *Crypto) MatchSecret(secret []byte) bool {
	if len(secret) != len(c.secret) {
		return false
	}
	for i, sb := range c.secret {
		if secret[i] != sb {
			return false
		}
	}
	return true
}

// Encrypt encrypts or returns original data if no key has been provided
func (c *Crypto) Encrypt(data []byte) []byte {
	if c.aes == nil {
		return data
	}

	// Use CTR-AES.
	ctr := cipher.NewCTR(c.aes, c.nonce)
	dst := make([]byte, len(data))
	ctr.XORKeyStream(dst, data)

	return dst
}

// Decrypt decrypts or returns original data if no key has been provided
func (c *Crypto) Decrypt(data []byte) []byte {
	if c.aes == nil {
		return data
	}

	// Use CTR-AES.
	ctr := cipher.NewCTR(c.aes, c.nonce)
	ctr.XORKeyStream(data, data)

	return data
}
