export type InstallPlatform = "windows" | "linux" | "macos" | "android";

export function detectInstallPlatform(userAgent: string): InstallPlatform | null {
  if (/android/i.test(userAgent)) return "android";
  if (/iphone|ipad|ipod|macintosh.*mobile/i.test(userAgent)) return null;
  if (/windows/i.test(userAgent)) return "windows";
  if (/macintosh|mac os x/i.test(userAgent)) return "macos";
  if (/linux/i.test(userAgent)) return "linux";
  return null;
}
