package app

import (
	"strings"
	"testing"

	"github.com/lvbibir/opswright/internal/assets"
	"github.com/lvbibir/opswright/locales"
)

// TestHostGatewaySaved: the editor keeps the gateway a host is reached
// through, and clearing it connects directly.
func TestHostGatewaySaved(t *testing.T) {
	a, _ := newTestApp(t)
	cr, err := a.store.SaveCredential(assets.Credential{Name: "pw", Kind: assets.CredPassword})
	if err != nil {
		t.Fatal(err)
	}
	gw, err := a.store.SaveGateway(assets.Gateway{Name: "bastion", Address: "b.example", User: "me", CredentialID: cr.ID})
	if err != nil {
		t.Fatal(err)
	}
	m := locales.In("en")
	a.openEditor(nil)
	a.form.name, a.form.address, a.form.user = "target", "10.0.0.9", "root"
	a.form.gateway = gw.ID
	if !a.SaveForm(m) {
		t.Fatalf("save failed: %s", a.form.err)
	}
	got, ok := a.store.Get(a.selected)
	if !ok || got.GatewayID != gw.ID {
		t.Fatalf("saved gateway %+v", got)
	}
	a.openEditor(&got)
	if a.form.gateway != gw.ID {
		t.Fatalf("reopened gateway %q", a.form.gateway)
	}
	a.form.gateway = ""
	if !a.SaveForm(m) {
		t.Fatalf("save failed: %s", a.form.err)
	}
	if got, _ := a.store.Get(got.ID); got.GatewayID != "" {
		t.Fatalf("cleared gateway %+v", got)
	}
}

// TestGatewayFormSave: the gateway sheet saves its address, account,
// credential and capabilities, and the note names them.
func TestGatewayFormSave(t *testing.T) {
	a, _ := newTestApp(t)
	cr, err := a.store.SaveCredential(assets.Credential{Name: "pw", Kind: assets.CredPassword})
	if err != nil {
		t.Fatal(err)
	}
	m := locales.In("en")
	a.openGatewayForm(nil)
	f := a.gatewayForm
	f.name, f.address, f.port, f.user, f.cred = "bastion", "10.0.0.1", "2222", "me", cr.ID
	if !a.SaveGatewayForm(m) {
		t.Fatalf("save failed: %s", f.err)
	}
	gs := a.store.Gateways()
	if len(gs) != 1 || gs[0].Port != 2222 || gs[0].DirectTCPIP || gs[0].User != "me" || gs[0].CredentialID == "" {
		t.Fatalf("saved %+v", gs)
	}
	// A routed gateway declares exec and sftp; the note names them.
	a.openGatewayForm(&gs[0])
	a.gatewayForm.exec = true
	if !a.SaveGatewayForm(m) {
		t.Fatal("second save failed")
	}
	got, _ := a.store.Gateway(gs[0].ID)
	if !got.Exec || got.SFTP || !strings.Contains(gatewayNote(m, got), "exec") {
		t.Fatalf("capabilities %+v", got)
	}
}
