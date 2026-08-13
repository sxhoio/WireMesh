package control

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"slices"
	"strings"
	"time"
)

// jsonWebKey / jsonWebKeySet 是 OIDC JWKS 文档的最小表示（仅 RSA 与 EC P-256）。
type jsonWebKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

type jsonWebKeySet struct {
	Keys []jsonWebKey `json:"keys"`
}

// verifyOIDCIDToken 校验 OIDC ID token：签名（经 JWKS）、issuer、audience、过期时间与 nonce。
func verifyOIDCIDToken(ctx context.Context, idToken, jwksURI, issuer, clientID, nonce string) error {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return errors.New("malformed ID token")
	}
	headerRaw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return errors.New("malformed ID token header")
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(headerRaw, &header); err != nil {
		return errors.New("malformed ID token header")
	}
	claimsRaw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return errors.New("malformed ID token claims")
	}
	var claims struct {
		Iss   string          `json:"iss"`
		Aud   json.RawMessage `json:"aud"`
		Exp   int64           `json:"exp"`
		Nonce string          `json:"nonce"`
	}
	if err := json.Unmarshal(claimsRaw, &claims); err != nil {
		return errors.New("malformed ID token claims")
	}
	if claims.Iss != issuer {
		return errors.New("ID token issuer mismatch")
	}
	audiences, err := parseAudiences(claims.Aud)
	if err != nil {
		return errors.New("malformed ID token audience")
	}
	if !slices.Contains(audiences, clientID) {
		return errors.New("ID token audience mismatch")
	}
	if claims.Exp <= time.Now().Unix() {
		return errors.New("ID token expired")
	}
	if claims.Nonce != nonce {
		return errors.New("ID token nonce mismatch")
	}
	key, err := fetchOIDCSigningKey(ctx, jwksURI, header.Kid, header.Alg)
	if err != nil {
		return err
	}
	signingInput := parts[0] + "." + parts[1]
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return errors.New("malformed ID token signature")
	}
	if err := verifyJWSSignature(key, header.Alg, signingInput, signature); err != nil {
		return fmt.Errorf("ID token signature: %w", err)
	}
	return nil
}

func parseAudiences(raw json.RawMessage) ([]string, error) {
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		return list, nil
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		return []string{single}, nil
	}
	return nil, errors.New("invalid audience")
}

// fetchOIDCSigningKey 从 JWKS 文档中按 kid/alg 选择签名公钥（单个签名密钥时容忍缺 kid）。
func fetchOIDCSigningKey(ctx context.Context, jwksURI, kid, alg string) (any, error) {
	if strings.TrimSpace(jwksURI) == "" {
		return nil, errors.New("OIDC discovery is missing jwks_uri")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURI, nil)
	if err != nil {
		return nil, err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jwks endpoint returned %s", response.Status)
	}
	var set jsonWebKeySet
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&set); err != nil {
		return nil, err
	}
	var candidates []jsonWebKey
	for _, key := range set.Keys {
		if key.Use != "" && key.Use != "sig" {
			continue
		}
		if kid != "" && key.Kid != kid {
			continue
		}
		candidates = append(candidates, key)
	}
	if len(candidates) == 0 && kid != "" {
		// 部分 IdP 的 kid 变化较快，退回按 alg 匹配
		for _, key := range set.Keys {
			if key.Alg == alg {
				candidates = append(candidates, key)
			}
		}
	}
	if len(candidates) == 0 {
		return nil, errors.New("no matching signing key in JWKS")
	}
	for _, key := range candidates {
		public, err := publicKeyFromJWK(key)
		if err == nil {
			return public, nil
		}
	}
	return nil, errors.New("unsupported JWK signing key")
}

func publicKeyFromJWK(key jsonWebKey) (any, error) {
	switch key.Kty {
	case "RSA":
		nBytes, err := base64.RawURLEncoding.DecodeString(key.N)
		if err != nil {
			return nil, err
		}
		eBytes, err := base64.RawURLEncoding.DecodeString(key.E)
		if err != nil {
			return nil, err
		}
		e := 0
		for _, b := range eBytes {
			e = e<<8 | int(b)
		}
		return &rsa.PublicKey{N: new(big.Int).SetBytes(nBytes), E: e}, nil
	case "EC":
		if key.Crv != "P-256" {
			return nil, errors.New("unsupported EC curve")
		}
		xBytes, err := base64.RawURLEncoding.DecodeString(key.X)
		if err != nil {
			return nil, err
		}
		yBytes, err := base64.RawURLEncoding.DecodeString(key.Y)
		if err != nil {
			return nil, err
		}
		return &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(xBytes), Y: new(big.Int).SetBytes(yBytes)}, nil
	default:
		return nil, errors.New("unsupported key type")
	}
}

func verifyJWSSignature(key any, alg, signingInput string, signature []byte) error {
	switch public := key.(type) {
	case *rsa.PublicKey:
		if alg != "RS256" && alg != "RS384" && alg != "RS512" {
			return errors.New("unexpected RSA algorithm")
		}
		var hash crypto.Hash
		var digest []byte
		switch alg {
		case "RS384":
			hash = crypto.SHA384
			sum := sha512.Sum384([]byte(signingInput))
			digest = sum[:]
		case "RS512":
			hash = crypto.SHA512
			sum := sha512.Sum512([]byte(signingInput))
			digest = sum[:]
		default:
			hash = crypto.SHA256
			sum := sha256.Sum256([]byte(signingInput))
			digest = sum[:]
		}
		return rsa.VerifyPKCS1v15(public, hash, digest, signature)
	case *ecdsa.PublicKey:
		if alg != "ES256" {
			return errors.New("unexpected EC algorithm")
		}
		if len(signature) != 64 {
			return errors.New("malformed ES256 signature")
		}
		r := new(big.Int).SetBytes(signature[:32])
		s := new(big.Int).SetBytes(signature[32:])
		sum := sha256.Sum256([]byte(signingInput))
		if !ecdsa.Verify(public, sum[:], r, s) {
			return errors.New("signature verification failed")
		}
		return nil
	default:
		return errors.New("unsupported signing key")
	}
}
