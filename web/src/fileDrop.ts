export type UploadItem = { file: File; relativePath: string };
export type UploadPlan = { files: UploadItem[]; directories: string[] };
export type DropEntry = {
  name: string;
  isFile: boolean;
  isDirectory: boolean;
  file?: (success: (file: File) => void, error: (error: DOMException) => void) => void;
  createReader?: () => { readEntries: (success: (entries: DropEntry[]) => void, error: (error: DOMException) => void) => void };
};
export type DropSource = { entry: DropEntry } | { file: File };

// Drag data is only readable during the drop event. Capture it before awaiting traversal.
export function snapshotDrop(data: DataTransfer): DropSource[] {
  const sources: DropSource[] = [];
  for (const item of Array.from(data.items)) {
    if (item.kind !== "file") continue;
    const entry = item.webkitGetAsEntry?.();
    if (entry) sources.push({ entry });
    else {
      const file = item.getAsFile();
      if (file) sources.push({ file });
    }
  }
  return sources.length ? sources : Array.from(data.files, (file) => ({ file }));
}

export function validateRelativePath(path: string): string {
  if (!path || path.includes("\\") || path.includes("\0") || /^[A-Za-z]:/.test(path)
    || path.split("/").some((part) => !part || part === "." || part === "..")) {
    throw new Error("Invalid upload path: " + path);
  }
  return path;
}

export function joinUploadPath(root: string, relative: string): string {
  validateRelativePath(relative);
  return `${root.replace(/\\/g, "/").replace(/\/+$/, "")}/${relative}`;
}

export function filesUploadPlan(files: File[]): UploadPlan {
  return { files: files.map((file) => ({ file, relativePath: validateRelativePath(file.name) })), directories: [] };
}

export async function collectDrop(sources: DropSource[], signal: AbortSignal): Promise<UploadPlan> {
  const files: UploadItem[] = [];
  const directories = new Set<string>();
  const paths = new Set<string>();
  const claim = (path: string) => {
    validateRelativePath(path);
    if (paths.has(path)) throw new Error("Duplicate upload path: " + path);
    paths.add(path);
  };
  const walk = async (entry: DropEntry, parent = "") => {
    signal.throwIfAborted();
    // Entry names are single components, even on platforms accepting unusual file names.
    if (entry.name.includes("/")) throw new Error("Invalid upload name: " + entry.name);
    const path = parent ? `${parent}/${entry.name}` : entry.name;
    claim(path);
    if (entry.isDirectory && entry.createReader) {
      directories.add(path);
      const reader = entry.createReader();
      for (;;) {
        signal.throwIfAborted();
        const children = await new Promise<DropEntry[]>((resolve, reject) => reader.readEntries(resolve, reject));
        if (!children.length) break;
        for (const child of children) await walk(child, path);
      }
    } else if (entry.isFile && entry.file) {
      const file = await new Promise<File>((resolve, reject) => entry.file!(resolve, reject));
      signal.throwIfAborted();
      if (file.name !== entry.name) throw new Error("Upload entry name changed: " + path);
      files.push({ file, relativePath: path });
    } else {
      throw new Error("Cannot read dropped entry: " + path);
    }
  };
  for (const source of sources) {
    signal.throwIfAborted();
    if ("entry" in source) await walk(source.entry);
    else {
      claim(source.file.name);
      files.push({ file: source.file, relativePath: source.file.name });
    }
  }
  signal.throwIfAborted();
  return { files, directories: [...directories] };
}
