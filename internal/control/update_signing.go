package control

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"strings"

	"github.com/wiremesh/wiremesh/internal/wireproto"
)

// parseECDSAPrivateKeyPEM 解析 PEM 编码的 ECDSA P-256 私钥（更新清单签名用）。
func parseECDSAPrivateKeyPEM(pemText string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemText))
	if block == nil {
		return nil, errors.New("invalid PEM block")
	}
	key, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	return key, nil
}

// manifestSignedPayload 生成更新清单被签名的规范 JSON（固定字段顺序，
// 服务端与 Agent 端必须一致）。
func manifestSignedPayload(manifest wireproto.AgentUpdateManifest) ([]byte, error) {
	return json.Marshal(struct {
		Version         string `json:"version"`
		OS              string `json:"os"`
		Arch            string `json:"arch"`
		Size            int64  `json:"size"`
		SHA256          string `json:"sha256"`
		MinAgentVersion string `json:"min_agent_version"`
	}{
		Version: manifest.Version, OS: manifest.OS, Arch: manifest.Arch,
		Size: manifest.Size, SHA256: manifest.SHA256, MinAgentVersion: manifest.MinAgentVersion,
	})
}

// signUpdateManifest 用服务端更新签名私钥对清单关键字段签名（base64 raw 64 字节 R||S）。
func signUpdateManifest(key *ecdsa.PrivateKey, manifest wireproto.AgentUpdateManifest) (string, error) {
	payload, err := manifestSignedPayload(manifest)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return base64.StdEncoding.EncodeToString(sig), nil
}

// verifyUpdateManifestSignature 用 Agent 配置的更新公钥验证清单签名。
func verifyUpdateManifestSignature(public *ecdsa.PublicKey, manifest wireproto.AgentUpdateManifest) error {
	if strings.TrimSpace(manifest.Signature) == "" {
		return errors.New("update manifest is missing a signature")
	}
	raw, err := base64.StdEncoding.DecodeString(manifest.Signature)
	if err != nil || len(raw) != 64 {
		return errors.New("update manifest signature is malformed")
	}
	payload, err := manifestSignedPayload(manifest)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(payload)
	r := new(big.Int).SetBytes(raw[:32])
	s := new(big.Int).SetBytes(raw[32:])
	if !ecdsa.Verify(public, digest[:], r, s) {
		return errors.New("update manifest signature verification failed")
	}
	return nil
}
