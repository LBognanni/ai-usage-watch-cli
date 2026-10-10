// Package credentials reads existing client logins. It never refreshes or writes them.
package credentials

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

type Credential struct {
	Token     string
	AccountID string
	Identity  string
	Label     string
	Plan      string
}

type Result struct {
	Credential Credential
	Status     string
	Message    string
}

func identity(id, value string) string {
	sum := sha256.Sum256([]byte(id + ":" + value))
	return hex.EncodeToString(sum[:])
}

func failure(status, message string) Result { return Result{Status: status, Message: message} }

func Claude(path string) Result {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if errors.Is(err, os.ErrNotExist) {
		return failure("not connected", "Sign in with Claude Code (claude auth login).")
	}
	if err != nil {
		return failure("unavailable", "Cannot read Claude Code credentials.")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 64*1024 || info.Mode().Perm()&0077 != 0 {
		return failure("unavailable", "Claude credential file must be a private regular file (chmod 600).")
	}
	var doc struct {
		OAuth struct {
			Access string `json:"accessToken"`
			Plan   string `json:"subscriptionType"`
		} `json:"claudeAiOauth"`
	}
	if json.NewDecoder(io.LimitReader(f, 64*1024)).Decode(&doc) != nil {
		return failure("unavailable", "Malformed Claude Code credentials.")
	}
	if doc.OAuth.Access == "" {
		return failure("not connected", "Claude Code must use a subscription login, not an API key.")
	}
	return Result{Credential: Credential{Token: doc.OAuth.Access, Plan: doc.OAuth.Plan, Label: "Claude Code", Identity: identity("claude", doc.OAuth.Access)}}
}

func ClaudePath() string {
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".claude")
	}
	return filepath.Join(dir, ".credentials.json")
}

// Capped output prevents a broken local CLI from allocating unbounded memory.
type cappedBuffer struct{ bytes.Buffer }

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 1024*1024 {
		return 0, errors.New("credential response too large")
	}
	return b.Buffer.Write(p)
}

func OpenCode(ctx context.Context) map[string]Result {
	cmd := exec.CommandContext(ctx, "opencode", "api", "get", "/api/credential")
	var stdout cappedBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = io.Discard // CLI diagnostics can contain sensitive provider data.
	if err := cmd.Run(); err != nil {
		message := "Cannot read OpenCode accounts; ensure opencode is installed and its service is available."
		if ctx.Err() != nil {
			message = "OpenCode credential lookup timed out."
		}
		return map[string]Result{"openai": failure("unavailable", message), "copilot": failure("unavailable", message)}
	}
	return DecodeOpenCode(stdout.Bytes())
}

func DecodeOpenCode(raw []byte) map[string]Result {
	results := map[string]Result{
		"openai":  failure("not connected", "Connect OpenAI with ChatGPT OAuth in OpenCode (/connect)."),
		"copilot": failure("not connected", "Connect GitHub Copilot in OpenCode (/connect)."),
	}
	var doc struct {
		Data []struct {
			Integration string `json:"integrationID"`
			Label       string `json:"label"`
			Active      bool   `json:"active"`
			Value       struct {
				Type     string                     `json:"type"`
				Access   string                     `json:"access"`
				Metadata map[string]json.RawMessage `json:"metadata"`
			} `json:"value"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &doc) != nil || doc.Data == nil {
		for id := range results {
			results[id] = failure("unavailable", "Unsupported OpenCode credential response.")
		}
		return results
	}
	for _, entry := range doc.Data {
		id := entry.Integration
		if id == "github-copilot" {
			id = "copilot"
		}
		if !entry.Active || (id != "openai" && id != "copilot") {
			continue
		}
		if entry.Value.Type != "oauth" || entry.Value.Access == "" {
			results[id] = failure("not connected", "Active OpenCode account must use subscription OAuth.")
			continue
		}
		metadata := func(key string) string { var v string; _ = json.Unmarshal(entry.Value.Metadata[key], &v); return v }
		// v1 intentionally supports GitHub.com only; never send an enterprise
		// credential to the public GitHub host.
		if id == "copilot" {
			for _, key := range []string{"enterpriseDomain", "enterpriseUrl", "enterpriseURL", "host"} {
				v := metadata(key)
				if v != "" && strings.TrimRight(strings.TrimPrefix(v, "https://"), "/") != "github.com" {
					results[id] = failure("unsupported", "Enterprise-hosted Copilot credentials are not supported in v1.")
					id = ""
					break
				}
			}
			if id == "" {
				continue
			}
		}
		account := metadata("accountID")
		key := account
		if key == "" {
			key = entry.Value.Access
		}
		results[id] = Result{Credential: Credential{Token: entry.Value.Access, AccountID: account, Label: entry.Label, Identity: identity(id, key)}}
	}
	return results
}
