package globals

import (
	"maps"
	"slices"
)

// AppAuditConfig is portable policy only; credentials are runtime inputs.
type AppAuditConfig struct {
	Enabled      *bool         `json:"enabled,omitempty"`
	ExpectedApps []ExpectedApp `json:"expected_apps,omitempty"`
}

// ExpectedApp bounds the grants of one explicitly named app.
type ExpectedApp struct {
	AppID        int64             `json:"app_id"`
	Permissions  map[string]string `json:"permissions"`
	Repositories []string          `json:"repositories"`
}

var (
	AppAudit          bool
	AppAuditPolicy    *AppAuditConfig
	AppPrivateKeys    []string
	AppPrivateKeyEnvs []string
	AppTokenEnvs      []string
)

// AppAuditEnabled keeps the bot-managed default separate from an explicit choice.
func AppAuditEnabled() bool {
	if Explicit("app-audit") || AppAudit {
		return AppAudit
	}
	if AppAuditPolicy != nil {
		return AppAuditPolicy.Enabled == nil || *AppAuditPolicy.Enabled
	}
	return BotManaged
}

func cloneAppAudit(cfg *AppAuditConfig) *AppAuditConfig {
	if cfg == nil {
		return nil
	}
	out := *cfg
	if cfg.Enabled != nil {
		enabled := *cfg.Enabled
		out.Enabled = &enabled
	}
	out.ExpectedApps = slices.Clone(cfg.ExpectedApps)
	for i, app := range cfg.ExpectedApps {
		out.ExpectedApps[i].Permissions = maps.Clone(app.Permissions)
		out.ExpectedApps[i].Repositories = slices.Clone(app.Repositories)
	}
	return &out
}

func currentAppAudit() *AppAuditConfig {
	cfg := cloneAppAudit(AppAuditPolicy)
	if Explicit("app-audit") || AppAudit {
		if cfg == nil {
			cfg = &AppAuditConfig{}
		}
		enabled := AppAudit
		cfg.Enabled = &enabled
	}
	return cfg
}
