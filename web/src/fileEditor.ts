export const FALLBACK_EDITOR_LANGUAGE = "gosshd-config-fallback";

const plainTextNames = new Set([
  "readme", "license", "copying", "changelog", "authors", "notice",
  ".gitignore", ".gitattributes", ".dockerignore", ".editorconfig", ".npmrc", ".yarnrc", ".nvmrc",
]);
const plainTextExtension = /\.(txt|text|log|csv|tsv|rst|adoc|diff|patch)$/;

export function inferEditorLanguage(path: string) {
  const normalized = path.replace(/\\/g, "/").toLowerCase();
  const filename = normalized.split("/").pop() || normalized;
  if (plainTextNames.has(filename) || plainTextExtension.test(filename)) return "plaintext";
  if (/(^|\/)nginx(\/|$)/.test(normalized) || filename === "nginx.conf" || filename.endsWith(".nginx") || filename.endsWith(".nginx.conf")) return "nginx";
  if (filename === "dockerfile" || filename.startsWith("dockerfile.")) return "dockerfile";
  if (filename === "compose.yaml" || filename === "compose.yml" || filename === "docker-compose.yaml" || filename === "docker-compose.yml") return "yaml";
  if (filename === "makefile" || filename === "gnumakefile") return "shell";
  if (filename === "crontab" || filename === "hosts" || filename === "sudoers") return "shell";
  if (filename === ".gitconfig" || filename === ".gitmodules" || filename === "gitconfig") return "ini";
  if (filename.endsWith(".service") || filename.endsWith(".timer") || filename.endsWith(".socket") || filename.endsWith(".mount") || filename.endsWith(".target")) return "ini";
  if (filename === ".env" || filename.endsWith(".env") || filename.startsWith(".env.")) return "shell";
  if (filename.endsWith(".properties") || filename.endsWith(".propertis") || filename.endsWith(".props")) return "ini";
  if (filename.endsWith(".conf") || filename.endsWith(".config") || filename.endsWith(".cfg") || filename.endsWith(".cnf") || filename.endsWith(".ini")) return "ini";
  if (filename.endsWith(".yaml") || filename.endsWith(".yml")) return "yaml";
  if (filename.endsWith(".json") || filename.endsWith(".jsonc")) return "json";
  if (filename.endsWith(".toml")) return "ini";
  if (filename.endsWith(".hcl") || filename.endsWith(".tf") || filename.endsWith(".tfvars")) return "hcl";
  if (filename.endsWith(".xml") || filename.endsWith(".svg")) return "xml";
  if (filename.endsWith(".html") || filename.endsWith(".htm")) return "html";
  if (filename.endsWith(".vue") || filename.endsWith(".svelte")) return "html";
  if (filename.endsWith(".css")) return "css";
  if (filename.endsWith(".scss")) return "scss";
  if (filename.endsWith(".less")) return "less";
  if (filename.endsWith(".js") || filename.endsWith(".mjs") || filename.endsWith(".cjs")) return "javascript";
  if (filename.endsWith(".ts") || filename.endsWith(".tsx")) return "typescript";
  if (filename.endsWith(".jsx")) return "javascript";
  if (filename.endsWith(".sh") || filename.endsWith(".bash") || filename.endsWith(".zsh") || filename.endsWith(".profile") || filename.endsWith(".bashrc") || filename.endsWith(".zshrc")) return "shell";
  if (filename.endsWith(".ps1") || filename.endsWith(".psm1")) return "powershell";
  if (filename.endsWith(".py")) return "python";
  if (filename.endsWith(".go")) return "go";
  if (filename.endsWith(".rs")) return "rust";
  if (filename.endsWith(".java")) return "java";
  if (filename.endsWith(".kt") || filename.endsWith(".kts")) return "kotlin";
  if (filename.endsWith(".c") || filename.endsWith(".h") || filename.endsWith(".cc") || filename.endsWith(".cpp") || filename.endsWith(".cxx") || filename.endsWith(".hpp")) return "cpp";
  if (filename.endsWith(".cs")) return "csharp";
  if (filename.endsWith(".php")) return "php";
  if (filename.endsWith(".rb")) return "ruby";
  if (filename.endsWith(".lua")) return "lua";
  if (filename.endsWith(".pl") || filename.endsWith(".pm")) return "perl";
  if (filename.endsWith(".r")) return "r";
  if (filename.endsWith(".swift")) return "swift";
  if (filename.endsWith(".scala")) return "scala";
  if (filename.endsWith(".dart")) return "dart";
  if (filename.endsWith(".ex") || filename.endsWith(".exs")) return "elixir";
  if (filename.endsWith(".sol")) return "solidity";
  if (filename.endsWith(".proto")) return "protobuf";
  if (filename.endsWith(".graphql") || filename.endsWith(".gql")) return "graphql";
  if (filename.endsWith(".sql")) return "sql";
  if (filename.endsWith(".md") || filename.endsWith(".markdown")) return "markdown";
  return FALLBACK_EDITOR_LANGUAGE;
}

export function isTextFile(path: string): boolean {
  // Classify by basename: a binary file inside an nginx directory is still binary.
  const filename = path.replace(/\\/g, "/").split("/").pop() || "";
  return inferEditorLanguage(filename) !== FALLBACK_EDITOR_LANGUAGE;
}
