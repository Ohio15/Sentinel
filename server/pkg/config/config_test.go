package config

import "testing"

func TestValidateTrustedProxies(t *testing.T) {
	valid := [][]string{nil, {"172.25.0.3"}, {"172.25.0.0/16", "::1"}, {"fd00::/8"}}
	for _, v := range valid {
		if err := validateTrustedProxies(v); err != nil {
			t.Errorf("%v: unexpected error %v", v, err)
		}
	}
	invalid := [][]string{{"infra-traefik"}, {"172.25.0.0/33"}, {"0.0.0.0/0"}, {"::/0"}, {"172.25.0.3", "bogus"}}
	for _, v := range invalid {
		if err := validateTrustedProxies(v); err == nil {
			t.Errorf("%v: expected error", v)
		}
	}
}

func TestLoadTrustedProxies(t *testing.T) {
	t.Setenv("SERVER_ENV", "development")
	t.Setenv("TRUSTED_PROXIES", " 172.25.0.3 , 172.30.0.0/24 ")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.TrustedProxies) != 2 || cfg.TrustedProxies[0] != "172.25.0.3" || cfg.TrustedProxies[1] != "172.30.0.0/24" {
		t.Fatalf("TrustedProxies = %v", cfg.TrustedProxies)
	}

	t.Setenv("TRUSTED_PROXIES", "0.0.0.0/0")
	if _, err := Load(); err == nil {
		t.Fatal("Load accepted a trust-everything TRUSTED_PROXIES")
	}
}

func TestLoadTrustedProxiesDefaultEmpty(t *testing.T) {
	t.Setenv("SERVER_ENV", "development")
	t.Setenv("TRUSTED_PROXIES", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.TrustedProxies) != 0 {
		t.Fatalf("default TrustedProxies = %v, want none", cfg.TrustedProxies)
	}
}
