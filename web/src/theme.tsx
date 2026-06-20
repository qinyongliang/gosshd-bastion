import { createContext, ReactNode, useContext, useMemo, useState } from "react";

type Theme = "light" | "dark";
const storageKey = "gosshd_theme";

type ThemeValue = {
  theme: Theme;
  setTheme: (theme: Theme) => void;
};

const ThemeContext = createContext<ThemeValue | null>(null);

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, updateTheme] = useState<Theme>(() => {
    const stored = window.localStorage.getItem(storageKey);
    return stored === "dark" ? "dark" : "light";
  });

  const value = useMemo<ThemeValue>(() => ({
    theme,
    setTheme(next) {
      window.localStorage.setItem(storageKey, next);
      document.documentElement.dataset.theme = next;
      updateTheme(next);
    },
  }), [theme]);

  document.documentElement.dataset.theme = theme;
  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

export function useTheme() {
  const context = useContext(ThemeContext);
  if (!context) throw new Error("useTheme must be used within ThemeProvider");
  return context;
}
