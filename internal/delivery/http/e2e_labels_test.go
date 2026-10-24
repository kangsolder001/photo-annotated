package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"photo-annotated/internal/testutil/pgtest"
)

// send mengirim request JSON dengan method apa pun dan mengembalikan respons + body terurai.
func (s *stack) send(t *testing.T, method, path, body string) (*http.Response, map[string]any) {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, _ := http.NewRequest(method, s.srv.URL+path, r)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return resp, m
}

func labelList(t *testing.T, m map[string]any) []map[string]any {
	t.Helper()
	raw, ok := m["labels"].([]any)
	if !ok {
		t.Fatalf("respons tanpa labels: %v", m)
	}
	out := make([]map[string]any, len(raw))
	for i, v := range raw {
		out[i] = v.(map[string]any)
	}
	return out
}

func idOf(m map[string]any) string { return strconv.Itoa(int(m["id"].(float64))) }

func TestE2ELabelsAdminCRUD(t *testing.T) {
	st := newStack(t, stackOpts{admin: testAdmin(t), adminPerMin: 1_000_000})
	resp, m := st.getJSON(t, "/api/v1/labels")
	if resp.StatusCode != 200 || resp.Header.Get("Cache-Control") != "private, max-age=60" || len(labelList(t, m)) != 0 {
		t.Fatalf("GET /labels kosong: %d %v %v", resp.StatusCode, resp.Header, m)
	}
	// tanpa token admin → 401
	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/v1/admin/labels"}, {"POST", "/api/v1/admin/labels"}, {"PUT", "/api/v1/admin/labels/1"},
		{"PUT", "/api/v1/admin/labels/order"}, {"DELETE", "/api/v1/admin/labels/1"},
	} {
		if resp, _ := st.send(t, tc.method, tc.path, `{}`); resp.StatusCode != 401 {
			t.Fatalf("%s %s tanpa token: %d", tc.method, tc.path, resp.StatusCode)
		}
	}
	admin := st.adminLogin(t)

	resp, retak := admin.send(t, "POST", "/api/v1/admin/labels", `{"name":"Retak","description":"Retakan pada dinding paket","color":"#E4572E"}`)
	if resp.StatusCode != 201 || retak["color"] != "#e4572e" || retak["archived"] != false || retak["usage_count"] != 0.0 ||
		retak["sort_order"] != 0.0 || retak["created_at"] == nil || retak["updated_at"] == nil {
		t.Fatalf("create: %d %v", resp.StatusCode, retak)
	}
	_, penyok := admin.send(t, "POST", "/api/v1/admin/labels", `{"name":"Penyok"}`)
	if penyok["description"] != nil || penyok["color"] == "" {
		t.Fatalf("create tanpa deskripsi/warna: %v", penyok)
	}
	for _, tc := range []struct {
		body   string
		status int
		code   string
	}{
		{`{"name":"retak"}`, 409, "LABEL_NAME_TAKEN"},
		{`{"name":"Basah","color":"biru"}`, 422, "INVALID_LABEL_COLOR"},
		{`{"name":""}`, 422, "INVALID_LABEL_NAME"},
		{`{"name":"x","text":"y"}`, 400, "INVALID_JSON"},
	} {
		if resp, m := admin.send(t, "POST", "/api/v1/admin/labels", tc.body); resp.StatusCode != tc.status || errField(m) != tc.code {
			t.Fatalf("%s: %d %v", tc.body, resp.StatusCode, m)
		}
	}

	_, m = st.getJSON(t, "/api/v1/labels")
	ls := labelList(t, m)
	if len(ls) != 2 || ls[0]["name"] != "Retak" || ls[0]["description"] != "Retakan pada dinding paket" || ls[1]["name"] != "Penyok" || len(ls[0]) != 4 {
		t.Fatalf("GET /labels = %v", ls)
	}

	// urutan
	resp, m = admin.send(t, "PUT", "/api/v1/admin/labels/order", `{"ids":[`+idOf(penyok)+`,`+idOf(retak)+`]}`)
	if resp.StatusCode != 200 || labelList(t, m)[0]["name"] != "Penyok" {
		t.Fatalf("reorder: %d %v", resp.StatusCode, m)
	}
	if resp, m := admin.send(t, "PUT", "/api/v1/admin/labels/order", `{"ids":[`+idOf(penyok)+`]}`); resp.StatusCode != 409 || errField(m) != "LABEL_ORDER_STALE" {
		t.Fatalf("reorder basi: %d %v", resp.StatusCode, m)
	}

	// ganti nama + arsip
	resp, m = admin.send(t, "PUT", "/api/v1/admin/labels/"+idOf(retak), `{"name":"Retak halus","description":null,"color":"#123456","archived":true}`)
	if resp.StatusCode != 200 || m["name"] != "Retak halus" || m["archived"] != true || m["description"] != nil {
		t.Fatalf("update: %d %v", resp.StatusCode, m)
	}
	_, m = st.getJSON(t, "/api/v1/labels")
	if ls := labelList(t, m); len(ls) != 1 || ls[0]["name"] != "Penyok" {
		t.Fatalf("label arsip masih tampil untuk pengguna: %v", ls)
	}
	_, m = admin.getJSON(t, "/api/v1/admin/labels")
	if ls := labelList(t, m); len(ls) != 2 || ls[1]["archived"] != true {
		t.Fatalf("admin/labels: %v", ls)
	}
	for _, p := range []string{"/api/v1/admin/labels/999999", "/api/v1/admin/labels/abc", "/api/v1/admin/labels/-1"} {
		if resp, _ := admin.send(t, "PUT", p, `{"name":"x","color":"#000000"}`); resp.StatusCode != 404 {
			t.Fatalf("PUT %s: %d", p, resp.StatusCode)
		}
	}

	// hapus: terpakai → 409 dengan usage_count; tidak terpakai → 204
	id, _ := strconv.Atoi(idOf(penyok))
	pgtest.InsertLabeledAnnotation(t, st.pool, id, "attached")
	resp, m = admin.send(t, "DELETE", "/api/v1/admin/labels/"+idOf(penyok), "")
	if det, _ := m["error"].(map[string]any)["details"].(map[string]any); resp.StatusCode != 409 || errField(m) != "LABEL_IN_USE" || det["usage_count"] != 1.0 {
		t.Fatalf("hapus terpakai: %d %v", resp.StatusCode, m)
	}
	if resp, _ := admin.send(t, "DELETE", "/api/v1/admin/labels/"+idOf(retak), ""); resp.StatusCode != 204 {
		t.Fatalf("hapus: %d", resp.StatusCode)
	}
	if resp, _ := admin.send(t, "DELETE", "/api/v1/admin/labels/"+idOf(retak), ""); resp.StatusCode != 404 {
		t.Fatalf("hapus ulang: %d", resp.StatusCode)
	}

	// preflight DELETE dari FE (dev lintas-origin)
	req, _ := http.NewRequest("OPTIONS", st.srv.URL+"/api/v1/admin/labels/1", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", "DELETE")
	req.Header.Set("Access-Control-Request-Headers", "authorization")
	pre, err := st.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	pre.Body.Close()
	if pre.StatusCode != 204 || !strings.Contains(pre.Header.Get("Access-Control-Allow-Methods"), "DELETE") {
		t.Fatalf("preflight DELETE: %d %v", pre.StatusCode, pre.Header)
	}
}

func TestE2ELabelsAccessOTPMode(t *testing.T) {
	cf := fakeSiteverify(t)
	s := newAuthStack(t, cf.URL, 0)
	admin := s.adminLogin(t)
	if resp, m := admin.send(t, "POST", "/api/v1/admin/labels", `{"name":"Retak"}`); resp.StatusCode != 201 {
		t.Fatalf("create: %d %v", resp.StatusCode, m)
	}
	if resp, _ := s.getJSON(t, "/api/v1/labels"); resp.StatusCode != 401 {
		t.Fatalf("GET /labels tanpa sesi pada mode otp: %d", resp.StatusCode)
	}
	user := s.login(t, "081200000001", "6281200000001")
	if resp, m := user.getJSON(t, "/api/v1/labels"); resp.StatusCode != 200 || len(labelList(t, m)) != 1 {
		t.Fatalf("GET /labels pengguna: %d %v", resp.StatusCode, m)
	}
	if resp, m := admin.getJSON(t, "/api/v1/labels"); resp.StatusCode != 200 || len(labelList(t, m)) != 1 {
		t.Fatalf("GET /labels admin: %d %v", resp.StatusCode, m)
	}
	// token pengguna tidak bisa mengelola label
	if resp, _ := user.send(t, "POST", "/api/v1/admin/labels", `{"name":"Penyok"}`); resp.StatusCode != 401 {
		t.Fatalf("pengguna membuat label: %d", resp.StatusCode)
	}
	if resp, _ := user.getJSON(t, "/api/v1/admin/labels"); resp.StatusCode != 401 {
		t.Fatalf("pengguna membaca admin/labels: %d", resp.StatusCode)
	}
}
