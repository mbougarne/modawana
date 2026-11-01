/* eslint-disable no-control-regex -- Reject control bytes in untrusted URLs. */
import { Children, isValidElement, useState, type ReactNode } from 'react';
import Markdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import remarkMath from 'remark-math';
import rehypeKatex from 'rehype-katex';
import { CodeBlock, Prose } from '../../../../ui/src/components/posts';
import { useI18n } from '../../../../ui/src/i18n/I18nProvider';
import type { MediaPresentation, PublicMedia } from '../../api/content';

const mediaPath = /^\/media\/([0-9a-f]{32})$/;

export function safeLink(value: string): string | undefined {
  if (!value || /[\u0000-\u0020\u007f\\]/.test(value)) {
    return;
  }
  let decoded: string;
  try {
    decoded = decodeURIComponent(value);
  } catch {
    return;
  }
  if (/[\u0000-\u0020\u007f\\]/.test(decoded) || decoded.startsWith('//')) {
    return;
  }
  if (value.startsWith('#') || (value.startsWith('/') && !value.startsWith('//'))) {
    return value;
  }
  try {
    const u = new URL(value);
    if ((u.protocol === 'https:' || u.protocol === 'http:') && !u.username && !u.password) {
      return value;
    }
    if (u.protocol === 'mailto:' && /^[^?@\s]+@[^?@\s]+$/.test(value.slice(7))) {
      return value;
    }
  } catch {
    /* Unsafe or relative URL. */
  }
}
function codeText(children: ReactNode): string {
  return Children.toArray(children)
    .map((child) =>
      typeof child === 'string'
        ? child
        : isValidElement<{ children?: ReactNode }>(child)
          ? codeText(child.props.children)
          : '',
    )
    .join('');
}
function ControlledMedia({
  item,
  alt,
  presentation,
  manifest,
}: {
  item: PublicMedia;
  alt: string;
  presentation?: MediaPresentation;
  manifest: Map<string, PublicMedia>;
}) {
  const { t, locale } = useI18n();
  const [failed, setFailed] = useState(false);
  const src = '/media/' + item.id;
  if (failed) {
    return <span role="status">{t('reader.mediaError')}</span>;
  }
  if (item.kind === 'image') {
    return (
      <img
        src={src}
        alt={alt}
        loading="lazy"
        width={item.width}
        height={item.height}
        onError={() => setFailed(true)}
      />
    );
  }
  if (item.kind === 'video') {
    const poster = presentation?.poster_media_id && manifest.get(presentation.poster_media_id);
    const track =
      presentation?.caption_track_media_id && manifest.get(presentation.caption_track_media_id);
    return (
      <figure className="vb-media">
        <video
          controls
          playsInline
          preload="metadata"
          src={src}
          title={alt}
          poster={poster && poster.kind === 'image' ? '/media/' + poster.id : undefined}
          onError={() => setFailed(true)}
        >
          {track && track.kind === 'captions' && (
            <track
              kind="captions"
              src={'/media/' + track.id}
              srcLang={locale}
              label={t('reader.captions')}
              default
            />
          )}
        </video>
        {presentation?.caption && <figcaption>{presentation.caption}</figcaption>}
        {presentation?.transcript && (
          <details>
            <summary>{t('reader.transcript')}</summary>
            <p>{presentation.transcript}</p>
          </details>
        )}
      </figure>
    );
  }
  return (
    <a href={src} download>
      {alt || t('reader.download')}
    </a>
  );
}

/** Public reading and authenticated previews share this exact pipeline. The server supplies the authorized manifest. */
export function MarkdownReader({
  markdown,
  media = [],
  presentations = [],
  takeaway,
}: {
  markdown: string;
  media?: PublicMedia[];
  presentations?: MediaPresentation[];
  takeaway?: string;
}) {
  const { t, dir } = useI18n();
  const manifest = new Map(media.filter((m) => /^[0-9a-f]{32}$/.test(m.id)).map((m) => [m.id, m]));
  const lookup = (path?: string) => {
    const id = path?.match(mediaPath)?.[1];
    return id ? manifest.get(id) : undefined;
  };
  const figure = (item: PublicMedia, alt: string) => (
    <ControlledMedia
      item={item}
      alt={alt}
      manifest={manifest}
      presentation={presentations.find((p) => p.media_id === item.id)}
    />
  );
  if (new TextEncoder().encode(markdown).length > 1048576) {
    return <p role="alert">{t('reader.tooLarge')}</p>;
  }
  return (
    <div className="vb-reader" dir={dir}>
      {takeaway && (
        <aside className="vb-takeaway">
          <h2>{t('reader.takeaway')}</h2>
          <MarkdownReader markdown={takeaway} media={media} presentations={presentations} />
        </aside>
      )}
      <Prose>
        <Markdown
          skipHtml
          remarkPlugins={[remarkGfm, remarkMath]}
          rehypePlugins={[
            [
              rehypeKatex,
              { output: 'mathml', trust: false, strict: 'error', maxExpand: 1000, maxSize: 20 },
            ],
          ]}
          urlTransform={(value) => safeLink(value) ?? ''}
          components={{
            img: ({ src, alt }) => {
              const m = lookup(typeof src === 'string' ? src : undefined);
              return m?.kind === 'image' ? (
                figure(m, alt ?? '')
              ) : (
                <span>{t('reader.mediaUnavailable')}</span>
              );
            },
            a: ({ href, children }) => {
              if (href?.startsWith('/media/')) {
                const m = lookup(href);
                return m ? (
                  <a href={'/media/' + m.id} download={m.kind === 'pdf' || undefined}>
                    {children}
                  </a>
                ) : (
                  <span>
                    {children} ({t('reader.mediaUnavailable')})
                  </span>
                );
              }
              const url = safeLink(href ?? '');
              return url ? (
                <a href={url} rel="noopener noreferrer">
                  {children}
                </a>
              ) : (
                <span>{children}</span>
              );
            },
            p: ({ node, children }) => {
              const only = node?.children.length === 1 ? node.children[0] : undefined;
              if (only?.type === 'element' && only.tagName === 'a') {
                const m = lookup(String(only.properties.href ?? ''));
                if (m?.kind === 'video') {
                  return figure(m, codeText(children));
                }
              }
              return <p>{children}</p>;
            },
            pre: ({ children }) => <CodeBlock code={codeText(children).replace(/\n$/, '')} />,
            code: ({ children, className }) => (
              <code dir="ltr" className={className}>
                {children}
              </code>
            ),
            table: ({ children }) => (
              <div
                className="vb-reader-table"
                role="region"
                aria-label={t('reader.table')}
                tabIndex={0}
              >
                <table>{children}</table>
              </div>
            ),
          }}
        >
          {markdown}
        </Markdown>
      </Prose>
    </div>
  );
}
