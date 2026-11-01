import type { RefObject } from 'react';
import { Icon, type IconName } from '../components/Icon';
import { applyEdit, markdownEdit, type MarkdownAction } from './markdown';

const GROUPS: (MarkdownAction | 'image' | 'video')[][] = [
  ['h2', 'h3'],
  ['bold', 'italic', 'code', 'link'],
  ['quote', 'ul', 'ol', 'codeblock', 'table', 'hr'],
  ['image', 'video'],
];
const ICON: Partial<Record<string, IconName>> = {
  bold: 'bold',
  italic: 'italic',
  code: 'code',
  link: 'link',
  quote: 'quote',
  ul: 'ul',
  ol: 'ol',
  codeblock: 'codeblock',
  table: 'table',
  hr: 'hr',
  image: 'image',
  video: 'video',
};

/**
 * Formatting toolbar bound to a textarea. `labels` gives each button its accessible name in the current language.
 * Image/video buttons call onUpload; insert the returned media with imageMarkdown()/videoMarkdown().
 */
export function MarkdownToolbar({
  target,
  labels,
  onUpload,
  split,
  onToggleSplit,
  onEdit,
  label = 'Markdown',
}: {
  target: RefObject<HTMLTextAreaElement | null>;
  labels: Record<MarkdownAction | 'image' | 'video' | 'split', string>;
  onUpload: (kind: 'image' | 'video') => void;
  split?: boolean;
  onToggleSplit?: () => void;
  onEdit?: (action: MarkdownAction) => void;
  label?: string;
}) {
  const run = (a: MarkdownAction) => {
    if (onEdit) {
      onEdit(a);
      return;
    }
    const el = target.current;
    if (!el) {
      return;
    }
    applyEdit(el, markdownEdit(el.value, el.selectionStart, el.selectionEnd, a));
  };
  return (
    <div className="vb-toolbar" role="toolbar" aria-label={label}>
      {GROUPS.map((g, gi) => (
        <span key={gi} className="vb-toolbar-group">
          {gi > 0 && <span className="vb-sep" aria-hidden="true" />}
          {g.map((a) => (
            <button
              key={a}
              type="button"
              className="vb-tool"
              aria-label={labels[a]}
              title={labels[a]}
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => (a === 'image' || a === 'video' ? onUpload(a) : run(a))}
            >
              {ICON[a] ? <Icon name={ICON[a]!} /> : a.toUpperCase()}
            </button>
          ))}
        </span>
      ))}
      {onToggleSplit && (
        <>
          <span className="vb-toolbar-spacer" />
          <button
            type="button"
            className="vb-tool wide vb-split-toggle"
            aria-pressed={!!split}
            onClick={onToggleSplit}
          >
            <Icon name="split" />
            {labels.split}
          </button>
        </>
      )}
    </div>
  );
}
