// Package hostlist reads a list of hosts to import: one host per line of
// blank-separated key=value fields, typed or pasted, or a CSV table whose
// header row names its columns with the same keys. It only reads; the app
// shows what it found and saves what the user chooses.
package hostlist

import (
	"encoding/csv"
	"errors"
	"os"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/lvbibir/opswright/internal/assets"
)

// The limits a list is read under.
const (
	maxRows  = 10000
	maxBytes = 4 << 20
)

var (
	// ErrEmpty is a list with no host in it.
	ErrEmpty = errors.New("hostlist: no host found")
	// ErrTooLarge is a list over the limits.
	ErrTooLarge = errors.New("hostlist: the list is too large")
	// ErrEncoding is a list that is not UTF-8.
	ErrEncoding = errors.New("hostlist: the list is not UTF-8")
)

// Example is the format itself, for the text area's placeholder.
const Example = "host=192.168.1.10 port=22 user=root password=secret title=web-01\n" +
	"ip=10.0.0.5 username=admin label=\"db 01\" group=prod gateway=bastion su=1 supassword=rootpw"

// Issue is what a line, or a field of it, could not say. Code is one of
// the codes below; Field names the key it is about, "" for the line.
type Issue struct {
	Code  string
	Field string
}

// The codes of an Issue.
const (
	// IssueHost is a line without a host or ip.
	IssueHost = "host"
	// IssuePort is a port that is not a number from 1 to 65535.
	IssuePort = "port"
	// IssueAddress is a host or ip a connection could not use.
	IssueAddress = "address"
	// IssueField is a key the list does not know.
	IssueField = "field"
	// IssueQuote is a value whose quote is never closed.
	IssueQuote = "quote"
	// IssueSuPrompt is a password prompt a shell could not be waited on.
	IssueSuPrompt = "suprompt"
	// IssueSuPassword is a password that cannot be typed at a prompt.
	IssueSuPassword = "supassword"
)

// Row is one host of the list.
type Row struct {
	Name, Address, User, Group string
	Port                       int
	Password                   []byte
	// Gateway names the saved gateway the host is reached through, ""
	// for none; the app matches it against the gateways there are.
	Gateway string
	// Su runs the host's terminals as root with su -, waiting for
	// SuPrompt and typing SuPassword.
	Su         bool
	SuPrompt   string
	SuPassword []byte
	// Issues is what the row could not say; a row with any is not saved.
	Issues []Issue
}

// OK reports a row that can be saved.
func (r Row) OK() bool { return len(r.Issues) == 0 }

// Label names the row in the preview.
func (r Row) Label() string {
	if r.Name != "" {
		return r.Name
	}
	if r.Address != "" {
		return r.Address
	}
	return "?"
}

// ReadFile reads the list in the file at path, refusing one larger than a
// list may be before it is read at all.
func ReadFile(path string) ([]byte, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if st.Size() > maxBytes {
		return nil, ErrTooLarge
	}
	return os.ReadFile(path)
}

// Parse reads a host list, deciding between the text form and a CSV table
// by its first line, and returns its hosts in order. A list with no host
// in it is ErrEmpty; one over the limits, or not UTF-8, fails as such.
func Parse(data []byte) ([]Row, error) {
	if len(data) > maxBytes {
		return nil, ErrTooLarge
	}
	text, ok := decode(data)
	if !ok {
		return nil, ErrEncoding
	}
	var rows []Row
	var err error
	if header, _, _ := strings.Cut(text, "\n"); isCSVHeader(header) {
		rows, err = parseCSV(text)
	} else {
		rows, err = parseText(text)
	}
	if err != nil {
		return nil, err
	}
	switch {
	case len(rows) == 0:
		return nil, ErrEmpty
	case len(rows) > maxRows:
		return nil, ErrTooLarge
	}
	for i := range rows {
		check(&rows[i])
	}
	return rows, nil
}

// decode is data as text, a UTF-8 byte order mark taken off; ok is false
// for a list that is not UTF-8 (a file from a code page, say).
func decode(data []byte) (string, bool) {
	s := string(data)
	s = strings.TrimPrefix(s, "\ufeff")
	return s, utf8.ValidString(s)
}

// isCSVHeader reports a first line that names its columns with a comma, a
// semicolon or a tab and a host or ip among them.
func isCSVHeader(line string) bool {
	sep := csvSep(line)
	if sep == 0 {
		return false
	}
	for _, cell := range strings.Split(line, string(sep)) {
		switch strings.ToLower(strings.TrimSpace(cell)) {
		case "host", "ip":
			return true
		}
	}
	return false
}

// csvSep is what a header's columns are split on: a comma, else a
// semicolon, else a tab, else 0 for none.
func csvSep(line string) rune {
	for _, r := range []rune{',', ';', '\t'} {
		if strings.ContainsRune(line, r) {
			return r
		}
	}
	return 0
}

// parseCSV reads a host table: one host per row, a cell under its column
// name as a key=value field, a blank cell as no field. Rows without a
// value and # comments are skipped.
func parseCSV(text string) ([]Row, error) {
	r := csv.NewReader(strings.NewReader(text))
	header, _, _ := strings.Cut(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	r.Comma = csvSep(header)
	r.Comment = '#'
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	records, err := r.ReadAll()
	if err != nil || len(records) == 0 {
		return nil, ErrEmpty
	}
	keys := records[0]
	var rows []Row
	for _, rec := range records[1:] {
		var fields []field
		for i, v := range rec[:min(len(rec), len(keys))] {
			key := strings.TrimSpace(keys[i])
			if key == "" {
				continue
			}
			v = strings.TrimSpace(v)
			if v != "" {
				fields = append(fields, field{key, v})
			}
		}
		if len(fields) > 0 {
			rows = append(rows, entry(fields))
		}
	}
	return rows, nil
}

// parseText reads one host per line of key=value fields separated by
// blanks. Blank lines, # comments and lines without a field are skipped.
func parseText(text string) ([]Row, error) {
	var rows []Row
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' {
			continue
		}
		fields, unclosed := fieldsOf(line)
		if len(fields) == 0 {
			continue
		}
		row := entry(fields)
		if unclosed != "" {
			row.Issues = append(row.Issues, Issue{Code: IssueQuote, Field: unclosed})
		}
		rows = append(rows, row)
	}
	return rows, nil
}

type field struct{ key, value string }

// entry is the host a line's fields describe.
func entry(fields []field) Row {
	var row Row
	for _, f := range fields {
		switch strings.ToLower(f.key) {
		case "host", "ip":
			row.Address = f.value
		case "port":
			n, err := strconv.Atoi(strings.TrimSpace(f.value))
			if err != nil {
				row.Issues = append(row.Issues, Issue{Code: IssuePort})
				continue
			}
			row.Port = n
		case "user", "username":
			row.User = f.value
		case "password":
			row.Password = []byte(f.value)
		case "title", "name", "label":
			row.Name = f.value
		case "group":
			row.Group = f.value
		case "gateway":
			row.Gateway = f.value
		case "su":
			row.Su = truthy(f.value)
		case "suprompt":
			row.SuPrompt = f.value
		case "supassword":
			row.SuPassword = []byte(f.value)
		default:
			row.Issues = append(row.Issues, Issue{Code: IssueField, Field: f.key})
		}
	}
	return row
}

// truthy is whether v means yes: 1, true, yes or on, in any case.
func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// fieldsOf splits a line into key=value fields; a word without = is
// skipped. unclosed is the key whose quoted value is never closed: its
// value is everything to the line's end, so the row is still shown.
func fieldsOf(line string) (fields []field, unclosed string) {
	for {
		line = strings.TrimLeftFunc(line, unicode.IsSpace)
		if line == "" {
			return fields, ""
		}
		i := strings.IndexFunc(line, func(r rune) bool { return r == '=' || unicode.IsSpace(r) })
		if i < 0 {
			return fields, ""
		}
		if line[i] != '=' || i == 0 {
			line = line[max(i, 1):] // a word, or a = without a key
			continue
		}
		key := line[:i]
		value, rest, closed := valueOf(line[i+1:])
		fields = append(fields, field{key, value})
		if !closed {
			return fields, key
		}
		line = rest
	}
}

// valueOf reads a field's value: one opening with " or ' runs to the
// matching quote, where in double quotes \" and \\ escape; an unquoted
// value is literal up to the next blank. closed is false for an unclosed
// quote.
func valueOf(s string) (value, rest string, closed bool) {
	if s == "" {
		return "", "", true
	}
	if q := s[0]; q == '"' || q == '\'' {
		var b strings.Builder
		for i := 1; i < len(s); i++ {
			switch c := s[i]; {
			case c == q:
				return b.String(), s[i+1:], true
			case c == '\\' && q == '"' && i+1 < len(s) && (s[i+1] == '"' || s[i+1] == '\\'):
				b.WriteByte(s[i+1])
				i++
			default:
				b.WriteByte(c)
			}
		}
		return b.String(), "", false
	}
	i := strings.IndexFunc(s, unicode.IsSpace)
	if i < 0 {
		return s, "", true
	}
	return s[:i], s[i:], true
}

// check is what a row still cannot say: the things a host needs to be
// saved at all.
func check(r *Row) {
	if r.Address == "" {
		r.Issues = append(r.Issues, Issue{Code: IssueHost})
	} else if strings.ContainsAny(r.Address, " \t/@") {
		r.Issues = append(r.Issues, Issue{Code: IssueAddress})
	}
	switch {
	case r.Port == 0:
		r.Port = 22
	case r.Port < 1 || r.Port > 65535:
		r.Issues = append(r.Issues, Issue{Code: IssuePort})
	}
	if !r.Su {
		return
	}
	if err := assets.ValidateSuPrompt(strings.TrimSpace(r.SuPrompt)); err != nil {
		r.Issues = append(r.Issues, Issue{Code: IssueSuPrompt})
	}
	if err := assets.ValidateSuPassword(string(r.SuPassword)); err != nil {
		r.Issues = append(r.Issues, Issue{Code: IssueSuPassword})
	}
}
