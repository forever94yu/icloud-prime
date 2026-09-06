package account

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestNewManagerLoadsDocumentedAccountsArray(t *testing.T) {
	dir := t.TempDir()
	raw := `{
  "accounts": [
    {
      "id": "acc_1",
      "name": "main",
      "host": "icloud.com",
      "cookies": {
        "X-APPLE-WEBAUTH-TOKEN": "token-value"
      },
      "app_password": "app-pass"
    }
  ]
}`

	if err := os.WriteFile(filepath.Join(dir, "accounts.json"), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}

	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}

	acc, ok := mgr.GetAccount("acc_1")
	if !ok {
		t.Fatal("expected account acc_1 to load")
	}
	if got := acc.Cookies["X-APPLE-WEBAUTH-TOKEN"]; got != "token-value" {
		t.Fatalf("expected cookie to load, got %q", got)
	}
	if acc.AppPassword != "app-pass" {
		t.Fatalf("expected app password to load, got %q", acc.AppPassword)
	}
}

func TestAccountSnapshotsOwnTheirCookies(t *testing.T) {
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	added, err := mgr.AddAccount("test", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	added.Name = "changed outside manager"
	cookies := map[string]string{"session": "placeholder"}
	if err := mgr.SaveCookies(added.ID, cookies); err != nil {
		t.Fatal(err)
	}
	cookies["session"] = "changed by caller"
	first, _ := mgr.GetAccount(added.ID)
	if first.Name != "test" || first.Cookies["session"] != "placeholder" {
		t.Fatal("input alias changed account")
	}
	first.Cookies["session"] = "changed snapshot"
	second, _ := mgr.GetAccount(added.ID)
	if second.Cookies["session"] != "placeholder" {
		t.Fatal("account snapshot shares cookies")
	}
	public := second.Public()
	if !public.HasCookies || public.Cookies != nil {
		t.Fatal("incorrect public credentials")
	}
	public.Name = "changed public copy"
	if second.Name != "test" {
		t.Fatal("public account aliases internal snapshot")
	}
}

func TestLateCookieRefreshCannotOverwriteNewCredentials(t *testing.T) {
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	acc, err := mgr.AddAccount("test", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	initial := map[string]string{"session": "initial"}
	if err := mgr.SaveCookies(acc.ID, initial); err != nil {
		t.Fatal(err)
	}
	refreshed := map[string]string{"session": "refreshed"}
	if err := mgr.SaveCookiesIfUnchanged(acc.ID, initial, refreshed); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SaveCookiesIfUnchanged(acc.ID, initial, map[string]string{"session": "stale response"}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Reload(); err != nil {
		t.Fatal(err)
	}
	current, _ := mgr.GetAccount(acc.ID)
	if current.Cookies["session"] != "refreshed" {
		t.Fatal("late refresh replaced newer credentials")
	}
	if err := mgr.SaveCookies(acc.ID, map[string]string{"session": "user update"}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SaveCookiesIfUnchanged(acc.ID, refreshed, map[string]string{"session": "late refresh"}); err != nil {
		t.Fatal(err)
	}
	current, _ = mgr.GetAccount(acc.ID)
	if current.Cookies["session"] != "user update" {
		t.Fatal("refresh replaced explicit user credentials")
	}
}

func TestAccountUpdatesAndReadersAreConcurrentSafe(t *testing.T) {
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	acc, err := mgr.AddAccount("test", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				if err := mgr.SaveCookies(acc.ID, map[string]string{"session": "placeholder"}); err != nil {
					t.Error(err)
				}
				copy, _ := mgr.GetAccount(acc.ID)
				copy.Cookies["session"] = "local edit"
				_ = mgr.ListAccounts()
			}
		}()
	}
	wg.Wait()
	if err := mgr.Reload(); err != nil {
		t.Fatal(err)
	}
}

func TestAccountPersistenceFailureRollsBack(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	acc, err := mgr.AddAccount("test", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.SaveCookies(acc.ID, map[string]string{"session": "original"}); err != nil {
		t.Fatal(err)
	}
	previousFile := mgr.dataFile
	mgr.dataFile = filepath.Join(dir, "unwritable-target")
	if err := os.Mkdir(mgr.dataFile, 0700); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SaveCookies(acc.ID, map[string]string{"session": "new"}); err == nil {
		t.Fatal("expected persistence failure")
	}
	current, _ := mgr.GetAccount(acc.ID)
	if current.Cookies["session"] != "original" {
		t.Fatal("failed update changed memory")
	}
	if removed, err := mgr.RemoveAccount(acc.ID); err == nil || removed {
		t.Fatal("failed removal reported success")
	}
	if _, exists := mgr.GetAccount(acc.ID); !exists {
		t.Fatal("failed removal changed memory")
	}
	if _, err := mgr.AddAccount("failed add", "", "", ""); err == nil {
		t.Fatal("expected add failure")
	}
	if len(mgr.ListAccounts()) != 1 {
		t.Fatal("failed add left an account")
	}
	mgr.dataFile = previousFile
	if err := mgr.Reload(); err != nil {
		t.Fatal(err)
	}
	current, _ = mgr.GetAccount(acc.ID)
	if current.Cookies["session"] != "original" {
		t.Fatal("failed save damaged persisted account")
	}
}

func TestNewManagerImportsEditedExampleWhenRuntimeFileMissing(t *testing.T) {
	dir := t.TempDir()
	raw := `{
  "accounts": {
    "acc_1": {
      "id": "acc_1",
      "name": "main",
      "host": "icloud.com",
      "cookies": {
        "X-APPLE-WEBAUTH-TOKEN": "real-token-value",
        "X-APPLE-WEBAUTH-USER": "real-user-value"
      },
      "icloud_email": "user@icloud.com",
      "app_password": "real-app-password"
    },
    "acc_placeholder": {
      "id": "acc_placeholder",
      "name": "Example account",
      "host": "icloud.com",
      "cookies": {
        "X-APPLE-WEBAUTH-TOKEN": "PASTE_YOUR_COOKIE_VALUE_HERE",
        "X-APPLE-WEBAUTH-USER": "PASTE_YOUR_COOKIE_VALUE_HERE",
        "X-APPLE-DS-WEB-SESSION-TOKEN": "PASTE_YOUR_COOKIE_VALUE_HERE"
      },
      "icloud_email": "your_email@icloud.com",
      "app_password": "xxxx-xxxx-xxxx-xxxx",
      "status": "pending"
    }
  }
}`

	if err := os.WriteFile(filepath.Join(dir, "accounts.example.json"), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}

	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}

	acc, ok := mgr.GetAccount("acc_1")
	if !ok {
		t.Fatal("expected edited accounts.example.json to be imported")
	}
	if got := acc.Cookies["X-APPLE-WEBAUTH-TOKEN"]; got != "real-token-value" {
		t.Fatalf("expected imported cookie, got %q", got)
	}
	if _, ok := mgr.GetAccount("acc_placeholder"); ok {
		t.Fatal("expected placeholder example account to be ignored")
	}
	if _, err := os.Stat(filepath.Join(dir, "accounts.json")); err != nil {
		t.Fatalf("expected imported accounts to be saved to accounts.json: %v", err)
	}
}

func TestNewManagerLoadsBrowserCookieExportAndLegacyAppPasswords(t *testing.T) {
	dir := t.TempDir()
	raw := `{
  "accounts": {
    "acc_1": {
      "id": "acc_1",
      "name": "main",
      "host": "icloud.com",
      "cookies": [
        {
          "name": "X-APPLE-WEBAUTH-TOKEN",
          "value": "token-value"
        }
      ],
      "app_passwords": [
        {
          "icloud_email": "user@icloud.com",
          "password": "app-pass"
        }
      ]
    }
  }
}`

	if err := os.WriteFile(filepath.Join(dir, "accounts.json"), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}

	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}

	acc, ok := mgr.GetAccount("acc_1")
	if !ok {
		t.Fatal("expected account acc_1 to load")
	}
	if got := acc.Cookies["X-APPLE-WEBAUTH-TOKEN"]; got != "token-value" {
		t.Fatalf("expected browser cookie export to load, got %q", got)
	}
	if acc.ICloudEmail != "user@icloud.com" {
		t.Fatalf("expected icloud email to load, got %q", acc.ICloudEmail)
	}
	if acc.AppPassword != "app-pass" {
		t.Fatalf("expected legacy app password to load, got %q", acc.AppPassword)
	}
}

func TestListAccountsRedactsSensitiveFields(t *testing.T) {
	dir := t.TempDir()
	raw := `{
  "accounts": {
    "acc_1": {
      "id": "acc_1",
      "name": "main",
      "cookies": {
        "X-APPLE-WEBAUTH-TOKEN": "token-value"
      },
      "app_password": "app-pass"
    }
  }
}`

	if err := os.WriteFile(filepath.Join(dir, "accounts.json"), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}

	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}

	accounts := mgr.ListAccounts()
	if len(accounts) != 1 {
		t.Fatalf("expected one account, got %d", len(accounts))
	}
	if accounts[0].Cookies != nil {
		t.Fatal("expected cookies to be redacted")
	}
	if accounts[0].AppPassword != "" {
		t.Fatal("expected app password to be redacted")
	}
}
