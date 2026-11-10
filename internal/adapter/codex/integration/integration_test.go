package integration

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

const program = "/opt/qcrew/bin/qcrew"

func config() Config {
	return Config{CodexHome: "/home/u/.codex", DataDir: "/home/u/.qatlas/qcrew", Program: program}
}

const foreignHooks = `{
  "description": "mine",
  "hooks": {
    "Stop": [
      {
        "hooks": [
          { "type": "command", "command": "notify-send done", "timeout": 5 }
        ]
      }
    ],
    "Custom": [1, 2]
  }
}
`

const foreignConfig = `# my settings
model = "gpt-x"

[hooks.state."/home/u/.codex/hooks.json:stop:0:0"]
trusted_hash = "sha256:aaaa"

[mcp_servers.docs]
command = "docs-server"
args = [
  "--flag",
  [1],
]
note = """
[hooks.state."/home/u/.codex/hooks.json:stop:1:0"]
"""
`

func snap(hooks, conf string) snapshot {
	return snapshot{hooks: []byte(hooks), hooksExists: hooks != "", config: []byte(conf), configExists: conf != ""}
}

func plan(t *testing.T, c Config, s snapshot, a Action) (snapshot, []string) {
	t.Helper()
	st, err := c.read(s)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	next, warnings, err := st.plan(a)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	return next, warnings
}

func report(t *testing.T, c Config, s snapshot) Report {
	t.Helper()
	st, err := c.read(s)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	r, err := st.report()
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	return r
}

// The expected values are SHA-256 of the identity Codex 0.167.0 builds, written out by hand:
// {"event_name":"stop","hooks":[{"async":true,"command":"/opt/qcrew/bin/qcrew hook codex","timeout":600,"type":"command"}]}
// {"event_name":"session_end","hooks":[{"async":false,"command":"'/my bin/qcrew' hook codex","timeout":1,"type":"command"}]}
func TestTrustHash(t *testing.T) {
	if got, want := config().trustHash("Stop"),
		"sha256:72b8d41aaf5dc9b98d95ad76f13555c094f80fb84940befd6b2f8f73b6027b88"; got != want {
		t.Errorf("Stop: got %s, want %s", got, want)
	}
	c := Config{Program: "/my bin/qcrew"}
	if got, want := c.trustHash("SessionEnd"),
		"sha256:6ff7c5d5fdae77abbc76be1bfb180ec82e67529e7fc48d1399c466449ed2bf7e"; got != want {
		t.Errorf("SessionEnd: got %s, want %s", got, want)
	}
}

func TestTrustKey(t *testing.T) {
	if got, want := trustKey("/home/u/.codex/hooks.json", "PermissionRequest", 2, 1),
		"/home/u/.codex/hooks.json:permission_request:2:1"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestSerdeString(t *testing.T) {
	if got, want := serdeString("a\"\\\x01\x1f\n\t/<é\x7f"), `"a\"\\\u0001\u001f\n\t/<é`+"\x7f"+`"`; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestOwn(t *testing.T) {
	c := Config{Program: "/my bin/qcrew"}
	for cmd, want := range map[string]bool{
		"'/my bin/qcrew' hook codex":       true,
		"/usr/local/bin/qcrew hook codex":  true,
		"/opt/qcrew/bin/qcrew hook claude": false,
		"/usr/bin/other hook codex":        false,
		"qcrew hook codex --notify":        false,
		"'/x/it'\\''s/qcrew' hook codex":   true,
	} {
		if got := c.own(cmd); got != want {
			t.Errorf("own(%q) = %v, want %v", cmd, got, want)
		}
	}
}

func TestNewHooksFileIsValid(t *testing.T) {
	c := Config{CodexHome: "/h", Program: `/a "b"/qcrew`}
	var doc struct {
		Hooks map[string][]struct {
			Hooks []map[string]any `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(c.newHooksFile()), &doc); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if len(doc.Hooks) != 9 || doc.Hooks["Stop"][0].Hooks[0]["command"] != `'/a "b"/qcrew' hook codex` ||
		doc.Hooks["Stop"][0].Hooks[0]["async"] != true || doc.Hooks["SessionEnd"][0].Hooks[0]["async"] != nil {
		t.Errorf("unexpected content: %+v", doc)
	}
}

func TestStatus(t *testing.T) {
	c := config()
	done, _ := plan(t, c, snap(foreignHooks, foreignConfig), Setup)
	hooks, conf := string(done.hooks), string(done.config)
	stopKey := `[hooks.state."/home/u/.codex/hooks.json:stop:1:0"]` + "\ntrusted_hash = \"" + c.trustHash("Stop") + "\"\n"
	if !strings.Contains(conf, stopKey) {
		t.Fatalf("no approval of Stop at index 1:\n%s", conf)
	}
	cases := []struct {
		name   string
		s      snapshot
		status Status
		reason string
	}{
		{"nothing", snap("", ""), Missing, ""},
		{"foreign only", snap(foreignHooks, foreignConfig), Missing, ""},
		{"set up", done, Configured, ""},
		{"other program", snap(strings.ReplaceAll(hooks, program, "/usr/bin/qcrew"), conf), Stale,
			"Stop: the hook differs"},
		{"changed handler", snap(strings.Replace(hooks, `hook codex", "async": true}]}
    ],
    "PermissionRequest"`, `hook codex", "async": true, "timeout": 9}]}
    ],
    "PermissionRequest"`, 1), conf), Stale, "PreToolUse: the hook differs"},
		{"moved index", snap(strings.Replace(hooks, `"Stop": [`, `"Stop": [{"hooks": []},`, 1), conf), Stale,
			"Stop: the hook is not approved"},
		{"approval missing", snap(hooks, strings.Replace(conf, stopKey, "", 1)), Stale,
			"Stop: the hook is not approved"},
		{"approval outdated", snap(hooks, strings.Replace(conf, c.trustHash("Stop"), "sha256:0", 1)), Stale,
			"Stop: the approval does not match the hook"},
		{"disabled", snap(hooks, strings.Replace(conf, stopKey, stopKey+"enabled = false\n", 1)), Stale,
			"Stop: the hook is disabled"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := report(t, c, tc.s)
			if r.Status != tc.status {
				t.Fatalf("status %s, want %s (%v)", r.Status, tc.status, r.Problems)
			}
			if tc.reason != "" && !strings.Contains(strings.Join(r.Problems, "\n"), tc.reason) {
				t.Errorf("problems %q lack %q", r.Problems, tc.reason)
			}
		})
	}
}

// isInsertion tells whether b is a with text inserted only, so that every byte of a stays in order.
func isInsertion(a, b string) bool {
	i := 0
	for j := 0; j < len(b) && i < len(a); j++ {
		if a[i] == b[j] {
			i++
		}
	}
	return i == len(a)
}

func TestSetupAndRemoveKeepForeignBytes(t *testing.T) {
	c := config()
	orig := snap(foreignHooks, foreignConfig)
	done, warnings := plan(t, c, orig, Setup)
	if len(warnings) > 0 {
		t.Errorf("warnings: %v", warnings)
	}
	if !strings.HasPrefix(string(done.config), foreignConfig) {
		t.Errorf("config.toml changed before the new tables:\n%s", done.config)
	}
	if !isInsertion(foreignHooks, string(done.hooks)) {
		t.Errorf("hooks.json lost foreign bytes:\n%s", done.hooks)
	}
	// The foreign group keeps index 0 of Stop, so its approval stays valid; qcrew never approves it.
	var doc struct {
		Hooks struct {
			Stop []struct {
				Hooks []struct{ Command string }
			}
		}
	}
	if err := json.Unmarshal(done.hooks, &doc); err != nil {
		t.Fatalf("hooks.json is no longer JSON: %v\n%s", err, done.hooks)
	}
	if s := doc.Hooks.Stop; len(s) != 2 || s[0].Hooks[0].Command != "notify-send done" {
		t.Errorf("Stop groups: %+v", s)
	}
	if strings.Count(string(done.config), "trusted_hash") != 10 {
		t.Errorf("want the foreign approval and nine of qcrew:\n%s", done.config)
	}
	if r := report(t, c, done); r.Status != Configured {
		t.Fatalf("after setup: %s %v", r.Status, r.Problems)
	}
	again, _ := plan(t, c, done, Setup)
	if !reflect.DeepEqual(again, done) {
		t.Errorf("a second setup changes the files")
	}
	back, _ := plan(t, c, done, Remove)
	if string(back.hooks) != foreignHooks || string(back.config) != foreignConfig || !back.hooksExists ||
		!back.configExists {
		t.Errorf("remove does not restore the files:\n%s\n%s", back.hooks, back.config)
	}
}

func TestRoundTripLayouts(t *testing.T) {
	cases := map[string]snapshot{
		"no files":                     snap("", ""),
		"no line breaks":               snap(`{"description":"d"}`, `a = 1`),
		"compact hooks":                snap(`{"hooks":{"Stop":[{"hooks":[]}]}}`, "[x]\r\ny = 2\r\n"),
		"inline event list":            snap(`{"hooks": {"Stop": [{"hooks": []}], "PreCompact": []}}`, "\n"),
		"description only":             snap("{\n\t\"description\": \"d\"\n}\n", "# only a comment"),
		"approval table without a key": snap(`{"hooks":{"Custom":[]}}`, "[hooks.state]\n"),
	}
	for name, orig := range cases {
		t.Run(name, func(t *testing.T) {
			c := config()
			done, _ := plan(t, c, orig, Setup)
			if r := report(t, c, done); r.Status != Configured {
				t.Fatalf("after setup: %s %v\n%s\n%s", r.Status, r.Problems, done.hooks, done.config)
			}
			back, _ := plan(t, c, done, Remove)
			if string(back.hooks) != string(orig.hooks) || back.hooksExists != orig.hooksExists ||
				string(back.config) != string(orig.config) || back.configExists != orig.configExists {
				t.Errorf("remove does not restore the files:\n%q %v\n%q %v", back.hooks, back.hooksExists,
					back.config, back.configExists)
			}
		})
	}
}

func TestSetupReplacesInPlace(t *testing.T) {
	c := config()
	hooks := `{"hooks": {"Stop": [
  {"hooks": [{"type": "command", "command": "/old/qcrew hook codex"}]},
  {"hooks": [{"type": "command", "command": "say hi"}]}
]}}`
	done, warnings := plan(t, c, snap(hooks, ""), Setup)
	if len(warnings) > 0 {
		t.Errorf("warnings: %v", warnings)
	}
	want := `{"hooks": {"Stop": [
  ` + c.groupText("Stop") + `,
  {"hooks": [{"type": "command", "command": "say hi"}]}
]`
	if !strings.HasPrefix(string(done.hooks), want) {
		t.Errorf("got\n%s\nwant prefix\n%s", done.hooks, want)
	}
	if !strings.Contains(string(done.config), `:stop:0:0"]`) || strings.Contains(string(done.config), `:stop:1:0"]`) {
		t.Errorf("approval at the wrong index:\n%s", done.config)
	}
}

func TestRemoveWarnsAboutShiftedHooks(t *testing.T) {
	c := config()
	hooks := `{"hooks": {"Stop": [` + c.groupText("Stop") + `, {"hooks": [{"type": "command", "command": "say hi"}]}]}}`
	back, warnings := plan(t, c, snap(hooks, ""), Remove)
	if len(warnings) != 1 || !strings.Contains(warnings[0], "approved again") {
		t.Errorf("warnings: %v", warnings)
	}
	if want := `{"hooks": {"Stop": [{"hooks": [{"type": "command", "command": "say hi"}]}]}}`; string(back.hooks) != want {
		t.Errorf("got %s", back.hooks)
	}
}

func TestRefuses(t *testing.T) {
	c := config()
	cases := map[string]snapshot{
		"unknown top key":         snap(`{"hook": {}}`, ""),
		"trailing comma":          snap(`{"hooks": {},}`, ""),
		"duplicate key":           snap(`{"hooks": {}, "hooks": {}}`, ""),
		"byte order mark":         snap("\ufeff{}", ""),
		"event not a list":        snap(`{"hooks": {"Stop": {}}}`, ""),
		"handler without type":    snap(`{"hooks": {"Stop": [{"hooks": [{"command": "x"}]}]}}`, ""),
		"invalid TOML":            snap("", "a = \n"),
		"hooks as inline table":   snap("", "hooks = { state = {} }\n"),
		"state as value":          snap("", "[hooks]\nstate = 1\n"),
		"approval as inline":      snap("", "[hooks.state]\n\"/home/u/.codex/hooks.json:pre_model_request:0:0\" = {}\n"),
		"array of approval table": snap("", "[[hooks.state]]\n"),
	}
	for name, s := range cases {
		t.Run(name, func(t *testing.T) {
			st, err := c.read(s)
			if err == nil {
				_, _, err = st.plan(Setup)
			}
			if err == nil {
				t.Errorf("no error")
			}
		})
	}
}

func TestPreview(t *testing.T) {
	c := config()
	orig := snap("", "x = \"\x1b[31m\"\n")
	next, _ := plan(t, c, orig, Setup)
	ch := &Change{cfg: c, old: orig, new: next}
	p := ch.Preview()
	for _, want := range []string{
		"--- /dev/null\n+++ /home/u/.codex/hooks.json\n@@ -0,0 +1,31 @@\n+{\n+  \"hooks\": {\n",
		"--- /home/u/.codex/config.toml\n+++ /home/u/.codex/config.toml\n@@ -1,1 +1,19 @@\n x = \"\\u001b[31m\"\n" +
			"+[hooks.state.\"/home/u/.codex/hooks.json:pre_model_request:0:0\"]\n+trusted_hash = \"sha256:",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("preview lacks %q:\n%s", want, p)
		}
	}
	if strings.ContainsRune(p, 0x1b) {
		t.Errorf("preview holds a raw escape")
	}
	if (&Change{cfg: c, old: orig, new: orig}).Preview() != "" || !(&Change{old: orig, new: orig}).Empty() {
		t.Errorf("no change must give an empty preview")
	}
}

func TestUnifiedDiff(t *testing.T) {
	old := "a\nb\nc\nd\ne\nf\ng\nh\ni\nj"
	new := "a\nb\nC\nd\ne\nf\ng\nh\ni\nj\nk\n"
	want := "--- f\n+++ f\n@@ -1,10 +1,11 @@\n a\n b\n-c\n+C\n d\n e\n f\n g\n h\n i\n-j\n" +
		"\\ No newline at end of file\n+j\n+k\n"
	if got := unifiedDiff("f", []byte(old), []byte(new), true, true); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestMask(t *testing.T) {
	if got, want := mask("a\tb\x1b\x7f\u202e\xffé"), "a\tb\\u001b\\u007f\\u202e\\xffé"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// A file that holds nothing but what the removal takes out goes; Codex reads a missing file like an empty one.
func TestRemoveDeletesFilesLeftEmpty(t *testing.T) {
	c := config()
	done, _ := plan(t, c, snap(`{}`, ""), Setup)
	back, _ := plan(t, c, done, Remove)
	if back.hooksExists || back.configExists {
		t.Errorf("files stay: %q %q", back.hooks, back.config)
	}
}

func TestTwoHunks(t *testing.T) {
	old := "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n"
	new := "1\nx\n3\n4\n5\n6\n7\n8\n9\ny\n"
	want := "--- f\n+++ f\n@@ -1,5 +1,5 @@\n 1\n-2\n+x\n 3\n 4\n 5\n@@ -7,4 +7,4 @@\n 7\n 8\n 9\n-10\n+y\n"
	if got := unifiedDiff("f", []byte(old), []byte(new), true, true); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestTOMLOutline(t *testing.T) {
	const k = "/h/hooks.json:stop:0:0"
	data := `top = 1979-05-27 07:32:00Z
lit = '''
[hooks.state."/h/hooks.json:stop:0:0"]
'''
arr = [
  [1],
  "]", # comment ]
]
inline = { a = "}", b = [ "x" ] }

[ hooks . state . "/h/hooks.json:stop:0:0" ] # mine
trusted_hash = 'sha256:abc'
# trailing comment that belongs to the next table

[other]
x = """a\"""b"""
`
	d, err := parseTOML([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	i, err := d.stateTable(k)
	if err != nil || i < 0 {
		t.Fatalf("table %d, %v", i, err)
	}
	tb := d.tables[i]
	want := "[ hooks . state . \"/h/hooks.json:stop:0:0\" ] # mine\ntrusted_hash = 'sha256:abc'\n"
	if got := data[tb.start:tb.end]; got != want {
		t.Errorf("span %q", got)
	}
	if hash, disabled := d.approval(i); hash != "sha256:abc" || disabled {
		t.Errorf("approval %q %v", hash, disabled)
	}
	if len(d.tables) != 2 || len(d.keys) != 6 {
		t.Errorf("%d tables, %d keys", len(d.tables), len(d.keys))
	}
}
