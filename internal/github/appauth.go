package github

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"strconv"
	"time"
)

func appJWT(appID int64, keyPEM []byte, now time.Time) (string, error) {
	if appID <= 0 {
		return "", fmt.Errorf("app ID must be positive")
	}
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return "", fmt.Errorf("app key must be an unencrypted RSA PEM key")
	}
	var key *rsa.PrivateKey
	switch block.Type {
	case "RSA PRIVATE KEY":
		key, _ = x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		parsed, _ := x509.ParsePKCS8PrivateKey(block.Bytes)
		key, _ = parsed.(*rsa.PrivateKey)
	}
	if key == nil || key.Validate() != nil || key.N.BitLen() < 2048 {
		return "", fmt.Errorf("app key must be a valid RSA private key of at least 2048 bits")
	}
	claims, err := json.Marshal(struct {
		Iss string `json:"iss"`
		Iat int64  `json:"iat"`
		Exp int64  `json:"exp"`
	}{strconv.FormatInt(appID, 10), now.Unix() - 60, now.Unix() + 540})
	if err != nil {
		return "", fmt.Errorf("could not encode app authentication claims")
	}
	unsigned := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`)) + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(unsigned))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("could not sign app authentication claims")
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}
