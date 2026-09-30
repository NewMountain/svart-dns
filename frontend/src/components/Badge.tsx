interface BadgeProps {
  variant:
    | 'block'
    | 'allow'
    | 'rewrite'
    | 'cache'
    | 'red'
    | 'green'
    | 'blue'
    | 'purple'
    | 'orange'
    | 'yellow';
  children: React.ReactNode;
}

export default function Badge({ variant, children }: BadgeProps) {
  return <span className={`badge badge-${variant}`}>{children}</span>;
}
