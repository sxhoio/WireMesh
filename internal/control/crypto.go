package control

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
)

// EncryptedSecret uses a random data-encryption key wrapped by the app master key.
// Production deployments must source the master key from a KMS or HSM integration.
type EncryptedSecret struct {
	WrappedDEK, DEKNonce, Ciphertext, DataNonce string
}

type SecretBox struct{ masterKey []byte }

func NewSecretBox(masterKey string) (*SecretBox, error) {
	if masterKey == "" {
		return nil, errors.New("master key is required: set WIREMESH_MASTER_KEY to a long random secret")
	}
	sum := sha256.Sum256([]byte(masterKey))
	return &SecretBox{masterKey: sum[:]}, nil
}

func (b *SecretBox) Encrypt(plaintext []byte) (EncryptedSecret, error) {
	dek := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return EncryptedSecret{}, err
	}
	wrapped, wrapNonce, err := seal(b.masterKey, dek)
	if err != nil {
		return EncryptedSecret{}, err
	}
	ciphertext, dataNonce, err := seal(dek, plaintext)
	if err != nil {
		return EncryptedSecret{}, err
	}
	return EncryptedSecret{base64.StdEncoding.EncodeToString(wrapped), base64.StdEncoding.EncodeToString(wrapNonce), base64.StdEncoding.EncodeToString(ciphertext), base64.StdEncoding.EncodeToString(dataNonce)}, nil
}

func (b *SecretBox) Decrypt(secret EncryptedSecret) ([]byte, error) {
	wrapped, err := base64.StdEncoding.DecodeString(secret.WrappedDEK)
	if err != nil {
		return nil, err
	}
	wrapNonce, err := base64.StdEncoding.DecodeString(secret.DEKNonce)
	if err != nil {
		return nil, err
	}
	dek, err := open(b.masterKey, wrapped, wrapNonce)
	if err != nil {
		return nil, err
	}
	ciphertext, err := base64.StdEncoding.DecodeString(secret.Ciphertext)
	if err != nil {
		return nil, err
	}
	dataNonce, err := base64.StdEncoding.DecodeString(secret.DataNonce)
	if err != nil {
		return nil, err
	}
	return open(dek, ciphertext, dataNonce)
}

func seal(key, plaintext []byte) ([]byte, []byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}
	return gcm.Seal(nil, nonce, plaintext, nil), nonce, nil
}

func open(key, ciphertext, nonce []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, errors.New("invalid encryption nonce")
	}
	return gcm.Open(nil, nonce, ciphertext, nil)
}
