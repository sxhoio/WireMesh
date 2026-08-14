package control

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"

	"golang.org/x/crypto/argon2"
)

// EncryptedSecret uses a random data-encryption key wrapped by the app master key.
// Production deployments must source the master key from a KMS or HSM integration.
type EncryptedSecret struct {
	WrappedDEK, DEKNonce, Ciphertext, DataNonce string
}

// SecretBox 用主密钥派生的 AES-256-GCM 密钥包装数据加密密钥（DEK）。
// 主密钥先经 Argon2id KDF 派生出加密密钥（一次性成本，进程内缓存），
// 再用于包装，防止低熵主密钥被直接暴力破解（S14：master key 无 KDF）。
// legacy 保留旧的 SHA-256 派生密钥：KDF 升级前加密的历史数据仍可解密。
type SecretBox struct {
	key    []byte
	legacy []byte
}

func NewSecretBox(masterKey string) (*SecretBox, error) {
	if masterKey == "" {
		return nil, errors.New("master key is required: set WIREMESH_MASTER_KEY to a long random secret")
	}
	// 固定域分离盐 + Argon2id：主密钥是高熵随机值时 KDF 是纵深防御；
	// 盐无需随实例变化（无持久化负担），成本与随机盐一致。
	salt := []byte("wiremesh-master-key-v1")
	key := argon2.IDKey([]byte(masterKey), salt, 1, 64*1024, 1, 32)
	legacy := sha256.Sum256([]byte(masterKey))
	return &SecretBox{key: key, legacy: legacy[:]}, nil
}

func (b *SecretBox) Encrypt(plaintext []byte) (EncryptedSecret, error) {
	dek := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return EncryptedSecret{}, err
	}
	wrapped, wrapNonce, err := seal(b.key, dek)
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
	// 优先用 KDF 派生密钥解密；失败时回退到旧 SHA-256 派生密钥，
	// 兼容 KDF 升级前加密的历史数据（CA、数据库配置、节点私钥等）。
	if data, err := b.decryptWith(b.key, secret); err == nil {
		return data, nil
	}
	return b.decryptWith(b.legacy, secret)
}

func (b *SecretBox) decryptWith(key []byte, secret EncryptedSecret) ([]byte, error) {
	wrapped, err := base64.StdEncoding.DecodeString(secret.WrappedDEK)
	if err != nil {
		return nil, err
	}
	wrapNonce, err := base64.StdEncoding.DecodeString(secret.DEKNonce)
	if err != nil {
		return nil, err
	}
	dek, err := open(key, wrapped, wrapNonce)
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
