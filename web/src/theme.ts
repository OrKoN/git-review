export type ColorScheme = "system" | "light" | "dark";

const storageKey = "git-review-color-scheme";

export function loadColorScheme(): ColorScheme {
  const value = localStorage.getItem(storageKey);
  return value === "light" || value === "dark" ? value : "system";
}

export function applyColorScheme(scheme: ColorScheme): void {
  document.documentElement.dataset.scheme = scheme;
  if (scheme === "system") localStorage.removeItem(storageKey);
  else localStorage.setItem(storageKey, scheme);
}
