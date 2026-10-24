import { faImages, faTags } from "@fortawesome/free-solid-svg-icons";
import { Link } from "react-router-dom";
import { Icon } from "../../components/Icon";
import { usePageTitle } from "./context";

/** Beranda dashboard. Status sistem dan ringkasan pekerjaan menyusul (T51). */
export function HomePage() {
  usePageTitle("Beranda");
  return (
    <div className="admin-page">
      <h1>Beranda</h1>
      <div className="row">
        <Link className="btn" to="/admin/anotasi"><Icon icon={faImages} /> Anotasi</Link>
        <Link className="btn" to="/admin/label"><Icon icon={faTags} /> Label</Link>
      </div>
    </div>
  );
}
