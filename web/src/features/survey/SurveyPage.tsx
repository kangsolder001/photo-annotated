import { faRotate } from "@fortawesome/free-solid-svg-icons/faRotate";
import { Icon } from "../../components/Icon";
import { Banner, Spinner } from "../../components/ui";
import { SurveyWizard } from "./SurveyWizard";
import { useSurveyForm } from "./useSurveyForm";

/** Pendataan baru satu rumah tangga (spec-pendataan §3.2). */
export function SurveyPage() {
  const { form, error, reload } = useSurveyForm();
  return (
    <>
      <h1>Pendataan baru</h1>
      {error ? (
        <Banner kind="error">
          Formulir gagal dimuat: {error}{" "}
          <button type="button" className="btn btn-small" onClick={() => void reload()}><Icon icon={faRotate} /> Coba lagi</button>
        </Banner>
      ) : !form ? (
        <Spinner label="Memuat formulir" />
      ) : (
        <SurveyWizard form={form} />
      )}
    </>
  );
}
