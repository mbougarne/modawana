import { Link } from 'react-router-dom';
import { useI18n } from '../../../../ui/src/i18n/I18nProvider';
import { PostList } from '../../../../ui/src/components/posts';
import type { DiscoveryEntry, TaxonomyTerm, PublicCollection } from '../../api/content';

export function EntryList({
  items,
  terms = [],
}: {
  items: DiscoveryEntry[];
  terms?: TaxonomyTerm[];
}) {
  const { t } = useI18n();
  if (!items.length) {
    return <p className="public-empty">{t('public.empty')}</p>;
  }
  return (
    <PostList
      posts={items.map((item) => {
        const category = terms.find(
          (term) => term.kind === 'category' && term.key === item.category_key,
        );
        return {
          id: item.key,
          locked: item.locked,
          slug: item.slug,
          kind: item.kind,
          title: item.title,
          publishedAt: item.published_at,
          href: item.url,
          readingMinutes: item.reading_minutes,
          category: category
            ? { id: category.key, slug: category.slug, label: category.label }
            : undefined,
          tags: item.tag_keys
            .map((key) => terms.find((term) => term.kind === 'tag' && term.key === key)?.label)
            .filter((label): label is string => !!label),
        };
      })}
    />
  );
}

export function CollectionCards({ items }: { items: PublicCollection[] }) {
  const { t } = useI18n();
  return (
    <div className="vb-learn">
      {items.map((item, index) => (
        <Link key={item.key} className="vb-course" to={item.url}>
          <span className={'vb-badge b' + ((index % 3) + 1)} aria-hidden="true">
            {Array.from(item.title.trim()).slice(0, 2).join('').toUpperCase()}
          </span>
          <div>
            <h3>{item.title}</h3>
            {item.summary && <p>{item.summary}</p>}
            <span className="vb-meta">
              {t('public.counts', {
                published: item.published_count,
                announced: item.announced_count,
              })}
            </span>
          </div>
        </Link>
      ))}
    </div>
  );
}
