// Package applock is when SSHYard locks and how long it makes a wrong
// master password wait. The lock itself is the secrets store's: it opens
// with the master password alone (keychain), so this package keeps
// nothing that could open it, and its file, applock.json, holds only the
// idle minutes, whether locking the screen or sleeping locks SSHYard,
// and the failed attempts' back-off.
package applock

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"slices"
	"time"

	"github.com/lvbibir/sshyard/internal/settings"
)

// File is the name of the lock's file in the data folder.
const File = "applock.json"

// IdleChoices are the minutes of inactivity the lock may wait before it
// locks, 0 for never.
var IdleChoices = []int{1, 5, 15, 30, 60, 0}

// Config is the lock as applock.json keeps it.
type Config struct {
	// IdleMinutes of no input in SSHYard lock it; 0 never does.
	IdleMinutes int `json:"idleMinutes"`
	// LockOnSleep locks as the screen locks or the computer sleeps.
	LockOnSleep bool `json:"lockOnSleep"`
	// Failures is how many attempts in a row failed, RetryAt when the
	// next may be made: kept so that quitting does not start them over.
	Failures int       `json:"failures,omitempty"`
	RetryAt  time.Time `json:"retryAt,omitzero"`
}

// Default is the lock before one is set up.
func Default() Config { return Config{IdleMinutes: 15, LockOnSleep: true} }

// Load reads the lock at path; a missing file is Default. A file that
// cannot be read is an error, which the caller may fall back to Default
// for: what opens the lock is the master password, never this file.
func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, err
	}
	c := Default()
	if err := json.Unmarshal(b, &c); err != nil {
		return Config{}, err
	}
	if !c.valid() {
		return Config{}, errors.New("applock: invalid configuration")
	}
	return c, nil
}

func (c Config) valid() bool {
	return c.IdleMinutes >= 0 && slices.Contains(IdleChoices, c.IdleMinutes)
}

// Save writes the lock to path atomically.
func Save(path string, c Config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return settings.WriteFileAtomic(path, b, 0o600)
}

// The wait after failed attempts: the first freeTries cost nothing, each
// one after doubles the wait from firstWait, up to maxWait.
const (
	freeTries = 5
	firstWait = 30 * time.Second
	maxWait   = 15 * time.Minute
)

// Fail counts a failed attempt at now.
func (c *Config) Fail(now time.Time) {
	c.Failures++
	if n := c.Failures - freeTries; n > 0 {
		d := maxWait
		if n <= 10 {
			d = min(firstWait<<(n-1), maxWait)
		}
		c.RetryAt = now.Add(d)
	}
}

// Succeed forgets the failed attempts.
func (c *Config) Succeed() { c.Failures, c.RetryAt = 0, time.Time{} }

// Wait is how long until the next attempt may be made at now: never more
// than maxWait, so that a clock set back does not lock the user out.
func (c Config) Wait(now time.Time) time.Duration {
	if !now.Before(c.RetryAt) {
		return 0
	}
	return min(c.RetryAt.Sub(now), maxWait)
}
