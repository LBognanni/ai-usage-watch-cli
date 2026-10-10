package credentials

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenCodeActiveOAuth(t *testing.T) {
	raw := []byte(`{"data":[
	 {"integrationID":"openai","label":"old","active":false,"value":{"type":"oauth","access":"old-secret"}},
	 {"integrationID":"openai","label":"personal","active":true,"value":{"type":"oauth","access":"secret","metadata":{"accountID":"account"}}},
	 {"integrationID":"github-copilot","label":"work","active":true,"value":{"type":"oauth","access":"github-secret"}},
	 {"integrationID":"unrelated","active":true,"value":{"type":"key","key":"ignore"}}
	]}`)
	got := DecodeOpenCode(raw)
	if got["openai"].Credential.Token != "secret" || got["openai"].Credential.AccountID != "account" || got["openai"].Credential.Label != "personal" {
		t.Fatal("wrong active credential")
	}
	if got["copilot"].Credential.Token != "github-secret" {
		t.Fatal("copilot not recognized")
	}
	if strings.Contains(got["openai"].Credential.Identity, "secret") {
		t.Fatal("identity contains secret")
	}
}

func TestOpenCodeFailures(t *testing.T) {
	for _, raw := range []string{`oops`, `{}`, `{"data":[]}`, `{"data":[{"integrationID":"openai","active":true,"value":{"type":"key","key":"secret"}}]}`} {
		got := DecodeOpenCode([]byte(raw))
		if got["openai"].Status == "" {
			t.Fatalf("accepted invalid login: %s", raw)
		}
		if strings.Contains(got["openai"].Message, "secret") {
			t.Fatal("secret leaked")
		}
	}
	got := DecodeOpenCode([]byte(`{"data":[{"integrationID":"github-copilot","active":true,"value":{"type":"oauth","access":"secret","metadata":{"enterpriseDomain":"company.ghe.com"}}}]}`))
	if got["copilot"].Status != "unsupported" {
		t.Fatal("enterprise credential accepted for public host")
	}
}

func TestClaude(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".credentials.json")
	if Claude(path).Status != "not connected" {
		t.Fatal("missing credential")
	}
	if err := os.WriteFile(path, []byte(`{"claudeAiOauth":{"accessToken":"secret","subscriptionType":"pro"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	got := Claude(path)
	if got.Status != "" || got.Credential.Plan != "pro" {
		t.Fatal(got.Status)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if Claude(path).Status != "unavailable" {
		t.Fatal("insecure credential accepted")
	}
	link := filepath.Join(filepath.Dir(path), "symlink")
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if Claude(link).Status != "unavailable" {
		t.Fatal("credential symlink accepted")
	}
}
