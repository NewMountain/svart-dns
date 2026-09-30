import { ReactNode, useEffect, useRef } from 'react';

interface MenuItem {
  label: string;
  icon?: ReactNode;
  onClick: () => void;
  danger?: boolean;
  success?: boolean;
}

interface ContextMenuProps {
  x: number;
  y: number;
  header?: string;
  items: MenuItem[];
  onClose: () => void;
}

export default function ContextMenu({ x, y, header, items, onClose }: ContextMenuProps) {
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    function handleClick(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        onClose();
      }
    }
    function handleEsc(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose();
    }
    document.addEventListener('mousedown', handleClick);
    document.addEventListener('keydown', handleEsc);
    return () => {
      document.removeEventListener('mousedown', handleClick);
      document.removeEventListener('keydown', handleEsc);
    };
  }, [onClose]);

  const style: React.CSSProperties = {
    position: 'fixed',
    left: Math.min(x, window.innerWidth - 220),
    top: Math.min(y, window.innerHeight - items.length * 40 - 60),
  };

  return (
    <div className="context-menu" ref={ref} style={style}>
      {header && <div className="menu-header">{header}</div>}
      {items.map((item, i) => (
        <div
          key={i}
          className={`menu-item${item.danger ? ' danger' : ''}${item.success ? ' success' : ''}`}
          onClick={() => {
            item.onClick();
            onClose();
          }}
        >
          {item.icon}
          {item.label}
        </div>
      ))}
    </div>
  );
}
