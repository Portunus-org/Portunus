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
		SerialNumber: serial,
		// Every server certificate carries this name as its issuer, and a
		// server shows its certificate to whoever connects: the name must
		// not tell a stranger what software runs here.
		Subject:               pkix.Name{CommonName: "Local CA " + randomHex(6)},
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

// NodeCertificate creates the certificate a node's agent serves its control
// channel with. The control port answers anyone, and a server shows its
// certificate before it learns who is calling, so the certificate must say
// nothing about what runs here: it is signed by its own key, names only the
// address, and the panel recognises it by its exact bytes (it made it), not
// by who issued it. It lives as long as the CA: replacing it means joining
// the node again.
func NodeCertificate(address string) (certPEM, keyPEM string, err error) {
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
		Subject:               pkix.Name{CommonName: address},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(caValidity),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	if ip := net.ParseIP(address); ip != nil {
		tmpl.IPAddresses = []net.IP{ip}
	} else {
		tmpl.DNSNames = []string{address}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return "", "", fmt.Errorf("make node certificate: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})), nil
}

// SelfIssued reports whether a PEM certificate is signed by its own key,
// as NodeCertificate makes them; false for one issued by a CA.
func SelfIssued(certPEM string) bool {
	b, _ := pem.Decode([]byte(certPEM))
	if b == nil {
		return false
	}
	c, err := x509.ParseCertificate(b.Bytes)
	return err == nil && c.CheckSignature(c.SignatureAlgorithm, c.RawTBSCertificate, c.Signature) == nil
}

// PanelCommonName is the subject of the certificate the panel presents to
// its nodes.
const PanelCommonName = "portunus-panel"

// IssueClient creates the certificate the panel authenticates with when it
// talks to its nodes. Unlike server certificates it is good for client
// authentication only, so a node (which holds server keys issued by the
// same CA) cannot pose as the panel to another node.
func (ca *CA) IssueClient() (certPEM, keyPEM string, err error) {
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
		Subject:               pkix.Name{CommonName: PanelCommonName},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(caValidity),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.Cert, &key.PublicKey, ca.Key)
	if err != nil {
		return "", "", fmt.Errorf("issue client certificate: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})), nil
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
