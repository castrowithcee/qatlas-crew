package release

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestTrustedKeysMatchAllowedSigners(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", ".github", "release-allowed-signers"))
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[0] != Namespace {
			t.Fatalf("unexpected allowed signers line %q", line)
		}
		want = append(want, fields[1]+" "+fields[2])
	}
	if strings.Join(trustedKeyLines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("compiled keys %q differ from .github/release-allowed-signers %q", trustedKeyLines, want)
	}
	if len(TrustedKeys()) != len(trustedKeyLines) {
		t.Fatalf("TrustedKeys() has %d keys without a build-time addition, want %d", len(TrustedKeys()), len(trustedKeyLines))
	}
}

func TestExtraKeyLinesAddKeys(t *testing.T) {
	old := extraKeyLines
	t.Cleanup(func() { extraKeyLines = old })
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	extraKeyLines = "\n" + strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))) + "\n  \n"
	if got, want := len(TrustedKeys()), len(trustedKeyLines)+1; got != want {
		t.Fatalf("TrustedKeys() = %d keys, want %d", got, want)
	}
}
