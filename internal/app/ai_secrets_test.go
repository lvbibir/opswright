package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lvbibir/opswright/internal/ai/history"
	"github.com/lvbibir/opswright/internal/assets"
	"github.com/lvbibir/opswright/internal/keychain"
	"github.com/lvbibir/opswright/internal/settings"
	"github.com/lvbibir/opswright/locales"
)

// TestAIHistorySaveFailureKeepsCurrentConversation: a history that cannot
// be written leaves the open conversation alone and says so.
func TestAIHistorySaveFailureKeepsCurrentConversation(t *testing.T) {
	a, tt := newTestApp(t)
	h := savedHost(t, a, "AI host", "host.example")
	id, err := a.OpenAI(h.ID, openAt{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	c := a.conns[id]
	a.noteQuestion(c, "unsaved conversation", nil)
	c.ai.items = []aiItem{{kind: "user", text: "unsaved conversation"}}
	conv := c.ai.conv
	if err := os.WriteFile(filepath.Join(a.p.DataDir, "ai-history"), []byte("block directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.NewChat(id)
	tt.Frame()
	if c.ai.conv != conv || len(c.ai.items) != 1 || !tt.HasText(locales.In("en").AiHistorySaveFailed()) {
		t.Fatal("save failure dropped current conversation or hid error")
	}
}

// TestAIProviderClientPreservesLockedFailure: a locked store reports
// locked, never a missing key.
func TestAIProviderClientPreservesLockedFailure(t *testing.T) {
	a, _ := newTestApp(t)
	p := &settings.AIProvider{ID: "test", BaseURL: "https://example.com", Model: "test"}
	// A store with no entry reports it missing.
	if _, err := a.providerClient(p); !errors.Is(err, errAIKey) {
		t.Fatalf("missing key: %v", err)
	}
	if err := a.kc.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.providerClient(p); !errors.Is(err, keychain.ErrLocked) || errors.Is(err, errAIKey) {
		t.Fatalf("locked key reported missing: %v", err)
	}
}

// TestAIHistoryReadOrDeleteFailurePreservesConversation: damaged history
// is reported, never presented as empty, and the open conversation stays.
func TestAIHistoryReadOrDeleteFailurePreservesConversation(t *testing.T) {
	a, tt := newTestApp(t)
	h := savedHost(t, a, "AI host", "host.example")
	prior := history.Conversation{ID: history.NewID(), Host: h.ID, Title: "previous", Created: time.Now(), Updated: time.Now(), Items: []history.Item{{Kind: "text", Text: "previous answer"}}}
	if err := a.chatHistory().Save(prior); err != nil {
		t.Fatal(err)
	}
	id, err := a.OpenAI(h.ID, openAt{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	c := a.conns[id]
	a.noteQuestion(c, "unsaved current", nil)
	c.ai.items = []aiItem{{kind: "user", text: "unsaved current"}}
	current := c.ai.conv
	indices, err := filepath.Glob(filepath.Join(a.p.DataDir, "ai-history", "*", "index"))
	if err != nil || len(indices) != 1 {
		t.Fatalf("history index: %v %v", indices, err)
	}
	if err := os.WriteFile(indices[0], []byte("damaged ciphertext"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Loading the prior file succeeds, but saving the current index fails.
	a.OpenChat(id, prior.ID)
	if c.ai.conv != current || len(c.ai.items) != 1 {
		t.Fatal("open discarded unsaved conversation after save failure")
	}
	a.ClearChat(id)
	if c.ai.conv != current || len(c.ai.items) != 1 {
		t.Fatal("clear discarded conversation after delete failure")
	}
	a.DeleteChat(h.ID, current)
	if c.ai.conv != current || len(c.ai.items) != 1 {
		t.Fatal("delete discarded conversation after delete failure")
	}
	tt.Frame()
	mustClick(t, tt, locales.In("en").AiChatHistory())
	tt.Frame()
	if !tt.HasText(locales.In("en").AiHistoryLoadFailed()) || tt.HasText(locales.In("en").AiHistoryEmpty()) {
		t.Fatal("damaged history was presented as empty")
	}
}

// TestAITemporaryACPChecksSavedConnectionSecrets: a temporary ACP
// connection needs the store only for what is saved in it.
func TestAITemporaryACPChecksSavedConnectionSecrets(t *testing.T) {
	a, _ := newTestApp(t)
	credential, err := a.store.AddCredential(assets.Credential{ID: assets.NewID(), Name: "login", Kind: assets.CredPassword})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.kc.Set(keychain.Cred(credential.ID, keychain.Password), []byte("test-password")); err != nil {
		t.Fatal(err)
	}
	// A store that is locked reports that, not a missing credential.
	if err := a.kc.Lock(); err != nil {
		t.Fatal(err)
	}
	a.unlock.stored = true
	a.unlock.secretsReady.Store(false)
	c := &conn{host: assets.Host{ID: tempPrefix + "acp", Auth: assets.AuthCredential, CredentialID: credential.ID}, ai: &aiState{agent: "acp"}}
	if !a.aiNeedsSecrets(c) {
		t.Fatal("temporary ACP ignored saved credential")
	}
	a.typed[keychain.Cred(credential.ID, keychain.Password)] = "typed-password"
	if a.aiNeedsSecrets(c) {
		t.Fatal("temporary ACP required the store for a typed password")
	}
	c.host.Auth = assets.AuthAgent
	if a.aiNeedsSecrets(c) {
		t.Fatal("temporary ACP required the store for an SSH agent")
	}
	proxy, err := a.store.AddProxy(assets.Proxy{ID: assets.NewID(), Name: "proxy", Kind: assets.ProxySOCKS5, Address: "localhost", User: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	c.host.ProxyID = proxy.ID
	if !a.aiNeedsSecrets(c) {
		t.Fatal("temporary ACP ignored an authenticated proxy")
	}
}
