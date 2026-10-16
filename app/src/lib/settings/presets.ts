// Country presets for Power's tariff, ported verbatim from the Python
// widget (power-monitor.py COUNTRY_PRESETS): all-in marginal household rates
// — energy, distribution, levies and VAT — from Eurostat 2025-S2 band DC
// (taxes included, published 2026-08-12) and national regulators, where the
// regulator's all-in figure wins (Poland: URE G-11; UK: the Ofgem cap).
// Approximations: tariffs vary by supplier, region and contract, so every
// value stays editable. Applying one sets all five (the Python applied only
// the rate and the currency).
export interface Preset { country: string; currency: string; flat: number; peak: number; offPeak: number; tou: boolean }

export const presets: Preset[] = [
  { country: "Poland", currency: "PLN", flat: 1.30, peak: 1.45, offPeak: 0.90, tou: true },
  { country: "Germany", currency: "EUR", flat: 0.29, peak: 0.33, offPeak: 0.24, tou: false },
  { country: "France", currency: "EUR", flat: 0.32, peak: 0.35, offPeak: 0.23, tou: true },
  { country: "UK", currency: "GBP", flat: 0.26, peak: 0.31, offPeak: 0.11, tou: true },
  { country: "USA", currency: "USD", flat: 0.18, peak: 0.30, offPeak: 0.11, tou: false },
  { country: "Japan", currency: "JPY", flat: 31.0, peak: 36.0, offPeak: 18.0, tou: true },
  { country: "Canada", currency: "CAD", flat: 0.17, peak: 0.20, offPeak: 0.10, tou: true },
  { country: "Australia", currency: "AUD", flat: 0.28, peak: 0.40, offPeak: 0.18, tou: true },
  { country: "Spain", currency: "EUR", flat: 0.26, peak: 0.30, offPeak: 0.15, tou: true },
  { country: "Netherlands", currency: "EUR", flat: 0.40, peak: 0.43, offPeak: 0.34, tou: true },
  { country: "Italy", currency: "EUR", flat: 0.30, peak: 0.34, offPeak: 0.25, tou: true },
  { country: "Belgium", currency: "EUR", flat: 0.35, peak: 0.39, offPeak: 0.27, tou: true },
  { country: "Austria", currency: "EUR", flat: 0.27, peak: 0.30, offPeak: 0.22, tou: false },
  { country: "Ireland", currency: "EUR", flat: 0.39, peak: 0.43, offPeak: 0.25, tou: true },
  { country: "Portugal", currency: "EUR", flat: 0.24, peak: 0.28, offPeak: 0.16, tou: true },
  { country: "Sweden", currency: "SEK", flat: 2.90, peak: 3.30, offPeak: 2.20, tou: true },
  { country: "Denmark", currency: "DKK", flat: 2.10, peak: 2.40, offPeak: 1.60, tou: true },
  { country: "Finland", currency: "EUR", flat: 0.26, peak: 0.30, offPeak: 0.18, tou: true },
  { country: "Norway", currency: "NOK", flat: 2.05, peak: 2.40, offPeak: 1.50, tou: true },
  { country: "Czechia", currency: "CZK", flat: 4.20, peak: 4.70, offPeak: 3.10, tou: true },
  { country: "Greece", currency: "EUR", flat: 0.24, peak: 0.28, offPeak: 0.17, tou: true },
  { country: "Romania", currency: "RON", flat: 1.30, peak: 1.45, offPeak: 1.00, tou: false },
  { country: "Hungary", currency: "HUF", flat: 43.0, peak: 48.0, offPeak: 32.0, tou: true },
  { country: "Switzerland", currency: "CHF", flat: 0.30, peak: 0.34, offPeak: 0.23, tou: true },
];
