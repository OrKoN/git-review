import type { RepositoryFile } from "./app-model.ts";

export interface FileTreeDirectory {
  kind: "directory";
  name: string;
  path: string;
  directories: FileTreeDirectory[];
  files: RepositoryFile[];
}

export const fileName = (file: RepositoryFile) => file.path.slice(file.path.lastIndexOf("/") + 1);

export function buildFileTree(files: RepositoryFile[]): FileTreeDirectory {
  const root: FileTreeDirectory = { kind: "directory", name: "", path: "", directories: [], files: [] };
  for (const file of files) {
    const parts = file.path.split("/");
    let directory = root;
    for (const name of parts.slice(0, -1)) {
      let child = directory.directories.find((item) => item.name === name);
      if (!child) {
        child = {
          kind: "directory",
          name,
          path: directory.path ? `${directory.path}/${name}` : name,
          directories: [],
          files: [],
        };
        directory.directories.push(child);
      }
      directory = child;
    }
    directory.files.push(file);
  }
  const sort = (directory: FileTreeDirectory) => {
    directory.directories.sort((left, right) => left.name.localeCompare(right.name));
    directory.files.sort((left, right) => fileName(left).localeCompare(fileName(right)));
    directory.directories.forEach(sort);
  };
  sort(root);
  return root;
}
