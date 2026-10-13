// Keep Windows paths absolute for SFTP, even when a prompt reports C:\... .
export function normalizeRemotePath(value: string): string {
  value = value.replace(/\\/g, "/");
  const drive = value.match(/^\/?([A-Za-z]:)(?:\/|$)(.*)$/);
  if (drive) {
    const suffix = normalizeRemotePath(`/${drive[2] || ""}`).replace(/^\//, "");
    return `/${drive[1].toUpperCase()}/${suffix}`;
  }
  const unc = value.startsWith("//");
  const absolute = value.startsWith("/");
  const parts: string[] = [];
  for (const part of value.split("/")) {
    if (!part || part === ".") continue;
    if (part === "..") {
      if (parts.length > (unc ? 2 : 0)) parts.pop();
      continue;
    }
    parts.push(part);
  }
  const next = parts.join("/");
  return absolute ? `${unc ? "//" : "/"}${next}` : next || ".";
}

export function remoteJoin(dir: string, name: string): string {
  if (!name) return normalizeRemotePath(dir);
  if (/^[A-Za-z]:[/\\]?/.test(name) || /^[\/\\]/.test(name)) return normalizeRemotePath(name);
  if (!dir || dir === ".") return normalizeRemotePath(name);
  return normalizeRemotePath(`${dir.replace(/[\/\\]+$/, "")}/${name}`);
}

export function remoteParent(path: string): string {
  const normalized = normalizeRemotePath(path);
  if (/^\/[A-Za-z]:\/$/.test(normalized)) return normalized;
  const trimmed = normalized.replace(/\/$/, "");
  if (trimmed.startsWith("//") && trimmed.split("/").filter(Boolean).length <= 2) return trimmed;
  const index = trimmed.lastIndexOf("/");
  if (index === 3 && /^\/[A-Za-z]:/.test(trimmed)) return `${trimmed.slice(0, 3)}/`;
  if (index <= 0) return "/";
  return trimmed.slice(0, index);
}
