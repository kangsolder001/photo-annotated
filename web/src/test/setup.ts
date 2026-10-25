import { configure } from "@testing-library/react";

// Halaman admin dimuat lazy; saat seluruh suite berjalan paralel, batas bawaan findBy/waitFor (1 detik)
// kadang habis sebelum chunk dan data palsu selesai dimuat.
configure({ asyncUtilTimeout: 5000 });
