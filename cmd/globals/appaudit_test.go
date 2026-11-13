package globals

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAppAuditPrecedenceAndExport(t *testing.T) {
	for _, tc := range []struct {
		name     string
		bot      bool
		block    *AppAuditConfig
		explicit bool
		flag     bool
		want     bool
	}{
		{"off", false, nil, false, false, false},
		{"bot default", true, nil, false, false, true},
		{"block opts in", false, &AppAuditConfig{}, false, false, true},
		{"block opts out", true, &AppAuditConfig{Enabled: boolPtr(false)}, false, false, false},
		{"flag opts out", true, &AppAuditConfig{Enabled: boolPtr(true)}, true, false, false},
		{"flag opts in", false, &AppAuditConfig{Enabled: boolPtr(false)}, true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetBaseline(t)
			t.Cleanup(func() { AppAudit = false; AppAuditPolicy = nil })
			BotManaged, AppAudit = tc.bot, tc.flag
			if tc.explicit {
				NoteExplicitBools(func(name string) bool { return name == "app-audit" })
			}
			if err := ApplyBaseline(Baseline{AppAudit: tc.block}); err != nil {
				t.Fatal(err)
			}
			if AppAuditEnabled() != tc.want {
				t.Fatalf("enabled = %v, want %v", AppAuditEnabled(), tc.want)
			}
			b := CurrentBaseline()
			AppAudit, AppAuditPolicy = false, nil
			ResetExplicit()
			if err := ApplyBaseline(b); err != nil {
				t.Fatal(err)
			}
			if AppAuditEnabled() != tc.want {
				t.Fatal("export lost app-audit choice")
			}
		})
	}
}

func TestAppAuditPolicyCopiesAndNoCredentials(t *testing.T) {
	resetBaseline(t)
	t.Cleanup(func() { AppAuditPolicy = nil; AppPrivateKeys = nil; AppPrivateKeyEnvs = nil; AppTokenEnvs = nil })
	cfg := &AppAuditConfig{ExpectedApps: []ExpectedApp{{AppID: 123, Permissions: map[string]string{"contents": "read"}, Repositories: []string{"owner/repo"}}}}
	if err := ApplyBaseline(Baseline{AppAudit: cfg}); err != nil {
		t.Fatal(err)
	}
	cfg.ExpectedApps[0].Permissions["contents"] = "write"
	cfg.ExpectedApps[0].Repositories[0] = "owner/other"
	if AppAuditPolicy.ExpectedApps[0].Permissions["contents"] != "read" || AppAuditPolicy.ExpectedApps[0].Repositories[0] != "owner/repo" {
		t.Fatal("load retained caller-owned maps or slices")
	}
	AppPrivateKeys = []string{"123=/secret/key.pem"}
	AppPrivateKeyEnvs = []string{"123=SECRET_PEM"}
	AppTokenEnvs = []string{"123=SECRET_TOKEN"}
	b := CurrentBaseline()
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"/secret", "SECRET_PEM", "SECRET_TOKEN"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("runtime credential reference serialized")
		}
	}
	b.AppAudit.ExpectedApps[0].Permissions["contents"] = "admin"
	if AppAuditPolicy.ExpectedApps[0].Permissions["contents"] != "read" {
		t.Fatal("export retained live maps")
	}
}
