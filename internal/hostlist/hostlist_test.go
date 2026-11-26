package hostlist

import (
	"errors"
	"strings"
	"testing"
)

func TestParseText(t *testing.T) {
	rows, err := Parse([]byte("# a comment\n" +
		"host=192.168.1.10 port=2222 user=root password=s3cret title=web-01 group=prod gateway=bastion su=1 supassword=rootpw\n" +
		"ip=10.0.0.5 username=admin label=\"db 01\"\n" +
		"\n" +
		"host=10.0.0.9\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows %d: %+v", len(rows), rows)
	}
	first := rows[0]
	if first.Address != "192.168.1.10" || first.Port != 2222 || first.User != "root" ||
		first.Name != "web-01" || first.Group != "prod" || first.Gateway != "bastion" || !first.Su ||
		string(first.Password) != "s3cret" || string(first.SuPassword) != "rootpw" || !first.OK() {
		t.Fatalf("first %+v", first)
	}
	second := rows[1]
	if second.Address != "10.0.0.5" || second.User != "admin" || second.Name != "db 01" || second.Port != 22 || second.Su {
		t.Fatalf("second %+v", second)
	}
	if rows[2].Port != 22 || !rows[2].OK() {
		t.Fatalf("third %+v", rows[2])
	}
}

func TestParseTextIssues(t *testing.T) {
	rows, err := Parse([]byte("host=10.0.0.1 port=abc weird=1\n" +
		"host=\"10.0.0.2\n" +
		"port=22 user=root\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows %d", len(rows))
	}
	if !hasIssue(rows[0], IssuePort) || !hasIssue(rows[0], IssueField) {
		t.Errorf("first %+v", rows[0].Issues)
	}
	if !hasIssue(rows[1], IssueQuote) || rows[1].Address != "10.0.0.2" {
		t.Errorf("second %+v", rows[1])
	}
	if !hasIssue(rows[2], IssueHost) || rows[2].OK() {
		t.Errorf("third %+v", rows[2].Issues)
	}
	if rows[0].OK() || rows[1].OK() {
		t.Error("a row with an issue counts as saveable")
	}
}

func TestParseCSV(t *testing.T) {
	for _, tc := range []struct {
		name, data string
	}{
		{name: "commas", data: "host,port,user,password,title,group,su,supassword\n10.0.0.1,22,root,pw,web,prod,true,rootpw\n,,\n"},
		{name: "semicolons", data: "host;port;user;title\n10.0.0.2;2200;admin;db\n"},
		{name: "tabs", data: "host\tport\tuser\n10.0.0.3\t22\tdemo\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := Parse([]byte(tc.data))
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 {
				t.Fatalf("rows %d: %+v", len(rows), rows)
			}
			if !rows[0].OK() {
				t.Fatalf("issues %+v", rows[0].Issues)
			}
		})
	}
	rows, err := Parse([]byte("host,port,user,password,title,group,gateway,su,supassword\n10.0.0.1,22,root,pw,web,prod,bastion,true,rootpw\n"))
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows %v %v", rows, err)
	}
	r := rows[0]
	if r.Address != "10.0.0.1" || r.Port != 22 || r.User != "root" || string(r.Password) != "pw" ||
		r.Name != "web" || r.Group != "prod" || r.Gateway != "bastion" || !r.Su || string(r.SuPassword) != "rootpw" {
		t.Fatalf("row %+v", r)
	}
}

func TestParseSuIssues(t *testing.T) {
	rows, err := Parse([]byte("host=10.0.0.1 su=1 supassword=\"a\x0bb\"\n" +
		"host=10.0.0.2 su=1 suprompt=\"" + strings.Repeat("p", 65) + "\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !hasIssue(rows[0], IssueSuPassword) {
		t.Errorf("a password with a line break was accepted: %+v", rows[0].Issues)
	}
	if !hasIssue(rows[1], IssueSuPrompt) {
		t.Errorf("a long prompt was accepted: %+v", rows[1].Issues)
	}
}

func TestParseRejects(t *testing.T) {
	if _, err := Parse(nil); !errors.Is(err, ErrEmpty) {
		t.Errorf("empty = %v", err)
	}
	if _, err := Parse([]byte("# only comments\n\n")); !errors.Is(err, ErrEmpty) {
		t.Errorf("comments = %v", err)
	}
	if _, err := Parse([]byte{0xff, 0xfe, 'h', 'o', 's', 't'}); !errors.Is(err, ErrEncoding) {
		t.Errorf("not utf-8 = %v", err)
	}
	if _, err := Parse([]byte(strings.Repeat("host=10.0.0.1\n", maxRows+1))); !errors.Is(err, ErrTooLarge) {
		t.Errorf("too many rows = %v", err)
	}
	if _, err := Parse(make([]byte, maxBytes+1)); !errors.Is(err, ErrTooLarge) {
		t.Errorf("too many bytes = %v", err)
	}
}

func TestParseBOMAndCRLF(t *testing.T) {
	// A byte order mark goes, and a carriage return does not end up in a
	// value; one host per line still means one host per line.
	rows, err := Parse([]byte("\ufeffhost=10.0.0.1 port=2200\r\nhost=10.0.0.2\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Address != "10.0.0.1" || rows[0].Port != 2200 || rows[1].Address != "10.0.0.2" {
		t.Fatalf("rows %+v", rows)
	}
}

func hasIssue(r Row, code string) bool {
	for _, i := range r.Issues {
		if i.Code == code {
			return true
		}
	}
	return false
}
