import { useLayoutEffect, useRef, useState } from 'react';
import { Button, Field, useI18n } from '@ui';
import type { Locale, PageBlock, PageBlockDocument } from '../../../api/content';
import { MediaLibrary, type UsedMedia } from '../../../features/owner/MediaLibrary';
import {
  contributionKey,
  localizedContribution,
  useExtensionCatalog,
} from '../../../features/owner/ExtensionCatalog';
import {
  blockColumns,
  blockKind,
  blockKinds,
  countBlocks,
  createBlock,
  moveBlock,
  type BlockKind,
} from './pageBlocks';

type ListProps = {
  blocks: PageBlock[];
  onChange: (blocks: PageBlock[]) => void;
  level: number;
  full: boolean;
  locale: Locale;
  focus: (id: string) => void;
  pickImage: (id: string) => void;
};

export function PageBlockEditor({
  value,
  locale,
  csrf,
  onChange,
}: {
  value: PageBlockDocument;
  locale: Locale;
  csrf: string;
  onChange: (value: PageBlockDocument) => void;
}) {
  const { t, locale: interfaceLocale } = useI18n();
  const [picker, setPicker] = useState('');
  const nextFocus = useRef('');
  useLayoutEffect(() => {
    if (nextFocus.current) {
      document.getElementById('page-block-' + nextFocus.current)?.focus();
      nextFocus.current = '';
    }
  }, [value, picker]);
  function choose(media: UsedMedia) {
    if (media.kind !== 'image') {
      return;
    }
    function select(blocks: PageBlock[]): PageBlock[] {
      return blocks.map((block) => {
        if (block.id === picker) {
          return { ...block, properties: { ...block.properties, media_id: media.id } };
        }
        const columns = blockColumns(block);
        return columns
          ? { ...block, properties: { ...block.properties, columns: columns.map(select) } }
          : block;
      });
    }
    nextFocus.current = picker;
    onChange({ ...value, blocks: select(value.blocks) });
    setPicker('');
  }
  return (
    <div className="block-editor" lang={interfaceLocale} dir={locale === 'ar' ? 'rtl' : 'ltr'}>
      {picker ? (
        <section aria-label={t('pageBlocks.chooseImage')}>
          <Button
            type="button"
            onClick={() => {
              nextFocus.current = picker;
              setPicker('');
            }}
          >
            {t('config.cancel')}
          </Button>
          <MediaLibrary csrf={csrf} imageOnly onSelect={choose} />
        </section>
      ) : (
        <BlockList
          blocks={value.blocks}
          onChange={(blocks) => onChange({ ...value, blocks })}
          level={1}
          locale={locale}
          full={countBlocks(value.blocks) >= 100}
          focus={(id) => {
            nextFocus.current = id;
          }}
          pickImage={setPicker}
        />
      )}
    </div>
  );
}

function BlockList({ blocks, onChange, level, full, locale, focus, pickImage }: ListProps) {
  const { t, locale: interfaceLocale } = useI18n();
  const { items } = useExtensionCatalog();
  const extensionBlocks = items.filter((item) => item.kind === 'builder_block');
  const [kind, setKind] = useState<string>('markdown');
  const selected = extensionBlocks.find((item) => contributionKey(item) === kind);
  const selectedCore = blockKinds.find((item) => item === kind) || 'markdown';
  const update = (index: number, block: PageBlock) =>
    onChange(blocks.map((item, i) => (i === index ? block : item)));
  return (
    <div className="block-editor-list">
      <ol className="block-editor-items">
        {blocks.map((block, index) => {
          const currentKind = blockKind(block);
          const extension = extensionBlocks.find(
            (item) => contributionKey(item) === block.type && item.version === block.version,
          );
          const columns = blockColumns(block);
          const change = (field: string, value: unknown) => {
            const properties = { ...block.properties, [field]: value };
            if (currentKind === 'hero' && field === 'media_id' && value === '') {
              delete properties.alt;
            }
            update(index, { ...block, properties });
          };
          const move = (offset: number) => {
            const reordered = moveBlock(blocks, index, offset);
            if (reordered !== blocks) {
              focus(block.id);
              onChange(reordered);
            }
          };
          return (
            <li key={block.id}>
              <fieldset
                id={'page-block-' + block.id}
                className="block-editor-card"
                tabIndex={-1}
                onKeyDown={(event) => {
                  if (event.target !== event.currentTarget || !event.altKey) {
                    return;
                  }
                  if (event.key === 'ArrowUp' || event.key === 'ArrowDown') {
                    event.preventDefault();
                    move(event.key === 'ArrowUp' ? -1 : 1);
                  }
                }}
              >
                <legend>
                  {index + 1}.{' '}
                  {currentKind
                    ? t('pageBlocks.' + currentKind)
                    : extension
                      ? localizedContribution(extension.label, interfaceLocale)
                      : t('pageBlocks.unavailable')}
                </legend>
                <div className="block-editor-actions">
                  {([-1, 1] as const).map((offset) => (
                    <Button
                      key={offset}
                      type="button"
                      disabled={index + offset < 0 || index + offset >= blocks.length}
                      onClick={() => move(offset)}
                    >
                      {t(offset < 0 ? 'pageBlocks.moveUp' : 'pageBlocks.moveDown')}
                    </Button>
                  ))}
                  <Button
                    type="button"
                    variant="quiet"
                    onClick={() => {
                      const neighbor = blocks[index + 1] ?? blocks[index - 1];
                      focus(neighbor ? neighbor.id : '');
                      onChange(blocks.filter((item) => item.id !== block.id));
                    }}
                  >
                    {t('config.remove')}
                  </Button>
                </div>
                {columns ? (
                  <div className="block-editor-columns">
                    {columns.map((col, column) => (
                      <fieldset key={column}>
                        <legend>{t('pageBlocks.column', { n: column + 1 })}</legend>
                        <BlockList
                          blocks={col}
                          onChange={(next) =>
                            change(
                              'columns',
                              columns.map((items, i) => (i === column ? next : items)),
                            )
                          }
                          level={level + 1}
                          locale={locale}
                          full={full}
                          focus={focus}
                          pickImage={pickImage}
                        />
                      </fieldset>
                    ))}
                    <div className="block-editor-actions">
                      {columns.length === 2 ? (
                        <Button type="button" onClick={() => change('columns', [...columns, []])}>
                          {t('pageBlocks.addColumn')}
                        </Button>
                      ) : (
                        <Button
                          type="button"
                          disabled={columns[2].length > 0}
                          onClick={() => change('columns', columns.slice(0, 2))}
                        >
                          {t('pageBlocks.removeColumn')}
                        </Button>
                      )}
                    </div>
                  </div>
                ) : currentKind ? (
                  <BlockFields
                    block={block}
                    kind={currentKind}
                    locale={locale}
                    change={change}
                    pickImage={pickImage}
                  />
                ) : extension ? (
                  <TextPanelFields block={block} locale={locale} change={change} />
                ) : (
                  <p className="vb-help">{t('pageBlocks.unknownHelp')}</p>
                )}
              </fieldset>
            </li>
          );
        })}
      </ol>
      <div className="block-editor-actions">
        <Field label={t('pageBlocks.type')}>
          <select
            value={selected ? kind : selectedCore}
            onChange={(event) => setKind(event.target.value)}
          >
            {blockKinds
              .filter((type) => type !== 'columns' || level < 3)
              .map((type) => (
                <option key={type} value={type}>
                  {t('pageBlocks.' + type)}
                </option>
              ))}
            {extensionBlocks.map((item) => (
              <option key={contributionKey(item)} value={contributionKey(item)}>
                {localizedContribution(item.label, interfaceLocale)}
              </option>
            ))}
          </select>
        </Field>
        <Button
          type="button"
          disabled={full}
          onClick={() => {
            const block = selected
              ? {
                  id: crypto.randomUUID(),
                  type: kind,
                  version: selected.version,
                  properties: { text: '' },
                }
              : createBlock(selectedCore);
            focus(block.id);
            onChange([...blocks, block]);
          }}
        >
          {t('pageBlocks.add')}
        </Button>
      </div>
    </div>
  );
}

function TextPanelFields({
  block,
  locale,
  change,
}: {
  block: PageBlock;
  locale: Locale;
  change: (field: string, value: unknown) => void;
}) {
  const { t } = useI18n();
  return (
    <>
      {(['heading', 'text'] as const).map((key) => (
        <Field key={key} label={t('pageBlocks.field.' + key)}>
          <input
            dir="auto"
            lang={locale}
            maxLength={key === 'heading' ? 200 : 500}
            value={typeof block.properties[key] === 'string' ? block.properties[key] : ''}
            onChange={(event) => change(key, event.target.value)}
          />
        </Field>
      ))}
    </>
  );
}

function BlockFields({
  block,
  kind,
  locale,
  change,
  pickImage,
}: {
  block: PageBlock;
  kind: BlockKind;
  locale: Locale;
  change: (field: string, value: unknown) => void;
  pickImage: (id: string) => void;
}) {
  const { t } = useI18n();
  const props = block.properties;
  const value = (key: string) => {
    const field = props[key];
    return typeof field === 'string' ? field : '';
  };
  const text = (key: string, maxLength: number) => (
    <Field label={t('pageBlocks.field.' + key)}>
      <input
        dir="auto"
        lang={locale}
        maxLength={maxLength}
        value={value(key)}
        onChange={(e) => change(key, e.target.value)}
      />
    </Field>
  );
  return (
    <>
      {kind === 'markdown' && (
        <Field label={t('owner.markdown')}>
          <textarea
            dir="auto"
            lang={locale}
            rows={7}
            value={value('markdown')}
            onChange={(e) => change('markdown', e.target.value)}
          />
        </Field>
      )}
      {kind === 'heading' && (
        <>
          {text('text', 200)}
          <Field label={t('pageBlocks.level')}>
            <select
              value={Number(props.level)}
              onChange={(e) => change('level', Number(e.target.value))}
            >
              {[2, 3, 4, 5, 6].map((level) => (
                <option key={level} value={level}>
                  {level}
                </option>
              ))}
            </select>
          </Field>
        </>
      )}
      {(kind === 'hero' || kind === 'call_to_action') && (
        <>
          {text('heading', 200)}
          {text('text', 1000)}
        </>
      )}
      {(kind === 'button' || kind === 'call_to_action') && (
        <>
          {text('label', 80)}
          {text('href', 2048)}
        </>
      )}
      {(kind === 'image' || kind === 'hero') && (
        <>
          <div className="block-editor-actions">
            {value('media_id') && (
              <img className="block-editor-image" src={'/media/' + value('media_id')} alt="" />
            )}
            <Button type="button" onClick={() => pickImage(block.id)}>
              {t('pageBlocks.chooseImage')}
            </Button>
            {kind === 'hero' && value('media_id') && (
              <Button type="button" onClick={() => change('media_id', '')}>
                {t('pageBlocks.clearImage')}
              </Button>
            )}
          </div>
          {(kind === 'image' || value('media_id')) && text('alt', 400)}
          {kind === 'image' && text('caption', 500)}
        </>
      )}
      {kind === 'content_list' && (
        <>
          <Field label={t('pageBlocks.section')}>
            <select value={value('section')} onChange={(e) => change('section', e.target.value)}>
              {['writing', 'pages', 'learning', 'docs'].map((section) => (
                <option value={section} key={section}>
                  {t(section === 'pages' ? 'owner.pages' : 'slot.' + section)}
                </option>
              ))}
            </select>
          </Field>
          <Field label={t('pageBlocks.limit')}>
            <input
              type="number"
              min={1}
              max={12}
              value={Number(props.limit)}
              onChange={(e) => change('limit', Number(e.target.value))}
            />
          </Field>
        </>
      )}
    </>
  );
}
