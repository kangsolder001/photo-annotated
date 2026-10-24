package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

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

// clearLabels membuang label bawaan stack (belum dipakai) agar test mulai dari daftar kosong.
func (s *stack) clearLabels(t *testing.T) {
	t.Helper()
	if _, err := s.pool.Exec(context.Background(), `DELETE FROM labels`); err != nil {
		t.Fatal(err)
	}
}

func TestE2ELabelsAdminCRUD(t *testing.T) {
	st := newStack(t, stackOpts{admin: testAdmin(t), adminPerMin: 1_000_000})
	st.clearLabels(t)
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
	s.clearLabels(t)
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

// AC 6–8, 10–11: kotak memakai label_id, ganti nama langsung terlihat di GET dan export, label arsip
// ditolak untuk kotak baru tetapi boleh dipertahankan saat edit, dan baris lama tetap terbaca.
func TestE2EAnnotationsUseLabels(t *testing.T) {
	st := newStack(t, stackOpts{admin: testAdmin(t), adminPerMin: 1_000_000})
	admin := st.adminLogin(t)
	sub := st.submitted(t, 2) // foto 0: kotak label 1 dan 2
	tok := map[string]string{"X-Edit-Token": sub.Token}

	if resp, m := admin.send(t, "PUT", "/api/v1/admin/labels/1", `{"name":"Retak","color":"#e4572e","archived":false}`); resp.StatusCode != 200 {
		t.Fatalf("ganti nama: %d %v", resp.StatusCode, m)
	}
	_, _, raw := st.get(t, "/api/v1/submissions/"+sub.ID.String(), tok)
	if !strings.Contains(string(raw), `"label":{"id":1,"name":"Retak","color":"#e4572e","archived":false},"text":null`) {
		t.Fatalf("ganti nama harus langsung terlihat: %s", raw)
	}
	exportRows := func(q string) []map[string]any {
		t.Helper()
		code, _, body := admin.get(t, "/api/v1/admin/export"+q, nil)
		if code != 200 {
			t.Fatalf("export%s: %d %s", q, code, body)
		}
		var rows []map[string]any
		for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
			if line == "" {
				continue
			}
			var m map[string]any
			if err := json.Unmarshal([]byte(line), &m); err != nil {
				t.Fatal(err)
			}
			rows = append(rows, m)
		}
		return rows
	}
	rows := exportRows("?label_id=1")
	if len(rows) != 1 {
		t.Fatalf("export label_id=1: %d baris", len(rows))
	}
	a := rows[0]["annotations"].([]any)[0].(map[string]any)
	if a["label_id"] != 1.0 || a["label"] != "Retak" || a["text"] != nil {
		t.Fatalf("anotasi export = %v", a)
	}
	if code, _, _ := admin.get(t, "/api/v1/admin/export?label_id=satu", nil); code != 422 {
		t.Fatalf("label_id bukan angka: %d", code)
	}

	// arsip: kotak baru ditolak, kotak lama di foto yang sama boleh dipertahankan
	if resp, _ := admin.send(t, "PUT", "/api/v1/admin/labels/1", `{"name":"Retak","color":"#e4572e","archived":true}`); resp.StatusCode != 200 {
		t.Fatal("arsip gagal")
	}
	p := st.uploadedPhoto(t, 0x55)
	r := st.submit(t, `{"photos":[{"photo_id":"`+p.String()+`","annotations":[{"label_id":1,"x":0.1,"y":0.1,"w":0.2,"h":0.2}]}],"client_request_id":"`+uuid.NewString()+`"}`)
	if r.Status != 422 || r.Code != "LABEL_ARCHIVED" {
		t.Fatalf("submit label arsip: %+v", r)
	}
	keep := `{"version":1,"photos":[{"photo_id":"` + sub.Photo[0].String() + `","annotations":[{"label_id":1,"x":0.12,"y":0.30,"w":0.25,"h":0.18}]},{"photo_id":"` + sub.Photo[1].String() + `","annotations":[]}]}`
	moved := `{"version":1,"photos":[{"photo_id":"` + sub.Photo[0].String() + `","annotations":[]},{"photo_id":"` + sub.Photo[1].String() + `","annotations":[{"label_id":1,"x":0.12,"y":0.30,"w":0.25,"h":0.18}]}]}`
	if e := st.edit(t, sub.ID, sub.Token, moved); e.Status != 422 || e.Code != "LABEL_ARCHIVED" {
		t.Fatalf("label arsip ke foto lain: %+v", e)
	}
	if e := st.edit(t, sub.ID, sub.Token, keep); e.Status != 200 {
		t.Fatalf("mempertahankan label arsip: %+v", e)
	}
	_, _, raw = st.get(t, "/api/v1/submissions/"+sub.ID.String(), tok)
	if !strings.Contains(string(raw), `"archived":true`) {
		t.Fatalf("label arsip tetap tampil: %s", raw)
	}

	// baris lama dari masa teks bebas
	if _, err := st.pool.Exec(context.Background(), `INSERT INTO annotations (photo_id, text, x, y, w, h) VALUES ($1, 'Kerusakan <kiri> & lama', 0.5, 0.5, 0.1, 0.1)`, sub.Photo[1]); err != nil {
		t.Fatal(err)
	}
	_, _, raw = st.get(t, "/api/v1/submissions/"+sub.ID.String(), tok)
	if !strings.Contains(string(raw), `"label":null,"text":"Kerusakan \u003ckiri\u003e \u0026 lama"`) {
		t.Fatalf("baris lama: %s", raw)
	}
	var legacy map[string]any
	for _, row := range exportRows("") {
		if row["photo_id"] == sub.Photo[1].String() {
			legacy = row["annotations"].([]any)[0].(map[string]any)
		}
	}
	if legacy["label_id"] != nil || legacy["label"] != nil || legacy["text"] != "Kerusakan <kiri> & lama" {
		t.Fatalf("baris lama di export: %v", legacy)
	}
}
