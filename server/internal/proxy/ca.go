package proxy

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"time"
)

// CA is the installation's private certificate authority. Servers without a
// public certificate get one issued by it; clients trust the CA once (sing-box
// profiles embed it, devices can install it), so yearly renewals of the
// server certificate go unnoticed.
type CA struct {
	Cert    *x509.Certificate
	Key     *ecdsa.PrivateKey
	CertPEM string
}

const (
	caValidity = 10 * 365 * 24 * time.Hour
	// Apple platforms reject TLS server certificates valid for more than
	// 825 days, even under a user-installed root; stay well below.
	leafValidity = 397 * 24 * time.Hour
	// Renew the server certificate this long before it expires.
	LeafRenewBefore = 30 * 24 * time.Hour
)

// NewCA creates a CA and returns its certificate and key as PEM.
func NewCA() (certPEM, keyPEM string, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	serial, err := randomSerial()
	if err != nil {
		return "", "", err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "Portunus Local CA " + randomHex(6), Organization: []string{"Portunus"}},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(caValidity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return "", "", err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})), nil
}

// ParseCA loads a CA from PEM.
func ParseCA(certPEM, keyPEM string) (*CA, error) {
	cb, _ := pem.Decode([]byte(certPEM))
	kb, _ := pem.Decode([]byte(keyPEM))
	if cb == nil || kb == nil {
		return nil, errors.New("invalid CA PEM")
	}
	cert, err := x509.ParseCertificate(cb.Bytes)
	if err != nil {
		return nil, err
	}
	k, err := x509.ParsePKCS8PrivateKey(kb.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := k.(*ecdsa.PrivateKey)
	if !ok || !cert.IsCA {
		return nil, errors.New("not an ECDSA CA")
	}
	return &CA{Cert: cert, Key: key, CertPEM: certPEM}, nil
}

// Issue creates a server certificate for name signed by the CA. It returns
// PEM certificate, PEM key, the hex SHA-256 of the DER certificate (for
// pinning) and its expiry.
func (ca *CA) Issue(name string) (certPEM, keyPEM, sum string, notAfter time.Time, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", "", time.Time{}, err
	}
	serial, err := randomSerial()
	if err != nil {
		return "", "", "", time.Time{}, err
	}
	now := time.Now()
	notAfter = now.Add(leafValidity)
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	if ip := net.ParseIP(name); ip != nil {
		tmpl.IPAddresses = []net.IP{ip}
	} else {
		tmpl.DNSNames = []string{name}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.Cert, &key.PublicKey, ca.Key)
	if err != nil {
		return "", "", "", time.Time{}, fmt.Errorf("issue certificate: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", "", "", time.Time{}, err
	}
	h := sha256.Sum256(der)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})),
		hex.EncodeToString(h[:]), notAfter, nil
}

// CertExpiry returns the NotAfter of a PEM certificate.
func CertExpiry(certPEM string) (time.Time, error) {
	b, _ := pem.Decode([]byte(certPEM))
	if b == nil {
		return time.Time{}, errors.New("invalid certificate PEM")
	}
	c, err := x509.ParseCertificate(b.Bytes)
	if err != nil {
		return time.Time{}, err
	}
	return c.NotAfter, nil
}

func randomSerial() (*big.Int, error) {
	return rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
}
