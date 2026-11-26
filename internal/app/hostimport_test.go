package app

import (
	"slices"
	"strings"
	"testing"

	"github.com/lvbibir/opswright/internal/assets"
	"github.com/lvbibir/opswright/internal/keychain"
)

// importText is a list pasted into the import sheet, parsed.
func importText(t *testing.T, a *App, text string) *hostImport {
	t.Helper()
	a.openImport()
	a.imp.text = text
	a.parseImport(en)
	if a.imp.err != "" {
		t.Fatalf("parse: %q", a.imp.err)
	}
	return a.imp
}

// TestImportHosts: a pasted list becomes hosts, their passwords and their
// su passwords, in the encrypted store.
func TestImportHosts(t *testing.T) {
	a, _ := newTestApp(t)
	if err := a.store.AddGroup("prod"); err != nil {
		t.Fatal(err)
	}
	groups := a.store.Groups()
	importText(t, a, "host=192.168.1.10 port=2222 user=root password=s3cret title=web-01 group=prod su=1 supassword=rootpw\n"+
		"ip=10.0.0.5 username=admin\n")
	a.Import(en)
	if a.importing || a.imp != nil {
		t.Fatalf("the sheet stayed open: %+v", a.imp)
	}
	hosts := a.store.List()
	if len(hosts) != 2 {
		t.Fatalf("hosts %+v", hosts)
	}
	var web, db assets.Host
	for _, h := range hosts {
		if h.Name == "web-01" {
			web = h
		}
		if h.Address == "10.0.0.5" {
			db = h
		}
	}
	if web.ID == "" || web.Address != "192.168.1.10" || web.Port != 2222 || web.User != "root" ||
		web.Group != "prod" || web.Auth != assets.AuthPassword || !web.TerminalSu {
		t.Fatalf("web %+v", web)
	}
	if v, err := a.kc.Get(keychain.Host(web.ID, keychain.Password)); err != nil || string(v) != "s3cret" {
		t.Fatalf("password %q %v", v, err)
	}
	if v, err := a.kc.Get(keychain.HostElevate(web.ID)); err != nil || string(v) != "rootpw" {
		t.Fatalf("su password %q %v", v, err)
	}
	if db.ID == "" || db.Name != "" || db.Address != "10.0.0.5" || db.User != "admin" || db.Port != 22 ||
		db.Auth != assets.AuthAsk || db.TerminalSu {
		t.Fatalf("db %+v", db)
	}
	if _, err := a.kc.Get(keychain.HostElevate(db.ID)); err == nil {
		t.Error("a host that does not elevate has a su password")
	}
	noSecrets(t, a.p.DataDir, "s3cret", "rootpw")
	if got := a.store.Groups(); !slices.Equal(got, groups) {
		t.Fatalf("the groups became %q, were %q", got, groups)
	}

	// The list is read again as a CSV table.
	b, _ := newTestApp(t)
	importText(t, b, "host,port,user,password,title,group,su,supassword\n10.0.0.7,22,root,pw,edge,dmz,1,rpw\n")
	b.Import(en)
	hosts = b.store.List()
	if len(hosts) != 1 || hosts[0].Name != "edge" || hosts[0].Group != "dmz" || !hosts[0].TerminalSu {
		t.Fatalf("csv hosts %+v", hosts)
	}
	if v, err := b.kc.Get(keychain.HostElevate(hosts[0].ID)); err != nil || string(v) != "rpw" {
		t.Fatalf("csv su password %q %v", v, err)
	}
}

// TestImportConflicts: a row a saved host already answers for is skipped,
// or overwrites it, or is added beside it, as the preview says.
func TestImportConflicts(t *testing.T) {
	for _, tc := range []struct {
		name   string
		action func(m []string) string
		want   int
		// wantAuth is what the saved host logs in with afterwards.
		wantAuth assets.AuthMethod
		wantName string
	}{
		{name: "skip", action: func(m []string) string { return m[0] }, want: 1, wantAuth: assets.AuthPassword, wantName: "web"},
		{name: "overwrite", action: func(m []string) string { return m[1] }, want: 1, wantAuth: assets.AuthPassword, wantName: "web"},
		{name: "keep both", action: func(m []string) string { return m[2] }, want: 2, wantAuth: assets.AuthPassword, wantName: "web"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := newTestApp(t)
			old, err := a.store.Add(assets.Host{Name: "web", Address: "10.0.0.1", Port: 22, User: "root",
				Auth: assets.AuthPassword, CwdFollow: true})
			if err != nil {
				t.Fatal(err)
			}
			if err := a.kc.Set(keychain.Host(old.ID, keychain.Password), []byte("oldpw")); err != nil {
				t.Fatal(err)
			}
			f := importText(t, a, "host=10.0.0.1 user=root password=newpw title=web-01\n")
			if f.rows[0].conflict != old.ID {
				t.Fatalf("conflict %+v", f.rows[0])
			}
			actions := importActions(en)
			f.rows[0].action = tc.action(actions)
			a.Import(en)
			hosts := a.store.List()
			if len(hosts) != tc.want {
				t.Fatalf("hosts %+v", hosts)
			}
			kept, _ := a.store.Get(old.ID)
			if kept.Auth != tc.wantAuth {
				t.Fatalf("kept %+v", kept)
			}
			switch tc.name {
			case "skip":
				if v, _ := a.kc.Get(keychain.Host(old.ID, keychain.Password)); string(v) != "oldpw" {
					t.Fatalf("skipped row wrote %q", v)
				}
			case "overwrite":
				if v, _ := a.kc.Get(keychain.Host(old.ID, keychain.Password)); string(v) != "newpw" {
					t.Fatalf("overwrite wrote %q", v)
				}
				if kept.Name != "web-01" {
					t.Fatalf("overwrite name %q", kept.Name)
				}
			case "keep both":
				if v, _ := a.kc.Get(keychain.Host(old.ID, keychain.Password)); string(v) != "oldpw" {
					t.Fatalf("the saved host changed: %q", v)
				}
				var added assets.Host
				for _, h := range hosts {
					if h.ID != old.ID {
						added = h
					}
				}
				if added.Name != "web-01" || added.Auth != assets.AuthPassword {
					t.Fatalf("added %+v", added)
				}
			}
		})
	}
}

// TestImportOverwriteClearsSecrets: overwriting a host whose terminals no
// longer elevate, or which no longer logs in with a password, leaves
// nothing of the kind behind.
func TestImportOverwriteClearsSecrets(t *testing.T) {
	a, _ := newTestApp(t)
	old, err := a.store.Add(assets.Host{Name: "web", Address: "10.0.0.1", Port: 22, User: "root",
		Auth: assets.AuthPassword, TerminalSu: true, CwdFollow: true})
	if err != nil {
		t.Fatal(err)
	}
	pw, su := keychain.Host(old.ID, keychain.Password), keychain.HostElevate(old.ID)
	if err := a.kc.Set(pw, []byte("oldpw")); err != nil {
		t.Fatal(err)
	}
	if err := a.kc.Set(su, []byte("rootpw")); err != nil {
		t.Fatal(err)
	}
	f := importText(t, a, "host=10.0.0.1 user=root title=web\n")
	f.rows[0].action = importActions(en)[1]
	a.Import(en)
	if a.kc.Has(pw) || a.kc.Has(su) {
		t.Fatal("overwriting kept the secrets of the old login")
	}
	kept, _ := a.store.Get(old.ID)
	if kept.Auth != assets.AuthAsk || kept.TerminalSu {
		t.Fatalf("kept %+v", kept)
	}
}

// TestImportInvalidRows: a row the list could not say enough about is not
// imported, and says why.
func TestImportInvalidRows(t *testing.T) {
	a, _ := newTestApp(t)
	f := importText(t, a, "host=10.0.0.1 weird=1\n"+
		"host=10.0.0.2 port=99999\n"+
		"port=22 user=root\n")
	if len(f.rows) != 3 {
		t.Fatalf("rows %d", len(f.rows))
	}
	for i, r := range f.rows {
		if r.pick {
			t.Errorf("row %d starts picked: %+v", i, r)
		}
		if importIssueText(en, r.row) == "" {
			t.Errorf("row %d has no reason: %+v", i, r.row)
		}
	}
	if n, _, out := a.importCounts(en, f); n != 0 || out != 3 {
		t.Fatalf("counts %d %d", n, out)
	}
	a.Import(en)
	if len(a.store.List()) != 0 {
		t.Fatal("invalid rows were saved")
	}
}

// TestImportSuWithoutPassword: a host set to become root without a
// password is still imported: it elevates once the editor has one.
func TestImportSuWithoutPassword(t *testing.T) {
	a, _ := newTestApp(t)
	f := importText(t, a, "host=10.0.0.1 user=root su=1 suprompt=Password:\n")
	if n, _, out := a.importCounts(en, f); n != 1 || out != 0 {
		t.Fatalf("counts %d %d", n, out)
	}
	a.Import(en)
	hosts := a.store.List()
	if len(hosts) != 1 || !hosts[0].TerminalSu || hosts[0].TerminalSuPrompt != "Password:" {
		t.Fatalf("hosts %+v", hosts)
	}
	if a.kc.Has(keychain.HostElevate(hosts[0].ID)) {
		t.Fatal("a su password was written without one being given")
	}
	if a.elevateOptions(hosts[0]) != nil {
		t.Fatal("a host without a password elevates")
	}
}

// TestImportOverwriteKeepsGateway: overwriting a host with a row that
// names no gateway keeps the one it uses, rather than sending its
// terminals straight at the server.
func TestImportOverwriteKeepsGateway(t *testing.T) {
	a, _ := newTestApp(t)
	cr, err := a.Store().SaveCredential(assets.Credential{Name: "bastion-cred", Kind: assets.CredPassword, User: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	gw, err := a.Store().SaveGateway(assets.Gateway{Name: "bastion", Address: "10.0.0.9", User: "demo", CredentialID: cr.ID, DirectTCPIP: true})
	if err != nil {
		t.Fatal(err)
	}
	h, err := a.store.Add(assets.Host{Name: "web", Address: "10.0.0.1", Port: 22, User: "root",
		Auth: assets.AuthAsk, GatewayID: gw.ID, CwdFollow: true})
	if err != nil {
		t.Fatal(err)
	}
	f := importText(t, a, "host=10.0.0.1 user=root title=web-01\n")
	f.rows[0].action = importActions(en)[1]
	a.Import(en)
	kept, _ := a.store.Get(h.ID)
	if kept.GatewayID != gw.ID {
		t.Fatalf("the gateway was dropped: %+v", kept)
	}
	// Naming one puts it there, by name or address.
	f = importText(t, a, "host=10.0.0.1 user=root title=web-01 gateway=10.0.0.9\n")
	f.rows[0].action = importActions(en)[1]
	a.Import(en)
	if kept, _ = a.store.Get(h.ID); kept.GatewayID != gw.ID {
		t.Fatalf("the gateway was not set: %+v", kept)
	}
}

// TestImportRoundTripUpdates: a list exported here, given the passwords it
// was missing and read back, updates the hosts it came from rather than
// only being skipped, once the sheet's one choice for rows already saved
// says so.
func TestImportRoundTripUpdates(t *testing.T) {
	a, _ := newTestApp(t)
	h, err := a.store.Add(assets.Host{Name: "web", Address: "10.0.0.1", Port: 22, User: "root",
		Group: "prod", Auth: assets.AuthAsk, CwdFollow: true})
	if err != nil {
		t.Fatal(err)
	}
	out, err := a.exportCSV(&hostExportForm{hosts: []assets.Host{h}})
	if err != nil {
		t.Fatal(err)
	}
	// What the user does to it: give it a password, and a su password.
	list := strings.Replace(string(out), "host,port,user,title,group,gateway,su,suprompt,supassword\r\n",
		"host,port,user,title,group,gateway,su,suprompt,supassword,password\r\n", 1)
	list = strings.Replace(list, "10.0.0.1,22,root,web,prod,,,,\r\n", "10.0.0.1,22,root,web,prod,,1,,rootpw,s3cret\r\n", 1)
	if !strings.Contains(list, "rootpw,s3cret") {
		t.Fatalf("the edited list is not what was meant: %q", list)
	}
	f := importText(t, a, list)
	if f.rows[0].conflict != h.ID {
		t.Fatalf("no conflict: %+v", f.rows[0])
	}
	// By default it is skipped, and the import says so rather than closing
	// on an empty import.
	if n, existing, out := a.importCounts(en, f); n != 0 || existing != 1 || out != 1 {
		t.Fatalf("counts %d %d %d", n, existing, out)
	}
	a.Import(en)
	kept, _ := a.store.Get(h.ID)
	if kept.Auth != assets.AuthAsk || a.kc.Has(keychain.Host(h.ID, keychain.Password)) {
		t.Fatalf("skipped row changed the host: %+v", kept)
	}
	if !a.importing {
		t.Fatal("a skipped import closed the sheet")
	}
	// One choice for every row already saved turns them into updates.
	if f.action != importActions(en)[0] {
		t.Fatalf("the one choice starts at %q", f.action)
	}
	f.action = importActions(en)[1]
	importEvery(f, f.action)
	a.Import(en)
	if a.importing || a.imp != nil {
		t.Fatal("the sheet stayed open")
	}
	hosts := a.store.List()
	if len(hosts) != 1 {
		t.Fatalf("hosts %+v", hosts)
	}
	kept, _ = a.store.Get(h.ID)
	if kept.Name != "web" || kept.Group != "prod" || kept.Auth != assets.AuthPassword || !kept.TerminalSu {
		t.Fatalf("updated %+v", kept)
	}
	if v, err := a.kc.Get(keychain.Host(h.ID, keychain.Password)); err != nil || string(v) != "s3cret" {
		t.Fatalf("password %q %v", v, err)
	}
	if v, err := a.kc.Get(keychain.HostElevate(h.ID)); err != nil || string(v) != "rootpw" {
		t.Fatalf("su password %q %v", v, err)
	}
	noSecrets(t, a.p.DataDir, "s3cret", "rootpw")
}

// TestImportNeedsUnlock: a list holding passwords is not imported into a
// locked store, and says so.
func TestImportNeedsUnlock(t *testing.T) {
	a, tt, _ := newLockedTestApp(t)
	setMasterPassword(t, a, tt, "correct horse")
	a.LockApp()
	f := importText(t, a, "host=10.0.0.1 user=root password=s3cret\n")
	if f.rows[0].conflict != "" {
		t.Fatalf("conflict %+v", f.rows[0])
	}
	a.Import(en)
	if f.err != en.ImportNeedsUnlock() {
		t.Fatalf("err %q", f.err)
	}
	if len(a.store.List()) != 0 {
		t.Fatal("a locked store imported a password")
	}
	// A list without secrets goes in with nothing unlocked.
	f.text = "host=10.0.0.2 user=demo\n"
	a.parseImport(en)
	a.Import(en)
	if len(a.store.List()) != 1 {
		t.Fatalf("hosts %+v", a.store.List())
	}
}

// TestExportCSV: the chosen hosts become a CSV host list, the same one
// the import reads, with the passwords in it only when they were asked
// for, a byte order mark a spreadsheet reads and the gateway a host is
// reached through.
func TestExportCSV(t *testing.T) {
	a, _ := newTestApp(t)
	cr, err := a.Store().SaveCredential(assets.Credential{Name: "bastion-cred", Kind: assets.CredPassword, User: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	gw, err := a.Store().SaveGateway(assets.Gateway{Name: "bastion", Address: "10.0.0.9", User: "demo", CredentialID: cr.ID, DirectTCPIP: true})
	if err != nil {
		t.Fatal(err)
	}
	web, err := a.store.Add(assets.Host{Name: "web", Address: "10.0.0.1", Port: 2222, User: "root",
		Group: "prod", Auth: assets.AuthPassword, TerminalSu: true, TerminalSuPrompt: "Password:", GatewayID: gw.ID, CwdFollow: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.kc.Set(keychain.Host(web.ID, keychain.Password), []byte("s3cret")); err != nil {
		t.Fatal(err)
	}
	if err := a.kc.Set(keychain.HostElevate(web.ID), []byte("rootpw")); err != nil {
		t.Fatal(err)
	}
	db, err := a.store.Add(assets.Host{Name: "db", Address: "10.0.0.2", User: "admin", Auth: assets.AuthAgent, CwdFollow: true})
	if err != nil {
		t.Fatal(err)
	}

	plain, err := a.exportCSV(&hostExportForm{hosts: []assets.Host{web, db}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(plain), "\ufeff") {
		t.Fatalf("no byte order mark: %q", plain[:min(len(plain), 12)])
	}
	if !strings.Contains(string(plain), "\r\n") {
		t.Fatal("the lines do not end CR LF")
	}
	body := strings.TrimPrefix(string(plain), "\ufeff")
	if strings.Contains(body, "s3cret") || strings.Contains(body, "rootpw") || strings.Contains(body, ",password,") {
		t.Fatalf("a file without secrets holds one: %q", body)
	}
	if !strings.Contains(body, "host,port,user,title,group,gateway,su,suprompt,supassword") {
		t.Fatalf("columns %q", body)
	}
	if !strings.Contains(body, "10.0.0.1,2222,root,web,prod,bastion,1,Password:,") {
		t.Fatalf("web row %q", body)
	}
	if !strings.Contains(body, "10.0.0.2,22,admin,db,,,,,\r\n") {
		t.Fatalf("db row %q", body)
	}

	with, err := a.exportCSV(&hostExportForm{hosts: []assets.Host{web, db}, secrets: true})
	if err != nil {
		t.Fatal(err)
	}
	body = strings.TrimPrefix(string(with), "\ufeff")
	if !strings.Contains(body, ",s3cret,") || !strings.Contains(body, ",rootpw") {
		t.Fatalf("secrets absent: %q", body)
	}
	// It reads back as the same hosts, the gateway found by its name.
	b, _ := newTestApp(t)
	bcr, err := b.Store().SaveCredential(assets.Credential{Name: "bastion-cred", Kind: assets.CredPassword, User: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Store().SaveGateway(assets.Gateway{Name: "bastion", Address: "10.0.0.9", User: "demo", CredentialID: bcr.ID}); err != nil {
		t.Fatal(err)
	}
	f := importText(t, b, string(with))
	if n, _, out := b.importCounts(en, f); n != 2 || out != 0 {
		t.Fatalf("counts %d %d (%q)", n, out, b.imp.err)
	}
	b.Import(en)
	hosts := b.store.List()
	if len(hosts) != 2 {
		t.Fatalf("hosts %+v", hosts)
	}
	var back assets.Host
	for _, h := range hosts {
		if h.Name == "web" {
			back = h
		}
	}
	if back.Address != "10.0.0.1" || back.Port != 2222 || back.User != "root" || back.Group != "prod" ||
		back.Auth != assets.AuthPassword || !back.TerminalSu || back.TerminalSuPrompt != "Password:" {
		t.Fatalf("round trip %+v", back)
	}
	here, ok := b.store.Gateway(back.GatewayID)
	if !ok || here.Name != "bastion" {
		t.Fatalf("round trip gateway %q %v", back.GatewayID, ok)
	}
	if v, _ := b.kc.Get(keychain.HostElevate(back.ID)); string(v) != "rootpw" {
		t.Fatalf("round trip su password %q", v)
	}
	for _, h := range hosts {
		if h.Name == "db" && h.GatewayID != "" {
			t.Fatalf("a host without a gateway was given one: %+v", h)
		}
	}
}

// TestImportGateway: a row naming a gateway that is not here is imported
// without one, and says so rather than being refused.
func TestImportGateway(t *testing.T) {
	a, _ := newTestApp(t)
	f := importText(t, a, "host=10.0.0.1 user=root gateway=bastion\n")
	if f.rows[0].gateway != "" || !f.rows[0].row.OK() {
		t.Fatalf("row %+v", f.rows[0])
	}
	if n, _, out := a.importCounts(en, f); n != 1 || out != 0 {
		t.Fatalf("counts %d %d", n, out)
	}
	a.Import(en)
	hosts := a.store.List()
	if len(hosts) != 1 || hosts[0].GatewayID != "" {
		t.Fatalf("hosts %+v", hosts)
	}
	// Overwriting it with a row that names no gateway keeps none, and one
	// that names one that is not here keeps the saved one.
	f = importText(t, a, "host=10.0.0.1 user=root title=other\n")
	f.rows[0].action = importActions(en)[1]
	a.Import(en)
	for _, h := range a.store.List() {
		if h.GatewayID != "" {
			t.Fatalf("a gateway came from nowhere: %+v", h)
		}
	}

	cr, err := a.Store().SaveCredential(assets.Credential{Name: "bastion-cred", Kind: assets.CredPassword, User: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Store().SaveGateway(assets.Gateway{Name: "bastion", Address: "10.0.0.9", User: "demo", CredentialID: cr.ID}); err != nil {
		t.Fatal(err)
	}
	f = importText(t, a, "host=10.0.0.2 user=root gateway=BASTION\n")
	if f.rows[0].gateway == "" {
		t.Fatal("a gateway named in another case was not found")
	}
	a.Import(en)
	var got assets.Host
	for _, h := range a.store.List() {
		if h.Address == "10.0.0.2" {
			got = h
		}
	}
	if got.GatewayID == "" {
		t.Fatal("the gateway was not set")
	}
}
