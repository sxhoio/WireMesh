package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"

	"golang.org/x/crypto/argon2"
)

type EncryptedSecret struct {
	WrappedDEK, DEKNonce, Ciphertext, DataNonce string
}
type Stored struct {
	Version int             `json:"version"`
	Driver  string          `json:"driver"`
	Secret  EncryptedSecret `json:"secret"`
}

func open(key, ciphertext, nonce []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil { return nil, err }
	gcm, err := cipher.NewGCM(block)
	if err != nil { return nil, err }
	if len(nonce) != gcm.NonceSize() { return nil, fmt.Errorf("bad nonce len") }
	return gcm.Open(nil, nonce, ciphertext, nil)
}

func tryDecrypt(label, masterKey string, s EncryptedSecret) {
	wrapped, e1 := base64.StdEncoding.DecodeString(s.WrappedDEK)
	wrapNonce, e2 := base64.StdEncoding.DecodeString(s.DEKNonce)
	ct, e3 := base64.StdEncoding.DecodeString(s.Ciphertext)
	dtNonce, e4 := base64.StdEncoding.DecodeString(s.DataNonce)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
		fmt.Printf("%s: base64 decode error\n", label); return
	}
	// 尝试 Argon2id 新派生
	salt := []byte("wiremesh-master-key-v1")
	newKey := argon2.IDKey([]byte(masterKey), salt, 1, 64*1024, 1, 32)
	if dek, err := open(newKey, wrapped, wrapNonce); err == nil {
		if plain, err2 := open(dek, ct, dtNonce); err2 == nil {
			fmt.Printf("%s: MATCH (Argon2id) -> %s\n", label, string(plain)); return
		}
	}
	// 尝试旧 SHA-256 派生（legacy 兼容）
	legacy := sha256.Sum256([]byte(masterKey))
	if dek, err := open(legacy[:], wrapped, wrapNonce); err == nil {
		if plain, err2 := open(dek, ct, dtNonce); err2 == nil {
			fmt.Printf("%s: MATCH (legacy SHA-256) -> %s\n", label, string(plain)); return
		}
	}
	fmt.Printf("%s: NO MATCH\n", label)
}

func main() {
	raw, err := os.ReadFile(os.Args[1])
	if err != nil { panic(err) }
	var stored Stored
	if err := json.Unmarshal(raw, &stored); err != nil { panic(err) }
	fmt.Printf("driver=%s version=%d\n", stored.Driver, stored.Version)

	// 候选 key 列表：命令行逐个传入
	for i := 2; i < len(os.Args); i++ {
		tryDecrypt(fmt.Sprintf("key[%d]", i-1), os.Args[i], stored.Secret)
	}
}
