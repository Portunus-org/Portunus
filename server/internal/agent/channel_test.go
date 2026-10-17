package agent

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Portunus-org/Portunus/server/internal/proxy"
)

func newCA(t *testing.T) *proxy.CA {
	t.Helper()
	certPEM, keyPEM, err := proxy.NewCA()
	if err != nil {
		t.Fatal(err)
	}
	ca, err := proxy.ParseCA(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	return ca
}

// A node's control port answers on the open internet, so the only thing
// between a stranger and "replace this server's configuration" is the TLS
// layer. Everyone who is not the panel of this installation is turned away
// before a single request is read: a caller with no certificate, another
// node of the same installation, a certificate that merely calls itself the
// panel, the panel of another installation, and an old protocol version.
func TestOnlyThePanelCommandsANode(t *testing.T) {
	ca, other := newCA(t), newCA(t)
	nodeCert, nodeKey, err := proxy.NodeCertificate("127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Bundle{CA: ca.CertPEM, Cert: nodeCert, Key: nodeKey}.TLSConfig()
	if err != nil {
		t.Fatal(err)
	}
	reached := 0
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached++
		writeJSON(w, http.StatusOK, Status{Version: "test"})
	}))
	srv.TLS = cfg
	srv.StartTLS()
	defer srv.Close()
	host, portText, _ := net.SplitHostPort(srv.Listener.Addr().String())
	port, _ := strconv.Atoi(portText)

	// The panel itself is let in.
	panelCert, panelKey, err := ca.IssueClient()
	if err != nil {
		t.Fatal(err)
	}
	cl, err := NewClient(host, port, []string{nodeCert}, panelCert, panelKey)
	if err != nil {
		t.Fatal(err)
	}
	if st, err := cl.Status(context.Background()); err != nil || st.Version != "test" {
		t.Fatalf("the panel was not let in: %v", err)
	}
	cl.Close()
	if reached != 1 {
		t.Fatalf("the panel's request reached the node %d times", reached)
	}

	pair := func(certPEM, keyPEM string) []tls.Certificate {
		c, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
		if err != nil {
			t.Fatal(err)
		}
		return []tls.Certificate{c}
	}
	// Another node of the same installation: a real certificate of the same
	// CA, but one for serving, not for commanding.
	siblingCert, siblingKey, _ := proxy.NodeCertificate("203.0.113.7")
	// A server certificate of the installation's CA, as connections have.
	serverCert, serverKey, _, _, _ := ca.Issue("203.0.113.7")
	// A server certificate that was issued under the panel's own name.
	namedCert, namedKey, _, _, _ := ca.Issue(proxy.PanelCommonName)
	// The panel of another installation.
	foreignCert, foreignKey, _ := other.IssueClient()

	strangers := map[string]*tls.Config{
		"no certificate":                    {},
		"another node of the installation":  {Certificates: pair(siblingCert, siblingKey)},
		"a server certificate of the CA":    {Certificates: pair(serverCert, serverKey)},
		"a server certificate named panel":  {Certificates: pair(namedCert, namedKey)},
		"a self-signed certificate":         {Certificates: selfSigned(t, proxy.PanelCommonName)},
		"the panel of another installation": {Certificates: pair(foreignCert, foreignKey)},
		"the panel, but over TLS 1.2":       {Certificates: pair(panelCert, panelKey), MaxVersion: tls.VersionTLS12},
	}
	for who, conf := range strangers {
		conf.InsecureSkipVerify = true // the stranger does not care whom it reached
		client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: conf}}
		res, err := client.Get("https://" + srv.Listener.Addr().String() + "/v1/status")
		if err == nil {
			io.Copy(io.Discard, res.Body)
			res.Body.Close()
			t.Errorf("%s: answered with %s", who, res.Status)
		}
		client.CloseIdleConnections()
	}
	// Without TLS at all.
	if res, err := http.Get("http://" + srv.Listener.Addr().String() + "/v1/status"); err == nil {
		res.Body.Close()
		if res.StatusCode == http.StatusOK {
			t.Error("plain HTTP: answered")
		}
	}
	if reached != 1 {
		t.Fatalf("%d requests from strangers reached the node", reached-1)
	}
}

// The panel sends a node its whole configuration, keys included, so it
// must be sure whom it talks to. It made the node's certificate itself and
// accepts that very one: a server in the path that answers with anything
// else gets nothing, even with a certificate of this installation's own CA
// for the right address.
func TestThePanelTalksOnlyToItsNode(t *testing.T) {
	ca, other := newCA(t), newCA(t)
	panelCert, panelKey, _ := ca.IssueClient()
	nodeCert, _, _ := proxy.NodeCertificate("127.0.0.1")
	impostors := map[string][2]string{}
	c, k, _ := proxy.NodeCertificate("127.0.0.1")
	impostors["a look-alike made for the same address"] = [2]string{c, k}
	c, k, _, _, _ = other.Issue("127.0.0.1")
	impostors["a certificate from another authority"] = [2]string{c, k}
	c, k, _, _, _ = ca.Issue("127.0.0.1")
	impostors["a certificate of this installation for the same address"] = [2]string{c, k}

	for who, pem := range impostors {
		cert, err := tls.X509KeyPair([]byte(pem[0]), []byte(pem[1]))
		if err != nil {
			t.Fatal(err)
		}
		reached, shownPanel := false, false
		srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reached = true
			writeJSON(w, http.StatusOK, Status{})
		}))
		srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, ClientAuth: tls.RequestClientCert,
			VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
				shownPanel = shownPanel || len(raw) > 0
				return nil
			}}
		srv.Config.ErrorLog = log.New(io.Discard, "", 0)
		srv.StartTLS()
		host, portText, _ := net.SplitHostPort(srv.Listener.Addr().String())
		port, _ := strconv.Atoi(portText)
		cl, err := NewClient(host, port, []string{nodeCert}, panelCert, panelKey)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := cl.SetConfig(context.Background(), []byte(`{"secret":"keys"}`)); err == nil || reached {
			t.Errorf("%s: the panel sent its configuration", who)
		}
		if shownPanel {
			t.Errorf("%s: the panel showed its own certificate", who)
		}
		cl.Close()
		srv.Close()
	}
}

// What a stranger learns by knocking on the control port. The node shows
// its certificate, and says whose certificate it wants, before it knows who
// is calling: neither may say what software runs here.
func TestTheControlPortDoesNotNameTheSoftware(t *testing.T) {
	ca := newCA(t)
	nodeCert, nodeKey, _ := proxy.NodeCertificate("127.0.0.1")
	cfg, err := Bundle{CA: ca.CertPEM, Cert: nodeCert, Key: nodeKey}.TLSConfig()
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.NotFoundHandler())
	srv.TLS = cfg
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	srv.StartTLS()
	defer srv.Close()

	var seen []string
	asked := false
	conn, err := tls.Dial("tcp", srv.Listener.Addr().String(), &tls.Config{
		InsecureSkipVerify: true,
		GetClientCertificate: func(info *tls.CertificateRequestInfo) (*tls.Certificate, error) {
			asked = true
			for _, dn := range info.AcceptableCAs {
				var name pkix.RDNSequence
				if _, err := asn1.Unmarshal(dn, &name); err == nil {
					seen = append(seen, "wanted from: "+name.String())
				} else {
					seen = append(seen, "wanted from: "+string(dn))
				}
			}
			return &tls.Certificate{}, nil
		},
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			for _, der := range raw {
				if c, err := x509.ParseCertificate(der); err == nil {
					seen = append(seen, "subject: "+c.Subject.String(), "issuer: "+c.Issuer.String())
				}
			}
			return nil
		},
	})
	if err == nil {
		conn.Read(make([]byte, 1)) // let the handshake's last flight arrive
		conn.Close()
	}
	if !asked || len(seen) == 0 {
		t.Fatalf("the probe saw nothing (asked for a certificate: %v, names: %v)", asked, seen)
	}
	for _, name := range seen {
		if strings.Contains(strings.ToLower(name), "portunus") || strings.Contains(name, ca.Cert.Subject.CommonName) || strings.HasPrefix(name, "wanted from") {
			t.Errorf("a stranger reads %q on the control port", name)
		}
	}
	// And the names of the CA itself, which every server certificate with
	// a private certificate carries as its issuer.
	if n := ca.Cert.Subject.String(); strings.Contains(strings.ToLower(n), "portunus") {
		t.Errorf("the CA is called %q", n)
	}
}

// A node joined by an earlier version answers with a certificate issued by
// the CA. The panel goes on accepting that exact certificate next to the
// new one, and tells which of the two the node showed.
func TestANodeOfAnEarlierVersionStaysConnected(t *testing.T) {
	ca := newCA(t)
	panelCert, panelKey, _ := ca.IssueClient()
	oldCert, oldKey, _, _, _ := ca.Issue("127.0.0.1")
	newCert, newKey, _ := proxy.NodeCertificate("127.0.0.1")
	for want, pair := range [][2]string{{newCert, newKey}, {oldCert, oldKey}} {
		cfg, err := Bundle{CA: ca.CertPEM, Cert: pair[0], Key: pair[1]}.TLSConfig()
		if err != nil {
			t.Fatal(err)
		}
		srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, Status{Version: "test"})
		}))
		srv.TLS = cfg
		srv.StartTLS()
		host, portText, _ := net.SplitHostPort(srv.Listener.Addr().String())
		port, _ := strconv.Atoi(portText)
		cl, err := NewClient(host, port, []string{newCert, oldCert}, panelCert, panelKey)
		if err != nil {
			t.Fatal(err)
		}
		if cl.Shown() != -1 {
			t.Fatal("a certificate is reported before the node answered")
		}
		if _, err := cl.Status(context.Background()); err != nil {
			t.Fatalf("certificate %d: %v", want, err)
		}
		if cl.Shown() != want {
			t.Fatalf("the node showed certificate %d, the panel says %d", want, cl.Shown())
		}
		cl.Close()
		srv.Close()
	}
	if !proxy.SelfIssued(newCert) || proxy.SelfIssued(oldCert) {
		t.Fatal("the two kinds of certificate are not told apart")
	}
}

func selfSigned(t *testing.T, name string) []tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, _ := x509.MarshalPKCS8PrivateKey(key)
	c, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}),
	)
	if err != nil {
		t.Fatal(err)
	}
	return []tls.Certificate{c}
}
