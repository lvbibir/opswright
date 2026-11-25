package app

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/lvbibir/opswright/internal/keychain"
	"github.com/lvbibir/opswright/locales"
)

func TestSecretStoreErrorTextSanitizesTypedErrors(t *testing.T) {
	for _, lang := range []string{"en", "zh-Hans"} {
		t.Run(lang, func(t *testing.T) {
			m := locales.In(lang)
			cases := []struct {
				err  error
				want string
			}{
				{&keychain.OpError{Op: "file-write", Err: &os.PathError{Op: "write", Path: "/private/secret-data/secrets.enc", Err: syscall.ENOSPC}}, m.SecretsFailureDetail(m.SecretsStageWriteFile(), m.SecretsReasonDiskFull())},
				{&keychain.OpError{Op: "file-decrypt", Err: fmt.Errorf("private-token: %w", keychain.ErrCorrupt)}, m.SecretsFailureDetail(m.SecretsStageDecrypt(), m.SecretsReasonCorrupt())},
				{&keychain.OpError{Op: "master-cache", Err: fmt.Errorf("private-token /private/secret-data: %w", errors.New("denied"))}, m.SecretsFailureDetail(m.SecretsStageMasterCache(), m.SecretsReasonSystem())},
				{&keychain.OpError{Op: "master-password", Err: fmt.Errorf("private-token: %w", keychain.ErrWrongPassword)}, m.SecretsFailureDetail(m.SecretsStageMasterPassword(), m.MasterPasswordWrong())},
			}
			for _, tc := range cases {
				got := secretStoreErrorText(m, tc.err)
				if got != tc.want || strings.Contains(got, "private-token") || strings.Contains(got, "/private/") {
					t.Fatalf("unsafe or imprecise error classification: %q", got)
				}
			}
			if got := secretStoreErrorText(m, errFlaky); got != errFlaky.Error() {
				t.Fatal("unrelated backend error wording changed")
			}
			if got := secretStoreErrorText(m, keychain.ErrMasterMissing); got != m.SecretsFailureDetail(m.SecretsStageOpen(), m.SecretsReasonMasterMissing()) {
				t.Fatal("known store error was not classified")
			}
			if got := snippetErrorText(m, keychain.ErrLocked); got != m.SecretsFailureDetail(m.SecretsStageOpen(), m.SecretsReasonSystem()) {
				t.Fatal("snippet error was not classified")
			}
		})
	}
}
