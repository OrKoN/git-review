import { RangeSetBuilder } from "@codemirror/state";
import { type EditorView, GutterMarker, gutter } from "@codemirror/view";
import type { CommentAnchor, ReviewComment, Selection } from "./app-model";

class CommentMarker extends GutterMarker {
  constructor(private readonly count: number) {
    super();
  }

  toDOM() {
    const marker = document.createElement("span");
    marker.className = "source-comment-count";
    marker.textContent = String(this.count);
    marker.title = `${this.count} comment${this.count === 1 ? "" : "s"}`;
    return marker;
  }
}

class AddCommentMarker extends GutterMarker {
  toDOM() {
    const marker = document.createElement("span");
    marker.className = "source-comment-add";
    marker.textContent = "+";
    marker.title = "Add line comment";
    return marker;
  }
}

const addCommentMarker = new AddCommentMarker();

export function commentGutter(
  comments: ReviewComment[],
  selection: Selection,
  openComment: (anchor: CommentAnchor) => void,
) {
  return gutter({
    class: "cm-comment-gutter",
    lineMarker: () => addCommentMarker,
    markers: (view: EditorView) => {
      const builder = new RangeSetBuilder<GutterMarker>();
      const counts = new Map<number, number>();
      for (const comment of comments)
        if (comment.path === selection?.path && comment.area === "file" && comment.line > 0 && !comment.outdated)
          counts.set(comment.line, (counts.get(comment.line) || 0) + 1);
      for (const [number, count] of [...counts].sort((a, b) => a[0] - b[0]))
        if (number <= view.state.doc.lines) {
          const position = view.state.doc.line(number).from;
          builder.add(position, position, new CommentMarker(count));
        }
      return builder.finish();
    },
    domEventHandlers: {
      mousedown: (view, line) => {
        const number = view.state.doc.lineAt(line.from).number;
        openComment({
          path: selection?.path || "",
          area: "file",
          side: "new",
          line: number,
          context: view.state.doc.line(number).text,
        });
        return true;
      },
    },
  });
}
