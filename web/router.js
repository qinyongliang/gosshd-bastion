import { setRoute, state } from "./state.js";

export const userRoutes = ["dashboard", "orgs", "org-admin", "keys", "targets", "agents", "policies", "audit"];
export const adminRoutes = ["system-admin"];

export function routeFromLocation() {
  const route = window.location.pathname.replace(/^\/+/, "").split("/")[0] || "dashboard";
  return [...userRoutes, ...adminRoutes].includes(route) ? route : "dashboard";
}

export function navigate(route) {
  const next = route || "dashboard";
  if (state.route === next && window.location.pathname === `/${next}`) return;
  window.history.pushState({}, "", next === "dashboard" ? "/" : `/${next}`);
  setRoute(next);
}

export function bindRouter(onChange) {
  window.addEventListener("popstate", () => {
    setRoute(routeFromLocation());
    onChange();
  });
}
