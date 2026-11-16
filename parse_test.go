package smtp

import (
	"reflect"
	"testing"
)

func TestParseArgs(t *testing.T) {
	valid := []struct {
		raw  string
		want map[string]string
	}{
		{"", map[string]string{}},
		{" BODY=8BITMIME SIZE=1024 SMTPUTF8", map[string]string{
			"BODY":     "8BITMIME",
			"SIZE":     "1024",
			"SMTPUTF8": "",
		}},
		{" body=8bitmime", map[string]string{"BODY": "8bitmime"}},
	}
	for _, tc := range valid {
		got, err := parseArgs(tc.raw)
		if err != nil {
			t.Errorf("parseArgs(%q) unexpected error: %v", tc.raw, err)
		} else if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("parseArgs(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}

	invalid := []string{
		" SIZE=1 SIZE=2",     // duplicate parameter
		" size=1 SIZE=2",     // duplicate parameter, case-insensitive
		" SMTPUTF8 SMTPUTF8", // duplicate valueless parameter
		" SIZE=1 SIZE\r\n",   // bare CR/LF
		" SIZE=1\r",          // bare CR
		" SIZE=1\n",          // bare LF
		" SIZE=1 SIZE\x00=2", // NUL control character
		" SIZE\x7f=1",        // DEL control character
		" =value",            // empty key
		" SIZE=1=2",          // multiple equals signs
	}
	for _, tc := range invalid {
		if got, err := parseArgs(tc); err == nil {
			t.Errorf("parseArgs(%q) = %q, want error", tc, got)
		}
	}
}

func TestParser(t *testing.T) {
	validReversePaths := []struct {
		raw, path, after string
	}{
		{"<>", "", ""},
		{"<root@nsa.gov>", "root@nsa.gov", ""},
		{"root@nsa.gov", "root@nsa.gov", ""},
		{"<root@nsa.gov> AUTH=asdf@example.org", "root@nsa.gov", " AUTH=asdf@example.org"},
		{"root@nsa.gov AUTH=asdf@example.org", "root@nsa.gov", " AUTH=asdf@example.org"},
	}
	for _, tc := range validReversePaths {
		p := parser{tc.raw}
		path, err := p.parseReversePath()
		if err != nil {
			t.Errorf("parser.parseReversePath(%q) = %v", tc.raw, err)
		} else if path != tc.path {
			t.Errorf("parser.parseReversePath(%q) = %q, want %q", tc.raw, path, tc.path)
		} else if p.s != tc.after {
			t.Errorf("parser.parseReversePath(%q): got after = %q, want %q", tc.raw, p.s, tc.after)
		}
	}

	invalidReversePaths := []string{
		"",
		" ",
		"asdf",
		"<Foo Bar <root@nsa.gov>>",
		" BODY=8BITMIME SIZE=12345",
		"a:b:c@example.org",
		"<root@nsa.gov",
	}
	for _, tc := range invalidReversePaths {
		p := parser{tc}
		if path, err := p.parseReversePath(); err == nil {
			t.Errorf("parser.parseReversePath(%q) = %q, want error", tc, path)
		}
	}

	validForwardPaths := []struct {
		raw, path, after string
	}{
		{"<root@nsa.gov>", "root@nsa.gov", ""},
		{"<Postmaster>", "Postmaster", ""},
		{"<postmaster> NOTIFY=NEVER", "postmaster", " NOTIFY=NEVER"},
		{"<Postmaster@nsa.gov>", "Postmaster@nsa.gov", ""},
	}
	for _, tc := range validForwardPaths {
		p := parser{tc.raw}
		path, err := p.parseForwardPath()
		if err != nil {
			t.Errorf("parser.parseForwardPath(%q) = %v", tc.raw, err)
		} else if path != tc.path {
			t.Errorf("parser.parseForwardPath(%q) = %q, want %q", tc.raw, path, tc.path)
		} else if p.s != tc.after {
			t.Errorf("parser.parseForwardPath(%q): got after = %q, want %q", tc.raw, p.s, tc.after)
		}
	}

	invalidForwardPaths := []string{
		"",
		"<>",
		"<root>",
		"<postmasterx>",
		"<Postmaster",
	}
	for _, tc := range invalidForwardPaths {
		p := parser{tc}
		if path, err := p.parseForwardPath(); err == nil {
			t.Errorf("parser.parseForwardPath(%q) = %q, want error", tc, path)
		}
	}

	p := parser{"<Postmaster>"}
	if path, err := p.parseReversePath(); err == nil {
		t.Errorf("parser.parseReversePath(%q) = %q, want error: only a forward-path may omit the domain", "<Postmaster>", path)
	}
}
