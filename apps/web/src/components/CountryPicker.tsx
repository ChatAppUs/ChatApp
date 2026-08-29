"use client";

import { useEffect, useMemo, useState } from "react";
import { api } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import type { Country } from "@/lib/types";

export default function CountryPicker({
  value,
  onChange,
}: {
  value: string; // dial code, e.g. "+1"
  onChange: (dial: string, iso: string) => void;
}) {
  const { t } = useI18n();
  const [countries, setCountries] = useState<Country[]>([]);
  const [filter, setFilter] = useState("");

  useEffect(() => {
    api<{ countries: Country[] }>("/api/countries", {}, false)
      .then((d) => setCountries(d.countries))
      .catch(() => {});
  }, []);

  const filtered = useMemo(() => {
    const f = filter.toLowerCase();
    if (!f) return countries;
    return countries.filter(
      (c) => c.name.toLowerCase().includes(f) || c.dial.includes(f) || c.iso.toLowerCase().includes(f)
    );
  }, [countries, filter]);

  return (
    <div className="col">
      <input
        placeholder={t("selectCountry")}
        value={filter}
        onChange={(e) => setFilter(e.target.value)}
      />
      <select
        value={value}
        onChange={(e) => {
          const c = countries.find((x) => x.dial === e.target.value);
          onChange(e.target.value, c?.iso ?? "");
        }}
        size={6}
      >
        {filtered.map((c) => (
          <option key={c.iso} value={c.dial}>
            {c.flag} {c.name} ({c.dial})
          </option>
        ))}
      </select>
    </div>
  );
}
