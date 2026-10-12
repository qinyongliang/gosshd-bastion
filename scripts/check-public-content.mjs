import { execFileSync } from "node:child_process";
const paths = execFileSync("git", ["ls-files", "-z"], { encoding: "utf8" }).split("\0").filter(Boolean);
const privatePaths = [/^\.github\/workflows\/deploy-arm64\.yml$/, /^\.learnings\//, /^docs\/superpowers\//, /^design\/qa\//, /^local-only\//, /^docs\/.*-deploy(?:ment)?.*\.md$/, /^docs\/http-forward-load-results\.md$/];
const forbidden = paths.filter(path => privatePaths.some(pattern => pattern.test(path)));
if (forbidden.length) {
  console.error("Private operational material must not be tracked:", forbidden.join("\n"));
  process.exit(1);
}
console.log("Public-content path check passed.");
