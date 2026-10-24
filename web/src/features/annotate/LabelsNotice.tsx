import { faRotate } from "@fortawesome/free-solid-svg-icons";
import type { Label } from "../../api/types";
import { Icon } from "../../components/Icon";
import { Banner } from "../../components/ui";

/** Status daftar label di layar anotasi: gagal dimuat (dengan Coba lagi) atau kosong. */
export function LabelsNotice({ labels, error, onRetry }: { labels?: Label[]; error?: string; onRetry(): void }) {
  if (error) {
    return (
      <Banner kind="error">
        Daftar label gagal dimuat: {error}{" "}
        <button type="button" className="btn btn-small" onClick={onRetry}><Icon icon={faRotate} /> Coba lagi</button>
      </Banner>
    );
  }
  if (labels?.length === 0) return <Banner kind="warn">Belum ada label. Hubungi admin.</Banner>;
  return null;
}
