import type { SurveyForm } from "../api/types";

// Fixture kecil berbentuk respons GET /survey/form (spec-pendataan §5.1); kasus meniru test tabel T60.
const yesNo = [{ id: "yes", label: "Ya" }, { id: "no", label: "Tidak" }];
export const surveyForm: SurveyForm = {
  version: 1,
  consent_text: "Data dan foto tempat sampah …",
  sections: [
    {
      id: "A", title: "A. Identitas Pendataan", help: "Gunakan inisial.", questions: [
        { id: "a_date", label: "Tanggal pendataan", type: "date", required: true },
        { id: "a_rw", label: "RW", type: "text", required: true, max: 10 },
        { id: "a_consent", label: "Persetujuan pendataan", type: "choice", required: true, options: [{ id: "yes", label: "Bersedia" }, { id: "no", label: "Tidak bersedia" }] },
      ],
    },
    {
      id: "B1", title: "1. Pengetahuan", questions: [
        { id: "b1", label: "Apakah keluarga mengetahui pentingnya memilah sampah dari rumah?", type: "choice", required: true, options: yesNo },
        { id: "b7", label: "Apakah keluarga memisahkan sampah residu?", type: "choice", required: true, options: yesNo },
        { id: "b_note_1", label: "Keterangan butir B1–B8", type: "textarea", required: false },
      ],
    },
    {
      id: "R2", title: "R2. Data rumah tangga", questions: [
        { id: "r2_occupants", label: "Jumlah penghuni rumah", type: "int", required: false, min: 1, max: 50 },
      ],
    },
    {
      id: "R3", title: "R3. Foto dan label wadah", questions: [
        {
          id: "r3_container_types", label: "Jenis wadah di foto", type: "multi", required: false, options: [
            { id: "lidded", label: "Tempat sampah bertutup" }, { id: "bucket", label: "Ember" }, { id: "other", label: "Lainnya", other: true },
          ],
        },
      ],
    },
  ],
};
