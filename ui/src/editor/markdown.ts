// Markdown editing helpers for the editor's <textarea>. Every action writes plain Markdown at the caret.
// Media follows docs/media-and-discovery.md: images ![alt](/media/id), video = a standalone link [title](/media/id).

export type MarkdownAction =
  | 'h2'
  | 'h3'
  | 'bold'
  | 'italic'
  | 'code'
  | 'link'
  | 'quote'
  | 'ul'
  | 'ol'
  | 'codeblock'
  | 'table'
  | 'hr';

export interface EditResult {
  value: string;
  selectionStart: number;
  selectionEnd: number;
}

const PLACEHOLDER: Record<string, string> = {
  bold: 'bold text',
  italic: 'italic text',
  code: 'code',
  link: 'link text',
};

/** Pure function: returns the new text and selection. Easy to unit-test; apply it with applyEdit(). */
export function markdownEdit(
  value: string,
  start: number,
  end: number,
  action: MarkdownAction,
  table = '| Column | Column |\n| --- | --- |\n| Cell | Cell |',
): EditResult {
  const sel = value.slice(start, end);
  const replace = (
    text: string,
    selFrom: number,
    selTo: number,
    from = start,
    to = end,
  ): EditResult => ({
    value: value.slice(0, from) + text + value.slice(to),
    selectionStart: from + selFrom,
    selectionEnd: from + selTo,
  });
  const wrap = (mark: string, ph: string) => {
    const inner = sel || ph;
    return replace(mark + inner + mark, mark.length, mark.length + inner.length);
  };
  const lines = (prefix: (i: number) => string) => {
    const ls = value.lastIndexOf('\n', start - 1) + 1;
    let le = value.indexOf('\n', end);
    if (le < 0) {
      le = value.length;
    }
    const block = value
      .slice(ls, le)
      .split('\n')
      .map((l, i) => prefix(i) + l.replace(/^(#{1,3}\s|>\s|[-*]\s|\d+\.\s)/, ''))
      .join('\n');
    return replace(block, 0, block.length, ls, le);
  };
  const block = (text: string) => {
    const pre = start > 0 && value[start - 1] !== '\n' ? '\n\n' : '';
    return replace(pre + text + '\n', pre.length, pre.length + text.length);
  };
  switch (action) {
    case 'h2':
      return lines(() => '## ');
    case 'h3':
      return lines(() => '### ');
    case 'quote':
      return lines(() => '> ');
    case 'ul':
      return lines(() => '- ');
    case 'ol':
      return lines((i) => `${i + 1}. `);
    case 'bold':
      return wrap('**', PLACEHOLDER.bold);
    case 'italic':
      return wrap('*', PLACEHOLDER.italic);
    case 'code':
      return wrap('`', PLACEHOLDER.code);
    case 'link': {
      const txt = sel || PLACEHOLDER.link;
      return replace(`[${txt}](https://)`, txt.length + 3, txt.length + 11);
    }
    case 'codeblock':
      return block('```ts\n' + (sel || '// code') + '\n```');
    case 'table':
      return block(table);
    case 'hr':
      return block('---');
  }
}

export const imageMarkdown = (mediaId: string, alt: string) => `![${alt}](/media/${mediaId})`;

export const videoMarkdown = (mediaId: string, title: string) => `[${title}](/media/${mediaId})`;

/** Applies an edit to a textarea, keeping the browser's undo history where supported. */
export function applyEdit(el: HTMLTextAreaElement, edit: EditResult) {
  el.focus();
  el.select();
  const ok =
    typeof document.execCommand === 'function' &&
    document.execCommand('insertText', false, edit.value);
  if (!ok) {
    el.value = edit.value;
  }
  el.setSelectionRange(edit.selectionStart, edit.selectionEnd);
  el.dispatchEvent(new Event('input', { bubbles: true }));
}

/** Conservative whitespace-based estimate matching saved revision metadata. */
export function editorStatistics(markdown: string): { words: number; minutes: number } {
  const words = markdown.trim() ? markdown.trim().split(/\s+/u).length : 0;
  return { words, minutes: Math.ceil(words / 200) };
}
