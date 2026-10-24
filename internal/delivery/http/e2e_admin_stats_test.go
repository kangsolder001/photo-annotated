package httpapi_test

import (
	"strings"
	"testing"
)

func TestE2EAdminStatsAndSystem(t *testing.T) {
	cf := fakeSiteverify(t)
	s := newAuthStack(t, cf.URL, 0)
	alice := s.login(t, "081200000001", "6281200000001")
	sub := alice.submitted(t, 2) // foto 0: label 1 dan 2
	admin := s.adminLogin(t)

	code, hdr, raw := admin.get(t, "/api/v1/admin/stats/labels", nil)
	if code != 200 || hdr.Get("Cache-Control") != "no-store" {
		t.Fatalf("stats: %d %s", code, raw)
	}
	_, m := admin.getJSON(t, "/api/v1/admin/stats/labels")
	tot := m["totals"].(map[string]any)
	labels := m["labels"].([]any)
	if tot["submissions"] != 1.0 || tot["photos"] != 2.0 || tot["annotations"] != 2.0 || tot["legacy_text_annotations"] != 0.0 ||
		len(labels) != e2eLabels {
		t.Fatalf("stats: %v", m)
	}
	first := labels[0].(map[string]any)
	if first["annotations"] != 1.0 || first["share"] != 0.5 || first["archived"] != false || first["color"] == nil {
		t.Fatalf("label pertama: %v", first)
	}
	if last := labels[len(labels)-1].(map[string]any); last["annotations"] != 0.0 || last["share"] != 0.0 {
		t.Fatalf("label 0 contoh: %v", last)
	}
	_, m = admin.getJSON(t, "/api/v1/admin/stats/labels?review_status=accepted")
	if m["totals"].(map[string]any)["submissions"] != 0.0 {
		t.Fatalf("filter accepted: %v", m)
	}
	for _, q := range []string{"review_status=x", "from=kemarin"} {
		if code, _, _ := admin.get(t, "/api/v1/admin/stats/labels?"+q, nil); code != 422 {
			t.Fatalf("%s: %d", q, code)
		}
	}

	code, hdr, raw = admin.get(t, "/api/v1/admin/system", nil)
	if code != 200 || hdr.Get("Cache-Control") != "no-store" {
		t.Fatalf("system: %d %s", code, raw)
	}
	_, m = admin.getJSON(t, "/api/v1/admin/system")
	for _, k := range []string{"checked_at", "api", "database", "storage", "uploads", "whatsapp", "cleanup", "data", "host"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("bagian %s hilang: %v", k, m)
		}
	}
	db := m["database"].(map[string]any)
	data := m["data"].(map[string]any)
	wa := m["whatsapp"].(map[string]any)
	st := m["storage"].(map[string]any)
	if db["ok"] != true || data["submissions"] != 1.0 || data["review_pending"] != 1.0 || data["users_active"] != 1.0 ||
		wa["otp_available"] != true || wa["daily_cap"] != 500.0 || st["available"] != true || m["api"].(map[string]any)["auth_mode"] != "otp" {
		t.Fatalf("system: %v", m)
	}
	// tanpa data pribadi maupun secret
	for _, bad := range []string{"6281200000001", "62813", "u@example.com", "postgres://", sub.ID.String(), "Bearer"} {
		if strings.Contains(string(raw), bad) {
			t.Fatalf("status sistem memuat %q: %s", bad, raw)
		}
	}
	for _, p := range []string{"/api/v1/admin/system", "/api/v1/admin/stats/labels"} {
		if code, _, _ := alice.get(t, p, nil); code != 401 {
			t.Fatalf("token pengguna %s: %d", p, code)
		}
	}
}

func TestE2EAdminSystemModeNone(t *testing.T) {
	st := newStack(t, stackOpts{admin: testAdmin(t), adminPerMin: 1_000_000})
	admin := st.adminLogin(t)
	_, m := admin.getJSON(t, "/api/v1/admin/system")
	if m["api"].(map[string]any)["auth_mode"] != "none" || m["database"].(map[string]any)["ok"] != true {
		t.Fatalf("mode none: %v", m)
	}
}
