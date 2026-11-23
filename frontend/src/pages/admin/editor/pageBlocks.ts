import type { PageBlock, PageBlockDocument } from '../../../api/content';

const properties = {
  markdown: { markdown: '' },
  heading: { text: '', level: 2 },
  image: { media_id: '', alt: '' },
  button: { label: '', href: '' },
  columns: { columns: [[], []] },
  hero: { heading: '' },
  call_to_action: { heading: '', label: '', href: '' },
  content_list: { section: 'writing', limit: 6 },
};

export type BlockKind = keyof typeof properties;

export const blockKinds = Object.keys(properties) as BlockKind[];

export function blockKind(block: PageBlock): BlockKind | undefined {
  if (block.version !== 1 || !block.type.startsWith('core.')) {
    return undefined;
  }
  const kind = block.type.slice(5) as BlockKind;
  return blockKinds.includes(kind) ? kind : undefined;
}

export function blockColumns(block: PageBlock) {
  return blockKind(block) === 'columns' ? (block.properties.columns as PageBlock[][]) : undefined;
}

export function createBlock(kind: BlockKind, fields: Record<string, unknown> = {}): PageBlock {
  return {
    id: crypto.randomUUID(),
    type: 'core.' + kind,
    version: 1,
    properties: { ...structuredClone(properties[kind]), ...fields },
  };
}

export function landingBlocks(title: string, summary: string, fallback: string): PageBlockDocument {
  return {
    version: 1,
    blocks: [
      createBlock('hero', { heading: title.trim() || fallback, text: summary }),
      createBlock('content_list', { section: 'writing', limit: 3 }),
    ],
  };
}

export function textFitsMarkdownBlock(markdown: string) {
  return new TextEncoder().encode(markdown).length <= 64 * 1024;
}

export function countBlocks(blocks: PageBlock[]): number {
  return blocks.reduce(
    (n, block) =>
      n + 1 + (blockColumns(block)?.reduce((sum, col) => sum + countBlocks(col), 0) ?? 0),
    0,
  );
}

export function moveBlock(blocks: PageBlock[], from: number, offset: number) {
  const to = from + offset;
  if (to < 0 || to >= blocks.length) {
    return blocks;
  }
  const next = [...blocks];
  [next[from], next[to]] = [next[to], next[from]];
  return next;
}
