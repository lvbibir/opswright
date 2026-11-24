package applock

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestThrottle: the first tries are free, then each failure doubles the
// wait up to its cap, which a clock set back cannot exceed; a success
// starts over.
func TestThrottle(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	var c Config
	for range freeTries {
		c.Fail(now)
	}
	if w := c.Wait(now); w != 0 {
		t.Fatalf("free tries wait %v", w)
	}
	c.Fail(now)
	if w := c.Wait(now); w != firstWait {
		t.Fatalf("first wait %v", w)
	}
	c.Fail(now)
	if w := c.Wait(now); w != 2*firstWait {
		t.Fatalf("second wait %v", w)
	}
	if w := c.Wait(now.Add(2 * firstWait)); w != 0 {
		t.Fatalf("wait after it passed %v", w)
	}
	for range 20 {
		c.Fail(now)
	}
	if w := c.Wait(now); w != maxWait {
		t.Fatalf("capped wait %v", w)
	}
	if w := c.Wait(now.Add(-24 * time.Hour)); w != maxWait {
		t.Fatalf("clock set back: wait %v", w)
	}
	c.Succeed()
	if c.Failures != 0 || c.Wait(now) != 0 {
		t.Fatalf("after success: %+v", c)
	}
}

// TestLoadSave: a missing file is the default; a saved lock comes back as
// saved; a damaged one is an error, which the caller may fall back to
// Default for, since the file opens nothing.
func TestLoadSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, File)
	c, err := Load(path)
	if err != nil || c.IdleMinutes != 15 || !c.LockOnSleep {
		t.Fatalf("missing: %+v %v", c, err)
	}
	want := Config{IdleMinutes: 5, LockOnSleep: true, Failures: 6, RetryAt: time.Unix(1_700_000_000, 0).UTC()}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || got.IdleMinutes != 5 || !got.LockOnSleep || got.Failures != 6 || !got.RetryAt.Equal(want.RetryAt) {
		t.Fatalf("loaded %+v %v", got, err)
	}
	// The file never carries a password, a verifier or a code secret.
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"method", "verifier", "secret", "password"} {
		if strings.Contains(string(body), forbidden) {
			t.Fatalf("applock.json holds %q: %s", forbidden, body)
		}
	}
	for name, body := range map[string]string{
		"damaged": "{",
		"unknown": `{"idleMinutes":-3}`,
	} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if c, err := Load(path); err == nil {
			t.Errorf("%s: loaded %+v", name, c)
		}
	}
}
