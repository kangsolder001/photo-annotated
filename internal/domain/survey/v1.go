package survey

func intp(n int) *int { return &n }

var (
	yesNo = []Option{{ID: "yes", Label: "Ya"}, {ID: "no", Label: "Tidak"}}
	obs   = []Option{{ID: "yes", Label: "Ada/Sesuai"}, {ID: "no", Label: "Tidak"}, {ID: "na", Label: "Tidak teramati"}}
)

const bNoteHelp = "Opsional. Tulis nomor butirnya, contoh: B3 – belum bisa membedakan baterai dan popok."

// bSection membuat sub-bagian B: butir Ya/Tidak wajib + keterangan opsional.
func bSection(n, title, help, noteLabel string, items [][2]string) Section {
	s := Section{ID: "B" + n, Title: title, Help: help}
	for _, it := range items {
		s.Questions = append(s.Questions, Question{ID: it[0], Label: it[1], Type: TypeChoice, Required: true, Options: yesNo})
	}
	s.Questions = append(s.Questions, Question{ID: "b_note_" + n, Label: noteLabel, Help: bNoteHelp, Type: TypeTextarea})
	return s
}

// V1 adalah instrumen versi 1 (spec-pendataan §4.2; teks dari Google Form "Instrumen Baseline Rumah
// Tangga Cipadung Kulon Kelompok 4").
var V1 = &Form{
	Version: 1,
	ConsentText: "Data dan foto tempat sampah (tanpa wajah dan nomor rumah) dipakai untuk pendataan KKN dan " +
		"penelitian dosen. Bapak/Ibu boleh menolak.",
	Sections: []Section{
		{
			ID: SectionIdentity, Title: "A. Identitas Pendataan",
			Help: "Jangan menuliskan nama lengkap penghuni. Gunakan inisial.",
			Questions: []Question{
				{ID: "a_date", Label: "Tanggal pendataan", Type: TypeDate, Required: true},
				{ID: "a_rw", Label: "RW", Type: TypeText, Required: true, Max: intp(10)},
				{ID: "a_rt", Label: "RT", Type: TypeText, Required: true, Max: intp(10)},
				{ID: "a_household_code", Label: "Kode rumah tangga (RT, RW, no rumah)", Type: TypeText, Required: true, Max: intp(50)},
				{ID: "a_initials", Label: "Inisial responden", Help: "Contoh: Ny. S", Type: TypeText, Required: true, Max: intp(20)},
				{ID: "a_respondent", Label: "Status responden", Type: TypeChoice, Required: true, Options: []Option{
					{ID: "head", Label: "Kepala keluarga"}, {ID: "spouse", Label: "Istri/suami"}, {ID: "adult", Label: "Anggota keluarga dewasa"},
				}},
				{ID: ConsentID, Label: "Persetujuan pendataan", Type: TypeChoice, Required: true, Options: []Option{
					{ID: ConsentYes, Label: "Bersedia"}, {ID: ConsentNo, Label: "Tidak bersedia"},
				}},
			},
		},
		bSection("1", "1. Pengetahuan dan kebiasaan pemilahan sampah",
			"Tanyakan secara netral. Untuk pertanyaan pengetahuan, minta contoh singkat dari responden.",
			"Keterangan butir B1–B8", [][2]string{
				{"b1", "Apakah keluarga mengetahui pentingnya memilah sampah dari rumah?"},
				{"b2", "Apakah keluarga mengetahui perbedaan sampah organik dan anorganik?"},
				{"b3", "Apakah keluarga mengetahui kategori sampah B3 dan residu?"},
				{"b4", "Apakah keluarga telah menyediakan wadah sampah terpisah?"},
				{"b5", "Apakah keluarga sudah memilah sampah organik dan anorganik?"},
				{"b6", "Apakah keluarga memisahkan sampah B3 rumah tangga?"},
				{"b7", "Apakah keluarga memisahkan sampah residu?"},
				{"b8", "Apakah pemilahan dilakukan secara rutin setiap hari?"},
			}),
		bSection("2", "2. Kondisi pengelolaan sampah organik", "", "Keterangan butir B9–B14", [][2]string{
			{"b9", "Apakah keluarga menghasilkan sampah organik dari aktivitas sehari-hari?"},
			{"b10", "Apakah sampah organik dipisahkan dari sampah lainnya?"},
			{"b11", "Apakah keluarga mengetahui cara membuat kompos sederhana?"},
			{"b12", "Apakah keluarga memiliki atau mengakses fasilitas komposter?"},
			{"b13", "Apakah keluarga sudah mengolah sampah organik menjadi kompos atau produk lain?"},
			{"b14", "Apakah keluarga bersedia mengikuti pelatihan pengolahan sampah organik?"},
		}),
		bSection("3", "3. Pengelolaan sampah anorganik dan bank sampah", "", "Keterangan butir B15–B19", [][2]string{
			{"b15", "Apakah keluarga memisahkan botol plastik, kardus atau bahan daur ulang lainnya?"},
			{"b16", "Apakah keluarga mengetahui adanya bank sampah di wilayahnya?"},
			{"b17", "Apakah keluarga menjadi anggota atau memanfaatkan bank sampah?"},
			{"b18", "Apakah keluarga pernah menyetorkan sampah anorganik ke bank sampah?"},
			{"b19", "Apakah keluarga bersedia berpartisipasi dalam kegiatan bank sampah?"},
		}),
		bSection("4", "4. Hilirisasi hasil riset ecoenzim", "", "Keterangan butir B20–B25", [][2]string{
			{"b20", "Apakah keluarga pernah mendengar tentang ecoenzim?"},
			{"b21", "Apakah keluarga mengetahui bahan organik yang dapat digunakan untuk membuat ecoenzim?"},
			{"b22", "Apakah keluarga pernah membuat ecoenzim?"},
			{"b23", "Apakah keluarga memiliki akses terhadap bahan dan wadah untuk pembuatan ecoenzim?"},
			{"b24", "Apakah keluarga bersedia mengikuti demonstrasi pembuatan ecoenzim?"},
			{"b25", "Apakah keluarga bersedia melakukan pemantauan proses ecoenzim bersama kader/mahasiswa?"},
		}),
		bSection("5", "5. Dukungan dan keberlanjutan",
			"Pertanyaan kesediaan mengukur niat berpartisipasi, bukan bukti perubahan perilaku.",
			"Keterangan butir B26–B30", [][2]string{
				{"b26", "Apakah keluarga pernah memperoleh edukasi pengelolaan sampah?"},
				{"b27", "Apakah terdapat kader atau penggerak lingkungan yang dikenal keluarga?"},
				{"b28", "Apakah keluarga bersedia mengikuti pendampingan pemilahan sampah?"},
				{"b29", "Apakah keluarga bersedia menerima kunjungan evaluasi berikutnya?"},
				{"b30", "Apakah keluarga bersedia mempertahankan pemilahan sampah setelah KKN selesai?"},
			}),
		{
			ID: "C", Title: "C. Checklist Verifikasi Observasi Langsung",
			Help: "Wajib diisi berdasarkan pengamatan mahasiswa, bukan hanya jawaban responden. Jika tidak dapat " +
				"diperiksa, pilih \"Tidak teramati\". \"Tidak teramati\" berbeda dengan \"Tidak\". Jangan membuka " +
				"tempat sampah sendiri dan jangan menyentuh sampah B3.",
			Questions: []Question{
				{ID: "c1", Label: "Terdapat wadah sampah organik", Type: TypeChoice, Required: true, Options: obs},
				{ID: "c2", Label: "Terdapat wadah sampah anorganik", Type: TypeChoice, Required: true, Options: obs},
				{ID: "c3", Label: "Sampah organik dan anorganik benar-benar terpisah", Type: TypeChoice, Required: true, Options: obs},
				{ID: "c4", Label: "Sampah B3 rumah tangga disimpan terpisah dengan aman", Type: TypeChoice, Required: true, Options: obs},
				{ID: "c5", Label: "Sampah residu dipisahkan dari sampah yang dapat diolah", Type: TypeChoice, Required: true, Options: obs},
				{ID: "c6", Label: "Terdapat sarana pengomposan yang dapat digunakan", Type: TypeChoice, Required: true, Options: obs},
				{ID: "c7", Label: "Ada bukti pengolahan sampah organik oleh rumah tangga", Type: TypeChoice, Required: true, Options: obs},
				{ID: "c8", Label: "Ada tempat penyimpanan sampah anorganik untuk disalurkan", Type: TypeChoice, Required: true, Options: obs},
				{ID: "c9", Label: "Ada bukti kegiatan pemanfaatan atau penyetoran ke bank sampah", Type: TypeChoice, Required: true, Options: obs},
				{ID: "c10", Label: "Ada kegiatan ecoenzim yang benar-benar dilakukan keluarga", Type: TypeChoice, Required: true, Options: obs},
				{ID: "c_note", Label: "Catatan observasi", Type: TypeTextarea},
			},
		},
		{
			ID: "R2", Title: "R2. Data rumah tangga",
			Questions: []Question{
				{ID: "r2_occupants", Label: "Jumlah penghuni rumah", Type: TypeInt, Min: intp(1), Max: intp(50)},
				{ID: "r2_head_age", Label: "Usia kepala keluarga", Type: TypeChoice, Options: []Option{
					{ID: "lt30", Label: "< 30 tahun"}, {ID: "30_44", Label: "30–44 tahun"}, {ID: "45_59", Label: "45–59 tahun"}, {ID: "ge60", Label: "≥ 60 tahun"},
				}},
				{ID: "r2_head_edu", Label: "Pendidikan terakhir KK", Type: TypeChoice, Options: []Option{
					{ID: "sd", Label: "Tidak sekolah/SD"}, {ID: "smp", Label: "SMP"}, {ID: "sma", Label: "SMA/SMK"}, {ID: "pt", Label: "Diploma/Sarjana ke atas"},
				}},
			},
		},
		{
			ID: "R3", Title: "R3. Foto dan label wadah",
			Help: "Protokol foto: minta warga membuka tutup wadah (jangan dibuka sendiri). Foto dari atas, jarak ±50 cm, " +
				"semua wadah masuk satu bingkai, cahaya cukup. Tanpa wajah, tangan, nomor rumah, atau dokumen. 1 foto " +
				"utama, boleh tambah maks. 2 foto dekat.",
			Questions: []Question{
				{ID: "r3_sorting", Label: "Label lapangan: kondisi pemilahan di foto", Type: TypeChoice, Required: true,
					Help: "Terpilah: ada ≥2 wadah terpisah dan isi tiap wadah sesuai jenisnya (paling banyak 1–2 benda kecil " +
						"salah tempat). Sebagian: ada wadah terpisah tetapi isinya masih banyak tercampur, ATAU hanya sebagian " +
						"jenis yang dipisah (misal plastik saja dipisah, sisanya campur). Tercampur: semua sampah dalam satu " +
						"wadah/kantong, atau wadah terpisah tetapi isinya sama-sama campur. Tidak bisa dinilai: isi tidak " +
						"terlihat (kantong terikat, tertutup), foto buram, atau tidak ada foto. Ini label lapangan (label awal). " +
						"Label final ditetapkan kemudian oleh 2 pelabel dan DPL.",
					Options: []Option{
						{ID: "sorted", Label: "Terpilah"}, {ID: "partial", Label: "Sebagian"}, {ID: "mixed", Label: "Tercampur"}, {ID: "unknown", Label: "Tidak bisa dinilai"},
					}},
				{ID: "r3_visible", Label: "Isi wadah terlihat di foto?", Type: TypeChoice, Required: true, Options: []Option{
					{ID: "clear", Label: "Ya, terlihat jelas"}, {ID: "partial", Label: "Sebagian terlihat"}, {ID: "hidden", Label: "Tidak terlihat (tertutup/kantong terikat)"},
				}},
				{ID: "r3_containers", Label: "Jumlah wadah yang terlihat di foto", Type: TypeInt, Min: intp(0), Max: intp(20)},
				{ID: "r3_container_types", Label: "Jenis wadah di foto", Type: TypeMulti, Options: []Option{
					{ID: "lidded", Label: "Tempat sampah bertutup"}, {ID: "bucket", Label: "Ember"}, {ID: "plastic_bag", Label: "Kantong plastik"},
					{ID: "sack", Label: "Karung"}, {ID: "cardboard", Label: "Kardus"}, {ID: "other", Label: "Lainnya", Other: true},
				}},
				{ID: "r3_location", Label: "Lokasi wadah", Type: TypeChoice, Options: []Option{
					{ID: "indoor", Label: "Dalam rumah/dapur"}, {ID: "porch", Label: "Teras/depan rumah"}, {ID: "other", Label: "Lainnya"},
				}},
				{ID: "r3_lighting", Label: "Pencahayaan saat foto", Type: TypeChoice, Options: []Option{
					{ID: "bright", Label: "Terang (siang/luar ruangan)"}, {ID: "dim", Label: "Redup/dalam ruangan"}, {ID: "flash", Label: "Pakai flash"},
				}},
			},
		},
	},
}
