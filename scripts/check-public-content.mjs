import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
const paths = execFileSync("git", ["ls-files", "-z"], { encoding: "utf8" }).split("\0").filter(Boolean);
const privatePaths = [/^\.github\/workflows\/deploy-arm64\.yml$/, /^\.learnings\//, /^docs\/superpowers\//, /^design\/qa\//, /^local-only\//, /^docs\/.*-deploy(?:ment)?.*\.md$/, /^docs\/http-forward-load-results\.md$/];
const forbidden = paths.filter(path => privatePaths.some(pattern => pattern.test(path)) || /\.(?:keystore|jks|p12|pfx)$/i.test(path));
if (forbidden.length) {
  console.error("Private operational material must not be tracked:", forbidden.join("\n"));
  process.exit(1);
}
const privateKeyFiles = paths.filter(path => {
  try { return /-----BEGIN (?:RSA |EC |OPENSSH |ENCRYPTED )?PRIVATE KEY-----/.test(readFileSync(path, "utf8")); }
  catch { return false; }
});
if (privateKeyFiles.length) {
  console.error("Private key content must not be tracked:", privateKeyFiles.join("\n"));
  process.exit(1);
}
console.log("Public-content path and private-key checks passed.");
