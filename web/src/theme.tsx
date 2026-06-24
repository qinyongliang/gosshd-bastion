import { createContext, ReactNode, useContext, useEffect, useMemo, useState } from "react";

type Theme = "light" | "dark";
const storageKey = "gosshd_theme";

type ThemeValue = {
  theme: Theme;
  setTheme: (theme: Theme) => void;
};

const ThemeContext = createContext<ThemeValue | null>(null);

function systemTheme(): Theme {
  return window.matchMedia?.("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, updateTheme] = useState<Theme>(() => {
    const stored = window.localStorage.getItem(storageKey);
    return stored === "dark" || stored === "light" ? stored : systemTheme();
  });

  useEffect(() => {
    const media = window.matchMedia?.("(prefers-color-scheme: dark)");
    if (!media) return;
    const onChange = () => {
      const stored = window.localStorage.getItem(storageKey);
      if (stored !== "dark" && stored !== "light") updateTheme(systemTheme());
    };
    media.addEventListener("change", onChange);
    return () => media.removeEventListener("change", onChange);
  }, []);

  useEffect(() => {
    document.documentElement.dataset.theme = theme;
  }, [theme]);

  const value = useMemo<ThemeValue>(() => ({
    theme,
    setTheme(next) {
      window.localStorage.setItem(storageKey, next);
      updateTheme(next);
    },
  }), [theme]);

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

export function useTheme() {
  const context = useContext(ThemeContext);
  if (!context) throw new Error("useTheme must be used within ThemeProvider");
  return context;
}
