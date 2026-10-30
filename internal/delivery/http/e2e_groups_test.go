package httpapi_test

import (
	"testing"
)

func groupList(t *testing.T, m map[string]any) []map[string]any {
	t.Helper()
	raw, ok := m["groups"].([]any)
	if !ok {
		t.Fatalf("respons tanpa groups: %v", m)
	}
	out := make([]map[string]any, len(raw))
	for i, v := range raw {
		out[i] = v.(map[string]any)
	}
	return out
}

// spec-pendataan §5.2, §5.5: CRUD kelompok oleh admin, GET /groups publik hanya yang aktif.
func TestE2EGroupsAdminCRUD(t *testing.T) {
	st := newStack(t, stackOpts{admin: testAdmin(t), adminPerMin: 1_000_000})
	resp, m := st.getJSON(t, "/api/v1/groups")
	if resp.StatusCode != 200 || len(groupList(t, m)) != 0 {
		t.Fatalf("GET /groups kosong: %d %v", resp.StatusCode, m)
	}
	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/v1/admin/groups"}, {"POST", "/api/v1/admin/groups"}, {"PUT", "/api/v1/admin/groups/1"},
		{"DELETE", "/api/v1/admin/groups/1"},
	} {
		if resp, _ := st.send(t, tc.method, tc.path, `{}`); resp.StatusCode != 401 {
			t.Fatalf("%s %s tanpa token: %d", tc.method, tc.path, resp.StatusCode)
		}
	}
	admin := st.adminLogin(t)

	resp, k4 := admin.send(t, "POST", "/api/v1/admin/groups", `{"name":"  Kelompok 4 – CL-05 "}`)
	if resp.StatusCode != 201 || k4["name"] != "Kelompok 4 – CL-05" || k4["archived"] != false || k4["student_count"] != 0.0 ||
		k4["created_at"] == nil || k4["updated_at"] == nil {
		t.Fatalf("create: %d %v", resp.StatusCode, k4)
	}
	_, k1 := admin.send(t, "POST", "/api/v1/admin/groups", `{"name":"Kelompok 1"}`)
	for _, tc := range []struct {
		body   string
		status int
		code   string
	}{
		{`{"name":"kelompok 4 – cl-05"}`, 409, "GROUP_NAME_TAKEN"},
		{`{"name":""}`, 422, "INVALID_GROUP_NAME"},
		{`{"name":"x","color":"#000000"}`, 400, "INVALID_JSON"},
	} {
		if resp, m := admin.send(t, "POST", "/api/v1/admin/groups", tc.body); resp.StatusCode != tc.status || errField(m) != tc.code {
			t.Fatalf("%s: %d %v", tc.body, resp.StatusCode, m)
		}
	}

	_, m = st.getJSON(t, "/api/v1/groups")
	gs := groupList(t, m)
	if len(gs) != 2 || gs[0]["name"] != "Kelompok 1" || gs[1]["name"] != "Kelompok 4 – CL-05" || len(gs[0]) != 2 {
		t.Fatalf("GET /groups urut nama, hanya id+name: %v", gs)
	}

	// ganti nama ke nama kembar → 409; arsip → hilang dari /groups, tetap di /admin/groups
	if resp, m := admin.send(t, "PUT", "/api/v1/admin/groups/"+idOf(k1), `{"name":"KELOMPOK 4 – CL-05","archived":false}`); resp.StatusCode != 409 || errField(m) != "GROUP_NAME_TAKEN" {
		t.Fatalf("rename kembar: %d %v", resp.StatusCode, m)
	}
	resp, m = admin.send(t, "PUT", "/api/v1/admin/groups/"+idOf(k1), `{"name":"Kelompok 1 – CL-01","archived":true}`)
	if resp.StatusCode != 200 || m["name"] != "Kelompok 1 – CL-01" || m["archived"] != true {
		t.Fatalf("update: %d %v", resp.StatusCode, m)
	}
	_, m = st.getJSON(t, "/api/v1/groups")
	if gs := groupList(t, m); len(gs) != 1 || gs[0]["name"] != "Kelompok 4 – CL-05" {
		t.Fatalf("kelompok arsip masih tampil: %v", gs)
	}
	_, m = admin.getJSON(t, "/api/v1/admin/groups")
	if gs := groupList(t, m); len(gs) != 2 || gs[0]["archived"] != false || gs[1]["archived"] != true {
		t.Fatalf("admin/groups: %v", gs)
	}
	// buka arsip
	if resp, m := admin.send(t, "PUT", "/api/v1/admin/groups/"+idOf(k1), `{"name":"Kelompok 1 – CL-01","archived":false}`); resp.StatusCode != 200 || m["archived"] != false {
		t.Fatalf("buka arsip: %d %v", resp.StatusCode, m)
	}
	for _, p := range []string{"/api/v1/admin/groups/999999", "/api/v1/admin/groups/abc", "/api/v1/admin/groups/0"} {
		if resp, _ := admin.send(t, "PUT", p, `{"name":"x"}`); resp.StatusCode != 404 {
			t.Fatalf("PUT %s: %d", p, resp.StatusCode)
		}
	}

	// hapus: belum dipakai → 204; ulang → 404
	if resp, _ := admin.send(t, "DELETE", "/api/v1/admin/groups/"+idOf(k1), ""); resp.StatusCode != 204 {
		t.Fatalf("hapus: %d", resp.StatusCode)
	}
	if resp, _ := admin.send(t, "DELETE", "/api/v1/admin/groups/"+idOf(k1), ""); resp.StatusCode != 404 {
		t.Fatalf("hapus ulang: %d", resp.StatusCode)
	}
}
