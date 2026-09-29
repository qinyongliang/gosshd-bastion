import type { Organization, Target } from "./types";

export const tagPalette = ["gray", "red", "orange", "yellow", "green", "blue", "purple"] as const;

export function ownerFromOrg(org: Organization | undefined) {
  return org ? { owner_type: "organization" as const, owner_id: org.id } : undefined;
}

export function targetEndpoint(target: Target) {
  const user = target.remote_username || "ssh";
  const host = target.host || "private-node";
  const port = target.port || 22;
  return `${user}@${host}:${port}`;
}

export function splitTags(value: string) {
  return value
    .split(/[,\n，]/)
    .map((item) => item.trim())
    .filter(Boolean);
}

export function tagColor(tag: string, colors?: Record<string, string>) {
  return colors?.[tag] || tagPalette[Math.abs(hash(tag)) % tagPalette.length];
}

export async function copyText(value: string) {
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(value);
      return;
    } catch {
      // The fallback also works when clipboard permissions reject writeText.
    }
  }
  const textarea = document.createElement("textarea");
  textarea.value = value;
  textarea.style.position = "fixed";
  textarea.style.opacity = "0";
  document.body.appendChild(textarea);
  textarea.focus();
  textarea.select();
  const copied = document.execCommand("copy");
  textarea.remove();
  if (!copied) throw new Error("Clipboard copy failed");
}

function hash(value: string) {
  let out = 0;
  for (let index = 0; index < value.length; index += 1) out = (out << 5) - out + value.charCodeAt(index);
  return out;
}
