import {
  cloneElement,
  useId,
  type ReactElement,
  forwardRef,
  type AnchorHTMLAttributes,
  type ButtonHTMLAttributes,
  type InputHTMLAttributes,
  type ReactNode,
  type SelectHTMLAttributes,
  type TextareaHTMLAttributes,
} from 'react';
import { cx } from './cx';

type BtnVariant = 'primary' | 'secondary' | 'quiet';
type BtnBase = {
  variant?: BtnVariant;
  size?: 'md' | 'sm';
  icon?: ReactNode;
  children?: ReactNode;
  className?: string;
};
type ButtonAsButton = BtnBase &
  Omit<ButtonHTMLAttributes<HTMLButtonElement>, keyof BtnBase> & { href?: undefined };
type ButtonAsLink = BtnBase &
  Omit<AnchorHTMLAttributes<HTMLAnchorElement>, keyof BtnBase> & { href: string };

/** Pill button. Pass href to render a link styled as a button. One primary per view. */
export function Button(props: ButtonAsButton | ButtonAsLink) {
  const { variant = 'secondary', size = 'md', icon, children, className } = props;
  const cls = cx('vb-btn', `vb-btn-${variant}`, size === 'sm' && 'vb-btn-sm', className);
  if (props.href !== undefined) {
    const {
      variant: _v,
      size: _s,
      icon: _i,
      children: _c,
      className: _cl,
      ...a
    } = props as ButtonAsLink;
    return (
      <a className={cls} {...a}>
        {icon}
        {children}
      </a>
    );
  }
  const {
    variant: _v,
    size: _s,
    icon: _i,
    children: _c,
    className: _cl,
    ...b
  } = props as ButtonAsButton;
  return (
    <button type="button" className={cls} {...b}>
      {icon}
      {children}
    </button>
  );
}

/** Small round icon-only button. Always give it a label. */
export function IconButton({
  label,
  children,
  className,
  ...rest
}: { label: string; children: ReactNode } & ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      type="button"
      className={cx('vb-icon', className)}
      aria-label={label}
      title={label}
      {...rest}
    >
      {children}
    </button>
  );
}

/** Mono pill. tone: new (sun), lesson (rose outline), doc (blue soft), default (plain). */
export function Tag({
  tone = 'default',
  children,
}: {
  tone?: 'default' | 'new' | 'lesson' | 'doc';
  children: ReactNode;
}) {
  return <span className={cx('vb-tag', tone !== 'default' && `vb-tag-${tone}`)}>{children}</span>;
}

/** Hand-placed rotated label. Use one or two per screen. */
export function Sticker({
  tone = 'sun',
  children,
}: {
  tone?: 'sun' | 'blue' | 'plain';
  children: ReactNode;
}) {
  return <span className={cx('vb-sticker', tone !== 'sun' && tone)}>{children}</span>;
}

/** Category filter chip (single choice). */
export function Chip({
  pressed,
  count,
  children,
  ...rest
}: {
  pressed: boolean;
  count?: number;
  children: ReactNode;
} & ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button type="button" className="vb-chip" aria-pressed={pressed} {...rest}>
      {children}
      {count !== undefined && <small>{count}</small>}
    </button>
  );
}

/** Tag filter chip, rendered as #tag. A second click should clear it. */
export function TagChip({
  tag,
  pressed,
  ...rest
}: { tag: string; pressed: boolean } & ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button type="button" className="vb-tagchip" aria-pressed={pressed} dir="ltr" {...rest}>
      #{tag}
    </button>
  );
}

export function Card({
  title,
  children,
  className,
}: {
  title?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <section className={cx('vb-card', className)}>
      {title && <h3>{title}</h3>}
      {children}
    </section>
  );
}

/** Label + control + help text. Wrap any input. */
export function Field({
  label,
  help,
  children,
}: {
  label: ReactNode;
  help?: ReactNode;
  children: ReactElement<{ id?: string; 'aria-describedby'?: string }>;
}) {
  const generated = useId();
  const id = children.props.id || generated;
  const description =
    [children.props['aria-describedby'], help ? generated + '-help' : undefined]
      .filter(Boolean)
      .join(' ') || undefined;
  return (
    <div className="vb-field">
      <label htmlFor={id}>{label}</label>
      {cloneElement(children, { id, 'aria-describedby': description })}
      {help && (
        <span className="vb-help" id={generated + '-help'}>
          {help}
        </span>
      )}
    </div>
  );
}

export const TextInput = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement>>(
  function TextInput({ className, ...rest }, ref) {
    return <input ref={ref} className={cx('vb-input', className)} dir="auto" {...rest} />;
  },
);

export const TextArea = forwardRef<
  HTMLTextAreaElement,
  TextareaHTMLAttributes<HTMLTextAreaElement>
>(function TextArea({ className, ...rest }, ref) {
  return <textarea ref={ref} className={cx('vb-input', className)} dir="auto" {...rest} />;
});

export function Select({ className, children, ...rest }: SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select className={cx('vb-select', className)} {...rest}>
      {children}
    </select>
  );
}

export function Checkbox({
  label,
  ...rest
}: { label: ReactNode } & InputHTMLAttributes<HTMLInputElement>) {
  return (
    <label className="vb-check">
      <input type="checkbox" {...rest} /> {label}
    </label>
  );
}

export function Switch({
  label,
  ...rest
}: { label: ReactNode } & InputHTMLAttributes<HTMLInputElement>) {
  return (
    <label className="vb-switch">
      <input type="checkbox" role="switch" {...rest} />
      <span aria-hidden="true" />
      {label}
    </label>
  );
}

/** Truthful save state for editors and settings: saved · dirty (unsaved) · live (published) · busy. */
export function SaveState({
  state,
  children,
}: {
  state: 'saved' | 'dirty' | 'live' | 'busy';
  children: ReactNode;
}) {
  return (
    <span className="vb-state" data-s={state === 'busy' ? '' : state} role="status">
      {children}
    </span>
  );
}
