export type DiffLine = {
  type: "add" | "del" | "context" | "meta";
  raw: string;
  text: string;
  oldNo?: number | null;
  newNo?: number | null;
  ordinal?: number;
};

export type DiffHunk = {
  index: number;
  header: string;
  lines: DiffLine[];
};

export type DiffSummary = {
  additions: number;
  deletions: number;
  hunks: number;
};
