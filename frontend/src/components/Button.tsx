import type { ButtonHTMLAttributes } from 'react';

type Variant = 'acid' | 'paper' | 'ink' | 'danger';

interface Props extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: Variant;
  big?: boolean;
}

export function Button({ variant = 'acid', big, className = '', ...rest }: Props) {
  return <button className={`btn btn--${variant} ${big ? 'btn--big' : ''} ${className}`} {...rest} />;
}
