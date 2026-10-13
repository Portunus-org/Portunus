package core

import (
	"context"
	"testing"

	"github.com/PaulichP/Portunus/server/internal/agent"
	"github.com/PaulichP/Portunus/server/internal/proxy"
)

// A node joined by an earlier version holds a certificate issued by the
// CA, which names the CA to whoever connects to the control port. After an
// update the panel makes it a certificate of the new kind and hands that
// one out in the join command, but goes on accepting the old one: nothing
// may stop working because the panel was updated.
func TestNodesOfAnEarlierVersionGetANewCertificate(t *testing.T) {
	c, _ := newBindingCore(t)
	ctx := context.Background()
	n, err := c.CreateNode(ctx, NodeInput{Name: "Exit", Kind: NodeAgent, Address: "203.0.113.20", IsExit: true, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if !proxy.SelfIssued(n.Secret.AgentCert) || n.Secret.PrevAgentCert != "" {
		t.Fatal("a new node did not get a certificate of the new kind")
	}
	// A new node is left alone.
	c.renewNodeCertificates(ctx)
	if again, _ := c.Node(ctx, n.ID); again.Secret.AgentCert != n.Secret.AgentCert {
		t.Fatal("the certificate of a new node was replaced")
	}

	// The same node as an earlier version left it.
	ca, err := c.CA(ctx)
	if err != nil {
		t.Fatal(err)
	}
	oldCert, oldKey, _, _, err := ca.Issue(n.Address)
	if err != nil {
		t.Fatal(err)
	}
	n.Secret.AgentCert, n.Secret.AgentKey = oldCert, oldKey
	if err := c.saveNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	c.renewNodeCertificates(ctx)
	got, err := c.Node(ctx, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !proxy.SelfIssued(got.Secret.AgentCert) || got.Secret.AgentKey == oldKey {
		t.Fatal("the node kept the certificate that names the CA")
	}
	if got.Secret.PrevAgentCert != oldCert {
		t.Fatal("the certificate the node still shows is no longer accepted")
	}
	// The join command hands out the new one.
	join, err := c.NodeJoin(ctx, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	b, err := agent.DecodeBundle(tokenOf(join.Run))
	if err != nil || b.Cert != got.Secret.AgentCert || b.Key != got.Secret.AgentKey {
		t.Fatalf("the join command carries another certificate: %v", err)
	}
	// Once is enough.
	c.renewNodeCertificates(ctx)
	if again, _ := c.Node(ctx, n.ID); again.Secret.AgentCert != got.Secret.AgentCert || again.Secret.PrevAgentCert != oldCert {
		t.Fatal("the certificate was replaced a second time")
	}
}

func tokenOf(run string) string {
	const key = "PORTUNUS_JOIN="
	for i := 0; i+len(key) <= len(run); i++ {
		if run[i:i+len(key)] == key {
			rest := run[i+len(key):]
			for j := 0; j < len(rest); j++ {
				if rest[j] == ' ' {
					return rest[:j]
				}
			}
			return rest
		}
	}
	return ""
}
