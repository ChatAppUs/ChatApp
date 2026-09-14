// Client access to the platform's feature-flag and experiment planes (§74/§75).
// Flags evaluate server-side with a stable per-user bucket, so a caller always
// observes the same variant for a given flag across sessions and devices.
import { useEffect, useState } from "react";
import { api } from "./api";

export interface EvaluatedFlag {
  key: string;
  enabled: boolean;
  description: string;
  on: boolean;
}

export interface EvaluatedExperiment {
  key: string;
  flag: string;
  variant: "on" | "off";
}

export function useFlags(region = "", platform = "web") {
  const [flags, setFlags] = useState<EvaluatedFlag[]>([]);
  useEffect(() => {
    const q = `?platform=${encodeURIComponent(platform)}${region ? `&region=${encodeURIComponent(region)}` : ""}`;
    api<{ flags: EvaluatedFlag[] }>(`/api/me/flags${q}`)
      .then((d) => setFlags(d.flags))
      .catch(() => {});
  }, [region, platform]);
  return flags;
}

export function useExperiments() {
  const [experiments, setExperiments] = useState<EvaluatedExperiment[]>([]);
  useEffect(() => {
    api<{ experiments: EvaluatedExperiment[] }>("/api/me/experiments")
      .then((d) => setExperiments(d.experiments))
      .catch(() => {});
  }, []);
  return experiments;
}

export function experimentOn(experiments: EvaluatedExperiment[], key: string): boolean {
  return experiments.find((e) => e.key === key)?.variant === "on";
}
